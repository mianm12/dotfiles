package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestRemoveExtraPreservesLocalsAndRejectsProfileModule(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["profiled"]`)
	fixture.writeModule(t, "profiled", `
[[links]]
id = "config"
source = "config"
target = "~/.profiled"
`, map[string]string{"config": "profiled"})
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"

[[locals]]
id = "local"
example = "local.example"
target = "~/.extra.local"
`, map[string]string{
		"config":        "extra",
		"local.example": "local",
	})
	fixture.writeMachine(t, []string{"base"}, []string{"extra"})

	code, _, stderr := fixture.run("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	assertApplyNoMutation(t, fixture, fixture.run)

	code, _, stderr = fixture.run("remove", "extra")
	if code != exitOK {
		t.Fatalf("remove extra = (%d, %q)", code, stderr)
	}
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want empty", extras)
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))
	localTarget := filepath.Join(fixture.home, ".extra.local")
	if data, err := os.ReadFile(localTarget); err != nil || string(data) != "local" {
		t.Fatalf("local after remove = (%q, %v), want preserved", data, err)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".profiled"),
		filepath.Join(fixture.repository, "modules", "profiled", "config"),
	)
	assertApplyNoMutation(t, fixture, fixture.run)

	before := snapshotTree(t, fixture.root)
	code, stdout, stderr := fixture.run("remove", "profiled")
	if code != exitError || stdout != "" || !strings.Contains(stderr, "active profile") {
		t.Fatalf("remove profiled = (%d, %q, %q), want refusal", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
}

func TestRemoveRedundantProfileExtraUsesProfileSelection(t *testing.T) {
	t.Run("applicable keeps the profile module active", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "app"})
		fixture.writeMachine(t, []string{"base"}, []string{"app"})
		if code, _, stderr := fixture.run("apply"); code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}

		code, _, stderr := fixture.run("remove", "app")

		if code != exitOK {
			t.Fatalf("remove profile+extra app = (%d, %q)", code, stderr)
		}
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want empty", extras)
		}
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".app"),
			filepath.Join(fixture.repository, "modules", "app", "config"),
		)
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("not applicable cleans old profile ownership", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[match]
os = ["macos"]

[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "app"})
		fixture.writeMachine(t, []string{"base"}, []string{"app"})
		fixture.env.platform = func() config.Platform {
			return cliTestPlatform("macos", "", "aarch64")
		}
		if code, _, stderr := fixture.runInjected("apply"); code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		fixture.env.platform = func() config.Platform {
			return cliTestPlatform("linux", "ubuntu", "x86_64")
		}

		code, _, stderr := fixture.runInjected("remove", "app")

		if code != exitOK {
			t.Fatalf("remove not-applicable profile+extra app = (%d, %q)", code, stderr)
		}
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want empty", extras)
		}
		assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
		if modules := loadTestState(t, fixture).Modules; len(modules) != 0 {
			t.Fatalf("state modules = %#v, want empty", modules)
		}
		assertApplyNoMutation(t, fixture, fixture.runInjected)
	})

	t.Run("indeterminate remains zero-write", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", distroGatedLinkManifest, map[string]string{
			"config": "app",
		})
		fixture.writeMachine(t, []string{"base"}, []string{"app"})
		if code, _, stderr := fixture.runInjected("apply"); code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		fixture.env.platform = func() config.Platform {
			return cliIndeterminateLinuxPlatform("distribution is unavailable")
		}
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected("remove", "app")

		if code != exitError ||
			stdout != "" ||
			!strings.Contains(stderr, "applicability is indeterminate") {
			t.Fatalf(
				"remove indeterminate profile+extra app = (%d, %q, %q), want blocker",
				code,
				stdout,
				stderr,
			)
		}
		assertSnapshotUnchanged(t, before)
	})

	t.Run("invalid manifest remains zero-write", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", "unknown = true", nil)
		fixture.writeMachine(t, []string{"base"}, []string{"app"})
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.run("remove", "app")

		if code != exitError ||
			stdout != "" ||
			!strings.Contains(stderr, "invalid configuration") {
			t.Fatalf(
				"remove invalid profile+extra app = (%d, %q, %q), want config failure",
				code,
				stdout,
				stderr,
			)
		}
		assertSnapshotUnchanged(t, before)
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, fixture.lock)
	})
}

func TestApplyRejectsDeletedProfileModuleWithoutMutation(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["gone"]`)
	fixture.writeMachine(t, []string{"base"}, nil)
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("apply")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "references missing module") {
		t.Fatalf("apply = (%d, %q, %q), want missing profile failure", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
}

func TestRemoveCleansDeletedExtraModule(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeMachine(t, []string{"base"}, []string{"gone"})
	target := filepath.Join(fixture.home, ".gone")
	destination := filepath.Join(fixture.repository, "modules", "gone", "removed")
	if err := os.Symlink(destination, target); err != nil {
		t.Fatalf("os.Symlink(stale target) error = %v", err)
	}
	resolved, err := corepaths.ResolveTarget(fixture.home, "~/.gone")
	if err != nil {
		t.Fatalf("ResolveTarget(stale target) error = %v", err)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"gone": {Placements: map[string]state.Placement{
				"config": {
					Kind:            state.KindLink,
					Target:          target,
					ResolvedTarget:  resolved.Resolved(),
					LinkDestination: destination,
				},
			}},
		},
	})

	code, _, stderr := fixture.run("remove", "gone")
	if code != exitOK {
		t.Fatalf("remove gone = (%d, %q)", code, stderr)
	}
	assertCLIMissing(t, target)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want empty", extras)
	}
	if modules := loadTestState(t, fixture).Modules; len(modules) != 0 {
		t.Fatalf("state modules = %#v, want empty", modules)
	}
	assertApplyNoMutation(t, fixture, fixture.run)
}

func TestRemoveInactiveKnownModuleWithoutStateIsNoop(t *testing.T) {
	fixture := newCLITestEnv(t, "base = []")
	fixture.writeModule(t, "idle", `
[[links]]
id = "config"
source = "config"
target = "~/.idle"
`, map[string]string{"config": "idle"})
	fixture.writeMachine(t, []string{"base"}, nil)
	target := filepath.Join(fixture.home, ".idle")
	before := snapshotPaths(t, fixture.config)

	for range 2 {
		code, stdout, stderr := fixture.run("remove", "idle")
		if code != exitOK ||
			!strings.Contains(stdout, "state_changed=false") ||
			!strings.Contains(stderr, "state is missing") {
			t.Fatalf("remove inactive module = (%d, %q, %q), want no-op", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, target)
	}
}

func TestRemoveDeletedExtrasPreservesRemainingSelection(t *testing.T) {
	t.Run("deleted extras are removed one by one", func(t *testing.T) {
		fixture := newCLITestEnv(t, "base = []")
		fixture.writeModule(t, "kept", `
[[links]]
id = "config"
source = "config"
target = "~/.kept"
`, map[string]string{"config": "kept"})
		fixture.writeMachine(t, []string{"base"}, []string{"gone-one", "gone-two", "kept"})
		targets := map[string]string{
			"gone-one": filepath.Join(fixture.home, ".gone-one"),
			"gone-two": filepath.Join(fixture.home, ".gone-two"),
		}
		modules := make(map[string]state.Module, len(targets))
		for moduleID, target := range targets {
			destination := filepath.Join(fixture.repository, "modules", moduleID, "removed")
			if err := os.Symlink(destination, target); err != nil {
				t.Fatalf("os.Symlink() error = %v", err)
			}
			resolved, err := corepaths.ResolveTarget(fixture.home, "~/"+filepath.Base(target))
			if err != nil {
				t.Fatalf("ResolveTarget() error = %v", err)
			}
			modules[moduleID] = state.Module{Placements: map[string]state.Placement{
				"config": {
					Kind:            state.KindLink,
					Target:          target,
					ResolvedTarget:  resolved.Resolved(),
					LinkDestination: destination,
				},
			}}
		}
		keptTarget := filepath.Join(fixture.home, ".kept")
		keptDestination := filepath.Join(fixture.repository, "modules", "kept", "config")
		if err := os.Symlink(keptDestination, keptTarget); err != nil {
			t.Fatalf("os.Symlink() error = %v", err)
		}
		keptResolved, err := corepaths.ResolveTarget(fixture.home, "~/.kept")
		if err != nil {
			t.Fatalf("ResolveTarget() error = %v", err)
		}
		modules["kept"] = state.Module{Placements: map[string]state.Placement{
			"config": {
				Kind:            state.KindLink,
				Target:          keptTarget,
				ResolvedTarget:  keptResolved.Resolved(),
				LinkDestination: keptDestination,
			},
		}}
		fixture.writeState(t, state.Snapshot{Home: fixture.home, Modules: modules})

		code, _, stderr := fixture.run("remove", "gone-one")
		if code != exitOK {
			t.Fatalf("remove gone-one = (%d, %q)", code, stderr)
		}
		assertCLIMissing(t, targets["gone-one"])
		assertCLILink(
			t,
			targets["gone-two"],
			filepath.Join(fixture.repository, "modules", "gone-two", "removed"),
		)
		if extras := fixture.loadMachine(t).ExtraModules; !reflect.DeepEqual(
			extras,
			[]string{"gone-two", "kept"},
		) {
			t.Fatalf("extra_modules after first remove = %v, want [gone-two kept]", extras)
		}
		loaded := loadTestState(t, fixture)
		if _, exists := loaded.Modules["gone-one"]; exists {
			t.Fatalf("state still contains gone-one: %#v", loaded)
		}
		if _, exists := loaded.Modules["gone-two"]; !exists {
			t.Fatalf("state lost gone-two: %#v", loaded)
		}
		if _, exists := loaded.Modules["kept"]; !exists {
			t.Fatalf("state lost kept: %#v", loaded)
		}
		assertCLILink(t, keptTarget, keptDestination)

		code, _, stderr = fixture.run("remove", "gone-two")
		if code != exitOK {
			t.Fatalf("remove gone-two = (%d, %q)", code, stderr)
		}
		assertCLIMissing(t, targets["gone-two"])
		if extras := fixture.loadMachine(t).ExtraModules; !reflect.DeepEqual(
			extras,
			[]string{"kept"},
		) {
			t.Fatalf("extra_modules after second remove = %v, want [kept]", extras)
		}
		loaded = loadTestState(t, fixture)
		if _, exists := loaded.Modules["kept"]; !exists || len(loaded.Modules) != 1 {
			t.Fatalf("state modules after cleanup = %#v, want only kept", loaded.Modules)
		}
		assertCLILink(t, keptTarget, keptDestination)
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("malformed remaining extra blocks cleanup", func(t *testing.T) {
		fixture := newCLITestEnv(t, "base = []")
		fixture.writeMachine(t, []string{"base"}, []string{"broken", "gone"})
		writeCLIFile(
			t,
			filepath.Join(fixture.repository, "modules", "broken", "module.toml"),
			"unknown = true\n",
		)
		target := filepath.Join(fixture.home, ".gone")
		destination := filepath.Join(fixture.repository, "modules", "gone", "removed")
		if err := os.Symlink(destination, target); err != nil {
			t.Fatalf("os.Symlink() error = %v", err)
		}
		resolved, err := corepaths.ResolveTarget(fixture.home, "~/.gone")
		if err != nil {
			t.Fatalf("ResolveTarget() error = %v", err)
		}
		fixture.writeState(t, state.Snapshot{
			Home: fixture.home,
			Modules: map[string]state.Module{
				"gone": {Placements: map[string]state.Placement{
					"config": {
						Kind:            state.KindLink,
						Target:          target,
						ResolvedTarget:  resolved.Resolved(),
						LinkDestination: destination,
					},
				}},
			},
		})
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.run("remove", "gone")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "broken") {
			t.Fatalf(
				"remove gone with broken extra = (%d, %q, %q), want strict loading failure",
				code,
				stdout,
				stderr,
			)
		}
		assertSnapshotUnchanged(t, before)
	})
}
