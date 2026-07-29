package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestScopedApplyAndRemoveIgnoreBrokenOutOfScopeModule(t *testing.T) {
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
	fixture.writeMachine(t, []string{"base"}, []string{"remove-good"})

	code, _, stderr := fixture.run("apply", "apply-good")
	if code != exitOK {
		t.Fatalf("scoped apply with broken out-of-scope module = (%d, %q)", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".apply-good"),
		filepath.Join(fixture.repository, "modules", "apply-good", "config"),
	)
	assertApplyNoMutation(t, fixture, fixture.run, "apply-good")

	code, _, stderr = fixture.run("apply", "remove-good")
	if code != exitOK {
		t.Fatalf("scoped apply remove-good = (%d, %q)", code, stderr)
	}
	assertApplyNoMutation(t, fixture, fixture.run, "remove-good")

	code, _, stderr = fixture.run("remove", "remove-good")
	if code != exitOK {
		t.Fatalf("remove with broken out-of-scope module = (%d, %q)", code, stderr)
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".remove-good"))
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 ||
		extras[0] != "apply-good" {
		t.Fatalf("extra_modules = %v, want [apply-good]", extras)
	}
	assertApplyNoMutation(t, fixture, fixture.run)

	before := snapshotTree(t, fixture.root)
	code, stdout, stderr := fixture.run("apply", "broken")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, `module "broken"`) {
		t.Fatalf("explicit broken apply = (%d, %q, %q), want strict failure", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
}

func TestScopedApplyIgnoresNestedTargetsBetweenOtherEffectiveModules(t *testing.T) {
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
	fixture.writeMachine(t, []string{"base"}, nil)

	code, stdout, stderr := fixture.run("apply", "selected")

	if code != exitOK || !strings.Contains(stderr, "state is missing") {
		t.Fatalf("scoped apply = (%d, %q, %q), want success", code, stdout, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".selected"),
		filepath.Join(fixture.repository, "modules", "selected", "config"),
	)
	assertCLIMissing(t, filepath.Join(fixture.home, ".shared"))
	extras := fixture.loadMachine(t).ExtraModules
	if len(extras) != 1 || extras[0] != "selected" {
		t.Fatalf("extra_modules = %v, want [selected]", extras)
	}
	assertApplyNoMutation(t, fixture, fixture.run, "selected")

	beforeFull := snapshotTree(t, fixture.root)
	code, stdout, stderr = fixture.run("apply")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "target paths conflict") {
		t.Fatalf("full apply = (%d, %q, %q), want unrelated target conflict", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, beforeFull)
}

func TestScopedApplyRejectsTargetRelationshipBeforeSelectionMutation(t *testing.T) {
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
	fixture.writeMachine(t, []string{"base"}, nil)
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("apply", "selected")

	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "target paths conflict") {
		t.Fatalf("scoped apply = (%d, %q, %q), want target conflict", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged empty selection", extras)
	}
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
	assertCLIMissing(t, filepath.Join(fixture.home, ".tree"))
}

func TestScopedApplyRejectsStateOwnedParentLinkBeforeMutation(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "active", `
[[links]]
id = "child"
source = "config"
target = "~/.shared/child"
`, map[string]string{"config": "active"})
	fixture.writeMachine(t, []string{"base"}, nil)

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

	code, stdout, stderr := fixture.run("apply", "active")

	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "traverses state-owned link") {
		t.Fatalf(
			"scoped apply = (%d, %q, %q), want parent ownership conflict",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged empty selection", extras)
	}
	assertCLIMissing(t, filepath.Join(oldTree, "child"))
	assertCLIMissing(t, fixture.lock)
}

func TestScopedApplyThroughDriftedParentConvergesWithoutCleaningOtherState(
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

	code, stdout, stderr := fixture.run("apply", "active")

	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "targets_changed=true state_changed=true") {
		t.Fatalf(
			"scoped apply through drifted parent = (%d, %q, %q)",
			code,
			stdout,
			stderr,
		)
	}
	destination := filepath.Join(fixture.repository, "modules", "active", "config")
	assertCLILink(t, filepath.Join(userTree, "child"), destination)
	loaded := loadTestState(t, fixture)
	if _, exists := loaded.Modules["stale"]; !exists {
		t.Fatal("scoped apply removed scope-out stale state")
	}
	if _, exists := loaded.Modules["active"].Placements["child"]; !exists {
		t.Fatal("scoped apply did not record active child")
	}

	assertApplyNoMutation(t, fixture, fixture.run, "active")
}

func TestScopedRemoveRejectsOwnedParentUsedByOutOfScopeDesired(t *testing.T) {
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

	code, stdout, stderr := fixture.run("remove", "stale")

	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "state-owned link is traversed by active module") {
		t.Fatalf(
			"scoped remove = (%d, %q, %q), want out-of-scope dependency conflict",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, filepath.Join(oldTree, "child"))
	assertCLIMissing(t, fixture.lock)
}
