package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
)

func TestApplyDistinguishesProfileAndExtraNotApplicableModules(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["portable", "gated"]`)
	fixture.writeModule(t, "portable", `
[[links]]
id = "config"
source = "config"
target = "~/.portable"
`, map[string]string{"config": "portable"})
	fixture.writeModule(t, "gated", `
[match]
os = ["macos"]

[[links]]
id = "config"
source = "config"
target = "~/.gated"
`, map[string]string{"config": "gated"})
	fixture.writeMachine(t, []string{"base"}, nil)

	code, _, stderr := fixture.runInjected("apply")
	if code != exitOK {
		t.Fatalf("apply = (%d, %q), want success", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".portable"),
		filepath.Join(fixture.repository, "modules", "portable", "config"),
	)
	assertCLIMissing(t, filepath.Join(fixture.home, ".gated"))

	code, stdout, stderr := fixture.runInjected("status")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "fact module=portable selection=profile state=present applicability=applicable") ||
		!strings.Contains(stdout, "fact module=gated selection=profile state=absent applicability=not-applicable") {
		t.Fatalf("status = (%d, %q, %q), want converged portable and skipped gated", code, stdout, stderr)
	}
	assertApplyNoMutation(t, fixture, fixture.runInjected)

	explicit := newCLITestEnv(t, `base = []`)
	explicit.writeModule(t, "gated", `
[match]
os = ["macos"]

[[links]]
id = "config"
source = "config"
target = "~/.gated"
`, map[string]string{"config": "gated"})
	explicit.writeMachine(t, []string{"base"}, []string{"gated"})
	before := snapshotTree(t, explicit.root)

	code, stdout, stderr = explicit.runInjected("apply")
	if code != exitError ||
		!strings.Contains(stdout, "skip module=gated") ||
		!strings.Contains(stdout, "not applicable") ||
		!strings.Contains(stderr, "state is missing") ||
		strings.Contains(stderr, "error:") {
		t.Fatalf("extra apply = (%d, %q, %q), want skip result", code, stdout, stderr)
	}
	assertOnlyLockBookkeepingChanged(t, before, explicit)
	assertCLIMissing(t, explicit.state)
	if extras := explicit.loadMachine(t).ExtraModules; len(extras) != 1 || extras[0] != "gated" {
		t.Fatalf("extra_modules = %v, want unchanged gated selection", extras)
	}
}

func TestApplySourceContentChangeIsNoop(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "before"})
	fixture.writeMachine(t, []string{"base"}, nil)

	code, _, stderr := fixture.run("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	assertApplyNoMutation(t, fixture, fixture.run)

	source := filepath.Join(fixture.repository, "modules", "app", "config")
	if err := os.WriteFile(source, []byte("after"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(source) error = %v", err)
	}
	target := filepath.Join(fixture.home, ".app")
	before := snapshotPaths(t, fixture.config, fixture.state, fixture.lock, target)

	code, stdout, stderr := fixture.run("apply")
	if code != exitOK || stderr != "" {
		t.Fatalf("apply after source content change = (%d, %q, %q)", code, stdout, stderr)
	}
	assertCLINoMutationResult(t, stdout)
	assertSnapshotUnchanged(t, before)
	assertCLILink(t, target, source)
}

func TestApplyConvergesSelectedExtraModuleWithoutChangingSelection(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
	fixture.writeMachine(t, []string{"base"}, []string{"extra"})
	machineBefore := snapshotPaths(t, fixture.config)

	code, _, stderr := fixture.run("apply")
	if code != exitOK {
		t.Fatalf("apply extra = (%d, %q), want success", code, stderr)
	}
	assertSnapshotUnchanged(t, machineBefore)
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".extra"),
		filepath.Join(fixture.repository, "modules", "extra", "config"),
	)
	assertApplyNoMutation(t, fixture, fixture.run)
}

func TestApplyHandlesKnownPlatformMismatchAndRejectsInvalidOS(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["portable", "gated"]`)
	fixture.writeModule(t, "portable", `
[[links]]
id = "config"
source = "config"
target = "~/.portable"
`, map[string]string{"config": "portable"})
	fixture.writeModule(t, "gated", `
[variants.ubuntu]
root = "."

[variants.ubuntu.match]
os = ["linux"]
distro = ["ubuntu"]

[[variants.ubuntu.links]]
id = "config"
source = "config"
target = "~/.gated"
`, map[string]string{"config": "gated"})
	fixture.writeModule(t, "invalid-os", `
[match]
os = ["freebsd"]
`, nil)
	fixture.writeMachine(t, []string{"base"}, nil)
	fixture.env.platform = func() config.Platform {
		return cliTestPlatform("linux", "gentoo", "riscv64")
	}

	code, _, stderr := fixture.runInjected("apply")
	if code != exitOK {
		t.Fatalf("known-mismatch apply = (%d, %q)", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".portable"),
		filepath.Join(fixture.repository, "modules", "portable", "config"),
	)
	assertCLIMissing(t, filepath.Join(fixture.home, ".gated"))
	code, stdout, stderr := fixture.runInjected("status")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "fact module=portable selection=profile state=present applicability=applicable") ||
		!strings.Contains(stdout, "fact module=gated selection=profile state=absent applicability=not-applicable") {
		t.Fatalf("known-mismatch status = (%d, %q, %q)", code, stdout, stderr)
	}
	assertApplyNoMutation(t, fixture, fixture.runInjected)

	fixture.writeMachine(t, []string{"base"}, []string{"invalid-os"})
	before := snapshotTree(t, fixture.root)
	code, stdout, stderr = fixture.runInjected("apply")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "unsupported os token") {
		t.Fatalf("invalid-os apply = (%d, %q, %q), want strict config failure", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 || extras[0] != "invalid-os" {
		t.Fatalf("extra_modules = %v, want unchanged [invalid-os] selection", extras)
	}
}
