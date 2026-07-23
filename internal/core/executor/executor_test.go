package executor

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/lock"
)

func TestAcceptance01_ExecutorRepeatApplyDoesNotMutate(t *testing.T) {
	fixture := newFixture(t)
	linkSource := fixture.writeRepositoryFile(t, "modules/base/config", "portable")
	localSource := fixture.writeRepositoryFile(t, "modules/base/local.example", "local")
	linkTarget := filepath.Join(fixture.home, ".config", "base", "config")
	localTarget := filepath.Join(fixture.home, ".config", "base", "local")
	request := fixture.request([]config.Module{{
		ID:   "base",
		Root: filepath.Join(fixture.repository, "modules", "base"),
		Links: []config.Link{{
			ID:         "config",
			SourcePath: linkSource,
			Target:     "~/.config/base/config",
		}},
		Locals: []config.Local{{
			ID:          "local",
			ExamplePath: localSource,
			Target:      "~/.config/base/local",
		}},
	}})

	first, err := Run(request)
	if err != nil {
		t.Fatalf("Run(first) error = %v", err)
	}
	if !first.TargetsChanged || !first.StateChanged {
		t.Fatalf("Run(first) = %#v, want target and state changes", first)
	}
	assertSymlink(t, linkTarget, linkSource)
	if data, err := os.ReadFile(localTarget); err != nil || string(data) != "local" {
		t.Fatalf("local target = (%q, %v), want local", data, err)
	}

	before := snapshotFiles(t, linkTarget, localTarget, fixture.state)
	second, err := Run(request)
	if err != nil {
		t.Fatalf("Run(second) error = %v", err)
	}
	if second.TargetsChanged || second.StateChanged {
		t.Fatalf("Run(second) = %#v, want zero artifact/state mutation", second)
	}
	assertFilesUnchanged(t, before)
}

func TestAcceptance01_EmptySelectionCommitsStateOnce(t *testing.T) {
	fixture := newFixture(t)
	request := fixture.request(nil)

	first, err := Run(request)
	if err != nil {
		t.Fatalf("Run(first) error = %v", err)
	}
	if first.TargetsChanged || !first.StateChanged {
		t.Fatalf("Run(first) = %#v, want only initial empty state commit", first)
	}
	before := snapshotFiles(t, fixture.state)
	second, err := Run(request)
	if err != nil {
		t.Fatalf("Run(second) error = %v", err)
	}
	if second.TargetsChanged || second.StateChanged {
		t.Fatalf("Run(second) = %#v, want zero mutation", second)
	}
	assertFilesUnchanged(t, before)
}

func TestExecutorDoesNotPruneUntilAllActiveCreatesSucceed(t *testing.T) {
	fixture := newFixture(t)
	staleTarget := filepath.Join(fixture.home, ".stale")
	staleDestination := fixture.writeRepositoryFile(t, "modules/old/file", "old")
	if err := os.Symlink(staleDestination, staleTarget); err != nil {
		t.Fatalf("os.Symlink(stale) error = %v", err)
	}
	resolvedStale, err := corepaths.ResolveTarget(fixture.home, "~/.stale")
	if err != nil {
		t.Fatalf("ResolveTarget(stale) error = %v", err)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"old": {Placements: map[string]state.Placement{
				"file": {
					Kind:            state.KindLink,
					Target:          staleTarget,
					ResolvedTarget:  resolvedStale.Resolved(),
					LinkDestination: staleDestination,
				},
			}},
		},
	})

	missingSource := filepath.Join(fixture.repository, "modules", "new", "missing")
	request := fixture.request([]config.Module{{
		ID: "new",
		Locals: []config.Local{{
			ID:          "file",
			ExamplePath: missingSource,
			Target:      "~/.new",
		}},
	}})
	_, err = Run(request)
	if err == nil {
		t.Fatal("Run() error = nil, want changed-target verification failure")
	}
	assertSymlink(t, staleTarget, staleDestination)
	loaded, loadErr := state.Load(fixture.state, fixture.home)
	if loadErr != nil {
		t.Fatalf("state.Load() error = %v", loadErr)
	}
	if _, exists := loaded.Snapshot.Modules["old"]; !exists {
		t.Fatal("old state was committed before active changes completed")
	}
}

func TestExecutorDoesNotUpdateUntilAllCreatesSucceed(t *testing.T) {
	fixture := newFixture(t)
	oldSource := fixture.writeRepositoryFile(t, "modules/base/old", "old")
	newSource := fixture.writeRepositoryFile(t, "modules/base/new", "new")
	target := filepath.Join(fixture.home, ".owned")
	if err := os.Symlink(oldSource, target); err != nil {
		t.Fatalf("os.Symlink(old) error = %v", err)
	}
	fixture.writeLinkState(t, target, oldSource)

	request := fixture.request([]config.Module{
		{
			ID: "base",
			Links: []config.Link{{
				ID:         "file",
				SourcePath: newSource,
				Target:     "~/.owned",
			}},
		},
		{
			ID: "create",
			Locals: []config.Local{{
				ID:          "file",
				ExamplePath: filepath.Join(fixture.repository, "missing.example"),
				Target:      "~/.create",
			}},
		},
	})
	_, err := Run(request)
	if err == nil {
		t.Fatal("Run() error = nil, want create failure")
	}
	assertSymlink(t, target, oldSource)
	loaded, loadErr := state.Load(fixture.state, fixture.home)
	if loadErr != nil {
		t.Fatalf("state.Load() error = %v", loadErr)
	}
	if got := loaded.Snapshot.Modules["base"].Placements["file"].LinkDestination; got != oldSource {
		t.Fatalf("state destination = %q, want unchanged %q", got, oldSource)
	}
}

func TestAcceptance06_ExecutorMovesTargetBeforePruning(t *testing.T) {
	fixture := newFixture(t)
	source := fixture.writeRepositoryFile(t, "modules/base/file", "file")
	oldTarget := filepath.Join(fixture.home, ".old")
	newTarget := filepath.Join(fixture.home, ".new")
	if err := os.Symlink(source, oldTarget); err != nil {
		t.Fatalf("os.Symlink(old) error = %v", err)
	}
	fixture.writeLinkState(t, oldTarget, source)

	request := fixture.linkRequest(source, "~/.new")
	first, err := Run(request)
	if err != nil {
		t.Fatalf("Run(first) error = %v", err)
	}
	if !first.TargetsChanged || !first.StateChanged {
		t.Fatalf("Run(first) = %#v, want target and state changes", first)
	}
	if _, err := os.Lstat(oldTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old target error = %v, want missing; plan = %#v", err, first.Plan)
	}
	assertSymlink(t, newTarget, source)
	loaded, err := state.Load(fixture.state, fixture.home)
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}
	record := loaded.Snapshot.Modules["base"].Placements["file"]
	if record.Target != newTarget || record.LinkDestination != source {
		t.Fatalf("new state record = %#v, want new target", record)
	}

	before := snapshotFiles(t, newTarget, fixture.state)
	second, err := Run(request)
	if err != nil {
		t.Fatalf("Run(second) error = %v", err)
	}
	if second.TargetsChanged || second.StateChanged {
		t.Fatalf("Run(second) = %#v, want zero mutation", second)
	}
	assertFilesUnchanged(t, before)
}

func TestExecutorUpdatesOwnedLinkAndCommitsVerifiedState(t *testing.T) {
	fixture := newFixture(t)
	oldSource := fixture.writeRepositoryFile(t, "modules/base/old", "old")
	newSource := fixture.writeRepositoryFile(t, "modules/base/new", "new")
	target := filepath.Join(fixture.home, ".file")
	if err := os.Symlink(oldSource, target); err != nil {
		t.Fatalf("os.Symlink(old) error = %v", err)
	}
	fixture.writeLinkState(t, target, oldSource)

	request := fixture.linkRequest(newSource, "~/.file")
	first, err := Run(request)
	if err != nil {
		t.Fatalf("Run(first) error = %v", err)
	}
	if !first.TargetsChanged || !first.StateChanged {
		t.Fatalf("Run(first) = %#v, want target and state changes", first)
	}
	assertSymlink(t, target, newSource)
	loaded, err := state.Load(fixture.state, fixture.home)
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}
	if got := loaded.Snapshot.Modules["base"].Placements["file"].LinkDestination; got != newSource {
		t.Fatalf("state destination = %q, want %q", got, newSource)
	}

	before := snapshotFiles(t, target, fixture.state)
	second, err := Run(request)
	if err != nil {
		t.Fatalf("Run(second) error = %v", err)
	}
	if second.TargetsChanged || second.StateChanged {
		t.Fatalf("Run(second) = %#v, want zero mutation", second)
	}
	assertFilesUnchanged(t, before)
}

func TestExecutorRejectsConflictBeforeArtifactOrStateMutation(t *testing.T) {
	fixture := newFixture(t)
	target := filepath.Join(fixture.home, ".config", "blocked")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(target, []byte("personal"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	source := fixture.writeRepositoryFile(t, "modules/base/file", "portable")
	request := fixture.request([]config.Module{{
		ID: "base",
		Links: []config.Link{{
			ID:         "file",
			SourcePath: source,
			Target:     "~/.config/blocked",
		}},
	}})

	before := snapshotFiles(t, target)
	result, err := Run(request)
	if err == nil || !result.Plan.HasConflicts() {
		t.Fatalf("Run() = (%#v, %v), want deterministic conflict", result, err)
	}
	assertFilesUnchanged(t, before)
	if _, err := os.Lstat(fixture.state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state path error = %v, want missing", err)
	}
}

func TestAcceptance11_ExecutorRechecksRawAndResolvedFactsBeforeDelete(t *testing.T) {
	t.Run("raw destination", func(t *testing.T) {
		fixture := newFixture(t)
		target := filepath.Join(fixture.home, ".owned")
		oldDestination := fixture.writeRepositoryFile(t, "modules/base/old", "old")
		otherDestination := fixture.writeRepositoryFile(t, "modules/base/other", "other")
		if err := os.Symlink(otherDestination, target); err != nil {
			t.Fatalf("os.Symlink() error = %v", err)
		}
		resolved, err := corepaths.ResolveTarget(fixture.home, "~/.owned")
		if err != nil {
			t.Fatalf("ResolveTarget() error = %v", err)
		}
		run := mutationRun{home: fixture.home}
		err = run.removeOwnedLink(planner.Action{
			Target:                  target,
			ExpectedResolvedTarget:  resolved.Resolved(),
			ExpectedLinkDestination: oldDestination,
		})
		if err == nil || !strings.Contains(err.Error(), "destination changed") {
			t.Fatalf("removeOwnedLink() error = %v, want raw-destination drift", err)
		}
		assertSymlink(t, target, otherDestination)
	})

	t.Run("resolved target", func(t *testing.T) {
		fixture := newFixture(t)
		realOne := filepath.Join(fixture.home, "real-one")
		realTwo := filepath.Join(fixture.home, "real-two")
		for _, directory := range []string{realOne, realTwo} {
			if err := os.MkdirAll(directory, 0o700); err != nil {
				t.Fatalf("os.MkdirAll() error = %v", err)
			}
		}
		parent := filepath.Join(fixture.home, "current")
		if err := os.Symlink(realTwo, parent); err != nil {
			t.Fatalf("os.Symlink(parent) error = %v", err)
		}
		target := filepath.Join(parent, "owned")
		destination := fixture.writeRepositoryFile(t, "modules/base/file", "file")
		if err := os.Symlink(destination, target); err != nil {
			t.Fatalf("os.Symlink(target) error = %v", err)
		}
		run := mutationRun{home: fixture.home}
		err := run.removeOwnedLink(planner.Action{
			Target:                  target,
			ExpectedResolvedTarget:  filepath.Join(realOne, "owned"),
			ExpectedLinkDestination: destination,
		})
		if err == nil || !strings.Contains(err.Error(), "resolved target changed") {
			t.Fatalf("removeOwnedLink() error = %v, want resolved drift", err)
		}
		assertSymlink(t, target, destination)
	})
}

func TestAcceptance13_InterruptedFactsConvergeAndThenRemainUnchanged(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, fixture) Request
	}{
		{
			name: "link created before state",
			prepare: func(t *testing.T, fixture fixture) Request {
				source := fixture.writeRepositoryFile(t, "modules/base/file", "file")
				if err := os.Symlink(source, filepath.Join(fixture.home, ".file")); err != nil {
					t.Fatalf("os.Symlink() error = %v", err)
				}
				return fixture.linkRequest(source, "~/.file")
			},
		},
		{
			name: "local published before state",
			prepare: func(t *testing.T, fixture fixture) Request {
				source := fixture.writeRepositoryFile(t, "modules/base/local.example", "example")
				if err := os.WriteFile(filepath.Join(fixture.home, ".local"), []byte("personal"), 0o600); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
				return fixture.localRequest(source, "~/.local")
			},
		},
		{
			name: "updated link before state",
			prepare: func(t *testing.T, fixture fixture) Request {
				oldSource := fixture.writeRepositoryFile(t, "modules/base/old", "old")
				newSource := fixture.writeRepositoryFile(t, "modules/base/new", "new")
				target := filepath.Join(fixture.home, ".file")
				if err := os.Symlink(newSource, target); err != nil {
					t.Fatalf("os.Symlink() error = %v", err)
				}
				fixture.writeLinkState(t, target, oldSource)
				return fixture.linkRequest(newSource, "~/.file")
			},
		},
		{
			name: "old link deleted during update",
			prepare: func(t *testing.T, fixture fixture) Request {
				oldSource := fixture.writeRepositoryFile(t, "modules/base/old", "old")
				newSource := fixture.writeRepositoryFile(t, "modules/base/new", "new")
				target := filepath.Join(fixture.home, ".file")
				fixture.writeLinkState(t, target, oldSource)
				return fixture.linkRequest(newSource, "~/.file")
			},
		},
		{
			name: "stale link pruned before state",
			prepare: func(t *testing.T, fixture fixture) Request {
				source := fixture.writeRepositoryFile(t, "modules/base/file", "file")
				target := filepath.Join(fixture.home, ".file")
				fixture.writeLinkState(t, target, source)
				return fixture.request(nil)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t)
			request := test.prepare(t, fixture)
			first, err := Run(request)
			if err != nil {
				t.Fatalf("Run(first) error = %v", err)
			}
			if !first.StateChanged {
				t.Fatalf("Run(first) = %#v, want state recovery", first)
			}
			before := snapshotExistingControlAndTargets(t, fixture)
			second, err := Run(request)
			if err != nil {
				t.Fatalf("Run(second) error = %v", err)
			}
			if second.TargetsChanged || second.StateChanged {
				t.Fatalf("Run(second) = %#v, want no mutation", second)
			}
			assertFilesUnchanged(t, before)
		})
	}
}

func TestAcceptance13_StateCommitFailureLeavesSafeArtifactForRerun(t *testing.T) {
	fixture := newFixture(t)
	source := fixture.writeRepositoryFile(t, "modules/base/file", "file")
	request := fixture.linkRequest(source, "~/.file")

	result, err := runLocked(request, func(string, state.Snapshot) error {
		return errors.New("injected commit failure")
	})
	if err == nil || !strings.Contains(err.Error(), "partially applied") {
		t.Fatalf("runLocked() = (%#v, %v), want partial-apply error", result, err)
	}
	assertSymlink(t, filepath.Join(fixture.home, ".file"), source)
	if _, err := os.Lstat(fixture.state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state path error = %v, want missing", err)
	}

	recovered, err := Run(request)
	if err != nil {
		t.Fatalf("Run(recovery) error = %v", err)
	}
	if recovered.TargetsChanged || !recovered.StateChanged {
		t.Fatalf("Run(recovery) = %#v, want adopt plus state commit", recovered)
	}
	before := snapshotFiles(t, filepath.Join(fixture.home, ".file"), fixture.state)
	repeated, err := Run(request)
	if err != nil {
		t.Fatalf("Run(repeated) error = %v", err)
	}
	if repeated.TargetsChanged || repeated.StateChanged {
		t.Fatalf("Run(repeated) = %#v, want zero mutation", repeated)
	}
	assertFilesUnchanged(t, before)
}

func TestAcceptance07_ScopedExecutorRepeatApplyDoesNotMutate(t *testing.T) {
	fixture := newFixture(t)
	source := fixture.writeRepositoryFile(t, "modules/extra/file", "file")
	request := fixture.request([]config.Module{{
		ID: "extra",
		Links: []config.Link{{
			ID:         "file",
			SourcePath: source,
			Target:     "~/.extra",
		}},
	}})
	request.Scope = []string{"extra"}

	first, err := Run(request)
	if err != nil {
		t.Fatalf("Run(first) error = %v", err)
	}
	if !first.TargetsChanged || !first.StateChanged {
		t.Fatalf("Run(first) = %#v, want target and state changes", first)
	}
	before := snapshotFiles(t, filepath.Join(fixture.home, ".extra"), fixture.state)
	second, err := Run(request)
	if err != nil {
		t.Fatalf("Run(second) error = %v", err)
	}
	if second.TargetsChanged || second.StateChanged {
		t.Fatalf("Run(second) = %#v, want zero mutation", second)
	}
	assertFilesUnchanged(t, before)
}

func TestExecutorUsesNoClobberCreation(t *testing.T) {
	t.Run("link", func(t *testing.T) {
		fixture := newFixture(t)
		target := filepath.Join(fixture.home, ".target")
		if err := os.WriteFile(target, []byte("personal"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(target) error = %v", err)
		}
		run := mutationRun{home: fixture.home}
		if err := run.createLink("/desired", target); err == nil {
			t.Fatal("createLink() error = nil, want no-clobber failure")
		}
		if data, err := os.ReadFile(target); err != nil || string(data) != "personal" {
			t.Fatalf("target = (%q, %v), want personal bytes", data, err)
		}
	})

	t.Run("local", func(t *testing.T) {
		fixture := newFixture(t)
		source := fixture.writeRepositoryFile(t, "modules/base/local.example", "example")
		target := filepath.Join(fixture.home, ".local")
		if err := os.WriteFile(target, []byte("personal"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(target) error = %v", err)
		}
		run := mutationRun{home: fixture.home}
		if err := run.createLocal(source, target); err == nil {
			t.Fatal("createLocal() error = nil, want no-clobber failure")
		}
		if data, err := os.ReadFile(target); err != nil || string(data) != "personal" {
			t.Fatalf("target = (%q, %v), want personal bytes", data, err)
		}
	})
}

func TestExecutorUsesSingleAdvisoryLock(t *testing.T) {
	fixture := newFixture(t)
	owner, err := lock.Acquire(filepath.Dir(fixture.lock), fixture.lock)
	if err != nil {
		t.Fatalf("lock.Acquire() error = %v", err)
	}
	defer func() {
		if err := owner.Release(); err != nil {
			t.Errorf("owner.Release() error = %v", err)
		}
	}()

	_, err = Run(fixture.request(nil))
	if !errors.Is(err, lock.ErrBusy) {
		t.Fatalf("Run() error = %v, want lock.ErrBusy", err)
	}
	if _, err := os.Lstat(fixture.state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state path error = %v, want missing", err)
	}
}

func TestCommitStateCreatesAndReplacesWithPrivatePermissions(t *testing.T) {
	fixture := newFixture(t)
	initial, err := state.New(fixture.home)
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	if err := commitState(fixture.state, initial); err != nil {
		t.Fatalf("commitState(initial) error = %v", err)
	}
	if err := os.Chmod(fixture.state, 0o644); err != nil {
		t.Fatalf("os.Chmod(state) error = %v", err)
	}

	updated := cloneSnapshot(initial)
	updated.Modules["base"] = state.Module{Placements: map[string]state.Placement{
		"local": {
			Kind:   state.KindLocal,
			Target: filepath.Join(fixture.home, ".local"),
		},
	}}
	if err := commitState(fixture.state, updated); err != nil {
		t.Fatalf("commitState(updated) error = %v", err)
	}
	info, err := os.Stat(fixture.state)
	if err != nil {
		t.Fatalf("os.Stat(state) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state permissions = %04o, want 0600", got)
	}
	loaded, err := state.Load(fixture.state, fixture.home)
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded.Snapshot, updated) {
		t.Fatalf("loaded state = %#v, want %#v", loaded.Snapshot, updated)
	}
}

type fixture struct {
	root       string
	home       string
	repository string
	config     string
	state      string
	lock       string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	fixture := fixture{
		root:       root,
		home:       filepath.Join(root, "home"),
		repository: filepath.Join(root, "repository"),
		config:     filepath.Join(root, "control", "machine.toml"),
		state:      filepath.Join(root, "control", "state.json"),
		lock:       filepath.Join(root, "control", "mutation.lock"),
	}
	for _, directory := range []string{fixture.home, fixture.repository} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	return fixture
}

func (fixture fixture) request(modules []config.Module) Request {
	return Request{
		Home: fixture.home,
		Controls: corepaths.Controls{
			Repository: fixture.repository,
			Config:     fixture.config,
			State:      fixture.state,
			Lock:       fixture.lock,
		},
		Modules: modules,
	}
}

func (fixture fixture) linkRequest(source, target string) Request {
	return fixture.request([]config.Module{{
		ID: "base",
		Links: []config.Link{{
			ID:         "file",
			SourcePath: source,
			Target:     target,
		}},
	}})
}

func (fixture fixture) localRequest(source, target string) Request {
	return fixture.request([]config.Module{{
		ID: "base",
		Locals: []config.Local{{
			ID:          "local",
			ExamplePath: source,
			Target:      target,
		}},
	}})
}

func (fixture fixture) writeRepositoryFile(t *testing.T, relative, contents string) string {
	t.Helper()
	path := filepath.Join(fixture.repository, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}

func (fixture fixture) writeState(t *testing.T, snapshot state.Snapshot) {
	t.Helper()
	if err := commitState(fixture.state, snapshot); err != nil {
		t.Fatalf("commitState() error = %v", err)
	}
}

func (fixture fixture) writeLinkState(t *testing.T, target, destination string) {
	t.Helper()
	relative, err := filepath.Rel(fixture.home, target)
	if err != nil {
		t.Fatalf("filepath.Rel() error = %v", err)
	}
	resolved, err := corepaths.ResolveTarget(
		fixture.home,
		"~/"+filepath.ToSlash(relative),
	)
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"base": {Placements: map[string]state.Placement{
				"file": {
					Kind:            state.KindLink,
					Target:          target,
					ResolvedTarget:  resolved.Resolved(),
					LinkDestination: destination,
				},
			}},
		},
	})
}

type fileSnapshot struct {
	path string
	info os.FileInfo
	data []byte
	link string
}

func snapshotFiles(t *testing.T, paths ...string) []fileSnapshot {
	t.Helper()
	result := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("os.Lstat(%q) error = %v", path, err)
		}
		item := fileSnapshot{path: path, info: info}
		if info.Mode()&os.ModeSymlink != 0 {
			item.link, err = os.Readlink(path)
		} else {
			item.data, err = os.ReadFile(path)
		}
		if err != nil {
			t.Fatalf("snapshot %q error = %v", path, err)
		}
		result = append(result, item)
	}
	return result
}

func snapshotExistingControlAndTargets(t *testing.T, fixture fixture) []fileSnapshot {
	t.Helper()
	paths := []string{fixture.state}
	for _, target := range []string{
		filepath.Join(fixture.home, ".file"),
		filepath.Join(fixture.home, ".local"),
	} {
		if _, err := os.Lstat(target); err == nil {
			paths = append(paths, target)
		}
	}
	return snapshotFiles(t, paths...)
}

func assertFilesUnchanged(t *testing.T, before []fileSnapshot) {
	t.Helper()
	for _, expected := range before {
		info, err := os.Lstat(expected.path)
		if err != nil {
			t.Fatalf("os.Lstat(%q) error = %v", expected.path, err)
		}
		if !os.SameFile(expected.info, info) ||
			!expected.info.ModTime().Equal(info.ModTime()) ||
			expected.info.Mode() != info.Mode() {
			t.Errorf("%q metadata changed", expected.path)
		}
		var data []byte
		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(expected.path)
		} else {
			data, err = os.ReadFile(expected.path)
		}
		if err != nil {
			t.Fatalf("read %q error = %v", expected.path, err)
		}
		if link != expected.link || !reflect.DeepEqual(data, expected.data) {
			t.Errorf("%q contents changed", expected.path)
		}
	}
}

func assertSymlink(t *testing.T, path, destination string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%q mode = %v, want symlink", path, info.Mode())
	}
	got, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("os.Readlink(%q) error = %v", path, err)
	}
	if got != destination {
		t.Fatalf("os.Readlink(%q) = %q, want %q", path, got, destination)
	}
}
