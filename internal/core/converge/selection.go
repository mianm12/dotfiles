package converge

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/storage"
)

// Initialize creates the first machine selection without converging targets.
func Initialize(
	environment Environment,
	repository string,
	profiles []string,
) (SelectionResult, error) {
	environment, err := normalizeEnvironment(environment)
	if err != nil {
		return SelectionResult{}, newFailure(false, nil, err)
	}
	repository, err = cleanAbsolute("repository", repository)
	if err != nil {
		return SelectionResult{}, newFailure(false, nil, err)
	}
	machine := config.Machine{
		Version:      1,
		Repository:   repository,
		Profiles:     append([]string(nil), profiles...),
		ExtraModules: []string{},
	}
	if _, err := config.MarshalMachine(machine); err != nil {
		return SelectionResult{}, newFailure(false, nil, err)
	}
	return runSelectionMutation(environment, func() (SelectionResult, error) {
		return checkInitialize(environment, machine)
	})
}

// SelectAdd adds one direct module selection without converging targets.
func SelectAdd(environment Environment, moduleID string) (SelectionResult, error) {
	environment, err := normalizeEnvironment(environment)
	if err != nil {
		return SelectionResult{}, newFailure(false, nil, err)
	}
	if err := config.ValidateModuleID(moduleID); err != nil {
		return SelectionResult{}, newFailure(false, nil, err)
	}
	return runSelectionMutation(environment, func() (SelectionResult, error) {
		return checkSelectAdd(environment, moduleID)
	})
}

// SelectRemove removes one direct module selection without converging targets.
func SelectRemove(environment Environment, moduleID string) (SelectionResult, error) {
	environment, err := normalizeEnvironment(environment)
	if err != nil {
		return SelectionResult{}, newFailure(false, nil, err)
	}
	if err := config.ValidateModuleID(moduleID); err != nil {
		return SelectionResult{}, newFailure(false, nil, err)
	}
	return runSelectionMutation(environment, func() (SelectionResult, error) {
		return checkSelectRemove(environment, moduleID)
	})
}

func runSelectionMutation(
	environment Environment,
	check func() (SelectionResult, error),
) (result SelectionResult, err error) {
	if err := validateLockBoundary(environment); err != nil {
		return SelectionResult{}, newFailure(false, nil, err)
	}
	release, err := acquireLock(filepath.Dir(environment.LockPath), environment.LockPath)
	if err != nil {
		return SelectionResult{}, newFailure(false, nil, err)
	}
	return runSelectionMutationLocked(environment, release, check)
}

func runSelectionMutationLocked(
	environment Environment,
	release func() error,
	check func() (SelectionResult, error),
) (result SelectionResult, err error) {
	defer func() { err = joinReleaseFailure(err, release(), result.Changed) }()

	locked, err := check()
	if err != nil {
		return SelectionResult{}, newFailure(false, nil, err)
	}
	return publishSelection(environment.ConfigPath, locked)
}

func checkInitialize(
	environment Environment,
	machine config.Machine,
) (SelectionResult, error) {
	controls := environmentControls(environment, machine.Repository)
	_, err := validateControls(controls)
	if err != nil {
		return SelectionResult{}, err
	}
	_, exists, err := config.LoadMachine(environment.ConfigPath)
	if err != nil {
		return SelectionResult{}, err
	}
	if exists {
		return SelectionResult{}, fmt.Errorf(
			"machine is already initialized at %q",
			environment.ConfigPath,
		)
	}
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return SelectionResult{}, err
	}
	if _, err := repository.ProfileModules(machine.Profiles); err != nil {
		return SelectionResult{}, err
	}
	return SelectionResult{Machine: cloneMachine(machine), Changed: true}, nil
}

func checkSelectAdd(environment Environment, moduleID string) (SelectionResult, error) {
	checked, repository, err := checkCurrentSelection(environment, moduleID)
	if err != nil {
		return SelectionResult{}, err
	}
	if checked.ProfileSelected ||
		slices.Contains(checked.Machine.ExtraModules, moduleID) {
		return checked, nil
	}
	platform, err := resolvePlatform(environment)
	if err != nil {
		return SelectionResult{}, err
	}
	_, exists, applicability, err := repository.InspectModule(moduleID, platform)
	if err != nil {
		return SelectionResult{}, err
	}
	if !exists {
		return SelectionResult{}, fmt.Errorf("unknown module %q", moduleID)
	}
	switch applicability.State {
	case config.ApplicabilityApplicable:
	case config.ApplicabilityNotApplicable:
		return SelectionResult{}, fmt.Errorf("module %q is not applicable", moduleID)
	case config.ApplicabilityIndeterminate:
		return SelectionResult{}, fmt.Errorf(
			"module %q applicability is indeterminate: %s",
			moduleID,
			applicability.Diagnostic,
		)
	default:
		return SelectionResult{}, fmt.Errorf(
			"module %q returned invalid applicability %q",
			moduleID,
			applicability.State,
		)
	}
	checked.Machine.ExtraModules = append(
		checked.Machine.ExtraModules,
		moduleID,
	)
	slices.Sort(checked.Machine.ExtraModules)
	checked.Changed = true
	return checked, nil
}

func checkSelectRemove(environment Environment, moduleID string) (SelectionResult, error) {
	checked, _, err := checkCurrentSelection(environment, moduleID)
	if err != nil {
		return SelectionResult{}, err
	}
	checked.Changed = slices.Contains(checked.Machine.ExtraModules, moduleID)
	checked.Machine.ExtraModules = slices.DeleteFunc(
		checked.Machine.ExtraModules,
		func(candidate string) bool { return candidate == moduleID },
	)
	return checked, nil
}

func checkCurrentSelection(
	environment Environment,
	moduleID string,
) (SelectionResult, config.Repository, error) {
	machine, err := requireMachine(environment.ConfigPath)
	if err != nil {
		return SelectionResult{}, config.Repository{}, err
	}
	controls := environmentControls(environment, machine.Repository)
	_, err = validateControls(controls)
	if err != nil {
		return SelectionResult{}, config.Repository{}, err
	}
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return SelectionResult{}, config.Repository{}, err
	}
	profileModules, err := repository.ProfileModules(machine.Profiles)
	if err != nil {
		return SelectionResult{}, config.Repository{}, err
	}
	return SelectionResult{
		Machine:         cloneMachine(machine),
		ProfileSelected: slices.Contains(profileModules, moduleID),
	}, repository, nil
}

func publishSelection(path string, result SelectionResult) (SelectionResult, error) {
	if !result.Changed {
		return result, nil
	}
	data, err := config.MarshalMachine(result.Machine)
	if err != nil {
		return SelectionResult{}, newFailure(false, nil, err)
	}
	changed, err := storage.PublishPrivateFile(path, data)
	result.Changed = changed
	if err != nil {
		return result, newFailure(
			changed,
			nil,
			fmt.Errorf("publish machine config %q: %w", path, err),
		)
	}
	return result, nil
}
