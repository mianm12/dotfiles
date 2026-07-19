package planner

import (
	"errors"
	"fmt"
	"path"
)

// ErrUnsupportedDecisionInput 表示输入含 M1 planner 不支持或不能安全解释的封闭类型。
var ErrUnsupportedDecisionInput = errors.New("unsupported decision input")

// DecisionOptions 保存会改变纯决策结果的显式用户授权；它不提供执行能力。
type DecisionOptions struct {
	Force bool
}

// Owned 是 M1 planner 与后续 prune 共享的唯一所有权谓词。symlink 只比较 raw link text；
// scaffold 记录只表示一次性生命周期，永不拥有 target。
func Owned(historical HistoricalState, observed Observation) bool {
	switch historical.Kind {
	case StateSymlink:
		return observed.Kind == ObjectSymlink && observed.LinkDest == historical.LinkDest
	case StateScaffold:
		return false
	default:
		return false
	}
}

// Decide 按 desired kind 对一个已完成 identity join 的 target 纯函数决策。任一不支持输入都
// fail closed，返回零值 Action；本函数不读取文件系统、不修改 target 或 state。
func Decide(target ObservedTarget, options DecisionOptions) (Action, error) {
	if err := validateDecisionInput(target); err != nil {
		return Action{}, err
	}

	switch target.Desired.Kind {
	case DesiredLink:
		if target.HasState && target.State.Kind == StateScaffold {
			return Action{}, fmt.Errorf(
				"%w: migration from %q to %q is not implemented",
				ErrUnsupportedDecisionInput,
				target.State.Kind,
				target.Desired.Kind,
			)
		}
		return decideLink(target, options, target.HasState), nil
	case DesiredScaffold:
		if target.HasState && target.State.Kind == StateSymlink {
			return Action{}, fmt.Errorf(
				"%w: migration from %q to %q is not implemented",
				ErrUnsupportedDecisionInput,
				target.State.Kind,
				target.Desired.Kind,
			)
		}
		return decideScaffold(target, options), nil
	default:
		return Action{}, fmt.Errorf(
			"%w: desired kind %q",
			ErrUnsupportedDecisionInput,
			target.Desired.Kind,
		)
	}
}

func validateDecisionInput(target ObservedTarget) error {
	switch target.Desired.Kind {
	case DesiredLink, DesiredScaffold:
		// supported
	default:
		return fmt.Errorf("%w: desired kind %q", ErrUnsupportedDecisionInput, target.Desired.Kind)
	}
	switch target.Observed.Kind {
	case ObjectMissing, ObjectSymlink, ObjectRegular, ObjectDirectory, ObjectSpecial:
		// supported
	default:
		return fmt.Errorf("%w: observed object kind %q", ErrUnsupportedDecisionInput, target.Observed.Kind)
	}
	if !target.HasState {
		return nil
	}
	switch target.State.Kind {
	case StateSymlink, StateScaffold:
		return nil
	default:
		return fmt.Errorf("%w: historical state kind %q", ErrUnsupportedDecisionInput, target.State.Kind)
	}
}

func decideLink(target ObservedTarget, options DecisionOptions, useStateEvidence bool) Action {
	observed := target.Observed
	switch observed.Kind {
	case ObjectMissing: // L1
		return plannedAction(target, ActionCreateLink, ReasonTargetMissing, StateUpsert)
	case ObjectSymlink:
		if observed.LinkDest == target.Desired.SourcePath { // L2
			if useStateEvidence && linkMetadataCurrent(target.Desired, target.State) {
				return plannedAction(target, ActionSkip, ReasonExpectedLink, StatePreserve)
			}
			return plannedAction(target, ActionAdopt, ReasonStateMetadata, StateUpsert)
		}
		if useStateEvidence {
			if Owned(target.State, observed) { // L3
				return plannedAction(target, ActionCreateLink, ReasonOwnedLinkStale, StateUpsert)
			}
			return linkConflict(target, options, ReasonLinkDrift) // L4
		}
		return linkConflict(target, options, ReasonUnownedLink) // L5
	case ObjectRegular: // L6
		if options.Force {
			return plannedAction(target, ActionBackupReplace, ReasonRegularConflict, StateUpsert)
		}
		return plannedAction(target, ActionConflict, ReasonRegularConflict, StatePreserve)
	case ObjectDirectory: // L6
		return plannedAction(target, ActionConflict, ReasonDirectoryConflict, StatePreserve)
	case ObjectSpecial: // L6
		return plannedAction(target, ActionConflict, ReasonSpecialConflict, StatePreserve)
	default:
		panic("validated observation kind became unsupported")
	}
}

func linkConflict(target ObservedTarget, options DecisionOptions, reason ActionReason) Action {
	if options.Force {
		return plannedAction(target, ActionBackupReplace, reason, StateUpsert)
	}
	return plannedAction(target, ActionConflict, reason, StatePreserve)
}

func decideScaffold(target ObservedTarget, options DecisionOptions) Action {
	if target.Observed.Kind != ObjectMissing {
		if !target.HasState { // S1b
			return plannedAction(target, ActionAdopt, ReasonScaffoldPresent, StateUpsert)
		}
		if scaffoldMetadataCurrent(target.Desired, target.State) { // S1a
			return plannedAction(target, ActionSkip, ReasonScaffoldPresent, StatePreserve)
		}
		return plannedAction(target, ActionAdopt, ReasonStateMetadata, StateUpsert)
	}

	if !target.HasState { // S3
		return plannedAction(target, ActionScaffold, ReasonTargetMissing, StateUpsert)
	}
	if options.Force { // S2
		return plannedAction(target, ActionScaffold, ReasonScaffoldRebuild, StateUpsert)
	}
	if !scaffoldMetadataCurrent(target.Desired, target.State) {
		return plannedAction(target, ActionAdopt, ReasonStateMetadata, StateUpsert)
	}
	return plannedAction(target, ActionSkip, ReasonScaffoldDeleted, StatePreserve)
}

func plannedAction(
	target ObservedTarget,
	verb ActionVerb,
	reason ActionReason,
	success StateEffectKind,
) Action {
	precondition := Precondition{
		TargetPath: target.Desired.TargetPath,
		Observed:   target.Observed.Clone(),
	}
	if verb == ActionCreateLink || verb == ActionBackupReplace {
		precondition.SourcePath = target.Desired.SourcePath
		precondition.RequireRegularSource = true
	}
	action := Action{
		Verb:         verb,
		Target:       target.Desired.Target,
		Reason:       reason,
		Desired:      target.Desired.Clone(),
		HasDesired:   true,
		Precondition: precondition,
		OnSuccess:    StateEffect{Kind: success},
		OnFailure:    StateEffect{Kind: StatePreserve},
	}
	if success == StateUpsert {
		action.OnSuccess = upsertDesiredState(target)
	}
	return action
}

func upsertDesiredState(target ObservedTarget) StateEffect {
	previousKey := ""
	if target.HasState && target.State.Key != target.Desired.Target {
		previousKey = target.State.Key
	}
	return StateEffect{
		Kind:        StateUpsert,
		Key:         target.Desired.Target,
		PreviousKey: previousKey,
		Entry:       desiredHistoricalState(target.Desired),
	}
}

func desiredHistoricalState(desired Desired) HistoricalState {
	historical := HistoricalState{
		Key:    desired.Target,
		Module: desired.Module,
		Source: path.Join("modules", desired.Module, desired.Source),
	}
	switch desired.Kind {
	case DesiredLink:
		historical.Kind = StateSymlink
		historical.LinkDest = desired.SourcePath
	case DesiredScaffold:
		historical.Kind = StateScaffold
	}
	return historical
}

func linkMetadataCurrent(desired Desired, historical HistoricalState) bool {
	want := desiredHistoricalState(desired)
	return historical.Key == want.Key &&
		historical.Module == want.Module &&
		historical.Kind == want.Kind &&
		historical.Source == want.Source &&
		historical.LinkDest == want.LinkDest
}

func scaffoldMetadataCurrent(desired Desired, historical HistoricalState) bool {
	want := desiredHistoricalState(desired)
	return historical.Key == want.Key &&
		historical.Module == want.Module &&
		historical.Kind == want.Kind &&
		historical.Source == want.Source
}
