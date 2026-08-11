package converge

import (
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/mianm12/dotfiles/internal/core/state"
)

type executionResult struct {
	TargetsChanged bool
	StateChanged   bool
}

type stateCommitter func(string, state.Snapshot) error

func executePlan(
	home string,
	statePath string,
	plan Plan,
	loaded state.Loaded,
	commit stateCommitter,
) (executionResult, error) {
	if !plan.Executable() {
		return executionResult{}, conflictError(plan)
	}

	next := cloneSnapshot(loaded.Snapshot)
	mutation := mutationRun{home: filepath.Clean(home)}
	if err := mutation.apply(plan, &next); err != nil {
		return executionResult{
			TargetsChanged: mutation.changed,
		}, mutation.wrapError(err)
	}

	stateChanged := loaded.Missing ||
		loaded.NeedsRewrite ||
		!reflect.DeepEqual(loaded.Snapshot, next)
	if stateChanged {
		if err := commit(statePath, next); err != nil {
			commitErr := fmt.Errorf(
				"commit state: %w; state was not committed",
				err,
			)
			return executionResult{
				TargetsChanged: mutation.changed,
			}, fmt.Errorf("%w: %w", ErrPartial, commitErr)
		}
	}
	return executionResult{
		TargetsChanged: mutation.changed,
		StateChanged:   stateChanged,
	}, nil
}

func loadState(path, home string) (state.Loaded, error) {
	loaded, err := state.Load(path, home)
	if err != nil {
		return state.Loaded{}, fmt.Errorf("%w: %w", ErrState, err)
	}
	return loaded, nil
}

func conflictError(plan Plan) error {
	if len(plan.Issues) != 0 {
		issue := plan.Issues[0]
		return fmt.Errorf(
			"plan %s for %s/%s: %s",
			issue.Kind,
			issue.ModuleID,
			issue.PlacementID,
			issue.Reason,
		)
	}
	return fmt.Errorf("convergence plan is incomplete")
}

func cloneSnapshot(snapshot state.Snapshot) state.Snapshot {
	cloned := state.Snapshot{
		Home:    snapshot.Home,
		Modules: make(map[string]state.Module, len(snapshot.Modules)),
	}
	for moduleID, module := range snapshot.Modules {
		placements := make(map[string]state.Placement, len(module.Placements))
		for placementID, placement := range module.Placements {
			placements[placementID] = placement
		}
		cloned.Modules[moduleID] = state.Module{Placements: placements}
	}
	return cloned
}
