package planner_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestOrderedLinkDecisionRules(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *fixture) (config.Module, state.Snapshot)
		want  planner.Decision
	}{
		{
			name: "other module ownership wins before absent create",
			setup: func(t *testing.T, fixture *fixture) (config.Module, state.Snapshot) {
				source := fixture.file(t, "repo/modules/app/config", "config")
				target := fixture.target(".config/app/config")
				snapshot := state.Snapshot{
					Home: fixture.home,
					Modules: map[string]state.Module{
						"other": {
							Placements: map[string]state.Placement{
								"config": linkRecord(
									target,
									fixture.resolved(t, target),
									source,
								),
							},
						},
					},
				}
				return linkModule("app", "config", source, "~/.config/app/config"), snapshot
			},
			want: planner.DecisionConflict,
		},
		{
			name: "regular file conflicts",
			setup: func(t *testing.T, fixture *fixture) (config.Module, state.Snapshot) {
				source := fixture.file(t, "repo/modules/app/config", "config")
				fixture.fileAbsolute(t, fixture.target(".config/app/config"), "user")
				return linkModule("app", "config", source, "~/.config/app/config"),
					fixture.snapshot(nil)
			},
			want: planner.DecisionConflict,
		},
		{
			name: "directory conflicts",
			setup: func(t *testing.T, fixture *fixture) (config.Module, state.Snapshot) {
				source := fixture.file(t, "repo/modules/app/config", "config")
				target := fixture.target(".config/app/config")
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(target) error = %v", err)
				}
				return linkModule("app", "config", source, "~/.config/app/config"),
					fixture.snapshot(nil)
			},
			want: planner.DecisionConflict,
		},
		{
			name: "absent creates even with matching state",
			setup: func(t *testing.T, fixture *fixture) (config.Module, state.Snapshot) {
				source := fixture.file(t, "repo/modules/app/config", "config")
				target := fixture.target(".config/app/config")
				snapshot := fixture.snapshot(map[string]state.Placement{
					"config": linkRecord(target, fixture.resolved(t, target), source),
				})
				return linkModule("app", "config", source, "~/.config/app/config"), snapshot
			},
			want: planner.DecisionCreateLink,
		},
		{
			name: "correct unknown symlink adopts",
			setup: func(t *testing.T, fixture *fixture) (config.Module, state.Snapshot) {
				source := fixture.file(t, "repo/modules/app/config", "config")
				fixture.symlink(t, source, fixture.target(".config/app/config"))
				return linkModule("app", "config", source, "~/.config/app/config"),
					fixture.snapshot(nil)
			},
			want: planner.DecisionAdopt,
		},
		{
			name: "correct owned symlink keeps",
			setup: func(t *testing.T, fixture *fixture) (config.Module, state.Snapshot) {
				source := fixture.file(t, "repo/modules/app/config", "config")
				target := fixture.target(".config/app/config")
				fixture.symlink(t, source, target)
				snapshot := fixture.snapshot(map[string]state.Placement{
					"config": linkRecord(target, fixture.resolved(t, target), source),
				})
				return linkModule("app", "config", source, "~/.config/app/config"), snapshot
			},
			want: planner.DecisionKeep,
		},
		{
			name: "correct symlink repairs lagging state",
			setup: func(t *testing.T, fixture *fixture) (config.Module, state.Snapshot) {
				oldSource := fixture.file(t, "repo/modules/app/old", "old")
				source := fixture.file(t, "repo/modules/app/config", "config")
				target := fixture.target(".config/app/config")
				fixture.symlink(t, source, target)
				snapshot := fixture.snapshot(map[string]state.Placement{
					"config": linkRecord(target, fixture.resolved(t, target), oldSource),
				})
				return linkModule("app", "config", source, "~/.config/app/config"), snapshot
			},
			want: planner.DecisionRepairState,
		},
		{
			name: "state explained old symlink updates",
			setup: func(t *testing.T, fixture *fixture) (config.Module, state.Snapshot) {
				oldSource := fixture.file(t, "repo/modules/app/old", "old")
				source := fixture.file(t, "repo/modules/app/config", "config")
				target := fixture.target(".config/app/config")
				fixture.symlink(t, oldSource, target)
				snapshot := fixture.snapshot(map[string]state.Placement{
					"config": linkRecord(target, fixture.resolved(t, target), oldSource),
				})
				return linkModule("app", "config", source, "~/.config/app/config"), snapshot
			},
			want: planner.DecisionUpdate,
		},
		{
			name: "unknown wrong symlink conflicts",
			setup: func(t *testing.T, fixture *fixture) (config.Module, state.Snapshot) {
				source := fixture.file(t, "repo/modules/app/config", "config")
				other := fixture.file(t, "user/config", "user")
				fixture.symlink(t, other, fixture.target(".config/app/config"))
				return linkModule("app", "config", source, "~/.config/app/config"),
					fixture.snapshot(nil)
			},
			want: planner.DecisionConflict,
		},
		{
			name: "symlink deviated from state conflicts",
			setup: func(t *testing.T, fixture *fixture) (config.Module, state.Snapshot) {
				oldSource := fixture.file(t, "repo/modules/app/old", "old")
				source := fixture.file(t, "repo/modules/app/config", "config")
				other := fixture.file(t, "user/config", "user")
				target := fixture.target(".config/app/config")
				fixture.symlink(t, other, target)
				snapshot := fixture.snapshot(map[string]state.Placement{
					"config": linkRecord(target, fixture.resolved(t, target), oldSource),
				})
				return linkModule("app", "config", source, "~/.config/app/config"), snapshot
			},
			want: planner.DecisionConflict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t)
			module, snapshot := test.setup(t, fixture)
			before := snapshotTree(t, fixture.root)

			plan := fixture.build(t, []config.Module{module}, snapshot)

			if len(plan.Actions) == 0 || plan.Actions[0].Decision != test.want {
				t.Fatalf("Build() first action = %#v, want %q", plan.Actions, test.want)
			}
			assertTreeUnchanged(t, fixture.root, before)
		})
	}
}

func TestUpdateAndPruneCarryStateRecheckFacts(t *testing.T) {
	t.Run("update", func(t *testing.T) {
		fixture := newFixture(t)
		oldSource := fixture.file(t, "repo/modules/app/old", "old")
		newSource := fixture.file(t, "repo/modules/app/new", "new")
		target := fixture.target(".config/app/config")
		fixture.symlink(t, oldSource, target)
		resolved := fixture.resolved(t, target)
		snapshot := fixture.snapshot(map[string]state.Placement{
			"config": linkRecord(target, resolved, oldSource),
		})

		plan := fixture.build(
			t,
			[]config.Module{
				linkModule("app", "config", newSource, "~/.config/app/config"),
			},
			snapshot,
		)

		action := plan.Actions[0]
		if action.Decision != planner.DecisionUpdate ||
			action.ExpectedResolvedTarget != resolved ||
			action.ExpectedLinkDestination != oldSource {
			t.Fatalf("update action = %#v, want both state recheck facts", action)
		}
	})

	t.Run("prune dangling link", func(t *testing.T) {
		fixture := newFixture(t)
		missingDestination := fixture.path("missing/source")
		target := fixture.target(".config/app/stale")
		fixture.symlink(t, missingDestination, target)
		resolved := fixture.resolved(t, target)
		snapshot := fixture.snapshot(map[string]state.Placement{
			"stale": linkRecord(target, resolved, missingDestination),
		})

		plan := fixture.build(t, nil, snapshot)

		assertDecisions(t, plan, planner.DecisionPrune)
		action := plan.Actions[0]
		if action.ExpectedResolvedTarget != resolved ||
			action.ExpectedLinkDestination != missingDestination {
			t.Fatalf("prune action = %#v, want dangling-link recheck facts", action)
		}
	})
}

func TestStaleLocalForgetsWithoutInspectingContent(t *testing.T) {
	fixture := newFixture(t)
	target := fixture.target(".config/app/config.local")
	fixture.fileAbsolute(t, target, "secret")
	snapshot := fixture.snapshot(map[string]state.Placement{
		"local": {
			Kind:   state.KindLocal,
			Target: target,
		},
	})
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, nil, snapshot)

	assertDecisions(t, plan, planner.DecisionForget)
	if got := plan.Actions[0].Reason; !strings.Contains(got, "local") {
		t.Fatalf("forget reason = %q, want local retention reason", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestStaleLocalBlockedAncestorForgets(t *testing.T) {
	fixture := newFixture(t)
	blocked := fixture.fileAbsolute(t, fixture.target(".blocked"), "user")
	target := filepath.Join(blocked, "config.local")
	snapshot := fixture.snapshot(map[string]state.Placement{
		"local": {
			Kind:   state.KindLocal,
			Target: target,
		},
	})
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, nil, snapshot)

	assertDecisions(t, plan, planner.DecisionForget)
	if got := plan.Actions[0].Reason; !strings.Contains(got, "local") {
		t.Fatalf("forget reason = %q, want local retention reason", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestStaleNonSymlinkForgets(t *testing.T) {
	fixture := newFixture(t)
	source := fixture.file(t, "repo/modules/app/old", "old")
	target := fixture.target(".config/app/stale")
	fixture.fileAbsolute(t, target, "user")
	snapshot := fixture.snapshot(map[string]state.Placement{
		"stale": linkRecord(target, fixture.resolved(t, target), source),
	})
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, nil, snapshot)

	assertDecisions(t, plan, planner.DecisionForget)
	if plan.HasConflicts() {
		t.Fatalf("Build() = %#v, want non-blocking forget", plan)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestStateMapOrderDoesNotChangePlan(t *testing.T) {
	fixture := newFixture(t)
	firstTarget := fixture.target(".config/app/a")
	secondTarget := fixture.target(".config/app/b")
	firstSource := fixture.file(t, "repo/modules/app/a", "a")
	secondSource := fixture.file(t, "repo/modules/app/b", "b")
	fixture.symlink(t, firstSource, firstTarget)
	fixture.symlink(t, secondSource, secondTarget)
	placements := map[string]state.Placement{
		"b": linkRecord(secondTarget, fixture.resolved(t, secondTarget), secondSource),
		"a": linkRecord(firstTarget, fixture.resolved(t, firstTarget), firstSource),
	}
	snapshot := fixture.snapshot(placements)

	first := fixture.build(t, nil, snapshot)
	second := fixture.build(t, nil, snapshot)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Build() is not deterministic\nfirst=%#v\nsecond=%#v", first, second)
	}
	if first.Actions[0].PlacementID != "a" || first.Actions[1].PlacementID != "b" {
		t.Fatalf("Build() state order = %#v, want placement IDs a, b", first.Actions)
	}
}

func TestBuildRejectsStateBoundToDifferentHome(t *testing.T) {
	fixture := newFixture(t)
	plan, err := planner.Build(planner.Request{
		Home:     fixture.home,
		Controls: fixture.controls,
		State: state.Snapshot{
			Home:    filepath.Join(fixture.root, "other-home"),
			Modules: map[string]state.Module{},
		},
	})
	if err == nil {
		t.Fatalf("Build() = %#v, nil error; want HOME mismatch", plan)
	}
}

func TestStaleTargetReuseDoesNotOverrideActiveOwnershipConflict(t *testing.T) {
	fixture := newFixture(t)
	oldSource := fixture.file(t, "repo/modules/old/config", "old")
	newSource := fixture.file(t, "repo/modules/app/config", "new")
	target := fixture.target(".config/app/config")
	snapshot := state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"old": {
				Placements: map[string]state.Placement{
					"config": linkRecord(target, fixture.resolved(t, target), oldSource),
				},
			},
		},
	}
	module := linkModule("app", "config", newSource, "~/.config/app/config")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, planner.DecisionConflict, planner.DecisionForget)
	if !plan.HasConflicts() {
		t.Fatal("Build() HasConflicts() = false, want active ownership conflict")
	}
	if got := plan.Actions[1].Reason; got != "stale target is reused by desired configuration" {
		t.Fatalf("stale reuse reason = %q", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestScopedPlanChecksOnlyRelationshipsInvolvingScope(t *testing.T) {
	fixture := newFixture(t)
	appSource := fixture.file(t, "repo/modules/app/config", "app")
	firstSource := fixture.file(t, "repo/modules/first/config", "first")
	secondSource := fixture.file(t, "repo/modules/second/config", "second")
	modules := []config.Module{
		linkModule("app", "config", appSource, "~/.config/app"),
		linkModule("first", "config", firstSource, "~/.config/shared"),
		linkModule("second", "config", secondSource, "~/.config/shared/child"),
	}

	plan, err := planner.Build(planner.Request{
		Home:     fixture.home,
		Controls: fixture.controls,
		Modules:  modules,
		Scope:    []string{"app"},
		State:    fixture.snapshot(nil),
	})
	if err != nil {
		t.Fatalf("Build(scoped) error = %v", err)
	}
	assertDecisions(t, plan, planner.DecisionCreateLink)

	_, err = planner.Build(planner.Request{
		Home:     fixture.home,
		Controls: fixture.controls,
		Modules:  modules,
		State:    fixture.snapshot(nil),
	})
	if !errors.Is(err, corepaths.ErrTargetConflict) {
		t.Fatalf("Build(full unrelated nested targets) error = %v, want target conflict", err)
	}

	modules[1].Links[0].Target = "~/.config/app/child"
	modules[2].Links[0].Target = "~/.config/second"
	_, err = planner.Build(planner.Request{
		Home:     fixture.home,
		Controls: fixture.controls,
		Modules:  modules,
		Scope:    []string{"app"},
		State:    fixture.snapshot(nil),
	})
	if err == nil {
		t.Fatal("Build(scoped selected-parent conflict) error = nil")
	}

	modules[1].Links[0].Target = "~/.config"
	_, err = planner.Build(planner.Request{
		Home:     fixture.home,
		Controls: fixture.controls,
		Modules:  modules,
		Scope:    []string{"app"},
		State:    fixture.snapshot(nil),
	})
	if err == nil {
		t.Fatal("Build(scoped selected-child conflict) error = nil")
	}
}

func TestBuildRejectsNestedTargetsForEveryPlacementKindCombination(t *testing.T) {
	combinations := []struct {
		name           string
		parentKind     state.Kind
		descendantKind state.Kind
	}{
		{name: "link-link", parentKind: state.KindLink, descendantKind: state.KindLink},
		{name: "link-local", parentKind: state.KindLink, descendantKind: state.KindLocal},
		{name: "local-link", parentKind: state.KindLocal, descendantKind: state.KindLink},
		{name: "local-local", parentKind: state.KindLocal, descendantKind: state.KindLocal},
	}

	for _, combination := range combinations {
		t.Run(combination.name, func(t *testing.T) {
			fixture := newFixture(t)
			parentSource := fixture.file(t, "repo/modules/app/parent", "parent")
			descendantSource := fixture.file(t, "repo/modules/app/descendant", "descendant")
			module := config.Module{ID: "app"}
			add := func(kind state.Kind, id, source, target string) {
				t.Helper()
				switch kind {
				case state.KindLink:
					module.Links = append(module.Links, config.Link{
						ID:         id,
						SourcePath: source,
						Target:     target,
					})
				case state.KindLocal:
					module.Locals = append(module.Locals, config.Local{
						ID:          id,
						ExamplePath: source,
						Target:      target,
					})
				default:
					t.Fatalf("unsupported test kind %q", kind)
				}
			}
			add(combination.parentKind, "parent", parentSource, "~/.config/app")
			add(
				combination.descendantKind,
				"descendant",
				descendantSource,
				"~/.config/app/child",
			)
			for _, operation := range []struct {
				name  string
				scope []string
			}{
				{name: "full"},
				{name: "scoped", scope: []string{"app"}},
			} {
				t.Run(operation.name, func(t *testing.T) {
					before := snapshotTree(t, fixture.root)

					plan, err := planner.Build(planner.Request{
						Home:     fixture.home,
						Controls: fixture.controls,
						Modules:  []config.Module{module},
						Scope:    operation.scope,
						State:    fixture.snapshot(nil),
					})

					if !errors.Is(err, corepaths.ErrTargetConflict) {
						t.Fatalf("Build() = (%#v, %v), want target conflict", plan, err)
					}
					if plan.Actions != nil {
						t.Fatalf("Build() returned partial plan %#v", plan)
					}
					assertTreeUnchanged(t, fixture.root, before)
				})
			}
		})
	}
}

func TestScopedPlanLeavesOtherModuleStateUntouched(t *testing.T) {
	fixture := newFixture(t)
	appSource := fixture.file(t, "repo/modules/app/config", "app")
	otherSource := fixture.file(t, "repo/modules/other/config", "other")
	otherTarget := fixture.target(".config/other")
	fixture.symlink(t, otherSource, otherTarget)
	snapshot := state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"other": {
				Placements: map[string]state.Placement{
					"config": linkRecord(
						otherTarget,
						fixture.resolved(t, otherTarget),
						otherSource,
					),
				},
			},
		},
	}

	plan, err := planner.Build(planner.Request{
		Home:     fixture.home,
		Controls: fixture.controls,
		Modules: []config.Module{
			linkModule("app", "config", appSource, "~/.config/app"),
		},
		Scope: []string{"app"},
		State: snapshot,
	})
	if err != nil {
		t.Fatalf("Build(scoped) error = %v", err)
	}
	assertDecisions(t, plan, planner.DecisionCreateLink)
}

func TestBuildPropagatesStaleFilesystemErrorWithoutPartialPlan(t *testing.T) {
	fixture := newFixture(t)
	blocked := fixture.target("blocked")
	if err := os.MkdirAll(blocked, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(blocked) error = %v", err)
	}
	target := filepath.Join(blocked, "config")
	resolved := fixture.resolved(t, target)
	snapshot := fixture.snapshot(map[string]state.Placement{
		"stale": linkRecord(target, resolved, fixture.path("repo/source")),
	})
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatalf("os.Chmod(blocked) error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0o700); err != nil {
			t.Errorf("restore blocked directory mode: %v", err)
		}
	})

	plan, err := planner.Build(planner.Request{
		Home:     fixture.home,
		Controls: fixture.controls,
		State:    snapshot,
	})
	if err == nil {
		t.Skip("filesystem did not enforce directory search permission")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Build() error = %v, want permission error", err)
	}
	if plan.Actions != nil {
		t.Fatalf("Build() returned partial plan %#v", plan)
	}
}

func TestStaleDanglingAncestorForgets(t *testing.T) {
	fixture := newFixture(t)
	oldParent := fixture.dir(t, "parents/old")
	parentLink := fixture.target("alias")
	fixture.symlink(t, oldParent, parentLink)
	source := fixture.file(t, "repo/modules/app/config", "config")
	target := parentLink + "/config"
	fixture.symlink(t, source, filepath.Join(oldParent, "config"))
	snapshot := fixture.snapshot(map[string]state.Placement{
		"stale": linkRecord(target, fixture.resolved(t, target), source),
	})
	if err := os.Remove(parentLink); err != nil {
		t.Fatalf("os.Remove(parent link) error = %v", err)
	}
	fixture.symlink(t, fixture.path("missing-parent"), parentLink)

	plan := fixture.build(t, nil, snapshot)

	assertDecisions(t, plan, planner.DecisionForget)
	if got := plan.Actions[0].Reason; got != "stale target cannot be resolved safely" {
		t.Fatalf("forget reason = %q, want safe-resolution reason", got)
	}
}

func TestStaleLoopedAncestorForgets(t *testing.T) {
	fixture := newFixture(t)
	oldParent := fixture.dir(t, "parents/old")
	parentLink := fixture.target("alias")
	fixture.symlink(t, oldParent, parentLink)
	source := fixture.file(t, "repo/modules/app/config", "config")
	target := parentLink + "/config"
	fixture.symlink(t, source, filepath.Join(oldParent, "config"))
	snapshot := fixture.snapshot(map[string]state.Placement{
		"stale": linkRecord(target, fixture.resolved(t, target), source),
	})
	if err := os.Remove(parentLink); err != nil {
		t.Fatalf("os.Remove(parent link) error = %v", err)
	}
	fixture.symlink(t, "alias", parentLink)
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, nil, snapshot)

	assertDecisions(t, plan, planner.DecisionForget)
	if got := plan.Actions[0].Reason; got != "stale target cannot be resolved safely" {
		t.Fatalf("forget reason = %q, want safe-resolution reason", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestStaleLinkTargetContainingDesiredTargetIsConflict(t *testing.T) {
	fixture := newFixture(t)
	oldDirectory := fixture.dir(t, "repo-old/app")
	parentTarget := fixture.target(".config/app")
	fixture.symlink(t, oldDirectory, parentTarget)
	oldResolved := fixture.resolved(t, parentTarget)
	newSource := fixture.file(t, "repo/modules/app/new", "new")
	snapshot := fixture.snapshot(map[string]state.Placement{
		"config": linkRecord(parentTarget, oldResolved, oldDirectory),
	})
	module := linkModule("app", "config", newSource, "~/.config/app/child")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, planner.DecisionCreateLink, planner.DecisionConflict)
	if !plan.HasConflicts() {
		t.Fatal("Build() HasConflicts() = false, want unsafe parent prune conflict")
	}
	if got := plan.Actions[1].Reason; got != "stale link target contains an active desired target" {
		t.Fatalf("stale parent conflict reason = %q", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestStaleTargetBlockedByRegularAncestorForgets(t *testing.T) {
	fixture := newFixture(t)
	blockingAncestor := fixture.target(".config/app")
	if err := os.MkdirAll(blockingAncestor, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(blocking ancestor) error = %v", err)
	}
	staleTarget := filepath.Join(blockingAncestor, "config")
	staleResolved := fixture.resolved(t, staleTarget)
	if err := os.Remove(blockingAncestor); err != nil {
		t.Fatalf("os.Remove(blocking ancestor directory) error = %v", err)
	}
	fixture.fileAbsolute(t, blockingAncestor, "user")
	oldSource := fixture.file(t, "repo/modules/app/old", "old")
	newSource := fixture.file(t, "repo/modules/app/new", "new")
	snapshot := fixture.snapshot(map[string]state.Placement{
		"old": linkRecord(staleTarget, staleResolved, oldSource),
	})
	module := linkModule("app", "new", newSource, "~/.config/app-new")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, planner.DecisionCreateLink, planner.DecisionForget)
	if plan.HasConflicts() {
		t.Fatalf("Build() = %#v, want non-blocking stale takeover", plan)
	}
	assertTreeUnchanged(t, fixture.root, before)
}
