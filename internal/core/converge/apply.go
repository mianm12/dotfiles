package converge

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/storage"
)

// Apply performs a complete read-only preflight, acquires the lock, repeats
// the same analysis from current inputs, and executes only the locked plan.
func Apply(environment Environment) (result ApplyResult, err error) {
	preflight, blockedResult, err := prepareApply(environment)
	if err != nil {
		return blockedResult, err
	}
	release, err := acquire(environment, preflight.controls)
	if err != nil {
		return ApplyResult{}, err
	}
	defer func() { err = joinReleaseError(err, release()) }()
	return applyLocked(environment, preflight.fingerprint)
}

type applyPreflight struct {
	controls    corepaths.Controls
	fingerprint []byte
}

func prepareApply(environment Environment) (applyPreflight, ApplyResult, error) {
	preflight, err := analyzeEnvironment(environment)
	if err != nil {
		return applyPreflight{}, ApplyResult{}, err
	}
	if !preflight.report.Plan.Executable() {
		result := ApplyResult{Report: cloneReport(preflight.report)}
		return applyPreflight{}, result, blockedError(preflight.report.Plan)
	}
	return applyPreflight{
		controls:    preflight.controls,
		fingerprint: append([]byte(nil), preflight.fingerprint...),
	}, ApplyResult{}, nil
}

func applyLocked(environment Environment, expectedFingerprint []byte) (ApplyResult, error) {
	locked, err := analyzeEnvironment(environment)
	if err != nil {
		return ApplyResult{}, err
	}
	if !bytes.Equal(expectedFingerprint, locked.fingerprint) {
		return ApplyResult{Report: cloneReport(locked.report)}, fmt.Errorf(
			"%w: machine config changed while waiting for the mutation lock",
			ErrBlocked,
		)
	}
	if !locked.report.Plan.Executable() {
		return ApplyResult{Report: cloneReport(locked.report)}, blockedError(locked.report.Plan)
	}
	execution, runErr := executePlan(
		locked.loaded.Snapshot.Home,
		locked.controls.State,
		locked.report.Plan,
		locked.loaded,
		commitState,
	)
	result := ApplyResult{
		Report:         cloneReport(locked.report),
		TargetsChanged: execution.TargetsChanged,
		StateChanged:   execution.StateChanged,
	}
	return result, runErr
}

func blockedError(plan Plan) error {
	return fmt.Errorf("%w: %w", ErrBlocked, conflictError(plan))
}

func normalizeEnvironment(environment Environment) (Environment, error) {
	home, err := cleanAbsolute("HOME", environment.Home)
	if err != nil {
		return Environment{}, err
	}
	configPath, err := cleanAbsolute("machine config", environment.ConfigPath)
	if err != nil {
		return Environment{}, err
	}
	statePath, err := cleanAbsolute("state", environment.StatePath)
	if err != nil {
		return Environment{}, err
	}
	lockPath, err := cleanAbsolute("lock", environment.LockPath)
	if err != nil {
		return Environment{}, err
	}
	return Environment{
		Home:       home,
		ConfigPath: configPath,
		StatePath:  statePath,
		LockPath:   lockPath,
		Platform:   environment.Platform,
	}, nil
}

func cleanAbsolute(label, path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) ||
		strings.ContainsRune(path, '\x00') || !utf8.ValidString(path) {
		return "", fmt.Errorf(
			"%s must be a non-empty absolute path without NUL and with valid UTF-8",
			label,
		)
	}
	return filepath.Clean(path), nil
}

func environmentControls(environment Environment, repository string) corepaths.Controls {
	return corepaths.Controls{
		Repository: filepath.Clean(repository),
		Config:     environment.ConfigPath,
		State:      environment.StatePath,
		Lock:       environment.LockPath,
	}
}

func validateControls(controls corepaths.Controls) error {
	controls = cleanControls(controls)
	if err := validateControlTopology(controls); err != nil {
		return err
	}
	return validateControlEntries(controls.Config, controls.State, controls.Lock)
}

func validateEnvironmentControls(environment Environment) error {
	return validateControlEntries(
		environment.ConfigPath,
		environment.StatePath,
		environment.LockPath,
	)
}

func validateControlTopology(controls corepaths.Controls) error {
	if err := corepaths.ValidateControlTopology(controls); err != nil {
		return controlError{cause: err}
	}
	return nil
}

func validateControlEntries(configPath, statePath, lockPath string) error {
	configRoot := filepath.Dir(configPath)
	if err := storage.ValidateRoot(configRoot); err != nil {
		return controlError{cause: fmt.Errorf("validate machine config root %q: %w", configRoot, err)}
	}
	if err := storage.ValidatePrivateFile(configPath); err != nil {
		return controlError{cause: fmt.Errorf("validate machine config %q: %w", configPath, err)}
	}
	stateRoot := filepath.Dir(statePath)
	if err := validateLock(stateRoot, lockPath); err != nil {
		return controlError{cause: fmt.Errorf("validate state root and lock %q: %w", lockPath, err)}
	}
	if err := storage.ValidatePrivateFile(statePath); err != nil {
		return controlError{cause: fmt.Errorf("validate state %q: %w", statePath, err)}
	}
	return nil
}

type controlError struct {
	cause error
}

func (err controlError) Error() string {
	return err.cause.Error()
}

func (err controlError) Unwrap() error {
	return err.cause
}

func (err controlError) Is(target error) bool {
	return target == ErrControl
}

func acquire(environment Environment, controls corepaths.Controls) (func() error, error) {
	if _, err := normalizeEnvironment(environment); err != nil {
		return nil, err
	}
	controls = cleanControls(controls)
	return acquireLock(filepath.Dir(controls.Lock), controls.Lock)
}

func requireMachine(path string) (config.Machine, error) {
	machine, exists, err := config.LoadMachine(path)
	if err != nil {
		return config.Machine{}, err
	}
	if !exists {
		return config.Machine{}, fmt.Errorf("%w: machine config %q is missing", ErrUninitialized, path)
	}
	return machine, nil
}

func machineFingerprint(machine config.Machine) ([]byte, error) {
	semantic := cloneMachine(machine)
	semantic.Profiles = sortedUnique(semantic.Profiles)
	semantic.ExtraModules = sortedUnique(semantic.ExtraModules)
	return config.MarshalMachine(semantic)
}

func sortedUnique(values []string) []string {
	result := append([]string(nil), values...)
	slices.Sort(result)
	return slices.Compact(result)
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

func joinReleaseError(runErr, releaseErr error) error {
	if releaseErr == nil {
		return runErr
	}
	partial := fmt.Errorf("%w: release mutation lock: %w", ErrPartial, releaseErr)
	return errors.Join(runErr, partial)
}
