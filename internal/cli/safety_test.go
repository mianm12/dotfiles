package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestApplyRejectsStaleTargetContainingControlPath(t *testing.T) {
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

func TestApplyRejectsFinalMachineConfigRootSymlinkBeforeArtifactMutation(t *testing.T) {
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
	before := snapshotPaths(t, configRoot, externalRoot, fixture.config)

	code, stdout, stderr := fixture.run("apply")

	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "symbolic link") {
		t.Fatalf(
			"apply = (%d, %q, %q), want machine-config root symlink failure",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
	assertCLIMissing(t, fixture.state)
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
