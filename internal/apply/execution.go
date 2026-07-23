package apply

import (
	"errors"
	"fmt"

	"github.com/mianm12/dotfiles/internal/executor"
	"github.com/mianm12/dotfiles/internal/planner"
	"github.com/mianm12/dotfiles/internal/state"
)

func runExecution(
	options Options,
	mutation loadedMutation,
	scope executionScope,
	operations runOperations,
	result *Result,
) error {
	fileOutcomePositions := result.beginExecution(scope.files, scope.prune, scope.hooks)
	updates, filesConverged, fileErr := executeFilePhase(
		mutation,
		scope.files,
		fileOutcomePositions,
		operations,
		result,
	)
	deletes, pruneErr := executePrunePhase(options, mutation, scope, filesConverged, operations, result)
	runOnceUpdates, hookErr := executeHookPhase(options, scope.hooks, operations, result)
	return commitExecutionEffects(
		mutation,
		state.ChangeSet{EntryUpdates: updates, EntryDeletes: deletes, RunOnceUpdates: runOnceUpdates},
		errors.Join(fileErr, pruneErr, hookErr),
		result,
	)
}

func executeHookPhase(
	options Options,
	hooks []planner.HookAction,
	operations runOperations,
	result *Result,
) ([]state.RunOnceUpdate, error) {
	updates := make([]state.RunOnceUpdate, 0, len(hooks))
	for index, action := range hooks {
		outcome := &result.hookOutcomes[index]
		if action.Verb == planner.HookSkip {
			continue
		}
		outcome.attempted = true
		hookResult, executeErr := operations.executeHook(action, executor.HookStreams{
			Stdin: options.Stdin, Stdout: options.Stdout, Stderr: options.Stderr,
		})
		success, failure, protocolErr := validateHookResult(action, hookResult, executeErr)
		if protocolErr != nil {
			executeErr = errors.Join(executeErr, protocolErr)
		}
		switch {
		case protocolErr != nil:
		case success:
			update, updateErr := runOnceUpdate(action.OnSuccess, operations.now())
			if updateErr != nil {
				executeErr = errors.Join(executeErr, updateErr)
			} else {
				updates = append(updates, update)
				outcome.stateEffectReady = true
			}
		case failure:
			if executeErr == nil {
				executeErr = fmt.Errorf(
					"%w: hook action %d for %q returned failure effect without error",
					ErrExecutionProtocol,
					index,
					action.StateKey,
				)
			}
		}
		if executeErr != nil {
			outcome.Status = ActionFailed
			return updates, fmt.Errorf("execute hook action %d for %q: %w", index, action.StateKey, executeErr)
		}
		outcome.Status = ActionSucceeded
	}
	return updates, nil
}

func executeFilePhase(
	mutation loadedMutation,
	files []planner.FileAction,
	outcomePositions map[int]int,
	operations runOperations,
	result *Result,
) ([]state.EntryUpdate, bool, error) {
	updates := make([]state.EntryUpdate, 0, len(files))
	for index, action := range files {
		if action.Verb.ExecutionClass() == planner.FilePlanOnly {
			continue
		}
		outcome := &result.fileOutcomes[outcomePositions[index]]
		outcome.attempted = true
		fileResult, executeErr := operations.execute(mutation.control(), action)
		outcome.targetCommitted = fileResult.TargetMutated

		success, failure, protocolErr := validateFileResult(action, fileResult, executeErr)
		if protocolErr != nil {
			executeErr = errors.Join(executeErr, protocolErr)
		}
		switch {
		case protocolErr != nil:
			// 矛盾结果不能形成 state update；已报告的物理提交留给既定收养路径恢复。
		case success:
			update, updateErr := entryUpdate(action.OnSuccess, operations.now())
			if updateErr != nil {
				executeErr = errors.Join(executeErr, updateErr)
			} else {
				updates = append(updates, update)
				outcome.stateEffectReady = true
			}
		case failure:
			if executeErr == nil {
				executeErr = fmt.Errorf(
					"%w: file action %d for %q returned failure effect without error",
					ErrExecutionProtocol,
					index,
					action.Target,
				)
			}
		}
		if executeErr != nil {
			if protocolErr == nil && executor.IsPurePreconditionMismatch(executeErr) {
				outcome.Status = ActionConflict
				return updates, false, nil
			}
			outcome.Status = ActionFailed
			return updates, false, fmt.Errorf("execute file action %d for %q: %w", index, action.Target, executeErr)
		}
		outcome.Status = ActionSucceeded
	}
	return updates, true, nil
}

func executePrunePhase(
	options Options,
	mutation loadedMutation,
	scope executionScope,
	filesConverged bool,
	operations runOperations,
	result *Result,
) ([]string, error) {
	deletes := make([]string, 0, len(scope.prune))
	if !filesConverged || !hasActivePrune(scope.prune) {
		return deletes, nil
	}
	confirmed, confirmErr := confirmPrunePhase(options.Confirm, scope.groups, result)
	if !confirmed {
		return deletes, confirmErr
	}

	for index, action := range scope.prune {
		if action.Deferred {
			continue
		}
		outcome := &result.pruneOutcomes[index]
		outcome.attempted = true
		pruneResult, pruneErr := operations.pruneExecute(mutation.control(), action)
		outcome.targetCommitted = pruneResult.TargetMutated
		success, failure, protocolErr := validatePruneResult(action, pruneResult, pruneErr)
		if protocolErr != nil {
			pruneErr = errors.Join(pruneErr, protocolErr)
		}
		switch {
		case protocolErr != nil:
		case success:
			deletes = append(deletes, action.OnSuccess.Key)
			outcome.stateEffectReady = true
		case failure:
			if pruneErr == nil {
				pruneErr = fmt.Errorf(
					"%w: prune action %d for %q returned failure effect without error",
					ErrExecutionProtocol,
					index,
					action.Target,
				)
			}
		}
		if pruneErr != nil {
			if protocolErr == nil && executor.IsPurePreconditionMismatch(pruneErr) {
				outcome.Status = ActionConflict
				return deletes, nil
			}
			outcome.Status = ActionFailed
			return deletes, fmt.Errorf("execute prune action %d for %q: %w", index, action.Target, pruneErr)
		}
		outcome.Status = ActionSucceeded
	}
	return deletes, confirmErr
}

func hasActivePrune(actions []planner.PruneAction) bool {
	for _, action := range actions {
		if !action.Deferred {
			return true
		}
	}
	return false
}

func confirmPrunePhase(confirm ConfirmPrune, groups []planner.PruneConfirmationGroup, result *Result) (bool, error) {
	if len(groups) == 0 {
		return true, nil
	}
	result.confirmRequested = true
	if confirm == nil {
		return false, nil
	}
	accepted, err := confirm(cloneConfirmationGroups(groups))
	if err != nil {
		return false, fmt.Errorf("confirm whole-module prune: %w", err)
	}
	result.confirmAccepted = accepted
	return accepted, nil
}

func commitExecutionEffects(
	mutation loadedMutation,
	changes state.ChangeSet,
	executionErr error,
	result *Result,
) error {
	if len(changes.EntryUpdates) == 0 && len(changes.EntryDeletes) == 0 && len(changes.RunOnceUpdates) == 0 {
		return executionErr
	}
	candidate, changed, transitionErr := state.Transition(mutation.baseline(), changes)
	if transitionErr != nil {
		return errors.Join(executionErr, transitionErr)
	}
	if !changed {
		return errors.Join(
			executionErr,
			fmt.Errorf("%w: successful state effects produced no transition", ErrExecutionProtocol),
		)
	}
	if commitErr := mutation.commit(candidate); commitErr != nil {
		return errors.Join(executionErr, fmt.Errorf("commit apply state: %w", commitErr))
	}
	result.stateCommitted = true
	return executionErr
}
