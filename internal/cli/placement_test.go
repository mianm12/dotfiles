package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestApplyAddsPlacementsAndPrunesOnlyOwnedUnchangedLinks(t *testing.T) {
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

func TestApplyCreatesNewTargetBeforePruningOldTarget(t *testing.T) {
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

func TestApplyCreatesLocalOnlyWhenAbsent(t *testing.T) {
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

func TestApplyAdoptsMatchingLinkAndRejectsDriftOrKindChange(t *testing.T) {
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

func TestApplyRejectsParentSymlinkDrift(t *testing.T) {
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

func TestApplyRejectsInvalidSourcesAndExamplesWithoutMutation(t *testing.T) {
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
