package converge

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/storage"
)

type checkedSelection struct {
	result      SelectionResult
	controls    corepaths.Controls
	fingerprint []byte
}

// Initialize creates the first machine selection without converging targets.
func Initialize(
	environment Environment,
	repository string,
	profiles []string,
) (result SelectionResult, err error) {
	environment, err = normalizeEnvironment(environment)
	if err != nil {
		return SelectionResult{}, err
	}
	repository, err = cleanAbsolute("repository", repository)
	if err != nil {
		return SelectionResult{}, err
	}
	machine := config.Machine{
		Version:      1,
		Repository:   repository,
		Profiles:     append([]string(nil), profiles...),
		ExtraModules: []string{},
	}
	preflight, err := checkInitialize(environment, machine)
	if err != nil {
		return SelectionResult{}, err
	}
	release, err := acquire(environment, preflight.controls)
	if err != nil {
		return SelectionResult{}, err
	}
	defer func() { err = joinReleaseError(err, release()) }()

	locked, err := checkInitialize(environment, machine)
	if err != nil {
		return SelectionResult{}, err
	}
	return publishSelection(environment.ConfigPath, locked.result)
}

// SelectAdd adds one direct module selection without converging targets.
func SelectAdd(environment Environment, moduleID string) (result SelectionResult, err error) {
	environment, err = normalizeEnvironment(environment)
	if err != nil {
		return SelectionResult{}, err
	}
	if err := config.ValidateModuleID(moduleID); err != nil {
		return SelectionResult{}, err
	}
	preflight, err := checkSelectAdd(environment, moduleID)
	if err != nil {
		return SelectionResult{}, err
	}
	release, err := acquire(environment, preflight.controls)
	if err != nil {
		return SelectionResult{}, err
	}
	defer func() { err = joinReleaseError(err, release()) }()

	locked, err := checkSelectAdd(environment, moduleID)
	if err != nil {
		return SelectionResult{}, err
	}
	if err := verifySelectionFingerprint(preflight, locked); err != nil {
		return SelectionResult{}, err
	}
	return publishSelection(environment.ConfigPath, locked.result)
}

// SelectRemove removes one direct module selection without converging targets.
func SelectRemove(environment Environment, moduleID string) (result SelectionResult, err error) {
	environment, err = normalizeEnvironment(environment)
	if err != nil {
		return SelectionResult{}, err
	}
	if err := config.ValidateModuleID(moduleID); err != nil {
		return SelectionResult{}, err
	}
	preflight, err := checkSelectRemove(environment, moduleID)
	if err != nil {
		return SelectionResult{}, err
	}
	release, err := acquire(environment, preflight.controls)
	if err != nil {
		return SelectionResult{}, err
	}
	defer func() { err = joinReleaseError(err, release()) }()

	locked, err := checkSelectRemove(environment, moduleID)
	if err != nil {
		return SelectionResult{}, err
	}
	if err := verifySelectionFingerprint(preflight, locked); err != nil {
		return SelectionResult{}, err
	}
	return publishSelection(environment.ConfigPath, locked.result)
}

func verifySelectionFingerprint(preflight, locked checkedSelection) error {
	if bytes.Equal(preflight.fingerprint, locked.fingerprint) {
		return nil
	}
	return fmt.Errorf(
		"%w: machine config changed while waiting for the mutation lock",
		ErrBlocked,
	)
}

func checkInitialize(
	environment Environment,
	machine config.Machine,
) (checkedSelection, error) {
	if _, err := config.MarshalMachine(machine); err != nil {
		return checkedSelection{}, err
	}
	controls := environmentControls(environment, machine.Repository)
	if err := validateControls(controls); err != nil {
		return checkedSelection{}, err
	}
	_, exists, err := config.LoadMachine(environment.ConfigPath)
	if err != nil {
		return checkedSelection{}, err
	}
	if exists {
		return checkedSelection{}, fmt.Errorf(
			"machine is already initialized at %q",
			environment.ConfigPath,
		)
	}
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return checkedSelection{}, err
	}
	if _, err := repository.ProfileModules(machine.Profiles); err != nil {
		return checkedSelection{}, err
	}
	return checkedSelection{
		result:   SelectionResult{Machine: cloneMachine(machine), Changed: true},
		controls: controls,
	}, nil
}

func checkSelectAdd(environment Environment, moduleID string) (checkedSelection, error) {
	checked, repository, err := checkCurrentSelection(environment, moduleID)
	if err != nil {
		return checkedSelection{}, err
	}
	if checked.result.ProfileSelected ||
		slices.Contains(checked.result.Machine.ExtraModules, moduleID) {
		return checked, nil
	}
	_, exists, applicability, err := repository.InspectModule(moduleID, environment.Platform)
	if err != nil {
		return checkedSelection{}, err
	}
	if !exists {
		return checkedSelection{}, fmt.Errorf("unknown module %q", moduleID)
	}
	switch applicability.State {
	case config.ApplicabilityApplicable:
	case config.ApplicabilityNotApplicable:
		return checkedSelection{}, fmt.Errorf("module %q is not applicable", moduleID)
	case config.ApplicabilityIndeterminate:
		return checkedSelection{}, fmt.Errorf(
			"module %q applicability is indeterminate: %s",
			moduleID,
			applicability.Diagnostic,
		)
	default:
		return checkedSelection{}, fmt.Errorf(
			"module %q returned invalid applicability %q",
			moduleID,
			applicability.State,
		)
	}
	checked.result.Machine.ExtraModules = append(
		checked.result.Machine.ExtraModules,
		moduleID,
	)
	slices.Sort(checked.result.Machine.ExtraModules)
	checked.result.Changed = true
	return checked, nil
}

func checkSelectRemove(environment Environment, moduleID string) (checkedSelection, error) {
	checked, _, err := checkCurrentSelection(environment, moduleID)
	if err != nil {
		return checkedSelection{}, err
	}
	checked.result.Changed = slices.Contains(checked.result.Machine.ExtraModules, moduleID)
	checked.result.Machine.ExtraModules = slices.DeleteFunc(
		checked.result.Machine.ExtraModules,
		func(candidate string) bool { return candidate == moduleID },
	)
	return checked, nil
}

func checkCurrentSelection(
	environment Environment,
	moduleID string,
) (checkedSelection, config.Repository, error) {
	machine, err := requireMachine(environment.ConfigPath)
	if err != nil {
		return checkedSelection{}, config.Repository{}, err
	}
	controls := environmentControls(environment, machine.Repository)
	if err := validateControls(controls); err != nil {
		return checkedSelection{}, config.Repository{}, err
	}
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return checkedSelection{}, config.Repository{}, err
	}
	profileModules, err := repository.ProfileModules(machine.Profiles)
	if err != nil {
		return checkedSelection{}, config.Repository{}, err
	}
	fingerprint, err := machineFingerprint(machine)
	if err != nil {
		return checkedSelection{}, config.Repository{}, err
	}
	return checkedSelection{
		result: SelectionResult{
			Machine:         cloneMachine(machine),
			ProfileSelected: slices.Contains(profileModules, moduleID),
		},
		controls:    controls,
		fingerprint: fingerprint,
	}, repository, nil
}

func publishSelection(path string, result SelectionResult) (SelectionResult, error) {
	if !result.Changed {
		return result, nil
	}
	data, err := config.MarshalMachine(result.Machine)
	if err != nil {
		return SelectionResult{}, err
	}
	changed, err := storage.PublishPrivateFile(path, data)
	result.Changed = changed
	if err != nil {
		if changed {
			err = fmt.Errorf("%w: %w", ErrPartial, err)
		}
		return result, fmt.Errorf("publish machine config %q: %w", path, err)
	}
	return result, nil
}
