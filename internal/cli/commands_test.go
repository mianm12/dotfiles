package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

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

func TestStatusReportsExistingLocalWithoutProvenanceAsPending(t *testing.T) {
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

	beforeStatus := snapshotPaths(t, fixture.config, target)
	code, stdout, stderr := fixture.run("status")
	if code != exitOK ||
		!strings.Contains(stdout, "app  pending") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf("status before provenance = (%d, %q, %q), want pending", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, beforeStatus)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)

	code, stdout, stderr = fixture.run("apply")
	if code != exitOK ||
		!strings.Contains(stdout, "state_changed=true") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf("apply local provenance = (%d, %q, %q), want state-only mutation", code, stdout, stderr)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "personal" {
		t.Fatalf("local after apply = (%q, %v), want preserved", data, err)
	}

	beforeRepeat := snapshotPaths(t, fixture.config, fixture.state, fixture.lock, target)
	code, stdout, stderr = fixture.run("status")
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "app  converged") {
		t.Fatalf("status after provenance = (%d, %q, %q), want converged", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, beforeRepeat)
	assertApplyNoMutation(t, fixture, fixture.run)
}

func TestStatusReportsLinkStateRefreshAsPending(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/current/config"
`, map[string]string{"config": "portable"})
	fixture.writeMachine(t, []string{"base"}, nil)

	physicalA := filepath.Join(fixture.home, "physical-a")
	physicalB := filepath.Join(fixture.home, "physical-b")
	for _, directory := range []string{physicalA, physicalB} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", directory, err)
		}
	}
	parent := filepath.Join(fixture.home, "current")
	if err := os.Symlink(physicalA, parent); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", physicalA, parent, err)
	}

	code, _, stderr := fixture.run("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	destination := filepath.Join(fixture.repository, "modules", "app", "config")
	oldTarget := filepath.Join(physicalA, "config")
	assertCLILink(t, oldTarget, destination)

	if err := os.Remove(parent); err != nil {
		t.Fatalf("os.Remove(%q) error = %v", parent, err)
	}
	if err := os.Symlink(physicalB, parent); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", physicalB, parent, err)
	}
	newTarget := filepath.Join(physicalB, "config")
	if err := os.Symlink(destination, newTarget); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", destination, newTarget, err)
	}

	beforeStatus := snapshotPaths(
		t,
		fixture.config,
		fixture.state,
		fixture.lock,
		parent,
		oldTarget,
		newTarget,
	)
	code, stdout, stderr := fixture.run("status")
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "app  pending") {
		t.Fatalf("status before state refresh = (%d, %q, %q), want pending", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, beforeStatus)

	code, stdout, stderr = fixture.run("apply")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "targets_changed=false state_changed=true") {
		t.Fatalf("state refresh apply = (%d, %q, %q)", code, stdout, stderr)
	}
	assertApplyNoMutation(t, fixture, fixture.run)
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
