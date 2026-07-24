package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mianm12/dotfiles/internal/buildinfo"
	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/lock"
)

func TestAcceptance01_InitProfilesOnMacOSAndLinuxThroughCLI(t *testing.T) {
	tests := []struct {
		name     string
		platform config.Platform
	}{
		{
			name:     "macos",
			platform: config.Platform{OS: "macos", Arch: "aarch64"},
		},
		{
			name: "linux",
			platform: config.Platform{
				OS:     "linux",
				Distro: "ubuntu",
				Arch:   "x86_64",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newC1Fixture(t, `base = ["app"]`)
			fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
			fixture.env.platform = func() config.Platform { return test.platform }

			code, _, stderr := fixture.runC1Injected(
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
			assertC1ApplyNoMutation(t, fixture, fixture.runC1Injected)
		})
	}
}

func TestAcceptance02_InitConflictIsStrictlyReadOnlyThroughCLI(t *testing.T) {
	fixture := newC1Fixture(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.config/app/config"
`, map[string]string{"config": "portable"})
	target := filepath.Join(fixture.home, ".config", "app", "config")
	writeCLIFile(t, target, "personal")
	before := snapshotC1Tree(t, fixture.root)

	code, stdout, stderr := fixture.runC1(
		"init",
		fixture.repository,
		"--profile",
		"base",
	)
	if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
		t.Fatalf("init = (%d, %q, %q), want stderr-only conflict", code, stdout, stderr)
	}
	assertC1SnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.config)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
}

func TestAcceptance03_ProfileNotApplicableSkipsAndRepeatsThroughCLI(t *testing.T) {
	fixture := newC1Fixture(t, `base = ["portable", "gated"]`)
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

	code, _, stderr := fixture.runC1Injected("apply")
	if code != exitOK || stderr == "" {
		t.Fatalf("apply = (%d, %q), want success with missing-state warning", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".portable"),
		filepath.Join(fixture.repository, "modules", "portable", "config"),
	)
	assertCLIMissing(t, filepath.Join(fixture.home, ".gated"))

	code, stdout, stderr := fixture.runC1Injected("status")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "portable  converged") ||
		!strings.Contains(stdout, "gated  not-applicable") {
		t.Fatalf("status = (%d, %q, %q), want converged portable and skipped gated", code, stdout, stderr)
	}
	assertC1ApplyNoMutation(t, fixture, fixture.runC1Injected)

	explicit := newC1Fixture(t, `base = []`)
	explicit.writeModule(t, "gated", `
[match]
os = ["macos"]

[[links]]
id = "config"
source = "config"
target = "~/.gated"
`, map[string]string{"config": "gated"})
	explicit.writeMachine(t, []string{"base"}, nil)
	before := snapshotC1Tree(t, explicit.root)

	code, stdout, stderr = explicit.runC1Injected("apply", "gated")
	if code != exitError || stdout != "" || !strings.Contains(stderr, "not applicable") {
		t.Fatalf("explicit apply = (%d, %q, %q), want not-applicable failure", code, stdout, stderr)
	}
	assertC1SnapshotUnchanged(t, before)
	if extras := explicit.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged empty selection", extras)
	}
}

func TestAcceptance04_SourceContentChangeIsNoopThroughCLI(t *testing.T) {
	fixture := newC1Fixture(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "before"})
	fixture.writeMachine(t, []string{"base"}, nil)

	code, _, stderr := fixture.runC1("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	assertC1ApplyNoMutation(t, fixture, fixture.runC1)

	source := filepath.Join(fixture.repository, "modules", "app", "config")
	if err := os.WriteFile(source, []byte("after"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(source) error = %v", err)
	}
	target := filepath.Join(fixture.home, ".app")
	before := snapshotC1Paths(t, fixture.config, fixture.state, fixture.lock, target)

	code, stdout, stderr := fixture.runC1("apply")
	if code != exitOK || stderr != "" {
		t.Fatalf("apply after source content change = (%d, %q, %q)", code, stdout, stderr)
	}
	assertCLINoMutationResult(t, stdout)
	assertC1SnapshotUnchanged(t, before)
	assertCLILink(t, target, source)
}

func TestAcceptance05_PlacementChangesPruneOnlySafeLinksThroughCLI(t *testing.T) {
	t.Run("add and safe prune", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "old"
source = "old"
target = "~/.app-old"
`, map[string]string{
			"old": "old",
			"new": "new",
		})
		fixture.writeMachine(t, []string{"base"}, nil)

		code, _, stderr := fixture.runC1("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)

		writeC1ModuleManifest(t, fixture, "app", `
[[links]]
id = "new"
source = "new"
target = "~/.app-new"
`)
		code, _, stderr = fixture.runC1("apply")
		if code != exitOK {
			t.Fatalf("apply changed placements = (%d, %q)", code, stderr)
		}
		assertCLIMissing(t, filepath.Join(fixture.home, ".app-old"))
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".app-new"),
			filepath.Join(fixture.repository, "modules", "app", "new"),
		)
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
	})

	t.Run("drifted stale link warns and forgets", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "old"
source = "old"
target = "~/.app-old"
`, map[string]string{
			"old": "old",
			"new": "new",
		})
		fixture.writeMachine(t, []string{"base"}, nil)

		code, _, stderr := fixture.runC1("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)

		oldTarget := filepath.Join(fixture.home, ".app-old")
		if err := os.Remove(oldTarget); err != nil {
			t.Fatalf("os.Remove(old target) error = %v", err)
		}
		userDestination := filepath.Join(fixture.root, "user-owned")
		writeCLIFile(t, userDestination, "user")
		if err := os.Symlink(userDestination, oldTarget); err != nil {
			t.Fatalf("os.Symlink(user destination) error = %v", err)
		}
		writeC1ModuleManifest(t, fixture, "app", `
[[links]]
id = "new"
source = "new"
target = "~/.app-new"
`)

		code, _, stderr = fixture.runC1("apply")
		if code != exitOK || !strings.Contains(stderr, "warning") {
			t.Fatalf("apply with drifted stale link = (%d, %q), want warning success", code, stderr)
		}
		assertCLILink(t, oldTarget, userDestination)
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".app-new"),
			filepath.Join(fixture.repository, "modules", "app", "new"),
		)
		loaded := loadC1State(t, fixture)
		if placements := loaded.Modules["app"].Placements; len(placements) != 1 {
			t.Fatalf("state placements = %#v, want only new placement", placements)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
	})
}

func TestAcceptance06_TargetChangeConvergesAndRepeatsThroughCLI(t *testing.T) {
	fixture := newC1Fixture(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app-old"
`, map[string]string{"config": "config"})
	fixture.writeMachine(t, []string{"base"}, nil)

	code, _, stderr := fixture.runC1("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	assertC1ApplyNoMutation(t, fixture, fixture.runC1)

	writeC1ModuleManifest(t, fixture, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app-new"
`)
	code, _, stderr = fixture.runC1("apply")
	if code != exitOK {
		t.Fatalf("apply target change = (%d, %q)", code, stderr)
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".app-old"))
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".app-new"),
		filepath.Join(fixture.repository, "modules", "app", "config"),
	)
	assertC1ApplyNoMutation(t, fixture, fixture.runC1)

	failure := newC1Fixture(t, `base = ["app"]`)
	failure.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.old"
`, map[string]string{"config": "config"})
	failure.writeMachine(t, []string{"base"}, nil)
	code, _, stderr = failure.runC1("apply")
	if code != exitOK {
		t.Fatalf("initial ordering apply = (%d, %q)", code, stderr)
	}
	assertC1ApplyNoMutation(t, failure, failure.runC1)
	writeC1ModuleManifest(t, failure, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.new"
`)
	oldTarget := filepath.Join(failure.home, ".old")
	newTarget := filepath.Join(failure.home, ".new")
	beforeControl := snapshotC1Paths(
		t,
		failure.config,
		failure.state,
		failure.lock,
		oldTarget,
	)
	failure.env.beforeExecution = func() {
		writeCLIFile(t, newTarget, "user")
	}

	code, stdout, stderr := failure.runC1Injected("apply")
	if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
		t.Fatalf("ordered failure apply = (%d, %q, %q), want create failure", code, stdout, stderr)
	}
	assertC1SnapshotUnchanged(t, beforeControl)
	assertCLILink(
		t,
		oldTarget,
		filepath.Join(failure.repository, "modules", "app", "config"),
	)
	if data, err := os.ReadFile(newTarget); err != nil || string(data) != "user" {
		t.Fatalf("new target = (%q, %v), want injected user file", data, err)
	}
}

func TestAcceptance07_ExplicitApplyActivatesExtraAndRepeatsThroughCLI(t *testing.T) {
	fixture := newC1Fixture(t, `base = []`)
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
	fixture.writeMachine(t, []string{"base"}, nil)

	code, _, stderr := fixture.runC1("apply", "extra")
	if code != exitOK || stderr == "" {
		t.Fatalf("apply extra = (%d, %q), want success with missing-state warning", code, stderr)
	}
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 || extras[0] != "extra" {
		t.Fatalf("extra_modules = %v, want [extra]", extras)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".extra"),
		filepath.Join(fixture.repository, "modules", "extra", "config"),
	)
	assertC1ApplyNoMutation(t, fixture, fixture.runC1, "extra")
}

func TestAcceptance08_RemoveExtraAndRejectProfileModuleThroughCLI(t *testing.T) {
	fixture := newC1Fixture(t, `base = ["profiled"]`)
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

	code, _, stderr := fixture.runC1("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	assertC1ApplyNoMutation(t, fixture, fixture.runC1)

	code, _, stderr = fixture.runC1("remove", "extra")
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
	assertC1ApplyNoMutation(t, fixture, fixture.runC1)

	before := snapshotC1Tree(t, fixture.root)
	code, stdout, stderr := fixture.runC1("remove", "profiled")
	if code != exitError || stdout != "" || !strings.Contains(stderr, "active profile") {
		t.Fatalf("remove profiled = (%d, %q, %q), want refusal", code, stdout, stderr)
	}
	assertC1SnapshotUnchanged(t, before)
}

func TestAcceptance09_LocalCreateKeepAndExampleUpdateThroughCLI(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *c1Fixture, string)
	}{
		{name: "absent"},
		{
			name: "regular file",
			setup: func(t *testing.T, _ *c1Fixture, target string) {
				writeCLIFile(t, target, "user")
			},
		},
		{
			name: "directory",
			setup: func(t *testing.T, _ *c1Fixture, target string) {
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatalf("os.Mkdir(target) error = %v", err)
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, fixture *c1Fixture, target string) {
				source := filepath.Join(fixture.root, "user-local")
				writeCLIFile(t, source, "user")
				if err := os.Symlink(source, target); err != nil {
					t.Fatalf("os.Symlink(target) error = %v", err)
				}
			},
		},
		{
			name: "dangling symlink",
			setup: func(t *testing.T, fixture *c1Fixture, target string) {
				if err := os.Symlink(filepath.Join(fixture.root, "missing"), target); err != nil {
					t.Fatalf("os.Symlink(dangling target) error = %v", err)
				}
			},
		},
		{
			name: "special file",
			setup: func(t *testing.T, _ *c1Fixture, target string) {
				if err := syscall.Mkfifo(target, 0o600); err != nil {
					t.Fatalf("syscall.Mkfifo(target) error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newC1Fixture(t, `base = ["app"]`)
			fixture.writeModule(t, "app", `
[[locals]]
id = "local"
example = "local.example"
target = "~/.app.local"
`, map[string]string{"local.example": "example"})
			fixture.writeMachine(t, []string{"base"}, nil)
			target := filepath.Join(fixture.home, ".app.local")
			var beforeTarget c1Snapshot
			if test.setup != nil {
				test.setup(t, fixture, target)
				beforeTarget = snapshotC1Paths(t, target)
			}

			code, _, stderr := fixture.runC1("apply")
			if code != exitOK {
				t.Fatalf("initial apply = (%d, %q)", code, stderr)
			}
			if test.setup == nil {
				info, err := os.Lstat(target)
				if err != nil {
					t.Fatalf("os.Lstat(local) error = %v", err)
				}
				data, err := os.ReadFile(target)
				if err != nil || string(data) != "example" || info.Mode().Perm() != fs.FileMode(0o600) {
					t.Fatalf(
						"created local = (%q, %v, %v), want example mode 0600",
						data,
						info.Mode().Perm(),
						err,
					)
				}
			} else {
				assertC1SnapshotUnchanged(t, beforeTarget)
			}
			assertC1ApplyNoMutation(t, fixture, fixture.runC1)

			example := filepath.Join(
				fixture.repository,
				"modules",
				"app",
				"local.example",
			)
			if err := os.WriteFile(example, []byte("updated"), 0o600); err != nil {
				t.Fatalf("os.WriteFile(example) error = %v", err)
			}
			beforeTarget = snapshotC1Paths(t, target)
			code, stdout, stderr := fixture.runC1("apply")
			if code != exitOK || stderr != "" {
				t.Fatalf("apply after example update = (%d, %q, %q)", code, stdout, stderr)
			}
			assertCLINoMutationResult(t, stdout)
			assertC1SnapshotUnchanged(t, beforeTarget)
			if test.setup == nil {
				data, err := os.ReadFile(target)
				if err != nil || string(data) != "example" {
					t.Fatalf("local after example update = (%q, %v), want original", data, err)
				}
			}
		})
	}
}

func TestAcceptance10_AdoptDriftAndKindChangeThroughCLI(t *testing.T) {
	t.Run("adopt then reject drift", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["app"]`)
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
			t.Fatalf("os.Symlink(desired) error = %v", err)
		}
		beforeTarget := snapshotC1Paths(t, target)

		code, stdout, stderr := fixture.runC1("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr == "" {
			t.Fatalf("adopt apply = (%d, %q, %q)", code, stdout, stderr)
		}
		assertC1SnapshotUnchanged(t, beforeTarget)
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)

		if err := os.Remove(target); err != nil {
			t.Fatalf("os.Remove(target) error = %v", err)
		}
		userDestination := filepath.Join(fixture.root, "user-config")
		writeCLIFile(t, userDestination, "user")
		if err := os.Symlink(userDestination, target); err != nil {
			t.Fatalf("os.Symlink(user destination) error = %v", err)
		}
		before := snapshotC1Tree(t, fixture.root)
		code, stdout, stderr = fixture.runC1("apply")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
			t.Fatalf("apply after drift = (%d, %q, %q), want conflict", code, stdout, stderr)
		}
		assertC1SnapshotUnchanged(t, before)
	})

	t.Run("kind change conflicts", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[locals]]
id = "shared"
example = "local.example"
target = "~/.shared"
`, map[string]string{
			"config":        "config",
			"local.example": "local",
		})
		fixture.writeMachine(t, []string{"base"}, nil)

		code, _, stderr := fixture.runC1("apply")
		if code != exitOK {
			t.Fatalf("initial local apply = (%d, %q)", code, stderr)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
		writeC1ModuleManifest(t, fixture, "app", `
[[links]]
id = "shared"
source = "config"
target = "~/.shared"
`)
		before := snapshotC1Tree(t, fixture.root)
		code, stdout, stderr := fixture.runC1("apply")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
			t.Fatalf("apply after kind change = (%d, %q, %q), want conflict", code, stdout, stderr)
		}
		assertC1SnapshotUnchanged(t, before)
	})
}

func TestAcceptance11_ParentSymlinkDriftThroughCLI(t *testing.T) {
	t.Run("active update is rejected", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "old"
target = "~/alias/config"
`, map[string]string{
			"old": "old",
			"new": "new",
		})
		fixture.writeMachine(t, []string{"base"}, nil)
		firstParent, secondParent, alias := makeC1ParentAlias(t, fixture)

		code, _, stderr := fixture.runC1("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
		moveC1ParentAlias(t, alias, secondParent)
		oldDestination := filepath.Join(fixture.repository, "modules", "app", "old")
		if err := os.Symlink(oldDestination, filepath.Join(secondParent, "config")); err != nil {
			t.Fatalf("os.Symlink(second target) error = %v", err)
		}
		writeC1ModuleManifest(t, fixture, "app", `
[[links]]
id = "config"
source = "new"
target = "~/alias/config"
`)
		before := snapshotC1Tree(t, fixture.root)

		code, stdout, stderr := fixture.runC1("apply")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
			t.Fatalf("apply after parent drift = (%d, %q, %q), want conflict", code, stdout, stderr)
		}
		assertC1SnapshotUnchanged(t, before)
		assertCLILink(t, filepath.Join(firstParent, "config"), oldDestination)
		assertCLILink(t, filepath.Join(secondParent, "config"), oldDestination)
	})

	t.Run("stale prune warns and forgets", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/alias/config"
`, map[string]string{"config": "config"})
		fixture.writeMachine(t, []string{"base"}, nil)
		firstParent, secondParent, alias := makeC1ParentAlias(t, fixture)

		code, _, stderr := fixture.runC1("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
		moveC1ParentAlias(t, alias, secondParent)
		destination := filepath.Join(fixture.repository, "modules", "app", "config")
		if err := os.Symlink(destination, filepath.Join(secondParent, "config")); err != nil {
			t.Fatalf("os.Symlink(second target) error = %v", err)
		}
		writeC1ModuleManifest(t, fixture, "app", "")

		code, _, stderr = fixture.runC1("apply")
		if code != exitOK || !strings.Contains(stderr, "warning") {
			t.Fatalf("apply stale parent drift = (%d, %q), want warning success", code, stderr)
		}
		assertCLILink(t, filepath.Join(firstParent, "config"), destination)
		assertCLILink(t, filepath.Join(secondParent, "config"), destination)
		if modules := loadC1State(t, fixture).Modules; len(modules) != 0 {
			t.Fatalf("state modules = %#v, want stale ownership forgotten", modules)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
	})
}

func TestAcceptance12_TargetConflictsFailBeforeMutationThroughCLI(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		setup    func(*testing.T, *c1Fixture)
		want     string
	}{
		{
			name: "lexically equal targets",
			manifest: `
[[links]]
id = "first"
source = "first"
target = "~/.same"

[[links]]
id = "second"
source = "second"
target = "~/.config/../.same"
`,
		},
		{
			name: "resolved targets equal",
			manifest: `
[[links]]
id = "first"
source = "first"
target = "~/real/config"

[[links]]
id = "second"
source = "second"
target = "~/alias/config"
`,
			setup: func(t *testing.T, fixture *c1Fixture) {
				if err := os.Mkdir(filepath.Join(fixture.home, "real"), 0o700); err != nil {
					t.Fatalf("os.Mkdir(real) error = %v", err)
				}
				if err := os.Symlink("real", filepath.Join(fixture.home, "alias")); err != nil {
					t.Fatalf("os.Symlink(alias) error = %v", err)
				}
			},
		},
		{
			name: "dangling ancestor aliases missing directory",
			manifest: `
[[links]]
id = "first"
source = "first"
target = "~/alias/config"

[[links]]
id = "second"
source = "second"
target = "~/missing/config"
`,
			setup: func(t *testing.T, fixture *c1Fixture) {
				if err := os.Symlink("missing", filepath.Join(fixture.home, "alias")); err != nil {
					t.Fatalf("os.Symlink(dangling ancestor) error = %v", err)
				}
			},
			want: "path is blocked",
		},
		{
			name: "directory link owns descendant target",
			manifest: `
[[links]]
id = "first"
source = "directory"
target = "~/tree"

[[links]]
id = "second"
source = "second"
target = "~/tree/child"
`,
			setup: func(t *testing.T, fixture *c1Fixture) {
				root := filepath.Join(fixture.repository, "modules", "app")
				if err := os.Mkdir(filepath.Join(root, "directory"), 0o700); err != nil {
					t.Fatalf("os.Mkdir(directory source) error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newC1Fixture(t, `base = ["app"]`)
			fixture.writeModule(t, "app", test.manifest, map[string]string{
				"first":  "first",
				"second": "second",
			})
			fixture.writeMachine(t, []string{"base"}, nil)
			if test.setup != nil {
				test.setup(t, fixture)
			}
			before := snapshotC1Tree(t, fixture.root)

			code, stdout, stderr := fixture.runC1("apply")
			want := test.want
			if want == "" {
				want = "conflict"
			}
			if code != exitError || stdout != "" || !strings.Contains(stderr, want) {
				t.Fatalf("apply = (%d, %q, %q), want preflight %q failure", code, stdout, stderr, want)
			}
			assertC1SnapshotUnchanged(t, before)
			assertCLIMissing(t, fixture.state)
			assertCLIMissing(t, fixture.lock)
		})
	}
}

func TestAcceptance13_InterruptedFactsConvergeAndRepeatThroughCLI(t *testing.T) {
	t.Run("selection persisted before artifacts", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = []`)
		fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
		fixture.writeMachine(t, []string{"base"}, []string{"extra"})

		code, _, stderr := fixture.runC1("apply")
		if code != exitOK || stderr == "" {
			t.Fatalf("recovery apply = (%d, %q)", code, stderr)
		}
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".extra"),
			filepath.Join(fixture.repository, "modules", "extra", "config"),
		)
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
	})

	t.Run("link created before state commit", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["app"]`)
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

		code, stdout, stderr := fixture.runC1("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr == "" {
			t.Fatalf("recovery apply = (%d, %q, %q)", code, stdout, stderr)
		}
		assertCLILink(t, target, destination)
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
	})

	t.Run("local published before state commit", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[locals]]
id = "local"
example = "local.example"
target = "~/.app.local"
`, map[string]string{"local.example": "example"})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".app.local")
		writeCLIFile(t, target, "personal")

		code, stdout, stderr := fixture.runC1("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr == "" {
			t.Fatalf("recovery apply = (%d, %q, %q)", code, stdout, stderr)
		}
		if record := loadC1State(t, fixture).Modules["app"].Placements["local"]; record.Kind != state.KindLocal {
			t.Fatalf("local state record = %#v, want local provenance", record)
		}
		if data, err := os.ReadFile(target); err != nil || string(data) != "personal" {
			t.Fatalf("local = (%q, %v), want preserved personal bytes", data, err)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
	})

	t.Run("updated link before state commit", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["app"]`)
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
		writeC1LinkState(t, fixture, target, oldDestination)

		code, stdout, stderr := fixture.runC1("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr != "" {
			t.Fatalf("repair-state apply = (%d, %q, %q)", code, stdout, stderr)
		}
		record := loadC1State(t, fixture).Modules["app"].Placements["config"]
		if record.LinkDestination != newDestination {
			t.Fatalf("state destination = %q, want %q", record.LinkDestination, newDestination)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
	})

	t.Run("old link deleted during update", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["app"]`)
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
		writeC1LinkState(t, fixture, target, oldDestination)

		code, _, stderr := fixture.runC1("apply")
		if code != exitOK {
			t.Fatalf("recovery apply = (%d, %q)", code, stderr)
		}
		assertCLILink(
			t,
			target,
			filepath.Join(fixture.repository, "modules", "app", "new"),
		)
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
	})

	t.Run("prune completed before state commit", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["app"]`)
		fixture.writeModule(t, "app", "", map[string]string{"old": "old"})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".old")
		oldDestination := filepath.Join(fixture.repository, "modules", "app", "old")
		writeC1LinkState(t, fixture, target, oldDestination)

		code, _, stderr := fixture.runC1("apply")
		if code != exitOK {
			t.Fatalf("recovery apply = (%d, %q)", code, stderr)
		}
		if modules := loadC1State(t, fixture).Modules; len(modules) != 0 {
			t.Fatalf("state modules = %#v, want stale record forgotten", modules)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
	})
}

func TestAcceptance14_InvalidStateRejectsMutationThroughCLI(t *testing.T) {
	tests := []struct {
		name     string
		document string
		want     string
	}{
		{name: "corrupt", document: "{", want: "invalid state"},
		{
			name:     "legacy v1",
			document: `{"version":1,"entries":{},"run_once":{}}`,
			want:     "legacy state version",
		},
		{name: "too new", document: `{"version":3}`, want: "state version is newer"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newC1Fixture(t, `base = ["app"]`)
			fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "config"})
			fixture.writeMachine(t, []string{"base"}, nil)
			writeCLIFile(t, fixture.state, test.document)
			before := snapshotC1Tree(t, fixture.root)

			code, stdout, stderr := fixture.runC1("apply")
			if code != exitError || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("apply = (%d, %q, %q), want %q failure", code, stdout, stderr, test.want)
			}
			assertC1SnapshotUnchanged(t, before)
			assertCLIMissing(t, fixture.lock)
			assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
		})
	}
}

func TestAcceptance15_LockAndReadOnlyCommandsThroughCLI(t *testing.T) {
	t.Run("second process fails on held mutation lock", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = []`)
		fixture.writeMachine(t, []string{"base"}, nil)
		owner, err := lock.Acquire(filepath.Dir(fixture.lock), fixture.lock)
		if err != nil {
			t.Fatalf("lock.Acquire() error = %v", err)
		}
		defer func() {
			if err := owner.Release(); err != nil {
				t.Fatalf("owner.Release() error = %v", err)
			}
		}()

		code, stdout, stderr := fixture.runC1Process("apply")
		if code != exitError ||
			stdout != "" ||
			!strings.Contains(stderr, "another dot process") {
			t.Fatalf("locked apply = (%d, %q, %q), want lock failure", code, stdout, stderr)
		}
	})

	t.Run("status and dry-run are strictly read-only", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = []`)
		fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
		fixture.writeMachine(t, []string{"base"}, nil)
		before := snapshotC1Tree(t, fixture.root)

		code, _, stderr := fixture.runC1("status")
		if code != exitOK || stderr == "" {
			t.Fatalf("status = (%d, %q)", code, stderr)
		}
		code, stdout, stderr := fixture.runC1("apply", "extra", "--dry-run")
		if code != exitOK ||
			!strings.Contains(stdout, "create-link") ||
			stderr == "" {
			t.Fatalf("dry-run = (%d, %q, %q)", code, stdout, stderr)
		}
		assertC1SnapshotUnchanged(t, before)
		assertCLIMissing(t, fixture.lock)
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want unchanged", extras)
		}
	})
}

func TestAcceptance16_DeletedProfileFailsAndDeletedExtraCleansThroughCLI(t *testing.T) {
	t.Run("active profile references deleted module", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = ["gone"]`)
		fixture.writeMachine(t, []string{"base"}, nil)
		before := snapshotC1Tree(t, fixture.root)

		code, stdout, stderr := fixture.runC1("apply")
		if code != exitError ||
			stdout != "" ||
			!strings.Contains(stderr, "references missing module") {
			t.Fatalf("apply = (%d, %q, %q), want missing profile failure", code, stdout, stderr)
		}
		assertC1SnapshotUnchanged(t, before)
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, fixture.lock)
	})

	t.Run("deleted extra and state are removable", func(t *testing.T) {
		fixture := newC1Fixture(t, `base = []`)
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

		code, _, stderr := fixture.runC1("remove", "gone")
		if code != exitOK {
			t.Fatalf("remove gone = (%d, %q)", code, stderr)
		}
		assertCLIMissing(t, target)
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want empty", extras)
		}
		if modules := loadC1State(t, fixture).Modules; len(modules) != 0 {
			t.Fatalf("state modules = %#v, want empty", modules)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.runC1)
	})
}

func TestAcceptance17_ScopedApplyAndRemoveIgnoreBrokenOutOfScopeModule(t *testing.T) {
	fixture := newC1Fixture(t, `base = []`)
	fixture.writeModule(t, "apply-good", `
[[links]]
id = "config"
source = "config"
target = "~/.apply-good"
`, map[string]string{"config": "apply-good"})
	fixture.writeModule(t, "remove-good", `
[[links]]
id = "config"
source = "config"
target = "~/.remove-good"
`, map[string]string{"config": "remove-good"})
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "modules", "broken", "module.toml"),
		"unknown = true\n",
	)
	fixture.writeMachine(t, []string{"base"}, []string{"remove-good"})

	code, _, stderr := fixture.runC1("apply", "apply-good")
	if code != exitOK {
		t.Fatalf("scoped apply with broken out-of-scope module = (%d, %q)", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".apply-good"),
		filepath.Join(fixture.repository, "modules", "apply-good", "config"),
	)
	assertC1ApplyNoMutation(t, fixture, fixture.runC1, "apply-good")

	code, _, stderr = fixture.runC1("apply", "remove-good")
	if code != exitOK {
		t.Fatalf("scoped apply remove-good = (%d, %q)", code, stderr)
	}
	assertC1ApplyNoMutation(t, fixture, fixture.runC1, "remove-good")

	code, _, stderr = fixture.runC1("remove", "remove-good")
	if code != exitOK {
		t.Fatalf("remove with broken out-of-scope module = (%d, %q)", code, stderr)
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".remove-good"))
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 ||
		extras[0] != "apply-good" {
		t.Fatalf("extra_modules = %v, want [apply-good]", extras)
	}
	assertC1ApplyNoMutation(t, fixture, fixture.runC1)

	before := snapshotC1Tree(t, fixture.root)
	code, stdout, stderr := fixture.runC1("apply", "broken")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, `module "broken"`) {
		t.Fatalf("explicit broken apply = (%d, %q, %q), want strict failure", code, stdout, stderr)
	}
	assertC1SnapshotUnchanged(t, before)
}

func TestAcceptance18_InvalidSourcesAndExamplesRejectMutationThroughCLI(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		files    map[string]string
		setup    func(*testing.T, *c1Fixture)
	}{
		{
			name: "missing link source",
			manifest: `
[[links]]
id = "config"
source = "missing"
target = "~/.invalid"
`,
		},
		{
			name: "link source is symlink",
			manifest: `
[[links]]
id = "config"
source = "alias"
target = "~/.invalid"
`,
			files: map[string]string{"real": "real"},
			setup: func(t *testing.T, fixture *c1Fixture) {
				root := filepath.Join(fixture.repository, "modules", "app")
				if err := os.Symlink("real", filepath.Join(root, "alias")); err != nil {
					t.Fatalf("os.Symlink(source alias) error = %v", err)
				}
			},
		},
		{
			name: "link source is special",
			manifest: `
[[links]]
id = "config"
source = "fifo"
target = "~/.invalid"
`,
			setup: func(t *testing.T, fixture *c1Fixture) {
				root := filepath.Join(fixture.repository, "modules", "app")
				if err := syscall.Mkfifo(filepath.Join(root, "fifo"), 0o600); err != nil {
					t.Fatalf("syscall.Mkfifo(link source) error = %v", err)
				}
			},
		},
		{
			name: "missing local example",
			manifest: `
[[locals]]
id = "local"
example = "missing.example"
target = "~/.invalid"
`,
		},
		{
			name: "local example is directory",
			manifest: `
[[locals]]
id = "local"
example = "example"
target = "~/.invalid"
`,
			setup: func(t *testing.T, fixture *c1Fixture) {
				root := filepath.Join(fixture.repository, "modules", "app")
				if err := os.Mkdir(filepath.Join(root, "example"), 0o700); err != nil {
					t.Fatalf("os.Mkdir(example) error = %v", err)
				}
			},
		},
		{
			name: "local example is symlink",
			manifest: `
[[locals]]
id = "local"
example = "alias.example"
target = "~/.invalid"
`,
			files: map[string]string{"real.example": "real"},
			setup: func(t *testing.T, fixture *c1Fixture) {
				root := filepath.Join(fixture.repository, "modules", "app")
				if err := os.Symlink(
					"real.example",
					filepath.Join(root, "alias.example"),
				); err != nil {
					t.Fatalf("os.Symlink(example alias) error = %v", err)
				}
			},
		},
		{
			name: "local example is special",
			manifest: `
[[locals]]
id = "local"
example = "fifo.example"
target = "~/.invalid"
`,
			setup: func(t *testing.T, fixture *c1Fixture) {
				root := filepath.Join(fixture.repository, "modules", "app")
				if err := syscall.Mkfifo(
					filepath.Join(root, "fifo.example"),
					0o600,
				); err != nil {
					t.Fatalf("syscall.Mkfifo(local example) error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newC1Fixture(t, `base = ["app"]`)
			fixture.writeModule(t, "app", test.manifest, test.files)
			fixture.writeMachine(t, []string{"base"}, nil)
			if test.setup != nil {
				test.setup(t, fixture)
			}
			before := snapshotC1Tree(t, fixture.root)

			code, stdout, stderr := fixture.runC1("apply")
			if code != exitError ||
				stdout != "" ||
				(!strings.Contains(stderr, "source") &&
					!strings.Contains(stderr, "example")) {
				t.Fatalf("apply = (%d, %q, %q), want strict source/example failure", code, stdout, stderr)
			}
			assertC1SnapshotUnchanged(t, before)
			assertCLIMissing(t, fixture.state)
			assertCLIMissing(t, fixture.lock)
			assertCLIMissing(t, filepath.Join(fixture.home, ".invalid"))
		})
	}
}

func TestAcceptance19_UnknownPlatformAndInvalidOSThroughCLI(t *testing.T) {
	fixture := newC1Fixture(t, `base = ["portable", "gated"]`)
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
		return config.Platform{OS: "linux", Distro: "gentoo", Arch: "riscv64"}
	}

	code, _, stderr := fixture.runC1Injected("apply")
	if code != exitOK || stderr == "" {
		t.Fatalf("unknown-platform apply = (%d, %q)", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".portable"),
		filepath.Join(fixture.repository, "modules", "portable", "config"),
	)
	assertCLIMissing(t, filepath.Join(fixture.home, ".gated"))
	code, stdout, stderr := fixture.runC1Injected("status")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "portable  converged") ||
		!strings.Contains(stdout, "gated  not-applicable") {
		t.Fatalf("unknown-platform status = (%d, %q, %q)", code, stdout, stderr)
	}
	assertC1ApplyNoMutation(t, fixture, fixture.runC1Injected)

	before := snapshotC1Tree(t, fixture.root)
	code, stdout, stderr = fixture.runC1Injected("apply", "invalid-os")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "unsupported os token") {
		t.Fatalf("invalid-os apply = (%d, %q, %q), want strict config failure", code, stdout, stderr)
	}
	assertC1SnapshotUnchanged(t, before)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged empty selection", extras)
	}
}

func makeC1ParentAlias(
	t *testing.T,
	fixture *c1Fixture,
) (firstParent, secondParent, alias string) {
	t.Helper()
	firstParent = filepath.Join(fixture.root, "first-parent")
	secondParent = filepath.Join(fixture.root, "second-parent")
	for _, directory := range []string{firstParent, secondParent} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", directory, err)
		}
	}
	alias = filepath.Join(fixture.home, "alias")
	if err := os.Symlink(firstParent, alias); err != nil {
		t.Fatalf("os.Symlink(first parent) error = %v", err)
	}
	return firstParent, secondParent, alias
}

func moveC1ParentAlias(t *testing.T, alias, destination string) {
	t.Helper()
	if err := os.Remove(alias); err != nil {
		t.Fatalf("os.Remove(alias) error = %v", err)
	}
	if err := os.Symlink(destination, alias); err != nil {
		t.Fatalf("os.Symlink(moved alias) error = %v", err)
	}
}

type c1Fixture struct {
	root       string
	home       string
	repository string
	config     string
	state      string
	lock       string
	env        environment
}

func newC1Fixture(t *testing.T, profiles string) *c1Fixture {
	t.Helper()
	root := t.TempDir()
	fixture := &c1Fixture{
		root:       root,
		home:       filepath.Join(root, "home"),
		repository: filepath.Join(root, "repository"),
	}
	fixture.config = filepath.Join(fixture.home, ".config", "dot", "config.toml")
	fixture.state = filepath.Join(fixture.home, ".local", "state", "dot", "state.json")
	fixture.lock = filepath.Join(fixture.home, ".local", "state", "dot", "lock")
	for _, directory := range []string{fixture.home, fixture.repository} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "dot.toml"),
		"version = 1\n[profiles]\n"+profiles+"\n",
	)
	fixture.env = environment{
		stdin: strings.NewReader(""),
		getwd: func() (string, error) {
			return fixture.repository, nil
		},
		userHomeDir: func() (string, error) {
			return fixture.home, nil
		},
		platform: func() config.Platform {
			return config.Platform{OS: "linux", Distro: "ubuntu", Arch: "x86_64"}
		},
		build: buildinfo.Info{Version: "test", Commit: "test", BuildTime: "test"},
	}
	paths := map[string]string{
		"root":       fixture.root,
		"HOME":       fixture.home,
		"repository": fixture.repository,
		"config":     fixture.config,
		"state":      fixture.state,
		"lock":       fixture.lock,
	}
	for name, path := range paths {
		if !filepath.IsAbs(path) {
			t.Fatalf("%s path %q is not absolute", name, path)
		}
		relative, err := filepath.Rel(fixture.root, path)
		if err != nil ||
			relative == ".." ||
			strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Fatalf("%s path %q is outside synthetic root %q", name, path, fixture.root)
		}
	}
	t.Setenv("HOME", fixture.home)
	return fixture
}

func (fixture *c1Fixture) runC1(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func (fixture *c1Fixture) runC1Injected(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	env := fixture.env
	env.stdout = &stdout
	env.stderr = &stderr
	code := run(args, env)
	return code, stdout.String(), stderr.String()
}

func (fixture *c1Fixture) runC1Process(args ...string) (int, string, string) {
	commandArgs := []string{"-test.run=^TestC1CLIHelperProcess$", "--"}
	commandArgs = append(commandArgs, args...)
	command := exec.Command(os.Args[0], commandArgs...)
	command.Env = append(os.Environ(), "DOT_C1_CLI_HELPER_PROCESS=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	if err == nil {
		return exitOK, stdout.String(), stderr.String()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), stdout.String(), stderr.String()
	}
	return -1, stdout.String(), stderr.String() + err.Error()
}

func (fixture *c1Fixture) writeMachine(
	t *testing.T,
	profiles, extras []string,
) {
	t.Helper()
	if _, err := config.PublishMachine(fixture.config, config.Machine{
		Version:      1,
		Repository:   fixture.repository,
		Profiles:     append([]string(nil), profiles...),
		ExtraModules: append([]string(nil), extras...),
	}); err != nil {
		t.Fatalf("PublishMachine() error = %v", err)
	}
}

func (fixture *c1Fixture) loadMachine(t *testing.T) config.Machine {
	t.Helper()
	machine, exists, err := config.LoadMachine(fixture.config)
	if err != nil || !exists {
		t.Fatalf("LoadMachine() = (%#v, %t, %v)", machine, exists, err)
	}
	return machine
}

func (fixture *c1Fixture) writeModule(
	t *testing.T,
	id, manifest string,
	files map[string]string,
) {
	t.Helper()
	root := filepath.Join(fixture.repository, "modules", id)
	writeCLIFile(
		t,
		filepath.Join(root, "module.toml"),
		strings.TrimSpace(manifest)+"\n",
	)
	for relative, content := range files {
		writeCLIFile(t, filepath.Join(root, filepath.FromSlash(relative)), content)
	}
}

func (fixture *c1Fixture) writeState(t *testing.T, snapshot state.Snapshot) {
	t.Helper()
	data, err := state.Marshal(snapshot)
	if err != nil {
		t.Fatalf("state.Marshal() error = %v", err)
	}
	writeCLIFile(t, fixture.state, string(data))
}

func TestC1CLIHelperProcess(t *testing.T) {
	if os.Getenv("DOT_C1_CLI_HELPER_PROCESS") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		os.Exit(exitUsage)
	}
	os.Exit(Run(os.Args[separator+1:], os.Stdin, os.Stdout, os.Stderr))
}

type c1PathSnapshot struct {
	path     string
	info     fs.FileInfo
	mode     fs.FileMode
	data     string
	link     string
	modified int64
	size     int64
}

type c1Snapshot struct {
	root    string
	entries []c1PathSnapshot
}

func snapshotC1Tree(t *testing.T, root string) c1Snapshot {
	t.Helper()
	var paths []string
	if err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	}); err != nil {
		t.Fatalf("filepath.WalkDir(%q) error = %v", root, err)
	}
	return c1Snapshot{root: root, entries: snapshotC1ExactPaths(t, paths...)}
}

func snapshotC1Paths(t *testing.T, paths ...string) c1Snapshot {
	t.Helper()
	return c1Snapshot{entries: snapshotC1ExactPaths(t, paths...)}
}

func snapshotC1ExactPaths(t *testing.T, paths ...string) []c1PathSnapshot {
	t.Helper()
	result := make([]c1PathSnapshot, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("os.Lstat(%q) error = %v", path, err)
		}
		entry := c1PathSnapshot{
			path:     path,
			info:     info,
			mode:     info.Mode(),
			modified: info.ModTime().UnixNano(),
			size:     info.Size(),
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			entry.link, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var data []byte
			data, err = os.ReadFile(path)
			entry.data = string(data)
		}
		if err != nil {
			t.Fatalf("snapshot %q error = %v", path, err)
		}
		result = append(result, entry)
	}
	return result
}

func assertC1SnapshotUnchanged(t *testing.T, before c1Snapshot) {
	t.Helper()
	var after []c1PathSnapshot
	if before.root != "" {
		after = snapshotC1Tree(t, before.root).entries
	} else {
		paths := make([]string, len(before.entries))
		for index := range before.entries {
			paths[index] = before.entries[index].path
		}
		after = snapshotC1ExactPaths(t, paths...)
	}
	if len(after) != len(before.entries) {
		t.Fatalf(
			"filesystem entry count changed: before=%d after=%d",
			len(before.entries),
			len(after),
		)
	}
	for index := range before.entries {
		oldEntry := before.entries[index]
		newEntry := after[index]
		if oldEntry.path != newEntry.path ||
			oldEntry.mode != newEntry.mode ||
			oldEntry.data != newEntry.data ||
			oldEntry.link != newEntry.link ||
			oldEntry.modified != newEntry.modified ||
			oldEntry.size != newEntry.size ||
			!os.SameFile(oldEntry.info, newEntry.info) {
			t.Fatalf(
				"path changed\nbefore=%#v\nafter=%#v",
				oldEntry,
				newEntry,
			)
		}
	}
}

func assertC1ApplyNoMutation(
	t *testing.T,
	fixture *c1Fixture,
	run func(...string) (int, string, string),
	module ...string,
) {
	t.Helper()
	before := snapshotC1Tree(t, fixture.root)
	args := append([]string{"apply"}, module...)
	code, stdout, stderr := run(args...)
	if code != exitOK || stderr != "" {
		t.Fatalf("repeated apply = (%d, %q, %q)", code, stdout, stderr)
	}
	assertCLINoMutationResult(t, stdout)
	assertC1SnapshotUnchanged(t, before)
}

func writeC1ModuleManifest(t *testing.T, fixture *c1Fixture, id, manifest string) {
	t.Helper()
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "modules", id, "module.toml"),
		strings.TrimSpace(manifest)+"\n",
	)
}

func loadC1State(t *testing.T, fixture *c1Fixture) state.Snapshot {
	t.Helper()
	loaded, err := state.Load(fixture.state, fixture.home)
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}
	return loaded.Snapshot
}

func writeC1LinkState(
	t *testing.T,
	fixture *c1Fixture,
	target, destination string,
) {
	t.Helper()
	relative, err := filepath.Rel(fixture.home, target)
	if err != nil {
		t.Fatalf("filepath.Rel(target) error = %v", err)
	}
	resolved, err := corepaths.ResolveTarget(
		fixture.home,
		"~/"+filepath.ToSlash(relative),
	)
	if err != nil {
		t.Fatalf("ResolveTarget(target) error = %v", err)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"app": {Placements: map[string]state.Placement{
				"config": {
					Kind:            state.KindLink,
					Target:          target,
					ResolvedTarget:  resolved.Resolved(),
					LinkDestination: destination,
				},
			}},
		},
	})
}
