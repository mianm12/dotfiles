package planner_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestAcceptance04_SourceContentChangeIsNoOp(t *testing.T) {
	fixture := newFixture(t)
	source := fixture.file(t, "repo/modules/app/config", "before")
	target := fixture.target(".config/app/config")
	fixture.symlink(t, source, target)
	snapshot := fixture.snapshot(map[string]state.Placement{
		"config": linkRecord(target, fixture.resolved(t, target), source),
	})
	module := linkModule("app", "config", source, "~/.config/app/config")

	if err := os.WriteFile(source, []byte("after"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(source) error = %v", err)
	}
	before := snapshotTree(t, fixture.root)

	first := fixture.build(t, []config.Module{module}, snapshot)
	second := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, first, planner.DecisionKeep)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated Build() changed plan\nfirst=%#v\nsecond=%#v", first, second)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestAcceptance05_AddAndSafeStalePruneAreOrdered(t *testing.T) {
	fixture := newFixture(t)
	newSource := fixture.file(t, "repo/modules/app/new", "new")
	oldSource := fixture.file(t, "repo/modules/app/old", "old")
	oldTarget := fixture.target(".config/app/old")
	fixture.symlink(t, oldSource, oldTarget)
	newTarget := fixture.target(".config/app/new")
	snapshot := fixture.snapshot(map[string]state.Placement{
		"old": linkRecord(oldTarget, fixture.resolved(t, oldTarget), oldSource),
	})
	module := linkModule("app", "new", newSource, "~/.config/app/new")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, planner.DecisionCreateLink, planner.DecisionPrune)
	if plan.Actions[0].Target != newTarget || plan.Actions[1].Target != oldTarget {
		t.Fatalf("Build() targets = %#v, want create %q then prune %q", plan.Actions, newTarget, oldTarget)
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("Build() warnings = %v, want none", plan.Warnings)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestAcceptance05_DriftedStaleLinkWarnsAndDoesNotBlock(t *testing.T) {
	fixture := newFixture(t)
	newSource := fixture.file(t, "repo/modules/app/new", "new")
	oldSource := fixture.file(t, "repo/modules/app/old", "old")
	userSource := fixture.file(t, "user/owned", "user")
	oldTarget := fixture.target(".config/app/old")
	fixture.symlink(t, userSource, oldTarget)
	snapshot := fixture.snapshot(map[string]state.Placement{
		"old": linkRecord(oldTarget, fixture.resolved(t, oldTarget), oldSource),
	})
	module := linkModule("app", "new", newSource, "~/.config/app/new")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, planner.DecisionCreateLink, planner.DecisionForget)
	if plan.HasConflicts() {
		t.Fatal("Build() has conflict, want drifted stale link to remain non-blocking")
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("Build() warnings = %v, want one stale warning", plan.Warnings)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestAcceptance06_TargetChangeCreatesBeforePrune(t *testing.T) {
	fixture := newFixture(t)
	source := fixture.file(t, "repo/modules/app/config", "config")
	oldTarget := fixture.target(".old/app")
	newTarget := fixture.target(".config/app")
	fixture.symlink(t, source, oldTarget)
	snapshot := fixture.snapshot(map[string]state.Placement{
		"config": linkRecord(oldTarget, fixture.resolved(t, oldTarget), source),
	})
	module := linkModule("app", "config", source, "~/.config/app")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, planner.DecisionCreateLink, planner.DecisionPrune)
	if plan.Actions[0].Target != newTarget || plan.Actions[1].Target != oldTarget {
		t.Fatalf("Build() targets = %#v, want new target before old target", plan.Actions)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestAcceptance09_LocalAbsentCreatesAndEveryExistingEntryKeeps(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *fixture, string)
		want  planner.Decision
	}{
		{
			name: "absent",
			setup: func(*testing.T, *fixture, string) {
			},
			want: planner.DecisionCreateLocal,
		},
		{
			name: "regular file",
			setup: func(t *testing.T, fixture *fixture, target string) {
				fixture.fileAbsolute(t, target, "user")
			},
			want: planner.DecisionKeep,
		},
		{
			name: "directory",
			setup: func(t *testing.T, _ *fixture, target string) {
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(target) error = %v", err)
				}
			},
			want: planner.DecisionKeep,
		},
		{
			name: "symlink",
			setup: func(t *testing.T, fixture *fixture, target string) {
				source := fixture.file(t, "user/local", "user")
				fixture.symlink(t, source, target)
			},
			want: planner.DecisionKeep,
		},
		{
			name: "dangling symlink",
			setup: func(t *testing.T, fixture *fixture, target string) {
				fixture.symlink(t, fixture.path("missing"), target)
			},
			want: planner.DecisionKeep,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t)
			example := fixture.file(t, "repo/modules/app/config.local.example", "example")
			target := fixture.target(".config/app/config.local")
			test.setup(t, fixture, target)
			module := localModule("app", "local", example, "~/.config/app/config.local")
			before := snapshotTree(t, fixture.root)

			plan := fixture.build(t, []config.Module{module}, fixture.snapshot(nil))

			assertDecisions(t, plan, test.want)
			assertTreeUnchanged(t, fixture.root, before)
		})
	}
}

func TestAcceptance09_ExampleUpdateDoesNotOverwriteLocal(t *testing.T) {
	fixture := newFixture(t)
	example := fixture.file(t, "repo/modules/app/config.local.example", "before")
	target := fixture.target(".config/app/config.local")
	fixture.fileAbsolute(t, target, "user")
	module := localModule("app", "local", example, "~/.config/app/config.local")
	snapshot := fixture.snapshot(map[string]state.Placement{
		"local": {
			Kind:   state.KindLocal,
			Target: target,
		},
	})
	if err := os.WriteFile(example, []byte("after"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(example) error = %v", err)
	}
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, planner.DecisionKeep)
	assertTreeUnchanged(t, fixture.root, before)
}

func TestAcceptance09_CrossModuleStaleLinkDoesNotBlockLocal(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *fixture, string, string)
		want  planner.Decision
	}{
		{
			name: "absent target creates local",
			setup: func(*testing.T, *fixture, string, string) {
			},
			want: planner.DecisionCreateLocal,
		},
		{
			name: "user file keeps local",
			setup: func(t *testing.T, fixture *fixture, target, _ string) {
				fixture.fileAbsolute(t, target, "user")
			},
			want: planner.DecisionKeep,
		},
		{
			name: "matching old symlink keeps local",
			setup: func(t *testing.T, fixture *fixture, target, source string) {
				fixture.symlink(t, source, target)
			},
			want: planner.DecisionKeep,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t)
			oldSource := fixture.file(t, "repo/modules/old/config", "old")
			example := fixture.file(t, "repo/modules/new/local.example", "example")
			target := fixture.target(".config/shared")
			test.setup(t, fixture, target, oldSource)
			snapshot := state.Snapshot{
				Home: fixture.home,
				Modules: map[string]state.Module{
					"old": {
						Placements: map[string]state.Placement{
							"link": linkRecord(
								target,
								fixture.resolved(t, target),
								oldSource,
							),
						},
					},
				},
			}
			module := localModule("new", "local", example, "~/.config/shared")
			before := snapshotTree(t, fixture.root)

			plan := fixture.build(t, []config.Module{module}, snapshot)

			assertDecisions(t, plan, test.want, planner.DecisionForget)
			if plan.HasConflicts() || len(plan.Warnings) != 1 {
				t.Fatalf("Build() = %#v, want local decision plus stale warning/forget", plan)
			}
			assertTreeUnchanged(t, fixture.root, before)
		})
	}
}

func TestAcceptance10_UnknownCorrectSymlinkAdopts(t *testing.T) {
	fixture := newFixture(t)
	source := fixture.file(t, "repo/modules/app/config", "config")
	target := fixture.target(".config/app/config")
	fixture.symlink(t, source, target)
	module := linkModule("app", "config", source, "~/.config/app/config")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, fixture.snapshot(nil))

	assertDecisions(t, plan, planner.DecisionAdopt)
	assertTreeUnchanged(t, fixture.root, before)
}

func TestAcceptance10_StateOwnedSymlinkDriftIsConflict(t *testing.T) {
	fixture := newFixture(t)
	source := fixture.file(t, "repo/modules/app/config", "config")
	userSource := fixture.file(t, "user/config", "user")
	target := fixture.target(".config/app/config")
	fixture.symlink(t, userSource, target)
	module := linkModule("app", "config", source, "~/.config/app/config")
	snapshot := fixture.snapshot(map[string]state.Placement{
		"config": linkRecord(target, fixture.resolved(t, target), source),
	})
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, planner.DecisionConflict)
	if !plan.HasConflicts() {
		t.Fatal("Build() HasConflicts() = false, want true")
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestAcceptance10_PlacementKindChangeIsConflict(t *testing.T) {
	fixture := newFixture(t)
	source := fixture.file(t, "repo/modules/app/config", "config")
	target := fixture.target(".config/app/config")
	module := linkModule("app", "config", source, "~/.config/app/config")
	snapshot := fixture.snapshot(map[string]state.Placement{
		"config": {
			Kind:   state.KindLocal,
			Target: target,
		},
	})
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, planner.DecisionConflict)
	assertTreeUnchanged(t, fixture.root, before)
}

func TestAcceptance11_ParentSymlinkDriftRejectsUpdate(t *testing.T) {
	fixture := newFixture(t)
	oldParent := fixture.dir(t, "parents/old")
	newParent := fixture.dir(t, "parents/new")
	parentLink := fixture.target("alias")
	fixture.symlink(t, oldParent, parentLink)
	oldSource := fixture.file(t, "repo/modules/app/old", "old")
	newSource := fixture.file(t, "repo/modules/app/new", "new")
	oldResolved := filepath.Join(oldParent, "config")
	fixture.symlink(t, oldSource, oldResolved)
	snapshot := fixture.snapshot(map[string]state.Placement{
		"config": linkRecord(parentLink+"/config", fixture.resolved(t, parentLink+"/config"), oldSource),
	})
	if err := os.Remove(parentLink); err != nil {
		t.Fatalf("os.Remove(parent link) error = %v", err)
	}
	fixture.symlink(t, newParent, parentLink)
	fixture.symlink(t, oldSource, filepath.Join(newParent, "config"))
	module := linkModule("app", "config", newSource, "~/alias/config")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, planner.DecisionConflict)
	if plan.Actions[0].Reason == "" {
		t.Fatal("Build() conflict has empty reason")
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestAcceptance11_ParentSymlinkDriftRejectsPruneButContinues(t *testing.T) {
	fixture := newFixture(t)
	oldParent := fixture.dir(t, "parents/old")
	newParent := fixture.dir(t, "parents/new")
	parentLink := fixture.target("alias")
	fixture.symlink(t, oldParent, parentLink)
	source := fixture.file(t, "repo/modules/app/config", "config")
	oldResolved := filepath.Join(oldParent, "config")
	fixture.symlink(t, source, oldResolved)
	snapshot := fixture.snapshot(map[string]state.Placement{
		"old": linkRecord(parentLink+"/config", fixture.resolved(t, parentLink+"/config"), source),
	})
	if err := os.Remove(parentLink); err != nil {
		t.Fatalf("os.Remove(parent link) error = %v", err)
	}
	fixture.symlink(t, newParent, parentLink)
	fixture.symlink(t, source, filepath.Join(newParent, "config"))
	newSource := fixture.file(t, "repo/modules/app/new", "new")
	module := linkModule("app", "new", newSource, "~/.config/app/new")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, planner.DecisionCreateLink, planner.DecisionForget)
	if plan.HasConflicts() {
		t.Fatal("Build() has conflict, want stale drift to be non-blocking")
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("Build() warnings = %v, want one", plan.Warnings)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestAcceptance11_ParentSymlinkDriftWithAbsentNewLeafStillWarns(t *testing.T) {
	fixture := newFixture(t)
	oldParent := fixture.dir(t, "parents/old")
	newParent := fixture.dir(t, "parents/new")
	parentLink := fixture.target("alias")
	fixture.symlink(t, oldParent, parentLink)
	source := fixture.file(t, "repo/modules/app/config", "config")
	oldTarget := filepath.Join(oldParent, "config")
	fixture.symlink(t, source, oldTarget)
	snapshot := fixture.snapshot(map[string]state.Placement{
		"old": linkRecord(
			parentLink+"/config",
			fixture.resolved(t, parentLink+"/config"),
			source,
		),
	})
	if err := os.Remove(parentLink); err != nil {
		t.Fatalf("os.Remove(parent link) error = %v", err)
	}
	fixture.symlink(t, newParent, parentLink)
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, nil, snapshot)

	assertDecisions(t, plan, planner.DecisionForget)
	if len(plan.Warnings) != 1 {
		t.Fatalf("Build() warnings = %v, want parent-drift warning", plan.Warnings)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

type fixture struct {
	root     string
	home     string
	repo     string
	controls corepaths.Controls
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	for _, path := range []string{home, repo} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", path, err)
		}
	}
	return &fixture{
		root: root,
		home: home,
		repo: repo,
		controls: corepaths.Controls{
			Repository: repo,
			Config:     filepath.Join(root, "control", "machine.toml"),
			State:      filepath.Join(root, "control", "state.json"),
			Lock:       filepath.Join(root, "control", "dot.lock"),
		},
	}
}

func (fixture *fixture) build(
	t *testing.T,
	modules []config.Module,
	snapshot state.Snapshot,
) planner.Plan {
	t.Helper()
	plan, err := planner.Build(planner.Request{
		Home:     fixture.home,
		Controls: fixture.controls,
		Modules:  modules,
		State:    snapshot,
	})
	if err != nil {
		t.Fatalf("planner.Build() error = %v", err)
	}
	return plan
}

func (fixture *fixture) snapshot(placements map[string]state.Placement) state.Snapshot {
	modules := make(map[string]state.Module)
	if placements != nil {
		modules["app"] = state.Module{Placements: placements}
	}
	return state.Snapshot{Home: fixture.home, Modules: modules}
}

func (fixture *fixture) path(relative string) string {
	return filepath.Join(fixture.root, filepath.FromSlash(relative))
}

func (fixture *fixture) target(relative string) string {
	return filepath.Join(fixture.home, filepath.FromSlash(relative))
}

func (fixture *fixture) resolved(t *testing.T, target string) string {
	t.Helper()
	relative, err := filepath.Rel(fixture.home, target)
	if err != nil {
		t.Fatalf("filepath.Rel(HOME, target) error = %v", err)
	}
	resolved, err := corepaths.ResolveTarget(
		fixture.home,
		"~/"+filepath.ToSlash(relative),
	)
	if err != nil {
		t.Fatalf("paths.ResolveTarget(%q) error = %v", target, err)
	}
	return resolved.Resolved()
}

func (fixture *fixture) dir(t *testing.T, relative string) string {
	t.Helper()
	path := fixture.path(relative)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", path, err)
	}
	return path
}

func (fixture *fixture) file(t *testing.T, relative, content string) string {
	t.Helper()
	return fixture.fileAbsolute(t, fixture.path(relative), content)
}

func (fixture *fixture) fileAbsolute(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}

func (fixture *fixture) symlink(t *testing.T, destination, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(target), err)
	}
	if err := os.Symlink(destination, target); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", destination, target, err)
	}
}

func linkModule(moduleID, placementID, source, target string) config.Module {
	return config.Module{
		ID: moduleID,
		Links: []config.Link{{
			ID:         placementID,
			SourcePath: source,
			Target:     target,
			SourceMode: 0o600,
		}},
	}
}

func localModule(moduleID, placementID, example, target string) config.Module {
	return config.Module{
		ID: moduleID,
		Locals: []config.Local{{
			ID:          placementID,
			ExamplePath: example,
			Target:      target,
		}},
	}
}

func linkRecord(target, resolved, destination string) state.Placement {
	return state.Placement{
		Kind:            state.KindLink,
		Target:          target,
		ResolvedTarget:  resolved,
		LinkDestination: destination,
	}
}

func assertDecisions(t *testing.T, plan planner.Plan, want ...planner.Decision) {
	t.Helper()
	got := make([]planner.Decision, len(plan.Actions))
	for index, action := range plan.Actions {
		got[index] = action.Decision
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Build() decisions = %v, want %v; actions=%#v", got, want, plan.Actions)
	}
}

type treeEntry struct {
	mode fs.FileMode
	link string
	data string
}

func snapshotTree(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	snapshot := make(map[string]treeEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		record := treeEntry{mode: info.Mode()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			record.link, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var content []byte
			content, err = os.ReadFile(path)
			record.data = string(content)
		}
		if err != nil {
			return err
		}
		snapshot[relative] = record
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot tree %q: %v", root, err)
	}
	return snapshot
}

func assertTreeUnchanged(t *testing.T, root string, before map[string]treeEntry) {
	t.Helper()
	after := snapshotTree(t, root)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("planning mutated fixture\nbefore=%v\nafter=%v", before, after)
	}
}
