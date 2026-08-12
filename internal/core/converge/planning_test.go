package converge

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

const conflictDecision Decision = "conflict"

func TestPlanSourceContentChangeIsNoOp(t *testing.T) {
	fixture := newPlanFixture(t)
	source := fixture.file(t, "repo/modules/app/config", "before")
	target := fixture.target(".config/app/config")
	fixture.symlink(t, source, target)
	snapshot := fixture.snapshot(map[string]state.LinkRecord{
		"config": linkRecord(target, fixture.resolved(t, target), source),
	})
	module := linkModule("app", "config", source, "~/.config/app/config")

	if err := os.WriteFile(source, []byte("after"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(source) error = %v", err)
	}
	before := snapshotTree(t, fixture.root)

	first := fixture.build(t, []config.Module{module}, snapshot)
	second := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, first)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated Build() changed plan\nfirst=%#v\nsecond=%#v", first, second)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanAddAndSafeStalePruneAreOrdered(t *testing.T) {
	fixture := newPlanFixture(t)
	newSource := fixture.file(t, "repo/modules/app/new", "new")
	oldSource := fixture.file(t, "repo/modules/app/old", "old")
	oldTarget := fixture.target(".config/app/old")
	fixture.symlink(t, oldSource, oldTarget)
	newTarget := fixture.target(".config/app/new")
	snapshot := fixture.snapshot(map[string]state.LinkRecord{
		"old": linkRecord(oldTarget, fixture.resolved(t, oldTarget), oldSource),
	})
	module := linkModule("app", "new", newSource, "~/.config/app/new")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, DecisionCreateLink, DecisionPrune)
	actions := plan.Actions()
	if actions[0].Target != newTarget || actions[1].Target != oldTarget {
		t.Fatalf("Build() targets = %#v, want create %q then prune %q", actions, newTarget, oldTarget)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanIndependentAliasUnderStaleSourceDoesNotBlockPrune(t *testing.T) {
	fixture := newPlanFixture(t)
	oldDestination := fixture.dir(t, "old-repo/app")
	oldDestination, err := filepath.EvalSymlinks(oldDestination)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(old destination) error = %v", err)
	}
	staleTarget := fixture.target("stale")
	fixture.symlink(t, oldDestination, staleTarget)
	snapshot := fixture.snapshot(map[string]state.LinkRecord{
		"old": linkRecord(
			staleTarget,
			fixture.resolved(t, staleTarget),
			oldDestination,
		),
	})

	alias := fixture.target("alias")
	fixture.symlink(t, oldDestination, alias)
	newSource := fixture.file(t, "repo/modules/app/new", "new")
	module := linkModule("app", "new", newSource, "~/alias/child")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, DecisionCreateLink, DecisionPrune)
	if len(plan.Problems()) != 0 {
		t.Fatal("Build() has conflict, want independent alias target followed by stale prune")
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanRejectsActiveTargetTraversingStateOwnedParentLink(t *testing.T) {
	tests := []struct {
		name        string
		local       bool
		childTarget func(*testing.T, *planFixture, string) string
	}{
		{
			name: "direct descendant",
			childTarget: func(_ *testing.T, fixture *planFixture, _ string) string {
				return filepath.Join(fixture.home, "owned", "child")
			},
		},
		{
			name: "alias chain",
			childTarget: func(t *testing.T, fixture *planFixture, parent string) string {
				alias := fixture.target("alias")
				fixture.symlink(t, parent, alias)
				return filepath.Join(alias, "child")
			},
		},
		{
			name:  "alias chain local",
			local: true,
			childTarget: func(t *testing.T, fixture *planFixture, parent string) string {
				alias := fixture.target("alias")
				fixture.symlink(t, parent, alias)
				return filepath.Join(alias, "child")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlanFixture(t)
			oldTree := fixture.dir(t, "old-repo/tree")
			parent := fixture.target("owned")
			fixture.symlink(t, oldTree, parent)
			snapshot := state.Snapshot{
				Home: fixture.home,
				Links: map[state.Key]state.LinkRecord{
					{ModuleID: "stale", PlacementID: "tree"}: linkRecord(
						parent,
						fixture.resolved(t, parent),
						oldTree,
					),
				},
			}
			child := test.childTarget(t, fixture, parent)
			relative, err := filepath.Rel(fixture.home, child)
			if err != nil {
				t.Fatalf("filepath.Rel(child) error = %v", err)
			}
			source := fixture.file(t, "repo/modules/active/config", "active")
			target := "~/" + filepath.ToSlash(relative)
			module := linkModule("active", "child", source, target)
			if test.local {
				module = localModule("active", "child", source, target)
			}
			before := snapshotTree(t, fixture.root)

			plan, err := buildPlan(planRequest{
				Home:     fixture.home,
				Controls: resolveTestControls(t, fixture.controls),
				Modules:  []config.Module{module},
				State:    snapshot,
			})
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}

			assertDecisions(t, plan, conflictDecision, conflictDecision)
			if got := plan.Problems()[0].Reason; !strings.Contains(
				got,
				`state-owned link from module "stale" placement "tree"`,
			) {
				t.Fatalf("conflict reason = %q, want state owner", got)
			}
			assertTreeUnchanged(t, fixture.root, before)
		})
	}
}

func TestPlanRejectsAdoptedLinkTraversedByEffectiveDesiredTarget(t *testing.T) {
	fixture := newPlanFixture(t)
	parentSource := fixture.dir(t, "repo/modules/parent/tree")
	outside := fixture.dir(t, "outside")
	fixture.symlink(t, outside, filepath.Join(parentSource, "out"))
	parentTarget := fixture.target("owned")
	fixture.symlink(t, parentSource, parentTarget)
	fixture.symlink(t, filepath.Join(parentTarget, "out"), fixture.target("access"))
	childSource := fixture.file(t, "repo/modules/child/config", "child")
	modules := []config.Module{
		linkModule("parent", "tree", parentSource, "~/owned"),
		linkModule("child", "config", childSource, "~/access/child"),
	}
	snapshot := state.Snapshot{
		Home:  fixture.home,
		Links: map[state.Key]state.LinkRecord{},
	}
	before := snapshotTree(t, fixture.root)

	parentOnly := fixture.build(t, modules[:1], snapshot)
	assertDecisions(t, parentOnly, DecisionAdopt)

	plan := fixture.build(t, modules, snapshot)

	assertDecisions(t, plan, conflictDecision, DecisionCreateLink)
	if got := plan.Problems()[0].Reason; !strings.Contains(
		got,
		`effective module "child" placement "config"`,
	) {
		t.Fatalf("prospective ownership conflict reason = %q", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanRejectsResolvedDriftKeepTraversedByEffectiveDesiredTarget(
	t *testing.T,
) {
	fixture := newPlanFixture(t)
	parentSource := fixture.dir(t, "repo/modules/parent/tree")
	outside := fixture.dir(t, "outside")
	fixture.symlink(t, outside, filepath.Join(parentSource, "out"))
	oldParent := fixture.dir(t, "parents/old")
	newParent := fixture.dir(t, "parents/new")
	alias := fixture.target("alias")
	fixture.symlink(t, oldParent, alias)
	parentTarget := filepath.Join(alias, "owned")
	fixture.symlink(t, parentSource, filepath.Join(oldParent, "owned"))
	recordedResolved := fixture.resolved(t, parentTarget)
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "parent", PlacementID: "tree"}: linkRecord(
				parentTarget,
				recordedResolved,
				parentSource,
			),
		},
	}
	if err := os.Remove(alias); err != nil {
		t.Fatalf("os.Remove(alias) error = %v", err)
	}
	fixture.symlink(t, newParent, alias)
	fixture.symlink(t, parentSource, filepath.Join(newParent, "owned"))
	fixture.symlink(t, filepath.Join(parentTarget, "out"), fixture.target("access"))
	childSource := fixture.file(t, "repo/modules/child/config", "child")
	modules := []config.Module{
		linkModule("parent", "tree", parentSource, "~/alias/owned"),
		linkModule("child", "config", childSource, "~/access/child"),
	}
	before := snapshotTree(t, fixture.root)

	parentOnly := fixture.build(t, modules[:1], snapshot)
	assertDecisions(t, parentOnly, DecisionKeep)

	plan := fixture.build(t, modules, snapshot)

	assertDecisions(t, plan, conflictDecision, DecisionCreateLink)
	if got := plan.Problems()[0].Reason; !strings.Contains(
		got,
		`effective module "child" placement "config"`,
	) {
		t.Fatalf("resolved-drift ownership conflict reason = %q", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanRejectsRepairStateTraversedByEffectiveDesiredTarget(t *testing.T) {
	fixture := newPlanFixture(t)
	parentSource := fixture.dir(t, "repo/modules/parent/tree")
	oldSource := fixture.dir(t, "repo/modules/parent/old")
	outside := fixture.dir(t, "outside")
	fixture.symlink(t, outside, filepath.Join(parentSource, "out"))
	parentTarget := fixture.target("owned")
	fixture.symlink(t, parentSource, parentTarget)
	fixture.symlink(t, filepath.Join(parentTarget, "out"), fixture.target("access"))
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "parent", PlacementID: "tree"}: linkRecord(
				parentTarget,
				fixture.resolved(t, parentTarget),
				oldSource,
			),
		},
	}
	childSource := fixture.file(t, "repo/modules/child/config", "child")
	modules := []config.Module{
		linkModule("parent", "tree", parentSource, "~/owned"),
		linkModule("child", "config", childSource, "~/access/child"),
	}
	before := snapshotTree(t, fixture.root)

	parentOnly := fixture.build(t, modules[:1], snapshot)
	assertDecisions(t, parentOnly, DecisionRepairState)

	plan := fixture.build(t, modules, snapshot)

	assertDecisions(t, plan, conflictDecision, DecisionCreateLink)
	if got := plan.Problems()[0].Reason; !strings.Contains(
		got,
		`effective module "child" placement "config"`,
	) {
		t.Fatalf("repair-state ownership conflict reason = %q", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestFullPlanRejectsLinkUpdateTraversedByDesiredTarget(t *testing.T) {
	tests := []struct {
		name  string
		local bool
	}{
		{name: "link"},
		{name: "local", local: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlanFixture(t)
			oldSource := fixture.dir(t, "repo/modules/parent/old")
			newSource := fixture.dir(t, "repo/modules/parent/new")
			outside := fixture.dir(t, "outside")
			fixture.symlink(t, outside, filepath.Join(oldSource, "out"))
			parentTarget := fixture.target("owned")
			fixture.symlink(t, oldSource, parentTarget)
			fixture.symlink(
				t,
				filepath.Join(parentTarget, "out"),
				fixture.target("access"),
			)
			snapshot := state.Snapshot{
				Home: fixture.home,
				Links: map[state.Key]state.LinkRecord{
					{ModuleID: "parent", PlacementID: "tree"}: linkRecord(
						parentTarget,
						fixture.resolved(t, parentTarget),
						oldSource,
					),
				},
			}
			childSource := fixture.file(t, "repo/modules/child/config", "child")
			parent := linkModule("parent", "tree", newSource, "~/owned")
			child := linkModule("child", "config", childSource, "~/access/child")
			if test.local {
				child = localModule("child", "config", childSource, "~/access/child")
			}
			before := snapshotTree(t, fixture.root)

			parentOnly := fixture.build(t, []config.Module{parent}, snapshot)
			assertDecisions(t, parentOnly, DecisionUpdate)

			plan, err := buildPlan(planRequest{
				Home:     fixture.home,
				Controls: resolveTestControls(t, fixture.controls),
				Modules:  []config.Module{parent, child},
				State:    snapshot,
			})
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}

			assertDecisions(t, plan, conflictDecision, conflictDecision)
			if got := plan.Problems()[0].Reason; !strings.Contains(
				got,
				`effective module "child" placement "config"`,
			) {
				t.Fatalf("update conflict reason = %q", got)
			}
			assertTreeUnchanged(t, fixture.root, before)
		})
	}
}

func TestFullPlanAllowsFullyOwnedKeepTraversedByDesired(
	t *testing.T,
) {
	fixture := newPlanFixture(t)
	parentSource := fixture.dir(t, "repo/modules/parent/tree")
	outside := fixture.dir(t, "outside")
	fixture.symlink(t, outside, filepath.Join(parentSource, "out"))
	parentTarget := fixture.target("owned")
	fixture.symlink(t, parentSource, parentTarget)
	fixture.symlink(t, filepath.Join(parentTarget, "out"), fixture.target("access"))
	childSource := fixture.file(t, "repo/modules/child/config", "child")
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "parent", PlacementID: "tree"}: linkRecord(
				parentTarget,
				fixture.resolved(t, parentTarget),
				parentSource,
			),
		},
	}
	before := snapshotTree(t, fixture.root)

	plan, err := buildPlan(planRequest{
		Home:     fixture.home,
		Controls: resolveTestControls(t, fixture.controls),
		Modules: []config.Module{
			linkModule("parent", "tree", parentSource, "~/owned"),
			linkModule("child", "config", childSource, "~/access/child"),
		},
		State: snapshot,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	assertDecisions(t, plan, conflictDecision)
	assertTreeUnchanged(t, fixture.root, before)
}

func TestFullPlanGuardsAliasRebindOnlyWhenOwnershipNeedsRefresh(
	t *testing.T,
) {
	tests := []struct {
		name           string
		removeOldAlias bool
		want           Decision
	}{
		{
			name: "existing ownership remains valid",
			want: DecisionKeep,
		},
		{
			name:           "recorded alias no longer proves ownership",
			removeOldAlias: true,
			want:           conflictDecision,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlanFixture(t)
			parentSource := fixture.dir(t, "repo/modules/parent/tree")
			outside := fixture.dir(t, "outside")
			fixture.symlink(t, outside, filepath.Join(parentSource, "out"))
			realParent := fixture.dir(t, "targets")
			oldAlias := fixture.target("old-alias")
			newAlias := fixture.target("new-alias")
			fixture.symlink(t, realParent, oldAlias)
			fixture.symlink(t, realParent, newAlias)
			recordedTarget := filepath.Join(oldAlias, "owned")
			desiredTarget := filepath.Join(newAlias, "owned")
			fixture.symlink(t, parentSource, filepath.Join(realParent, "owned"))
			recordedResolved := fixture.resolved(t, recordedTarget)
			if test.removeOldAlias {
				if err := os.Remove(oldAlias); err != nil {
					t.Fatalf("os.Remove(old alias) error = %v", err)
				}
			}
			fixture.symlink(
				t,
				filepath.Join(desiredTarget, "out"),
				fixture.target("access"),
			)
			childSource := fixture.file(t, "repo/modules/child/config", "child")
			snapshot := state.Snapshot{
				Home: fixture.home,
				Links: map[state.Key]state.LinkRecord{
					{ModuleID: "parent", PlacementID: "tree"}: linkRecord(
						recordedTarget,
						recordedResolved,
						parentSource,
					),
				},
			}
			before := snapshotTree(t, fixture.root)

			plan, err := buildPlan(planRequest{
				Home:     fixture.home,
				Controls: resolveTestControls(t, fixture.controls),
				Modules: []config.Module{
					linkModule(
						"parent",
						"tree",
						parentSource,
						"~/new-alias/owned",
					),
					linkModule("child", "config", childSource, "~/access/child"),
				},
				State: snapshot,
			})
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}

			if test.want == DecisionKeep {
				assertDecisions(t, plan, DecisionKeep, conflictDecision)
			} else {
				assertDecisions(t, plan, conflictDecision, DecisionCreateLink)
			}
			if test.want == conflictDecision &&
				!strings.Contains(
					plan.Problems()[0].Reason,
					`effective module "child" placement "config"`,
				) {
				t.Fatalf("rebind conflict reason = %q", plan.Problems()[0].Reason)
			}
			assertTreeUnchanged(t, fixture.root, before)
		})
	}
}

func TestFullPlanRejectsStaleCleanupTraversedByDesired(
	t *testing.T,
) {
	tests := []struct {
		name        string
		local       bool
		childTarget func(*testing.T, *planFixture, string) string
	}{
		{
			name: "direct link",
			childTarget: func(_ *testing.T, fixture *planFixture, _ string) string {
				return filepath.Join(fixture.home, "owned", "child")
			},
		},
		{
			name:  "alias local",
			local: true,
			childTarget: func(t *testing.T, fixture *planFixture, parent string) string {
				alias := fixture.target("alias")
				fixture.symlink(t, parent, alias)
				return filepath.Join(alias, "child")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlanFixture(t)
			oldTree := fixture.dir(t, "old-repo/tree")
			parent := fixture.target("owned")
			fixture.symlink(t, oldTree, parent)
			snapshot := state.Snapshot{
				Home: fixture.home,
				Links: map[state.Key]state.LinkRecord{
					{ModuleID: "stale", PlacementID: "tree"}: linkRecord(
						parent,
						fixture.resolved(t, parent),
						oldTree,
					),
				},
			}
			child := test.childTarget(t, fixture, parent)
			relative, err := filepath.Rel(fixture.home, child)
			if err != nil {
				t.Fatalf("filepath.Rel(child) error = %v", err)
			}
			source := fixture.file(t, "repo/modules/active/config", "active")
			target := "~/" + filepath.ToSlash(relative)
			module := linkModule("active", "child", source, target)
			if test.local {
				module = localModule("active", "child", source, target)
			}
			before := snapshotTree(t, fixture.root)

			plan, err := buildPlan(planRequest{
				Home:     fixture.home,
				Controls: resolveTestControls(t, fixture.controls),
				Modules:  []config.Module{module},
				State:    snapshot,
			})
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}

			assertDecisions(t, plan, conflictDecision, conflictDecision)
			found := false
			for _, problem := range plan.Problems() {
				if strings.Contains(problem.Reason, `active module "active" placement "child"`) {
					found = true
				}
			}
			if !found {
				t.Fatalf("problems = %#v, want stale cleanup dependent child", plan.Problems())
			}
			assertTreeUnchanged(t, fixture.root, before)
		})
	}
}

func TestPlanDoesNotConfuseIndependentAliasWithOwnedLinkTraversal(t *testing.T) {
	fixture := newPlanFixture(t)
	oldTree := fixture.dir(t, "old-repo/tree")
	newTree := fixture.dir(t, "repo/modules/parent/new")
	parent := fixture.target("owned")
	fixture.symlink(t, oldTree, parent)
	alias := fixture.target("alias")
	fixture.symlink(t, oldTree, alias)
	childSource := fixture.file(t, "repo/modules/active/config", "active")
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "parent", PlacementID: "tree"}: linkRecord(
				parent,
				fixture.resolved(t, parent),
				oldTree,
			),
		},
	}
	modules := []config.Module{
		linkModule("parent", "tree", newTree, "~/owned"),
		linkModule("active", "child", childSource, "~/alias/child"),
	}
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, modules, snapshot)

	assertDecisions(t, plan, DecisionCreateLink, DecisionUpdate)
	if len(plan.Problems()) != 0 {
		t.Fatalf("Build() = %#v, want independent alias to remain executable", plan)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanRejectsUpdateBeforeTraversedStalePrune(t *testing.T) {
	fixture := newPlanFixture(t)
	oldSource := fixture.dir(t, "repo/modules/parent/old")
	newSource := fixture.dir(t, "repo/modules/parent/new")
	outside := fixture.dir(t, "outside")
	fixture.symlink(t, outside, filepath.Join(oldSource, "out"))
	parentTarget := fixture.target("owned")
	fixture.symlink(t, oldSource, parentTarget)
	fixture.symlink(t, filepath.Join(parentTarget, "out"), fixture.target("access"))
	staleSource := fixture.file(t, "old-repo/child", "stale")
	staleTarget := fixture.target("access/child")
	fixture.symlink(t, staleSource, filepath.Join(outside, "child"))
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "parent", PlacementID: "tree"}: linkRecord(
				parentTarget,
				fixture.resolved(t, parentTarget),
				oldSource,
			),
			{ModuleID: "stale", PlacementID: "child"}: linkRecord(
				staleTarget,
				fixture.resolved(t, staleTarget),
				staleSource,
			),
		},
	}
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{
		linkModule("parent", "tree", newSource, "~/owned"),
	}, snapshot)

	assertDecisions(t, plan, DecisionUpdate, conflictDecision)
	if got := plan.Problems()[0].Reason; !strings.Contains(
		got,
		`active link update from module "parent" placement "tree"`,
	) {
		t.Fatalf("stale cleanup conflict reason = %q", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanConflictedUpdateDoesNotBlockIndependentStaleCleanup(t *testing.T) {
	fixture := newPlanFixture(t)
	oldTree := fixture.dir(t, "repo/modules/parent/old")
	newTree := fixture.dir(t, "repo/modules/parent/new")
	outside := fixture.dir(t, "outside")
	fixture.symlink(t, outside, filepath.Join(oldTree, "out"))
	parentTarget := fixture.target("parent")
	fixture.symlink(t, oldTree, parentTarget)
	access := fixture.target("access")
	fixture.symlink(t, filepath.Join(parentTarget, "out"), access)

	liveSource := fixture.file(t, "repo/modules/live/config", "live")
	staleSource := fixture.file(t, "old-repo/stale", "stale")
	staleTarget := filepath.Join(access, "stale")
	fixture.symlink(t, staleSource, filepath.Join(outside, "stale"))
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "parent", PlacementID: "tree"}: linkRecord(
				parentTarget,
				fixture.resolved(t, parentTarget),
				oldTree,
			),
			{ModuleID: "stale", PlacementID: "config"}: linkRecord(
				staleTarget,
				fixture.resolved(t, staleTarget),
				staleSource,
			),
		},
	}
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{
		linkModule("parent", "tree", newTree, "~/parent"),
		linkModule("live", "config", liveSource, "~/access/live"),
	}, snapshot)

	assertDecisions(
		t,
		plan,
		conflictDecision,
		conflictDecision,
		DecisionPrune,
	)
	if plan.Actions()[0].ModuleID != "stale" {
		t.Fatalf("executable cleanup = %#v, want stale prune", plan.Actions())
	}
	for _, problem := range plan.Problems() {
		if problem.ModuleID == "stale" {
			t.Fatalf("stale cleanup was blocked by a conflicted update: %#v", plan)
		}
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanAllowsUpdateBeforeIndependentStalePrune(t *testing.T) {
	fixture := newPlanFixture(t)
	oldSource := fixture.dir(t, "repo/modules/parent/old")
	newSource := fixture.dir(t, "repo/modules/parent/new")
	outside := fixture.dir(t, "outside")
	fixture.symlink(t, outside, filepath.Join(oldSource, "out"))
	parentTarget := fixture.target("owned")
	fixture.symlink(t, oldSource, parentTarget)
	fixture.symlink(t, outside, fixture.target("access"))
	staleSource := fixture.file(t, "old-repo/child", "stale")
	staleTarget := fixture.target("access/child")
	fixture.symlink(t, staleSource, filepath.Join(outside, "child"))
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "parent", PlacementID: "tree"}: linkRecord(
				parentTarget,
				fixture.resolved(t, parentTarget),
				oldSource,
			),
			{ModuleID: "stale", PlacementID: "child"}: linkRecord(
				staleTarget,
				fixture.resolved(t, staleTarget),
				staleSource,
			),
		},
	}
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{
		linkModule("parent", "tree", newSource, "~/owned"),
	}, snapshot)

	assertDecisions(t, plan, DecisionUpdate, DecisionPrune)
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanAllowsUpdateWithTraversedDriftedStaleForget(t *testing.T) {
	fixture := newPlanFixture(t)
	oldSource := fixture.dir(t, "repo/modules/parent/old")
	newSource := fixture.dir(t, "repo/modules/parent/new")
	outside := fixture.dir(t, "outside")
	fixture.symlink(t, outside, filepath.Join(oldSource, "out"))
	parentTarget := fixture.target("owned")
	fixture.symlink(t, oldSource, parentTarget)
	fixture.symlink(t, filepath.Join(parentTarget, "out"), fixture.target("access"))
	recordedSource := fixture.file(t, "old-repo/child", "stale")
	userSource := fixture.file(t, "user/child", "user")
	staleTarget := fixture.target("access/child")
	fixture.symlink(t, userSource, filepath.Join(outside, "child"))
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "parent", PlacementID: "tree"}: linkRecord(
				parentTarget,
				fixture.resolved(t, parentTarget),
				oldSource,
			),
			{ModuleID: "stale", PlacementID: "child"}: linkRecord(
				staleTarget,
				fixture.resolved(t, staleTarget),
				recordedSource,
			),
		},
	}
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{
		linkModule("parent", "tree", newSource, "~/owned"),
	}, snapshot)

	assertDecisions(t, plan, DecisionUpdate, DecisionForget)
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanOrdersTraversedStalePrunesChildFirst(t *testing.T) {
	fixture := newPlanFixture(t)
	parentSource := fixture.dir(t, "old-repo/tree")
	outside := fixture.dir(t, "outside")
	fixture.symlink(t, outside, filepath.Join(parentSource, "out"))
	parentTarget := fixture.target("owned")
	fixture.symlink(t, parentSource, parentTarget)
	access := fixture.target("access")
	fixture.symlink(t, filepath.Join(parentSource, "out"), access)
	childSource := fixture.file(t, "old-repo/child", "stale")
	childTarget := filepath.Join(access, "child")
	fixture.symlink(t, childSource, filepath.Join(outside, "child"))
	childResolved := fixture.resolved(t, childTarget)
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "app", PlacementID: "a-parent"}: linkRecord(
				parentTarget,
				fixture.resolved(t, parentTarget),
				parentSource,
			),
			{ModuleID: "app", PlacementID: "z-child"}: linkRecord(
				childTarget,
				childResolved,
				childSource,
			),
		},
	}
	if err := os.Remove(access); err != nil {
		t.Fatalf("os.Remove(access) error = %v", err)
	}
	fixture.symlink(t, filepath.Join(parentTarget, "out"), access)
	if got := fixture.resolved(t, childTarget); got != childResolved {
		t.Fatalf("rebound child resolved target = %q, want %q", got, childResolved)
	}
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, nil, snapshot)

	assertDecisions(t, plan, DecisionPrune, DecisionPrune)
	actions := plan.Actions()
	if actions[0].PlacementID != "z-child" ||
		actions[1].PlacementID != "a-parent" {
		t.Fatalf("stale prune order = %#v, want child before parent", actions)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanCollapsesDuplicateStaleOwnershipToOnePrune(t *testing.T) {
	fixture := newPlanFixture(t)
	realParent := fixture.dir(t, "targets")
	firstAlias := fixture.target("first")
	secondAlias := fixture.target("second")
	fixture.symlink(t, realParent, firstAlias)
	fixture.symlink(t, realParent, secondAlias)
	source := fixture.file(t, "old-repo/config", "stale")
	fixture.symlink(t, source, filepath.Join(realParent, "config"))
	firstTarget := filepath.Join(firstAlias, "config")
	secondTarget := filepath.Join(secondAlias, "config")
	resolved := fixture.resolved(t, firstTarget)
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "app", PlacementID: "a-first"}:  linkRecord(firstTarget, resolved, source),
			{ModuleID: "app", PlacementID: "b-second"}: linkRecord(secondTarget, resolved, source),
		},
	}
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, nil, snapshot)

	assertDecisions(t, plan, DecisionPrune, DecisionForget)
	if got := plan.Actions()[1].Reason; !strings.Contains(
		got,
		`module "app" placement "a-first"`,
	) {
		t.Fatalf("duplicate ownership forget reason = %q", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanKeepsIndependentStalePrunesWithSameDestination(t *testing.T) {
	fixture := newPlanFixture(t)
	source := fixture.file(t, "old-repo/config", "stale")
	firstTarget := fixture.target("first")
	secondTarget := fixture.target("second")
	fixture.symlink(t, source, firstTarget)
	fixture.symlink(t, source, secondTarget)
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "app", PlacementID: "a-first"}: linkRecord(
				firstTarget,
				fixture.resolved(t, firstTarget),
				source,
			),
			{ModuleID: "app", PlacementID: "b-second"}: linkRecord(
				secondTarget,
				fixture.resolved(t, secondTarget),
				source,
			),
		},
	}
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, nil, snapshot)

	assertDecisions(t, plan, DecisionPrune, DecisionPrune)
	actions := plan.Actions()
	if actions[0].PlacementID != "a-first" ||
		actions[1].PlacementID != "b-second" {
		t.Fatalf("independent prune order = %#v", actions)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestFullPlanRejectsUpdateWithStaleDependency(t *testing.T) {
	fixture := newPlanFixture(t)
	oldSource := fixture.dir(t, "repo/modules/parent/old")
	newSource := fixture.dir(t, "repo/modules/parent/new")
	outside := fixture.dir(t, "outside")
	fixture.symlink(t, outside, filepath.Join(oldSource, "out"))
	parentTarget := fixture.target("owned")
	fixture.symlink(t, oldSource, parentTarget)
	fixture.symlink(t, filepath.Join(parentTarget, "out"), fixture.target("access"))
	staleSource := fixture.file(t, "old-repo/child", "stale")
	staleTarget := fixture.target("access/child")
	fixture.symlink(t, staleSource, filepath.Join(outside, "child"))
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "parent", PlacementID: "tree"}: linkRecord(
				parentTarget,
				fixture.resolved(t, parentTarget),
				oldSource,
			),
			{ModuleID: "stale", PlacementID: "child"}: linkRecord(
				staleTarget,
				fixture.resolved(t, staleTarget),
				staleSource,
			),
		},
	}
	before := snapshotTree(t, fixture.root)

	plan, err := buildPlan(planRequest{
		Home:     fixture.home,
		Controls: resolveTestControls(t, fixture.controls),
		Modules: []config.Module{
			linkModule("parent", "tree", newSource, "~/owned"),
		},
		State: snapshot,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	assertDecisions(t, plan, DecisionUpdate, conflictDecision)
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanDoesNotTreatDriftedParentLinkAsStateOwned(t *testing.T) {
	fixture := newPlanFixture(t)
	recordedTree := fixture.dir(t, "old-repo/tree")
	userTree := fixture.dir(t, "user/tree")
	parent := fixture.target("owned")
	fixture.symlink(t, userTree, parent)
	source := fixture.file(t, "repo/modules/active/config", "active")
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "stale", PlacementID: "tree"}: linkRecord(
				parent,
				fixture.resolved(t, parent),
				recordedTree,
			),
		},
	}
	module := linkModule("active", "child", source, "~/owned/child")
	before := snapshotTree(t, fixture.root)

	plan, err := buildPlan(planRequest{
		Home:     fixture.home,
		Controls: resolveTestControls(t, fixture.controls),
		Modules:  []config.Module{module},
		State:    snapshot,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	assertDecisions(t, plan, DecisionCreateLink, DecisionForget)
	if len(plan.Problems()) != 0 {
		t.Fatalf("Build() = %#v, want drifted state link to remain unowned", plan)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanDoesNotTreatResolvedParentDriftAsStateOwned(t *testing.T) {
	fixture := newPlanFixture(t)
	oldParent := fixture.dir(t, "parents/old")
	newParent := fixture.dir(t, "parents/new")
	tree := fixture.dir(t, "old-repo/tree")
	alias := fixture.target("alias")
	fixture.symlink(t, oldParent, alias)
	recordTarget := filepath.Join(alias, "owned")
	fixture.symlink(t, tree, filepath.Join(oldParent, "owned"))
	recordResolved := fixture.resolved(t, recordTarget)

	if err := os.Remove(alias); err != nil {
		t.Fatalf("os.Remove(alias) error = %v", err)
	}
	fixture.symlink(t, newParent, alias)
	fixture.symlink(t, tree, filepath.Join(newParent, "owned"))
	source := fixture.file(t, "repo/modules/active/config", "active")
	snapshot := state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "stale", PlacementID: "tree"}: linkRecord(
				recordTarget,
				recordResolved,
				tree,
			),
		},
	}
	module := linkModule("active", "child", source, "~/alias/owned/child")
	before := snapshotTree(t, fixture.root)

	plan, err := buildPlan(planRequest{
		Home:     fixture.home,
		Controls: resolveTestControls(t, fixture.controls),
		Modules:  []config.Module{module},
		State:    snapshot,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	assertDecisions(t, plan, DecisionCreateLink, DecisionForget)
	if len(plan.Problems()) != 0 {
		t.Fatalf("Build() = %#v, want resolved state drift to remain unowned", plan)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanStaleLinkInsideControlPathIsForgotten(t *testing.T) {
	for _, controlName := range []string{"repository", "config", "state", "lock"} {
		t.Run(controlName, func(t *testing.T) {
			fixture := newPlanFixture(t)
			controls := fixture.controls
			var target string
			switch controlName {
			case "repository":
				controls.Repository = fixture.target("repository")
				if err := os.MkdirAll(controls.Repository, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(repository) error = %v", err)
				}
				target = filepath.Join(controls.Repository, "owned-link")
			case "config":
				controls.Config = fixture.target(".config/dot/machine.toml")
				target = controls.Config
			case "state":
				controls.State = fixture.target(".local/state/dot/state.json")
				controls.Lock = fixture.target(".local/state/dot/lock")
				target = controls.State
			case "lock":
				controls.State = fixture.target(".local/state/dot/state.json")
				controls.Lock = fixture.target(".local/state/dot/lock")
				target = controls.Lock
			}

			source := fixture.file(t, "old-repo/source", "old")
			fixture.symlink(t, source, target)
			snapshot := fixture.snapshot(map[string]state.LinkRecord{
				"stale": linkRecord(
					target,
					fixture.resolved(t, target),
					source,
				),
			})
			before := snapshotTree(t, fixture.root)

			plan, err := buildPlan(planRequest{
				Home:     fixture.home,
				Controls: resolveTestControls(t, controls),
				State:    snapshot,
			})
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			assertDecisions(t, plan, DecisionForget)
			if got := plan.Actions()[0].Reason; got != "stale target overlaps a protected control path" {
				t.Fatalf("forget reason = %q", got)
			}
			assertTreeUnchanged(t, fixture.root, before)
		})
	}
}

func TestPlanStaleLinkContainingControlPathIsForgotten(t *testing.T) {
	for _, controlName := range []string{"repository", "config", "state", "lock"} {
		t.Run(controlName, func(t *testing.T) {
			fixture := newPlanFixture(t)
			controls := fixture.controls
			target := fixture.target("managed")
			switch controlName {
			case "repository":
				controls.Repository = filepath.Join(target, "repository")
			case "config":
				controls.Config = filepath.Join(target, "dot", "config.toml")
			case "state":
				controls.State = filepath.Join(target, "dot", "state.json")
				controls.Lock = filepath.Join(target, "dot", "lock")
			case "lock":
				controls.State = filepath.Join(target, "dot", "state.json")
				controls.Lock = filepath.Join(target, "dot", "lock")
			}

			source := fixture.file(t, "old-repo/source", "old")
			snapshot := fixture.snapshot(map[string]state.LinkRecord{
				"stale": linkRecord(
					target,
					fixture.resolved(t, target),
					source,
				),
			})
			before := snapshotTree(t, fixture.root)

			plan, err := buildPlan(planRequest{
				Home:     fixture.home,
				Controls: resolveTestControls(t, controls),
				State:    snapshot,
			})
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			assertDecisions(t, plan, DecisionForget)
			if got := plan.Actions()[0].Reason; got != "stale target overlaps a protected control path" {
				t.Fatalf("forget reason = %q", got)
			}
			assertTreeUnchanged(t, fixture.root, before)
		})
	}
}

func TestPlanStaleResolvedAliasOverlappingControlPathIsForgotten(t *testing.T) {
	fixture := newPlanFixture(t)
	repositoryAlias := filepath.Join(fixture.home, "repository-alias")
	if err := os.Symlink(fixture.repo, repositoryAlias); err != nil {
		t.Fatalf("os.Symlink(repository alias) error = %v", err)
	}
	target := filepath.Join(repositoryAlias, "owned")
	resolved := fixture.resolved(t, target)
	if err := os.WriteFile(resolved, []byte("control data"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(resolved target) error = %v", err)
	}
	snapshot := fixture.snapshot(map[string]state.LinkRecord{
		"stale": linkRecord(
			target,
			resolved,
			filepath.Join(fixture.root, "old-source"),
		),
	})
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, nil, snapshot)

	assertDecisions(t, plan, DecisionForget)
	if got := plan.Actions()[0].Reason; got != "stale target overlaps a protected control path" {
		t.Fatalf("forget reason = %q", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestResolveControlsRejectsInvalidTopologyBeforePlanning(t *testing.T) {
	fixture := newPlanFixture(t)
	controls := fixture.controls
	controls.State = filepath.Join(fixture.repo, "state.json")
	controls.Lock = filepath.Join(fixture.repo, "lock")
	before := snapshotTree(t, fixture.root)

	_, err := resolveControls(controls)
	if !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("resolveControls() error = %v, want control topology problem", err)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanDriftedStaleLinkForgetsAndDoesNotBlock(t *testing.T) {
	fixture := newPlanFixture(t)
	newSource := fixture.file(t, "repo/modules/app/new", "new")
	oldSource := fixture.file(t, "repo/modules/app/old", "old")
	userSource := fixture.file(t, "user/owned", "user")
	oldTarget := fixture.target(".config/app/old")
	fixture.symlink(t, userSource, oldTarget)
	snapshot := fixture.snapshot(map[string]state.LinkRecord{
		"old": linkRecord(oldTarget, fixture.resolved(t, oldTarget), oldSource),
	})
	module := linkModule("app", "new", newSource, "~/.config/app/new")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, DecisionCreateLink, DecisionForget)
	if len(plan.Problems()) != 0 {
		t.Fatal("Build() has conflict, want drifted stale link to remain non-blocking")
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanTargetChangeCreatesBeforePrune(t *testing.T) {
	fixture := newPlanFixture(t)
	source := fixture.file(t, "repo/modules/app/config", "config")
	oldTarget := fixture.target(".old/app")
	newTarget := fixture.target(".config/app")
	fixture.symlink(t, source, oldTarget)
	snapshot := fixture.snapshot(map[string]state.LinkRecord{
		"config": linkRecord(oldTarget, fixture.resolved(t, oldTarget), source),
	})
	module := linkModule("app", "config", source, "~/.config/app")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, DecisionCreateLink, DecisionPrune)
	actions := plan.Actions()
	if actions[0].Target != newTarget || actions[1].Target != oldTarget {
		t.Fatalf("Build() targets = %#v, want new target before old target", actions)
	}
	if len(plan.transitions) != 1 {
		t.Fatalf("Build() transitions = %#v, want one logical-key transition", plan.transitions)
	}
	planned := plan.transitions[0]
	if !planned.desired ||
		planned.moduleID != "app" ||
		planned.placementID != "config" ||
		len(planned.actionIndexes) != 2 ||
		planned.finalRecord.Target != newTarget {
		t.Fatalf("Build() transition = %#v, want desired move to new target", planned)
	}
	final := plan.finalSnapshot()
	wantKey := state.Key{ModuleID: "app", PlacementID: "config"}
	if len(final.Links) != 1 || final.Links[wantKey].Target != newTarget {
		t.Fatalf("Build() FinalState = %#v, want only new target", final)
	}
	plan.transitions[0].actionIndexes[0], plan.transitions[0].actionIndexes[1] = plan.transitions[0].actionIndexes[1], plan.transitions[0].actionIndexes[0]
	ordered := plan.Actions()
	if ordered[0].Target != newTarget || ordered[1].Target != oldTarget {
		t.Fatalf("Plan.Actions() = %#v, want explicit global action order", ordered)
	}
	delete(final.Links, wantKey)
	if got := plan.finalSnapshot().Links[wantKey].Target; got != newTarget {
		t.Fatalf("Plan FinalState changed through returned snapshot: got %q", got)
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanLocalAbsentCreatesAndEveryExistingEntryIsNoOp(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *planFixture, string)
		want  Decision
	}{
		{
			name: "absent",
			setup: func(*testing.T, *planFixture, string) {
			},
			want: DecisionCreateLocal,
		},
		{
			name: "regular file",
			setup: func(t *testing.T, fixture *planFixture, target string) {
				fixture.fileAbsolute(t, target, "user")
			},
			want: "",
		},
		{
			name: "directory",
			setup: func(t *testing.T, _ *planFixture, target string) {
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(target) error = %v", err)
				}
			},
			want: "",
		},
		{
			name: "symlink",
			setup: func(t *testing.T, fixture *planFixture, target string) {
				source := fixture.file(t, "user/local", "user")
				fixture.symlink(t, source, target)
			},
			want: "",
		},
		{
			name: "dangling symlink",
			setup: func(t *testing.T, fixture *planFixture, target string) {
				fixture.symlink(t, fixture.path("missing"), target)
			},
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlanFixture(t)
			example := fixture.file(t, "repo/modules/app/config.local.example", "example")
			target := fixture.target(".config/app/config.local")
			test.setup(t, fixture, target)
			module := localModule("app", "local", example, "~/.config/app/config.local")
			before := snapshotTree(t, fixture.root)

			plan := fixture.build(t, []config.Module{module}, fixture.snapshot(nil))

			if test.want == "" {
				assertDecisions(t, plan)
			} else {
				assertDecisions(t, plan, test.want)
			}
			if len(plan.finalSnapshot().Links) != 0 {
				t.Fatalf("local plan wrote ownership state: %#v", plan.finalSnapshot())
			}
			assertTreeUnchanged(t, fixture.root, before)
		})
	}
}

func TestPlanExampleUpdateDoesNotOverwriteLocal(t *testing.T) {
	fixture := newPlanFixture(t)
	example := fixture.file(t, "repo/modules/app/config.local.example", "before")
	target := fixture.target(".config/app/config.local")
	fixture.fileAbsolute(t, target, "user")
	module := localModule("app", "local", example, "~/.config/app/config.local")
	if err := os.WriteFile(example, []byte("after"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(example) error = %v", err)
	}
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, fixture.snapshot(nil))

	assertDecisions(t, plan)
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanCrossModuleStaleLinkDoesNotBlockLocal(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *planFixture, string, string)
		want  Decision
	}{
		{
			name: "absent target creates local",
			setup: func(*testing.T, *planFixture, string, string) {
			},
			want: DecisionCreateLocal,
		},
		{
			name: "user file keeps local",
			setup: func(t *testing.T, fixture *planFixture, target, _ string) {
				fixture.fileAbsolute(t, target, "user")
			},
			want: "",
		},
		{
			name: "matching old symlink keeps local",
			setup: func(t *testing.T, fixture *planFixture, target, source string) {
				fixture.symlink(t, source, target)
			},
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlanFixture(t)
			oldSource := fixture.file(t, "repo/modules/old/config", "old")
			example := fixture.file(t, "repo/modules/new/local.example", "example")
			target := fixture.target(".config/shared")
			test.setup(t, fixture, target, oldSource)
			snapshot := state.Snapshot{
				Home: fixture.home,
				Links: map[state.Key]state.LinkRecord{
					{ModuleID: "old", PlacementID: "link"}: linkRecord(
						target,
						fixture.resolved(t, target),
						oldSource,
					),
				},
			}
			module := localModule("new", "local", example, "~/.config/shared")
			before := snapshotTree(t, fixture.root)

			plan := fixture.build(t, []config.Module{module}, snapshot)

			if test.want == "" {
				assertDecisions(t, plan, DecisionForget)
			} else {
				assertDecisions(t, plan, test.want, DecisionForget)
			}
			if len(plan.Problems()) != 0 {
				t.Fatalf("Build() = %#v, want local decision plus stale forget", plan)
			}
			assertTreeUnchanged(t, fixture.root, before)
		})
	}
}

func TestPlanUnknownCorrectSymlinkAdopts(t *testing.T) {
	fixture := newPlanFixture(t)
	source := fixture.file(t, "repo/modules/app/config", "config")
	target := fixture.target(".config/app/config")
	fixture.symlink(t, source, target)
	module := linkModule("app", "config", source, "~/.config/app/config")
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, fixture.snapshot(nil))

	assertDecisions(t, plan, DecisionAdopt)
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanStateOwnedSymlinkDriftIsConflict(t *testing.T) {
	fixture := newPlanFixture(t)
	source := fixture.file(t, "repo/modules/app/config", "config")
	userSource := fixture.file(t, "user/config", "user")
	target := fixture.target(".config/app/config")
	fixture.symlink(t, userSource, target)
	module := linkModule("app", "config", source, "~/.config/app/config")
	snapshot := fixture.snapshot(map[string]state.LinkRecord{
		"config": linkRecord(target, fixture.resolved(t, target), source),
	})
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, conflictDecision)
	if len(plan.Problems()) == 0 {
		t.Fatal("Build() Problems is empty, want conflict")
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanLinkOwnershipToDesiredLocalIsConflict(t *testing.T) {
	fixture := newPlanFixture(t)
	source := fixture.file(t, "repo/modules/app/config.local.example", "example")
	target := fixture.target(".config/app/config")
	module := localModule("app", "config", source, "~/.config/app/config")
	snapshot := fixture.snapshot(map[string]state.LinkRecord{
		"config": linkRecord(target, fixture.resolved(t, target), source),
	})
	before := snapshotTree(t, fixture.root)

	plan := fixture.build(t, []config.Module{module}, snapshot)

	assertDecisions(t, plan, conflictDecision)
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanParentSymlinkDriftRejectsUpdate(t *testing.T) {
	fixture := newPlanFixture(t)
	oldParent := fixture.dir(t, "parents/old")
	newParent := fixture.dir(t, "parents/new")
	parentLink := fixture.target("alias")
	fixture.symlink(t, oldParent, parentLink)
	oldSource := fixture.file(t, "repo/modules/app/old", "old")
	newSource := fixture.file(t, "repo/modules/app/new", "new")
	oldResolved := filepath.Join(oldParent, "config")
	fixture.symlink(t, oldSource, oldResolved)
	snapshot := fixture.snapshot(map[string]state.LinkRecord{
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

	assertDecisions(t, plan, conflictDecision)
	if plan.Problems()[0].Reason == "" {
		t.Fatal("Build() conflict has empty reason")
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanParentSymlinkDriftRejectsPruneButContinues(t *testing.T) {
	fixture := newPlanFixture(t)
	oldParent := fixture.dir(t, "parents/old")
	newParent := fixture.dir(t, "parents/new")
	parentLink := fixture.target("alias")
	fixture.symlink(t, oldParent, parentLink)
	source := fixture.file(t, "repo/modules/app/config", "config")
	oldResolved := filepath.Join(oldParent, "config")
	fixture.symlink(t, source, oldResolved)
	snapshot := fixture.snapshot(map[string]state.LinkRecord{
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

	assertDecisions(t, plan, DecisionCreateLink, DecisionForget)
	if len(plan.Problems()) != 0 {
		t.Fatal("Build() has conflict, want stale drift to be non-blocking")
	}
	assertTreeUnchanged(t, fixture.root, before)
}

func TestPlanParentSymlinkDriftWithAbsentNewLeafForgets(t *testing.T) {
	fixture := newPlanFixture(t)
	oldParent := fixture.dir(t, "parents/old")
	newParent := fixture.dir(t, "parents/new")
	parentLink := fixture.target("alias")
	fixture.symlink(t, oldParent, parentLink)
	source := fixture.file(t, "repo/modules/app/config", "config")
	oldTarget := filepath.Join(oldParent, "config")
	fixture.symlink(t, source, oldTarget)
	snapshot := fixture.snapshot(map[string]state.LinkRecord{
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

	assertDecisions(t, plan, DecisionForget)
	assertTreeUnchanged(t, fixture.root, before)
}

type planFixture struct {
	root     string
	home     string
	repo     string
	controls corepaths.Controls
}

func newPlanFixture(t *testing.T) *planFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	for _, path := range []string{home, repo} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", path, err)
		}
	}
	return &planFixture{
		root: root,
		home: home,
		repo: repo,
		controls: corepaths.Controls{
			Repository: repo,
			Config:     filepath.Join(root, "config-control", "machine.toml"),
			State:      filepath.Join(root, "state-control", "state.json"),
			Lock:       filepath.Join(root, "state-control", "dot.lock"),
		},
	}
}

func resolveTestControls(t testing.TB, controls corepaths.Controls) corepaths.ResolvedControls {
	t.Helper()
	resolved, err := corepaths.ResolveControls(controls)
	if err != nil {
		t.Fatalf("ResolveControls() error = %v", err)
	}
	return resolved
}

func (fixture *planFixture) build(
	t *testing.T,
	modules []config.Module,
	snapshot state.Snapshot,
) Plan {
	t.Helper()
	plan, err := buildPlan(planRequest{
		Home:     fixture.home,
		Controls: resolveTestControls(t, fixture.controls),
		Modules:  modules,
		State:    snapshot,
	})
	if err != nil {
		t.Fatalf("buildPlan() error = %v", err)
	}
	expected := make(map[state.Key]bool, len(snapshot.Links))
	for key := range snapshot.Links {
		expected[key] = true
	}
	for _, module := range modules {
		for _, link := range module.Links {
			expected[state.Key{ModuleID: module.ID, PlacementID: link.ID}] = true
		}
		for _, local := range module.Locals {
			expected[state.Key{ModuleID: module.ID, PlacementID: local.ID}] = true
		}
	}
	seen := make(map[state.Key]bool, len(plan.transitions))
	for _, planned := range plan.transitions {
		key := state.Key{
			ModuleID:    planned.moduleID,
			PlacementID: planned.placementID,
		}
		if seen[key] {
			t.Fatalf("buildPlan() returned duplicate transition key %#v: %#v", key, plan)
		}
		seen[key] = true
	}
	if len(seen) != len(expected) {
		t.Fatalf("buildPlan() transition keys = %#v, want desired/state union %#v", seen, expected)
	}
	for key := range expected {
		if !seen[key] {
			t.Fatalf("buildPlan() omitted transition key %#v: %#v", key, plan)
		}
	}
	for _, problem := range plan.Problems() {
		if problem.Kind == ProblemBlocked {
			t.Fatalf("buildPlan() returned blocker: %#v", plan)
		}
	}
	return plan
}

func (fixture *planFixture) snapshot(records map[string]state.LinkRecord) state.Snapshot {
	flat := make(map[state.Key]state.LinkRecord, len(records))
	for placementID, record := range records {
		flat[state.Key{ModuleID: "app", PlacementID: placementID}] = record
	}
	return state.Snapshot{Home: fixture.home, Links: flat}
}

func (fixture *planFixture) path(relative string) string {
	return filepath.Join(fixture.root, filepath.FromSlash(relative))
}

func (fixture *planFixture) target(relative string) string {
	return filepath.Join(fixture.home, filepath.FromSlash(relative))
}

func (fixture *planFixture) resolved(t *testing.T, target string) string {
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

func (fixture *planFixture) dir(t *testing.T, relative string) string {
	t.Helper()
	path := fixture.path(relative)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", path, err)
	}
	return path
}

func (fixture *planFixture) file(t *testing.T, relative, content string) string {
	t.Helper()
	return fixture.fileAbsolute(t, fixture.path(relative), content)
}

func (fixture *planFixture) fileAbsolute(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}

func (fixture *planFixture) symlink(t *testing.T, destination, target string) {
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

func linkRecord(target, resolved, destination string) state.LinkRecord {
	return state.LinkRecord{
		Target:          target,
		ResolvedTarget:  resolved,
		LinkDestination: destination,
	}
}

func assertDecisions(t *testing.T, plan Plan, want ...Decision) {
	t.Helper()
	wantActions := make([]Decision, 0, len(want))
	wantProblems := 0
	for _, decision := range want {
		if decision == conflictDecision {
			wantProblems++
			continue
		}
		wantActions = append(wantActions, decision)
	}
	actions := plan.Actions()
	gotActions := make([]Decision, len(actions))
	for index, action := range actions {
		gotActions[index] = action.Decision
		if action.Decision == DecisionForget && action.Reason == "" {
			t.Fatalf("Build() forget action has empty reason: %#v", action)
		}
	}
	if !slices.Equal(gotActions, wantActions) || len(plan.Problems()) != wantProblems {
		t.Fatalf(
			"Build() actions = %v problems = %d, want actions = %v problems = %d; plan=%#v",
			gotActions,
			len(plan.Problems()),
			wantActions,
			wantProblems,
			plan,
		)
	}
}

func firstDecision(plan Plan) Decision {
	if len(plan.Problems()) != 0 {
		return conflictDecision
	}
	actions := plan.Actions()
	if len(actions) != 0 {
		return actions[0].Decision
	}
	return ""
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
