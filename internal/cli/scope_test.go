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
