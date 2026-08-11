package converge

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestExecutionCreationNeverClobbersChangedTargets(t *testing.T) {
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

func TestExecutionUpdateAndPruneRecheckRawDestination(t *testing.T) {
	for _, decision := range []Decision{
		DecisionUpdate,
		DecisionPrune,
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
			err = run.removeOwnedLink(Step{
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

func TestExecutionUpdateRechecksResolvedParent(t *testing.T) {
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
	err = run.removeOwnedLink(Step{
		Decision:                DecisionUpdate,
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

func TestVerifyAbsentRejectsReappearedTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := verifyAbsent(target); err != nil {
		t.Fatalf("verifyAbsent(missing) error = %v", err)
	}
	writeExecutorFile(t, target, "reappeared")
	if err := verifyAbsent(target); err == nil ||
		!strings.Contains(err.Error(), "reappeared") {
		t.Fatalf("verifyAbsent(reappeared) error = %v, want reappearance failure", err)
	}
	parent := filepath.Join(root, "not-a-directory")
	writeExecutorFile(t, parent, "file")
	if err := verifyAbsent(filepath.Join(parent, "child")); err == nil ||
		!strings.Contains(err.Error(), "re-read removed target") {
		t.Fatalf("verifyAbsent(unreadable) error = %v, want re-read failure", err)
	}
}

func TestStateCommitFailureLeavesRecoverableFacts(t *testing.T) {
	root, home := newExecutorRoot(t)
	repository := filepath.Join(root, "repository")
	source := filepath.Join(repository, "modules", "app", "config")
	writeExecutorFile(t, source, "config")
	target := filepath.Join(home, ".app")
	controlPaths := corepaths.Controls{
		Repository: repository,
		Config:     filepath.Join(home, ".config", "dot", "config.toml"),
		State:      filepath.Join(home, ".local", "state", "dot", "state.json"),
		Lock:       filepath.Join(home, ".local", "state", "dot", "lock"),
	}
	request := planRequest{
		Home:     home,
		Controls: resolveTestControls(t, controlPaths),
		Modules: []config.Module{{
			ID: "app",
			Links: []config.Link{{
				ID:         "config",
				SourcePath: source,
				Target:     "~/.app",
			}},
		}},
	}

	plan, loaded := prepareExecution(t, request)
	first, err := executePlan(request.Home, controlPaths.State, plan, loaded, func(string, state.Snapshot) error {
		return errors.New("injected state commit failure")
	})
	if err == nil ||
		!strings.Contains(err.Error(), "injected state commit failure") ||
		!strings.Contains(err.Error(), "partially applied") ||
		!first.TargetsChanged ||
		first.StateChanged {
		t.Fatalf("executePlan(failing commit) = (%#v, %v), want recoverable partial failure", first, err)
	}
	assertExecutorLink(t, target, source)
	if _, err := os.Lstat(controlPaths.State); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("state after failed commit error = %v, want missing", err)
	}

	plan, loaded = prepareExecution(t, request)
	second, err := executePlan(request.Home, controlPaths.State, plan, loaded, commitState)
	if err != nil ||
		second.TargetsChanged ||
		!second.StateChanged {
		t.Fatalf("executePlan(recovery) = (%#v, %v), want state-only recovery", second, err)
	}
	assertExecutorLink(t, target, source)
	beforeTarget := snapshotExecutorPath(t, target)
	beforeState := snapshotExecutorPath(t, controlPaths.State)

	plan, loaded = prepareExecution(t, request)
	third, err := executePlan(request.Home, controlPaths.State, plan, loaded, commitState)
	if err != nil || third.TargetsChanged || third.StateChanged {
		t.Fatalf("executePlan(repeat) = (%#v, %v), want zero mutation", third, err)
	}
	assertExecutorPathUnchanged(t, beforeTarget)
	assertExecutorPathUnchanged(t, beforeState)
}

func TestForgetCommitFailureDoesNotCompleteOwnershipRemoval(t *testing.T) {
	fixture := newFixture(t)
	target := filepath.Join(fixture.home, ".config", "app.local")
	writeExecutorFile(t, target, "personal")
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Records: map[state.Key]state.Record{
			{ModuleID: "app", PlacementID: "local"}: {
				Kind:   state.KindLocal,
				Target: target,
			},
		},
	})
	beforeTarget := snapshotExecutorPath(t, target)
	beforeState := snapshotExecutorPath(t, fixture.state)

	request := fixture.request(nil)
	plan, loaded := prepareExecution(t, request)
	result, err := executePlan(
		request.Home,
		fixture.state,
		plan,
		loaded,
		func(string, state.Snapshot) error {
			return errors.New("injected forget commit failure")
		},
	)

	if err == nil ||
		!strings.Contains(err.Error(), "injected forget commit failure") ||
		!strings.Contains(err.Error(), "state was not committed") ||
		!errors.Is(err, ErrPartial) {
		t.Fatalf("executePlan(failing forget commit) error = %v, want partial classification", err)
	}
	if result.TargetsChanged || result.StateChanged {
		t.Fatalf("executePlan(failing forget commit) result = %#v, want no completed change", result)
	}
	if len(plan.Steps) != 1 ||
		plan.Steps[0].Decision != DecisionForget ||
		plan.Steps[0].Reason == "" {
		t.Fatalf("buildPlan() = %#v, want structured forget", plan)
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
