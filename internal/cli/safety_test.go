package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/lock"
)

func TestApplyRejectsTargetConflictsBeforeMutation(t *testing.T) {
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
			name: "file link contains link target",
			manifest: `
[[links]]
id = "parent"
source = "first"
target = "~/tree"

[[links]]
id = "child"
source = "second"
target = "~/tree/child"
`,
		},
		{
			name: "link contains local target",
			manifest: `
[[links]]
id = "parent"
source = "first"
target = "~/tree"

[[locals]]
id = "child"
example = "second"
target = "~/tree/child"
`,
		},
		{
			name: "local contains link target",
			manifest: `
[[locals]]
id = "parent"
example = "first"
target = "~/tree"

[[links]]
id = "child"
source = "second"
target = "~/tree/child"
`,
		},
		{
			name: "local contains local target",
			manifest: `
[[locals]]
id = "parent"
example = "first"
target = "~/tree"

[[locals]]
id = "child"
example = "second"
target = "~/tree/child"
`,
		},
		{
			name: "resolved alias target contains descendant",
			manifest: `
[[links]]
id = "parent"
source = "first"
target = "~/alias/tree"

[[links]]
id = "child"
source = "second"
target = "~/real/tree/child"
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

func TestInitRejectsControlPathAncestorsBeforeMutation(t *testing.T) {
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

func TestCommandsRejectControlTopologyWithoutMutation(t *testing.T) {
	t.Run("init repository equals config root", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		repository := filepath.Dir(fixture.config)
		writeCLIFile(
			t,
			filepath.Join(repository, "dot.toml"),
			"version = 1\n[profiles]\nbase = []\n",
		)
		if err := os.Chmod(repository, 0o755); err != nil {
			t.Fatalf("os.Chmod(repository) error = %v", err)
		}
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.run(
			"init",
			repository,
			"--profile",
			"base",
		)

		assertCLIControlTopologyFailure(t, code, stdout, stderr, repository)
		assertSnapshotUnchanged(t, before)
		assertCLIMissing(t, fixture.config)
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, fixture.lock)
	})

	tests := []struct {
		name   string
		args   []string
		extras []string
	}{
		{name: "apply", args: []string{"apply"}},
		{name: "apply dry-run", args: []string{"apply", "--dry-run"}},
		{name: "remove before selection publish", args: []string{"remove", "app"}, extras: []string{"app"}},
		{name: "status empty repository", args: []string{"status"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = []`)
			fixture.repository = filepath.Dir(fixture.state)
			writeCLIFile(
				t,
				filepath.Join(fixture.repository, "dot.toml"),
				"version = 1\n[profiles]\nbase = []\n",
			)
			if err := os.Chmod(fixture.repository, 0o755); err != nil {
				t.Fatalf("os.Chmod(repository) error = %v", err)
			}
			fixture.writeMachine(t, []string{"base"}, test.extras)
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.run(test.args...)

			assertCLIControlTopologyFailure(
				t,
				code,
				stdout,
				stderr,
				fixture.repository,
			)
			assertSnapshotUnchanged(t, before)
			assertCLIMissing(t, fixture.state)
			assertCLIMissing(t, fixture.lock)
			if test.name == "remove before selection publish" {
				extras := fixture.loadMachine(t).ExtraModules
				if len(extras) != 1 || extras[0] != "app" {
					t.Fatalf("extra_modules = %v, want unchanged [app]", extras)
				}
			}
		})
	}

	t.Run("apply resolved repository alias", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		stateRoot := filepath.Dir(fixture.state)
		if err := os.MkdirAll(stateRoot, 0o755); err != nil {
			t.Fatalf("os.MkdirAll(state root) error = %v", err)
		}
		repositoryAlias := filepath.Join(fixture.root, "repository-alias")
		if err := os.Symlink(stateRoot, repositoryAlias); err != nil {
			t.Fatalf("os.Symlink(repository alias) error = %v", err)
		}
		fixture.repository = repositoryAlias
		writeCLIFile(
			t,
			filepath.Join(fixture.repository, "dot.toml"),
			"version = 1\n[profiles]\nbase = []\n",
		)
		fixture.writeMachine(t, []string{"base"}, nil)
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.run("apply")

		assertCLIControlTopologyFailure(
			t,
			code,
			stdout,
			stderr,
			repositoryAlias,
		)
		assertSnapshotUnchanged(t, before)
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, fixture.lock)
	})
}

func assertCLIControlTopologyFailure(
	t *testing.T,
	code int,
	stdout, stderr, conflictPath string,
) {
	t.Helper()
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "control paths conflict") ||
		!strings.Contains(stderr, conflictPath) ||
		!strings.Contains(stderr, "run `dot paths`") {
		t.Fatalf(
			"command = (%d, %q, %q), want topology error naming %q and dot paths",
			code,
			stdout,
			stderr,
			conflictPath,
		)
	}
}

func TestStatusAndDryRunSharePlacementControlValidation(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.config/dot/managed"
`, map[string]string{"config": "config"})
	fixture.writeMachine(t, []string{"base"}, nil)
	target := filepath.Join(filepath.Dir(fixture.config), "managed")
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("status")
	if code != exitOK ||
		!strings.Contains(stdout, "app  conflict") ||
		!strings.Contains(stderr, "state is missing") ||
		!strings.Contains(stderr, target) ||
		!strings.Contains(stderr, filepath.Dir(fixture.config)) ||
		!strings.Contains(stderr, "run `dot paths`") {
		t.Fatalf("status = (%d, %q, %q), want read-only conflict", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.run("apply", "--dry-run")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, target) ||
		!strings.Contains(stderr, filepath.Dir(fixture.config)) ||
		!strings.Contains(stderr, "run `dot paths`") {
		t.Fatalf(
			"apply --dry-run = (%d, %q, %q), want path conflict with recovery hint",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
}

func TestApplyForgetsStaleTargetOverlappingControlPath(t *testing.T) {
	for _, kind := range []state.Kind{state.KindLink, state.KindLocal} {
		t.Run(string(kind), func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = []`)
			fixture.repository = filepath.Join(fixture.home, "repository")
			writeCLIFile(
				t,
				filepath.Join(fixture.repository, "dot.toml"),
				"version = 1\n[profiles]\nbase = []\n",
			)
			fixture.writeMachine(t, []string{"base"}, nil)
			target := fixture.repository
			record := state.Placement{
				Kind:   kind,
				Target: target,
			}
			if kind == state.KindLink {
				resolvedTarget, err := filepath.EvalSymlinks(target)
				if err != nil {
					t.Fatalf("filepath.EvalSymlinks(target) error = %v", err)
				}
				record.ResolvedTarget = resolvedTarget
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
			beforeTarget := snapshotTree(t, target)

			code, stdout, stderr := fixture.run("apply")

			if code != exitOK ||
				!strings.Contains(stdout, "forget") ||
				!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
				!strings.Contains(stderr, "overlaps a protected control path") {
				t.Fatalf(
					"apply = (%d, %q, %q), want state-only forget",
					code,
					stdout,
					stderr,
				)
			}
			assertSnapshotUnchanged(t, beforeTarget)
			if modules := loadTestState(t, fixture).Modules; len(modules) != 0 {
				t.Fatalf("state modules after forget = %#v, want empty", modules)
			}

			assertApplyNoMutation(t, fixture, fixture.run)
		})
	}
}

func TestApplyForgetsStaleTargetContainingStateRootWithoutChangingEntry(t *testing.T) {
	for _, kind := range []state.Kind{state.KindLink, state.KindLocal} {
		t.Run(string(kind), func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = []`)
			fixture.writeMachine(t, []string{"base"}, nil)
			target := filepath.Join(fixture.home, ".local")
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatalf("os.MkdirAll(target) error = %v", err)
			}
			record := state.Placement{
				Kind:   kind,
				Target: target,
			}
			if kind == state.KindLink {
				resolvedTarget, err := filepath.EvalSymlinks(target)
				if err != nil {
					t.Fatalf("filepath.EvalSymlinks(target) error = %v", err)
				}
				record.ResolvedTarget = resolvedTarget
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
			beforeTarget := snapshotPaths(t, target)

			code, stdout, stderr := fixture.run("apply")

			if code != exitOK ||
				!strings.Contains(stdout, "forget") ||
				!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
				!strings.Contains(stderr, "overlaps a protected control path") {
				t.Fatalf(
					"apply = (%d, %q, %q), want state-only forget",
					code,
					stdout,
					stderr,
				)
			}
			assertSnapshotUnchanged(t, beforeTarget)
			if modules := loadTestState(t, fixture).Modules; len(modules) != 0 {
				t.Fatalf("state modules after forget = %#v, want empty", modules)
			}

			assertApplyNoMutation(t, fixture, fixture.run)
		})
	}
}

func TestStaleTargetEqualToLockIsReadOnlyUntilStateOnlyForget(t *testing.T) {
	for _, kind := range []state.Kind{state.KindLink, state.KindLocal} {
		t.Run(string(kind), func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = []`)
			fixture.writeMachine(t, []string{"base"}, nil)
			record := state.Placement{
				Kind:   kind,
				Target: fixture.lock,
			}
			if kind == state.KindLink {
				target, err := corepaths.ResolveTarget(
					fixture.home,
					"~/.local/state/dot/lock",
				)
				if err != nil {
					t.Fatalf("ResolveTarget(lock) error = %v", err)
				}
				record.ResolvedTarget = target.Resolved()
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
			assertCLIMissing(t, fixture.lock)
			beforeReadOnly := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.run("status")
			if code != exitOK ||
				!strings.Contains(stdout, "old  stale") ||
				!strings.Contains(stderr, "overlaps a protected control path") {
				t.Fatalf(
					"status = (%d, %q, %q), want stale control-overlap warning",
					code,
					stdout,
					stderr,
				)
			}
			assertSnapshotUnchanged(t, beforeReadOnly)
			assertCLIMissing(t, fixture.lock)

			code, stdout, stderr = fixture.run("apply", "--dry-run")
			if code != exitOK ||
				!strings.Contains(stdout, "forget") ||
				!strings.Contains(stderr, "overlaps a protected control path") {
				t.Fatalf(
					"apply --dry-run = (%d, %q, %q), want prospective forget",
					code,
					stdout,
					stderr,
				)
			}
			assertSnapshotUnchanged(t, beforeReadOnly)
			assertCLIMissing(t, fixture.lock)

			code, stdout, stderr = fixture.run("apply")
			if code != exitOK ||
				!strings.Contains(stdout, "forget") ||
				!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
				!strings.Contains(stderr, "overlaps a protected control path") {
				t.Fatalf(
					"apply = (%d, %q, %q), want state-only forget",
					code,
					stdout,
					stderr,
				)
			}
			lockInfo, statErr := os.Lstat(fixture.lock)
			if statErr != nil || !lockInfo.Mode().IsRegular() {
				t.Fatalf("lock after mutation = (%v, %v), want advisory regular file", lockInfo, statErr)
			}
			if lockInfo.Mode()&fs.ModeSymlink != 0 {
				t.Fatalf("lock mode = %v, want direct regular file", lockInfo.Mode())
			}
			if modules := loadTestState(t, fixture).Modules; len(modules) != 0 {
				t.Fatalf("state modules after forget = %#v, want empty", modules)
			}

			assertApplyNoMutation(t, fixture, fixture.run)
		})
	}
}

func TestApplyRejectsInvalidStateWithoutMutation(t *testing.T) {
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

func TestCommandsRejectFinalMachineConfigRootSymlinkReadOnly(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "config"})
	fixture.writeMachine(t, []string{"base"}, nil)

	configRoot := filepath.Dir(fixture.config)
	externalRoot := filepath.Join(fixture.root, "external-config")
	if err := os.Rename(configRoot, externalRoot); err != nil {
		t.Fatalf("os.Rename(%q, %q) error = %v", configRoot, externalRoot, err)
	}
	if err := os.Chmod(externalRoot, 0o755); err != nil {
		t.Fatalf("os.Chmod(%q) error = %v", externalRoot, err)
	}
	if err := os.Symlink(externalRoot, configRoot); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", externalRoot, configRoot, err)
	}
	before := snapshotTree(t, fixture.root)

	for _, args := range [][]string{
		{"status"},
		{"apply", "--dry-run"},
		{"apply"},
	} {
		code, stdout, stderr := fixture.run(args...)
		if code != exitError ||
			stdout != "" ||
			!strings.Contains(stderr, "symbolic link") ||
			!strings.Contains(stderr, "dot paths") {
			t.Fatalf(
				"%v = (%d, %q, %q), want machine-config root symlink failure",
				args,
				code,
				stdout,
				stderr,
			)
		}
		assertSnapshotUnchanged(t, before)
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, fixture.lock)
		assertCLIMissing(t, filepath.Dir(fixture.state))
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
}

func TestCommandsRejectAbnormalLockBeforeChangingStateRoot(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeMachine(t, []string{"base"}, nil)
	stateRoot := filepath.Dir(fixture.state)
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(state root) error = %v", err)
	}
	if err := os.Chmod(stateRoot, 0o755); err != nil {
		t.Fatalf("os.Chmod(state root) error = %v", err)
	}
	if err := os.Mkdir(fixture.lock, 0o755); err != nil {
		t.Fatalf("os.Mkdir(lock) error = %v", err)
	}
	before := snapshotTree(t, fixture.root)

	for _, args := range [][]string{
		{"status"},
		{"apply", "--dry-run"},
		{"apply"},
	} {
		code, stdout, stderr := fixture.run(args...)
		if code != exitError ||
			stdout != "" ||
			!strings.Contains(stderr, "lock") ||
			!strings.Contains(stderr, "regular file") ||
			!strings.Contains(stderr, "dot paths") {
			t.Fatalf(
				"%v = (%d, %q, %q), want abnormal lock failure",
				args,
				code,
				stdout,
				stderr,
			)
		}
		assertSnapshotUnchanged(t, before)
		assertCLIMissing(t, fixture.state)
	}
}

func TestMutationLockRejectsSecondProcess(t *testing.T) {
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
}

func TestStatusAndDryRunAreStrictlyReadOnly(t *testing.T) {
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
}
