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
		{"apply", "missing"},
		{"status", "app"},
		{"select"},
		{"select", "add"},
		{"select", "remove"},
		{"apply", "--unknown"},
		{"init", fixture.repository, "extra"},
		{"paths", "extra"},
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
	code, stdout, stderr := fixture.run("status")
	if code != exitOK || !strings.Contains(stdout, "conflict") || stderr == "" {
		t.Fatalf("status conflict = (%d, %q, %q), want successful status", code, stdout, stderr)
	}
}

func TestEmptySelectionModuleIsRejectedWithoutMutation(t *testing.T) {
	for _, args := range [][]string{
		{"select", "add", ""},
		{"select", "remove", ""},
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

func TestApplyAndStatusRejectRemovedModuleArgumentsWithoutReadingOrMutation(t *testing.T) {
	for _, args := range [][]string{
		{"apply", "app"},
		{"apply", "app", "--dry-run"},
		{"status", "app"},
		{"apply", ""},
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
			if code != exitUsage || stdout != "" || stderr == "" {
				t.Fatalf(
					"run(%q) = (%d, %q, %q), want stderr-only usage error",
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
	for _, command := range []string{"init", "select", "status", "apply", "paths", "version"} {
		if !strings.Contains(stdout, command) {
			t.Fatalf("help missing %q:\n%s", command, stdout)
		}
	}
	for _, removed := range []string{"remove", "add", "doctor", "diff"} {
		if strings.Contains(stdout, "\n  "+removed+" ") {
			t.Fatalf("help still lists %q:\n%s", removed, stdout)
		}
	}
}

func TestApplyHelpExplainsCurrentSelection(t *testing.T) {
	fixture := newCLITestEnv(t, "")
	code, stdout, stderr := fixture.run("help", "apply")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "current effective selection") {
		t.Fatalf("help apply = (%d, %q, %q), want convergence-only semantics", code, stdout, stderr)
	}
}

func TestSelectHelpExplainsConfigOnlySelection(t *testing.T) {
	fixture := newCLITestEnv(t, "")
	code, stdout, stderr := fixture.run("help", "select")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(
			stdout,
			"Change direct module selection without converging targets",
		) {
		t.Fatalf("help select = (%d, %q, %q), want config-only semantics", code, stdout, stderr)
	}
}

func TestVersionReportsBuildInformation(t *testing.T) {
	fixture := newCLITestEnv(t, "")
	code, stdout, stderr := fixture.runInjected("version")
	if code != exitOK ||
		stderr != "" ||
		stdout != "version=test\ncommit=test\nbuild_time=test\n" {
		t.Fatalf("version = (%d, %q, %q), want injected build information", code, stdout, stderr)
	}
}
