package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
)

func TestInitProfilesOnSupportedPlatformsConverges(t *testing.T) {
	tests := []struct {
		name     string
		platform config.Platform
	}{
		{
			name:     "macos",
			platform: cliTestPlatform("macos", "", "aarch64"),
		},
		{
			name:     "linux",
			platform: cliTestPlatform("linux", "ubuntu", "x86_64"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = ["app"]`)
			fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
			fixture.env.platform = func() config.Platform { return test.platform }

			code, _, stderr := fixture.runInjected(
				"init",
				fixture.repository,
				"--profile",
				"base",
			)
			if code != exitOK || stderr == "" {
				t.Fatalf("init = (%d, %q), want success with missing-state warning", code, stderr)
			}
			assertCLILink(
				t,
				filepath.Join(fixture.home, ".app"),
				filepath.Join(fixture.repository, "modules", "app", "config"),
			)
			assertApplyNoMutation(t, fixture, fixture.runInjected)
		})
	}
}

func TestInitConflictIsStrictlyReadOnly(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.config/app/config"
`, map[string]string{"config": "portable"})
	target := filepath.Join(fixture.home, ".config", "app", "config")
	writeCLIFile(t, target, "personal")
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run(
		"init",
		fixture.repository,
		"--profile",
		"base",
	)
	if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
		t.Fatalf("init = (%d, %q, %q), want stderr-only conflict", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.config)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
}

func TestInitRejectsExplicitEmptyRepositoryWithoutMutation(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		name := "mutation"
		if dryRun {
			name = "dry-run"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newCLITestEnv(t, "base = []")
			before := snapshotTree(t, fixture.root)
			args := []string{"init", "", "--profile", "base"}
			if dryRun {
				args = append(args, "--dry-run")
			}

			code, stdout, stderr := fixture.runProcessAt(fixture.repository, args...)
			if code != exitError ||
				stdout != "" ||
				!strings.Contains(stderr, "repository") ||
				!strings.Contains(stderr, "non-empty") {
				t.Fatalf(
					"init empty repository = (%d, %q, %q), want stderr-only runtime failure",
					code,
					stdout,
					stderr,
				)
			}
			assertSnapshotUnchanged(t, before)
			assertCLIMissing(t, fixture.config)
			assertCLIMissing(t, fixture.state)
			assertCLIMissing(t, fixture.lock)
		})
	}
}

func TestInitDryRunIsStrictlyReadOnly(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})

	code, stdout, stderr := fixture.run(
		"init",
		fixture.repository,
		"--profile",
		"base",
		"--dry-run",
	)
	if code != exitOK || !strings.Contains(stdout, "create-link") || stderr == "" {
		t.Fatalf("init dry-run = (%d, %q, %q)", code, stdout, stderr)
	}
	assertCLIMissing(t, fixture.config)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
	assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
}
