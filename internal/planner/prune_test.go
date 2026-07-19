package planner

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mianm12/dotfiles/internal/paths"
)

func TestPlanOrphanPrune_P1P2P3(t *testing.T) {
	resolution := pruneTestResolution(t)
	tests := []struct {
		name              string
		stateKind         StateKind
		stateLink         string
		observed          Observation
		wantMode          PruneMode
		wantReason        PruneReason
		wantWarning       bool
		wantDeletesTarget bool
	}{
		{
			name:        "P1 scaffold removes only state",
			stateKind:   StateScaffold,
			observed:    Observation{Kind: ObjectRegular, Content: []byte("user data")},
			wantMode:    PruneStateOnly,
			wantReason:  PruneReasonScaffold,
			wantWarning: false,
		},
		{
			name:              "P2 owned symlink removes target and state",
			stateKind:         StateSymlink,
			stateLink:         "/repo/source",
			observed:          Observation{Kind: ObjectSymlink, LinkDest: "/repo/source"},
			wantMode:          PruneTargetAndState,
			wantReason:        PruneReasonOwned,
			wantWarning:       false,
			wantDeletesTarget: true,
		},
		{
			name:        "P3 re-pointed symlink preserves target and warns",
			stateKind:   StateSymlink,
			stateLink:   "/repo/source",
			observed:    Observation{Kind: ObjectSymlink, LinkDest: "/user/source"},
			wantMode:    PruneStateOnly,
			wantReason:  PruneReasonUnowned,
			wantWarning: true,
		},
		{
			name:        "P3 missing target removes stale state and warns",
			stateKind:   StateSymlink,
			stateLink:   "/repo/source",
			observed:    Observation{Kind: ObjectMissing},
			wantMode:    PruneStateOnly,
			wantReason:  PruneReasonUnowned,
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orphan := OrphanTarget{
				TargetPath: filepath.Join(t.TempDir(), "target"),
				Resolution: resolution,
				State: HistoricalState{
					Key:      "~/.config/app",
					Module:   "app",
					Kind:     tt.stateKind,
					Source:   "modules/app/config",
					LinkDest: tt.stateLink,
				},
				Observed: tt.observed,
			}

			action, err := planOrphanPrune(orphan, false)
			if err != nil {
				t.Fatalf("planOrphanPrune() error = %v", err)
			}
			if action.Mode != tt.wantMode || action.Reason != tt.wantReason || action.Warning != tt.wantWarning {
				t.Fatalf("planOrphanPrune() = %#v, want mode=%q reason=%q warning=%t", action, tt.wantMode, tt.wantReason, tt.wantWarning)
			}
			if action.Deferred || action.DeletesTarget() != tt.wantDeletesTarget {
				t.Fatalf("planOrphanPrune() deferred/deletes = (%t, %t), want (false, %t)", action.Deferred, action.DeletesTarget(), tt.wantDeletesTarget)
			}
			if action.Target != orphan.State.Key || action.Module != orphan.State.Module {
				t.Fatalf("planOrphanPrune() target/module = (%q, %q), want (%q, %q)", action.Target, action.Module, orphan.State.Key, orphan.State.Module)
			}
			if action.Precondition.TargetPath != orphan.TargetPath ||
				!action.Precondition.TargetResolution.Equal(resolution) ||
				action.Precondition.Observed.Kind != orphan.Observed.Kind ||
				action.Precondition.Observed.LinkDest != orphan.Observed.LinkDest {
				t.Fatalf("planOrphanPrune() precondition = %#v, want plan-time orphan facts", action.Precondition)
			}
			if action.OnSuccess.Kind != StateDelete || action.OnSuccess.Key != orphan.State.Key {
				t.Fatalf("planOrphanPrune() success = %#v, want delete state key", action.OnSuccess)
			}
			if action.OnFailure.Kind != StatePreserve {
				t.Fatalf("planOrphanPrune() failure = %#v, want preserve", action.OnFailure)
			}
		})
	}
}

func TestPlanOrphanPrune_DeferredPreservesStateAndIsNotExecutable(t *testing.T) {
	orphan := OrphanTarget{
		TargetPath: filepath.Join(t.TempDir(), "target"),
		Resolution: pruneTestResolution(t),
		State: HistoricalState{
			Key:      "~/target",
			Module:   "app",
			Kind:     StateSymlink,
			LinkDest: "/repo/source",
		},
		Observed: Observation{Kind: ObjectSymlink, LinkDest: "/repo/source"},
	}

	action, err := planOrphanPrune(orphan, true)
	if err != nil {
		t.Fatalf("planOrphanPrune(deferred) error = %v", err)
	}
	if !action.Deferred || action.DeferredReason != PruneDeferredFileConflict || action.DeletesTarget() {
		t.Fatalf("planOrphanPrune(deferred) = %#v, want explicit non-executable conflict defer", action)
	}
	if action.Mode != PruneTargetAndState || action.Reason != PruneReasonOwned {
		t.Fatalf("deferred action lost underlying P2 classification: %#v", action)
	}
	if action.OnSuccess.Kind != StatePreserve || action.OnFailure.Kind != StatePreserve {
		t.Fatalf("deferred state effects = (%#v, %#v), want preserve/preserve", action.OnSuccess, action.OnFailure)
	}
}

func TestPlanOrphanPrune_RejectsUnsupportedStateKind(t *testing.T) {
	action, err := planOrphanPrune(OrphanTarget{
		TargetPath: filepath.Join(t.TempDir(), "target"),
		Resolution: pruneTestResolution(t),
		State: HistoricalState{
			Key:    "~/target",
			Module: "app",
			Kind:   StateKind("rendered"),
		},
		Observed: Observation{Kind: ObjectRegular},
	}, false)
	if !errors.Is(err, ErrUnsupportedPruneInput) || !reflect.DeepEqual(action, PruneAction{}) {
		t.Fatalf("planOrphanPrune() = (%#v, %v), want zero unsupported error", action, err)
	}
}

func pruneTestResolution(t *testing.T) paths.TargetResolution {
	t.Helper()
	resolution, err := paths.ResolveTarget(filepath.Join(t.TempDir(), "target"))
	if err != nil {
		t.Fatalf("paths.ResolveTarget() error = %v", err)
	}
	return resolution
}
