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

func TestStaleLocalWarnsAndForgetsWithoutInspectingContent(t *testing.T) {
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
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "local") {
		t.Fatalf("Build() warnings = %v, want local provenance warning", plan.Warnings)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestStaleNonSymlinkWarnsAndForgets(t *testing.T) {
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
	if plan.HasConflicts() || len(plan.Warnings) != 1 {
		t.Fatalf("Build() = %#v, want non-blocking forget warning", plan)
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

func TestScopedPlanChecksOnlyRelationshipsInvolvingScope(t *testing.T) {
	fixture := newFixture(t)
	appSource := fixture.file(t, "repo/modules/app/config", "app")
	firstSource := fixture.file(t, "repo/modules/first/config", "first")
	secondSource := fixture.file(t, "repo/modules/second/config", "second")
	modules := []config.Module{
		linkModule("app", "config", appSource, "~/.config/app"),
		linkModule("first", "config", firstSource, "~/.config/shared"),
		linkModule("second", "config", secondSource, "~/.config/shared"),
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

	modules[1].Links[0].Target = "~/.config/app"
	_, err = planner.Build(planner.Request{
		Home:     fixture.home,
		Controls: fixture.controls,
		Modules:  modules,
		Scope:    []string{"app"},
		State:    fixture.snapshot(nil),
	})
	if err == nil {
		t.Fatal("Build(scoped target conflict) error = nil, want conflict with effective module")
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
	if plan.Actions != nil || plan.Warnings != nil {
		t.Fatalf("Build() returned partial plan %#v", plan)
	}
}

func TestStaleDanglingAncestorWarnsAndForgets(t *testing.T) {
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
	if len(plan.Warnings) != 1 {
		t.Fatalf("Build() warnings = %v, want dangling-ancestor warning", plan.Warnings)
	}
}
