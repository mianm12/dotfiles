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

func TestExecuteLink_CreateMissingWithAncestors(t *testing.T) {
	fixture := newLinkFixture(t)
	target := filepath.Join(fixture.home, ".config", "zsh", ".zshrc")
	action := fixture.planLink(t, target, fixture.source, planner.HistoricalState{}, false)
	if action.Verb != planner.FileCreateLink || action.Reason != planner.FileReasonTargetMissing {
		t.Fatalf("planned action = %q/%q, want create-link/target-missing", action.Verb, action.Reason)
	}

	result, err := ExecuteFile(fixture.control, action)
	if err != nil {
		t.Fatalf("ExecuteFile() error = %v", err)
	}
	if !result.TargetMutated || result.StateEffect != action.OnSuccess {
		t.Fatalf("ExecuteFile() result = %#v, want committed success", result)
	}
	assertLinkText(t, target, fixture.source)
}

func TestExecuteLink_AdoptIsStateOnly(t *testing.T) {
	fixture := newLinkFixture(t)
	target := filepath.Join(fixture.home, ".zshrc")
	if err := os.Symlink(fixture.source, target); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	action := fixture.planLink(t, target, fixture.source, planner.HistoricalState{}, false)
	if action.Verb != planner.FileAdopt {
		t.Fatalf("planned verb = %q, want adopt", action.Verb)
	}

	result, err := ExecuteFile(fixture.control, action)
	if err != nil {
		t.Fatalf("ExecuteFile() error = %v", err)
	}
	if result.TargetMutated || result.StateEffect != action.OnSuccess {
		t.Fatalf("ExecuteFile() result = %#v, want state-only success", result)
	}
	assertLinkText(t, target, fixture.source)
}

func TestExecuteLink_UnmanagedObjectsRemainConflicts(t *testing.T) {
	tests := []struct {
		name  string
		setup func(string) error
	}{
		{name: "regular", setup: func(path string) error {
			return os.WriteFile(path, []byte("user data"), 0o600)
		}},
		{name: "symlink", setup: func(path string) error {
			return os.Symlink("../user-owned", path)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLinkFixture(t)
			target := filepath.Join(fixture.home, ".zshrc")
			if err := test.setup(target); err != nil {
				t.Fatalf("setup target error = %v", err)
			}
			action := fixture.planLink(t, target, fixture.source, planner.HistoricalState{}, false)
			if action.Verb != planner.FileConflict {
				t.Fatalf("planned verb = %q, want conflict", action.Verb)
			}
			if err := ValidateFileAction(action); !errors.Is(err, ErrUnsupportedFileAction) {
				t.Fatalf("ValidateFileAction(conflict) error = %v, want unsupported execution", err)
			}
		})
	}
}

func TestExecuteLink_PreconditionMismatchPreservesAppearedTarget(t *testing.T) {
	fixture := newLinkFixture(t)
	target := filepath.Join(fixture.home, ".zshrc")
	action := fixture.planLink(t, target, fixture.source, planner.HistoricalState{}, false)
	if err := os.WriteFile(target, []byte("appeared"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(target) error = %v", err)
	}

	result, err := ExecuteFile(fixture.control, action)
	if !IsPurePreconditionMismatch(err) || result.TargetMutated || result.StateEffect != action.OnFailure {
		t.Fatalf("ExecuteFile() = (%#v, %v), want pure mismatch without mutation", result, err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil || string(content) != "appeared" {
		t.Fatalf("target = (%q, %v), want preserved", content, readErr)
	}
}

func TestExecuteLink_RelinksOwnedStaleLink(t *testing.T) {
	fixture := newLinkFixture(t)
	oldSource := filepath.Join(fixture.repo, "modules", "zsh", "old-zshrc")
	if err := os.WriteFile(oldSource, []byte("old"), 0o600); err != nil {
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
		t.Fatalf("planned action = %q/%q, want owned relink", action.Verb, action.Reason)
	}

	result, err := ExecuteFile(fixture.control, action)
	if err != nil || !result.TargetMutated || result.StateEffect != action.OnSuccess {
		t.Fatalf("ExecuteFile() = (%#v, %v), want committed relink", result, err)
	}
	assertLinkText(t, target, fixture.source)
}

func TestValidateFileAction_RejectsIncompleteLinkUpsert(t *testing.T) {
	fixture := newLinkFixture(t)
	action := fixture.planLink(t, filepath.Join(fixture.home, ".zshrc"), fixture.source, planner.HistoricalState{}, false)
	action.OnSuccess.Entry.Source = "modules/other/file"
	if err := ValidateFileAction(action); !errors.Is(err, ErrUnsupportedFileAction) {
		t.Fatalf("ValidateFileAction() error = %v, want ErrUnsupportedFileAction", err)
	}
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
	control, err := paths.ResolveControlPlanePaths(
		home,
		repo,
		filepath.Join(home, ".config", "dot", "config.toml"),
	)
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
	relative, err := filepath.Rel(fixture.home, target)
	if err != nil {
		t.Fatalf("filepath.Rel() error = %v", err)
	}
	action, err := planner.Decide(planner.ObservedTarget{
		Desired: planner.Desired{
			Module:     "zsh",
			Source:     "zshrc",
			SourcePath: source,
			Target:     "~/" + filepath.ToSlash(relative),
			TargetPath: target,
			Kind:       planner.DesiredLink,
		},
		Resolution: resolution,
		Observed:   observed,
		State:      historical,
		HasState:   hasState,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	return action
}

func assertLinkText(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("os.Readlink(%q) error = %v", path, err)
	}
	if got != want {
		t.Fatalf("link text = %q, want %q", got, want)
	}
}

func assertPreconditionFailure(
	t *testing.T,
	result FileResult,
	action planner.FileAction,
	err error,
) {
	t.Helper()
	if !errors.Is(err, ErrPrecondition) {
		t.Fatalf("error = %v, want ErrPrecondition", err)
	}
	if result.TargetMutated || result.StateEffect != action.OnFailure {
		t.Fatalf("result = %#v, want uncommitted failure effect", result)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("os.Lstat(%q) error = %v, want missing", path, err)
	}
}

func mustRelative(t *testing.T, base, target string) string {
	t.Helper()
	relative, err := filepath.Rel(base, target)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q) error = %v", base, target, err)
	}
	return relative
}
