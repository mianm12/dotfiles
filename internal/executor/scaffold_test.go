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
