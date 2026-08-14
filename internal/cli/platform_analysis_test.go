package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
)

const distroGatedLinkManifest = `
[match]
os = ["linux"]
distro = ["ubuntu"]

[[links]]
id = "config"
source = "config"
target = "~/.gated"
`

func TestIndeterminateProfileBlocksMutationWithoutPruningOwnership(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["gated"]`)
	fixture.writeModule(
		t,
		"gated",
		distroGatedLinkManifest,
		map[string]string{"config": "gated"},
	)
	fixture.writeMachine(t, []string{"base"}, nil)

	code, _, stderr := fixture.runInjected("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	target := filepath.Join(fixture.home, ".gated")
	assertCLILink(
		t,
		target,
		filepath.Join(fixture.repository, "modules", "gated", "config"),
	)

	fixture.env.platform = func() config.Platform {
		return cliIndeterminateLinuxPlatform("synthetic os-release failure")
	}
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runInjected("status")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(
			stdout,
			"fact module=gated selection=profile state=present applicability=indeterminate ",
		) ||
		!strings.Contains(stdout, "synthetic os-release failure") ||
		!strings.Contains(stdout, "skip module=gated") ||
		strings.Contains(stdout, "remove module=gated") ||
		strings.Contains(stdout, "forget") {
		t.Fatalf(
			"indeterminate status = (%d, %q, %q), want blocker without cleanup",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.runInjected("apply", "--dry-run")
	if code != exitError ||
		stderr != "" ||
		!strings.Contains(stdout, "skip module=gated") ||
		!strings.Contains(stdout, "synthetic os-release failure") ||
		strings.Contains(stdout, "remove module=gated") ||
		strings.Contains(stdout, "forget") {
		t.Fatalf(
			"indeterminate dry-run = (%d, %q, %q), want blocker without cleanup",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.runInjected("apply")
	if code != exitError ||
		!strings.Contains(stdout, "skip module=gated") ||
		!strings.Contains(stdout, "applicability is indeterminate") ||
		!strings.Contains(stdout, "synthetic os-release failure") ||
		stderr != "" {
		t.Fatalf(
			"indeterminate apply = (%d, %q, %q), want zero-write failure",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLILink(
		t,
		target,
		filepath.Join(fixture.repository, "modules", "gated", "config"),
	)
}

func TestInactiveIndeterminateModuleIsNotResolvedByStatus(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(
		t,
		"gated",
		distroGatedLinkManifest,
		map[string]string{"config": "gated"},
	)
	fixture.writeMachine(t, []string{"base"}, nil)
	fixture.env.platform = func() config.Platform {
		return cliIndeterminateLinuxPlatform("distribution is unreadable")
	}
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runInjected("status")
	if code != exitOK ||
		stdout != "fact module=gated selection=none state=absent\n" ||
		strings.Contains(stdout, "blocked") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"inactive indeterminate status = (%d, %q, %q)",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.runInjected("select", "add", "gated")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "applicability is indeterminate") {
		t.Fatalf(
			"explicit indeterminate apply = (%d, %q, %q)",
			code,
			stdout,
			stderr,
		)
	}
	assertOnlyLockBookkeepingChanged(t, before, fixture)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged empty selection", extras)
	}
	assertCLIMissing(t, fixture.state)
	if _, err := os.Lstat(fixture.lock); err != nil {
		t.Fatalf("lock bookkeeping error = %v", err)
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".gated"))
}

func TestInitDoesNotResolveIndeterminateProfile(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["gated"]`)
	fixture.writeModule(
		t,
		"gated",
		distroGatedLinkManifest,
		map[string]string{"config": "gated"},
	)
	fixture.env.platform = func() config.Platform {
		return cliIndeterminateLinuxPlatform("distribution is unavailable")
	}
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runInjected(
		"init",
		fixture.repository,
		"--profile",
		"base",
	)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "run dot apply") {
		t.Fatalf(
			"indeterminate init = (%d, %q, %q), want config-only success",
			code,
			stdout,
			stderr,
		)
	}
	if before.root == "" {
		t.Fatal("synthetic snapshot unexpectedly missing root")
	}
	if profiles := fixture.loadMachine(t).Profiles; len(profiles) != 1 || profiles[0] != "base" {
		t.Fatalf("profiles = %v, want [base]", profiles)
	}
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, filepath.Join(fixture.home, ".gated"))
	assertCLIMissing(t, filepath.Join(fixture.home, ".gated"))
}

func TestAnyEffectiveIndeterminateModuleBlocksFullMutation(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["uncertain"]`)
	fixture.writeModule(t, "uncertain", `
[match]
os = ["linux"]
distro = ["ubuntu"]
`, nil)
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
	fixture.writeMachine(t, []string{"base"}, []string{"extra"})
	fixture.env.platform = func() config.Platform {
		return cliIndeterminateLinuxPlatform("distribution is unavailable")
	}
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runInjected("apply", "--dry-run")
	if code != exitError ||
		!strings.Contains(stdout, "skip module=uncertain") ||
		!strings.Contains(stdout, "link module=extra placement=config") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"full dry-run = (%d, %q, %q), want whole-operation blocker",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.runInjected("apply")
	if code != exitError ||
		!strings.Contains(stdout, "skip module=uncertain") ||
		!strings.Contains(stdout, "link module=extra placement=config") ||
		!strings.Contains(stdout, "applicability is indeterminate") ||
		!strings.Contains(stderr, "state is missing") ||
		strings.Contains(stderr, "error:") {
		t.Fatalf(
			"full apply = (%d, %q, %q), want whole-operation failure",
			code,
			stdout,
			stderr,
		)
	}
	assertOnlyLockBookkeepingChanged(t, before, fixture)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 || extras[0] != "extra" {
		t.Fatalf("extra_modules = %v, want unchanged [extra] selection", extras)
	}
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))
}

func TestRemoveContractsUncertainExtraAndCleansOwnedState(t *testing.T) {
	tests := []struct {
		name     string
		platform config.Platform
		drift    bool
	}{
		{
			name: "not applicable",
			platform: cliTestPlatform(
				"linux",
				"gentoo",
				"x86_64",
			),
		},
		{
			name: "indeterminate",
			platform: cliIndeterminateLinuxPlatform(
				"distribution cannot be determined",
			),
			drift: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = []`)
			fixture.writeModule(
				t,
				"gated",
				distroGatedLinkManifest,
				map[string]string{"config": "gated"},
			)
			fixture.writeMachine(t, []string{"base"}, []string{"gated"})
			code, _, stderr := fixture.runInjected("apply")
			if code != exitOK {
				t.Fatalf("initial apply = (%d, %q)", code, stderr)
			}

			fixture.env.platform = func() config.Platform {
				return test.platform
			}
			target := filepath.Join(fixture.home, ".gated")
			userDestination := filepath.Join(fixture.root, "user", "gated")
			if test.drift {
				if err := os.Remove(target); err != nil {
					t.Fatalf("os.Remove(owned target) error = %v", err)
				}
				if err := os.Symlink(userDestination, target); err != nil {
					t.Fatalf("os.Symlink(user target) error = %v", err)
				}
			}
			before := snapshotTree(t, fixture.root)
			code, stdout, stderr := fixture.runInjected("status")
			if code != exitOK || stderr != "" ||
				!strings.Contains(stdout, "skip module=gated") ||
				strings.Contains(stdout, "remove module=gated") ||
				strings.Contains(stdout, "forget module=gated") {
				t.Fatalf(
					"status before selection contraction = (%d, %q, %q), want blocker without cleanup",
					code,
					stdout,
					stderr,
				)
			}
			assertSnapshotUnchanged(t, before)

			code, stdout, stderr = fixture.runInjected("select", "remove", "gated")
			if code != exitOK ||
				!strings.Contains(stdout, "selection_changed=true") ||
				stderr != "" {
				t.Fatalf(
					"select remove = (%d, %q, %q), want selection-only contraction",
					code,
					stdout,
					stderr,
				)
			}
			if test.drift {
				assertCLILink(t, target, userDestination)
			} else {
				assertCLILink(
					t,
					target,
					filepath.Join(fixture.repository, "modules", "gated", "config"),
				)
			}

			code, stdout, stderr = fixture.runInjected("apply")
			if code != exitOK ||
				(!strings.Contains(stdout, "remove") && !strings.Contains(stdout, "forget")) {
				t.Fatalf(
					"apply after select remove = (%d, %q, %q), want cleanup",
					code,
					stdout,
					stderr,
				)
			}
			if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
				t.Fatalf("extra_modules = %v, want empty", extras)
			}
			if records := loadTestState(t, fixture).Links; len(records) != 0 {
				t.Fatalf("state records = %#v, want empty", records)
			}
			if test.drift {
				assertCLILink(t, target, userDestination)
			} else {
				assertCLIMissing(t, target)
			}
			assertApplyNoMutation(t, fixture, fixture.runInjected)
		})
	}
}

func TestRemoveUncertainExtraLeavesLocalOutsideState(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "gated", `
[match]
os = ["linux"]
distro = ["ubuntu"]

[[locals]]
id = "local"
example = "local.example"
target = "~/.gated.local"
`, map[string]string{"local.example": "initial"})
	fixture.writeMachine(t, []string{"base"}, []string{"gated"})
	fixture.env.platform = func() config.Platform {
		return cliTestPlatform("linux", "ubuntu", "x86_64")
	}
	if code, _, stderr := fixture.runInjected("apply"); code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	target := filepath.Join(fixture.home, ".gated.local")
	if err := os.WriteFile(target, []byte("user-owned"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(user local) error = %v", err)
	}
	fixture.env.platform = func() config.Platform {
		return cliIndeterminateLinuxPlatform("distribution cannot be determined")
	}
	code, stdout, stderr := fixture.runInjected("select", "remove", "gated")
	if code != exitOK ||
		!strings.Contains(stdout, "selection_changed=true") ||
		stderr != "" {
		t.Fatalf(
			"select remove = (%d, %q, %q), want selection contraction",
			code,
			stdout,
			stderr,
		)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "user-owned" {
		t.Fatalf("local after select remove = (%q, %v), want preserved user data", data, err)
	}

	code, stdout, stderr = fixture.runInjected("apply")
	if code != exitOK ||
		!strings.Contains(stdout, "converged") ||
		strings.Contains(stdout, "forget") ||
		stderr != "" {
		t.Fatalf(
			"apply after select remove = (%d, %q, %q), want no-op outside state",
			code,
			stdout,
			stderr,
		)
	}
	data, err = os.ReadFile(target)
	if err != nil || string(data) != "user-owned" {
		t.Fatalf("local after remove = (%q, %v), want preserved user data", data, err)
	}
	if records := loadTestState(t, fixture).Links; len(records) != 0 {
		t.Fatalf("state records = %#v, want empty", records)
	}
	assertApplyNoMutation(t, fixture, fixture.runInjected)
}

func TestRemoveStillBlocksOtherEffectiveIndeterminateModule(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["uncertain"]`)
	fixture.writeModule(t, "uncertain", `
[match]
os = ["linux"]
distro = ["ubuntu"]
`, nil)
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
	fixture.writeMachine(t, []string{"base"}, []string{"extra"})
	fixture.env.platform = func() config.Platform {
		return cliTestPlatform("linux", "ubuntu", "x86_64")
	}
	if code, _, stderr := fixture.runInjected("apply"); code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	fixture.env.platform = func() config.Platform {
		return cliIndeterminateLinuxPlatform("distribution is unavailable")
	}
	code, stdout, stderr := fixture.runInjected("select", "remove", "extra")
	if code != exitOK || !strings.Contains(stdout, "selection_changed=true") || stderr != "" {
		t.Fatalf(
			"select remove = (%d, %q, %q), want independent selection contraction",
			code,
			stdout,
			stderr,
		)
	}

	code, stdout, stderr = fixture.runInjected("apply")
	if code != exitError ||
		!strings.Contains(stdout, "skip module=uncertain") ||
		!strings.Contains(stdout, "applicability is indeterminate") ||
		stderr != "" {
		t.Fatalf(
			"apply = (%d, %q, %q), want effective blocker after selection contraction",
			code,
			stdout,
			stderr,
		)
	}
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want contracted selection", extras)
	}
}

func cliIndeterminateLinuxPlatform(diagnostic string) config.Platform {
	return config.Platform{
		OS:     config.KnownPlatformField("linux"),
		Distro: config.UnknownPlatformField(diagnostic),
		Arch:   config.KnownPlatformField("x86_64"),
	}
}
