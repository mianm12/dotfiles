// Package apply 连接 M1 planner、file executor 与持锁 runtime session。
package apply

import (
	"errors"
	"fmt"

	"github.com/mianm12/dotfiles/internal/executor"
	"github.com/mianm12/dotfiles/internal/planner"
)

// ErrUnsupportedPlan 表示 canonical plan 含有当前 CP4 尚未交付的可执行能力。
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
		if !action.Deferred {
			return fmt.Errorf(
				"%w: active prune action %d for %q",
				ErrUnsupportedPlan,
				index,
				action.Target,
			)
		}
	}
	for index, action := range hooks {
		switch action.Verb {
		case planner.HookSkip:
			// 非执行事实，保留既有 run_once。
		case planner.HookRun:
			return fmt.Errorf(
				"%w: hook action %d for %q requires execution",
				ErrUnsupportedPlan,
				index,
				action.StateKey,
			)
		default:
			return fmt.Errorf(
				"%w: hook action %d uses verb %q",
				ErrUnsupportedPlan,
				index,
				action.Verb,
			)
		}
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
