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

const (
	temporaryDirectoryPrefix = ".dot-link-"
	temporaryLinkName        = "link"
)

type fileOperations struct {
	mkdirAll  func(string, fs.FileMode) error
	mkdirTemp func(string, string) (string, error)
	symlink   func(string, string) error
	rename    func(string, string) error
	remove    func(string) error
}

// FileResult 保存单个动作供 runtime 消费的结果。StateEffect 已按成功或失败分支选择；
// TargetMutated 只在 target 提交点已经越过时为 true。
type FileResult struct {
	StateEffect   planner.StateEffect
	TargetMutated bool
}

// ExecuteFile 执行当前 M1 link 切片支持的动作。调用方负责只传入可信 ApplyPlan 中的动作，
// 本函数仍会拒绝不安全的动作形态，并在每个 target 提交点前重新复核 Precondition。
func ExecuteFile(control paths.ControlPlanePaths, action planner.FileAction) (FileResult, error) {
	return executeFile(control, action, defaultFileOperations())
}

func executeFile(
	control paths.ControlPlanePaths,
	action planner.FileAction,
	operations fileOperations,
) (FileResult, error) {
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
		switch action.Reason {
		case planner.FileReasonTargetMissing:
			return createMissingLink(control, action, operations)
		case planner.FileReasonOwnedLinkStale:
			return relinkOwned(control, action, operations)
		default:
			return failure, fmt.Errorf(
				"%w: create-link reason %q is not implemented",
				ErrUnsupportedFileAction,
				action.Reason,
			)
		}
	default:
		return failure, fmt.Errorf(
			"%w: verb %q is not implemented",
			ErrUnsupportedFileAction,
			action.Verb,
		)
	}
}

func defaultFileOperations() fileOperations {
	return fileOperations{
		mkdirAll:  os.MkdirAll,
		mkdirTemp: os.MkdirTemp,
		symlink:   os.Symlink,
		rename:    os.Rename,
		remove:    os.Remove,
	}
}

func createMissingLink(
	control paths.ControlPlanePaths,
	action planner.FileAction,
	operations fileOperations,
) (FileResult, error) {
	failure := FileResult{StateEffect: action.OnFailure}
	if err := validatePrecondition(control, action); err != nil {
		return failure, err
	}
	if err := operations.mkdirAll(filepath.Dir(action.Precondition.TargetPath), 0o755); err != nil {
		return failure, fmt.Errorf("create target ancestors: %w", err)
	}
	// mkdir 改变了路径拓扑；提交 target 前必须基于新快照完整复核，而不是沿用首次结论。
	if err := validatePrecondition(control, action); err != nil {
		return failure, err
	}
	if err := operations.symlink(action.Desired.SourcePath, action.Precondition.TargetPath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return failure, fmt.Errorf("%w: target appeared before link create: %w", ErrPrecondition, err)
		}
		return failure, fmt.Errorf("create target symlink: %w", err)
	}
	return FileResult{StateEffect: action.OnSuccess, TargetMutated: true}, nil
}

func relinkOwned(
	control paths.ControlPlanePaths,
	action planner.FileAction,
	operations fileOperations,
) (FileResult, error) {
	failure := FileResult{StateEffect: action.OnFailure}
	if err := validatePrecondition(control, action); err != nil {
		return failure, err
	}

	temporaryDirectory, err := operations.mkdirTemp(
		filepath.Dir(action.Precondition.TargetPath),
		temporaryDirectoryPrefix,
	)
	if err != nil {
		return failure, fmt.Errorf("create relink temporary directory: %w", err)
	}
	temporaryLink := filepath.Join(temporaryDirectory, temporaryLinkName)
	cleanup := func() error {
		return cleanupTemporaryLink(operations, temporaryLink, temporaryDirectory)
	}
	failPrepared := func(primary error) (FileResult, error) {
		return failure, errors.Join(primary, cleanup())
	}

	if err := operations.symlink(action.Desired.SourcePath, temporaryLink); err != nil {
		return failPrepared(fmt.Errorf("prepare complete relink symlink: %w", err))
	}
	// 准备工作不能延长旧快照的有效期；rename 前重新建立完整 target/source/control 证明。
	if err := validatePrecondition(control, action); err != nil {
		return failPrepared(err)
	}
	if err := operations.rename(temporaryLink, action.Precondition.TargetPath); err != nil {
		return failPrepared(fmt.Errorf("commit relink symlink: %w", err))
	}

	result := FileResult{StateEffect: action.OnSuccess, TargetMutated: true}
	if err := cleanup(); err != nil {
		// rename 已越过 target 提交点；cleanup 错误不能把成功 state effect 伪装成失败。
		return result, fmt.Errorf("cleanup committed relink temporary directory: %w", err)
	}
	return result, nil
}

func cleanupTemporaryLink(operations fileOperations, link, directory string) error {
	var cleanupErrors []error
	if err := operations.remove(link); err != nil && !errors.Is(err, fs.ErrNotExist) {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("remove temporary symlink: %w", err))
	}
	if err := operations.remove(directory); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("remove temporary directory: %w", err))
	}
	return errors.Join(cleanupErrors...)
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
		switch action.Reason {
		case planner.FileReasonTargetMissing:
			if action.Precondition.Observed.Kind != planner.ObjectMissing {
				return fmt.Errorf("%w: L1 create-link target was not planned missing", ErrUnsupportedFileAction)
			}
		case planner.FileReasonOwnedLinkStale:
			if action.Precondition.Observed.Kind != planner.ObjectSymlink ||
				action.Precondition.Observed.LinkDest == action.Desired.SourcePath {
				return fmt.Errorf("%w: L3 create-link lacks an owned stale link snapshot", ErrUnsupportedFileAction)
			}
		default:
			return fmt.Errorf("%w: create-link reason %q is not a link action", ErrUnsupportedFileAction, action.Reason)
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
