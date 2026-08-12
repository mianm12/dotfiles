package converge

import (
	"fmt"
	"path/filepath"

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

	next := plan.nextSnapshot()
	mutation := mutationRun{home: filepath.Clean(home)}
	if err := mutation.apply(plan); err != nil {
		return executionResult{
			TargetsChanged: mutation.changed,
		}, mutation.wrapError(err)
	}

	stateChanged := loaded.Missing || !state.Equal(loaded.Snapshot, next)
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
	for _, issue := range plan.Issues {
		if issue.Severity != IssueBlocker {
			continue
		}
		return fmt.Errorf(
			"plan blocker for %s/%s: %s",
			issue.ModuleID,
			issue.PlacementID,
			issue.Reason,
		)
	}
	return fmt.Errorf("convergence plan is not executable")
}

func cloneSnapshot(snapshot state.Snapshot) state.Snapshot {
	cloned := state.Snapshot{
		Home:  snapshot.Home,
		Links: make(map[state.Key]state.LinkRecord, len(snapshot.Links)),
	}
	for key, link := range snapshot.Links {
		cloned.Links[key] = link
	}
	return cloned
}
