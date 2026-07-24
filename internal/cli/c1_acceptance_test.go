package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/state"
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
			fixture := newCLIFixture(t, `base = ["app"]`)
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
			assertC1ApplyNoMutation(t, fixture, fixture.runInjected)
		})
	}
}

func TestAcceptance03_ProfileNotApplicableSkipsAndRepeatsThroughCLI(t *testing.T) {
	fixture := newCLIFixture(t, `base = ["portable", "gated"]`)
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
	assertC1ApplyNoMutation(t, fixture, fixture.runInjected)
}

func TestAcceptance04_SourceContentChangeIsNoopThroughCLI(t *testing.T) {
	fixture := newCLIFixture(t, `base = ["app"]`)
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
	assertC1ApplyNoMutation(t, fixture, fixture.run)

	source := filepath.Join(fixture.repository, "modules", "app", "config")
	if err := os.WriteFile(source, []byte("after"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(source) error = %v", err)
	}
	target := filepath.Join(fixture.home, ".app")
	before := snapshotCLIPaths(t, fixture.config, fixture.state, fixture.lock, target)

	code, stdout, stderr := fixture.run("apply")
	if code != exitOK || stderr != "" {
		t.Fatalf("apply after source content change = (%d, %q, %q)", code, stdout, stderr)
	}
	assertCLINoMutationResult(t, stdout)
	assertCLIPathsUnchanged(t, before)
	assertCLILink(t, target, source)
}

func TestAcceptance05_PlacementChangesPruneOnlySafeLinksThroughCLI(t *testing.T) {
	t.Run("add and safe prune", func(t *testing.T) {
		fixture := newCLIFixture(t, `base = ["app"]`)
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
		assertC1ApplyNoMutation(t, fixture, fixture.run)

		writeC1ModuleManifest(t, fixture, "app", `
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
		assertC1ApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("drifted stale link warns and forgets", func(t *testing.T) {
		fixture := newCLIFixture(t, `base = ["app"]`)
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
		assertC1ApplyNoMutation(t, fixture, fixture.run)

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
		loaded := loadC1State(t, fixture)
		if placements := loaded.Modules["app"].Placements; len(placements) != 1 {
			t.Fatalf("state placements = %#v, want only new placement", placements)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.run)
	})
}

func TestAcceptance06_TargetChangeConvergesAndRepeatsThroughCLI(t *testing.T) {
	fixture := newCLIFixture(t, `base = ["app"]`)
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
	assertC1ApplyNoMutation(t, fixture, fixture.run)

	writeC1ModuleManifest(t, fixture, "app", `
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
	assertC1ApplyNoMutation(t, fixture, fixture.run)
}

func TestAcceptance09_LocalCreateKeepAndExampleUpdateThroughCLI(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *cliFixture, string)
	}{
		{name: "absent"},
		{
			name: "regular file",
			setup: func(t *testing.T, _ *cliFixture, target string) {
				writeCLIFile(t, target, "user")
			},
		},
		{
			name: "directory",
			setup: func(t *testing.T, _ *cliFixture, target string) {
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatalf("os.Mkdir(target) error = %v", err)
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, fixture *cliFixture, target string) {
				source := filepath.Join(fixture.root, "user-local")
				writeCLIFile(t, source, "user")
				if err := os.Symlink(source, target); err != nil {
					t.Fatalf("os.Symlink(target) error = %v", err)
				}
			},
		},
		{
			name: "dangling symlink",
			setup: func(t *testing.T, fixture *cliFixture, target string) {
				if err := os.Symlink(filepath.Join(fixture.root, "missing"), target); err != nil {
					t.Fatalf("os.Symlink(dangling target) error = %v", err)
				}
			},
		},
		{
			name: "special file",
			setup: func(t *testing.T, _ *cliFixture, target string) {
				if err := syscall.Mkfifo(target, 0o600); err != nil {
					t.Fatalf("syscall.Mkfifo(target) error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLIFixture(t, `base = ["app"]`)
			fixture.writeModule(t, "app", `
[[locals]]
id = "local"
example = "local.example"
target = "~/.app.local"
`, map[string]string{"local.example": "example"})
			fixture.writeMachine(t, []string{"base"}, nil)
			target := filepath.Join(fixture.home, ".app.local")
			var beforeTarget []cliPathSnapshot
			if test.setup != nil {
				test.setup(t, fixture, target)
				beforeTarget = snapshotCLIExactPaths(t, target)
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
				assertCLIPathsUnchanged(t, beforeTarget)
			}
			assertC1ApplyNoMutation(t, fixture, fixture.run)

			example := filepath.Join(
				fixture.repository,
				"modules",
				"app",
				"local.example",
			)
			if err := os.WriteFile(example, []byte("updated"), 0o600); err != nil {
				t.Fatalf("os.WriteFile(example) error = %v", err)
			}
			beforeTarget = snapshotCLIExactPaths(t, target)
			code, stdout, stderr := fixture.run("apply")
			if code != exitOK || stderr != "" {
				t.Fatalf("apply after example update = (%d, %q, %q)", code, stdout, stderr)
			}
			assertCLINoMutationResult(t, stdout)
			assertCLIPathsUnchanged(t, beforeTarget)
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
		fixture := newCLIFixture(t, `base = ["app"]`)
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
		beforeTarget := snapshotCLIExactPaths(t, target)

		code, stdout, stderr := fixture.run("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr == "" {
			t.Fatalf("adopt apply = (%d, %q, %q)", code, stdout, stderr)
		}
		assertCLIPathsUnchanged(t, beforeTarget)
		assertC1ApplyNoMutation(t, fixture, fixture.run)

		if err := os.Remove(target); err != nil {
			t.Fatalf("os.Remove(target) error = %v", err)
		}
		userDestination := filepath.Join(fixture.root, "user-config")
		writeCLIFile(t, userDestination, "user")
		if err := os.Symlink(userDestination, target); err != nil {
			t.Fatalf("os.Symlink(user destination) error = %v", err)
		}
		before := snapshotCLITree(t, fixture.root)
		code, stdout, stderr = fixture.run("apply")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
			t.Fatalf("apply after drift = (%d, %q, %q), want conflict", code, stdout, stderr)
		}
		assertCLITreeUnchanged(t, before)
	})

	t.Run("kind change conflicts", func(t *testing.T) {
		fixture := newCLIFixture(t, `base = ["app"]`)
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
		assertC1ApplyNoMutation(t, fixture, fixture.run)
		writeC1ModuleManifest(t, fixture, "app", `
[[links]]
id = "shared"
source = "config"
target = "~/.shared"
`)
		before := snapshotCLITree(t, fixture.root)
		code, stdout, stderr := fixture.run("apply")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
			t.Fatalf("apply after kind change = (%d, %q, %q), want conflict", code, stdout, stderr)
		}
		assertCLITreeUnchanged(t, before)
	})
}

func TestAcceptance11_ParentSymlinkDriftThroughCLI(t *testing.T) {
	t.Run("active update is rejected", func(t *testing.T) {
		fixture := newCLIFixture(t, `base = ["app"]`)
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

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.run)
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
		before := snapshotCLITree(t, fixture.root)

		code, stdout, stderr := fixture.run("apply")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "plan conflict") {
			t.Fatalf("apply after parent drift = (%d, %q, %q), want conflict", code, stdout, stderr)
		}
		assertCLITreeUnchanged(t, before)
		assertCLILink(t, filepath.Join(firstParent, "config"), oldDestination)
		assertCLILink(t, filepath.Join(secondParent, "config"), oldDestination)
	})

	t.Run("stale prune warns and forgets", func(t *testing.T) {
		fixture := newCLIFixture(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/alias/config"
`, map[string]string{"config": "config"})
		fixture.writeMachine(t, []string{"base"}, nil)
		firstParent, secondParent, alias := makeC1ParentAlias(t, fixture)

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.run)
		moveC1ParentAlias(t, alias, secondParent)
		destination := filepath.Join(fixture.repository, "modules", "app", "config")
		if err := os.Symlink(destination, filepath.Join(secondParent, "config")); err != nil {
			t.Fatalf("os.Symlink(second target) error = %v", err)
		}
		writeC1ModuleManifest(t, fixture, "app", "")

		code, _, stderr = fixture.run("apply")
		if code != exitOK || !strings.Contains(stderr, "warning") {
			t.Fatalf("apply stale parent drift = (%d, %q), want warning success", code, stderr)
		}
		assertCLILink(t, filepath.Join(firstParent, "config"), destination)
		assertCLILink(t, filepath.Join(secondParent, "config"), destination)
		if modules := loadC1State(t, fixture).Modules; len(modules) != 0 {
			t.Fatalf("state modules = %#v, want stale ownership forgotten", modules)
		}
		assertC1ApplyNoMutation(t, fixture, fixture.run)
	})
}

func TestAcceptance12_TargetConflictsFailBeforeMutationThroughCLI(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		setup    func(*testing.T, *cliFixture)
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
			setup: func(t *testing.T, fixture *cliFixture) {
				if err := os.Mkdir(filepath.Join(fixture.home, "real"), 0o700); err != nil {
					t.Fatalf("os.Mkdir(real) error = %v", err)
				}
				if err := os.Symlink("real", filepath.Join(fixture.home, "alias")); err != nil {
					t.Fatalf("os.Symlink(alias) error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLIFixture(t, `base = ["app"]`)
			fixture.writeModule(t, "app", test.manifest, map[string]string{
				"first":  "first",
				"second": "second",
			})
			fixture.writeMachine(t, []string{"base"}, nil)
			if test.setup != nil {
				test.setup(t, fixture)
			}
			before := snapshotCLITree(t, fixture.root)

			code, stdout, stderr := fixture.run("apply")
			if code != exitError || stdout != "" || !strings.Contains(stderr, "conflict") {
				t.Fatalf("apply = (%d, %q, %q), want preflight target conflict", code, stdout, stderr)
			}
			assertCLITreeUnchanged(t, before)
			assertCLIMissing(t, fixture.state)
			assertCLIMissing(t, fixture.lock)
		})
	}
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
			fixture := newCLIFixture(t, `base = ["app"]`)
			fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "config"})
			fixture.writeMachine(t, []string{"base"}, nil)
			writeCLIFile(t, fixture.state, test.document)
			before := snapshotCLITree(t, fixture.root)

			code, stdout, stderr := fixture.run("apply")
			if code != exitError || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("apply = (%d, %q, %q), want %q failure", code, stdout, stderr, test.want)
			}
			assertCLITreeUnchanged(t, before)
			assertCLIMissing(t, fixture.lock)
			assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
		})
	}
}

func TestAcceptance17_ScopedRemoveIgnoresBrokenOutOfScopeModule(t *testing.T) {
	fixture := newCLIFixture(t, `base = []`)
	fixture.writeModule(t, "good", `
[[links]]
id = "config"
source = "config"
target = "~/.good"
`, map[string]string{"config": "good"})
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "modules", "broken", "module.toml"),
		"unknown = true\n",
	)
	fixture.writeMachine(t, []string{"base"}, []string{"good"})

	code, _, stderr := fixture.run("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	assertC1ApplyNoMutation(t, fixture, fixture.run)

	code, _, stderr = fixture.run("remove", "good")
	if code != exitOK {
		t.Fatalf("remove good with broken out-of-scope module = (%d, %q)", code, stderr)
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".good"))
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want empty", extras)
	}
	assertC1ApplyNoMutation(t, fixture, fixture.run)
}

func TestAcceptance18_InvalidSourcesAndExamplesRejectMutationThroughCLI(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		files    map[string]string
		setup    func(*testing.T, *cliFixture)
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
			setup: func(t *testing.T, fixture *cliFixture) {
				root := filepath.Join(fixture.repository, "modules", "app")
				if err := os.Symlink("real", filepath.Join(root, "alias")); err != nil {
					t.Fatalf("os.Symlink(source alias) error = %v", err)
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
			setup: func(t *testing.T, fixture *cliFixture) {
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
			setup: func(t *testing.T, fixture *cliFixture) {
				root := filepath.Join(fixture.repository, "modules", "app")
				if err := os.Symlink(
					"real.example",
					filepath.Join(root, "alias.example"),
				); err != nil {
					t.Fatalf("os.Symlink(example alias) error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLIFixture(t, `base = ["app"]`)
			fixture.writeModule(t, "app", test.manifest, test.files)
			fixture.writeMachine(t, []string{"base"}, nil)
			if test.setup != nil {
				test.setup(t, fixture)
			}
			before := snapshotCLITree(t, fixture.root)

			code, stdout, stderr := fixture.run("apply")
			if code != exitError ||
				stdout != "" ||
				(!strings.Contains(stderr, "source") &&
					!strings.Contains(stderr, "example")) {
				t.Fatalf("apply = (%d, %q, %q), want strict source/example failure", code, stdout, stderr)
			}
			assertCLITreeUnchanged(t, before)
			assertCLIMissing(t, fixture.state)
			assertCLIMissing(t, fixture.lock)
			assertCLIMissing(t, filepath.Join(fixture.home, ".invalid"))
		})
	}
}

func TestAcceptance19_UnknownPlatformAndInvalidOSThroughCLI(t *testing.T) {
	fixture := newCLIFixture(t, `base = ["portable", "gated"]`)
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
	assertC1ApplyNoMutation(t, fixture, fixture.runInjected)

	before := snapshotCLITree(t, fixture.root)
	code, stdout, stderr = fixture.runInjected("apply", "invalid-os")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "unsupported os token") {
		t.Fatalf("invalid-os apply = (%d, %q, %q), want strict config failure", code, stdout, stderr)
	}
	assertCLITreeUnchanged(t, before)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged empty selection", extras)
	}
}

func makeC1ParentAlias(
	t *testing.T,
	fixture *cliFixture,
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

func assertC1ApplyNoMutation(
	t *testing.T,
	fixture *cliFixture,
	run func(...string) (int, string, string),
	module ...string,
) {
	t.Helper()
	before := snapshotCLITree(t, fixture.root)
	args := append([]string{"apply"}, module...)
	code, stdout, stderr := run(args...)
	if code != exitOK || stderr != "" {
		t.Fatalf("repeated apply = (%d, %q, %q)", code, stdout, stderr)
	}
	assertCLINoMutationResult(t, stdout)
	assertCLITreeUnchanged(t, before)
}

func writeC1ModuleManifest(t *testing.T, fixture *cliFixture, id, manifest string) {
	t.Helper()
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "modules", id, "module.toml"),
		strings.TrimSpace(manifest)+"\n",
	)
}

func loadC1State(t *testing.T, fixture *cliFixture) state.Snapshot {
	t.Helper()
	loaded, err := state.Load(fixture.state, fixture.home)
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}
	return loaded.Snapshot
}
