package executor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/planner"
)

func TestExecutePrune_P1AndP3AreStateOnly(t *testing.T) {
	fixture := newLinkFixture(t)
	tests := []struct {
		name   string
		reason planner.PruneReason
		leaf   planner.LeafCondition
	}{
		{name: "P1 scaffold", reason: planner.PruneReasonScaffold, leaf: planner.LeafCondition{Kind: planner.LeafAny}},
		{
			name:   "P3 unowned symlink",
			reason: planner.PruneReasonUnowned,
			leaf:   planner.LeafCondition{Kind: planner.LeafNotOwnedSymlink, LinkDest: "/previous/owned"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := filepath.Join(fixture.home, test.name)
			if err := os.WriteFile(target, []byte("user data"), 0o640); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}
			action := pruneAction(t, target, test.reason, planner.PruneStateOnly, test.leaf)
			before, err := os.Stat(target)
			if err != nil {
				t.Fatalf("os.Stat() before error = %v", err)
			}

			result, err := ExecutePrune(fixture.control, action)
			if err != nil {
				t.Fatalf("ExecutePrune() error = %v", err)
			}
			if result.TargetMutated || result.StateEffect != action.OnSuccess {
				t.Fatalf("ExecutePrune() result = %#v", result)
			}
			after, err := os.Stat(target)
			if err != nil || !os.SameFile(before, after) || after.Mode().Perm() != 0o640 {
				t.Fatalf("state-only prune changed target: after=%#v err=%v", after, err)
			}
		})
	}
}

func TestExecutePrune_P2DeletesOnlyExactOwnedSymlink(t *testing.T) {
	fixture := newLinkFixture(t)
	target := filepath.Join(fixture.home, ".owned")
	ownedDestination := "/original/link/text"
	if err := os.Symlink(ownedDestination, target); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	action := pruneAction(t, target, planner.PruneReasonOwned, planner.PruneTargetAndState, planner.LeafCondition{
		Kind: planner.LeafExactSymlink, LinkDest: ownedDestination,
	})

	result, err := ExecutePrune(fixture.control, action)
	if err != nil {
		t.Fatalf("ExecutePrune() error = %v", err)
	}
	if !result.TargetMutated || result.StateEffect != action.OnSuccess {
		t.Fatalf("ExecutePrune() result = %#v", result)
	}
	if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("os.Lstat(target) error = %v, want missing", err)
	}
}

func TestExecutePrune_PreconditionMismatchPreservesTargetAndState(t *testing.T) {
	fixture := newLinkFixture(t)
	target := filepath.Join(fixture.home, ".drifted")
	if err := os.Symlink("/planned", target); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	action := pruneAction(t, target, planner.PruneReasonOwned, planner.PruneTargetAndState, planner.LeafCondition{
		Kind: planner.LeafExactSymlink, LinkDest: "/planned",
	})
	if err := os.Remove(target); err != nil {
		t.Fatalf("os.Remove() error = %v", err)
	}
	if err := os.WriteFile(target, []byte("replacement"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	result, err := ExecutePrune(fixture.control, action)
	if !errors.Is(err, ErrPrecondition) {
		t.Fatalf("ExecutePrune() error = %v, want ErrPrecondition", err)
	}
	if result.TargetMutated || result.StateEffect != action.OnFailure {
		t.Fatalf("ExecutePrune() result = %#v", result)
	}
	if !IsPurePreconditionMismatch(err) {
		t.Fatalf("ExecutePrune() error = %v, want pure evidence mismatch", err)
	}
	if got, readErr := os.ReadFile(target); readErr != nil || string(got) != "replacement" {
		t.Fatalf("target after failed prune = %q, %v", got, readErr)
	}
}

func TestExecutePrune_ObservationIOIsNotPureMismatch(t *testing.T) {
	fixture := newLinkFixture(t)
	parent := filepath.Join(fixture.home, "loop-parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}
	target := filepath.Join(parent, "owned")
	if err := os.Symlink("/planned", target); err != nil {
		t.Fatalf("os.Symlink(target) error = %v", err)
	}
	action := pruneAction(t, target, planner.PruneReasonOwned, planner.PruneTargetAndState, planner.LeafCondition{
		Kind: planner.LeafExactSymlink, LinkDest: "/planned",
	})
	if err := os.Remove(target); err != nil {
		t.Fatalf("os.Remove(target) error = %v", err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatalf("os.Remove(parent) error = %v", err)
	}
	if err := os.Symlink("loop-parent", parent); err != nil {
		t.Fatalf("os.Symlink(loop parent) error = %v", err)
	}

	result, err := ExecutePrune(fixture.control, action)
	if !errors.Is(err, ErrPrecondition) {
		t.Fatalf("ExecutePrune() error = %v, want ErrPrecondition", err)
	}
	if IsPurePreconditionMismatch(err) {
		t.Fatalf("ExecutePrune() error = %v, resolution IO must remain runtime error", err)
	}
	if result.TargetMutated || result.StateEffect != action.OnFailure {
		t.Fatalf("ExecutePrune() result = %#v", result)
	}
}

func TestExecutePrune_RejectsDeferredAndMalformedActions(t *testing.T) {
	fixture := newLinkFixture(t)
	target := filepath.Join(fixture.home, ".invalid")
	base := pruneAction(t, target, planner.PruneReasonScaffold, planner.PruneStateOnly, planner.LeafCondition{Kind: planner.LeafAny})
	tests := []struct {
		name   string
		mutate func(*planner.PruneAction)
	}{
		{name: "deferred", mutate: func(action *planner.PruneAction) {
			action.Deferred = true
			action.DeferredReason = planner.PruneDeferredFileConflict
			action.OnSuccess = planner.StateEffect{Kind: planner.StatePreserve}
		}},
		{name: "P2 state only", mutate: func(action *planner.PruneAction) { action.Reason = planner.PruneReasonOwned }},
		{name: "incomplete delete", mutate: func(action *planner.PruneAction) { action.OnSuccess.Key = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			action := base
			test.mutate(&action)
			result, err := ExecutePrune(fixture.control, action)
			if !errors.Is(err, ErrUnsupportedPruneAction) {
				t.Fatalf("ExecutePrune() error = %v, want ErrUnsupportedPruneAction", err)
			}
			if result.TargetMutated || result.StateEffect != action.OnFailure {
				t.Fatalf("ExecutePrune() result = %#v", result)
			}
		})
	}
}

func pruneAction(
	t *testing.T,
	target string,
	reason planner.PruneReason,
	mode planner.PruneMode,
	leaf planner.LeafCondition,
) planner.PruneAction {
	t.Helper()
	resolution, err := paths.ResolveTarget(target)
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	key := "~/" + filepath.Base(target)
	return planner.PruneAction{
		Mode:    mode,
		Target:  key,
		Module:  "old",
		Reason:  reason,
		Warning: reason == planner.PruneReasonUnowned,
		Precondition: planner.Precondition{
			TargetPath: target, TargetResolution: resolution, Leaf: leaf,
		},
		OnSuccess: planner.StateEffect{Kind: planner.StateDelete, Key: key},
		OnFailure: planner.StateEffect{Kind: planner.StatePreserve},
	}
}
