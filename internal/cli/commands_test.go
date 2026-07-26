package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExitCodesAndStatusConflict(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
	fixture.writeMachine(t, []string{"base"}, nil)
	writeCLIFile(t, filepath.Join(fixture.home, ".app"), "personal")

	for _, args := range [][]string{
		{"apply", "one", "two"},
		{"remove"},
		{"apply", "--unknown"},
		{"init", fixture.repository},
		{"version", "extra"},
		{"help", "does-not-exist"},
		{"help", "apply", "extra"},
		{"unknown"},
	} {
		code, stdout, stderr := fixture.run(args...)
		if code != exitUsage || stdout != "" || stderr == "" {
			t.Fatalf("run(%v) = (%d, %q, %q), want stderr-only usage error", args, code, stdout, stderr)
		}
	}
	code, stdout, stderr := fixture.run("apply", "missing")
	if code != exitError || stdout != "" || stderr == "" {
		t.Fatalf("apply missing = (%d, %q, %q), want stderr-only runtime error", code, stdout, stderr)
	}
	code, stdout, stderr = fixture.run("status")
	if code != exitOK || !strings.Contains(stdout, "conflict") || stderr == "" {
		t.Fatalf("status conflict = (%d, %q, %q), want successful status", code, stdout, stderr)
	}
}

func TestEmptyOptionalModuleIsRejectedWithoutMutation(t *testing.T) {
	for _, args := range [][]string{
		{"apply", ""},
		{"apply", "", "--dry-run"},
		{"status", ""},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = ["app"]`)
			fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
			fixture.writeMachine(t, []string{"base"}, nil)
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.run(args...)
			if code != exitError ||
				stdout != "" ||
				!strings.Contains(stderr, "invalid") ||
				!strings.Contains(stderr, "module ID") {
				t.Fatalf(
					"run(%q) = (%d, %q, %q), want stderr-only invalid module failure",
					args,
					code,
					stdout,
					stderr,
				)
			}
			assertSnapshotUnchanged(t, before)
			assertCLIMissing(t, fixture.state)
			assertCLIMissing(t, fixture.lock)
			assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
		})
	}
}

func TestHelpListsOnlyPublicCommands(t *testing.T) {
	fixture := newCLITestEnv(t, "")
	code, stdout, stderr := fixture.run("help")
	if code != exitOK || stderr != "" {
		t.Fatalf("help = (%d, %q)", code, stderr)
	}
	for _, command := range []string{"init", "status", "apply", "remove", "version"} {
		if !strings.Contains(stdout, command) {
			t.Fatalf("help missing %q:\n%s", command, stdout)
		}
	}
	for _, removed := range []string{"add", "doctor", "diff"} {
		if strings.Contains(stdout, "\n  "+removed+" ") {
			t.Fatalf("help still lists %q:\n%s", removed, stdout)
		}
	}
}
