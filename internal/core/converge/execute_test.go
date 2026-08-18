package converge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestCreateLinkDoesNotClobberExistingFile(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(home) error = %v", err)
	}
	target := filepath.Join(home, ".link")
	if err := os.WriteFile(target, []byte("user"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	run := mutationRun{}
	err := run.createLink(filepath.Join(home, "desired"), target)
	if err == nil || !strings.Contains(err.Error(), "create symlink") {
		t.Fatalf("createLink() error = %v, want no-clobber", err)
	}
}

func TestRemoveOwnedLinkRejectsDestinationDrift(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(home) error = %v", err)
	}
	target := filepath.Join(home, ".owned")
	if err := os.Symlink(filepath.Join(root, "changed"), target); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	run := mutationRun{}
	err := run.removeOwnedLink(target, filepath.Join(root, "expected"))
	if err == nil || !strings.Contains(err.Error(), "destination changed") {
		t.Fatalf("removeOwnedLink() error = %v, want dest drift", err)
	}
}

func TestUnknownLineDoesNotMutate(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(home) error = %v", err)
	}
	ownership, err := state.New(home)
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	run := mutationRun{}
	_, _, err = run.apply([]loopLine{{Line: Line{Op: Op("unknown"), Target: filepath.Join(home, "x")}}}, &ownership)
	if err == nil {
		t.Fatal("apply(unknown) error = nil")
	}
	var failure *Failure
	if !errors.As(err, &failure) || failure.Line == nil || failure.Line.Op != Op("unknown") {
		t.Fatalf("failure = %#v, want unknown line", failure)
	}
}

func TestExecuteLinesRejectsSkipBeforeAnyMutation(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(home) error = %v", err)
	}
	ownership, err := state.New(home)
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	target := filepath.Join(home, ".app")
	committed := false
	result, err := executeLines(
		filepath.Join(root, "state.json"),
		[]loopLine{
			{
				Line: Line{
					Op:          OpLink,
					ModuleID:    "app",
					PlacementID: "config",
					Target:      target,
				},
				dest:     filepath.Join(root, "source"),
				targetID: ".app",
			},
			{Line: Line{
				Op:          OpSkip,
				ModuleID:    "blocked",
				PlacementID: "config",
				Target:      filepath.Join(home, ".blocked"),
			}},
		},
		state.Loaded{Snapshot: ownership},
		func(string, state.Snapshot) (bool, error) {
			committed = true
			return true, nil
		},
	)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Line == nil || failure.Line.Op != OpSkip ||
		failure.MayHaveChanged || failure.Cause == nil ||
		failure.Cause.Error() != "internal invariant: executor received skip line" {
		t.Fatalf("executeLines(skip) error = %#v, want unchanged skip invariant failure", err)
	}
	if len(result.Done) != 0 || result.TargetsChanged || result.StateChanged || result.ControlsChanged {
		t.Fatalf("executeLines(skip) result = %#v, want zero result", result)
	}
	if committed {
		t.Fatal("executeLines(skip) called state committer")
	}
	if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target error = %v, want no mutation", statErr)
	}
	if len(ownership.Links) != 0 {
		t.Fatalf("input ownership = %#v, want unchanged", ownership)
	}
}

func TestStateCommitFailureReportsOnlyCompletedDiskLines(t *testing.T) {
	commitErr := errors.New("injected state commit failure")
	for _, test := range []struct {
		name           string
		line           func(*testing.T, string, string) loopLine
		seedOwnership  bool
		wantDone       []Op
		wantDiskChange bool
	}{
		{
			name: "link remains completed",
			line: func(_ *testing.T, target, destination string) loopLine {
				return loopLine{
					Line: Line{Op: OpLink, ModuleID: "app", PlacementID: "config", Target: target},
					dest: destination, targetID: ".app",
				}
			},
			wantDone:       []Op{OpLink},
			wantDiskChange: true,
		},
		{
			name: "record is not completed",
			line: func(t *testing.T, target, destination string) loopLine {
				if err := os.Symlink(destination, target); err != nil {
					t.Fatalf("os.Symlink(record) error = %v", err)
				}
				return loopLine{
					Line: Line{Op: OpRecord, ModuleID: "app", PlacementID: "config", Target: target},
					dest: destination, targetID: ".app",
				}
			},
		},
		{
			name:          "remove remains completed",
			seedOwnership: true,
			line: func(t *testing.T, target, destination string) loopLine {
				if err := os.Symlink(destination, target); err != nil {
					t.Fatalf("os.Symlink(remove) error = %v", err)
				}
				return loopLine{
					Line:       Line{Op: OpRemove, ModuleID: "app", PlacementID: "config", Target: target},
					beforeDest: destination, targetID: ".app",
				}
			},
			wantDone:       []Op{OpRemove},
			wantDiskChange: true,
		},
		{
			name:          "forget is not completed",
			seedOwnership: true,
			line: func(_ *testing.T, target, destination string) loopLine {
				return loopLine{
					Line:       Line{Op: OpForget, ModuleID: "app", PlacementID: "config", Target: target},
					beforeDest: destination, targetID: ".app",
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatalf("os.Mkdir(home) error = %v", err)
			}
			target := filepath.Join(home, ".app")
			destination := filepath.Join(root, "source")
			ownership, err := state.New(home)
			if err != nil {
				t.Fatalf("state.New() error = %v", err)
			}
			if test.seedOwnership {
				ownership.Links[state.Key{ModuleID: "app", PlacementID: "config"}] = state.LinkRecord{
					Target: ".app",
					Dest:   destination,
				}
			}
			line := test.line(t, target, destination)
			result, err := executeLines(
				filepath.Join(root, "state.json"),
				[]loopLine{line},
				state.Loaded{Snapshot: ownership},
				func(string, state.Snapshot) (bool, error) { return false, commitErr },
			)
			if !errors.Is(err, commitErr) {
				t.Fatalf("executeLines() error = %v, want commit failure", err)
			}
			assertLineOps(t, result.Done, test.wantDone...)
			if result.TargetsChanged != test.wantDiskChange || result.StateChanged {
				t.Fatalf("executeLines() result = %#v, want disk change=%t and no state change", result, test.wantDiskChange)
			}
			if FailureMayHaveChanged(err) != test.wantDiskChange {
				t.Fatalf("FailureMayHaveChanged() = %t, want %t", FailureMayHaveChanged(err), test.wantDiskChange)
			}
		})
	}
}

func TestStatePublishCleanupFailureDoesNotCompleteStateOnlyLine(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(home) error = %v", err)
	}
	ownership, err := state.New(home)
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	cleanupErr := errors.New("cleanup after publish")
	line := loopLine{
		Line:     Line{Op: OpRecord, ModuleID: "app", PlacementID: "config", Target: filepath.Join(home, ".app")},
		dest:     filepath.Join(root, "source"),
		targetID: ".app",
	}
	result, err := executeLines(
		filepath.Join(root, "state.json"),
		[]loopLine{line},
		state.Loaded{Snapshot: ownership},
		func(string, state.Snapshot) (bool, error) { return true, cleanupErr },
	)
	if !errors.Is(err, cleanupErr) || !result.StateChanged || !FailureMayHaveChanged(err) {
		t.Fatalf("executeLines() = (%#v, %v), want published state with cleanup failure", result, err)
	}
	assertLineOps(t, result.Done)
}

func TestLocalCleanupFailureKeepsCompletedFileLine(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(home) error = %v", err)
	}
	source := filepath.Join(root, "example")
	if err := os.WriteFile(source, []byte("local contents"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(source) error = %v", err)
	}
	target := filepath.Join(home, ".app")
	ownership, err := state.New(home)
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	cleanupErr := errors.New("injected cleanup after publish")
	run := mutationRun{
		removeTemporary: func(path string) error {
			if err := os.Remove(path); err != nil {
				return err
			}
			return cleanupErr
		},
	}
	done, stateOnly, err := run.apply([]loopLine{{
		Line: Line{
			Op:          OpFile,
			ModuleID:    "app",
			PlacementID: "local",
			Target:      target,
		},
		source: source,
	}}, &ownership)
	if !errors.Is(err, cleanupErr) || !FailureMayHaveChanged(err) {
		t.Fatalf("apply(file) error = %v, want changed cleanup failure", err)
	}
	assertLineOps(t, done, OpFile)
	assertLineOps(t, stateOnly)
	if !run.targetsChanged {
		t.Fatal("targetsChanged = false, want published local file")
	}
	contents, readErr := os.ReadFile(target)
	if readErr != nil || string(contents) != "local contents" {
		t.Fatalf("published local file = (%q, %v)", contents, readErr)
	}
}

func TestReplaceAndRemoveRecheckRawDestinationBeforeMutation(t *testing.T) {
	for _, op := range []Op{OpReplace, OpRemove} {
		t.Run(string(op), func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatalf("os.Mkdir(home) error = %v", err)
			}
			target := filepath.Join(home, ".app")
			changedDestination := filepath.Join(root, "changed")
			if err := os.Symlink(changedDestination, target); err != nil {
				t.Fatalf("os.Symlink(changed) error = %v", err)
			}
			ownership, err := state.New(home)
			if err != nil {
				t.Fatalf("state.New() error = %v", err)
			}
			line := loopLine{
				Line:       Line{Op: op, ModuleID: "app", PlacementID: "config", Target: target},
				dest:       filepath.Join(root, "new"),
				beforeDest: filepath.Join(root, "expected"),
				targetID:   ".app",
			}
			done, stateOnly, err := (&mutationRun{}).apply([]loopLine{line}, &ownership)
			if err == nil || !strings.Contains(err.Error(), "destination changed") {
				t.Fatalf("apply(%s) error = %v, want raw destination drift", op, err)
			}
			assertLineOps(t, done)
			assertLineOps(t, stateOnly)
			if destination, err := os.Readlink(target); err != nil || destination != changedDestination {
				t.Fatalf("target after %s = (%q, %v), want preserved", op, destination, err)
			}
		})
	}
}

func TestFailureFormatsControlLineWithoutPlacementFields(t *testing.T) {
	err := (&Failure{
		Line: &Line{
			Op:      OpChmod,
			Control: "state-root",
			Path:    "/tmp/state",
			Mode:    "0700",
		},
		Cause: errors.New("chmod failed"),
	}).Error()
	if !strings.Contains(err, `line=chmod control=state-root path="/tmp/state" mode=0700`) ||
		strings.Contains(err, "module=") || strings.Contains(err, "placement=") {
		t.Fatalf("Failure.Error() = %q, want control-shaped line", err)
	}
}
