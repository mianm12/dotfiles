package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
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
			assertSnapshotUnchanged(t, before)
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
		stdout != "" ||
		!strings.Contains(stderr, "target paths conflict") {
		t.Fatalf("full apply = (%d, %q, %q), want target conflict", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, filepath.Join(fixture.home, ".selected"))
	assertCLIMissing(t, filepath.Join(fixture.home, ".shared"))
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
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
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("apply")

	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "target paths conflict") {
		t.Fatalf("full apply = (%d, %q, %q), want target conflict", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 || extras[0] != "selected" {
		t.Fatalf("extra_modules = %v, want unchanged [selected]", extras)
	}
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
	assertCLIMissing(t, filepath.Join(fixture.home, ".tree"))
}

func TestFullApplyRejectsStateOwnedParentLinkBeforeMutation(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "active", `
[[links]]
id = "child"
source = "config"
target = "~/.shared/child"
`, map[string]string{"config": "active"})
	fixture.writeMachine(t, []string{"base"}, []string{"active"})

	oldTree := filepath.Join(fixture.root, "old-repository", "modules", "stale", "tree")
	if err := os.MkdirAll(oldTree, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", oldTree, err)
	}
	parentTarget := filepath.Join(fixture.home, ".shared")
	if err := os.Symlink(oldTree, parentTarget); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", oldTree, parentTarget, err)
	}
	resolvedParent, err := corepaths.ResolveAbsoluteTarget(fixture.home, parentTarget)
	if err != nil {
		t.Fatalf("ResolveAbsoluteTarget(parent) error = %v", err)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"stale": {Placements: map[string]state.Placement{
				"tree": {
					Kind:            state.KindLink,
					Target:          parentTarget,
					ResolvedTarget:  resolvedParent.Resolved(),
					LinkDestination: oldTree,
				},
			}},
		},
	})
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("apply")

	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "traverses state-owned link") {
		t.Fatalf(
			"full apply = (%d, %q, %q), want parent ownership conflict",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 || extras[0] != "active" {
		t.Fatalf("extra_modules = %v, want unchanged [active]", extras)
	}
	assertCLIMissing(t, filepath.Join(oldTree, "child"))
	assertCLIMissing(t, fixture.lock)
}

func TestFullApplyRejectsParentUpdateTraversedByEffectiveChild(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["parent", "child"]`)
	fixture.writeModule(t, "parent", `
[[links]]
id = "tree"
source = "new"
target = "~/owned"
`, map[string]string{
		"old/keep": "old",
		"new/keep": "new",
	})
	fixture.writeModule(t, "child", `
[[links]]
id = "config"
source = "config"
target = "~/access/child"
`, map[string]string{"config": "child"})
	fixture.writeMachine(t, []string{"base"}, nil)

	parentRoot := filepath.Join(fixture.repository, "modules", "parent")
	oldSource := filepath.Join(parentRoot, "old")
	newSource := filepath.Join(parentRoot, "new")
	outside := filepath.Join(fixture.root, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", outside, err)
	}
	if err := os.Symlink(outside, filepath.Join(oldSource, "out")); err != nil {
		t.Fatalf("os.Symlink(parent internal link) error = %v", err)
	}
	parentTarget := filepath.Join(fixture.home, "owned")
	if err := os.Symlink(oldSource, parentTarget); err != nil {
		t.Fatalf("os.Symlink(parent target) error = %v", err)
	}
	if err := os.Symlink(
		filepath.Join(parentTarget, "out"),
		filepath.Join(fixture.home, "access"),
	); err != nil {
		t.Fatalf("os.Symlink(access) error = %v", err)
	}
	childSource := filepath.Join(fixture.repository, "modules", "child", "config")
	childTarget := filepath.Join(outside, "child")
	if err := os.Symlink(childSource, childTarget); err != nil {
		t.Fatalf("os.Symlink(child target) error = %v", err)
	}
	resolvedParent, err := corepaths.ResolveAbsoluteTarget(
		fixture.home,
		parentTarget,
	)
	if err != nil {
		t.Fatalf("ResolveAbsoluteTarget(parent) error = %v", err)
	}
	lexicalChild := filepath.Join(fixture.home, "access", "child")
	resolvedChild, err := corepaths.ResolveAbsoluteTarget(
		fixture.home,
		lexicalChild,
	)
	if err != nil {
		t.Fatalf("ResolveAbsoluteTarget(child) error = %v", err)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"parent": {Placements: map[string]state.Placement{
				"tree": {
					Kind:            state.KindLink,
					Target:          parentTarget,
					ResolvedTarget:  resolvedParent.Resolved(),
					LinkDestination: oldSource,
				},
			}},
			"child": {Placements: map[string]state.Placement{
				"config": {
					Kind:            state.KindLink,
					Target:          lexicalChild,
					ResolvedTarget:  resolvedChild.Resolved(),
					LinkDestination: childSource,
				},
			}},
		},
	})
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("apply")

	if code != exitError ||
		stdout != "" ||
		(!strings.Contains(stderr, "active link cannot be owned or changed while traversed") &&
			!strings.Contains(stderr, "target traverses state-owned link")) {
		t.Fatalf(
			"full parent update = (%d, %q, %q), want traversal conflict",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLILink(t, parentTarget, oldSource)
	assertCLILink(t, childTarget, childSource)
	assertCLIMissing(t, filepath.Join(newSource, "out"))
	assertCLIMissing(t, fixture.lock)
}

func TestFullApplyThroughDriftedParentConvergesAndForgetsStaleState(
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
	resolvedParent, err := corepaths.ResolveAbsoluteTarget(fixture.home, parentTarget)
	if err != nil {
		t.Fatalf("ResolveAbsoluteTarget(parent) error = %v", err)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"stale": {Placements: map[string]state.Placement{
				"tree": {
					Kind:            state.KindLink,
					Target:          parentTarget,
					ResolvedTarget:  resolvedParent.Resolved(),
					LinkDestination: recordedTree,
				},
			}},
		},
	})

	code, stdout, stderr := fixture.run("apply")

	if code != exitOK ||
		!strings.Contains(stderr, "forgot ownership") ||
		!strings.Contains(stdout, "targets_changed=true state_changed=true") {
		t.Fatalf(
			"full apply through drifted parent = (%d, %q, %q)",
			code,
			stdout,
			stderr,
		)
	}
	destination := filepath.Join(fixture.repository, "modules", "active", "config")
	assertCLILink(t, filepath.Join(userTree, "child"), destination)
	loaded := loadTestState(t, fixture)
	if _, exists := loaded.Modules["stale"]; exists {
		t.Fatal("full apply retained stale state")
	}
	if _, exists := loaded.Modules["active"].Placements["child"]; !exists {
		t.Fatal("full apply did not record active child")
	}

	assertApplyNoMutation(t, fixture, fixture.run)
}

func TestFullApplyRejectsStaleOwnedParentUsedByActiveDesired(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["active"]`)
	fixture.writeModule(t, "active", `
[[links]]
id = "child"
source = "config"
target = "~/.shared/child"
`, map[string]string{"config": "active"})
	fixture.writeMachine(t, []string{"base"}, nil)

	oldTree := filepath.Join(fixture.root, "old-repository", "tree")
	if err := os.MkdirAll(oldTree, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", oldTree, err)
	}
	parentTarget := filepath.Join(fixture.home, ".shared")
	if err := os.Symlink(oldTree, parentTarget); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", oldTree, parentTarget, err)
	}
	resolvedParent, err := corepaths.ResolveAbsoluteTarget(fixture.home, parentTarget)
	if err != nil {
		t.Fatalf("ResolveAbsoluteTarget(parent) error = %v", err)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"stale": {Placements: map[string]state.Placement{
				"tree": {
					Kind:            state.KindLink,
					Target:          parentTarget,
					ResolvedTarget:  resolvedParent.Resolved(),
					LinkDestination: oldTree,
				},
			}},
		},
	})
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("apply")

	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "traverses state-owned link") {
		t.Fatalf(
			"full apply = (%d, %q, %q), want dependency conflict",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, filepath.Join(oldTree, "child"))
	assertCLIMissing(t, fixture.lock)
}
