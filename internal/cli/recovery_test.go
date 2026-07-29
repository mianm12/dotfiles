package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestApplyConvergesAfterInterruptedFacts(t *testing.T) {
	t.Run("selection persisted before artifacts", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
		fixture.writeMachine(t, []string{"base"}, []string{"extra"})

		code, _, stderr := fixture.run("apply")
		if code != exitOK || stderr == "" {
			t.Fatalf("recovery apply = (%d, %q)", code, stderr)
		}
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".extra"),
			filepath.Join(fixture.repository, "modules", "extra", "config"),
		)
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("link created before state commit", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "config"})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".app")
		destination := filepath.Join(fixture.repository, "modules", "app", "config")
		if err := os.Symlink(destination, target); err != nil {
			t.Fatalf("os.Symlink(interrupted link) error = %v", err)
		}

		code, stdout, stderr := fixture.run("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr == "" {
			t.Fatalf("recovery apply = (%d, %q, %q)", code, stdout, stderr)
		}
		assertCLILink(t, target, destination)
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("local published before state commit", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[locals]]
id = "local"
example = "local.example"
target = "~/.app.local"
`, map[string]string{"local.example": "example"})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".app.local")
		writeCLIFile(t, target, "personal")

		code, stdout, stderr := fixture.run("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr == "" {
			t.Fatalf("recovery apply = (%d, %q, %q)", code, stdout, stderr)
		}
		if record := loadTestState(t, fixture).Modules["app"].Placements["local"]; record.Kind != state.KindLocal {
			t.Fatalf("local state record = %#v, want local provenance", record)
		}
		if data, err := os.ReadFile(target); err != nil || string(data) != "personal" {
			t.Fatalf("local = (%q, %v), want preserved personal bytes", data, err)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("updated link before state commit", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "new"
target = "~/.app"
`, map[string]string{
			"old": "old",
			"new": "new",
		})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".app")
		oldDestination := filepath.Join(fixture.repository, "modules", "app", "old")
		newDestination := filepath.Join(fixture.repository, "modules", "app", "new")
		if err := os.Symlink(newDestination, target); err != nil {
			t.Fatalf("os.Symlink(updated link) error = %v", err)
		}
		writeLinkState(t, fixture, target, oldDestination)

		code, stdout, stderr := fixture.run("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr != "" {
			t.Fatalf("repair-state apply = (%d, %q, %q)", code, stdout, stderr)
		}
		record := loadTestState(t, fixture).Modules["app"].Placements["config"]
		if record.LinkDestination != newDestination {
			t.Fatalf("state destination = %q, want %q", record.LinkDestination, newDestination)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("old link deleted during update", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "new"
target = "~/.app"
`, map[string]string{
			"old": "old",
			"new": "new",
		})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".app")
		oldDestination := filepath.Join(fixture.repository, "modules", "app", "old")
		writeLinkState(t, fixture, target, oldDestination)

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("recovery apply = (%d, %q)", code, stderr)
		}
		assertCLILink(
			t,
			target,
			filepath.Join(fixture.repository, "modules", "app", "new"),
		)
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("prune completed before state commit", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", "", map[string]string{"old": "old"})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".old")
		oldDestination := filepath.Join(fixture.repository, "modules", "app", "old")
		writeLinkState(t, fixture, target, oldDestination)

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("recovery apply = (%d, %q)", code, stderr)
		}
		if modules := loadTestState(t, fixture).Modules; len(modules) != 0 {
			t.Fatalf("state modules = %#v, want stale record forgotten", modules)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
	})
}

func TestMutationOutputFailureAdvisesRerun(t *testing.T) {
	t.Run("init", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})

		stderr := runWithFailedStdout(
			t,
			[]string{"init", fixture.repository, "--profile", "base"},
		)
		assertOutputFailure(t, stderr, "dot apply")
		target := filepath.Join(fixture.home, ".app")
		assertCLILink(t, target, filepath.Join(fixture.repository, "modules", "app", "config"))
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("apply", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
		fixture.writeMachine(t, []string{"base"}, nil)

		stderr := runWithFailedStdout(t, []string{"apply"})
		assertOutputFailure(t, stderr, "dot apply")
		target := filepath.Join(fixture.home, ".app")
		assertCLILink(t, target, filepath.Join(fixture.repository, "modules", "app", "config"))
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("remove", func(t *testing.T) {
		fixture := newCLITestEnv(t, "base = []")
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
		fixture.writeMachine(t, []string{"base"}, []string{"app"})
		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}

		stderr = runWithFailedStdout(t, []string{"remove", "app"})
		assertOutputFailure(t, stderr, "dot remove app")
		assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want empty", extras)
		}

		before := snapshotPaths(t, fixture.config, fixture.state, fixture.lock)
		code, stdout, stderr := fixture.run("remove", "app")
		if code != exitOK || stderr != "" {
			t.Fatalf("recovery remove = (%d, %q, %q)", code, stdout, stderr)
		}
		assertCLINoMutationResult(t, stdout)
		assertSnapshotUnchanged(t, before)
	})
}

func TestScopedApplyFailureAdvisesScopedRerun(t *testing.T) {
	newFixture := func(t *testing.T) *cliTestEnv {
		t.Helper()
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "portable"})
		fixture.writeMachine(t, []string{"base"}, nil)
		return fixture
	}

	t.Run("selection publication interruption", func(t *testing.T) {
		fixture := newFixture(t)
		fixture.env.afterSelectionPublish = func() error {
			return errors.New("synthetic interruption")
		}

		code, stdout, stderr := fixture.runInjected("apply", "extra")

		if code != exitError ||
			stdout != "" ||
			!strings.Contains(stderr, "rerun dot apply extra") {
			t.Fatalf(
				"scoped apply interruption = (%d, %q, %q), want scoped rerun",
				code,
				stdout,
				stderr,
			)
		}
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 ||
			extras[0] != "extra" {
			t.Fatalf("extra_modules = %v, want persisted [extra]", extras)
		}
		assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))
	})

	t.Run("convergence failure", func(t *testing.T) {
		fixture := newFixture(t)
		target := filepath.Join(fixture.home, ".extra")
		fixture.env.beforeExecution = func() {
			writeCLIFile(t, target, "personal")
		}

		code, stdout, stderr := fixture.runInjected("apply", "extra")

		if code != exitError ||
			stdout != "" ||
			!strings.Contains(stderr, "rerun dot apply extra") {
			t.Fatalf(
				"scoped apply convergence failure = (%d, %q, %q), want scoped rerun",
				code,
				stdout,
				stderr,
			)
		}
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 ||
			extras[0] != "extra" {
			t.Fatalf("extra_modules = %v, want persisted [extra]", extras)
		}
	})

	t.Run("result output failure", func(t *testing.T) {
		fixture := newFixture(t)

		stderr := runWithFailedStdout(t, []string{"apply", "extra"})

		assertOutputFailure(t, stderr, "dot apply extra")
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".extra"),
			filepath.Join(fixture.repository, "modules", "extra", "config"),
		)
	})
}
