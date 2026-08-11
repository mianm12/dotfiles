package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWritesOnlyMachineConfig(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})

	code, stdout, stderr := fixture.runInjected(
		"init",
		fixture.repository,
		"--profile",
		"base",
	)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "run dot apply") {
		t.Fatalf("init = (%d, %q, %q), want config-only success", code, stdout, stderr)
	}
	machine := fixture.loadMachine(t)
	if len(machine.Profiles) != 1 || machine.Profiles[0] != "base" ||
		len(machine.ExtraModules) != 0 {
		t.Fatalf("machine selection = %#v, want profile base and no extras", machine)
	}
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, filepath.Join(fixture.home, ".app"))

	code, _, stderr = fixture.runInjected("apply")
	if code != exitOK {
		t.Fatalf("apply after init = (%d, %q)", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".app"),
		filepath.Join(fixture.repository, "modules", "app", "config"),
	)
	assertApplyNoMutation(t, fixture, fixture.runInjected)
}

func TestInitWithoutProfilesCreatesEmptySelection(t *testing.T) {
	fixture := newCLITestEnv(t, "")

	code, stdout, stderr := fixture.runInjected("init", fixture.repository)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "run dot apply") {
		t.Fatalf("init without profiles = (%d, %q, %q)", code, stdout, stderr)
	}
	machine := fixture.loadMachine(t)
	if len(machine.Profiles) != 0 || len(machine.ExtraModules) != 0 {
		t.Fatalf("machine selection = %#v, want empty profiles and extras", machine)
	}
	assertCLIMissing(t, fixture.state)
}

func TestInitValidationFailureIsStrictlyReadOnly(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["missing"]`)
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run(
		"init",
		fixture.repository,
		"--profile",
		"base",
	)
	if code != exitError || stdout != "" || !strings.Contains(stderr, "missing module") {
		t.Fatalf("init = (%d, %q, %q), want profile validation failure", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.config)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
}

func TestInitRejectsExplicitEmptyRepositoryWithoutMutation(t *testing.T) {
	fixture := newCLITestEnv(t, "base = []")
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runProcessAt(
		fixture.repository,
		"init",
		"",
		"--profile",
		"base",
	)
	if code != exitError || stdout != "" ||
		!strings.Contains(stderr, "repository") || !strings.Contains(stderr, "non-empty") {
		t.Fatalf("init empty repository = (%d, %q, %q)", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
}

func TestInitRejectsDryRunAsUsageError(t *testing.T) {
	fixture := newCLITestEnv(t, "base = []")
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("init", fixture.repository, "--dry-run")
	if code != exitUsage || stdout != "" || stderr == "" {
		t.Fatalf("init --dry-run = (%d, %q, %q), want usage error", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
}
