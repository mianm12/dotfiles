package converge

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/storage"
)

type mutationRun struct {
	started         bool
	targetsChanged  bool
	controlsChanged bool
	removeTemporary func(string) error
}

func (run *mutationRun) apply(
	lines []loopLine,
	ownership *state.Snapshot,
) ([]Line, []Line, error) {
	done := make([]Line, 0, len(lines))
	stateOnly := make([]Line, 0, len(lines))
	for _, line := range lines {
		completed, err := run.applyLine(line, ownership)
		if completed {
			if line.Op == OpRecord || line.Op == OpForget {
				stateOnly = append(stateOnly, line.Line)
			} else {
				done = append(done, line.Line)
			}
		}
		if err != nil {
			return done, stateOnly, newFailure(run.started, &line.Line, err)
		}
	}
	return done, stateOnly, nil
}

func (run *mutationRun) applyLine(line loopLine, ownership *state.Snapshot) (bool, error) {
	switch line.Op {
	case OpChmod:
		changed, err := storage.SetPrivateMode(line.Path, line.mode)
		if err != nil {
			run.started = true
			return false, err
		}
		run.started = run.started || changed
		run.controlsChanged = run.controlsChanged || changed
		return changed, nil
	case OpFile:
		if err := run.ensureParent(line.Target); err != nil {
			return false, err
		}
		return run.createLocal(line.source, line.Target)
	case OpLink:
		if err := run.ensureParent(line.Target); err != nil {
			return false, err
		}
		if err := run.createLink(line.dest, line.Target); err != nil {
			return false, err
		}
		setOwnership(ownership, line)
		return true, nil
	case OpReplace:
		if err := run.removeOwnedLink(line.Target, line.beforeDest); err != nil {
			return false, err
		}
		if err := run.createLink(line.dest, line.Target); err != nil {
			return false, err
		}
		setOwnership(ownership, line)
		return true, nil
	case OpRecord:
		setOwnership(ownership, line)
		return true, nil
	case OpRemove:
		if err := run.removeOwnedLink(line.Target, line.beforeDest); err != nil {
			return false, err
		}
		deleteOwnershipIfMatches(ownership, line)
		return true, nil
	case OpForget:
		deleteOwnershipIfMatches(ownership, line)
		return true, nil
	default:
		return false, fmt.Errorf("unsupported line %q", line.Op)
	}
}

func setOwnership(ownership *state.Snapshot, line loopLine) {
	ownership.Links[state.Key{
		ModuleID:    line.ModuleID,
		PlacementID: line.PlacementID,
	}] = state.LinkRecord{
		Target: line.targetID,
		Dest:   line.dest,
	}
}

func deleteOwnershipIfMatches(ownership *state.Snapshot, line loopLine) {
	key := state.Key{
		ModuleID:    line.ModuleID,
		PlacementID: line.PlacementID,
	}
	want := state.LinkRecord{
		Target: line.targetID,
		Dest:   line.beforeDest,
	}
	if current, exists := ownership.Links[key]; exists && current == want {
		delete(ownership.Links, key)
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
	run.targetsChanged = true
	return nil
}

func (run *mutationRun) createLink(destination, target string) error {
	run.started = true
	if err := os.Symlink(destination, target); err != nil {
		return fmt.Errorf("create symlink %q: %w", target, err)
	}
	run.targetsChanged = true
	return nil
}

func (run *mutationRun) createLocal(source, target string) (completed bool, err error) {
	sourceFile, err := os.Open(source)
	if err != nil {
		return false, fmt.Errorf("open local example %q: %w", source, err)
	}
	defer func() {
		err = errors.Join(err, sourceFile.Close())
	}()

	parent := filepath.Dir(target)
	temporary, err := os.CreateTemp(parent, ".dot-local-*")
	if err != nil {
		return false, fmt.Errorf("create local temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		remove := run.removeTemporary
		if remove == nil {
			remove = os.Remove
		}
		err = errors.Join(err, remove(temporaryPath))
	}()

	run.started = true
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("set local temporary permissions: %w", err)
	}
	if _, err := io.Copy(temporary, sourceFile); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("copy local example: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("sync local temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("close local temporary file: %w", err)
	}
	if err := os.Link(temporaryPath, target); err != nil {
		return false, fmt.Errorf("publish local file without overwrite %q: %w", target, err)
	}
	run.targetsChanged = true
	return true, nil
}

func (run *mutationRun) removeOwnedLink(target, expectedDest string) error {
	info, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("re-read owned symlink %q: %w", target, err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return fmt.Errorf("owned target %q is no longer a symlink", target)
	}
	destination, err := os.Readlink(target)
	if err != nil {
		return fmt.Errorf("re-read owned symlink destination %q: %w", target, err)
	}
	if destination != expectedDest {
		return fmt.Errorf(
			"symlink destination changed from %q to %q",
			expectedDest,
			destination,
		)
	}

	run.started = true
	if err := os.Remove(target); err != nil {
		return fmt.Errorf("remove owned symlink %q: %w", target, err)
	}
	run.targetsChanged = true
	return verifyAbsent(target)
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
