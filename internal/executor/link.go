// Package executor 执行 planner 已决定且通过校验的文件动作。
package executor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/planner"
)

var (
	// ErrUnsupportedFileAction 表示动作不属于当前 executor 已交付的封闭行为切片。
	ErrUnsupportedFileAction = errors.New("unsupported file action")
	// ErrPrecondition 表示计划时的 target、source、祖先或控制面事实已不成立。
	ErrPrecondition = errors.New("file action precondition failed")
)

// FileResult 保存单个动作供 runtime 消费的结果。StateEffect 已按成功或失败分支选择；
// TargetMutated 只在 target 提交点已经越过时为 true。
type FileResult struct {
	StateEffect   planner.StateEffect
	TargetMutated bool
}

// ExecuteFile 执行当前 M1 link 切片支持的动作。调用方负责只传入可信 ApplyPlan 中的动作，
// 本函数仍会拒绝不安全的动作形态，并在每个 target 提交点前重新复核 Precondition。
func ExecuteFile(control paths.ControlPlanePaths, action planner.FileAction) (FileResult, error) {
	failure := FileResult{StateEffect: action.OnFailure}
	if err := validateLinkAction(action); err != nil {
		return failure, err
	}

	switch action.Verb {
	case planner.FileAdopt:
		if err := validatePrecondition(control, action); err != nil {
			return failure, err
		}
		return FileResult{StateEffect: action.OnSuccess}, nil
	case planner.FileCreateLink:
		if action.Reason != planner.FileReasonTargetMissing {
			return failure, fmt.Errorf(
				"%w: create-link reason %q is not implemented",
				ErrUnsupportedFileAction,
				action.Reason,
			)
		}
		return createMissingLink(control, action)
	default:
		return failure, fmt.Errorf(
			"%w: verb %q is not implemented",
			ErrUnsupportedFileAction,
			action.Verb,
		)
	}
}

func createMissingLink(control paths.ControlPlanePaths, action planner.FileAction) (FileResult, error) {
	failure := FileResult{StateEffect: action.OnFailure}
	if err := validatePrecondition(control, action); err != nil {
		return failure, err
	}
	if err := os.MkdirAll(filepath.Dir(action.Precondition.TargetPath), 0o755); err != nil {
		return failure, fmt.Errorf("create target ancestors: %w", err)
	}
	// mkdir 改变了路径拓扑；提交 target 前必须基于新快照完整复核，而不是沿用首次结论。
	if err := validatePrecondition(control, action); err != nil {
		return failure, err
	}
	if err := os.Symlink(action.Desired.SourcePath, action.Precondition.TargetPath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return failure, fmt.Errorf("%w: target appeared before link create: %w", ErrPrecondition, err)
		}
		return failure, fmt.Errorf("create target symlink: %w", err)
	}
	return FileResult{StateEffect: action.OnSuccess, TargetMutated: true}, nil
}

func validateLinkAction(action planner.FileAction) error {
	if action.Desired.Kind != planner.DesiredLink ||
		action.Target == "" || action.Target != action.Desired.Target ||
		action.Precondition.TargetPath == "" ||
		action.Precondition.TargetPath != action.Desired.TargetPath ||
		action.OnFailure.Kind != planner.StatePreserve {
		return fmt.Errorf("%w: inconsistent link action identity or failure effect", ErrUnsupportedFileAction)
	}
	if action.OnSuccess.Kind != planner.StateUpsert ||
		action.OnSuccess.Key != action.Desired.Target ||
		action.OnSuccess.Entry.Kind != planner.StateSymlink ||
		action.OnSuccess.Entry.LinkDest != action.Desired.SourcePath {
		return fmt.Errorf("%w: inconsistent link state upsert", ErrUnsupportedFileAction)
	}

	switch action.Verb {
	case planner.FileAdopt:
		if action.Reason != planner.FileReasonStateMetadata ||
			action.Precondition.RequireRegularSource ||
			action.Precondition.SourcePath != "" ||
			action.Precondition.Observed.Kind != planner.ObjectSymlink ||
			action.Precondition.Observed.LinkDest != action.Desired.SourcePath {
			return fmt.Errorf("%w: inconsistent link adopt", ErrUnsupportedFileAction)
		}
	case planner.FileCreateLink:
		if !action.Precondition.RequireRegularSource ||
			action.Precondition.SourcePath != action.Desired.SourcePath ||
			!filepath.IsAbs(action.Precondition.SourcePath) {
			return fmt.Errorf("%w: create-link lacks its regular source requirement", ErrUnsupportedFileAction)
		}
		if action.Reason == planner.FileReasonTargetMissing &&
			action.Precondition.Observed.Kind != planner.ObjectMissing {
			return fmt.Errorf("%w: L1 create-link target was not planned missing", ErrUnsupportedFileAction)
		}
	default:
		return fmt.Errorf("%w: verb %q is not a link execution action", ErrUnsupportedFileAction, action.Verb)
	}
	return nil
}

func validatePrecondition(control paths.ControlPlanePaths, action planner.FileAction) error {
	target := paths.LabeledTarget{Label: "file action " + action.Target, Path: action.Precondition.TargetPath}
	if _, err := paths.ValidatePathBoundaries(control, []paths.LabeledTarget{target}); err != nil {
		return fmt.Errorf("%w: validate target/control boundary: %w", ErrPrecondition, err)
	}
	resolution, err := paths.ResolveTarget(action.Precondition.TargetPath)
	if err != nil {
		return fmt.Errorf("%w: resolve target: %w", ErrPrecondition, err)
	}
	if !resolution.Equal(action.Precondition.TargetResolution) {
		return fmt.Errorf("%w: target identity changed", ErrPrecondition)
	}
	observed, err := planner.ObserveTarget(action.Precondition.TargetPath)
	if err != nil {
		return fmt.Errorf("%w: observe target: %w", ErrPrecondition, err)
	}
	if !sameObservation(observed, action.Precondition.Observed) {
		return fmt.Errorf("%w: target observation changed", ErrPrecondition)
	}
	if action.Precondition.RequireRegularSource {
		info, err := os.Lstat(action.Precondition.SourcePath)
		if err != nil {
			return fmt.Errorf("%w: inspect link source: %w", ErrPrecondition, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: link source is not a regular file", ErrPrecondition)
		}
	}
	return nil
}

func sameObservation(left, right planner.Observation) bool {
	return left.Kind == right.Kind &&
		left.Mode == right.Mode &&
		left.LinkDest == right.LinkDest &&
		left.Hash == right.Hash &&
		bytes.Equal(left.Content, right.Content)
}
