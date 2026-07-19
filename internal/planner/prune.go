package planner

import (
	"errors"
	"fmt"
	"path/filepath"
)

// ErrUnsupportedPruneInput 表示输入含 M1 prune planner 不能安全解释的封闭类型或缺失事实。
var ErrUnsupportedPruneInput = errors.New("unsupported prune input")

// PruneMode 描述 active prune 成功时对 target/state 的处置范围。
type PruneMode string

const (
	// PruneStateOnly 只摘除历史 state，不触碰 target。
	PruneStateOnly PruneMode = "state-only"
	// PruneTargetAndState 删除仍 owned 的 target 后摘除历史 state。
	PruneTargetAndState PruneMode = "target-and-state"
)

// PruneReason 是 P1/P2/P3 的稳定分类，不把展示文案当内部协议。
type PruneReason string

const (
	// PruneReasonScaffold 表示 P1 scaffold 永不拥有 target。
	PruneReasonScaffold PruneReason = "scaffold-orphan"
	// PruneReasonOwned 表示 P2 symlink 仍满足共享 Owned 谓词。
	PruneReasonOwned PruneReason = "owned-orphan"
	// PruneReasonUnowned 表示 P3 target 已不再由历史记录拥有。
	PruneReasonUnowned PruneReason = "unowned-orphan"
)

// PruneDeferredReason 描述 active prune 被整体延迟的稳定原因。
type PruneDeferredReason string

const (
	// PruneDeferredNone 表示动作仍是 active prune 候选。
	PruneDeferredNone PruneDeferredReason = ""
	// PruneDeferredFileConflict 表示 file decision 中存在 unresolved conflict。
	PruneDeferredFileConflict PruneDeferredReason = "file-conflict"
)

// PruneAction 是一个 orphan 的自包含纯计划。Mode/Reason 始终保存 P1/P2/P3 基础分类；
// Deferred 只改变可执行性与 state effect，不抹掉重跑收敛所需的基础事实。
type PruneAction struct {
	Mode           PruneMode
	Target         string
	Module         string
	Reason         PruneReason
	Warning        bool
	Deferred       bool
	DeferredReason PruneDeferredReason
	Precondition   Precondition
	OnSuccess      StateEffect
	OnFailure      StateEffect
}

// DeletesTarget 报告本次计划是否可以实际删除 target；deferred P2 返回 false。
func (action PruneAction) DeletesTarget() bool {
	return !action.Deferred && action.Mode == PruneTargetAndState
}

// WouldDeleteTarget 报告基础 P 行在不 deferred 时是否删除 target，供确认组展示。
func (action PruneAction) WouldDeleteTarget() bool {
	return action.Mode == PruneTargetAndState
}

func planOrphanPrune(orphan OrphanTarget, deferred bool) (PruneAction, error) {
	if orphan.TargetPath == "" || !filepath.IsAbs(orphan.TargetPath) {
		return PruneAction{}, fmt.Errorf("%w: orphan target path %q", ErrUnsupportedPruneInput, orphan.TargetPath)
	}
	if orphan.State.Key == "" || orphan.State.Module == "" {
		return PruneAction{}, fmt.Errorf("%w: orphan state key and module are required", ErrUnsupportedPruneInput)
	}
	switch orphan.Observed.Kind {
	case ObjectMissing, ObjectSymlink, ObjectRegular, ObjectDirectory, ObjectSpecial:
		// supported
	default:
		return PruneAction{}, fmt.Errorf(
			"%w: observed object kind %q",
			ErrUnsupportedPruneInput,
			orphan.Observed.Kind,
		)
	}

	action := PruneAction{
		Target: orphan.State.Key,
		Module: orphan.State.Module,
		Precondition: Precondition{
			TargetPath:       orphan.TargetPath,
			TargetResolution: orphan.Resolution,
		},
		OnSuccess: StateEffect{Kind: StateDelete, Key: orphan.State.Key},
		OnFailure: StateEffect{Kind: StatePreserve},
	}
	switch orphan.State.Kind {
	case StateScaffold: // P1
		action.Mode = PruneStateOnly
		action.Reason = PruneReasonScaffold
		action.Precondition.Leaf = LeafCondition{Kind: LeafAny}
	case StateSymlink:
		if Owned(orphan.State, orphan.Observed) { // P2
			action.Mode = PruneTargetAndState
			action.Reason = PruneReasonOwned
			action.Precondition.Leaf = LeafCondition{
				Kind:     LeafExactSymlink,
				LinkDest: orphan.State.LinkDest,
			}
		} else { // P3
			action.Mode = PruneStateOnly
			action.Reason = PruneReasonUnowned
			action.Warning = true
			action.Precondition.Leaf = LeafCondition{
				Kind:     LeafNotOwnedSymlink,
				LinkDest: orphan.State.LinkDest,
			}
		}
	default:
		return PruneAction{}, fmt.Errorf(
			"%w: historical state kind %q",
			ErrUnsupportedPruneInput,
			orphan.State.Kind,
		)
	}
	if deferred {
		action.Deferred = true
		action.DeferredReason = PruneDeferredFileConflict
		action.OnSuccess = StateEffect{Kind: StatePreserve}
	}
	return action, nil
}
