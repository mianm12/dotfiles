package planner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/state"
)

func TestObserveProfileTargets_MatchesHistoricalAliasAndKeepsOrphans(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	realRoot := filepath.Join(home, "real")
	if err := os.MkdirAll(realRoot, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", realRoot, err)
	}
	aliasRoot := filepath.Join(home, "alias")
	if err := os.Symlink("real", aliasRoot); err != nil {
		t.Fatalf("os.Symlink(real, %q) error = %v", aliasRoot, err)
	}
	currentTarget := filepath.Join(aliasRoot, "config")
	if err := os.Symlink("../../repository/source", currentTarget); err != nil {
		t.Fatalf("os.Symlink(current target) error = %v", err)
	}
	orphanTarget := filepath.Join(home, "orphan")
	if err := os.WriteFile(orphanTarget, []byte("user scaffold\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(orphan) error = %v", err)
	}
	loaded := loadPlannerState(t, root, `{
  "version": 1,
  "entries": {
    "~/real/config": {
      "module": "app",
      "kind": "symlink",
      "source": "modules/app/old",
      "link_dest": "../../repository/source",
      "applied_at": "2026-07-19T00:00:00Z"
    },
    "~/orphan": {
      "module": "old",
      "kind": "scaffold",
      "source": "modules/old/orphan.template",
      "applied_at": "2026-07-19T00:00:00Z"
    }
  },
  "run_once": {}
}`)
	desired := []manifest.DesiredEntry{{
		Module:     "app",
		Source:     "config",
		SourcePath: filepath.Join(root, "repository", "source"),
		Target:     "~/alias/config",
		TargetPath: currentTarget,
		Kind:       manifest.FileKindLink,
	}}
	before := snapshotObservationTree(t, root)

	observed, err := ObserveProfileTargets(home, desired, loaded)
	if err != nil {
		t.Fatalf("ObserveProfileTargets() error = %v", err)
	}
	targets := observed.Targets()
	orphans := observed.Orphans()
	if len(targets) != 1 || !targets[0].HasState || targets[0].State.Key != "~/real/config" {
		t.Fatalf("Targets() = %#v, want one desired matched to historical alias", targets)
	}
	if targets[0].Desired.Target != "~/alias/config" || targets[0].Observed.Kind != ObjectSymlink ||
		targets[0].Observed.LinkDest != "../../repository/source" {
		t.Fatalf("matched target = %#v, want current desired display and raw link", targets[0])
	}
	if len(orphans) != 1 || orphans[0].State.Key != "~/orphan" || orphans[0].Observed.Kind != ObjectRegular {
		t.Fatalf("Orphans() = %#v, want only historical orphan", orphans)
	}
	wantOrphanResolution, err := paths.ResolveTarget(orphanTarget)
	if err != nil {
		t.Fatalf("paths.ResolveTarget(orphan) error = %v", err)
	}
	if !orphans[0].Resolution.Equal(wantOrphanResolution) {
		t.Fatalf("orphan resolution does not match plan-time target identity: %#v", orphans[0])
	}
	if after := snapshotObservationTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("ObserveProfileTargets() changed fixture: before=%v after=%v", before, after)
	}

	targets[0].Observed.LinkDest = "changed"
	orphans[0].Observed.Hash = "changed"
	orphans[0].Resolution = paths.TargetResolution{}
	againTargets := observed.Targets()
	againOrphans := observed.Orphans()
	if againTargets[0].Observed.LinkDest != "../../repository/source" || againOrphans[0].Observed.Hash != "" {
		t.Fatalf("mutating accessors changed observed set: targets=%#v orphans=%#v", againTargets, againOrphans)
	}
	if !againOrphans[0].Resolution.Equal(wantOrphanResolution) {
		t.Fatalf("mutating orphan resolution copy changed observed set: %#v", againOrphans)
	}
}

func TestObserveProfileTargets_RejectsMultipleHistoricalAliases(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	realRoot := filepath.Join(home, "real")
	if err := os.MkdirAll(realRoot, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", realRoot, err)
	}
	if err := os.Symlink("real", filepath.Join(home, "alias")); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	loaded := loadPlannerState(t, root, `{
  "version": 1,
  "entries": {
    "~/real/config": {
      "module": "app",
      "kind": "scaffold",
      "source": "modules/app/config.template",
      "applied_at": "2026-07-19T00:00:00Z"
    },
    "~/alias/config": {
      "module": "app",
      "kind": "scaffold",
      "source": "modules/app/config.template",
      "applied_at": "2026-07-19T00:00:00Z"
    }
  },
  "run_once": {}
}`)

	observed, err := ObserveProfileTargets(home, nil, loaded)
	if !errors.Is(err, state.ErrCorrupt) || !errors.Is(err, state.ErrTargetIdentityConflict) {
		t.Fatalf("ObserveProfileTargets() = (%#v, %v), want corrupt target identity conflict", observed, err)
	}
	for _, want := range []string{"~/alias/config", "~/real/config"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ObserveProfileTargets() error = %q, want key %q", err, want)
		}
	}
	if observed.Targets() != nil || observed.Orphans() != nil {
		t.Fatalf("ObserveProfileTargets() returned partial result: %#v", observed)
	}
}

func TestObserveProfileTargets_MissingStateAndTargetAreValid(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(home) error = %v", err)
	}
	loaded, err := state.Load(filepath.Join(root, "missing-state.json"))
	if err != nil || !loaded.Missing() {
		t.Fatalf("state.Load(missing) = (%#v, %v), want missing", loaded, err)
	}
	desired := []manifest.DesiredEntry{{
		Module:     "app",
		Source:     "config",
		SourcePath: filepath.Join(root, "repository", "config"),
		Target:     "~/config",
		TargetPath: filepath.Join(home, "config"),
		Kind:       manifest.FileKindLink,
	}}

	observed, err := ObserveProfileTargets(home, desired, loaded)
	if err != nil {
		t.Fatalf("ObserveProfileTargets() error = %v", err)
	}
	targets := observed.Targets()
	if len(targets) != 1 || targets[0].HasState || targets[0].Observed.Kind != ObjectMissing || len(observed.Orphans()) != 0 {
		t.Fatalf("ObserveProfileTargets() = %#v, want missing target without state", observed)
	}
}

func TestObserveProfileTargets_RejectsUnsupportedDesiredBeforeObservation(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(home) error = %v", err)
	}
	target := filepath.Join(home, "target")
	if err := os.Symlink("raw-destination", target); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	loaded, err := state.Load(filepath.Join(root, "missing-state.json"))
	if err != nil {
		t.Fatalf("state.Load(missing) error = %v", err)
	}
	desired := []manifest.DesiredEntry{{
		Module:     "app",
		Source:     "config.tmpl",
		SourcePath: filepath.Join(root, "repository", "config.tmpl"),
		Target:     "~/target",
		TargetPath: target,
		Kind:       manifest.FileKind("managed"),
	}}
	before := snapshotObservationTree(t, root)

	observed, err := ObserveProfileTargets(home, desired, loaded)
	if err == nil || !strings.Contains(err.Error(), "unsupported desired kind") {
		t.Fatalf("ObserveProfileTargets() = (%#v, %v), want unsupported desired error", observed, err)
	}
	if after := snapshotObservationTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("unsupported desired changed fixture: before=%v after=%v", before, after)
	}
}

func loadPlannerState(t *testing.T, root, document string) state.Loaded {
	t.Helper()
	path := filepath.Join(root, fmt.Sprintf("state-%d.json", len(document)))
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("os.WriteFile(state) error = %v", err)
	}
	loaded, err := state.Load(path)
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}
	return loaded
}
