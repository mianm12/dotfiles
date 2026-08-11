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

	next := plan.finalSnapshot()
	mutation := mutationRun{home: filepath.Clean(home)}
	if err := mutation.apply(plan); err != nil {
		return executionResult{
			TargetsChanged: mutation.changed,
		}, mutation.wrapError(err)
	}

	stateChanged := loaded.Missing || !reflect.DeepEqual(loaded.Snapshot, next)
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
	if len(plan.Problems) != 0 {
		problem := plan.Problems[0]
		return fmt.Errorf(
			"plan %s for %s/%s: %s",
			problem.Kind,
			problem.ModuleID,
			problem.PlacementID,
			problem.Reason,
		)
	}
	return fmt.Errorf("convergence plan is not executable")
}

func cloneSnapshot(snapshot state.Snapshot) state.Snapshot {
	cloned := state.Snapshot{
		Home:    snapshot.Home,
		Records: make(map[state.Key]state.Record, len(snapshot.Records)),
	}
	for key, record := range snapshot.Records {
		cloned.Records[key] = record
	}
	return cloned
}
