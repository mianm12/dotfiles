// Package executor 执行 planner 已决定且通过校验的文件动作。
package executor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

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
	mkdirAll   func(string, fs.FileMode) error
	mkdirTemp  func(string, string) (string, error)
	createTemp func(string, string) (*os.File, error)
	symlink    func(string, string) error
	hardLink   func(string, string) error
	rename     func(string, string) error
	remove     func(string) error
}

// FileResult 保存单个动作供 runtime 消费的结果。StateEffect 已按成功或失败分支选择；
// TargetMutated 只在 target 提交点已经越过时为 true。
type FileResult struct {
	StateEffect   planner.StateEffect
	TargetMutated bool
}

// ExecuteFile 执行当前 M1 link/scaffold 切片支持的动作。调用方负责只传入可信 ApplyPlan 中的
// 动作，本函数仍会拒绝不安全的动作形态，并在每个 target 提交点前重新复核 Precondition。
func ExecuteFile(control paths.ControlPlanePaths, action planner.FileAction) (FileResult, error) {
	return executeFile(control, action, defaultFileOperations())
}

func executeFile(
	control paths.ControlPlanePaths,
	action planner.FileAction,
	operations fileOperations,
) (FileResult, error) {
	failure := FileResult{StateEffect: action.OnFailure}
	if err := ValidateFileAction(action); err != nil {
		return failure, err
	}
	switch action.Desired.Kind {
	case planner.DesiredLink:
		return executeLinkFile(control, action, operations)
	case planner.DesiredScaffold:
		return executeScaffoldFile(control, action, operations)
	default:
		return failure, fmt.Errorf(
			"%w: desired kind %q is not implemented",
			ErrUnsupportedFileAction,
			action.Desired.Kind,
		)
	}
}

// ValidateFileAction 在不读取文件系统的情况下检查 action 是否属于当前 executor 的封闭能力，
// 并验证其字段一致性、leaf Precondition 与 state effect 形态。runner 用它在任何 mutation 前完成
// 全计划 preflight；ExecuteFile 复用同一检查。
func ValidateFileAction(action planner.FileAction) error {
	executionClass := action.Verb.ExecutionClass()
	if executionClass == "" || executionClass == planner.FilePlanOnly {
		return fmt.Errorf("%w: verb %q is not executable", ErrUnsupportedFileAction, action.Verb)
	}
	switch action.Desired.Kind {
	case planner.DesiredLink:
		return validateLinkAction(action)
	case planner.DesiredScaffold:
		return validateScaffoldAction(action)
	default:
		return fmt.Errorf(
			"%w: desired kind %q is not implemented",
			ErrUnsupportedFileAction,
			action.Desired.Kind,
		)
	}
}

func executeLinkFile(
	control paths.ControlPlanePaths,
	action planner.FileAction,
	operations fileOperations,
) (FileResult, error) {
	failure := FileResult{StateEffect: action.OnFailure}

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
		mkdirAll:   os.MkdirAll,
		mkdirTemp:  os.MkdirTemp,
		createTemp: os.CreateTemp,
		symlink:    os.Symlink,
		hardLink:   os.Link,
		rename:     os.Rename,
		remove:     os.Remove,
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
		action.OnFailure.Kind != planner.StatePreserve ||
		!action.Precondition.Leaf.Valid() {
		return fmt.Errorf("%w: inconsistent link action identity or failure effect", ErrUnsupportedFileAction)
	}
	if !filepath.IsAbs(action.Desired.SourcePath) {
		return fmt.Errorf("%w: link source path is not absolute", ErrUnsupportedFileAction)
	}
	if err := validateFileUpsert(action, planner.StateSymlink, action.Desired.SourcePath); err != nil {
		return err
	}

	switch action.Verb {
	case planner.FileAdopt:
		if action.Reason != planner.FileReasonStateMetadata ||
			action.Precondition.RequireRegularSource ||
			action.Precondition.SourcePath != "" ||
			action.Precondition.Leaf.Kind != planner.LeafExactSymlink ||
			action.Precondition.Leaf.LinkDest != action.Desired.SourcePath {
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
			if action.Precondition.Leaf.Kind != planner.LeafMissing {
				return fmt.Errorf("%w: L1 create-link target was not planned missing", ErrUnsupportedFileAction)
			}
		case planner.FileReasonOwnedLinkStale:
			if action.Precondition.Leaf.Kind != planner.LeafExactSymlink ||
				action.Precondition.Leaf.LinkDest == action.Desired.SourcePath {
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

func validateFileUpsert(action planner.FileAction, kind planner.StateKind, linkDest string) error {
	entry := action.OnSuccess.Entry
	if action.Desired.Module == "" || action.Desired.Source == "" ||
		action.OnSuccess.Kind != planner.StateUpsert ||
		action.OnSuccess.Key != action.Desired.Target ||
		entry.Key != action.Desired.Target ||
		entry.Module != action.Desired.Module ||
		entry.Kind != kind ||
		entry.Source != path.Join("modules", action.Desired.Module, action.Desired.Source) ||
		entry.LinkDest != linkDest {
		return fmt.Errorf("%w: inconsistent %s state upsert", ErrUnsupportedFileAction, kind)
	}
	return nil
}

func validatePrecondition(control paths.ControlPlanePaths, action planner.FileAction) error {
	if !action.Precondition.Leaf.Valid() {
		return fmt.Errorf("%w: leaf condition is invalid", ErrPrecondition)
	}
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
	observe := planner.ObserveTarget
	if action.Precondition.Leaf.RequiresRegularDigest() {
		observe = planner.ObserveTargetWithDigest
	}
	observed, err := observe(action.Precondition.TargetPath)
	if err != nil {
		return fmt.Errorf("%w: observe target: %w", ErrPrecondition, err)
	}
	if !action.Precondition.Leaf.Matches(observed) {
		return fmt.Errorf("%w: target leaf condition no longer holds", ErrPrecondition)
	}
	if action.Precondition.RequireRegularSource {
		if err := validateRegularModuleSource(control.Repository(), action); err != nil {
			return err
		}
	}
	return nil
}

func validateRegularModuleSource(repository string, action planner.FileAction) error {
	moduleRoot := filepath.Join(repository, "modules", action.Desired.Module)
	relative, err := filepath.Rel(moduleRoot, action.Precondition.SourcePath)
	if err != nil {
		return fmt.Errorf("%w: locate link source in module: %w", ErrPrecondition, err)
	}
	if relative == "." || filepath.IsAbs(relative) || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
		filepath.ToSlash(relative) != action.Desired.Source {
		return fmt.Errorf("%w: link source is outside its planned module path", ErrPrecondition)
	}

	components := []string{"modules", action.Desired.Module}
	components = append(components, strings.Split(filepath.Clean(relative), string(filepath.Separator))...)
	current := filepath.Clean(repository)
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("%w: inspect link source component %q: %w", ErrPrecondition, current, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: link source component %q is a symlink", ErrPrecondition, current)
		}
		last := index == len(components)-1
		switch {
		case last && !info.Mode().IsRegular():
			return fmt.Errorf("%w: link source %q is not a regular file", ErrPrecondition, current)
		case !last && !info.IsDir():
			return fmt.Errorf("%w: link source ancestor %q is not a directory", ErrPrecondition, current)
		}
	}
	return nil
}
