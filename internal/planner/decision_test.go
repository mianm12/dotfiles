package planner

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/state"
)

func TestOwned_M1Kinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		state    HistoricalState
		observed Observation
		want     bool
	}{
		{
			name:     "symlink raw destination matches",
			state:    HistoricalState{Kind: StateSymlink, LinkDest: "../repo/source"},
			observed: Observation{Kind: ObjectSymlink, LinkDest: "../repo/source"},
			want:     true,
		},
		{
			name:     "equivalent-looking destination does not match raw text",
			state:    HistoricalState{Kind: StateSymlink, LinkDest: "../repo/source"},
			observed: Observation{Kind: ObjectSymlink, LinkDest: "/repo/source"},
		},
		{
			name:     "regular object cannot satisfy symlink evidence",
			state:    HistoricalState{Kind: StateSymlink, LinkDest: "/repo/source"},
			observed: Observation{Kind: ObjectRegular, Hash: "sha256:abc"},
		},
		{
			name:     "scaffold never owns target",
			state:    HistoricalState{Kind: StateScaffold},
			observed: Observation{Kind: ObjectRegular, Hash: "sha256:abc"},
		},
		{
			name:     "unsupported historical kind is not ownership",
			state:    HistoricalState{Kind: StateKind("rendered")},
			observed: Observation{Kind: ObjectRegular, Hash: "sha256:abc"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := Owned(test.state, test.observed); got != test.want {
				t.Fatalf("Owned() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestDecide_LinkTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target ObservedTarget
		force  bool
		want   decisionWant
	}{
		{
			name:   "L1 missing creates link",
			target: linkTarget(Observation{Kind: ObjectMissing}, HistoricalState{}, false),
			want:   wantAction(FileCreateLink, FileReasonTargetMissing, StateUpsert),
		},
		{
			name: "L2 exact current link skips",
			target: linkTarget(
				Observation{Kind: ObjectSymlink, Mode: fs.ModeSymlink | 0o777, LinkDest: "/repo/modules/zsh/zshrc"},
				currentLinkState(),
				true,
			),
			want: wantAction(FileSkip, FileReasonExpectedLink, StatePreserve),
		},
		{
			name: "L2 exact link without state adopts",
			target: linkTarget(
				Observation{Kind: ObjectSymlink, Mode: fs.ModeSymlink | 0o777, LinkDest: "/repo/modules/zsh/zshrc"},
				HistoricalState{},
				false,
			),
			want: wantAction(FileAdopt, FileReasonStateMetadata, StateUpsert),
		},
		{
			name: "L2 stale metadata replaces historical alias",
			target: linkTarget(
				Observation{Kind: ObjectSymlink, Mode: fs.ModeSymlink | 0o777, LinkDest: "/repo/modules/zsh/zshrc"},
				HistoricalState{
					Key:       "~/.ZSHRC",
					Module:    "old-zsh",
					Kind:      StateSymlink,
					Source:    "modules/old-zsh/zshrc",
					LinkDest:  "/repo/modules/zsh/zshrc",
					AppliedAt: "2026-07-18T00:00:00Z",
				},
				true,
			),
			want: decisionWant{
				verb:        FileAdopt,
				reason:      FileReasonStateMetadata,
				success:     StateUpsert,
				previousKey: "~/.ZSHRC",
			},
		},
		{
			name: "L3 owned old link is relinked",
			target: linkTarget(
				Observation{Kind: ObjectSymlink, LinkDest: "/old/repo/zshrc"},
				HistoricalState{
					Key:      "~/.zshrc",
					Module:   "zsh",
					Kind:     StateSymlink,
					Source:   "modules/zsh/zshrc",
					LinkDest: "/old/repo/zshrc",
				},
				true,
			),
			want: wantAction(FileCreateLink, FileReasonOwnedLinkStale, StateUpsert),
		},
		{
			name: "L4 drifted recorded link conflicts",
			target: linkTarget(
				Observation{Kind: ObjectSymlink, LinkDest: "/user/changed"},
				currentLinkState(),
				true,
			),
			want: wantAction(FileConflict, FileReasonLinkDrift, StatePreserve),
		},
		{
			name: "L4 force backs up drifted link",
			target: linkTarget(
				Observation{Kind: ObjectSymlink, LinkDest: "/user/changed"},
				currentLinkState(),
				true,
			),
			force: true,
			want:  wantAction(FileBackupReplace, FileReasonLinkDrift, StateUpsert),
		},
		{
			name: "L5 unrecorded link conflicts",
			target: linkTarget(
				Observation{Kind: ObjectSymlink, LinkDest: "../equivalent-looking-source"},
				HistoricalState{},
				false,
			),
			want: wantAction(FileConflict, FileReasonUnownedLink, StatePreserve),
		},
		{
			name: "L5 force backs up unrecorded link",
			target: linkTarget(
				Observation{Kind: ObjectSymlink, LinkDest: "../other"},
				HistoricalState{},
				false,
			),
			force: true,
			want:  wantAction(FileBackupReplace, FileReasonUnownedLink, StateUpsert),
		},
		{
			name:   "L6 regular file conflicts",
			target: linkTarget(Observation{Kind: ObjectRegular, Mode: 0o644, Hash: "sha256:abc"}, HistoricalState{}, false),
			want:   wantAction(FileConflict, FileReasonRegularConflict, StatePreserve),
		},
		{
			name:   "L6 force backs up regular file",
			target: linkTarget(Observation{Kind: ObjectRegular, Mode: 0o644, Hash: "sha256:abc"}, HistoricalState{}, false),
			force:  true,
			want:   wantAction(FileBackupReplace, FileReasonRegularConflict, StateUpsert),
		},
		{
			name:   "L6 force still rejects directory",
			target: linkTarget(Observation{Kind: ObjectDirectory, Mode: fs.ModeDir | 0o755}, HistoricalState{}, false),
			force:  true,
			want:   wantAction(FileConflict, FileReasonDirectoryConflict, StatePreserve),
		},
		{
			name:   "L6 force still rejects special object",
			target: linkTarget(Observation{Kind: ObjectSpecial, Mode: fs.ModeNamedPipe | 0o600}, HistoricalState{}, false),
			force:  true,
			want:   wantAction(FileConflict, FileReasonSpecialConflict, StatePreserve),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			action, err := Decide(test.target, DecisionOptions{Force: test.force})
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			assertDecision(t, test.target, action, test.want)
		})
	}
}

func TestDecide_ScaffoldTable(t *testing.T) {
	t.Parallel()

	current := currentScaffoldState()
	tests := []struct {
		name   string
		target ObservedTarget
		force  bool
		want   decisionWant
	}{
		{
			name:   "S1a existing target with current record skips",
			target: scaffoldTarget(Observation{Kind: ObjectRegular, Mode: 0o600, Hash: "sha256:user"}, current, true),
			want:   wantAction(FileSkip, FileReasonScaffoldPresent, StatePreserve),
		},
		{
			name:   "S1a force does not replace existing target with current record",
			target: scaffoldTarget(Observation{Kind: ObjectRegular, Mode: 0o600, Hash: "sha256:user"}, current, true),
			force:  true,
			want:   wantAction(FileSkip, FileReasonScaffoldPresent, StatePreserve),
		},
		{
			name: "S1a existing target with stale metadata adopts",
			target: scaffoldTarget(
				Observation{Kind: ObjectDirectory, Mode: fs.ModeDir | 0o755},
				HistoricalState{Key: "~/.zshrc.local", Module: "old", Kind: StateScaffold, Source: "modules/old/local.template"},
				true,
			),
			want: wantAction(FileAdopt, FileReasonStateMetadata, StateUpsert),
		},
		{
			name: "S1a force only refreshes stale metadata without replacing target",
			target: scaffoldTarget(
				Observation{Kind: ObjectDirectory, Mode: fs.ModeDir | 0o755},
				HistoricalState{Key: "~/.zshrc.local", Module: "old", Kind: StateScaffold, Source: "modules/old/local.template"},
				true,
			),
			force: true,
			want:  wantAction(FileAdopt, FileReasonStateMetadata, StateUpsert),
		},
		{
			name:   "S1b regular target without record adopts",
			target: scaffoldTarget(Observation{Kind: ObjectRegular, Mode: 0o600, Hash: "sha256:user"}, HistoricalState{}, false),
			want:   wantAction(FileAdopt, FileReasonScaffoldPresent, StateUpsert),
		},
		{
			name:   "S1b symlink target without record adopts",
			target: scaffoldTarget(Observation{Kind: ObjectSymlink, LinkDest: "/user/link"}, HistoricalState{}, false),
			want:   wantAction(FileAdopt, FileReasonScaffoldPresent, StateUpsert),
		},
		{
			name:   "S1b directory target without record adopts",
			target: scaffoldTarget(Observation{Kind: ObjectDirectory, Mode: fs.ModeDir | 0o755}, HistoricalState{}, false),
			want:   wantAction(FileAdopt, FileReasonScaffoldPresent, StateUpsert),
		},
		{
			name:   "S1b special target without record adopts",
			target: scaffoldTarget(Observation{Kind: ObjectSpecial, Mode: fs.ModeNamedPipe | 0o600}, HistoricalState{}, false),
			want:   wantAction(FileAdopt, FileReasonScaffoldPresent, StateUpsert),
		},
		{
			name:   "S1b force still adopts existing target without replacing it",
			target: scaffoldTarget(Observation{Kind: ObjectSymlink, LinkDest: "/user/link"}, HistoricalState{}, false),
			force:  true,
			want:   wantAction(FileAdopt, FileReasonScaffoldPresent, StateUpsert),
		},
		{
			name:   "S2 deleted scaffold stays deleted",
			target: scaffoldTarget(Observation{Kind: ObjectMissing}, current, true),
			want:   wantAction(FileSkip, FileReasonScaffoldDeleted, StatePreserve),
		},
		{
			name: "S2 deleted scaffold refreshes stale metadata without rebuilding",
			target: scaffoldTarget(
				Observation{Kind: ObjectMissing},
				HistoricalState{Key: "~/.old-local", Module: "old", Kind: StateScaffold, Source: "modules/old/local.template"},
				true,
			),
			want: decisionWant{
				verb:        FileAdopt,
				reason:      FileReasonStateMetadata,
				success:     StateUpsert,
				previousKey: "~/.old-local",
			},
		},
		{
			name:   "S2 force rebuilds missing scaffold without backup",
			target: scaffoldTarget(Observation{Kind: ObjectMissing}, current, true),
			force:  true,
			want:   wantAction(FileScaffold, FileReasonScaffoldRebuild, StateUpsert),
		},
		{
			name: "S2 force rebuilds missing scaffold and refreshes stale metadata",
			target: scaffoldTarget(
				Observation{Kind: ObjectMissing},
				HistoricalState{Key: "~/.old-local", Module: "old", Kind: StateScaffold, Source: "modules/old/local.template"},
				true,
			),
			force: true,
			want: decisionWant{
				verb:        FileScaffold,
				reason:      FileReasonScaffoldRebuild,
				success:     StateUpsert,
				previousKey: "~/.old-local",
			},
		},
		{
			name:   "S3 first missing scaffold is created",
			target: scaffoldTarget(Observation{Kind: ObjectMissing}, HistoricalState{}, false),
			want:   wantAction(FileScaffold, FileReasonTargetMissing, StateUpsert),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			action, err := Decide(test.target, DecisionOptions{Force: test.force})
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			assertDecision(t, test.target, action, test.want)
		})
	}
}

func TestDecide_RejectsUnsupportedInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target ObservedTarget
	}{
		{
			name: "managed desired",
			target: ObservedTarget{
				Desired:  Desired{Kind: DesiredKind("managed"), Target: "~/.config", TargetPath: "/home/test/.config"},
				Observed: Observation{Kind: ObjectMissing},
			},
		},
		{
			name: "rendered history",
			target: ObservedTarget{
				Desired:  linkDesired(),
				Observed: Observation{Kind: ObjectRegular},
				State:    HistoricalState{Kind: StateKind("rendered")},
				HasState: true,
			},
		},
		{
			name: "unknown observation",
			target: ObservedTarget{
				Desired:  linkDesired(),
				Observed: Observation{Kind: ObjectKind("future")},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			action, err := Decide(test.target, DecisionOptions{})
			if !errors.Is(err, ErrUnsupportedDecisionInput) {
				t.Fatalf("Decide() error = %v, want ErrUnsupportedDecisionInput", err)
			}
			if !reflect.DeepEqual(action, FileAction{}) {
				t.Fatalf("Decide() action = %#v, want zero action", action)
			}
		})
	}
}

func TestDecide_M1KindMigration(t *testing.T) {
	t.Parallel()

	ownedSymlink := HistoricalState{
		Key:       "~/.zshrc.local",
		Module:    "zsh",
		Kind:      StateSymlink,
		Source:    "modules/zsh/old-local",
		LinkDest:  "/repo/modules/zsh/old-local",
		AppliedAt: "2026-07-18T00:00:00Z",
	}
	oldScaffold := HistoricalState{
		Key:       "~/.zshrc",
		Module:    "zsh",
		Kind:      StateScaffold,
		Source:    "modules/zsh/old-zshrc.template",
		AppliedAt: "2026-07-18T00:00:00Z",
	}
	tests := []struct {
		name   string
		target ObservedTarget
		force  bool
		want   decisionWant
	}{
		{
			name: "owned symlink migrates to independent scaffold",
			target: scaffoldTarget(
				Observation{Kind: ObjectSymlink, LinkDest: "/repo/modules/zsh/old-local"},
				ownedSymlink,
				true,
			),
			want: wantAction(FileScaffold, FileReasonOwnedLinkToScaffold, StateUpsert),
		},
		{
			name: "drifted symlink releases ownership without touching target",
			target: scaffoldTarget(
				Observation{Kind: ObjectSymlink, LinkDest: "/user/repointed"},
				ownedSymlink,
				true,
			),
			want: wantAction(FileAdopt, FileReasonReleaseOwnershipToScaffold, StateUpsert),
		},
		{
			name: "missing former symlink records deletion as scaffold lifecycle",
			target: scaffoldTarget(
				Observation{Kind: ObjectMissing},
				ownedSymlink,
				true,
			),
			want: wantAction(FileAdopt, FileReasonReleaseOwnershipToScaffold, StateUpsert),
		},
		{
			name: "force cannot replace non-owned object while migrating into scaffold",
			target: scaffoldTarget(
				Observation{Kind: ObjectRegular, Mode: 0o600, Hash: "sha256:user"},
				ownedSymlink,
				true,
			),
			force: true,
			want:  wantAction(FileAdopt, FileReasonReleaseOwnershipToScaffold, StateUpsert),
		},
		{
			name: "scaffold to link missing target follows L1 without-record semantics",
			target: linkTarget(
				Observation{Kind: ObjectMissing},
				oldScaffold,
				true,
			),
			want: wantAction(FileCreateLink, FileReasonTargetMissing, StateUpsert),
		},
		{
			name: "scaffold to exact link follows L2 automatic adoption",
			target: linkTarget(
				Observation{Kind: ObjectSymlink, LinkDest: "/repo/modules/zsh/zshrc"},
				oldScaffold,
				true,
			),
			want: wantAction(FileAdopt, FileReasonStateMetadata, StateUpsert),
		},
		{
			name: "scaffold to other link follows L5 conflict and preserves old record",
			target: linkTarget(
				Observation{Kind: ObjectSymlink, LinkDest: "/user/link"},
				oldScaffold,
				true,
			),
			want: wantAction(FileConflict, FileReasonUnownedLink, StatePreserve),
		},
		{
			name: "scaffold to regular link target needs explicit force",
			target: linkTarget(
				Observation{Kind: ObjectRegular, Mode: 0o644, Hash: "sha256:user"},
				oldScaffold,
				true,
			),
			want: wantAction(FileConflict, FileReasonRegularConflict, StatePreserve),
		},
		{
			name: "scaffold to regular link target force plans backup replace",
			target: linkTarget(
				Observation{Kind: ObjectRegular, Mode: 0o644, Hash: "sha256:user"},
				oldScaffold,
				true,
			),
			force: true,
			want:  wantAction(FileBackupReplace, FileReasonRegularConflict, StateUpsert),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			action, err := Decide(test.target, DecisionOptions{Force: test.force})
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			assertDecision(t, test.target, action, test.want)
		})
	}
}

func TestDecide_PreconditionRetainsPlanTimeTargetResolution(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := filepath.Join(home, "root")
	left := filepath.Join(root, "A")
	right := filepath.Join(root, "B")
	for _, directory := range []string{left, right} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", directory, err)
		}
	}
	sourcePath := filepath.Join(root, "source")
	if err := os.WriteFile(sourcePath, []byte("source"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}
	ancestor := filepath.Join(home, "current")
	if err := os.Symlink(left, ancestor); err != nil {
		t.Fatalf("Symlink(A) error = %v", err)
	}
	targetPath := filepath.Join(ancestor, "config")
	loaded, err := state.Load(filepath.Join(home, "missing-state.json"))
	if err != nil {
		t.Fatalf("state.Load(missing) error = %v", err)
	}
	profile, err := ObserveProfileTargets(home, []manifest.DesiredEntry{{
		Module:     "test",
		Source:     "source",
		SourcePath: sourcePath,
		Target:     "~/current/config",
		TargetPath: targetPath,
		Kind:       manifest.FileKindLink,
	}}, loaded)
	if err != nil {
		t.Fatalf("ObserveProfileTargets() error = %v", err)
	}
	targets := profile.Targets()
	if len(targets) != 1 {
		t.Fatalf("Targets() count = %d, want 1", len(targets))
	}
	action, err := Decide(targets[0], DecisionOptions{})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if err := os.Remove(ancestor); err != nil {
		t.Fatalf("Remove(ancestor symlink) error = %v", err)
	}
	if err := os.Symlink(right, ancestor); err != nil {
		t.Fatalf("Symlink(B) error = %v", err)
	}

	observedAfter, err := ObserveTarget(targetPath)
	if err != nil {
		t.Fatalf("ObserveTarget(after retarget) error = %v", err)
	}
	if !reflect.DeepEqual(observedAfter, action.Precondition.Observed) {
		t.Fatalf("leaf observation changed unexpectedly: got %#v, planned %#v", observedAfter, action.Precondition.Observed)
	}
	resolvedAfter, err := paths.ResolveTarget(targetPath)
	if err != nil {
		t.Fatalf("ResolveTarget(after retarget) error = %v", err)
	}
	if action.Precondition.TargetResolution.Equal(resolvedAfter) {
		t.Fatal("plan-time target resolution still matches after ancestor symlink retarget")
	}
}

type decisionWant struct {
	verb        FileVerb
	reason      FileReason
	success     StateEffectKind
	previousKey string
}

func wantAction(verb FileVerb, reason FileReason, success StateEffectKind) decisionWant {
	return decisionWant{verb: verb, reason: reason, success: success}
}

func assertDecision(t *testing.T, target ObservedTarget, action FileAction, want decisionWant) {
	t.Helper()
	if action.Verb != want.verb || action.Reason != want.reason {
		t.Fatalf("action verb/reason = %q/%q, want %q/%q", action.Verb, action.Reason, want.verb, want.reason)
	}
	if action.Target != target.Desired.Target || !reflect.DeepEqual(action.Desired, target.Desired) {
		t.Fatalf("action desired payload = %#v, want target desired %#v", action, target.Desired)
	}
	wantPrecondition := Precondition{
		TargetPath:       target.Desired.TargetPath,
		TargetResolution: target.Resolution,
		Observed:         target.Observed,
	}
	if action.Verb == FileCreateLink || action.Verb == FileBackupReplace {
		wantPrecondition.SourcePath = target.Desired.SourcePath
		wantPrecondition.RequireRegularSource = true
	}
	if !reflect.DeepEqual(action.Precondition, wantPrecondition) {
		t.Fatalf("action precondition = %#v, want %#v", action.Precondition, wantPrecondition)
	}
	if action.OnFailure.Kind != StatePreserve {
		t.Fatalf("failure state effect = %#v, want preserve", action.OnFailure)
	}
	if action.OnSuccess.Kind != want.success {
		t.Fatalf("success state effect = %#v, want kind %q", action.OnSuccess, want.success)
	}
	if want.success != StateUpsert {
		return
	}
	wantEntry := desiredHistoricalState(target.Desired)
	if action.OnSuccess.Key != target.Desired.Target ||
		action.OnSuccess.PreviousKey != want.previousKey ||
		!reflect.DeepEqual(action.OnSuccess.Entry, wantEntry) {
		t.Fatalf("success upsert = %#v, want key %q previous %q entry %#v", action.OnSuccess, target.Desired.Target, want.previousKey, wantEntry)
	}
}

func linkTarget(observed Observation, historical HistoricalState, hasState bool) ObservedTarget {
	return ObservedTarget{Desired: linkDesired(), Observed: observed, State: historical, HasState: hasState}
}

func scaffoldTarget(observed Observation, historical HistoricalState, hasState bool) ObservedTarget {
	return ObservedTarget{Desired: scaffoldDesired(), Observed: observed, State: historical, HasState: hasState}
}

func linkDesired() Desired {
	return Desired{
		Module:     "zsh",
		Source:     "zshrc",
		SourcePath: "/repo/modules/zsh/zshrc",
		Target:     "~/.zshrc",
		TargetPath: "/home/test/.zshrc",
		Kind:       DesiredLink,
	}
}

func scaffoldDesired() Desired {
	return Desired{
		Module:     "zsh",
		Source:     "zshrc.local.template",
		SourcePath: "/repo/modules/zsh/zshrc.local.template",
		Target:     "~/.zshrc.local",
		TargetPath: "/home/test/.zshrc.local",
		Kind:       DesiredScaffold,
		Mode:       0o600,
		Content:    []byte("initial\n"),
	}
}

func currentLinkState() HistoricalState {
	return HistoricalState{
		Key:       "~/.zshrc",
		Module:    "zsh",
		Kind:      StateSymlink,
		Source:    "modules/zsh/zshrc",
		LinkDest:  "/repo/modules/zsh/zshrc",
		AppliedAt: "2026-07-18T00:00:00Z",
	}
}

func currentScaffoldState() HistoricalState {
	return HistoricalState{
		Key:       "~/.zshrc.local",
		Module:    "zsh",
		Kind:      StateScaffold,
		Source:    "modules/zsh/zshrc.local.template",
		AppliedAt: "2026-07-18T00:00:00Z",
	}
}
