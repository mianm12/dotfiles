package converge

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
)

type mutationRun struct {
	home    string
	started bool
	changed bool
}

func (run *mutationRun) apply(plan Plan) error {
	for _, action := range plan.Actions {
		if err := run.applyAction(action); err != nil {
			recovery := RecoveryNone
			if run.started {
				recovery = RecoveryRerunApply
			}
			return newFailure(
				FailureStageExecute,
				run.started,
				recovery,
				&action,
				err,
			)
		}
	}
	return nil
}

func (run *mutationRun) applyAction(action Action) error {
	switch action.Decision {
	case DecisionCreateLocal:
		if err := run.ensureParent(action.Target); err != nil {
			return err
		}
		if err := run.createLocal(action.Source, action.Target); err != nil {
			return err
		}
		return verifyLocal(action.Target)
	case DecisionCreateLink:
		if err := run.ensureParent(action.Target); err != nil {
			return err
		}
		if err := run.createLink(action.LinkDestination, action.Target); err != nil {
			return err
		}
		return run.verifyLink(action)
	case DecisionUpdate:
		if err := run.removeOwnedLink(action); err != nil {
			return err
		}
		if err := run.createLink(action.LinkDestination, action.Target); err != nil {
			return err
		}
		return run.verifyLink(action)
	case DecisionAdopt, DecisionRepairState:
		return nil
	case DecisionPrune:
		return run.removeOwnedLink(action)
	case DecisionForget:
		return nil
	default:
		return fmt.Errorf("unsupported action decision %q", action.Decision)
	}
}

func (run *mutationRun) ensureParent(target string) error {
	parent := filepath.Dir(target)
	_, err := os.Stat(parent)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect target parent %q: %w", parent, err)
	}
	run.started = true
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create target parent %q: %w", parent, err)
	}
	run.changed = true
	return nil
}

func (run *mutationRun) createLink(destination, target string) error {
	run.started = true
	if err := os.Symlink(destination, target); err != nil {
		return fmt.Errorf("create symlink %q: %w", target, err)
	}
	run.changed = true
	return nil
}

func (run *mutationRun) createLocal(source, target string) (err error) {
	sourceFile, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open local example %q: %w", source, err)
	}
	defer func() {
		err = errors.Join(err, sourceFile.Close())
	}()

	parent := filepath.Dir(target)
	temporary, err := os.CreateTemp(parent, ".dot-local-*")
	if err != nil {
		return fmt.Errorf("create local temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		err = errors.Join(err, os.Remove(temporaryPath))
	}()

	run.started = true
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set local temporary permissions: %w", err)
	}
	if _, err := io.Copy(temporary, sourceFile); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("copy local example: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync local temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close local temporary file: %w", err)
	}
	if err := os.Link(temporaryPath, target); err != nil {
		return fmt.Errorf("publish local file without overwrite %q: %w", target, err)
	}
	run.changed = true
	return nil
}

func (run *mutationRun) removeOwnedLink(action Action) error {
	resolved, err := run.resolveTarget(action.Target)
	if err != nil {
		return err
	}
	if resolved.Resolved() != action.ExpectedResolvedTarget {
		return fmt.Errorf(
			"resolved target changed from %q to %q",
			action.ExpectedResolvedTarget,
			resolved.Resolved(),
		)
	}
	info, err := os.Lstat(action.Target)
	if err != nil {
		return fmt.Errorf("re-read owned symlink %q: %w", action.Target, err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return fmt.Errorf("owned target %q is no longer a symlink", action.Target)
	}
	destination, err := os.Readlink(action.Target)
	if err != nil {
		return fmt.Errorf("re-read owned symlink destination %q: %w", action.Target, err)
	}
	if destination != action.ExpectedLinkDestination {
		return fmt.Errorf(
			"symlink destination changed from %q to %q",
			action.ExpectedLinkDestination,
			destination,
		)
	}

	run.started = true
	if err := os.Remove(action.Target); err != nil {
		return fmt.Errorf("remove owned symlink %q: %w", action.Target, err)
	}
	run.changed = true
	return verifyAbsent(action.Target)
}

func verifyAbsent(target string) error {
	_, err := os.Lstat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("re-read removed target %q: %w", target, err)
	default:
		return fmt.Errorf("removed target %q reappeared", target)
	}
}

func (run *mutationRun) verifyLink(action Action) error {
	resolved, err := run.resolveTarget(action.Target)
	if err != nil {
		return err
	}
	if resolved.Resolved() != action.ResolvedTarget {
		return fmt.Errorf(
			"changed target resolved to %q, want %q",
			resolved.Resolved(),
			action.ResolvedTarget,
		)
	}
	info, err := os.Lstat(action.Target)
	if err != nil {
		return fmt.Errorf("re-read changed symlink %q: %w", action.Target, err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return fmt.Errorf("changed target %q is not a symlink", action.Target)
	}
	destination, err := os.Readlink(action.Target)
	if err != nil {
		return fmt.Errorf("re-read changed symlink destination %q: %w", action.Target, err)
	}
	if destination != action.LinkDestination {
		return fmt.Errorf(
			"changed symlink destination is %q, want %q",
			destination,
			action.LinkDestination,
		)
	}
	return nil
}

func (run *mutationRun) resolveTarget(target string) (corepaths.Target, error) {
	resolved, err := corepaths.ResolveAbsoluteTarget(run.home, target)
	if err != nil {
		return corepaths.Target{}, fmt.Errorf("resolve target %q: %w", target, err)
	}
	return resolved, nil
}

func verifyLocal(target string) error {
	info, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("re-read changed local %q: %w", target, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("changed local %q is not a regular file", target)
	}
	return nil
}
