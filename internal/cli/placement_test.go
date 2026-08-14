package cli

import (
	"errors"
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
		if code != exitOK {
			t.Fatalf("apply with drifted stale link = (%d, %q), want success", code, stderr)
		}
		assertCLILink(t, oldTarget, userDestination)
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".app-new"),
			filepath.Join(fixture.repository, "modules", "app", "new"),
		)
		loaded := loadTestState(t, fixture)
		if records := loaded.Links; len(records) != 1 {
			t.Fatalf("state records = %#v, want only new placement", records)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
	})
}

func TestApplyMigratesDirectoryLinkToLeafPlacementsInTwoStages(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "tree"
source = "tree"
target = "~/.app"
`, map[string]string{
		"tree/shared":   "shared",
		"local.example": "local",
	})
	fixture.writeMachine(t, []string{"base"}, nil)

	code, _, stderr := fixture.run("apply")
	if code != exitOK {
		t.Fatalf("initial directory-link apply = (%d, %q)", code, stderr)
	}
	parent := filepath.Join(fixture.home, ".app")
	assertCLILink(
		t,
		parent,
		filepath.Join(fixture.repository, "modules", "app", "tree"),
	)
	assertApplyNoMutation(t, fixture, fixture.run)

	writeModuleManifest(t, fixture, "app", "")
	code, _, stderr = fixture.run("apply")
	if code != exitOK {
		t.Fatalf("phase-one apply = (%d, %q)", code, stderr)
	}
	assertCLIMissing(t, parent)
	if records := loadTestState(t, fixture).Links; len(records) != 0 {
		t.Fatalf("state after phase one = %#v, want no records", records)
	}
	assertApplyNoMutation(t, fixture, fixture.run)

	writeModuleManifest(t, fixture, "app", `
[[links]]
id = "shared"
source = "tree/shared"
target = "~/.app/shared"

[[locals]]
id = "local"
example = "local.example"
target = "~/.app/local"
`)
	code, _, stderr = fixture.run("apply")
	if code != exitOK {
		t.Fatalf("phase-two apply = (%d, %q)", code, stderr)
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		t.Fatalf("parent after phase two = (%v, %v), want real directory", info, err)
	}
	assertCLILink(
		t,
		filepath.Join(parent, "shared"),
		filepath.Join(fixture.repository, "modules", "app", "tree", "shared"),
	)
	local, err := os.ReadFile(filepath.Join(parent, "local"))
	if err != nil || string(local) != "local" {
		t.Fatalf("local after phase two = (%q, %v), want initialized content", local, err)
	}
	assertApplyNoMutation(t, fixture, fixture.run)
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
			if links := loadTestState(t, fixture).Links; len(links) != 0 {
				t.Fatalf("local apply wrote ownership state: %#v", links)
			}
			encodedState, err := os.ReadFile(fixture.state)
			if err != nil {
				t.Fatalf("os.ReadFile(state) error = %v", err)
			}
			if !strings.Contains(string(encodedState), `"version": 5`) ||
				!strings.Contains(string(encodedState), `"links": []`) {
				t.Fatalf("local-only state = %q, want empty state v5", encodedState)
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

			writeModuleManifest(t, fixture, "app", "")
			beforeTarget = snapshotPaths(t, target)
			code, stdout, stderr = fixture.run("apply")
			if code != exitOK || stderr != "" {
				t.Fatalf("apply after local exits desired = (%d, %q, %q)", code, stdout, stderr)
			}
			assertCLINoMutationResult(t, stdout)
			assertSnapshotUnchanged(t, beforeTarget)
			if links := loadTestState(t, fixture).Links; len(links) != 0 {
				t.Fatalf("state after local exits desired = %#v, want no ownership", links)
			}
			assertApplyNoMutation(t, fixture, fixture.run)
		})
	}
}

func TestLocalWithNonDirectoryAncestorIsSkip(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[locals]]
id = "local"
example = "local.example"
target = "~/.blocked/local"
`, map[string]string{"local.example": "example"})
	fixture.writeMachine(t, []string{"base"}, nil)
	blocked := filepath.Join(fixture.home, ".blocked")
	writeCLIFile(t, blocked, "not a directory")
	before := snapshotTree(t, fixture.root)

	code, stdout, _ := fixture.runInjected("status")
	if code != exitOK ||
		!strings.Contains(stdout, "skip module=app placement=local") ||
		!strings.Contains(stdout, "ancestor is not a directory") ||
		strings.Contains(stdout, "file module=app placement=local") {
		t.Fatalf("status unreachable local = (%d, %q), want skip", code, stdout)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, _ = fixture.runInjected("apply", "--dry-run")
	if code != exitError ||
		!strings.Contains(stdout, "skip module=app placement=local") ||
		strings.Contains(stdout, "file module=app placement=local") {
		t.Fatalf("dry-run unreachable local = (%d, %q), want skip", code, stdout)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr := fixture.runInjected("apply")
	if code != exitError || !strings.Contains(stderr, "state is missing") ||
		!strings.Contains(stdout, "skip module=app placement=local") ||
		strings.Contains(stdout, "file module=app placement=local") {
		t.Fatalf("apply unreachable local = (%d, %q, %q), want zero-write skip", code, stdout, stderr)
	}
	assertOnlyLockBookkeepingChanged(t, before, fixture)
	if data, err := os.ReadFile(blocked); err != nil || string(data) != "not a directory" {
		t.Fatalf("blocking ancestor = (%q, %v), want unchanged", data, err)
	}
	if _, err := os.Lstat(filepath.Join(blocked, "local")); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("blocked local error = %v, want ENOTDIR", err)
	}
}

func TestApplyAdoptsMatchingLinkAndRequiresExplicitKindMigration(t *testing.T) {
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
		if code != exitOK || !strings.Contains(stdout, "record") {
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
		if code != exitError ||
			!strings.Contains(stdout, "skip") ||
			!strings.Contains(stdout, "actual symlink is not explained by desired or state") ||
			strings.Contains(stderr, "error:") {
			t.Fatalf("apply after drift = (%d, %q, %q), want conflict", code, stdout, stderr)
		}
		assertOnlyLockBookkeepingChanged(t, before, fixture)
	})

	t.Run("local to link requires two-stage cleanup and user handling", func(t *testing.T) {
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
		if code != exitError ||
			!strings.Contains(stdout, "skip") ||
			!strings.Contains(stdout, "actual target is regular file") ||
			strings.Contains(stderr, "error:") {
			t.Fatalf("apply after kind change = (%d, %q, %q), want conflict", code, stdout, stderr)
		}
		assertOnlyLockBookkeepingChanged(t, before, fixture)

		writeModuleManifest(t, fixture, "app", "")
		code, _, stderr = fixture.run("apply")
		if code != exitOK {
			t.Fatalf("phase-one local cleanup = (%d, %q)", code, stderr)
		}
		target := filepath.Join(fixture.home, ".shared")
		data, err := os.ReadFile(target)
		if err != nil || string(data) != "local" {
			t.Fatalf("local after phase one = (%q, %v), want preserved", data, err)
		}
		if records := loadTestState(t, fixture).Links; len(records) != 0 {
			t.Fatalf("state after phase one = %#v, want no records", records)
		}
		assertApplyNoMutation(t, fixture, fixture.run)

		writeModuleManifest(t, fixture, "app", `
[[links]]
id = "shared"
source = "config"
target = "~/.shared"
`)
		before = snapshotTree(t, fixture.root)
		code, stdout, stderr = fixture.run("apply")
		if code != exitError ||
			!strings.Contains(stdout, "skip") ||
			!strings.Contains(stdout, "actual target is regular file") ||
			strings.Contains(stderr, "error:") {
			t.Fatalf(
				"phase-two apply with retained local = (%d, %q, %q), want conflict",
				code,
				stdout,
				stderr,
			)
		}
		assertOnlyLockBookkeepingChanged(t, before, fixture)

		preserved := filepath.Join(fixture.root, "preserved-local")
		if err := os.Rename(target, preserved); err != nil {
			t.Fatalf("os.Rename(local for user handling) error = %v", err)
		}
		code, _, stderr = fixture.run("apply")
		if code != exitOK {
			t.Fatalf("phase-two link apply = (%d, %q)", code, stderr)
		}
		assertCLILink(
			t,
			target,
			filepath.Join(fixture.repository, "modules", "app", "config"),
		)
		data, err = os.ReadFile(preserved)
		if err != nil || string(data) != "local" {
			t.Fatalf("preserved local = (%q, %v), want original content", data, err)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("link to local creates only after two-stage cleanup", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "shared"
source = "config"
target = "~/.shared"
`, map[string]string{
			"config":        "config",
			"local.example": "local",
		})
		fixture.writeMachine(t, []string{"base"}, nil)

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("initial link apply = (%d, %q)", code, stderr)
		}
		assertApplyNoMutation(t, fixture, fixture.run)

		writeModuleManifest(t, fixture, "app", "")
		code, _, stderr = fixture.run("apply")
		if code != exitOK {
			t.Fatalf("phase-one link cleanup = (%d, %q)", code, stderr)
		}
		target := filepath.Join(fixture.home, ".shared")
		assertCLIMissing(t, target)
		if records := loadTestState(t, fixture).Links; len(records) != 0 {
			t.Fatalf("state after phase one = %#v, want no records", records)
		}
		assertApplyNoMutation(t, fixture, fixture.run)

		writeModuleManifest(t, fixture, "app", `
[[locals]]
id = "shared"
example = "local.example"
target = "~/.shared"
`)
		code, _, stderr = fixture.run("apply")
		if code != exitOK {
			t.Fatalf("phase-two local apply = (%d, %q)", code, stderr)
		}
		data, err := os.ReadFile(target)
		if err != nil || string(data) != "local" {
			t.Fatalf("local after phase two = (%q, %v), want initialized", data, err)
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
			assertOnlyLockBookkeepingChanged(t, before, fixture)
			assertCLIMissing(t, fixture.state)
			assertCLIMissing(t, filepath.Join(fixture.home, ".invalid"))
		})
	}
}
