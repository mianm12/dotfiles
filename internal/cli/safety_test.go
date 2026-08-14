package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/state"
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
				want = "lexically equal or nested"
			}
			if code != exitError ||
				(!strings.Contains(stdout, want) && !strings.Contains(stderr, want)) ||
				!strings.Contains(stdout, "skip ") {
				t.Fatalf("apply = (%d, %q, %q), want skip %q", code, stdout, stderr, want)
			}
			assertOnlyLockBookkeepingChanged(t, before, fixture)
			assertCLIMissing(t, fixture.state)
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
		assertOnlyLockBookkeepingChanged(t, before, fixture)
		assertCLIMissing(t, fixture.config)
		assertCLIMissing(t, fixture.state)
	})

	tests := []struct {
		name             string
		args             []string
		extras           []string
		readOnlyAnalysis bool
		wantCode         int
	}{
		{name: "apply", args: []string{"apply"}},
		{
			name:             "apply dry-run",
			args:             []string{"apply", "--dry-run"},
			readOnlyAnalysis: true,
			wantCode:         exitError,
		},
		{name: "select remove before publication", args: []string{"select", "remove", "app"}, extras: []string{"app"}},
		{
			name:             "status empty repository",
			args:             []string{"status"},
			readOnlyAnalysis: true,
			wantCode:         exitError,
		},
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

			if test.readOnlyAnalysis {
				if code != test.wantCode ||
					(!strings.Contains(stdout, "control paths conflict") &&
						!strings.Contains(stderr, "control paths conflict")) ||
					(!strings.Contains(stdout, fixture.repository) &&
						!strings.Contains(stderr, fixture.repository)) {
					t.Fatalf(
						"analysis = (%d, %q, %q), want topology failure",
						code,
						stdout,
						stderr,
					)
				}
			} else {
				assertCLIControlTopologyFailure(
					t,
					code,
					stdout,
					stderr,
					fixture.repository,
				)
			}
			if test.readOnlyAnalysis {
				assertSnapshotUnchanged(t, before)
				assertCLIMissing(t, fixture.lock)
			} else {
				assertOnlyLockBookkeepingChanged(t, before, fixture)
			}
			assertCLIMissing(t, fixture.state)
			if test.name == "select remove before publication" {
				extras := fixture.loadMachine(t).ExtraModules
				if len(extras) != 1 || extras[0] != "app" {
					t.Fatalf("extra_modules = %v, want unchanged [app]", extras)
				}
			}
		})
	}

	t.Run("reject repository final symlink", func(t *testing.T) {
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
		code, stdout, stderr := fixture.run("status")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "not a directory") {
			t.Fatalf("status with repository symlink = (%d, %q, %q), want rejection", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
	})
}

func assertCLIControlTopologyFailure(
	t *testing.T,
	code int,
	stdout, stderr, conflictPath string,
) {
	t.Helper()
	combined := stdout + stderr
	if code != exitError ||
		!strings.Contains(combined, "control paths conflict") ||
		!strings.Contains(combined, conflictPath) ||
		!strings.Contains(combined, "run `dot paths`") {
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
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("status")
	if code != exitOK ||
		!strings.Contains(stdout, "fact module=app selection=profile") ||
		!strings.Contains(stdout, "skip module=app") ||
		!strings.Contains(stdout, "overlaps a protected control path") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf("status = (%d, %q, %q), want control-prefix skip", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.run("apply", "--dry-run")
	if code != exitError ||
		!strings.Contains(stdout, "skip module=app") ||
		!strings.Contains(stdout, "overlaps a protected control path") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"apply --dry-run = (%d, %q, %q), want complete blocker analysis",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
}

func TestStatusDryRunAndApplyShareInventoryAndLoopResult(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
	fixture.writeMachine(t, []string{"base"}, nil)
	writeCLIFile(t, filepath.Join(fixture.home, ".app"), "personal")
	before := snapshotTree(t, fixture.root)

	statusCode, statusOut, statusErr := fixture.run("status")
	dryCode, dryOut, dryErr := fixture.run("apply", "--dry-run")
	applyCode, applyOut, applyErr := fixture.run("apply")
	if statusCode != exitOK || dryCode != exitError || applyCode != exitError {
		t.Fatalf(
			"exit codes = status:%d dry-run:%d apply:%d, want 0/1/1",
			statusCode,
			dryCode,
			applyCode,
		)
	}
	if !strings.Contains(statusOut, "fact module=app selection=profile state=absent") {
		t.Fatalf("status inventory = %q, want app fact", statusOut)
	}
	statusLines := strings.Join(filterCLILines(statusOut, "skip "), "\n")
	dryLines := strings.Join(filterCLILines(dryOut, "skip "), "\n")
	applyLines := strings.Join(filterCLILines(applyOut, "skip "), "\n")
	if statusLines == "" || statusLines != dryLines || statusLines != applyLines {
		t.Fatalf(
			"loop lines differ:\nstatus=%q\ndry-run=%q\napply=%q",
			statusLines,
			dryLines,
			applyLines,
		)
	}
	for name, stderr := range map[string]string{
		"status":  statusErr,
		"dry-run": dryErr,
		"apply":   applyErr,
	} {
		if !strings.Contains(stderr, "state is missing") || strings.Contains(stderr, "error:") {
			t.Fatalf("%s stderr = %q, want only missing-state warning", name, stderr)
		}
	}
	assertOnlyLockBookkeepingChanged(t, before, fixture)
	if contents, err := os.ReadFile(filepath.Join(fixture.home, ".app")); err != nil || string(contents) != "personal" {
		t.Fatalf("target after blocked commands = (%q, %v), want preserved", contents, err)
	}
}

func filterCLILines(output, prefix string) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.HasPrefix(line, prefix) {
			result = append(result, line)
		}
	}
	return result
}

func TestApplyForgetsStaleTargetOverlappingControlPath(t *testing.T) {
	t.Run("link", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.repository = filepath.Join(fixture.home, "repository")
		writeCLIFile(
			t,
			filepath.Join(fixture.repository, "dot.toml"),
			"version = 1\n[profiles]\nbase = []\n",
		)
		fixture.writeMachine(t, []string{"base"}, nil)
		target := fixture.repository
		record := state.LinkRecord{
			Target: "repository",
			Dest:   filepath.Join(fixture.repository, "removed"),
		}
		fixture.writeState(t, state.Snapshot{
			Home: fixture.home,
			Links: map[state.Key]state.LinkRecord{
				{ModuleID: "old", PlacementID: "stale"}: record,
			},
		})
		beforeTarget := snapshotTree(t, target)

		code, stdout, stderr := fixture.run("apply")

		if code != exitOK ||
			!strings.Contains(stdout, "forget") ||
			!strings.Contains(stdout, "overlaps a protected control path") {
			t.Fatalf(
				"apply = (%d, %q, %q), want state-only forget",
				code,
				stdout,
				stderr,
			)
		}
		assertSnapshotUnchanged(t, beforeTarget)
		if records := loadTestState(t, fixture).Links; len(records) != 0 {
			t.Fatalf("state records after forget = %#v, want empty", records)
		}

		assertApplyNoMutation(t, fixture, fixture.run)
	})
}

func TestApplyForgetsStaleTargetContainingStateRootWithoutChangingEntry(t *testing.T) {
	t.Run("link", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".local")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("os.MkdirAll(target) error = %v", err)
		}
		record := state.LinkRecord{
			Target: ".local",
			Dest:   filepath.Join(fixture.repository, "removed"),
		}
		fixture.writeState(t, state.Snapshot{
			Home: fixture.home,
			Links: map[state.Key]state.LinkRecord{
				{ModuleID: "old", PlacementID: "stale"}: record,
			},
		})
		beforeTarget := snapshotPaths(t, target)

		code, stdout, stderr := fixture.run("apply")

		if code != exitOK ||
			!strings.Contains(stdout, "forget") ||
			!strings.Contains(stdout, "overlaps a protected control path") {
			t.Fatalf(
				"apply = (%d, %q, %q), want state-only forget",
				code,
				stdout,
				stderr,
			)
		}
		assertSnapshotUnchanged(t, beforeTarget)
		if records := loadTestState(t, fixture).Links; len(records) != 0 {
			t.Fatalf("state records after forget = %#v, want empty", records)
		}

		assertApplyNoMutation(t, fixture, fixture.run)
	})
}

func TestStaleTargetEqualToLockIsReadOnlyUntilStateOnlyForget(t *testing.T) {
	t.Run("link", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeMachine(t, []string{"base"}, nil)
		record := state.LinkRecord{
			Target: ".local/state/dot/lock",
			Dest:   filepath.Join(fixture.repository, "removed"),
		}
		fixture.writeState(t, state.Snapshot{
			Home: fixture.home,
			Links: map[state.Key]state.LinkRecord{
				{ModuleID: "old", PlacementID: "stale"}: record,
			},
		})
		assertCLIMissing(t, fixture.lock)
		beforeReadOnly := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.run("status")
		if code != exitOK ||
			!strings.Contains(stdout, "fact module=old selection=none state=present") ||
			!strings.Contains(stdout, "forget") ||
			!strings.Contains(stdout, "stale target is absent") ||
			stderr != "" {
			t.Fatalf(
				"status = (%d, %q, %q), want structured stale forget",
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
			!strings.Contains(stdout, "stale target is absent") ||
			stderr != "" {
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
		if code != exitOK || !strings.Contains(stdout, "forget") {
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
		if records := loadTestState(t, fixture).Links; len(records) != 0 {
			t.Fatalf("state records after forget = %#v, want empty", records)
		}

		assertApplyNoMutation(t, fixture, fixture.run)
	})
}

func TestApplyRejectsInvalidStateWithoutMutation(t *testing.T) {
	tests := []struct {
		name         string
		document     string
		want         string
		wantPathHint bool
		wantHint     string
		forbid       string
	}{
		{name: "corrupt", document: "{", want: "invalid state"},
		{
			name:         "legacy v1",
			document:     `{"version":1,"entries":{},"run_once":{}}`,
			want:         "legacy state version",
			wantPathHint: true,
		},
		{
			name:         "legacy v2",
			document:     `{"version":2,"home":"/old","modules":{}}`,
			want:         "legacy state version",
			wantPathHint: true,
		},
		{
			name:         "legacy v3",
			document:     `{"version":3,"home":"/old","records":[]}`,
			want:         "legacy state version",
			wantPathHint: true,
		},
		{
			name:         "legacy v4",
			document:     `{"version":4,"home":"/old","links":[]}`,
			want:         "legacy state version",
			wantPathHint: true,
		},
		{
			name:     "too new",
			document: `{"version":6}`,
			want:     "state version is newer",
			wantHint: "use a newer `dot` version",
			forbid:   "archive or remove",
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
`, map[string]string{"config": "config"})
			fixture.writeMachine(t, []string{"base"}, nil)
			writeCLIFile(t, fixture.state, test.document)
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.run("apply")
			if code != exitError ||
				stdout != "" ||
				!strings.Contains(stderr, test.want) ||
				(test.wantPathHint && !strings.Contains(stderr, "dot paths")) ||
				(test.wantHint != "" && !strings.Contains(stderr, test.wantHint)) ||
				(test.forbid != "" && strings.Contains(stderr, test.forbid)) {
				t.Fatalf("apply = (%d, %q, %q), want %q failure", code, stdout, stderr, test.want)
			}
			assertOnlyLockBookkeepingChanged(t, before, fixture)
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

	code, stdout, stderr := fixture.run("status")
	if code != exitError || stdout != "" ||
		!strings.Contains(stderr, "symbolic link") || !strings.Contains(stderr, "dot paths") {
		t.Fatalf("status = (%d, %q, %q), want config-root symlink failure", code, stdout, stderr)
	}
	code, stdout, stderr = fixture.run("apply")
	if code != exitError ||
		!strings.Contains(stderr, "symbolic link") {
		t.Fatalf("apply = (%d, %q, %q), want config-root symlink failure", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
	assertCLIMissing(t, filepath.Dir(fixture.state))
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

	code, stdout, stderr := fixture.run("status")
	if code != exitError || stdout != "" ||
		!strings.Contains(stderr, "lock") || !strings.Contains(stderr, "dot paths") {
		t.Fatalf("status = (%d, %q, %q), want abnormal lock failure", code, stdout, stderr)
	}
	code, stdout, stderr = fixture.run("apply")
	if code != exitError ||
		!strings.Contains(stderr, "lock") {
		t.Fatalf("apply = (%d, %q, %q), want abnormal lock failure", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.state)
}

func TestStatusAndDryRunAreStrictlyReadOnly(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
	fixture.writeMachine(t, []string{"base"}, []string{"extra"})
	before := snapshotTree(t, fixture.root)

	code, _, stderr := fixture.run("status")
	if code != exitOK || stderr == "" {
		t.Fatalf("status = (%d, %q)", code, stderr)
	}
	code, stdout, stderr := fixture.run("apply", "--dry-run")
	if code != exitOK || !strings.Contains(stdout, "link module=extra") {
		t.Fatalf("dry-run = (%d, %q, %q)", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.lock)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 || extras[0] != "extra" {
		t.Fatalf("extra_modules = %v, want unchanged [extra]", extras)
	}
}
