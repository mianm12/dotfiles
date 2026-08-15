package cli

import (
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
		if code != exitOK {
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
		if code != exitOK || !strings.Contains(stdout, "record") {
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
		if code != exitOK || !strings.Contains(stdout, "converged") {
			t.Fatalf("recovery apply = (%d, %q, %q)", code, stdout, stderr)
		}
		if links := loadTestState(t, fixture).Links; len(links) != 0 {
			t.Fatalf("local recovery state links = %#v, want empty link-only state", links)
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
			!strings.Contains(stdout, "forget") && !strings.Contains(stdout, "record") ||
			stderr != "" {
			t.Fatalf("record apply = (%d, %q, %q)", code, stdout, stderr)
		}
		record := loadTestState(t, fixture).Links[state.Key{ModuleID: "app", PlacementID: "config"}]
		if record.Dest != newDestination {
			t.Fatalf("state destination = %q, want %q", record.Dest, newDestination)
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

	t.Run("remove completed before state commit", func(t *testing.T) {
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
		if links := loadTestState(t, fixture).Links; len(links) != 0 {
			t.Fatalf("state links = %#v, want stale ownership forgotten", links)
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
		if !strings.Contains(stderr, "selection may already be updated") ||
			!strings.Contains(stderr, "rerun dot init") {
			t.Fatalf("stderr = %q, want init selection recovery advice", stderr)
		}
		target := filepath.Join(fixture.home, ".app")
		assertCLIMissing(t, target)
		if code, _, applyErr := fixture.run("apply"); code != exitOK {
			t.Fatalf("apply after init output failure = (%d, %q)", code, applyErr)
		}
		assertCLILink(t, target, filepath.Join(fixture.repository, "modules", "app", "config"))
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

	t.Run("select remove", func(t *testing.T) {
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

		stderr = runWithFailedStdout(t, []string{"select", "remove", "app"})
		if !strings.Contains(stderr, "selection may already be updated") ||
			!strings.Contains(stderr, "rerun dot select remove app") {
			t.Fatalf("stderr = %q, want select remove recovery advice", stderr)
		}
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".app"),
			filepath.Join(fixture.repository, "modules", "app", "config"),
		)
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want empty", extras)
		}

		code, stdout, stderr := fixture.run("apply")
		if code != exitOK || stderr != "" {
			t.Fatalf("recovery apply = (%d, %q, %q)", code, stdout, stderr)
		}
		assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
	})
}

func TestApplyFailureAdvisesFullRerun(t *testing.T) {
	newFixture := func(t *testing.T) *cliTestEnv {
		t.Helper()
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "portable"})
		fixture.writeMachine(t, []string{"base"}, []string{"extra"})
		return fixture
	}

	t.Run("result output failure", func(t *testing.T) {
		fixture := newFixture(t)

		stderr := runWithFailedStdout(t, []string{"apply"})

		assertOutputFailure(t, stderr, "dot apply")
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".extra"),
			filepath.Join(fixture.repository, "modules", "extra", "config"),
		)
	})
}
