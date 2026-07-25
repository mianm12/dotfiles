package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/lock"
)

func TestAcceptanceContractCoverage(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not return the acceptance test filename")
	}
	file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatalf("parse acceptance suite: %v", err)
	}

	counts := make(map[int]int)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(function.Name.Name, "TestAC") {
			continue
		}
		name := function.Name.Name
		if len(name) < len("TestAC00_") || name[8] != '_' {
			t.Fatalf("acceptance test %q must use TestACNN_ naming", name)
		}
		number, convertErr := strconv.Atoi(name[6:8])
		if convertErr != nil || number < 1 || number > 19 {
			t.Fatalf("acceptance test %q has an invalid contract number", name)
		}
		counts[number]++
	}
	for number := 1; number <= 19; number++ {
		if counts[number] == 0 {
			t.Errorf("acceptance contract AC-%02d has no CLI test", number)
		}
	}
}

func TestAC01_InitProfilesOnMacOSAndLinuxThroughCLI(t *testing.T) {
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

func TestAC02_InitConflictIsStrictlyReadOnlyThroughCLI(t *testing.T) {
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

func TestAC03_ProfileNotApplicableSkipsAndRepeatsThroughCLI(t *testing.T) {
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
	if code != exitOK || stderr == "" {
		t.Fatalf("apply = (%d, %q), want success with missing-state warning", code, stderr)
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
		!strings.Contains(stdout, "portable  converged") ||
		!strings.Contains(stdout, "gated  not-applicable") {
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
	explicit.writeMachine(t, []string{"base"}, nil)
	before := snapshotTree(t, explicit.root)

	code, stdout, stderr = explicit.runInjected("apply", "gated")
	if code != exitError || stdout != "" || !strings.Contains(stderr, "not applicable") {
		t.Fatalf("explicit apply = (%d, %q, %q), want not-applicable failure", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	if extras := explicit.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged empty selection", extras)
	}
}

func TestAC04_SourceContentChangeIsNoopThroughCLI(t *testing.T) {
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

func TestAC05_PlacementChangesPruneOnlySafeLinksThroughCLI(t *testing.T) {
	t.Run("add and safe prune", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
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

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		assertApplyNoMutation(t, fixture, fixture.run)

		writeModuleManifest(t, fixture, "app", `
[[links]]
id = "new"
source = "new"
target = "~/.app-new"
`)
		code, _, stderr = fixture.run("apply")
		if code != exitOK {
			t.Fatalf("apply changed placements = (%d, %q)", code, stderr)
		}
		assertCLIMissing(t, filepath.Join(fixture.home, ".app-old"))
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".app-new"),
			filepath.Join(fixture.repository, "modules", "app", "new"),
		)
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("drifted stale link warns and forgets", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
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

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		assertApplyNoMutation(t, fixture, fixture.run)

		oldTarget := filepath.Join(fixture.home, ".app-old")
		if err := os.Remove(oldTarget); err != nil {
			t.Fatalf("os.Remove(old target) error = %v", err)
		}
		userDestination := filepath.Join(fixture.root, "user-owned")
		writeCLIFile(t, userDestination, "user")
		if err := os.Symlink(userDestination, oldTarget); err != nil {
			t.Fatalf("os.Symlink(user destination) error = %v", err)
		}
		writeModuleManifest(t, fixture, "app", `
[[links]]
id = "new"
source = "new"
target = "~/.app-new"
`)

		code, _, stderr = fixture.run("apply")
		if code != exitOK || !strings.Contains(stderr, "warning") {
			t.Fatalf("apply with drifted stale link = (%d, %q), want warning success", code, stderr)
		}
		assertCLILink(t, oldTarget, userDestination)
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".app-new"),
			filepath.Join(fixture.repository, "modules", "app", "new"),
		)
		loaded := loadTestState(t, fixture)
		if placements := loaded.Modules["app"].Placements; len(placements) != 1 {
			t.Fatalf("state placements = %#v, want only new placement", placements)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
	})
}

func TestAC06_TargetChangeConvergesAndRepeatsThroughCLI(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app-old"
`, map[string]string{"config": "config"})
	fixture.writeMachine(t, []string{"base"}, nil)

	code, _, stderr := fixture.run("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	assertApplyNoMutation(t, fixture, fixture.run)

	writeModuleManifest(t, fixture, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app-new"
`)
	code, _, stderr = fixture.run("apply")
	if code != exitOK {
		t.Fatalf("apply target change = (%d, %q)", code, stderr)
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".app-old"))
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".app-new"),
		filepath.Join(fixture.repository, "modules", "app", "config"),
	)
	assertApplyNoMutation(t, fixture, fixture.run)

	failure := newCLITestEnv(t, `base = ["app"]`)
	failure.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.old"
`, map[string]string{"config": "config"})
	failure.writeMachine(t, []string{"base"}, nil)
	code, _, stderr = failure.run("apply")
	if code != exitOK {
		t.Fatalf("initial ordering apply = (%d, %q)", code, stderr)
	}
	assertApplyNoMutation(t, failure, failure.run)
	writeModuleManifest(t, failure, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.blocked/new"
`)
	oldTarget := filepath.Join(failure.home, ".old")
	blockedParent := filepath.Join(failure.home, ".blocked")
	if err := os.Mkdir(blockedParent, 0o700); err != nil {
		t.Fatalf("os.Mkdir(blocked parent) error = %v", err)
	}
	newTarget := filepath.Join(blockedParent, "new")
	beforeControl := snapshotPaths(
		t,
		failure.config,
		failure.state,
		failure.lock,
		oldTarget,
	)
	failure.env.beforeExecution = func() {
		if err := os.Chmod(blockedParent, 0o500); err != nil {
			t.Fatalf("os.Chmod(blocked parent) error = %v", err)
		}
	}

	code, stdout, stderr := failure.runInjected("apply")
	if err := os.Chmod(blockedParent, 0o700); err != nil {
		t.Fatalf("os.Chmod(restore parent) error = %v", err)
	}
	if code != exitError || stdout != "" || !strings.Contains(stderr, "create symlink") {
		t.Fatalf("ordered failure apply = (%d, %q, %q), want execution-time create failure", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, beforeControl)
	assertCLILink(
		t,
		oldTarget,
		filepath.Join(failure.repository, "modules", "app", "config"),
	)
	assertCLIMissing(t, newTarget)
}

func TestAC07_ExplicitApplyActivatesExtraAndRepeatsThroughCLI(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
	fixture.writeMachine(t, []string{"base"}, nil)

	code, _, stderr := fixture.run("apply", "extra")
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
	assertApplyNoMutation(t, fixture, fixture.run, "extra")
}

func TestAC08_RemoveExtraAndRejectProfileModuleThroughCLI(t *testing.T) {
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

func TestAC09_LocalCreateKeepAndExampleUpdateThroughCLI(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *cliTestEnv, string)
	}{
		{name: "absent"},
		{
			name: "regular file",
			setup: func(t *testing.T, _ *cliTestEnv, target string) {
				writeCLIFile(t, target, "user")
			},
		},
		{
			name: "directory",
			setup: func(t *testing.T, _ *cliTestEnv, target string) {
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatalf("os.Mkdir(target) error = %v", err)
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, fixture *cliTestEnv, target string) {
				source := filepath.Join(fixture.root, "user-local")
				writeCLIFile(t, source, "user")
				if err := os.Symlink(source, target); err != nil {
					t.Fatalf("os.Symlink(target) error = %v", err)
				}
			},
		},
		{
			name: "dangling symlink",
			setup: func(t *testing.T, fixture *cliTestEnv, target string) {
				if err := os.Symlink(filepath.Join(fixture.root, "missing"), target); err != nil {
					t.Fatalf("os.Symlink(dangling target) error = %v", err)
				}
			},
		},
		{
			name: "special file",
			setup: func(t *testing.T, _ *cliTestEnv, target string) {
				if err := syscall.Mkfifo(target, 0o600); err != nil {
					t.Fatalf("syscall.Mkfifo(target) error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = ["app"]`)
			fixture.writeModule(t, "app", `
[[locals]]
id = "local"
example = "local.example"
target = "~/.app.local"
`, map[string]string{"local.example": "example"})
			fixture.writeMachine(t, []string{"base"}, nil)
			target := filepath.Join(fixture.home, ".app.local")
			var beforeTarget filesystemSnapshot
			if test.setup != nil {
				test.setup(t, fixture, target)
				beforeTarget = snapshotPaths(t, target)
			}

			code, _, stderr := fixture.run("apply")
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
				assertSnapshotUnchanged(t, beforeTarget)
			}
			assertApplyNoMutation(t, fixture, fixture.run)

			example := filepath.Join(
				fixture.repository,
				"modules",
				"app",
				"local.example",
			)
			if err := os.WriteFile(example, []byte("updated"), 0o600); err != nil {
				t.Fatalf("os.WriteFile(example) error = %v", err)
			}
			beforeTarget = snapshotPaths(t, target)
			code, stdout, stderr := fixture.run("apply")
			if code != exitOK || stderr != "" {
				t.Fatalf("apply after example update = (%d, %q, %q)", code, stdout, stderr)
			}
			assertCLINoMutationResult(t, stdout)
			assertSnapshotUnchanged(t, beforeTarget)
			if test.setup == nil {
				data, err := os.ReadFile(target)
				if err != nil || string(data) != "example" {
					t.Fatalf("local after example update = (%q, %v), want original", data, err)
				}
			}
		})
	}
}

func TestAC10_AdoptDriftAndKindChangeThroughCLI(t *testing.T) {
	t.Run("adopt then reject drift", func(t *testing.T) {
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
			t.Fatalf("os.Symlink(desired) error = %v", err)
		}
		beforeTarget := snapshotPaths(t, target)

		code, stdout, stderr := fixture.run("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr == "" {
			t.Fatalf("adopt apply = (%d, %q, %q)", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, beforeTarget)
		assertApplyNoMutation(t, fixture, fixture.run)

		if err := os.Remove(target); err != nil {
			t.Fatalf("os.Remove(target) error = %v", err)
		}
		userDestination := filepath.Join(fixture.root, "user-config")
		writeCLIFile(t, userDestination, "user")
		if err := os.Symlink(userDestination, target); err != nil {
			t.Fatalf("os.Symlink(user destination) error = %v", err)
		}
		before := snapshotTree(t, fixture.root)
		code, stdout, stderr = fixture.run("apply")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
			t.Fatalf("apply after drift = (%d, %q, %q), want conflict", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
	})

	t.Run("kind change conflicts", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
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

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("initial local apply = (%d, %q)", code, stderr)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
		writeModuleManifest(t, fixture, "app", `
[[links]]
id = "shared"
source = "config"
target = "~/.shared"
`)
		before := snapshotTree(t, fixture.root)
		code, stdout, stderr := fixture.run("apply")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
			t.Fatalf("apply after kind change = (%d, %q, %q), want conflict", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
	})
}

func TestAC11_ParentSymlinkDriftThroughCLI(t *testing.T) {
	t.Run("active update is rejected", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
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
		firstParent, secondParent, alias := makeParentAlias(t, fixture)

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
		moveParentAlias(t, alias, secondParent)
		oldDestination := filepath.Join(fixture.repository, "modules", "app", "old")
		if err := os.Symlink(oldDestination, filepath.Join(secondParent, "config")); err != nil {
			t.Fatalf("os.Symlink(second target) error = %v", err)
		}
		writeModuleManifest(t, fixture, "app", `
[[links]]
id = "config"
source = "new"
target = "~/alias/config"
`)
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.run("apply")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
			t.Fatalf("apply after parent drift = (%d, %q, %q), want conflict", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
		assertCLILink(t, filepath.Join(firstParent, "config"), oldDestination)
		assertCLILink(t, filepath.Join(secondParent, "config"), oldDestination)
	})

	t.Run("stale prune warns and forgets", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/alias/config"
`, map[string]string{"config": "config"})
		fixture.writeMachine(t, []string{"base"}, nil)
		firstParent, secondParent, alias := makeParentAlias(t, fixture)

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
		moveParentAlias(t, alias, secondParent)
		destination := filepath.Join(fixture.repository, "modules", "app", "config")
		if err := os.Symlink(destination, filepath.Join(secondParent, "config")); err != nil {
			t.Fatalf("os.Symlink(second target) error = %v", err)
		}
		writeModuleManifest(t, fixture, "app", "")

		code, _, stderr = fixture.run("apply")
		if code != exitOK || !strings.Contains(stderr, "warning") {
			t.Fatalf("apply stale parent drift = (%d, %q), want warning success", code, stderr)
		}
		assertCLILink(t, filepath.Join(firstParent, "config"), destination)
		assertCLILink(t, filepath.Join(secondParent, "config"), destination)
		if modules := loadTestState(t, fixture).Modules; len(modules) != 0 {
			t.Fatalf("state modules = %#v, want stale ownership forgotten", modules)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
	})
}

func TestAC12_TargetConflictsFailBeforeMutationThroughCLI(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		setup    func(*testing.T, *cliTestEnv)
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
			setup: func(t *testing.T, fixture *cliTestEnv) {
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
			setup: func(t *testing.T, fixture *cliTestEnv) {
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
			setup: func(t *testing.T, fixture *cliTestEnv) {
				root := filepath.Join(fixture.repository, "modules", "app")
				if err := os.Mkdir(filepath.Join(root, "directory"), 0o700); err != nil {
					t.Fatalf("os.Mkdir(directory source) error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = ["app"]`)
			fixture.writeModule(t, "app", test.manifest, map[string]string{
				"first":  "first",
				"second": "second",
			})
			fixture.writeMachine(t, []string{"base"}, nil)
			if test.setup != nil {
				test.setup(t, fixture)
			}
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.run("apply")
			want := test.want
			if want == "" {
				want = "conflict"
			}
			if code != exitError || stdout != "" || !strings.Contains(stderr, want) {
				t.Fatalf("apply = (%d, %q, %q), want preflight %q failure", code, stdout, stderr, want)
			}
			assertSnapshotUnchanged(t, before)
			assertCLIMissing(t, fixture.state)
			assertCLIMissing(t, fixture.lock)
		})
	}
}

func TestAC12_InitRejectsControlPathAncestorsBeforeMutationThroughCLI(t *testing.T) {
	tests := []struct {
		name         string
		target       string
		existingLink bool
		repository   func(*testing.T, *cliTestEnv) string
	}{
		{name: "missing machine-config ancestor", target: "~/.config"},
		{name: "missing state-and-lock ancestor", target: "~/.local"},
		{
			name:         "existing managed machine-config ancestor",
			target:       "~/.config",
			existingLink: true,
		},
		{
			name:   "repository inside target",
			target: "~/managed",
			repository: func(_ *testing.T, fixture *cliTestEnv) string {
				return filepath.Join(fixture.home, "managed", "repository")
			},
		},
		{
			name:   "repository inside resolved target",
			target: "~/alias/managed",
			repository: func(t *testing.T, fixture *cliTestEnv) string {
				actual := filepath.Join(fixture.root, "actual")
				if err := os.Mkdir(actual, 0o700); err != nil {
					t.Fatalf("os.Mkdir(actual) error = %v", err)
				}
				if err := os.Symlink(actual, filepath.Join(fixture.home, "alias")); err != nil {
					t.Fatalf("os.Symlink(alias) error = %v", err)
				}
				return filepath.Join(actual, "managed", "repository")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = ["app"]`)
			if test.repository != nil {
				fixture.repository = test.repository(t, fixture)
				writeCLIFile(
					t,
					filepath.Join(fixture.repository, "dot.toml"),
					"version = 1\n[profiles]\nbase = [\"app\"]\n",
				)
			}
			fixture.writeModule(t, "app", `
[[links]]
id = "root"
source = "root"
target = "`+test.target+`"
`, nil)
			source := filepath.Join(fixture.repository, "modules", "app", "root")
			if err := os.Mkdir(source, 0o700); err != nil {
				t.Fatalf("os.Mkdir(source) error = %v", err)
			}
			if test.existingLink {
				target := filepath.Join(
					fixture.home,
					filepath.FromSlash(strings.TrimPrefix(test.target, "~/")),
				)
				if err := os.Symlink(source, target); err != nil {
					t.Fatalf("os.Symlink(existing target) error = %v", err)
				}
			}
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.run(
				"init",
				fixture.repository,
				"--profile",
				"base",
			)

			if code != exitError ||
				stdout != "" ||
				!strings.Contains(stderr, "control path") {
				t.Fatalf(
					"init = (%d, %q, %q), want preflight control-boundary failure",
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

func TestAC12_StaleStateTargetContainingControlPathIsReadOnlyThroughCLI(t *testing.T) {
	for _, kind := range []state.Kind{state.KindLink, state.KindLocal} {
		t.Run(string(kind), func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = []`)
			fixture.writeMachine(t, []string{"base"}, nil)
			target := filepath.Join(fixture.home, ".local")
			record := state.Placement{
				Kind:   kind,
				Target: target,
			}
			if kind == state.KindLink {
				record.ResolvedTarget = target
				record.LinkDestination = filepath.Join(fixture.repository, "removed")
			}
			fixture.writeState(t, state.Snapshot{
				Home: fixture.home,
				Modules: map[string]state.Module{
					"old": {Placements: map[string]state.Placement{
						"stale": record,
					}},
				},
			})
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.run("apply")

			if code != exitError ||
				stdout != "" ||
				!strings.Contains(stderr, "control path") {
				t.Fatalf(
					"apply = (%d, %q, %q), want preflight control-boundary failure",
					code,
					stdout,
					stderr,
				)
			}
			assertSnapshotUnchanged(t, before)
			assertCLIMissing(t, fixture.lock)
		})
	}
}

func TestAC12_StatusReportsPathConflictThroughCLI(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["first", "second", "pending"]`)
	fixture.writeModule(t, "first", `
[[links]]
id = "config"
source = "config"
target = "~/.same"
`, map[string]string{"config": "first"})
	fixture.writeModule(t, "second", `
[[links]]
id = "config"
source = "config"
target = "~/.same"
`, map[string]string{"config": "second"})
	fixture.writeModule(t, "pending", `
[[links]]
id = "config"
source = "config"
target = "~/.pending"
`, map[string]string{"config": "pending"})
	fixture.writeMachine(t, []string{"base"}, nil)
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("status")
	if code != exitOK ||
		!strings.Contains(stdout, "first  conflict") ||
		!strings.Contains(stdout, "second  conflict") ||
		!strings.Contains(stdout, "pending  pending") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"status = (%d, %q, %q), want conflict plus independent pending status",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
}

func TestAC13_InterruptedFactsConvergeAndRepeatThroughCLI(t *testing.T) {
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

func TestAC14_InvalidStateRejectsMutationThroughCLI(t *testing.T) {
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
			fixture := newCLITestEnv(t, `base = ["app"]`)
			fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "config"})
			fixture.writeMachine(t, []string{"base"}, nil)
			writeCLIFile(t, fixture.state, test.document)
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.run("apply")
			if code != exitError || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("apply = (%d, %q, %q), want %q failure", code, stdout, stderr, test.want)
			}
			assertSnapshotUnchanged(t, before)
			assertCLIMissing(t, fixture.lock)
			assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
		})
	}
}

func TestAC15_LockAndReadOnlyCommandsThroughCLI(t *testing.T) {
	t.Run("second process fails on held mutation lock", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
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

		code, stdout, stderr := fixture.runProcess("apply")
		if code != exitError ||
			stdout != "" ||
			!strings.Contains(stderr, "another dot process") {
			t.Fatalf("locked apply = (%d, %q, %q), want lock failure", code, stdout, stderr)
		}
	})

	t.Run("status and dry-run are strictly read-only", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
		fixture.writeMachine(t, []string{"base"}, nil)
		before := snapshotTree(t, fixture.root)

		code, _, stderr := fixture.run("status")
		if code != exitOK || stderr == "" {
			t.Fatalf("status = (%d, %q)", code, stderr)
		}
		code, stdout, stderr := fixture.run("apply", "extra", "--dry-run")
		if code != exitOK ||
			!strings.Contains(stdout, "create-link") ||
			stderr == "" {
			t.Fatalf("dry-run = (%d, %q, %q)", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
		assertCLIMissing(t, fixture.lock)
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want unchanged", extras)
		}
	})
}

func TestAC16_DeletedProfileFailsAndDeletedExtraCleansThroughCLI(t *testing.T) {
	t.Run("active profile references deleted module", func(t *testing.T) {
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
	})

	t.Run("deleted extra and state are removable", func(t *testing.T) {
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
	})
}

func TestAC17_ScopedApplyAndRemoveIgnoreBrokenOutOfScopeModule(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
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

	code, _, stderr := fixture.run("apply", "apply-good")
	if code != exitOK {
		t.Fatalf("scoped apply with broken out-of-scope module = (%d, %q)", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".apply-good"),
		filepath.Join(fixture.repository, "modules", "apply-good", "config"),
	)
	assertApplyNoMutation(t, fixture, fixture.run, "apply-good")

	code, _, stderr = fixture.run("apply", "remove-good")
	if code != exitOK {
		t.Fatalf("scoped apply remove-good = (%d, %q)", code, stderr)
	}
	assertApplyNoMutation(t, fixture, fixture.run, "remove-good")

	code, _, stderr = fixture.run("remove", "remove-good")
	if code != exitOK {
		t.Fatalf("remove with broken out-of-scope module = (%d, %q)", code, stderr)
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".remove-good"))
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 ||
		extras[0] != "apply-good" {
		t.Fatalf("extra_modules = %v, want [apply-good]", extras)
	}
	assertApplyNoMutation(t, fixture, fixture.run)

	before := snapshotTree(t, fixture.root)
	code, stdout, stderr := fixture.run("apply", "broken")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, `module "broken"`) {
		t.Fatalf("explicit broken apply = (%d, %q, %q), want strict failure", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
}

func TestAC18_InvalidSourcesAndExamplesRejectMutationThroughCLI(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		files    map[string]string
		setup    func(*testing.T, *cliTestEnv)
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
			setup: func(t *testing.T, fixture *cliTestEnv) {
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
			setup: func(t *testing.T, fixture *cliTestEnv) {
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
			setup: func(t *testing.T, fixture *cliTestEnv) {
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
			setup: func(t *testing.T, fixture *cliTestEnv) {
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
			setup: func(t *testing.T, fixture *cliTestEnv) {
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
			fixture := newCLITestEnv(t, `base = ["app"]`)
			fixture.writeModule(t, "app", test.manifest, test.files)
			fixture.writeMachine(t, []string{"base"}, nil)
			if test.setup != nil {
				test.setup(t, fixture)
			}
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.run("apply")
			if code != exitError ||
				stdout != "" ||
				(!strings.Contains(stderr, "source") &&
					!strings.Contains(stderr, "example")) {
				t.Fatalf("apply = (%d, %q, %q), want strict source/example failure", code, stdout, stderr)
			}
			assertSnapshotUnchanged(t, before)
			assertCLIMissing(t, fixture.state)
			assertCLIMissing(t, fixture.lock)
			assertCLIMissing(t, filepath.Join(fixture.home, ".invalid"))
		})
	}
}

func TestAC19_UnknownPlatformAndInvalidOSThroughCLI(t *testing.T) {
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
		return config.Platform{OS: "linux", Distro: "gentoo", Arch: "riscv64"}
	}

	code, _, stderr := fixture.runInjected("apply")
	if code != exitOK || stderr == "" {
		t.Fatalf("unknown-platform apply = (%d, %q)", code, stderr)
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
		!strings.Contains(stdout, "portable  converged") ||
		!strings.Contains(stdout, "gated  not-applicable") {
		t.Fatalf("unknown-platform status = (%d, %q, %q)", code, stdout, stderr)
	}
	assertApplyNoMutation(t, fixture, fixture.runInjected)

	before := snapshotTree(t, fixture.root)
	code, stdout, stderr = fixture.runInjected("apply", "invalid-os")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "unsupported os token") {
		t.Fatalf("invalid-os apply = (%d, %q, %q), want strict config failure", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged empty selection", extras)
	}
}

func makeParentAlias(
	t *testing.T,
	fixture *cliTestEnv,
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

func moveParentAlias(t *testing.T, alias, destination string) {
	t.Helper()
	if err := os.Remove(alias); err != nil {
		t.Fatalf("os.Remove(alias) error = %v", err)
	}
	if err := os.Symlink(destination, alias); err != nil {
		t.Fatalf("os.Symlink(moved alias) error = %v", err)
	}
}
