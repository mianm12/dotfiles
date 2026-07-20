package executor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/planner"
)

// ErrUnsupportedPruneAction 表示动作不属于 canonical P1/P2/P3 可执行集合。
var ErrUnsupportedPruneAction = errors.New("unsupported prune action")

// PruneResult 保存单个 prune 动作已选择的 state effect 与 target 提交事实。
type PruneResult struct {
	StateEffect   planner.StateEffect
	TargetMutated bool
}

// ExecutePrune 执行 canonical active P1/P2/P3 动作，并在 target/state effect 提交前复核 Precondition。
func ExecutePrune(control paths.ControlPlanePaths, action planner.PruneAction) (PruneResult, error) {
	failure := PruneResult{StateEffect: action.OnFailure}
	if err := ValidatePruneAction(action); err != nil {
		return failure, err
	}
	if err := validateTargetPrecondition(control, "prune action "+action.Target, action.Precondition); err != nil {
		return failure, err
	}
	if action.Mode == planner.PruneStateOnly {
		return PruneResult{StateEffect: action.OnSuccess}, nil
	}
	if err := os.Remove(action.Precondition.TargetPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return failure, fmt.Errorf("%w: prune target disappeared before delete: %w", ErrPrecondition, err)
		}
		return failure, fmt.Errorf("delete owned prune target: %w", err)
	}
	return PruneResult{StateEffect: action.OnSuccess, TargetMutated: true}, nil
}

// ValidatePruneAction 以纯值方式拒绝 deferred、畸形或非 canonical P1/P2/P3 动作。
func ValidatePruneAction(action planner.PruneAction) error {
	if action.Target == "" || action.Module == "" || action.Precondition.TargetPath == "" ||
		!filepath.IsAbs(action.Precondition.TargetPath) || !action.Precondition.Leaf.Valid() ||
		action.Precondition.SourcePath != "" || action.Precondition.RequireRegularSource ||
		action.OnFailure != (planner.StateEffect{Kind: planner.StatePreserve}) ||
		action.OnSuccess != (planner.StateEffect{Kind: planner.StateDelete, Key: action.Target}) {
		return fmt.Errorf("%w: inconsistent identity, Precondition, or state effect", ErrUnsupportedPruneAction)
	}
	if action.Deferred || action.DeferredReason != planner.PruneDeferredNone {
		return fmt.Errorf("%w: deferred action is not executable", ErrUnsupportedPruneAction)
	}
	switch action.Reason {
	case planner.PruneReasonScaffold:
		if action.Mode != planner.PruneStateOnly || action.Warning || action.Precondition.Leaf.Kind != planner.LeafAny {
			return fmt.Errorf("%w: inconsistent P1 scaffold action", ErrUnsupportedPruneAction)
		}
	case planner.PruneReasonOwned:
		if action.Mode != planner.PruneTargetAndState || action.Warning ||
			action.Precondition.Leaf.Kind != planner.LeafExactSymlink {
			return fmt.Errorf("%w: inconsistent P2 owned action", ErrUnsupportedPruneAction)
		}
	case planner.PruneReasonUnowned:
		if action.Mode != planner.PruneStateOnly || !action.Warning ||
			action.Precondition.Leaf.Kind != planner.LeafNotOwnedSymlink {
			return fmt.Errorf("%w: inconsistent P3 unowned action", ErrUnsupportedPruneAction)
		}
	default:
		return fmt.Errorf("%w: unknown prune reason %q", ErrUnsupportedPruneAction, action.Reason)
	}
	return nil
}
