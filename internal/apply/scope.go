// Package apply 连接 M1 planner、file executor 与持锁 runtime session。
package apply

import (
	"errors"
	"fmt"

	"github.com/mianm12/dotfiles/internal/executor"
	"github.com/mianm12/dotfiles/internal/planner"
)

// ErrUnsupportedPlan 表示 canonical plan 含有当前 checkpoint 尚未交付的可执行能力。
var ErrUnsupportedPlan = errors.New("apply plan contains unsupported executable action")

func validateExecutionScope(
	files []planner.FileAction,
	prune []planner.PruneAction,
	hooks []planner.HookAction,
) error {
	for index, action := range files {
		if action.Verb.ExecutionClass() == planner.FilePlanOnly {
			continue
		}
		if err := executor.ValidateFileAction(action); err != nil {
			return unsupportedFileAction(index, action, err)
		}
	}
	for index, action := range prune {
		if action.Deferred {
			continue
		}
		if err := executor.ValidatePruneAction(action); err != nil {
			return fmt.Errorf("%w: prune action %d for %q: %w", ErrUnsupportedPlan, index, action.Target, err)
		}
	}
	for index, action := range hooks {
		return fmt.Errorf(
			"%w: hook action %d for %q uses %q before hook execution is available",
			ErrUnsupportedPlan,
			index,
			action.StateKey,
			action.Verb,
		)
	}
	return nil
}

func unsupportedFileAction(index int, action planner.FileAction, cause error) error {
	return fmt.Errorf(
		"%w: file action %d for %q uses %q/%q: %w",
		ErrUnsupportedPlan,
		index,
		action.Target,
		action.Verb,
		action.Reason,
		cause,
	)
}
