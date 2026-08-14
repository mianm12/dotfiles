package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesMissingConfigRootPrivately(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	configRoot := filepath.Dir(fixture.config)
	if _, err := os.Lstat(configRoot); !os.IsNotExist(err) {
		t.Fatalf("config root before init error = %v, want missing", err)
	}

	code, stdout, stderr := fixture.runInjected(
		"init",
		fixture.repository,
		"--profile",
		"base",
	)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "selection_changed=true") {
		t.Fatalf("init = (%d, %q, %q), want success", code, stdout, stderr)
	}

	rootInfo, err := os.Lstat(configRoot)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode().Perm() != 0o700 {
		t.Fatalf("config root = (%v, %v), want real directory mode 0700", rootInfo, err)
	}
	configInfo, err := os.Lstat(fixture.config)
	if err != nil || !configInfo.Mode().IsRegular() || configInfo.Mode().Perm() != 0o600 {
		t.Fatalf("machine config = (%v, %v), want regular file mode 0600", configInfo, err)
	}
}

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

func TestInitWithoutProfilesUsesDefault(t *testing.T) {
	fixture := newCLITestEnv(t, `default = ["app"]`)
	fixture.writeModule(t, "app", "", nil)

	code, stdout, stderr := fixture.runInjected("init", fixture.repository)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "run dot apply") {
		t.Fatalf("init without profiles = (%d, %q, %q)", code, stdout, stderr)
	}
	machine := fixture.loadMachine(t)
	if len(machine.Profiles) != 1 || machine.Profiles[0] != "default" ||
		len(machine.ExtraModules) != 0 {
		t.Fatalf("machine selection = %#v, want default profile and no extras", machine)
	}
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, filepath.Join(fixture.home, ".app"))

	before := snapshotPaths(t, fixture.config)
	code, stdout, stderr = fixture.runInjected("init", fixture.repository)
	if code != exitOK || stderr != "" ||
		!strings.Contains(stdout, "selection_changed=false") {
		t.Fatalf("repeated init = (%d, %q, %q), want no-op", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
}

func TestInitExplicitProfilesOverrideDefaultAndAreCanonical(t *testing.T) {
	fixture := newCLITestEnv(t, `
default = ["missing"]
zeta = []
alpha = []
`)

	code, stdout, stderr := fixture.runInjected(
		"init",
		fixture.repository,
		"--profile",
		"zeta",
		"--profile",
		"alpha",
	)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "selection_changed=true") {
		t.Fatalf("explicit init = (%d, %q, %q), want success", code, stdout, stderr)
	}
	machine := fixture.loadMachine(t)
	if len(machine.Profiles) != 2 || machine.Profiles[0] != "alpha" ||
		machine.Profiles[1] != "zeta" {
		t.Fatalf("profiles = %v, want canonical [alpha zeta]", machine.Profiles)
	}
}

func TestInitRejectsMissingDefaultAndDuplicateProfiles(t *testing.T) {
	t.Run("missing default", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected("init", fixture.repository)
		if code != exitError || stdout != "" || !strings.Contains(stderr, "unknown profile") {
			t.Fatalf("init missing default = (%d, %q, %q), want profile error", code, stdout, stderr)
		}
		assertOnlyLockBookkeepingChanged(t, before, fixture)
		assertCLIMissing(t, fixture.config)
	})

	t.Run("duplicate explicit profile", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected(
			"init",
			fixture.repository,
			"--profile",
			"base",
			"--profile",
			"base",
		)
		if code != exitError || stdout != "" || !strings.Contains(stderr, "duplicate profile") {
			t.Fatalf("init duplicate profile = (%d, %q, %q), want duplicate error", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
	})
}

func TestInitEquivalentBindingIsNoOpAndPreservesExistingSelection(t *testing.T) {
	fixture := newCLITestEnv(t, "alpha = []\nbeta = []")
	fixture.writeMachine(t, []string{"beta", "alpha"}, []string{"special"})
	writeCLIFile(t, fixture.state, "not valid state")
	target := filepath.Join(fixture.home, ".personal")
	writeCLIFile(t, target, "personal")
	before := snapshotPaths(t, fixture.config, fixture.state, target)

	code, stdout, stderr := fixture.runInjected(
		"init",
		fixture.repository,
		"--profile",
		"alpha",
		"--profile",
		"beta",
	)
	if code != exitOK || stderr != "" ||
		!strings.Contains(stdout, "selection_changed=false") {
		t.Fatalf("equivalent init = (%d, %q, %q), want no-op", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	machine := fixture.loadMachine(t)
	if len(machine.Profiles) != 2 || machine.Profiles[0] != "beta" ||
		machine.Profiles[1] != "alpha" || len(machine.ExtraModules) != 1 ||
		machine.ExtraModules[0] != "special" {
		t.Fatalf("machine = %#v, want original order and extras preserved", machine)
	}
}

func TestInitRejectsDifferentBindingWithoutReconfiguration(t *testing.T) {
	t.Run("profiles", func(t *testing.T) {
		fixture := newCLITestEnv(t, "base = []\nother = []")
		fixture.writeMachine(t, []string{"base"}, []string{"special"})
		before := snapshotPaths(t, fixture.config)

		code, stdout, stderr := fixture.runInjected(
			"init",
			fixture.repository,
			"--profile",
			"other",
		)
		if code != exitError || stdout != "" || !strings.Contains(stderr, "already initialized") {
			t.Fatalf("init different profile = (%d, %q, %q), want rejection", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
	})

	t.Run("repository", func(t *testing.T) {
		fixture := newCLITestEnv(t, "base = []")
		fixture.writeMachine(t, []string{"base"}, []string{"special"})
		other := filepath.Join(fixture.root, "other-repository")
		writeCLIFile(t, filepath.Join(other, "dot.toml"), "version = 1\n[profiles]\nbase = []\n")
		before := snapshotPaths(t, fixture.config)

		code, stdout, stderr := fixture.runInjected(
			"init",
			other,
			"--profile",
			"base",
		)
		if code != exitError || stdout != "" || !strings.Contains(stderr, "already initialized") {
			t.Fatalf("init different repository = (%d, %q, %q), want rejection", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
	})
}

func TestInitNoOpRevalidatesCurrentRepositoryProfile(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeMachine(t, []string{"base"}, []string{"special"})
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "dot.toml"),
		"version = 1\n[profiles]\nbase = [\"missing\"]\n",
	)
	before := snapshotPaths(t, fixture.config)

	code, stdout, stderr := fixture.runInjected(
		"init",
		fixture.repository,
		"--profile",
		"base",
	)
	if code != exitError || stdout != "" || !strings.Contains(stderr, "missing module") {
		t.Fatalf("revalidating init = (%d, %q, %q), want repository error", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
}

func TestInitValidationFailureLeavesOnlyLockBookkeeping(t *testing.T) {
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
	assertOnlyLockBookkeepingChanged(t, before, fixture)
	assertCLIMissing(t, fixture.config)
	assertCLIMissing(t, fixture.state)
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
