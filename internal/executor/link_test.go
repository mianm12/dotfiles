package executor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/planner"
)

func TestExecuteLink_CreateMissingWithAncestors(t *testing.T) {
	fixture := newLinkFixture(t)
	target := filepath.Join(fixture.home, ".config", "zsh", ".zshrc")
	action := fixture.planLink(t, target, fixture.source, planner.HistoricalState{}, false)
	if action.Verb != planner.FileCreateLink || action.Reason != planner.FileReasonTargetMissing {
		t.Fatalf("planned action = %q/%q, want L1 create-link", action.Verb, action.Reason)
	}

	result, err := ExecuteFile(fixture.control, action)
	if err != nil {
		t.Fatalf("ExecuteFile() error = %v", err)
	}
	if !result.TargetMutated {
		t.Fatal("ExecuteFile() TargetMutated = false, want true")
	}
	if result.StateEffect != action.OnSuccess {
		t.Fatalf("ExecuteFile() StateEffect = %#v, want %#v", result.StateEffect, action.OnSuccess)
	}
	assertLinkText(t, target, fixture.source)
	if info, statErr := os.Stat(filepath.Dir(target)); statErr != nil || !info.IsDir() {
		t.Fatalf("target parent Stat() = (%#v, %v), want directory", info, statErr)
	}
}

func TestExecuteLink_AdoptIsStateOnly(t *testing.T) {
	fixture := newLinkFixture(t)
	target := filepath.Join(fixture.home, ".zshrc")
	if err := os.Symlink(fixture.source, target); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	action := fixture.planLink(t, target, fixture.source, planner.HistoricalState{}, false)
	if action.Verb != planner.FileAdopt || action.Reason != planner.FileReasonStateMetadata {
		t.Fatalf("planned action = %q/%q, want L2 adopt", action.Verb, action.Reason)
	}
	before, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("os.Lstat() before error = %v", err)
	}

	result, err := ExecuteFile(fixture.control, action)
	if err != nil {
		t.Fatalf("ExecuteFile() error = %v", err)
	}
	if result.TargetMutated {
		t.Fatal("ExecuteFile() TargetMutated = true, want state-only adopt")
	}
	if result.StateEffect != action.OnSuccess {
		t.Fatalf("ExecuteFile() StateEffect = %#v, want %#v", result.StateEffect, action.OnSuccess)
	}
	after, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("os.Lstat() after error = %v", err)
	}
	if !os.SameFile(before, after) || before.Mode() != after.Mode() {
		t.Fatalf("adopt changed target identity/mode: before=%v after=%v", before.Mode(), after.Mode())
	}
	assertLinkText(t, target, fixture.source)
}

func TestExecuteLink_PreconditionFailuresPreserveTarget(t *testing.T) {
	t.Run("target appeared after L1 plan", func(t *testing.T) {
		fixture := newLinkFixture(t)
		target := filepath.Join(fixture.home, "appeared")
		action := fixture.planLink(t, target, fixture.source, planner.HistoricalState{}, false)
		if err := os.WriteFile(target, []byte("user data"), 0o600); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}

		result, err := ExecuteFile(fixture.control, action)
		assertPreconditionFailure(t, result, action, err)
		content, readErr := os.ReadFile(target)
		if readErr != nil || string(content) != "user data" {
			t.Fatalf("target after no-clobber = (%q, %v), want user data", content, readErr)
		}
	})

	t.Run("source is no longer regular", func(t *testing.T) {
		fixture := newLinkFixture(t)
		target := filepath.Join(fixture.home, "missing-source-target")
		action := fixture.planLink(t, target, fixture.source, planner.HistoricalState{}, false)
		if err := os.Remove(fixture.source); err != nil {
			t.Fatalf("os.Remove() error = %v", err)
		}
		if err := os.Symlink(filepath.Join(fixture.repo, "elsewhere"), fixture.source); err != nil {
			t.Fatalf("os.Symlink() source error = %v", err)
		}

		result, err := ExecuteFile(fixture.control, action)
		assertPreconditionFailure(t, result, action, err)
		assertMissing(t, target)
	})

	t.Run("ancestor symlink changed target identity", func(t *testing.T) {
		fixture := newLinkFixture(t)
		first := filepath.Join(fixture.root, "first")
		second := filepath.Join(fixture.root, "second")
		if err := os.Mkdir(first, 0o700); err != nil {
			t.Fatalf("os.Mkdir(first) error = %v", err)
		}
		if err := os.Mkdir(second, 0o700); err != nil {
			t.Fatalf("os.Mkdir(second) error = %v", err)
		}
		alias := filepath.Join(fixture.home, "alias")
		if err := os.Symlink(first, alias); err != nil {
			t.Fatalf("os.Symlink(first) error = %v", err)
		}
		target := filepath.Join(alias, "target")
		action := fixture.planLink(t, target, fixture.source, planner.HistoricalState{}, false)
		if err := os.Remove(alias); err != nil {
			t.Fatalf("os.Remove(alias) error = %v", err)
		}
		if err := os.Symlink(second, alias); err != nil {
			t.Fatalf("os.Symlink(second) error = %v", err)
		}

		result, err := ExecuteFile(fixture.control, action)
		assertPreconditionFailure(t, result, action, err)
		assertMissing(t, filepath.Join(first, "target"))
		assertMissing(t, filepath.Join(second, "target"))
	})

	t.Run("ancestor symlink enters control plane", func(t *testing.T) {
		fixture := newLinkFixture(t)
		realTarget := filepath.Join(fixture.root, "target-root")
		if err := os.Mkdir(realTarget, 0o700); err != nil {
			t.Fatalf("os.Mkdir(target root) error = %v", err)
		}
		if err := os.MkdirAll(fixture.control.StateRoot(), 0o700); err != nil {
			t.Fatalf("os.MkdirAll(state root) error = %v", err)
		}
		alias := filepath.Join(fixture.home, "control-alias")
		if err := os.Symlink(realTarget, alias); err != nil {
			t.Fatalf("os.Symlink(target root) error = %v", err)
		}
		target := filepath.Join(alias, "target")
		action := fixture.planLink(t, target, fixture.source, planner.HistoricalState{}, false)
		if err := os.Remove(alias); err != nil {
			t.Fatalf("os.Remove(alias) error = %v", err)
		}
		if err := os.Symlink(fixture.control.StateRoot(), alias); err != nil {
			t.Fatalf("os.Symlink(state root) error = %v", err)
		}

		result, err := ExecuteFile(fixture.control, action)
		assertPreconditionFailure(t, result, action, err)
		assertMissing(t, filepath.Join(realTarget, "target"))
		assertMissing(t, filepath.Join(fixture.control.StateRoot(), "target"))
	})
}

func TestExecuteLink_RelinkCommitsCompleteNewLink(t *testing.T) {
	fixture := newLinkFixture(t)
	target, oldSource, action := fixture.planRelink(t)

	result, err := ExecuteFile(fixture.control, action)
	if err != nil {
		t.Fatalf("ExecuteFile() error = %v", err)
	}
	if !result.TargetMutated || result.StateEffect != action.OnSuccess {
		t.Fatalf("ExecuteFile() result = %#v, want committed success %#v", result, action.OnSuccess)
	}
	assertLinkText(t, target, fixture.source)
	if got, readErr := os.ReadFile(oldSource); readErr != nil || string(got) != "old source" {
		t.Fatalf("old source after relink = (%q, %v), want unchanged", got, readErr)
	}
	assertNoExecutorTemps(t, filepath.Dir(target))
}

func TestExecuteLink_RelinkFailuresPreserveCommitBoundary(t *testing.T) {
	t.Run("temporary symlink preparation fails", func(t *testing.T) {
		fixture := newLinkFixture(t)
		target, oldSource, action := fixture.planRelink(t)
		operations := defaultFileOperations()
		injected := errors.New("prepare failed")
		operations.symlink = func(string, string) error { return injected }

		result, err := executeFile(fixture.control, action, operations)
		if !errors.Is(err, injected) {
			t.Fatalf("executeFile() error = %v, want injected prepare failure", err)
		}
		if result.TargetMutated || result.StateEffect != action.OnFailure {
			t.Fatalf("executeFile() result = %#v, want uncommitted failure", result)
		}
		assertLinkText(t, target, oldSource)
		assertNoExecutorTemps(t, filepath.Dir(target))
	})

	t.Run("target changes after preparation", func(t *testing.T) {
		fixture := newLinkFixture(t)
		target, _, action := fixture.planRelink(t)
		operations := defaultFileOperations()
		realSymlink := operations.symlink
		intruder := filepath.Join(fixture.root, "intruder")
		operations.symlink = func(oldname, newname string) error {
			if err := realSymlink(oldname, newname); err != nil {
				return err
			}
			if err := os.Remove(target); err != nil {
				return err
			}
			return os.Symlink(intruder, target)
		}

		result, err := executeFile(fixture.control, action, operations)
		assertPreconditionFailure(t, result, action, err)
		assertLinkText(t, target, intruder)
		assertNoExecutorTemps(t, filepath.Dir(target))
	})

	t.Run("rename fails", func(t *testing.T) {
		fixture := newLinkFixture(t)
		target, oldSource, action := fixture.planRelink(t)
		operations := defaultFileOperations()
		injected := errors.New("rename failed")
		operations.rename = func(string, string) error { return injected }

		result, err := executeFile(fixture.control, action, operations)
		if !errors.Is(err, injected) {
			t.Fatalf("executeFile() error = %v, want injected rename failure", err)
		}
		if result.TargetMutated || result.StateEffect != action.OnFailure {
			t.Fatalf("executeFile() result = %#v, want uncommitted failure", result)
		}
		assertLinkText(t, target, oldSource)
		assertNoExecutorTemps(t, filepath.Dir(target))
	})

	t.Run("post-commit cleanup failure keeps success effect", func(t *testing.T) {
		fixture := newLinkFixture(t)
		target, _, action := fixture.planRelink(t)
		operations := defaultFileOperations()
		realRemove := operations.remove
		injected := errors.New("cleanup failed")
		operations.remove = func(path string) error {
			if filepath.Base(path) == temporaryLinkName {
				return realRemove(path)
			}
			return injected
		}

		result, err := executeFile(fixture.control, action, operations)
		if !errors.Is(err, injected) {
			t.Fatalf("executeFile() error = %v, want cleanup failure", err)
		}
		if !result.TargetMutated || result.StateEffect != action.OnSuccess {
			t.Fatalf("executeFile() result = %#v, want committed success", result)
		}
		assertLinkText(t, target, fixture.source)
	})
}

type linkFixture struct {
	root    string
	home    string
	repo    string
	source  string
	control paths.ControlPlanePaths
}

func newLinkFixture(t *testing.T) linkFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	for _, directory := range []string{home, repo} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", directory, err)
		}
	}
	config := filepath.Join(home, ".config", "dot", "config.toml")
	control, err := paths.ResolveControlPlanePaths(home, repo, config)
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}
	source := filepath.Join(repo, "modules", "zsh", "zshrc")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(source parent) error = %v", err)
	}
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(source) error = %v", err)
	}
	return linkFixture{root: root, home: home, repo: repo, source: source, control: control}
}

func (fixture linkFixture) planLink(
	t *testing.T,
	target string,
	source string,
	historical planner.HistoricalState,
	hasState bool,
) planner.FileAction {
	t.Helper()
	resolution, err := paths.ResolveTarget(target)
	if err != nil {
		t.Fatalf("ResolveTarget(%q) error = %v", target, err)
	}
	observed, err := planner.ObserveTarget(target)
	if err != nil {
		t.Fatalf("ObserveTarget(%q) error = %v", target, err)
	}
	action, err := planner.Decide(planner.ObservedTarget{
		Desired: planner.Desired{
			Module:     "zsh",
			Source:     "zshrc",
			SourcePath: source,
			Target:     "~/" + filepath.ToSlash(mustRelative(t, fixture.home, target)),
			TargetPath: target,
			Kind:       planner.DesiredLink,
		},
		Resolution: resolution,
		Observed:   observed,
		State:      historical,
		HasState:   hasState,
	}, planner.DecisionOptions{})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	return action
}

func (fixture linkFixture) planRelink(t *testing.T) (string, string, planner.FileAction) {
	t.Helper()
	oldSource := filepath.Join(fixture.repo, "modules", "zsh", "old-zshrc")
	if err := os.WriteFile(oldSource, []byte("old source"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(old source) error = %v", err)
	}
	target := filepath.Join(fixture.home, ".zshrc")
	if err := os.Symlink(oldSource, target); err != nil {
		t.Fatalf("os.Symlink(old source) error = %v", err)
	}
	historical := planner.HistoricalState{
		Key:      "~/.zshrc",
		Module:   "zsh",
		Kind:     planner.StateSymlink,
		Source:   "modules/zsh/old-zshrc",
		LinkDest: oldSource,
	}
	action := fixture.planLink(t, target, fixture.source, historical, true)
	if action.Verb != planner.FileCreateLink || action.Reason != planner.FileReasonOwnedLinkStale {
		t.Fatalf("planned action = %q/%q, want L3 create-link", action.Verb, action.Reason)
	}
	return target, oldSource, action
}

func mustRelative(t *testing.T, base, target string) string {
	t.Helper()
	relative, err := filepath.Rel(base, target)
	if err != nil {
		t.Fatalf("filepath.Rel() error = %v", err)
	}
	return relative
}

func assertPreconditionFailure(
	t *testing.T,
	result FileResult,
	action planner.FileAction,
	err error,
) {
	t.Helper()
	if !errors.Is(err, ErrPrecondition) {
		t.Fatalf("ExecuteFile() error = %v, want ErrPrecondition", err)
	}
	if result.TargetMutated {
		t.Fatal("ExecuteFile() TargetMutated = true after failed Precondition")
	}
	if result.StateEffect != action.OnFailure {
		t.Fatalf("ExecuteFile() StateEffect = %#v, want failure %#v", result.StateEffect, action.OnFailure)
	}
}

func assertLinkText(t *testing.T, target, want string) {
	t.Helper()
	got, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("os.Readlink(%q) error = %v", target, err)
	}
	if got != want {
		t.Fatalf("os.Readlink(%q) = %q, want %q", target, got, want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("os.Lstat(%q) error = %v, want missing", path, err)
	}
}

func assertNoExecutorTemps(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("os.ReadDir(%q) error = %v", directory, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), temporaryDirectoryPrefix) {
			t.Fatalf("executor temporary entry remains: %q", entry.Name())
		}
	}
}
