package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestFullApplyAndSelectionIgnoreBrokenInactiveModule(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "apply-good", `
[[links]]
id = "config"
source = "config"
target = "~/.apply-good"
`, map[string]string{"config": "apply-good"})
	fixture.writeModule(t, "remove-good", `
[[links]]
id = "config"
source = "config"
target = "~/.remove-good"
`, map[string]string{"config": "remove-good"})
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "modules", "broken", "module.toml"),
		"unknown = true\n",
	)
	fixture.writeMachine(t, []string{"base"}, []string{"apply-good", "remove-good"})

	code, _, stderr := fixture.run("apply")
	if code != exitOK {
		t.Fatalf("full apply with broken inactive module = (%d, %q)", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".apply-good"),
		filepath.Join(fixture.repository, "modules", "apply-good", "config"),
	)
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".remove-good"),
		filepath.Join(fixture.repository, "modules", "remove-good", "config"),
	)
	assertApplyNoMutation(t, fixture, fixture.run)

	code, _, stderr = fixture.run("select", "remove", "remove-good")
	if code != exitOK {
		t.Fatalf("remove with broken inactive module = (%d, %q)", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".remove-good"),
		filepath.Join(fixture.repository, "modules", "remove-good", "config"),
	)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 ||
		extras[0] != "apply-good" {
		t.Fatalf("extra_modules = %v, want [apply-good]", extras)
	}
	if code, _, stderr := fixture.run("apply"); code != exitOK {
		t.Fatalf("apply after selection change = (%d, %q)", code, stderr)
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".remove-good"))
	assertApplyNoMutation(t, fixture, fixture.run)
}

func TestFullAnalysisFailsClosedOnMalformedEffectiveManifest(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "good", `
[[links]]
id = "config"
source = "config"
target = "~/.good"
`, map[string]string{"config": "good"})
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "modules", "broken", "module.toml"),
		"unknown = true\n",
	)
	fixture.writeMachine(t, []string{"base"}, []string{"broken", "good"})

	for _, args := range [][]string{
		{"apply"},
		{"apply", "--dry-run"},
		{"status"},
	} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			before := snapshotTree(t, fixture.root)
			code, stdout, stderr := fixture.run(args...)
			if code != exitError || stdout != "" || !strings.Contains(stderr, "broken") {
				t.Fatalf(
					"%v with malformed effective module = (%d, %q, %q), want fail-closed input error",
					args,
					code,
					stdout,
					stderr,
				)
			}
			if len(args) == 1 && args[0] == "apply" {
				assertOnlyLockBookkeepingChanged(t, before, fixture)
				assertCLIMissing(t, fixture.state)
			} else {
				assertSnapshotUnchanged(t, before)
			}
		})
	}
}

func TestFullApplyRejectsNestedTargetsAcrossEffectiveModules(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["first", "second"]`)
	fixture.writeModule(t, "first", `
[[links]]
id = "parent"
source = "config"
target = "~/.shared"
`, map[string]string{"config": "first"})
	fixture.writeModule(t, "second", `
[[links]]
id = "child"
source = "config"
target = "~/.shared/child"
`, map[string]string{"config": "second"})
	fixture.writeModule(t, "selected", `
[[links]]
id = "config"
source = "config"
target = "~/.selected"
`, map[string]string{"config": "selected"})
	fixture.writeMachine(t, []string{"base"}, []string{"selected"})

	before := snapshotTree(t, fixture.root)
	code, stdout, stderr := fixture.run("apply")
	if code != exitError ||
		!strings.Contains(stdout, "skip") ||
		!strings.Contains(stdout, "skip") ||
		!strings.Contains(stderr, "state is missing") ||
		strings.Contains(stderr, "error:") {
		t.Fatalf("full apply = (%d, %q, %q), want blocked target conflict", code, stdout, stderr)
	}
	assertOnlyLockBookkeepingChanged(t, before, fixture)
	assertCLIMissing(t, filepath.Join(fixture.home, ".selected"))
	assertCLIMissing(t, filepath.Join(fixture.home, ".shared"))
	assertCLIMissing(t, fixture.state)
}

func TestFullApplyRejectsTargetRelationshipBeforeMutation(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["effective"]`)
	fixture.writeModule(t, "effective", `
[[links]]
id = "parent"
source = "config"
target = "~/.tree"
`, map[string]string{"config": "effective"})
	fixture.writeModule(t, "selected", `
[[locals]]
id = "child"
example = "config.local.example"
target = "~/.tree/child"
`, map[string]string{"config.local.example": "selected"})
	fixture.writeMachine(t, []string{"base"}, []string{"selected"})
	writeCLIFile(t, filepath.Join(fixture.home, ".tree"), "user")
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("status")
	if code != exitOK ||
		!strings.Contains(stdout, "fact module=effective selection=profile") ||
		!strings.Contains(stdout, "fact module=selected selection=extra") ||
		!strings.Contains(stdout, "skip module=effective") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf("full status = (%d, %q, %q), want target conflict report", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.run("apply", "--dry-run")
	if code != exitError ||
		!strings.Contains(stdout, "skip") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf("full dry-run = (%d, %q, %q), want target conflict report", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.run("apply")

	if code != exitError ||
		!strings.Contains(stdout, "skip") ||
		!strings.Contains(stdout, "skip") ||
		!strings.Contains(stderr, "state is missing") ||
		strings.Contains(stderr, "error:") {
		t.Fatalf("full apply = (%d, %q, %q), want blocked target conflict", code, stdout, stderr)
	}
	assertOnlyLockBookkeepingChanged(t, before, fixture)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 || extras[0] != "selected" {
		t.Fatalf("extra_modules = %v, want unchanged [selected]", extras)
	}
	assertCLIMissing(t, fixture.state)
	if content, err := os.ReadFile(filepath.Join(fixture.home, ".tree")); err != nil || string(content) != "user" {
		t.Fatalf("ordinary parent changed: content=%q error=%v", content, err)
	}
}

func TestFullApplyRequiresTwoStagesForDriftedStaleParentAndDesiredChild(
	t *testing.T,
) {
	fixture := newCLITestEnv(t, `base = ["active"]`)
	fixture.writeModule(t, "active", `
[[links]]
id = "child"
source = "config"
target = "~/.shared/child"
`, map[string]string{"config": "active"})
	fixture.writeMachine(t, []string{"base"}, nil)

	recordedTree := filepath.Join(fixture.root, "old-repository", "tree")
	userTree := filepath.Join(fixture.root, "user", "tree")
	for _, directory := range []string{recordedTree, userTree} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	parentTarget := filepath.Join(fixture.home, ".shared")
	if err := os.Symlink(userTree, parentTarget); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", userTree, parentTarget, err)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "stale", PlacementID: "tree"}: {
				Target: ".shared",
				Dest:   recordedTree,
			},
		},
	})
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("status")
	if code != exitOK || strings.Count(stdout, "skip ") != 2 || stderr != "" {
		t.Fatalf(
			"status with related stale target = (%d, %q, %q)",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.run("apply", "--dry-run")
	if code != exitError || strings.Count(stdout, "skip ") != 2 || stderr != "" {
		t.Fatalf("dry-run = (%d, %q, %q), want two-stage refusal", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.run("apply")
	if code != exitError || strings.Count(stdout, "skip ") != 2 || stderr != "" {
		t.Fatalf("apply = (%d, %q, %q), want two-stage refusal", code, stdout, stderr)
	}
	assertOnlyLockBookkeepingChanged(t, before, fixture)
	if destination, err := os.Readlink(parentTarget); err != nil || destination != userTree {
		t.Fatalf("stale parent = (%q, %v), want preserved", destination, err)
	}
	assertCLIMissing(t, filepath.Join(userTree, "child"))
	loaded := loadTestState(t, fixture)
	if got, exists := loaded.Links[state.Key{ModuleID: "stale", PlacementID: "tree"}]; !exists || got.Target != ".shared" || got.Dest != recordedTree {
		t.Fatalf("stale state = (%#v, %t), want preserved", got, exists)
	}
}
