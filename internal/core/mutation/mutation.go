// Package mutation owns control validation, the advisory lock, selection
// publication, and artifact-apply orchestration.
package mutation

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/executor"
	mutationlock "github.com/mianm12/dotfiles/internal/core/mutation/internal/lock"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/selection"
	"github.com/mianm12/dotfiles/internal/storage"
)

var (
	// ErrBusy reports that another dot mutation currently owns the lock.
	ErrBusy = mutationlock.ErrBusy
	// ErrLockIO reports lock preparation, acquisition, or release I/O failure.
	ErrLockIO = mutationlock.ErrIO
)

// Result reports one applied artifact plan.
type Result = executor.Result

// ApplyRequest identifies one convergence operation. Machine is the exact
// machine selection observed during preflight; any locked config drift fails.
type ApplyRequest struct {
	Home      string
	Controls  corepaths.Controls
	Machine   config.Machine
	Platform  config.Platform
	RerunHint string
}

// Apply validates without writing, acquires the mutation lock, reloads and
// resolves current desired state, then applies and commits it.
func Apply(request ApplyRequest) (result Result, err error) {
	modules, err := resolveApply(request)
	if err != nil {
		return Result{}, err
	}
	preflight, err := executor.Analyze(executionRequest(request, modules))
	if err != nil {
		return Result{}, err
	}
	if blocked := preflight.BlockingError(); blocked != nil {
		return Result{
			Plan:     preflight.Plan,
			Warnings: preflight.Warnings,
		}, blocked
	}
	release, err := acquire(request.Home, request.Controls)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		err = joinReleaseError(err, release(), request.RerunHint)
	}()

	modules, err = resolveApply(request)
	if err != nil {
		return Result{}, err
	}
	return executor.Execute(executionRequest(request, modules))
}

func executionRequest(request ApplyRequest, modules []config.Module) executor.Request {
	return executor.Request{
		Home:     request.Home,
		Controls: cleanControls(request.Controls),
		Modules:  modules,
	}
}

func resolveApply(request ApplyRequest) ([]config.Module, error) {
	if _, err := validateHome(request.Home); err != nil {
		return nil, err
	}
	controls := cleanControls(request.Controls)
	if err := ValidateControls(controls); err != nil {
		return nil, err
	}
	current, err := requireExpectedMachine(controls, request.Machine)
	if err != nil {
		return nil, err
	}
	repository, err := config.OpenRepository(current.Repository)
	if err != nil {
		return nil, err
	}

	resolved, err := selection.Resolve(repository, current, request.Platform)
	if err != nil {
		return nil, err
	}
	if len(resolved.Issues) != 0 {
		issue := resolved.Issues[0]
		return nil, fmt.Errorf(
			"operation blocked for module %q: %s",
			issue.ModuleID,
			issue.Reason,
		)
	}
	return resolved.Modules, nil
}

// SelectionOperation identifies one machine-selection edit.
type SelectionOperation uint8

const (
	// SelectionInitialize creates the first machine configuration.
	SelectionInitialize SelectionOperation = iota + 1
	// SelectionAdd adds a direct extra module selection.
	SelectionAdd
	// SelectionRemove removes a direct extra module selection.
	SelectionRemove
)

// SelectionRequest describes a config-only selection edit. Machine is the
// desired initial config for initialize and the exact expected config for add/remove.
type SelectionRequest struct {
	Home      string
	Controls  corepaths.Controls
	Operation SelectionOperation
	Machine   config.Machine
	ModuleID  string
	Platform  config.Platform
	RerunHint string
}

// SelectionResult reports the resulting config and whether an active profile
// still selects the requested module.
type SelectionResult struct {
	Machine         config.Machine
	Changed         bool
	ProfileSelected bool
}

func checkSelection(request SelectionRequest) (SelectionResult, error) {
	if _, err := validateHome(request.Home); err != nil {
		return SelectionResult{}, err
	}
	controls := cleanControls(request.Controls)
	if err := ValidateControls(controls); err != nil {
		return SelectionResult{}, err
	}
	if request.Machine.Repository != controls.Repository {
		return SelectionResult{}, fmt.Errorf(
			"machine repository %q does not match mutation repository %q",
			request.Machine.Repository,
			controls.Repository,
		)
	}

	var current config.Machine
	switch request.Operation {
	case SelectionInitialize:
		if _, err := config.MarshalMachine(request.Machine); err != nil {
			return SelectionResult{}, err
		}
		_, exists, err := config.LoadMachine(controls.Config)
		if err != nil {
			return SelectionResult{}, err
		}
		if exists {
			return SelectionResult{}, fmt.Errorf(
				"machine is already initialized at %q",
				controls.Config,
			)
		}
		current = cloneMachine(request.Machine)
	case SelectionAdd, SelectionRemove:
		var err error
		current, err = requireExpectedMachine(controls, request.Machine)
		if err != nil {
			return SelectionResult{}, err
		}
		if err := validateModuleID(current.Repository, request.ModuleID); err != nil {
			return SelectionResult{}, err
		}
	default:
		return SelectionResult{}, fmt.Errorf(
			"unsupported selection operation %d",
			request.Operation,
		)
	}

	repository, err := config.OpenRepository(current.Repository)
	if err != nil {
		return SelectionResult{}, err
	}
	profileModules, err := repository.ProfileModules(current.Profiles)
	if err != nil {
		return SelectionResult{}, err
	}
	result := SelectionResult{
		Machine:         cloneMachine(current),
		ProfileSelected: slices.Contains(profileModules, request.ModuleID),
	}

	switch request.Operation {
	case SelectionInitialize:
		result.Changed = true
	case SelectionAdd:
		if result.ProfileSelected || slices.Contains(current.ExtraModules, request.ModuleID) {
			return result, nil
		}
		_, exists, applicability, err := repository.InspectModule(
			request.ModuleID,
			request.Platform,
		)
		if err != nil {
			return SelectionResult{}, err
		}
		if !exists {
			return SelectionResult{}, fmt.Errorf("unknown module %q", request.ModuleID)
		}
		switch applicability.State {
		case config.ApplicabilityApplicable:
		case config.ApplicabilityNotApplicable:
			return SelectionResult{}, fmt.Errorf("module %q is not applicable", request.ModuleID)
		case config.ApplicabilityIndeterminate:
			return SelectionResult{}, fmt.Errorf(
				"module %q applicability is indeterminate: %s",
				request.ModuleID,
				applicability.Diagnostic,
			)
		default:
			return SelectionResult{}, fmt.Errorf(
				"module %q returned invalid applicability %q",
				request.ModuleID,
				applicability.State,
			)
		}
		result.Machine.ExtraModules = append(result.Machine.ExtraModules, request.ModuleID)
		slices.Sort(result.Machine.ExtraModules)
		result.Changed = true
	case SelectionRemove:
		result.Changed = slices.Contains(current.ExtraModules, request.ModuleID)
		result.Machine.ExtraModules = slices.DeleteFunc(
			result.Machine.ExtraModules,
			func(candidate string) bool { return candidate == request.ModuleID },
		)
	}
	return result, nil
}

// UpdateSelection performs preflight, acquires the lock, repeats the complete
// validation against current inputs, and atomically publishes a changed config.
func UpdateSelection(request SelectionRequest) (result SelectionResult, err error) {
	if _, err := checkSelection(request); err != nil {
		return SelectionResult{}, err
	}
	release, err := acquire(request.Home, request.Controls)
	if err != nil {
		return SelectionResult{}, err
	}
	defer func() {
		err = joinReleaseError(err, release(), request.RerunHint)
	}()

	result, err = checkSelection(request)
	if err != nil {
		return SelectionResult{}, err
	}
	if !result.Changed {
		return result, nil
	}
	configPath := cleanControls(request.Controls).Config
	data, err := config.MarshalMachine(result.Machine)
	if err != nil {
		return SelectionResult{}, err
	}
	changed, err := storage.PublishPrivateFile(configPath, data)
	if err != nil {
		return SelectionResult{}, fmt.Errorf(
			"publish machine config %q: %w",
			configPath,
			err,
		)
	}
	result.Changed = changed
	return result, nil
}

// ValidateControls performs every read-only control check required before lock acquisition.
func ValidateControls(controls corepaths.Controls) error {
	controls = cleanControls(controls)
	if err := corepaths.ValidateControlTopology(controls); err != nil {
		return err
	}
	configRoot := filepath.Dir(controls.Config)
	if err := storage.ValidateRoot(configRoot); err != nil {
		return fmt.Errorf(
			"validate machine config root %q before mutation: %w; "+
				"run `dot paths` to inspect the active control paths",
			configRoot,
			err,
		)
	}
	if err := storage.ValidatePrivateFile(controls.Config); err != nil {
		return fmt.Errorf(
			"validate machine config %q before mutation: %w; "+
				"run `dot paths` to inspect the active control paths",
			controls.Config,
			err,
		)
	}
	stateRoot := filepath.Dir(controls.State)
	if err := mutationlock.Validate(stateRoot, controls.Lock); err != nil {
		return fmt.Errorf(
			"validate state root and lock %q before mutation: %w; "+
				"run `dot paths` to inspect the active control paths",
			controls.Lock,
			err,
		)
	}
	if err := storage.ValidatePrivateFile(controls.State); err != nil {
		return fmt.Errorf(
			"validate state %q before mutation: %w; "+
				"run `dot paths` to inspect the active control paths",
			controls.State,
			err,
		)
	}
	return nil
}

func acquire(home string, controls corepaths.Controls) (func() error, error) {
	if _, err := validateHome(home); err != nil {
		return nil, err
	}
	controls = cleanControls(controls)
	return mutationlock.Acquire(filepath.Dir(controls.Lock), controls.Lock)
}

func requireExpectedMachine(
	controls corepaths.Controls,
	expected config.Machine,
) (config.Machine, error) {
	current, exists, err := config.LoadMachine(controls.Config)
	if err != nil {
		return config.Machine{}, err
	}
	if !exists {
		return config.Machine{}, fmt.Errorf(
			"machine config %q is missing; run dot init",
			controls.Config,
		)
	}
	if current.Repository != controls.Repository {
		return config.Machine{}, fmt.Errorf(
			"machine repository changed from %q to %q while waiting for the mutation lock",
			controls.Repository,
			current.Repository,
		)
	}
	equal, err := equalMachine(current, expected)
	if err != nil {
		return config.Machine{}, err
	}
	if !equal {
		return config.Machine{}, fmt.Errorf(
			"machine config changed while waiting for the mutation lock; rerun the command",
		)
	}
	return current, nil
}

func validateModuleID(repository, moduleID string) error {
	_, err := config.MarshalMachine(config.Machine{
		Version:      1,
		Repository:   repository,
		ExtraModules: []string{moduleID},
	})
	return err
}

func equalMachine(left, right config.Machine) (bool, error) {
	leftData, err := config.MarshalMachine(left)
	if err != nil {
		return false, err
	}
	rightData, err := config.MarshalMachine(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftData, rightData), nil
}

func cloneMachine(machine config.Machine) config.Machine {
	return config.Machine{
		Version:      machine.Version,
		Repository:   machine.Repository,
		Profiles:     append([]string(nil), machine.Profiles...),
		ExtraModules: append([]string(nil), machine.ExtraModules...),
	}
}

func cleanControls(controls corepaths.Controls) corepaths.Controls {
	return corepaths.Controls{
		Repository: cleanPath(controls.Repository),
		Config:     cleanPath(controls.Config),
		State:      cleanPath(controls.State),
		Lock:       cleanPath(controls.Lock),
	}
}

func cleanPath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func validateHome(home string) (string, error) {
	if home == "" || !filepath.IsAbs(home) ||
		strings.ContainsRune(home, '\x00') || !utf8.ValidString(home) {
		return "", fmt.Errorf(
			"mutation HOME must be a non-empty absolute path without NUL and with valid UTF-8",
		)
	}
	return filepath.Clean(home), nil
}

func joinReleaseError(runErr, releaseErr error, rerun string) error {
	if releaseErr == nil {
		return runErr
	}
	message := "release mutation lock: %w; mutation may already be applied"
	if rerun != "" {
		message += "; rerun " + rerun
	}
	return errors.Join(runErr, fmt.Errorf(message, releaseErr))
}
