package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/executor"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/lock"
)

type preparedPlan struct {
	machine    config.Machine
	resolution config.Resolution
	scope      []string
	loaded     state.Loaded
	plan       planner.Plan
}

type mutationOwner struct {
	owner *lock.Ownership
}

func (owner mutationOwner) ownership() *lock.Ownership {
	return owner.owner
}

func preparePlan(
	context commandContext,
	machine config.Machine,
	resolution config.Resolution,
	scope []string,
) (preparedPlan, error) {
	loaded, err := state.Load(context.statePath, context.home)
	if err != nil {
		return preparedPlan{}, err
	}
	plan, err := planner.Build(planner.Request{
		Home:     context.home,
		Controls: context.controls(machine.Repository),
		Modules:  resolution.Modules,
		Scope:    scope,
		State:    loaded.Snapshot,
	})
	if err != nil {
		return preparedPlan{}, err
	}
	return preparedPlan{
		machine:    machine,
		resolution: resolution,
		scope:      append([]string(nil), scope...),
		loaded:     loaded,
		plan:       plan,
	}, nil
}

func prepareInit(
	context commandContext,
	machine config.Machine,
) (preparedPlan, error) {
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return preparedPlan{}, err
	}
	resolution, err := repository.Resolve(machine.Scope(), context.platform)
	if err != nil {
		return preparedPlan{}, err
	}
	prepared, err := preparePlan(context, machine, resolution, nil)
	return prepared, err
}

func prepareApply(
	context commandContext,
	machine config.Machine,
	moduleID string,
) (prepared preparedPlan, selectionChanged bool, err error) {
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return preparedPlan{}, false, err
	}
	scope := machine.Scope()
	var plannerScope []string
	if moduleID != "" {
		profileModules, err := repository.ProfileModules(machine.Profiles)
		if err != nil {
			return preparedPlan{}, false, err
		}
		if !slices.Contains(profileModules, moduleID) &&
			!slices.Contains(machine.ExtraModules, moduleID) {
			machine.ExtraModules = append(machine.ExtraModules, moduleID)
			slices.Sort(machine.ExtraModules)
			selectionChanged = true
		}
		scope = machine.Scope(moduleID)
		plannerScope = []string{moduleID}
	}
	resolution, err := repository.Resolve(scope, context.platform)
	if err != nil {
		return preparedPlan{}, false, err
	}
	prepared, err = preparePlan(context, machine, resolution, plannerScope)
	return prepared, selectionChanged, err
}

func prepareRemove(
	context commandContext,
	machine config.Machine,
	moduleID string,
) (prepared preparedPlan, selectionChanged bool, err error) {
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return preparedPlan{}, false, err
	}
	profileModules, err := repository.ProfileModules(machine.Profiles)
	if err != nil {
		return preparedPlan{}, false, err
	}
	if slices.Contains(profileModules, moduleID) {
		return preparedPlan{}, false, fmt.Errorf(
			"module %q is selected by an active profile; remove it from the repository profile first",
			moduleID,
		)
	}

	loaded, err := state.Load(context.statePath, context.home)
	if err != nil {
		return preparedPlan{}, false, err
	}
	_, knownInState := loaded.Snapshot.Modules[moduleID]
	knownAsExtra := slices.Contains(machine.ExtraModules, moduleID)
	_, exists, _, err := repository.InspectModule(moduleID, context.platform)
	if err != nil {
		return preparedPlan{}, false, err
	}
	if !exists && !knownAsExtra && !knownInState {
		return preparedPlan{}, false, fmt.Errorf("unknown module %q", moduleID)
	}

	if knownAsExtra {
		machine.ExtraModules = slices.DeleteFunc(
			append([]string(nil), machine.ExtraModules...),
			func(candidate string) bool { return candidate == moduleID },
		)
		selectionChanged = true
	}
	resolution, err := repository.Resolve(machine.Scope(), context.platform)
	if err != nil {
		return preparedPlan{}, false, err
	}
	plan, err := planner.Build(planner.Request{
		Home:     context.home,
		Controls: context.controls(machine.Repository),
		Modules:  resolution.Modules,
		Scope:    []string{moduleID},
		State:    loaded.Snapshot,
	})
	if err != nil {
		return preparedPlan{}, false, err
	}
	return preparedPlan{
		machine:    machine,
		resolution: resolution,
		scope:      []string{moduleID},
		loaded:     loaded,
		plan:       plan,
	}, selectionChanged, nil
}

func executePrepared(
	context commandContext,
	prepared preparedPlan,
	owner *lock.Ownership,
) (executor.Result, error) {
	return executor.RunWithLock(executor.Request{
		Home:     context.home,
		Controls: context.controls(prepared.machine.Repository),
		Modules:  prepared.resolution.Modules,
		Scope:    prepared.scope,
	}, owner)
}

func rejectConflicts(plan planner.Plan) error {
	if !plan.HasConflicts() {
		return nil
	}
	for _, action := range plan.Actions {
		if action.Decision == planner.DecisionConflict {
			return fmt.Errorf(
				"plan conflict for %s/%s: %s",
				action.ModuleID,
				action.PlacementID,
				action.Reason,
			)
		}
	}
	return fmt.Errorf("plan contains a conflict")
}

func withMutationLock(
	context commandContext,
	run func(mutationOwner) error,
) (err error) {
	owner, err := lock.Acquire(filepath.Dir(context.lockPath), context.lockPath)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, owner.Release())
	}()
	return run(mutationOwner{owner: owner})
}

func loadRequiredMachine(context commandContext) (config.Machine, error) {
	machine, exists, err := config.LoadMachine(context.configPath)
	if err != nil {
		return config.Machine{}, err
	}
	if !exists {
		return config.Machine{}, fmt.Errorf(
			"machine config %q is missing; run dot init",
			context.configPath,
		)
	}
	return machine, nil
}

func publishSelection(
	context commandContext,
	machine config.Machine,
	needed bool,
) (bool, error) {
	if !needed {
		return false, nil
	}
	return config.PublishMachine(context.configPath, machine)
}

func afterSelectionPublished(env environment, changed bool) error {
	if !changed || env.afterSelectionPublish == nil {
		return nil
	}
	return env.afterSelectionPublish()
}

func preparedWarnings(prepared preparedPlan) []string {
	return appendWarning(prepared.loaded.Warning, prepared.plan.Warnings)
}

func appendWarning(warning string, warnings []string) []string {
	result := make([]string, 0, len(warnings)+1)
	if warning != "" {
		result = append(result, warning)
	}
	return append(result, warnings...)
}
