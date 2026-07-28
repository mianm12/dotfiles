package cli

import (
	"path/filepath"
	"strings"
	"testing"
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
