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

func TestExecutorCreationNeverClobbersChangedTargets(t *testing.T) {
	t.Run("link", func(t *testing.T) {
		root, home := newExecutorRoot(t)
		target := filepath.Join(home, ".link")
		writeExecutorFile(t, target, "user")
		before := snapshotExecutorPath(t, target)

		run := mutationRun{home: home}
		err := run.createLink(filepath.Join(root, "desired"), target)
		if err == nil || !strings.Contains(err.Error(), "create symlink") {
			t.Fatalf("createLink() error = %v, want no-clobber failure", err)
		}
		assertExecutorPathUnchanged(t, before)
	})

	t.Run("local", func(t *testing.T) {
		root, home := newExecutorRoot(t)
		source := filepath.Join(root, "example")
		target := filepath.Join(home, ".local")
		writeExecutorFile(t, source, "example")
		writeExecutorFile(t, target, "user")
		before := snapshotExecutorPath(t, target)
		beforeEntries := c1ExecutionEntries(t, home)

		run := mutationRun{home: home}
		err := run.createLocal(source, target)
		if err == nil || !strings.Contains(err.Error(), "without overwrite") {
			t.Fatalf("createLocal() error = %v, want no-clobber failure", err)
		}
		assertExecutorPathUnchanged(t, before)
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

func TestExecutorUpdateAndPruneRecheckRawDestination(t *testing.T) {
	for _, decision := range []planner.Decision{
		planner.DecisionUpdate,
		planner.DecisionPrune,
	} {
		t.Run(string(decision), func(t *testing.T) {
			root, home := newExecutorRoot(t)
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
			before := snapshotExecutorPath(t, target)

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
			assertExecutorPathUnchanged(t, before)
		})
	}
}

func TestExecutorUpdateRechecksResolvedParent(t *testing.T) {
	root, home := newExecutorRoot(t)
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
	firstBefore := snapshotExecutorPath(t, filepath.Join(first, "owned"))
	secondBefore := snapshotExecutorPath(t, filepath.Join(second, "owned"))

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
	assertExecutorPathUnchanged(t, firstBefore)
	assertExecutorPathUnchanged(t, secondBefore)
}

func TestOpenSessionRejectsControlTopologyBeforeLockMutation(t *testing.T) {
	root, home := newExecutorRoot(t)
	repository := filepath.Join(root, "repository")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatalf("os.Mkdir(repository) error = %v", err)
	}
	controls := corepaths.Controls{
		Repository: repository,
		Config:     filepath.Join(root, "config-control", "config.toml"),
		State:      filepath.Join(repository, "state.json"),
		Lock:       filepath.Join(repository, "lock"),
	}
	beforeRepository := snapshotExecutorPath(t, repository)
	beforeEntries := c1ExecutionEntries(t, root)

	session, err := OpenSession(home, controls)

	if !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("OpenSession() = (%#v, %v), want control topology error", session, err)
	}
	if !strings.Contains(err.Error(), "run `dot paths`") {
		t.Fatalf("OpenSession() error = %q, want recovery hint", err)
	}
	assertExecutorPathUnchanged(t, beforeRepository)
	afterEntries := c1ExecutionEntries(t, root)
	if strings.Join(afterEntries, "\n") != strings.Join(beforeEntries, "\n") {
		t.Fatalf(
			"pre-lock failure changed entries: before=%v after=%v",
			beforeEntries,
			afterEntries,
		)
	}
	for _, path := range []string{controls.State, controls.Lock} {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatalf("control path %q error = %v, want missing", path, statErr)
		}
	}
}

func TestSessionConvergeRejectsActiveControlBoundary(t *testing.T) {
	root, home := newExecutorRoot(t)
	repository := filepath.Join(home, "repository")
	source := filepath.Join(repository, "modules", "app", "config")
	writeExecutorFile(t, source, "config")
	controls := corepaths.Controls{
		Repository: repository,
		Config:     filepath.Join(root, "config-control", "config.toml"),
		State:      filepath.Join(root, "state-control", "state.json"),
		Lock:       filepath.Join(root, "state-control", "lock"),
	}
	session, err := OpenSession(home, controls)
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Fatalf("Session.Close() error = %v", err)
		}
	}()
	modules := []config.Module{{
		ID:   "app",
		Root: filepath.Dir(source),
		Links: []config.Link{{
			ID:         "config",
			SourcePath: source,
			Target:     "~/repository",
		}},
	}}
	before := snapshotExecutorPath(t, repository)
	beforeEntries := c1ExecutionEntries(t, root)

	result, err := session.Converge(modules, nil)

	if !errors.Is(err, corepaths.ErrControlBoundary) {
		t.Fatalf("Session.Converge() = (%#v, %v), want control-boundary error", result, err)
	}
	if !strings.Contains(err.Error(), repository) ||
		!strings.Contains(err.Error(), "run `dot paths`") {
		t.Fatalf("Session.Converge() error = %q, want conflicting path and recovery hint", err)
	}
	if len(result.Plan.Actions) != 0 ||
		len(result.Plan.Warnings) != 0 ||
		result.TargetsChanged ||
		result.StateChanged ||
		len(result.Warnings) != 0 {
		t.Fatalf("Session.Converge() result = %#v, want zero result", result)
	}
	assertExecutorPathUnchanged(t, before)
	afterEntries := c1ExecutionEntries(t, root)
	if strings.Join(afterEntries, "\n") != strings.Join(beforeEntries, "\n") {
		t.Fatalf(
			"pre-lock boundary failure changed entries: before=%v after=%v",
			beforeEntries,
			afterEntries,
		)
	}
	if _, statErr := os.Lstat(controls.State); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("state path %q error = %v, want missing", controls.State, statErr)
	}
}

func TestSessionConvergeRevalidatesControlTopologyWithoutFurtherMutation(t *testing.T) {
	root, home := newExecutorRoot(t)
	repository := filepath.Join(root, "repository")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatalf("os.Mkdir(repository) error = %v", err)
	}
	stateRoot := filepath.Join(root, "state-control")
	lockPath := filepath.Join(stateRoot, "lock")
	controls := corepaths.Controls{
		Repository: repository,
		Config:     filepath.Join(root, "config-control", "config.toml"),
		State:      filepath.Join(stateRoot, "state.json"),
		Lock:       lockPath,
	}
	session, err := OpenSession(home, controls)
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Fatalf("Session.Close() error = %v", err)
		}
	}()
	if err := os.Remove(repository); err != nil {
		t.Fatalf("os.Remove(repository) error = %v", err)
	}
	if err := os.Symlink(stateRoot, repository); err != nil {
		t.Fatalf("os.Symlink(repository to state root) error = %v", err)
	}
	stateRootBefore := snapshotExecutorPath(t, stateRoot)
	lockBefore := snapshotExecutorPath(t, lockPath)
	beforeEntries := c1ExecutionEntries(t, root)

	result, runErr := session.Converge(nil, nil)

	if !errors.Is(runErr, corepaths.ErrControlTopology) {
		t.Fatalf("Session.Converge() = (%#v, %v), want topology error", result, runErr)
	}
	if len(result.Plan.Actions) != 0 ||
		result.TargetsChanged ||
		result.StateChanged ||
		len(result.Warnings) != 0 {
		t.Fatalf("Session.Converge() result = %#v, want zero result", result)
	}
	assertExecutorPathUnchanged(t, stateRootBefore)
	assertExecutorPathUnchanged(t, lockBefore)
	afterEntries := c1ExecutionEntries(t, root)
	if strings.Join(afterEntries, "\n") != strings.Join(beforeEntries, "\n") {
		t.Fatalf(
			"locked revalidation changed entries: before=%v after=%v",
			beforeEntries,
			afterEntries,
		)
	}
}

func TestStateCommitFailureLeavesRecoverableFacts(t *testing.T) {
	root, home := newExecutorRoot(t)
	repository := filepath.Join(root, "repository")
	source := filepath.Join(repository, "modules", "app", "config")
	writeExecutorFile(t, source, "config")
	target := filepath.Join(home, ".app")
	request := convergenceRequest{
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
	assertExecutorLink(t, target, source)
	if _, err := os.Lstat(request.Controls.State); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("state after failed commit error = %v, want missing", err)
	}

	second, err := runLocked(request, commitState)
	if err != nil ||
		second.TargetsChanged ||
		!second.StateChanged {
		t.Fatalf("runLocked(recovery) = (%#v, %v), want state-only recovery", second, err)
	}
	assertExecutorLink(t, target, source)
	beforeTarget := snapshotExecutorPath(t, target)
	beforeState := snapshotExecutorPath(t, request.Controls.State)

	third, err := runLocked(request, commitState)
	if err != nil || third.TargetsChanged || third.StateChanged {
		t.Fatalf("runLocked(repeat) = (%#v, %v), want zero mutation", third, err)
	}
	assertExecutorPathUnchanged(t, beforeTarget)
	assertExecutorPathUnchanged(t, beforeState)
}

type executionSnapshot struct {
	path string
	info fs.FileInfo
	mode fs.FileMode
	data string
	link string
}

func newExecutorRoot(t *testing.T) (string, string) {
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

func writeExecutorFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}

func snapshotExecutorPath(t *testing.T, path string) executionSnapshot {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", path, err)
	}
	snapshot := executionSnapshot{path: path, info: info, mode: info.Mode()}
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

func assertExecutorPathUnchanged(t *testing.T, before executionSnapshot) {
	t.Helper()
	after := snapshotExecutorPath(t, before.path)
	if before.mode != after.mode ||
		before.data != after.data ||
		before.link != after.link ||
		!os.SameFile(before.info, after.info) {
		t.Fatalf("path changed\nbefore=%#v\nafter=%#v", before, after)
	}
}

func assertExecutorLink(t *testing.T, target, destination string) {
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
