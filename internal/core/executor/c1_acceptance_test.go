package executor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestC1ExecutionCreationNeverClobbersChangedTargets(t *testing.T) {
	t.Run("link", func(t *testing.T) {
		root, home := newC1ExecutionRoot(t)
		target := filepath.Join(home, ".link")
		writeC1ExecutionFile(t, target, "user")
		before := snapshotC1ExecutionPath(t, target)

		run := mutationRun{home: home}
		err := run.createLink(filepath.Join(root, "desired"), target)
		if err == nil || !strings.Contains(err.Error(), "create symlink") {
			t.Fatalf("createLink() error = %v, want no-clobber failure", err)
		}
		assertC1ExecutionPathUnchanged(t, before)
	})

	t.Run("local", func(t *testing.T) {
		root, home := newC1ExecutionRoot(t)
		source := filepath.Join(root, "example")
		target := filepath.Join(home, ".local")
		writeC1ExecutionFile(t, source, "example")
		writeC1ExecutionFile(t, target, "user")
		before := snapshotC1ExecutionPath(t, target)
		beforeEntries := c1ExecutionEntries(t, home)

		run := mutationRun{home: home}
		err := run.createLocal(source, target)
		if err == nil || !strings.Contains(err.Error(), "without overwrite") {
			t.Fatalf("createLocal() error = %v, want no-clobber failure", err)
		}
		assertC1ExecutionPathUnchanged(t, before)
		afterEntries := c1ExecutionEntries(t, home)
		if strings.Join(beforeEntries, "\n") != strings.Join(afterEntries, "\n") {
			t.Fatalf(
				"local no-clobber left filesystem artifacts: before=%v after=%v",
				beforeEntries,
				afterEntries,
			)
		}
	})
}

func TestC1ExecutionUpdateAndPruneRecheckRawDestination(t *testing.T) {
	for _, decision := range []planner.Decision{
		planner.DecisionUpdate,
		planner.DecisionPrune,
	} {
		t.Run(string(decision), func(t *testing.T) {
			root, home := newC1ExecutionRoot(t)
			target := filepath.Join(home, ".owned")
			expected := filepath.Join(root, "expected")
			changed := filepath.Join(root, "changed")
			if err := os.Symlink(changed, target); err != nil {
				t.Fatalf("os.Symlink(changed target) error = %v", err)
			}
			resolved, err := corepaths.ResolveTarget(home, "~/.owned")
			if err != nil {
				t.Fatalf("ResolveTarget() error = %v", err)
			}
			before := snapshotC1ExecutionPath(t, target)

			run := mutationRun{home: home}
			err = run.removeOwnedLink(planner.Action{
				Decision:                decision,
				Target:                  target,
				ExpectedResolvedTarget:  resolved.Resolved(),
				ExpectedLinkDestination: expected,
			})
			if err == nil || !strings.Contains(err.Error(), "destination changed") {
				t.Fatalf("removeOwnedLink() error = %v, want raw-destination drift", err)
			}
			assertC1ExecutionPathUnchanged(t, before)
		})
	}
}

func TestC1ExecutionUpdateRechecksResolvedParent(t *testing.T) {
	root, home := newC1ExecutionRoot(t)
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, directory := range []string{first, second} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", directory, err)
		}
	}
	parent := filepath.Join(home, "current")
	if err := os.Symlink(first, parent); err != nil {
		t.Fatalf("os.Symlink(first parent) error = %v", err)
	}
	target := filepath.Join(parent, "owned")
	destination := filepath.Join(root, "destination")
	if err := os.Symlink(destination, filepath.Join(first, "owned")); err != nil {
		t.Fatalf("os.Symlink(first target) error = %v", err)
	}
	resolved, err := corepaths.ResolveTarget(home, "~/current/owned")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatalf("os.Remove(parent) error = %v", err)
	}
	if err := os.Symlink(second, parent); err != nil {
		t.Fatalf("os.Symlink(second parent) error = %v", err)
	}
	if err := os.Symlink(destination, filepath.Join(second, "owned")); err != nil {
		t.Fatalf("os.Symlink(second target) error = %v", err)
	}
	firstBefore := snapshotC1ExecutionPath(t, filepath.Join(first, "owned"))
	secondBefore := snapshotC1ExecutionPath(t, filepath.Join(second, "owned"))

	run := mutationRun{home: home}
	err = run.removeOwnedLink(planner.Action{
		Decision:                planner.DecisionUpdate,
		Target:                  target,
		ExpectedResolvedTarget:  resolved.Resolved(),
		ExpectedLinkDestination: destination,
	})
	if err == nil || !strings.Contains(err.Error(), "resolved target changed") {
		t.Fatalf("removeOwnedLink() error = %v, want resolved-parent drift", err)
	}
	assertC1ExecutionPathUnchanged(t, firstBefore)
	assertC1ExecutionPathUnchanged(t, secondBefore)
}

func TestC1StateCommitFailureLeavesRecoverableFacts(t *testing.T) {
	root, home := newC1ExecutionRoot(t)
	repository := filepath.Join(root, "repository")
	source := filepath.Join(repository, "modules", "app", "config")
	writeC1ExecutionFile(t, source, "config")
	target := filepath.Join(home, ".app")
	request := Request{
		Home: home,
		Controls: corepaths.Controls{
			Repository: repository,
			Config:     filepath.Join(home, ".config", "dot", "config.toml"),
			State:      filepath.Join(home, ".local", "state", "dot", "state.json"),
			Lock:       filepath.Join(home, ".local", "state", "dot", "lock"),
		},
		Modules: []config.Module{{
			ID:   "app",
			Root: filepath.Dir(source),
			Links: []config.Link{{
				ID:         "config",
				SourcePath: source,
				Target:     "~/.app",
			}},
		}},
	}

	first, err := runLocked(request, func(string, state.Snapshot) error {
		return errors.New("injected state commit failure")
	})
	if err == nil ||
		!strings.Contains(err.Error(), "injected state commit failure") ||
		!strings.Contains(err.Error(), "partially applied") ||
		!first.TargetsChanged ||
		first.StateChanged {
		t.Fatalf("runLocked(failing commit) = (%#v, %v), want recoverable partial failure", first, err)
	}
	assertC1ExecutionLink(t, target, source)
	if _, err := os.Lstat(request.Controls.State); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("state after failed commit error = %v, want missing", err)
	}

	second, err := runLocked(request, commitState)
	if err != nil ||
		second.TargetsChanged ||
		!second.StateChanged {
		t.Fatalf("runLocked(recovery) = (%#v, %v), want state-only recovery", second, err)
	}
	assertC1ExecutionLink(t, target, source)
	beforeTarget := snapshotC1ExecutionPath(t, target)
	beforeState := snapshotC1ExecutionPath(t, request.Controls.State)

	third, err := runLocked(request, commitState)
	if err != nil || third.TargetsChanged || third.StateChanged {
		t.Fatalf("runLocked(repeat) = (%#v, %v), want zero mutation", third, err)
	}
	assertC1ExecutionPathUnchanged(t, beforeTarget)
	assertC1ExecutionPathUnchanged(t, beforeState)
}

type c1ExecutionSnapshot struct {
	path string
	info fs.FileInfo
	mode fs.FileMode
	data string
	link string
}

func newC1ExecutionRoot(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(HOME) error = %v", err)
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(home) {
		t.Fatalf("synthetic paths must be absolute: root=%q HOME=%q", root, home)
	}
	return root, home
}

func writeC1ExecutionFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}

func snapshotC1ExecutionPath(t *testing.T, path string) c1ExecutionSnapshot {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", path, err)
	}
	snapshot := c1ExecutionSnapshot{path: path, info: info, mode: info.Mode()}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		snapshot.link, err = os.Readlink(path)
	case info.Mode().IsRegular():
		var data []byte
		data, err = os.ReadFile(path)
		snapshot.data = string(data)
	}
	if err != nil {
		t.Fatalf("snapshot %q error = %v", path, err)
	}
	return snapshot
}

func assertC1ExecutionPathUnchanged(t *testing.T, before c1ExecutionSnapshot) {
	t.Helper()
	after := snapshotC1ExecutionPath(t, before.path)
	if before.mode != after.mode ||
		before.data != after.data ||
		before.link != after.link ||
		!os.SameFile(before.info, after.info) {
		t.Fatalf("path changed\nbefore=%#v\nafter=%#v", before, after)
	}
}

func assertC1ExecutionLink(t *testing.T, target, destination string) {
	t.Helper()
	actual, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("os.Readlink(%q) error = %v", target, err)
	}
	if actual != destination {
		t.Fatalf("link %q = %q, want %q", target, actual, destination)
	}
}

func c1ExecutionEntries(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, relative)
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("filepath.WalkDir(%q) error = %v", root, err)
	}
	return entries
}
