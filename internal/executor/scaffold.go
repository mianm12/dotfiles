package executor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/planner"
)

const scaffoldTemporaryPrefix = ".dot-scaffold-"

func executeScaffoldFile(
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
	case planner.FileScaffold:
		switch action.Reason {
		case planner.FileReasonTargetMissing, planner.FileReasonScaffoldRebuild:
			return createMissingScaffold(control, action, operations)
		case planner.FileReasonOwnedLinkToScaffold:
			return migrateOwnedLinkToScaffold(control, action, operations)
		default:
			return failure, fmt.Errorf(
				"%w: scaffold reason %q is not implemented",
				ErrUnsupportedFileAction,
				action.Reason,
			)
		}
	default:
		return failure, fmt.Errorf(
			"%w: verb %q is not a scaffold execution action",
			ErrUnsupportedFileAction,
			action.Verb,
		)
	}
}

func migrateOwnedLinkToScaffold(
	control paths.ControlPlanePaths,
	action planner.FileAction,
	operations fileOperations,
) (FileResult, error) {
	failure := FileResult{StateEffect: action.OnFailure}
	if err := validatePrecondition(control, action); err != nil {
		return failure, err
	}
	temporaryFile, err := prepareScaffoldFile(
		operations,
		filepath.Dir(action.Precondition.TargetPath),
		action.Desired,
	)
	if err != nil {
		return failure, err
	}
	cleanup := func() error {
		if err := operations.remove(temporaryFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove temporary migration file: %w", err)
		}
		return nil
	}
	failPrepared := func(primary error) (FileResult, error) {
		return failure, errors.Join(primary, cleanup())
	}

	// 只有最终快照仍是 planner 证明 owned 的原 symlink 时，才允许 rename 替换。
	if err := validatePrecondition(control, action); err != nil {
		return failPrepared(err)
	}
	if err := operations.rename(temporaryFile, action.Precondition.TargetPath); err != nil {
		return failPrepared(fmt.Errorf("commit owned link-to-scaffold migration: %w", err))
	}
	return FileResult{StateEffect: action.OnSuccess, TargetMutated: true}, nil
}

func createMissingScaffold(
	control paths.ControlPlanePaths,
	action planner.FileAction,
	operations fileOperations,
) (FileResult, error) {
	failure := FileResult{StateEffect: action.OnFailure}
	if err := validatePrecondition(control, action); err != nil {
		return failure, err
	}
	parent := filepath.Dir(action.Precondition.TargetPath)
	if err := operations.mkdirAll(parent, 0o755); err != nil {
		return failure, fmt.Errorf("create scaffold target ancestors: %w", err)
	}
	// mkdir 改变了路径拓扑；准备临时文件前重新证明 target 仍安全缺失。
	if err := validatePrecondition(control, action); err != nil {
		return failure, err
	}

	temporaryFile, err := prepareScaffoldFile(operations, parent, action.Desired)
	if err != nil {
		return failure, err
	}
	cleanup := func() error {
		if err := operations.remove(temporaryFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove temporary scaffold file: %w", err)
		}
		return nil
	}
	failPrepared := func(primary error) (FileResult, error) {
		return failure, errors.Join(primary, cleanup())
	}

	// 完整新对象准备完毕后仍不能沿用旧快照；排他发布前再次复核全部 Precondition。
	if err := validatePrecondition(control, action); err != nil {
		return failPrepared(err)
	}
	if err := operations.hardLink(temporaryFile, action.Precondition.TargetPath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return failPrepared(fmt.Errorf(
				"%w: target appeared before scaffold publish: %v",
				ErrPreconditionMismatch,
				err,
			))
		}
		return failPrepared(fmt.Errorf("publish scaffold without clobber: %w", err))
	}

	result := FileResult{StateEffect: action.OnSuccess, TargetMutated: true}
	if err := cleanup(); err != nil {
		// hard-link 已越过 target 提交点；cleanup 错误不能丢弃成功 state effect。
		return result, fmt.Errorf("cleanup committed scaffold temporary file: %w", err)
	}
	return result, nil
}

func prepareScaffoldFile(
	operations fileOperations,
	parent string,
	desired planner.Desired,
) (temporaryPath string, resultErr error) {
	file, err := operations.createTemp(parent, scaffoldTemporaryPrefix)
	if err != nil {
		return "", fmt.Errorf("create temporary scaffold file: %w", err)
	}
	temporaryPath = file.Name()
	closed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, file.Close())
		}
		if resultErr != nil {
			if cleanupErr := operations.remove(temporaryPath); cleanupErr != nil &&
				!errors.Is(cleanupErr, fs.ErrNotExist) {
				resultErr = errors.Join(resultErr, fmt.Errorf(
					"remove incomplete temporary scaffold file: %w",
					cleanupErr,
				))
			}
		}
	}()

	if _, err := io.Copy(file, bytes.NewReader(desired.Content)); err != nil {
		resultErr = fmt.Errorf("write complete temporary scaffold file: %w", err)
		return
	}
	if err := file.Chmod(desired.Mode.Perm()); err != nil {
		resultErr = fmt.Errorf("set temporary scaffold mode: %w", err)
		return
	}
	if err := file.Sync(); err != nil {
		resultErr = fmt.Errorf("sync temporary scaffold file: %w", err)
		return
	}
	if err := file.Close(); err != nil {
		closed = true
		resultErr = fmt.Errorf("close temporary scaffold file: %w", err)
		return
	}
	closed = true
	return temporaryPath, nil
}

func validateScaffoldAction(action planner.FileAction) error {
	if action.Desired.Kind != planner.DesiredScaffold ||
		action.Target == "" || action.Target != action.Desired.Target ||
		action.Precondition.TargetPath == "" ||
		action.Precondition.TargetPath != action.Desired.TargetPath ||
		action.Precondition.RequireRegularSource ||
		action.Precondition.SourcePath != "" ||
		action.OnFailure.Kind != planner.StatePreserve ||
		action.Desired.Mode&^fs.ModePerm != 0 ||
		!action.Precondition.Leaf.Valid() {
		return fmt.Errorf(
			"%w: inconsistent scaffold identity, mode, source requirement, or failure effect",
			ErrUnsupportedFileAction,
		)
	}

	switch action.Verb {
	case planner.FileAdopt:
		if err := validateFileUpsert(action, planner.StateScaffold, ""); err != nil {
			return err
		}
		switch action.Reason {
		case planner.FileReasonScaffoldPresent:
			if action.Precondition.Leaf.Kind != planner.LeafPresent {
				return fmt.Errorf("%w: scaffold-present adopt was planned missing", ErrUnsupportedFileAction)
			}
		case planner.FileReasonStateMetadata:
			if action.Precondition.Leaf.Kind != planner.LeafAny {
				return fmt.Errorf("%w: scaffold metadata adopt has an unexpected leaf condition", ErrUnsupportedFileAction)
			}
		case planner.FileReasonReleaseOwnershipToScaffold:
			if action.Precondition.Leaf.Kind != planner.LeafNotOwnedSymlink {
				return fmt.Errorf("%w: ownership release lacks negative symlink evidence", ErrUnsupportedFileAction)
			}
		default:
			return fmt.Errorf("%w: reason %q is not a scaffold adopt", ErrUnsupportedFileAction, action.Reason)
		}
	case planner.FileScaffold:
		if err := validateFileUpsert(action, planner.StateScaffold, ""); err != nil {
			return err
		}
		if action.Reason != planner.FileReasonTargetMissing &&
			action.Reason != planner.FileReasonScaffoldRebuild &&
			action.Reason != planner.FileReasonOwnedLinkToScaffold {
			return fmt.Errorf("%w: reason %q is not a scaffold create", ErrUnsupportedFileAction, action.Reason)
		}
		switch action.Reason {
		case planner.FileReasonTargetMissing, planner.FileReasonScaffoldRebuild:
			if action.Precondition.Leaf.Kind != planner.LeafMissing {
				return fmt.Errorf("%w: scaffold create target was not planned missing", ErrUnsupportedFileAction)
			}
		case planner.FileReasonOwnedLinkToScaffold:
			if action.Precondition.Leaf.Kind != planner.LeafExactSymlink {
				return fmt.Errorf("%w: owned link migration was not planned from a symlink", ErrUnsupportedFileAction)
			}
		}
	default:
		return fmt.Errorf("%w: verb %q is not a scaffold action", ErrUnsupportedFileAction, action.Verb)
	}
	return nil
}
