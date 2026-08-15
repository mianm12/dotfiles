package converge

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/storage"
)

var (
	// ErrMachineUninitialized reports a missing machine configuration.
	ErrMachineUninitialized = errors.New("machine is not initialized")
	// ErrControlPaths reports invalid control topology or entry types.
	ErrControlPaths = errors.New("invalid control paths")
)

// Apply validates only the lock acquisition boundary before acquiring the
// mutation lock. Its one complete analysis and loop are formed inside the
// lock from current inputs.
func Apply(environment Environment) (result ApplyResult, err error) {
	environment, err = prepareMutationEnvironment(environment)
	if err != nil {
		return ApplyResult{}, err
	}
	release, err := acquireLock(filepath.Dir(environment.LockPath), environment.LockPath)
	if err != nil {
		return ApplyResult{}, newFailure(false, nil, err)
	}
	var mutated bool
	defer func() {
		releaseErr := release()
		err = joinReleaseFailure(err, releaseErr, mutated)
	}()
	result, runErr := applyLocked(environment)
	mutated = result.TargetsChanged || result.StateChanged || result.ControlsChanged
	return result, runErr
}

func applyLocked(environment Environment) (ApplyResult, error) {
	locked, err := analyzePreparedEnvironment(environment)
	if err != nil {
		return ApplyResult{}, newFailure(false, nil, err)
	}
	if locked.report.HasSkip() {
		return ApplyResult{
			Report: locked.report,
		}, nil
	}
	controlPaths, err := locked.controls.Paths()
	if err != nil {
		return ApplyResult{}, newFailure(false, nil, err)
	}
	execution, runErr := executeLines(
		controlPaths.State,
		locked.lines,
		locked.loaded,
		commitState,
	)
	result := ApplyResult{
		Report:          locked.report,
		Done:            execution.Done,
		TargetsChanged:  execution.TargetsChanged,
		StateChanged:    execution.StateChanged,
		ControlsChanged: execution.ControlsChanged,
	}
	if runErr != nil {
		return result, runErr
	}
	return result, nil
}

func prepareMutationEnvironment(environment Environment) (Environment, error) {
	normalized, err := normalizeEnvironment(environment)
	if err != nil {
		return Environment{}, newFailure(false, nil, err)
	}
	if err := validateLockBoundary(normalized); err != nil {
		return Environment{}, newFailure(false, nil, err)
	}
	return normalized, nil
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

func resolvePlatform(environment Environment) (config.Platform, error) {
	if environment.Platform == nil {
		return config.Platform{}, fmt.Errorf("platform resolver is unavailable")
	}
	return environment.Platform(), nil
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

func validateControls(controls corepaths.Controls) (corepaths.LexicalControls, error) {
	normalized, err := normalizeControls(controls)
	if err != nil {
		return corepaths.LexicalControls{}, err
	}
	paths, err := normalized.Paths()
	if err != nil {
		return corepaths.LexicalControls{}, err
	}
	if err := validateControlEntries(paths.Config, paths.State, paths.Lock); err != nil {
		return corepaths.LexicalControls{}, err
	}
	return normalized, nil
}

func validateLockBoundary(environment Environment) error {
	if err := corepaths.ValidateLockBoundary(
		environment.ConfigPath,
		environment.StatePath,
		environment.LockPath,
	); err != nil {
		return controlError{cause: err}
	}
	return validateControlEntries(
		environment.ConfigPath,
		environment.StatePath,
		environment.LockPath,
	)
}

func normalizeControls(controls corepaths.Controls) (corepaths.LexicalControls, error) {
	normalized, err := corepaths.NormalizeControls(controls)
	if err != nil {
		return corepaths.LexicalControls{}, controlError{cause: err}
	}
	return normalized, nil
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
	return target == ErrControlPaths
}

func requireMachine(path string) (config.Machine, error) {
	machine, exists, err := config.LoadMachine(path)
	if err != nil {
		return config.Machine{}, err
	}
	if !exists {
		return config.Machine{}, fmt.Errorf(
			"%w: machine config %q is missing",
			ErrMachineUninitialized,
			path,
		)
	}
	return machine, nil
}

func cloneMachine(machine config.Machine) config.Machine {
	return config.Machine{
		Version:      machine.Version,
		Repository:   machine.Repository,
		Profiles:     append([]string(nil), machine.Profiles...),
		ExtraModules: append([]string(nil), machine.ExtraModules...),
	}
}

func joinReleaseFailure(runErr, releaseErr error, mutated bool) error {
	if releaseErr == nil {
		return runErr
	}
	cause := fmt.Errorf("release mutation lock: %w", releaseErr)
	if runErr != nil {
		cause = errors.Join(runErr, cause)
	}
	return newFailure(mutated || FailureMayHaveChanged(runErr), nil, cause)
}
