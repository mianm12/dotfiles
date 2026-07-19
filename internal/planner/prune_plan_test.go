package planner

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mianm12/dotfiles/internal/paths"
)

func TestPlanPrune_FullAndPartialScopeUseCompleteDesiredForGroups(t *testing.T) {
	profile := ObservedProfile{
		targets: []ObservedTarget{
			{Desired: Desired{Module: "keep", Source: "current", Target: "~/current"}},
		},
		orphans: []OrphanTarget{
			pruneTestOrphan(t, "~/z-keep", "keep", StateScaffold, "", Observation{Kind: ObjectRegular}),
			pruneTestOrphan(t, "~/a-gone", "gone", StateSymlink, "/repo/gone", Observation{Kind: ObjectSymlink, LinkDest: "/repo/gone"}),
			pruneTestOrphan(t, "~/m-other", "other", StateSymlink, "/repo/other", Observation{Kind: ObjectMissing}),
		},
	}

	full, err := PlanPrune(profile, nil, PruneOptions{Enabled: true, Full: true})
	if err != nil {
		t.Fatalf("PlanPrune(full) error = %v", err)
	}
	fullActions := full.Actions()
	if got := pruneActionTargets(fullActions); !reflect.DeepEqual(got, []string{"~/a-gone", "~/m-other", "~/z-keep"}) {
		t.Fatalf("full action targets = %v, want stable all orphan order", got)
	}
	fullGroups := full.ConfirmationGroups()
	wantGroups := []PruneConfirmationGroup{
		{
			Module: "gone",
			Targets: []PruneConfirmationTarget{{
				Target:            "~/a-gone",
				WouldDeleteTarget: true,
			}},
		},
		{
			Module: "other",
			Targets: []PruneConfirmationTarget{{
				Target:            "~/m-other",
				WouldDeleteTarget: false,
			}},
		},
	}
	if !reflect.DeepEqual(fullGroups, wantGroups) {
		t.Fatalf("full confirmation groups = %#v, want %#v", fullGroups, wantGroups)
	}
	for _, group := range fullGroups {
		if group.Module == "keep" {
			t.Fatalf("partial action subset incorrectly made complete-desired module a whole-module group: %#v", fullGroups)
		}
	}

	partial, err := PlanPrune(profile, nil, PruneOptions{
		Enabled: true,
		Modules: []string{"other", "keep", "other"},
	})
	if err != nil {
		t.Fatalf("PlanPrune(partial) error = %v", err)
	}
	if got := pruneActionTargets(partial.Actions()); !reflect.DeepEqual(got, []string{"~/m-other", "~/z-keep"}) {
		t.Fatalf("partial action targets = %v, want requested modules only", got)
	}
	partialGroups := partial.ConfirmationGroups()
	if len(partialGroups) != 1 || partialGroups[0].Module != "other" {
		t.Fatalf("partial confirmation groups = %#v, want only whole-module other", partialGroups)
	}
}

func TestPlanPrune_FileConflictDefersEveryCandidate(t *testing.T) {
	profile := ObservedProfile{orphans: []OrphanTarget{
		pruneTestOrphan(t, "~/owned", "app", StateSymlink, "/repo/source", Observation{Kind: ObjectSymlink, LinkDest: "/repo/source"}),
		pruneTestOrphan(t, "~/scaffold", "app", StateScaffold, "", Observation{Kind: ObjectRegular}),
		pruneTestOrphan(t, "~/unowned", "app", StateSymlink, "/repo/source", Observation{Kind: ObjectMissing}),
	}}
	fileActions := []FileAction{
		{Verb: FileSkip},
		{Verb: FileConflict, Target: "~/blocking"},
	}

	plan, err := PlanPrune(profile, fileActions, PruneOptions{Enabled: true, Full: true})
	if err != nil {
		t.Fatalf("PlanPrune() error = %v", err)
	}
	actions := plan.Actions()
	if len(actions) != 3 {
		t.Fatalf("PlanPrune() actions = %#v, want all three deferred candidates", actions)
	}
	for _, action := range actions {
		if !action.Deferred || action.DeferredReason != PruneDeferredFileConflict || action.DeletesTarget() {
			t.Errorf("deferred action = %#v, want explicit non-executable file-conflict defer", action)
		}
		if action.OnSuccess.Kind != StatePreserve || action.OnFailure.Kind != StatePreserve {
			t.Errorf("deferred action state effects = (%#v, %#v), want preserve/preserve", action.OnSuccess, action.OnFailure)
		}
	}
	if actions[0].Mode != PruneTargetAndState || !actions[0].WouldDeleteTarget() {
		t.Fatalf("deferred P2 lost underlying target-delete classification: %#v", actions[0])
	}
}

func TestPlanPrune_NoPruneReturnsEmptyWithoutConsumingCandidates(t *testing.T) {
	profile := ObservedProfile{orphans: []OrphanTarget{{
		State: HistoricalState{Kind: StateKind("rendered")},
	}}}
	plan, err := PlanPrune(profile, []FileAction{{Verb: FileConflict}}, PruneOptions{Enabled: false})
	if err != nil {
		t.Fatalf("PlanPrune(no-prune) error = %v", err)
	}
	if plan.Actions() != nil || plan.ConfirmationGroups() != nil {
		t.Fatalf("PlanPrune(no-prune) = %#v, want empty plan", plan)
	}
}

func TestPlanPrune_RejectsAmbiguousOrEmptyPartialScope(t *testing.T) {
	profile := ObservedProfile{}
	tests := []PruneOptions{
		{Enabled: true},
		{Enabled: true, Full: true, Modules: []string{"app"}},
		{Enabled: true, Modules: []string{""}},
	}
	for _, options := range tests {
		plan, err := PlanPrune(profile, nil, options)
		if !errors.Is(err, ErrUnsupportedPruneInput) || plan.Actions() != nil || plan.ConfirmationGroups() != nil {
			t.Errorf("PlanPrune(%#v) = (%#v, %v), want zero unsupported error", options, plan, err)
		}
	}
}

func TestPrunePlanAccessors_ReturnIndependentCopies(t *testing.T) {
	profile := ObservedProfile{orphans: []OrphanTarget{
		pruneTestOrphan(t, "~/target", "gone", StateSymlink, "/repo/source", Observation{
			Kind: ObjectRegular,
		}),
	}}
	plan, err := PlanPrune(profile, nil, PruneOptions{Enabled: true, Full: true})
	if err != nil {
		t.Fatalf("PlanPrune() error = %v", err)
	}
	actions := plan.Actions()
	groups := plan.ConfirmationGroups()
	actions[0].Precondition.Leaf.LinkDest = "changed"
	groups[0].Targets[0].Target = "changed"

	if got := plan.Actions()[0].Precondition.Leaf.LinkDest; got != "/repo/source" {
		t.Fatalf("mutating Actions() changed plan leaf destination to %q", got)
	}
	if got := plan.ConfirmationGroups()[0].Targets[0].Target; got != "~/target" {
		t.Fatalf("mutating ConfirmationGroups() changed plan target to %q", got)
	}
}

func pruneTestOrphan(
	t *testing.T,
	key, module string,
	kind StateKind,
	linkDest string,
	observed Observation,
) OrphanTarget {
	t.Helper()
	targetPath := filepath.Join(t.TempDir(), filepath.Base(key))
	resolution, err := paths.ResolveTarget(targetPath)
	if err != nil {
		t.Fatalf("paths.ResolveTarget(%q) error = %v", targetPath, err)
	}
	return OrphanTarget{
		TargetPath: targetPath,
		Resolution: resolution,
		State: HistoricalState{
			Key:      key,
			Module:   module,
			Kind:     kind,
			Source:   "modules/" + module + "/config",
			LinkDest: linkDest,
		},
		Observed: observed,
	}
}

func pruneActionTargets(actions []PruneAction) []string {
	targets := make([]string, len(actions))
	for index, action := range actions {
		targets[index] = action.Target
	}
	return targets
}
