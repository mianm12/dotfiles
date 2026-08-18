package converge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestLoopCreatesLinkAndRecordsThenIsSilent(t *testing.T) {
	fixture := newLoopFixture(t)
	source := fixture.file("repo/modules/app/config", "data")
	module := linkModule("app", "config", source, "~/.app")
	first := fixture.build(t, []config.Module{module}, fixture.emptyState())
	assertOps(t, first, OpLink)

	if err := os.Symlink(source, fixture.target(".app")); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	owned := fixture.emptyState()
	owned.Links[state.Key{ModuleID: "app", PlacementID: "config"}] = state.LinkRecord{
		Target: ".app",
		Dest:   source,
	}
	second := fixture.build(t, []config.Module{module}, owned)
	assertOps(t, second)
}

func TestLoopRecordsExistingCorrectLink(t *testing.T) {
	fixture := newLoopFixture(t)
	source := fixture.file("repo/modules/app/config", "data")
	if err := os.Symlink(source, fixture.target(".app")); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	lines := fixture.build(t, []config.Module{linkModule("app", "config", source, "~/.app")}, fixture.emptyState())
	assertOps(t, lines, OpRecord)
}

func TestLoopSkipsRegularFileAndNestedDesired(t *testing.T) {
	fixture := newLoopFixture(t)
	source := fixture.file("repo/modules/app/config", "data")
	writeLoopFile(t, fixture.target(".app"), "personal")
	blocked := fixture.build(t, []config.Module{linkModule("app", "config", source, "~/.app")}, fixture.emptyState())
	assertOps(t, blocked, OpSkip)

	nested := fixture.build(t, []config.Module{
		linkModule("app", "parent", source, "~/.config/app"),
		linkModule("app", "child", source, "~/.config/app/child"),
	}, fixture.emptyState())
	assertOps(t, nested, OpSkip, OpSkip)
}

func TestLoopMovesCreateThenRemoveWhenNotNested(t *testing.T) {
	fixture := newLoopFixture(t)
	source := fixture.file("repo/modules/app/config", "data")
	old := fixture.target(".old")
	if err := os.Symlink(source, old); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	owned := fixture.emptyState()
	owned.Links[state.Key{ModuleID: "app", PlacementID: "config"}] = state.LinkRecord{
		Target: ".old",
		Dest:   source,
	}
	lines := fixture.build(t, []config.Module{linkModule("app", "config", source, "~/.new")}, owned)
	assertOps(t, lines, OpLink, OpRemove)
}

func TestLoopForgetsDriftedStaleLink(t *testing.T) {
	fixture := newLoopFixture(t)
	target := fixture.target(".stale")
	if err := os.Symlink(filepath.Join(fixture.root, "other"), target); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	owned := fixture.emptyState()
	owned.Links[state.Key{ModuleID: "app", PlacementID: "old"}] = state.LinkRecord{
		Target: ".stale",
		Dest:   filepath.Join(fixture.root, "original"),
	}
	lines := fixture.build(t, nil, owned)
	assertOps(t, lines, OpForget)
	if lines[0].Reason == "" {
		t.Fatal("forget line must include a reason")
	}
}

func TestLoopLocalAbsentCreatesExistingIsSilent(t *testing.T) {
	fixture := newLoopFixture(t)
	example := fixture.file("repo/modules/app/local.example", "example")
	module := config.Module{
		ID: "app",
		Locals: []config.Local{{
			ID:          "local",
			ExamplePath: example,
			Target:      mustTestTarget("~/.local"),
		}},
	}
	first := fixture.build(t, []config.Module{module}, fixture.emptyState())
	assertOps(t, first, OpFile)
	writeLoopFile(t, fixture.target(".local"), "user")
	second := fixture.build(t, []config.Module{module}, fixture.emptyState())
	assertOps(t, second)
}

func TestLoopLocalUnreachableIsSkip(t *testing.T) {
	fixture := newLoopFixture(t)
	example := fixture.file("repo/modules/app/local.example", "example")
	writeLoopFile(t, fixture.target(".blocked"), "not a directory")
	module := config.Module{
		ID: "app",
		Locals: []config.Local{{
			ID:          "local",
			ExamplePath: example,
			Target:      mustTestTarget("~/.blocked/local"),
		}},
	}

	lines := fixture.build(t, []config.Module{module}, fixture.emptyState())
	assertOps(t, lines, OpSkip)
	if !strings.Contains(lines[0].Reason, "ancestor is not a directory") {
		t.Fatalf("local skip reason = %q, want unreachable ancestor", lines[0].Reason)
	}
}

func TestLoopIncompleteModuleStateIsNotStale(t *testing.T) {
	t.Run("matching owned link stays silent", func(t *testing.T) {
		fixture := newLoopFixture(t)
		destination := filepath.Join(fixture.root, "blocked-source")
		if err := os.Symlink(destination, fixture.target(".blocked")); err != nil {
			t.Fatalf("os.Symlink(blocked) error = %v", err)
		}
		owned := fixture.emptyState()
		owned.Links[state.Key{ModuleID: "blocked", PlacementID: "config"}] = state.LinkRecord{
			Target: ".blocked",
			Dest:   destination,
		}
		source := fixture.file("repo/modules/ready/config", "ready")

		lines, err := buildLines(loopRequest{
			Home:              fixture.home,
			Controls:          fixture.controls,
			Modules:           []config.Module{linkModule("ready", "config", source, "~/.ready")},
			State:             owned,
			IncompleteModules: map[string]struct{}{"blocked": {}},
		})
		if err != nil {
			t.Fatalf("buildLines() error = %v", err)
		}
		assertOps(t, lines, OpLink)
	})

	t.Run("owned target still blocks another desired", func(t *testing.T) {
		fixture := newLoopFixture(t)
		destination := filepath.Join(fixture.root, "blocked-source")
		owned := fixture.emptyState()
		owned.Links[state.Key{ModuleID: "blocked", PlacementID: "config"}] = state.LinkRecord{
			Target: ".shared",
			Dest:   destination,
		}
		source := fixture.file("repo/modules/ready/config", "ready")

		lines, err := buildLines(loopRequest{
			Home:              fixture.home,
			Controls:          fixture.controls,
			Modules:           []config.Module{linkModule("ready", "config", source, "~/.shared")},
			State:             owned,
			IncompleteModules: map[string]struct{}{"blocked": {}},
		})
		if err != nil {
			t.Fatalf("buildLines() error = %v", err)
		}
		assertOps(t, lines, OpSkip)
		if !strings.Contains(lines[0].Reason, "stale blocked/config") {
			t.Fatalf("desired skip reason = %q, want blocked state owner", lines[0].Reason)
		}
	})
}

func TestLoopLinkLocalKindTransitionsAreRefused(t *testing.T) {
	t.Run("owned link to local", func(t *testing.T) {
		fixture := newLoopFixture(t)
		example := fixture.file("repo/modules/app/local.example", "example")
		owned := fixture.emptyState()
		owned.Links[state.Key{ModuleID: "app", PlacementID: "config"}] = state.LinkRecord{
			Target: ".app",
			Dest:   filepath.Join(fixture.root, "old-link-source"),
		}
		module := config.Module{
			ID: "app",
			Locals: []config.Local{{
				ID:          "config",
				ExamplePath: example,
				Target:      mustTestTarget("~/.app"),
			}},
		}
		assertOps(t, fixture.build(t, []config.Module{module}, owned), OpSkip)
	})

	t.Run("existing local to link", func(t *testing.T) {
		fixture := newLoopFixture(t)
		source := fixture.file("repo/modules/app/config", "data")
		writeLoopFile(t, fixture.target(".app"), "local")
		assertOps(
			t,
			fixture.build(
				t,
				[]config.Module{linkModule("app", "config", source, "~/.app")},
				fixture.emptyState(),
			),
			OpSkip,
		)
	})
}

func TestLoopOwnedLinkHiddenByLocalStillBlocksAnotherPlacement(t *testing.T) {
	fixture := newLoopFixture(t)
	example := fixture.file("repo/modules/app/local.example", "example")
	source := fixture.file("repo/modules/app/new", "new")
	owned := fixture.emptyState()
	owned.Links[state.Key{ModuleID: "app", PlacementID: "local"}] = state.LinkRecord{
		Target: ".old",
		Dest:   filepath.Join(fixture.root, "old-source"),
	}
	module := config.Module{
		ID: "app",
		Locals: []config.Local{{
			ID:          "local",
			ExamplePath: example,
			Target:      mustTestTarget("~/.new-local"),
		}},
		Links: []config.Link{{
			ID:         "new",
			SourcePath: source,
			Target:     mustTestTarget("~/.old"),
		}},
	}

	lines := fixture.build(t, []config.Module{module}, owned)
	assertOps(t, lines, OpSkip, OpSkip)
	for _, line := range lines {
		if line.PlacementID == "new" && !strings.Contains(line.Reason, "stale app/local") {
			t.Fatalf("new placement skip reason = %q, want stale owner", line.Reason)
		}
	}
}

func TestLoopActiveLinkDecisionMatrix(t *testing.T) {
	for _, test := range []struct {
		name      string
		setup     func(*testing.T, loopFixture, string, *state.Snapshot)
		want      []Op
		wantEmpty bool
	}{
		{name: "absent", want: []Op{OpLink}},
		{
			name: "correct unowned",
			setup: func(t *testing.T, fixture loopFixture, source string, _ *state.Snapshot) {
				if err := os.Symlink(source, fixture.target(".app")); err != nil {
					t.Fatalf("os.Symlink(correct) error = %v", err)
				}
			},
			want: []Op{OpRecord},
		},
		{
			name: "correct owned",
			setup: func(t *testing.T, fixture loopFixture, source string, owned *state.Snapshot) {
				if err := os.Symlink(source, fixture.target(".app")); err != nil {
					t.Fatalf("os.Symlink(correct) error = %v", err)
				}
				owned.Links[state.Key{ModuleID: "app", PlacementID: "config"}] = state.LinkRecord{
					Target: ".app",
					Dest:   source,
				}
			},
			wantEmpty: true,
		},
		{
			name: "owned old destination",
			setup: func(t *testing.T, fixture loopFixture, source string, owned *state.Snapshot) {
				old := filepath.Join(fixture.root, "old")
				if err := os.Symlink(old, fixture.target(".app")); err != nil {
					t.Fatalf("os.Symlink(old) error = %v", err)
				}
				owned.Links[state.Key{ModuleID: "app", PlacementID: "config"}] = state.LinkRecord{
					Target: ".app",
					Dest:   old,
				}
				_ = source
			},
			want: []Op{OpReplace},
		},
		{
			name: "unexplained symlink",
			setup: func(t *testing.T, fixture loopFixture, _ string, _ *state.Snapshot) {
				if err := os.Symlink(filepath.Join(fixture.root, "other"), fixture.target(".app")); err != nil {
					t.Fatalf("os.Symlink(other) error = %v", err)
				}
			},
			want: []Op{OpSkip},
		},
		{
			name: "regular file",
			setup: func(t *testing.T, fixture loopFixture, _ string, _ *state.Snapshot) {
				writeLoopFile(t, fixture.target(".app"), "user")
			},
			want: []Op{OpSkip},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLoopFixture(t)
			source := fixture.file("repo/modules/app/config", "data")
			owned := fixture.emptyState()
			if test.setup != nil {
				test.setup(t, fixture, source, &owned)
			}
			lines := fixture.build(
				t,
				[]config.Module{linkModule("app", "config", source, "~/.app")},
				owned,
			)
			if test.wantEmpty {
				assertOps(t, lines)
				return
			}
			assertOps(t, lines, test.want...)
		})
	}
}

func TestLoopStaleDecisionMatrix(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, loopFixture, string, string)
		want  Op
	}{
		{name: "absent", want: OpForget},
		{
			name: "matching symlink",
			setup: func(t *testing.T, fixture loopFixture, target, destination string) {
				if err := os.Symlink(destination, target); err != nil {
					t.Fatalf("os.Symlink(matching) error = %v", err)
				}
			},
			want: OpRemove,
		},
		{
			name: "drifted symlink",
			setup: func(t *testing.T, fixture loopFixture, target, _ string) {
				if err := os.Symlink(filepath.Join(fixture.root, "other"), target); err != nil {
					t.Fatalf("os.Symlink(drifted) error = %v", err)
				}
			},
			want: OpForget,
		},
		{
			name: "regular file",
			setup: func(t *testing.T, _ loopFixture, target, _ string) {
				writeLoopFile(t, target, "user")
			},
			want: OpForget,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLoopFixture(t)
			target := fixture.target(".stale")
			destination := filepath.Join(fixture.root, "owned")
			owned := fixture.emptyState()
			owned.Links[state.Key{ModuleID: "app", PlacementID: "old"}] = state.LinkRecord{
				Target: ".stale",
				Dest:   destination,
			}
			if test.setup != nil {
				test.setup(t, fixture, target, destination)
			}
			assertOps(t, fixture.build(t, nil, owned), test.want)
		})
	}
}

func TestLoopDifferentPlacementCannotTakeOwnedTarget(t *testing.T) {
	fixture := newLoopFixture(t)
	source := fixture.file("repo/modules/app/config", "data")
	owned := fixture.emptyState()
	owned.Links[state.Key{ModuleID: "app", PlacementID: "old"}] = state.LinkRecord{
		Target: ".app",
		Dest:   source,
	}

	lines := fixture.build(
		t,
		[]config.Module{linkModule("app", "new", source, "~/.app")},
		owned,
	)
	assertOps(t, lines, OpSkip, OpSkip)
}

func TestLoopSameKeyDisjointMoveExecutesCreateThenCleanup(t *testing.T) {
	fixture := newLoopFixture(t)
	source := fixture.file("repo/modules/app/config", "data")
	oldTarget := fixture.target(".old")
	if err := os.Symlink(source, oldTarget); err != nil {
		t.Fatalf("os.Symlink(old) error = %v", err)
	}
	key := state.Key{ModuleID: "app", PlacementID: "config"}
	owned := fixture.emptyState()
	owned.Links[key] = state.LinkRecord{Target: ".old", Dest: source}
	lines := fixture.build(
		t,
		[]config.Module{linkModule("app", "config", source, "~/.new")},
		owned,
	)
	assertOps(t, lines, OpLink, OpRemove)

	var committed state.Snapshot
	result, err := executeLines(
		filepath.Join(fixture.root, "state.json"),
		lines,
		state.Loaded{Snapshot: owned},
		func(_ string, snapshot state.Snapshot) (bool, error) {
			committed = cloneSnapshot(snapshot)
			return true, nil
		},
	)
	if err != nil || !result.TargetsChanged || !result.StateChanged {
		t.Fatalf("executeLines(move) = (%#v, %v), want target and state changes", result, err)
	}
	assertLineOps(t, result.Done, OpLink, OpRemove)
	if _, err := os.Lstat(oldTarget); !os.IsNotExist(err) {
		t.Fatalf("old target error = %v, want absent", err)
	}
	newTarget := filepath.Join(fixture.home, ".new")
	if destination, err := os.Readlink(newTarget); err != nil || destination != source {
		t.Fatalf("new target = (%q, %v), want %q", destination, err, source)
	}
	if got := committed.Links[key]; got != (state.LinkRecord{Target: ".new", Dest: source}) {
		t.Fatalf("committed record = %#v, want new target", got)
	}
}

func TestLoopSameKeyRelatedMoveRequiresTwoStages(t *testing.T) {
	for _, target := range []string{"~/.tree/child", "~/.tree"} {
		t.Run(target, func(t *testing.T) {
			fixture := newLoopFixture(t)
			source := fixture.file("repo/modules/app/config", "data")
			ownedTarget := ".tree"
			if target == "~/.tree" {
				ownedTarget = ".tree/child"
			}
			owned := fixture.emptyState()
			owned.Links[state.Key{ModuleID: "app", PlacementID: "config"}] = state.LinkRecord{
				Target: ownedTarget,
				Dest:   source,
			}
			assertOps(
				t,
				fixture.build(t, []config.Module{linkModule("app", "config", source, target)}, owned),
				OpSkip,
				OpSkip,
			)
		})
	}
}

func TestLoopStaleParentAndDesiredChildAlwaysRequireTwoStages(t *testing.T) {
	for _, disk := range []string{"absent", "matching", "drifted"} {
		t.Run(disk, func(t *testing.T) {
			fixture := newLoopFixture(t)
			newSource := fixture.file("repo/modules/new/config", "data")
			ownedDestination := filepath.Join(fixture.root, "owned-tree")
			otherDestination := filepath.Join(fixture.root, "other-tree")
			if err := os.MkdirAll(ownedDestination, 0o700); err != nil {
				t.Fatalf("os.MkdirAll(owned destination) error = %v", err)
			}
			if err := os.MkdirAll(otherDestination, 0o700); err != nil {
				t.Fatalf("os.MkdirAll(other destination) error = %v", err)
			}
			if disk != "absent" {
				destination := ownedDestination
				if disk == "drifted" {
					destination = otherDestination
				}
				if err := os.Symlink(destination, fixture.target(".tree")); err != nil {
					t.Fatalf("os.Symlink(stale parent) error = %v", err)
				}
			}
			owned := fixture.emptyState()
			owned.Links[state.Key{ModuleID: "old", PlacementID: "tree"}] = state.LinkRecord{
				Target: ".tree",
				Dest:   ownedDestination,
			}
			lines := fixture.build(
				t,
				[]config.Module{linkModule("new", "child", newSource, "~/.tree/child")},
				owned,
			)
			assertOps(t, lines, OpSkip, OpSkip)
		})
	}
}

func TestLoopRejectsZeroControlsWithoutPlacements(t *testing.T) {
	home := t.TempDir()
	snapshot, err := state.New(home)
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	_, err = buildLines(loopRequest{Home: home, State: snapshot})
	if !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("buildLines() error = %v, want ErrControlTopology", err)
	}
}

func TestLoopRejectsZeroTargetWithPlacementContext(t *testing.T) {
	fixture := newLoopFixture(t)
	source := fixture.file("repo/modules/app/config", "data")
	_, err := buildLines(loopRequest{
		Home:     fixture.home,
		Controls: fixture.controls,
		Modules: []config.Module{{
			ID: "app",
			Links: []config.Link{{
				ID:         "config",
				SourcePath: source,
			}},
		}},
		State: fixture.emptyState(),
	})
	if !errors.Is(err, corepaths.ErrInvalidPath) || !strings.Contains(err.Error(), "app/config") {
		t.Fatalf("buildLines(zero target) error = %v, want placement-scoped ErrInvalidPath", err)
	}
}

type loopFixture struct {
	root       string
	home       string
	repository string
	controls   corepaths.LexicalControls
}

func newLoopFixture(t *testing.T) loopFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	controls, err := corepaths.NormalizeControls(corepaths.Controls{
		Repository: filepath.Join(root, "repo"),
		Config:     filepath.Join(root, "config", "config.toml"),
		State:      filepath.Join(root, "state", "state.json"),
		Lock:       filepath.Join(root, "state", "lock"),
	})
	if err != nil {
		t.Fatalf("NormalizeControls() error = %v", err)
	}
	return loopFixture{root: root, home: home, repository: filepath.Join(root, "repo"), controls: controls}
}

func (fixture loopFixture) emptyState() state.Snapshot {
	snapshot, err := state.New(fixture.home)
	if err != nil {
		panic(err)
	}
	return snapshot
}

func (fixture loopFixture) target(relative string) string {
	path := filepath.Join(fixture.home, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		panic(err)
	}
	return path
}

func (fixture loopFixture) file(relative, content string) string {
	path := filepath.Join(fixture.root, filepath.FromSlash(relative))
	writeLoopFile(nil, path, content)
	return path
}

func (fixture loopFixture) build(t *testing.T, modules []config.Module, snapshot state.Snapshot) []loopLine {
	t.Helper()
	lines, err := buildLines(loopRequest{
		Home:     fixture.home,
		Controls: fixture.controls,
		Modules:  modules,
		State:    snapshot,
	})
	if err != nil {
		t.Fatalf("buildLines() error = %v", err)
	}
	return lines
}

func linkModule(id, placement, source, target string) config.Module {
	return config.Module{
		ID: id,
		Links: []config.Link{{
			ID:         placement,
			SourcePath: source,
			Target:     mustTestTarget(target),
		}},
	}
}

func mustTestTarget(expression string) corepaths.Target {
	target, err := corepaths.ParseTarget(expression)
	if err != nil {
		panic(err)
	}
	return target
}

func assertOps(t *testing.T, lines []loopLine, want ...Op) {
	t.Helper()
	got := make([]Op, len(lines))
	for index, line := range lines {
		got[index] = line.Op
	}
	if len(got) != len(want) {
		t.Fatalf("ops = %v, want %v; lines=%#v", got, want, lines)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("ops = %v, want %v", got, want)
		}
	}
}

func assertLineOps(t *testing.T, lines []Line, want ...Op) {
	t.Helper()
	got := make([]Op, len(lines))
	for index, line := range lines {
		got[index] = line.Op
	}
	if len(got) != len(want) {
		t.Fatalf("ops = %v, want %v; lines=%#v", got, want, lines)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("ops = %v, want %v", got, want)
		}
	}
}

func writeLoopFile(t *testing.T, path, content string) {
	if t != nil {
		t.Helper()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		if t != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
		panic(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		if t != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", path, err)
		}
		panic(err)
	}
}
