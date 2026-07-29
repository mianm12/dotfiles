package cli

import (
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

	code, stdout, stderr := fixture.runInjected("status", "gated")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(
			stdout,
			"gated  conflict selection=profile applicability=indeterminate ",
		) ||
		!strings.Contains(stdout, "synthetic os-release failure") ||
		!strings.Contains(stdout, "blocked module=gated") ||
		strings.Contains(stdout, "prune") ||
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
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "blocked module=gated") ||
		!strings.Contains(stdout, "synthetic os-release failure") ||
		strings.Contains(stdout, "prune") ||
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
		stdout != "" ||
		!strings.Contains(stderr, "applicability is indeterminate") ||
		!strings.Contains(stderr, "synthetic os-release failure") {
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

func TestExplicitIndeterminateModuleShowsAnalysisButDoesNotChangeSelection(t *testing.T) {
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

	code, stdout, stderr := fixture.runInjected("status", "gated")
	if code != exitOK ||
		!strings.Contains(
			stdout,
			"gated  inactive applicability=indeterminate ",
		) ||
		!strings.Contains(stdout, `reason="platform distro is unknown:`) ||
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

	code, stdout, stderr = fixture.runInjected(
		"apply",
		"gated",
		"--dry-run",
	)
	if code != exitOK ||
		!strings.Contains(stdout, "selection-delta add-extra module=gated") ||
		!strings.Contains(stdout, "blocked module=gated") ||
		!strings.Contains(stdout, "distribution is unreadable") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"explicit indeterminate dry-run = (%d, %q, %q)",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.runInjected("apply", "gated")
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
	assertSnapshotUnchanged(t, before)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged empty selection", extras)
	}
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
	assertCLIMissing(t, filepath.Join(fixture.home, ".gated"))
}

func TestInitWithIndeterminateProfileDoesNotPublishSelection(t *testing.T) {
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
		"--dry-run",
	)
	if code != exitOK ||
		!strings.Contains(stdout, "selection-delta create") ||
		!strings.Contains(stdout, "blocked module=gated") ||
		!strings.Contains(stdout, "distribution is unavailable") ||
		strings.Contains(stdout, "create-link") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"indeterminate init dry-run = (%d, %q, %q)",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.runInjected(
		"init",
		fixture.repository,
		"--profile",
		"base",
	)
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, `module "gated" applicability is indeterminate`) {
		t.Fatalf(
			"indeterminate init = (%d, %q, %q), want zero-write failure",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.config)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
	assertCLIMissing(t, filepath.Join(fixture.home, ".gated"))
}

func TestAnyEffectiveIndeterminateModuleBlocksScopedMutation(t *testing.T) {
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
	fixture.writeMachine(t, []string{"base"}, nil)
	fixture.env.platform = func() config.Platform {
		return cliIndeterminateLinuxPlatform("distribution is unavailable")
	}
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runInjected(
		"apply",
		"extra",
		"--dry-run",
	)
	if code != exitOK ||
		!strings.Contains(stdout, "selection-delta add-extra module=extra") ||
		!strings.Contains(stdout, "blocked module=uncertain") ||
		strings.Contains(stdout, "create-link") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"scoped dry-run = (%d, %q, %q), want whole-operation blocker",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.runInjected("apply", "extra")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, `module "uncertain" applicability is indeterminate`) {
		t.Fatalf(
			"scoped apply = (%d, %q, %q), want whole-operation failure",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged empty selection", extras)
	}
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
	assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))
}

func TestRemoveRefusesUncertainCurrentExtraWithoutCleanup(t *testing.T) {
	tests := []struct {
		name       string
		platform   config.Platform
		wantReason string
	}{
		{
			name: "not applicable",
			platform: cliTestPlatform(
				"linux",
				"gentoo",
				"x86_64",
			),
			wantReason: "not applicable",
		},
		{
			name: "indeterminate",
			platform: cliIndeterminateLinuxPlatform(
				"distribution cannot be determined",
			),
			wantReason: "applicability is indeterminate",
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
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.runInjected(
				"remove",
				"gated",
				"--dry-run",
			)
			if code != exitOK ||
				!strings.Contains(
					stdout,
					"selection-delta remove-extra module=gated",
				) ||
				!strings.Contains(stdout, "blocked module=gated") ||
				!strings.Contains(stdout, test.wantReason) ||
				strings.Contains(stdout, "prune") ||
				strings.Contains(stdout, "forget") ||
				stderr != "" {
				t.Fatalf(
					"remove dry-run = (%d, %q, %q), want blocker without cleanup",
					code,
					stdout,
					stderr,
				)
			}
			assertSnapshotUnchanged(t, before)

			code, stdout, stderr = fixture.runInjected("remove", "gated")
			if code != exitError ||
				stdout != "" ||
				!strings.Contains(stderr, test.wantReason) {
				t.Fatalf(
					"remove = (%d, %q, %q), want zero-write failure",
					code,
					stdout,
					stderr,
				)
			}
			assertSnapshotUnchanged(t, before)
			if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 ||
				extras[0] != "gated" {
				t.Fatalf(
					"extra_modules = %v, want unchanged [gated]",
					extras,
				)
			}
			assertCLILink(
				t,
				filepath.Join(fixture.home, ".gated"),
				filepath.Join(
					fixture.repository,
					"modules",
					"gated",
					"config",
				),
			)
		})
	}
}

func cliIndeterminateLinuxPlatform(diagnostic string) config.Platform {
	return config.Platform{
		OS:     config.KnownPlatformField("linux"),
		Distro: config.UnknownPlatformField(diagnostic),
		Arch:   config.KnownPlatformField("x86_64"),
	}
}
