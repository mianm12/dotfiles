package cli

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/executor"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/lock"
)

type mutationOwner struct {
	owner *lock.Ownership
}

func (owner mutationOwner) ownership() *lock.Ownership {
	return owner.owner
}

func validateOperationControls(context commandContext, machine config.Machine) error {
	return executor.ValidateMutationControls(context.controls(machine.Repository))
}

func executeResolved(
	context commandContext,
	repository string,
	resolution config.Resolution,
	scope []string,
	owner *lock.Ownership,
) (executor.Result, error) {
	return executor.RunWithLock(executor.Request{
		Home:     context.home,
		Controls: context.controls(repository),
		Modules:  resolution.Modules,
		Scope:    scope,
	}, owner)
}

func rejectAnalysis(analysis OperationAnalysis) error {
	if len(analysis.Blockers) != 0 {
		blocker := analysis.Blockers[0]
		if blocker.ModuleID != "" {
			return fmt.Errorf(
				"operation blocked for module %q: %s",
				blocker.ModuleID,
				blocker.Reason,
			)
		}
		return fmt.Errorf("operation blocked: %s", blocker.Reason)
	}
	for _, action := range analysis.Actions {
		if action.Decision == planner.DecisionConflict {
			return fmt.Errorf(
				"plan conflict for %s/%s: %s",
				action.ModuleID,
				action.PlacementID,
				action.Reason,
			)
		}
	}
	return nil
}

func withMutationLock(
	controls corepaths.Controls,
	run func(mutationOwner) error,
) (err error) {
	if err := executor.ValidateMutationControls(controls); err != nil {
		return err
	}
	owner, err := lock.Acquire(filepath.Dir(controls.Lock), controls.Lock)
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

func appendWarning(warning string, warnings []string) []string {
	result := make([]string, 0, len(warnings)+1)
	if warning != "" {
		result = append(result, warning)
	}
	return append(result, warnings...)
}
