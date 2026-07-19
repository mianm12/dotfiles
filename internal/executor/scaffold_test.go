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

func TestExecuteScaffold_CreatePublishesCompleteFile(t *testing.T) {
	fixture := newScaffoldFixture(t)
	target := filepath.Join(fixture.home, ".config", "app", "config")
	action := fixture.planScaffold(t, target, planner.HistoricalState{}, false, false)
	if action.Verb != planner.FileScaffold || action.Reason != planner.FileReasonTargetMissing {
		t.Fatalf("planned action = %q/%q, want S3 scaffold", action.Verb, action.Reason)
	}

	result, err := ExecuteFile(fixture.control, action)
	if err != nil {
		t.Fatalf("ExecuteFile() error = %v", err)
	}
	if !result.TargetMutated || result.StateEffect != action.OnSuccess {
		t.Fatalf("ExecuteFile() result = %#v, want committed success %#v", result, action.OnSuccess)
	}
	assertRegularFile(t, target, fixture.content, fixture.mode)
	assertNoScaffoldTemps(t, filepath.Dir(target))
}

func TestExecuteScaffold_CreateDoesNotClobberCommitRace(t *testing.T) {
	fixture := newScaffoldFixture(t)
	target := filepath.Join(fixture.home, "raced")
	action := fixture.planScaffold(t, target, planner.HistoricalState{}, false, false)
	operations := defaultFileOperations()
	realLink := operations.hardLink
	operations.hardLink = func(oldname, newname string) error {
		if err := os.WriteFile(newname, []byte("user data"), 0o600); err != nil {
			return err
		}
		return realLink(oldname, newname)
	}

	result, err := executeFile(fixture.control, action, operations)
	assertPreconditionFailure(t, result, action, err)
	content, readErr := os.ReadFile(target)
	if readErr != nil || string(content) != "user data" {
		t.Fatalf("target after commit race = (%q, %v), want user data", content, readErr)
	}
	assertNoScaffoldTemps(t, filepath.Dir(target))
}

func TestExecuteScaffold_AdoptIsStateOnly(t *testing.T) {
	fixture := newScaffoldFixture(t)
	target := filepath.Join(fixture.home, "existing")
	if err := os.WriteFile(target, []byte("user data"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	before, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("os.Lstat() before error = %v", err)
	}
	action := fixture.planScaffold(t, target, planner.HistoricalState{}, false, false)
	if action.Verb != planner.FileAdopt || action.Reason != planner.FileReasonScaffoldPresent {
		t.Fatalf("planned action = %q/%q, want S1b adopt", action.Verb, action.Reason)
	}

	result, err := ExecuteFile(fixture.control, action)
	if err != nil {
		t.Fatalf("ExecuteFile() error = %v", err)
	}
	if result.TargetMutated || result.StateEffect != action.OnSuccess {
		t.Fatalf("ExecuteFile() result = %#v, want state-only success %#v", result, action.OnSuccess)
	}
	after, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("os.Lstat() after error = %v", err)
	}
	if !os.SameFile(before, after) || before.Mode() != after.Mode() {
		t.Fatalf("adopt changed target identity/mode: before=%v after=%v", before.Mode(), after.Mode())
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "user data" {
		t.Fatalf("target after adopt = (%q, %v), want user data", content, err)
	}
}

func TestExecuteScaffold_SkipPreservesPresentAndDeletedTargets(t *testing.T) {
	t.Run("S1a present", func(t *testing.T) {
		fixture := newScaffoldFixture(t)
		target := filepath.Join(fixture.home, "present")
		if err := os.WriteFile(target, []byte("edited by user"), 0o640); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}
		action := fixture.planScaffold(t, target, fixture.currentState(t, target), true, false)
		if action.Verb != planner.FileSkip || action.Reason != planner.FileReasonScaffoldPresent {
			t.Fatalf("planned action = %q/%q, want S1a skip", action.Verb, action.Reason)
		}

		result, err := ExecuteFile(fixture.control, action)
		if err != nil {
			t.Fatalf("ExecuteFile() error = %v", err)
		}
		if result.TargetMutated || result.StateEffect != action.OnSuccess {
			t.Fatalf("ExecuteFile() result = %#v, want preserve", result)
		}
		content, readErr := os.ReadFile(target)
		if readErr != nil || string(content) != "edited by user" {
			t.Fatalf("target after skip = (%q, %v), want edited by user", content, readErr)
		}
	})

	t.Run("S2 deleted", func(t *testing.T) {
		fixture := newScaffoldFixture(t)
		target := filepath.Join(fixture.home, "deleted")
		action := fixture.planScaffold(t, target, fixture.currentState(t, target), true, false)
		if action.Verb != planner.FileSkip || action.Reason != planner.FileReasonScaffoldDeleted {
			t.Fatalf("planned action = %q/%q, want S2 skip", action.Verb, action.Reason)
		}

		result, err := ExecuteFile(fixture.control, action)
		if err != nil {
			t.Fatalf("ExecuteFile() error = %v", err)
		}
		if result.TargetMutated || result.StateEffect != action.OnSuccess {
			t.Fatalf("ExecuteFile() result = %#v, want preserve", result)
		}
		assertMissing(t, target)
	})
}

func TestExecuteScaffold_AdoptRefreshesMetadataWithoutMutation(t *testing.T) {
	for _, present := range []bool{true, false} {
		name := "deleted"
		if present {
			name = "present"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newScaffoldFixture(t)
			target := filepath.Join(fixture.home, name)
			if present {
				if err := os.WriteFile(target, []byte("user data"), 0o600); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
			}
			stale := fixture.currentState(t, target)
			stale.Module = "old"
			stale.Source = "modules/old/config.template"
			action := fixture.planScaffold(t, target, stale, true, false)
			if action.Verb != planner.FileAdopt || action.Reason != planner.FileReasonStateMetadata {
				t.Fatalf("planned action = %q/%q, want metadata adopt", action.Verb, action.Reason)
			}

			result, err := ExecuteFile(fixture.control, action)
			if err != nil {
				t.Fatalf("ExecuteFile() error = %v", err)
			}
			if result.TargetMutated || result.StateEffect != action.OnSuccess {
				t.Fatalf("ExecuteFile() result = %#v, want state-only success", result)
			}
			if present {
				content, readErr := os.ReadFile(target)
				if readErr != nil || string(content) != "user data" {
					t.Fatalf("target after metadata adopt = (%q, %v), want user data", content, readErr)
				}
			} else {
				assertMissing(t, target)
			}
		})
	}
}

func TestExecuteScaffold_RejectRebuildWithoutForceExecutor(t *testing.T) {
	fixture := newScaffoldFixture(t)
	target := filepath.Join(fixture.home, "deleted")
	action := fixture.planScaffold(t, target, fixture.currentState(t, target), true, true)
	if action.Verb != planner.FileScaffold || action.Reason != planner.FileReasonScaffoldRebuild {
		t.Fatalf("planned action = %q/%q, want force S2 rebuild", action.Verb, action.Reason)
	}

	result, err := ExecuteFile(fixture.control, action)
	if !errors.Is(err, ErrUnsupportedFileAction) {
		t.Fatalf("ExecuteFile() error = %v, want ErrUnsupportedFileAction", err)
	}
	if result.TargetMutated || result.StateEffect != action.OnFailure {
		t.Fatalf("ExecuteFile() result = %#v, want uncommitted failure", result)
	}
	assertMissing(t, target)
}

func TestExecuteScaffold_MigrationOwnedLinkToIndependentFile(t *testing.T) {
	fixture := newScaffoldFixture(t)
	target, oldSource, action := fixture.planOwnedLinkToScaffold(t)

	result, err := ExecuteFile(fixture.control, action)
	if err != nil {
		t.Fatalf("ExecuteFile() error = %v", err)
	}
	if !result.TargetMutated || result.StateEffect != action.OnSuccess {
		t.Fatalf("ExecuteFile() result = %#v, want committed success", result)
	}
	assertRegularFile(t, target, fixture.content, fixture.mode)
	oldInfo, err := os.Stat(oldSource)
	if err != nil {
		t.Fatalf("os.Stat(old source) error = %v", err)
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatalf("os.Stat(target) error = %v", err)
	}
	if os.SameFile(oldInfo, targetInfo) {
		t.Fatal("migrated scaffold still shares the old link source inode")
	}
	oldContent, err := os.ReadFile(oldSource)
	if err != nil || string(oldContent) != "old source" {
		t.Fatalf("old source after migration = (%q, %v), want unchanged", oldContent, err)
	}
	assertNoScaffoldTemps(t, filepath.Dir(target))
}

func TestExecuteScaffold_MigrationFailuresPreserveOwnedLink(t *testing.T) {
	t.Run("temporary file preparation fails", func(t *testing.T) {
		fixture := newScaffoldFixture(t)
		target, oldSource, action := fixture.planOwnedLinkToScaffold(t)
		operations := defaultFileOperations()
		injected := errors.New("prepare failed")
		operations.createTemp = func(string, string) (*os.File, error) { return nil, injected }

		result, err := executeFile(fixture.control, action, operations)
		if !errors.Is(err, injected) {
			t.Fatalf("executeFile() error = %v, want injected prepare failure", err)
		}
		if result.TargetMutated || result.StateEffect != action.OnFailure {
			t.Fatalf("executeFile() result = %#v, want uncommitted failure", result)
		}
		assertLinkText(t, target, oldSource)
		assertNoScaffoldTemps(t, filepath.Dir(target))
	})

	t.Run("target changes after preparation", func(t *testing.T) {
		fixture := newScaffoldFixture(t)
		target, _, action := fixture.planOwnedLinkToScaffold(t)
		operations := defaultFileOperations()
		realCreateTemp := operations.createTemp
		intruder := filepath.Join(fixture.root, "intruder")
		operations.createTemp = func(directory, pattern string) (*os.File, error) {
			file, err := realCreateTemp(directory, pattern)
			if err != nil {
				return nil, err
			}
			if err := os.Remove(target); err != nil {
				file.Close()
				return nil, err
			}
			if err := os.Symlink(intruder, target); err != nil {
				file.Close()
				return nil, err
			}
			return file, nil
		}

		result, err := executeFile(fixture.control, action, operations)
		assertPreconditionFailure(t, result, action, err)
		assertLinkText(t, target, intruder)
		assertNoScaffoldTemps(t, filepath.Dir(target))
	})

	t.Run("rename fails", func(t *testing.T) {
		fixture := newScaffoldFixture(t)
		target, oldSource, action := fixture.planOwnedLinkToScaffold(t)
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
		assertNoScaffoldTemps(t, filepath.Dir(target))
	})
}

func TestExecuteScaffold_MigrationReleaseOwnershipIsStateOnly(t *testing.T) {
	for _, targetPresent := range []bool{true, false} {
		name := "missing"
		if targetPresent {
			name = "nonowned"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newScaffoldFixture(t)
			target := filepath.Join(fixture.home, name)
			oldSource := filepath.Join(fixture.root, "old-source")
			if err := os.WriteFile(oldSource, []byte("old source"), 0o600); err != nil {
				t.Fatalf("os.WriteFile(old source) error = %v", err)
			}
			if targetPresent {
				if err := os.Symlink(filepath.Join(fixture.root, "intruder"), target); err != nil {
					t.Fatalf("os.Symlink(intruder) error = %v", err)
				}
			}
			historical := planner.HistoricalState{
				Key:      fixture.targetKey(t, target),
				Module:   "old",
				Kind:     planner.StateSymlink,
				Source:   "modules/old/config",
				LinkDest: oldSource,
			}
			action := fixture.planScaffold(t, target, historical, true, false)
			if action.Verb != planner.FileAdopt ||
				action.Reason != planner.FileReasonReleaseOwnershipToScaffold {
				t.Fatalf("planned action = %q/%q, want release-ownership adopt", action.Verb, action.Reason)
			}

			result, err := ExecuteFile(fixture.control, action)
			if err != nil {
				t.Fatalf("ExecuteFile() error = %v", err)
			}
			if result.TargetMutated || result.StateEffect != action.OnSuccess {
				t.Fatalf("ExecuteFile() result = %#v, want state-only success", result)
			}
			if targetPresent {
				assertLinkText(t, target, filepath.Join(fixture.root, "intruder"))
			} else {
				assertMissing(t, target)
			}
		})
	}
}

func TestExecuteScaffold_HardLinkIsolation(t *testing.T) {
	fixture := newScaffoldFixture(t)
	target := filepath.Join(fixture.home, "existing")
	sibling := filepath.Join(fixture.home, "sibling")
	if err := os.WriteFile(sibling, []byte("shared user data"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(sibling) error = %v", err)
	}
	if err := os.Link(sibling, target); err != nil {
		t.Fatalf("os.Link() error = %v", err)
	}
	beforeTarget, err := os.Stat(target)
	if err != nil {
		t.Fatalf("os.Stat(target) before error = %v", err)
	}
	beforeSibling, err := os.Stat(sibling)
	if err != nil {
		t.Fatalf("os.Stat(sibling) before error = %v", err)
	}
	if !os.SameFile(beforeTarget, beforeSibling) {
		t.Fatal("fixture target and sibling are not hard links")
	}
	action := fixture.planScaffold(t, target, planner.HistoricalState{}, false, false)

	result, err := ExecuteFile(fixture.control, action)
	if err != nil {
		t.Fatalf("ExecuteFile() error = %v", err)
	}
	if result.TargetMutated || result.StateEffect != action.OnSuccess {
		t.Fatalf("ExecuteFile() result = %#v, want state-only success", result)
	}
	afterTarget, err := os.Stat(target)
	if err != nil {
		t.Fatalf("os.Stat(target) after error = %v", err)
	}
	afterSibling, err := os.Stat(sibling)
	if err != nil {
		t.Fatalf("os.Stat(sibling) after error = %v", err)
	}
	if !os.SameFile(beforeTarget, afterTarget) || !os.SameFile(afterTarget, afterSibling) ||
		beforeTarget.Mode() != afterTarget.Mode() || beforeSibling.Mode() != afterSibling.Mode() {
		t.Fatalf("adopt changed hard-linked inode or mode: before=%v after=%v", beforeTarget.Mode(), afterTarget.Mode())
	}
	for _, path := range []string{target, sibling} {
		content, readErr := os.ReadFile(path)
		if readErr != nil || string(content) != "shared user data" {
			t.Fatalf("%s after adopt = (%q, %v), want shared user data", path, content, readErr)
		}
	}
}

func TestExecuteScaffold_MigrationToLinkUsesNoRecordSemantics(t *testing.T) {
	t.Run("missing target creates link", func(t *testing.T) {
		fixture := newScaffoldFixture(t)
		target := filepath.Join(fixture.home, "missing-link")
		action := fixture.planLink(t, target, fixture.source, fixture.currentState(t, target), true)
		if action.Verb != planner.FileCreateLink || action.Reason != planner.FileReasonTargetMissing {
			t.Fatalf("planned action = %q/%q, want no-record L1", action.Verb, action.Reason)
		}
		result, err := ExecuteFile(fixture.control, action)
		if err != nil {
			t.Fatalf("ExecuteFile() error = %v", err)
		}
		if !result.TargetMutated || result.StateEffect != action.OnSuccess {
			t.Fatalf("ExecuteFile() result = %#v, want committed link", result)
		}
		assertLinkText(t, target, fixture.source)
	})

	t.Run("expected link is adopted", func(t *testing.T) {
		fixture := newScaffoldFixture(t)
		target := filepath.Join(fixture.home, "expected-link")
		if err := os.Symlink(fixture.source, target); err != nil {
			t.Fatalf("os.Symlink() error = %v", err)
		}
		action := fixture.planLink(t, target, fixture.source, fixture.currentState(t, target), true)
		if action.Verb != planner.FileAdopt || action.Reason != planner.FileReasonStateMetadata {
			t.Fatalf("planned action = %q/%q, want no-record L2 adopt", action.Verb, action.Reason)
		}
		result, err := ExecuteFile(fixture.control, action)
		if err != nil {
			t.Fatalf("ExecuteFile() error = %v", err)
		}
		if result.TargetMutated || result.StateEffect != action.OnSuccess {
			t.Fatalf("ExecuteFile() result = %#v, want state-only link adopt", result)
		}
		assertLinkText(t, target, fixture.source)
	})

	t.Run("regular target remains conflict", func(t *testing.T) {
		fixture := newScaffoldFixture(t)
		target := filepath.Join(fixture.home, "regular-conflict")
		if err := os.WriteFile(target, []byte("user data"), 0o600); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}
		action := fixture.planLink(t, target, fixture.source, fixture.currentState(t, target), true)
		if action.Verb != planner.FileConflict || action.Reason != planner.FileReasonRegularConflict {
			t.Fatalf("planned action = %q/%q, want no-record L6 conflict", action.Verb, action.Reason)
		}
		result, err := ExecuteFile(fixture.control, action)
		if !errors.Is(err, ErrUnsupportedFileAction) {
			t.Fatalf("ExecuteFile() error = %v, want unsupported conflict", err)
		}
		if result.TargetMutated || result.StateEffect != action.OnFailure {
			t.Fatalf("ExecuteFile() result = %#v, want preserved conflict", result)
		}
		content, readErr := os.ReadFile(target)
		if readErr != nil || string(content) != "user data" {
			t.Fatalf("target after conflict = (%q, %v), want user data", content, readErr)
		}
	})
}

type scaffoldFixture struct {
	linkFixture
	content []byte
	mode    fs.FileMode
}

func newScaffoldFixture(t *testing.T) scaffoldFixture {
	t.Helper()
	return scaffoldFixture{
		linkFixture: newLinkFixture(t),
		content:     []byte("rendered scaffold\n"),
		mode:        0o640,
	}
}

func (fixture scaffoldFixture) planScaffold(
	t *testing.T,
	target string,
	historical planner.HistoricalState,
	hasState bool,
	force bool,
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
			Module:     "app",
			Source:     "config.template",
			SourcePath: filepath.Join(fixture.repo, "modules", "app", "config.template"),
			Target:     fixture.targetKey(t, target),
			TargetPath: target,
			Kind:       planner.DesiredScaffold,
			Mode:       fixture.mode,
			Content:    fixture.content,
		},
		Resolution: resolution,
		Observed:   observed,
		State:      historical,
		HasState:   hasState,
	}, planner.DecisionOptions{Force: force})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	return action
}

func (fixture scaffoldFixture) currentState(t *testing.T, target string) planner.HistoricalState {
	t.Helper()
	return planner.HistoricalState{
		Key:    fixture.targetKey(t, target),
		Module: "app",
		Kind:   planner.StateScaffold,
		Source: "modules/app/config.template",
	}
}

func (fixture scaffoldFixture) planOwnedLinkToScaffold(
	t *testing.T,
) (string, string, planner.FileAction) {
	t.Helper()
	oldSource := filepath.Join(fixture.root, "old-source")
	if err := os.WriteFile(oldSource, []byte("old source"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(old source) error = %v", err)
	}
	target := filepath.Join(fixture.home, "migrated")
	if err := os.Symlink(oldSource, target); err != nil {
		t.Fatalf("os.Symlink(old source) error = %v", err)
	}
	historical := planner.HistoricalState{
		Key:      fixture.targetKey(t, target),
		Module:   "old",
		Kind:     planner.StateSymlink,
		Source:   "modules/old/config",
		LinkDest: oldSource,
	}
	action := fixture.planScaffold(t, target, historical, true, false)
	if action.Verb != planner.FileScaffold || action.Reason != planner.FileReasonOwnedLinkToScaffold {
		t.Fatalf("planned action = %q/%q, want owned link-to-scaffold", action.Verb, action.Reason)
	}
	return target, oldSource, action
}

func (fixture scaffoldFixture) targetKey(t *testing.T, target string) string {
	t.Helper()
	return "~/" + filepath.ToSlash(mustRelative(t, fixture.home, target))
}

func assertRegularFile(t *testing.T, target string, wantContent []byte, wantMode fs.FileMode) {
	t.Helper()
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", target, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != wantMode.Perm() {
		t.Fatalf("target mode = %v, want regular %v", info.Mode(), wantMode.Perm())
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", target, err)
	}
	if string(content) != string(wantContent) {
		t.Fatalf("target content = %q, want %q", content, wantContent)
	}
}

func assertNoScaffoldTemps(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("os.ReadDir(%q) error = %v", directory, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), scaffoldTemporaryPrefix) {
			t.Fatalf("scaffold temporary entry remains: %q", entry.Name())
		}
	}
}
