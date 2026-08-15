package converge

import (
	"fmt"

	"github.com/mianm12/dotfiles/internal/core/state"
)

type executionResult struct {
	Done            []Line
	TargetsChanged  bool
	StateChanged    bool
	ControlsChanged bool
}

type stateCommitter func(string, state.Snapshot) (bool, error)

func executeLines(
	statePath string,
	lines []loopLine,
	loaded state.Loaded,
	commit stateCommitter,
) (executionResult, error) {
	for _, line := range lines {
		if line.Op == OpSkip {
			return executionResult{}, nil
		}
	}

	next := cloneSnapshot(loaded.Snapshot)
	mutation := mutationRun{}
	done, stateOnly, err := mutation.apply(lines, &next)
	if err != nil {
		return executionResult{
			Done:            done,
			TargetsChanged:  mutation.targetsChanged,
			ControlsChanged: mutation.controlsChanged,
		}, err
	}

	stateChanged, commitErr := commit(statePath, next)
	if commitErr != nil {
		return executionResult{
				Done:            done,
				TargetsChanged:  mutation.targetsChanged,
				StateChanged:    stateChanged,
				ControlsChanged: mutation.controlsChanged,
			}, newFailure(
				mutation.targetsChanged || mutation.controlsChanged || stateChanged,
				nil,
				fmt.Errorf("commit state: %w", commitErr),
			)
	}
	done = append(done, stateOnly...)
	return executionResult{
		Done:            done,
		TargetsChanged:  mutation.targetsChanged,
		StateChanged:    stateChanged,
		ControlsChanged: mutation.controlsChanged,
	}, nil
}

func loadState(path, home string) (state.Loaded, error) {
	loaded, err := state.Load(path, home)
	if err != nil {
		return state.Loaded{}, fmt.Errorf("load ownership state: %w", err)
	}
	return loaded, nil
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
