package executor

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
)

type mutationRun struct {
	home    string
	started bool
	changed bool
}

func (run *mutationRun) apply(plan planner.Plan, snapshot *state.Snapshot) error {
	active := make(map[actionKey]bool)
	for _, action := range plan.Actions {
		if action.Decision == planner.DecisionPrune ||
			action.Decision == planner.DecisionForget {
			continue
		}
		if err := run.applyActive(action); err != nil {
			return actionError(action, err)
		}
		applyState(snapshot, action)
		active[actionKey{moduleID: action.ModuleID, placementID: action.PlacementID}] = true
	}

	for _, action := range plan.Actions {
		if action.Decision != planner.DecisionPrune &&
			action.Decision != planner.DecisionForget {
			continue
		}
		if action.Decision == planner.DecisionPrune {
			if err := run.removeOwnedLink(action); err != nil {
				return actionError(action, err)
			}
		}
		if !active[actionKey{moduleID: action.ModuleID, placementID: action.PlacementID}] {
			applyState(snapshot, action)
		}
	}
	return nil
}

type actionKey struct {
	moduleID    string
	placementID string
}

func (run *mutationRun) applyActive(action planner.Action) error {
	switch action.Decision {
	case planner.DecisionCreateLocal:
		if err := run.ensureParent(action.Target); err != nil {
			return err
		}
		if err := run.createLocal(action.Source, action.Target); err != nil {
			return err
		}
		return verifyLocal(action.Target)
	case planner.DecisionCreateLink:
		if err := run.ensureParent(action.Target); err != nil {
			return err
		}
		if err := run.createLink(action.LinkDestination, action.Target); err != nil {
			return err
		}
		return run.verifyLink(action)
	case planner.DecisionUpdate:
		if err := run.removeOwnedLink(action); err != nil {
			return err
		}
		if err := run.createLink(action.LinkDestination, action.Target); err != nil {
			return err
		}
		return run.verifyLink(action)
	case planner.DecisionAdopt, planner.DecisionKeep, planner.DecisionRepairState:
		return nil
	default:
		return fmt.Errorf("unsupported active decision %q", action.Decision)
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

func (run *mutationRun) removeOwnedLink(action planner.Action) error {
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
	return nil
}

func (run *mutationRun) verifyLink(action planner.Action) error {
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
	relative, err := filepath.Rel(run.home, target)
	if err != nil ||
		relative == "." ||
		relative == ".." ||
		filepath.IsAbs(relative) ||
		relative == "" ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return corepaths.Target{}, fmt.Errorf("target %q is outside HOME %q", target, run.home)
	}
	resolved, err := corepaths.ResolveTarget(run.home, "~/"+filepath.ToSlash(relative))
	if err != nil {
		return corepaths.Target{}, fmt.Errorf("resolve target %q: %w", target, err)
	}
	return resolved, nil
}

func (run *mutationRun) wrapError(err error) error {
	if !run.started {
		return err
	}
	return fmt.Errorf("%w; this run may be partially applied, rerun to converge", err)
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

func actionError(action planner.Action, err error) error {
	return fmt.Errorf(
		"%s %s/%s target %q: %w",
		action.Decision,
		action.ModuleID,
		action.PlacementID,
		action.Target,
		err,
	)
}

func applyState(snapshot *state.Snapshot, action planner.Action) {
	switch action.Decision {
	case planner.DecisionPrune, planner.DecisionForget:
		removePlacement(snapshot, action.ModuleID, action.PlacementID)
	default:
		module := snapshot.Modules[action.ModuleID]
		if module.Placements == nil {
			module.Placements = make(map[string]state.Placement)
		}
		placement := state.Placement{
			Kind:   action.Kind,
			Target: action.Target,
		}
		if action.Kind == state.KindLink {
			placement.ResolvedTarget = action.ResolvedTarget
			placement.LinkDestination = action.LinkDestination
		}
		module.Placements[action.PlacementID] = placement
		snapshot.Modules[action.ModuleID] = module
	}
}

func removePlacement(snapshot *state.Snapshot, moduleID, placementID string) {
	module, exists := snapshot.Modules[moduleID]
	if !exists {
		return
	}
	delete(module.Placements, placementID)
	if len(module.Placements) == 0 {
		delete(snapshot.Modules, moduleID)
		return
	}
	snapshot.Modules[moduleID] = module
}
