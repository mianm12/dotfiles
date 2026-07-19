package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/buildinfo"
	"github.com/mianm12/dotfiles/internal/lock"
)

func TestDiff_PrintsStablePlanAndExitPriority(t *testing.T) {
	fixture := newPlanCLIFixture(t)
	before := snapshotCLITree(t, fixture.root)

	stdout, stderr, code := fixture.run(t, "diff", "alpha", "--verbose")
	wantStdout := "repo=" + fixture.repository + " profile=all os=" + runtime.GOOS + "\n" +
		"CONFLICT  ~/alpha/conflict  (regular-conflict)\n" +
		"link  ~/alpha/create  (target-missing)\n" +
		"skip  ~/alpha/stable  (expected-link)\n" +
		"prune (deferred)  ~/alpha/orphan  (owned-orphan)\n" +
		"run-hook  alpha/hooks/setup.sh  (pending-run-once)\n"
	if code != exitConflict || stdout != wantStdout || stderr != "" {
		t.Fatalf("diff = stdout %q, stderr %q, exit %d; want stdout %q, empty stderr, exit %d", stdout, stderr, code, wantStdout, exitConflict)
	}
	if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("diff changed isolated tree\nbefore=%v\nafter=%v", before, after)
	}

	stdout, stderr, code = fixture.run(t, "diff", "alpha")
	if code != exitConflict || stderr != "" || strings.Contains(stdout, "skip  ~/alpha/stable") {
		t.Fatalf("non-verbose diff = stdout %q, stderr %q, exit %d; want conflict without skip", stdout, stderr, code)
	}
}

func TestDiff_ForceNoPruneAndFullScope(t *testing.T) {
	fixture := newPlanCLIFixture(t)

	forced, stderr, code := fixture.run(t, "diff", "alpha", "--force")
	if code != exitActionable || stderr != "" {
		t.Fatalf("forced diff = stdout %q, stderr %q, exit %d; want actionable", forced, stderr, code)
	}
	for _, want := range []string{
		"backup+replace  ~/alpha/conflict  (regular-conflict)",
		"prune  ~/alpha/orphan  (owned-orphan)",
		"run-hook  alpha/hooks/setup.sh  (pending-run-once)",
	} {
		if !strings.Contains(forced, want) {
			t.Errorf("forced diff stdout = %q, want line %q", forced, want)
		}
	}
	if strings.Contains(forced, "prune (deferred)") {
		t.Errorf("forced diff stdout = %q, want active prune", forced)
	}

	withoutPrune, stderr, code := fixture.run(t, "diff", "alpha", "--no-prune")
	if code != exitConflict || stderr != "" || strings.Contains(withoutPrune, "prune") {
		t.Fatalf("no-prune diff = stdout %q, stderr %q, exit %d; want conflict without prune", withoutPrune, stderr, code)
	}

	full, stderr, code := fixture.run(t, "diff", "--force")
	if code != exitActionable || stderr != "" {
		t.Fatalf("full diff = stdout %q, stderr %q, exit %d; want actionable", full, stderr, code)
	}
	alphaIndex := strings.Index(full, "link  ~/alpha/create")
	betaIndex := strings.Index(full, "link  ~/beta/create")
	alphaHookIndex := strings.Index(full, "run-hook  alpha/hooks/setup.sh")
	betaHookIndex := strings.Index(full, "run-hook  beta/hooks/setup.sh")
	if alphaIndex < 0 || betaIndex <= alphaIndex || alphaHookIndex <= betaIndex || betaHookIndex <= alphaHookIndex {
		t.Fatalf("full diff output order is unstable: %q", full)
	}
}

func TestDiff_NoOpAndPlannerError(t *testing.T) {
	t.Run("no-op", func(t *testing.T) {
		fixture := newNoOpPlanCLIFixture(t)
		stdout, stderr, code := fixture.run(t, "diff")
		want := "repo=" + fixture.repository + " profile=clean os=" + runtime.GOOS + "\nAlready up to date.\n"
		if code != exitOK || stdout != want || stderr != "" {
			t.Fatalf("no-op diff = stdout %q, stderr %q, exit %d; want %q, empty, 0", stdout, stderr, code, want)
		}

		verbose, stderr, code := fixture.run(t, "diff", "--verbose")
		if code != exitOK || stderr != "" || !strings.Contains(verbose, "skip  ~/clean/stable  (expected-link)\n") || !strings.HasSuffix(verbose, "Already up to date.\n") {
			t.Fatalf("verbose no-op = stdout %q, stderr %q, exit %d", verbose, stderr, code)
		}
	})

	t.Run("runtime error wins", func(t *testing.T) {
		fixture := newPlanCLIFixture(t)
		writeCLIFile(t, filepath.Join(fixture.repository, "dot.toml"), "unknown = true\n")
		before := snapshotCLITree(t, fixture.root)
		stdout, stderr, code := fixture.run(t, "diff", "alpha")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "error:") {
			t.Fatalf("invalid diff = stdout %q, stderr %q, exit %d; want error-only exit 1", stdout, stderr, code)
		}
		for _, forbidden := range []string{"repo=", "Already up to date.", "CONFLICT", "run-hook"} {
			if strings.Contains(stdout+stderr, forbidden) {
				t.Errorf("invalid diff output %q contains forbidden success text %q", stdout+stderr, forbidden)
			}
		}
		if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
			t.Fatalf("invalid diff changed isolated tree\nbefore=%v\nafter=%v", before, after)
		}
	})
}

func TestApplyDryRun_MatchesDiffProjection(t *testing.T) {
	fixture := newPlanCLIFixture(t)
	tests := []struct {
		name    string
		options []string
	}{
		{name: "partial verbose conflict", options: []string{"alpha", "--verbose"}},
		{name: "partial force", options: []string{"alpha", "--force"}},
		{name: "partial no-prune", options: []string{"alpha", "--no-prune"}},
		{name: "full force yes", options: []string{"--force", "--yes"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diffArgs := append([]string{"diff"}, withoutOption(test.options, "--yes")...)
			applyArgs := append([]string{"apply"}, test.options...)
			applyArgs = append(applyArgs, "--dry-run")
			diffStdout, diffStderr, diffCode := fixture.run(t, diffArgs...)
			applyStdout, applyStderr, applyCode := fixture.run(t, applyArgs...)
			if applyCode != diffCode || applyStdout != diffStdout || applyStderr != diffStderr {
				t.Fatalf(
					"apply dry-run = stdout %q, stderr %q, exit %d; diff = stdout %q, stderr %q, exit %d",
					applyStdout,
					applyStderr,
					applyCode,
					diffStdout,
					diffStderr,
					diffCode,
				)
			}
		})
	}

	noPrune, noPruneStderr, noPruneCode := fixture.run(t, "apply", "alpha", "--dry-run", "--no-prune")
	pruneFalse, pruneFalseStderr, pruneFalseCode := fixture.run(t, "apply", "alpha", "--dry-run", "--prune=false")
	if noPruneCode != pruneFalseCode || noPrune != pruneFalse || noPruneStderr != pruneFalseStderr {
		t.Fatalf("--prune=false = (%q, %q, %d), want --no-prune projection (%q, %q, %d)", pruneFalse, pruneFalseStderr, pruneFalseCode, noPrune, noPruneStderr, noPruneCode)
	}
}

func TestApply_RejectsMutationAndAdoptBeforeRuntime(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "naked apply",
			args: []string{"apply"},
			want: "real apply is not available in M1; use dot apply --dry-run",
		},
		{
			name: "module mutation flags",
			args: []string{"apply", "alpha", "--force", "--no-prune"},
			want: "real apply is not available in M1; use dot apply --dry-run",
		},
		{
			name: "dry-run adopt",
			args: []string{"apply", "alpha", "--dry-run", "--adopt"},
			want: "--adopt requires M2 and is not supported in this build",
		},
		{
			name: "mutation adopt",
			args: []string{"apply", "--adopt"},
			want: "--adopt requires M2 and is not supported in this build",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlanCLIFixture(t)
			writeCLIFile(t, filepath.Join(fixture.home, ".config", "dot", "config.toml"), "invalid = [")
			writeCLIFile(t, filepath.Join(fixture.repository, "dot.toml"), "invalid = [")
			before := snapshotCLITree(t, fixture.root)

			stdout, stderr, code := fixture.run(t, test.args...)
			if code != exitError || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("rejected apply = stdout %q, stderr %q, exit %d; want %q", stdout, stderr, code, test.want)
			}
			for _, forbidden := range []string{"machine config", "manifest", "repo=", "Already up to date."} {
				if strings.Contains(stdout+stderr, forbidden) {
					t.Errorf("rejected apply output %q contains runtime/success text %q", stdout+stderr, forbidden)
				}
			}
			if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
				t.Fatalf("rejected apply changed isolated tree\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestReadOnlyPlan_RejectsConflictingPruneFlagsBeforeRuntime(t *testing.T) {
	for _, command := range []string{"diff", "apply"} {
		t.Run(command, func(t *testing.T) {
			fixture := newPlanCLIFixture(t)
			writeCLIFile(t, filepath.Join(fixture.home, ".config", "dot", "config.toml"), "invalid = [")
			before := snapshotCLITree(t, fixture.root)
			args := []string{command, "--prune", "--no-prune"}
			if command == "apply" {
				args = append(args, "--dry-run")
			}
			stdout, stderr, code := fixture.run(t, args...)
			if code != exitError || stdout != "" || !strings.Contains(stderr, "--prune and --no-prune must not be used together") {
				t.Fatalf("conflicting prune = stdout %q, stderr %q, exit %d", stdout, stderr, code)
			}
			if strings.Contains(stderr, "machine config") {
				t.Fatalf("conflicting prune read runtime: %q", stderr)
			}
			if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
				t.Fatalf("conflicting prune changed isolated tree\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestPlanCommands_RegisterSpecifiedFlags(t *testing.T) {
	root, err := newRootCommand(environment{})
	if err != nil {
		t.Fatalf("newRootCommand() error = %v", err)
	}
	for _, commandName := range []string{"diff", "apply"} {
		command, _, err := root.Find([]string{commandName})
		if err != nil || command.Name() != commandName {
			t.Fatalf("root.Find(%q) = (%#v, %v)", commandName, command, err)
		}
		for _, flagName := range []string{forceFlagName, pruneFlagName, noPruneFlagName} {
			if command.Flags().Lookup(flagName) == nil {
				t.Errorf("%s flag %q is not registered", commandName, flagName)
			}
		}
	}
	apply, _, _ := root.Find([]string{"apply"})
	for _, flagName := range []string{dryRunFlagName, adoptFlagName, yesFlagName} {
		if apply.Flags().Lookup(flagName) == nil {
			t.Errorf("apply flag %q is not registered", flagName)
		}
	}
	if shorthand := apply.Flags().Lookup(dryRunFlagName).Shorthand; shorthand != "n" {
		t.Errorf("--dry-run shorthand = %q, want n", shorthand)
	}
	if shorthand := apply.Flags().Lookup(yesFlagName).Shorthand; shorthand != "y" {
		t.Errorf("--yes shorthand = %q, want y", shorthand)
	}
}

func TestReadOnlyPlan_SucceedsWhileMutationLockIsHeld(t *testing.T) {
	fixture := newPlanCLIFixture(t)
	stateRoot := filepath.Join(fixture.home, ".local", "state", "dot")
	lockPath := filepath.Join(stateRoot, "lock")
	owner, err := lock.Acquire(stateRoot, lockPath)
	if err != nil {
		t.Fatalf("lock.Acquire() error = %v", err)
	}
	t.Cleanup(func() {
		if err := owner.Release(); err != nil {
			t.Errorf("lock Ownership.Release() error = %v", err)
		}
	})
	before := snapshotCLITree(t, fixture.root)

	for _, args := range [][]string{
		{"diff", "alpha"},
		{"apply", "alpha", "--dry-run"},
	} {
		stdout, stderr, code := fixture.run(t, args...)
		if code != exitConflict || stderr != "" || !strings.Contains(stdout, "CONFLICT") {
			t.Fatalf("occupied-lock %v = stdout %q, stderr %q, exit %d; want normal conflict plan", args, stdout, stderr, code)
		}
		if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
			t.Fatalf("occupied-lock %v changed isolated tree\nbefore=%v\nafter=%v", args, before, after)
		}
	}
}

func TestReadOnlyPlan_MissingStateRootRemainsMissing(t *testing.T) {
	fixture := newPlanCLIFixture(t)
	stateRoot := filepath.Join(fixture.home, ".local", "state", "dot")
	if err := os.Remove(filepath.Join(stateRoot, "state.json")); err != nil {
		t.Fatalf("os.Remove(state) error = %v", err)
	}
	if err := os.Remove(stateRoot); err != nil {
		t.Fatalf("os.Remove(state root) error = %v", err)
	}
	before := snapshotCLITree(t, fixture.root)

	for _, args := range [][]string{
		{"diff", "alpha", "--force"},
		{"apply", "alpha", "--dry-run", "--force"},
	} {
		stdout, stderr, code := fixture.run(t, args...)
		if code != exitActionable || stderr != "" || !strings.Contains(stdout, "link  ~/alpha/create") {
			t.Fatalf("missing-state %v = stdout %q, stderr %q, exit %d; want actionable plan", args, stdout, stderr, code)
		}
		if _, err := os.Lstat(stateRoot); !os.IsNotExist(err) {
			t.Fatalf("missing-state %v created state root: %v", args, err)
		}
		if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
			t.Fatalf("missing-state %v changed isolated tree\nbefore=%v\nafter=%v", args, before, after)
		}
	}
}

func TestReadOnlyPlan_OutputErrorOverridesConflict(t *testing.T) {
	fixture := newPlanCLIFixture(t)
	fixture.redirectEnvironment(t)
	var stderr bytes.Buffer
	code := run([]string{
		"diff", "alpha", "--home", fixture.home, "--repo", fixture.repository,
	}, environment{
		stdout:      failingWriter{err: os.ErrClosed},
		stderr:      &stderr,
		lookupEnv:   os.LookupEnv,
		userHomeDir: os.UserHomeDir,
		build:       buildinfo.Info{Version: "v0.0.0"},
		goos:        runtime.GOOS,
	})
	if code != exitError || !strings.Contains(stderr.String(), "write stdout") {
		t.Fatalf("output failure = stderr %q, exit %d; want output error priority 1", stderr.String(), code)
	}
}

func TestDiff_ReportsScaffoldDeletedAndUnownedPruneWarnings(t *testing.T) {
	t.Run("scaffold deleted", func(t *testing.T) {
		fixture := newNoOpPlanCLIFixture(t)
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "deleted.template"), "template\n")
		stableSource := filepath.Join(fixture.repository, "modules", "clean", "stable")
		writePlanState(t, fixture.home, planStateDocument{
			Version: 1,
			Entries: map[string]planStateEntry{
				"~/clean/stable": {
					Module:    "clean",
					Kind:      "symlink",
					Source:    "modules/clean/stable",
					LinkDest:  stableSource,
					AppliedAt: "2026-07-19T00:00:00Z",
				},
				"~/clean/deleted": {
					Module:    "clean",
					Kind:      "scaffold",
					Source:    "modules/clean/deleted.template",
					AppliedAt: "2026-07-19T00:00:00Z",
				},
			},
			RunOnce: map[string]planRunOnce{},
		})

		stdout, stderr, code := fixture.run(t, "diff")
		wantStdout := "repo=" + fixture.repository + " profile=clean os=" + runtime.GOOS + "\n"
		if code != exitActionable || stdout != wantStdout || !strings.Contains(stderr, "warning: ~/clean/deleted: scaffold target was deleted") {
			t.Fatalf("scaffold-deleted diff = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
	})

	t.Run("unowned orphan", func(t *testing.T) {
		fixture := newPlanCLIFixture(t)
		orphan := filepath.Join(fixture.home, "alpha", "orphan")
		if err := os.Remove(orphan); err != nil {
			t.Fatalf("os.Remove(orphan) error = %v", err)
		}
		if err := os.Symlink("changed", orphan); err != nil {
			t.Fatalf("os.Symlink(changed orphan) error = %v", err)
		}
		stdout, stderr, code := fixture.run(t, "diff", "alpha", "--force")
		if code != exitActionable || !strings.Contains(stdout, "prune  ~/alpha/orphan  (unowned-orphan)") ||
			!strings.Contains(stderr, "warning: ~/alpha/orphan: orphan target is no longer owned") {
			t.Fatalf("unowned-orphan diff = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
	})
}

func withoutOption(values []string, omitted string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value != omitted {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

type planCLIFixture struct {
	root       string
	home       string
	repository string
}

func newPlanCLIFixture(t *testing.T) planCLIFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	makeDirectory(t, home)
	writeCLIFile(t, filepath.Join(home, ".config", "dot", "config.toml"), "profile = \"all\"\n")
	writeCLIFile(t, filepath.Join(repository, "dot.toml"), `requires = ">=0.0.0"
[profiles]
all = ["beta", "alpha"]
`)
	writePlanModule(t, repository, "alpha", true)
	writePlanModule(t, repository, "beta", false)

	alphaRoot := filepath.Join(home, "alpha")
	makeDirectory(t, alphaRoot)
	writeCLIFile(t, filepath.Join(alphaRoot, "conflict"), "user data\n")
	stableSource := filepath.Join(repository, "modules", "alpha", "stable")
	if err := os.Symlink(stableSource, filepath.Join(alphaRoot, "stable")); err != nil {
		t.Fatalf("os.Symlink(stable) error = %v", err)
	}
	if err := os.Symlink("owned", filepath.Join(alphaRoot, "orphan")); err != nil {
		t.Fatalf("os.Symlink(orphan) error = %v", err)
	}

	state := planStateDocument{
		Version: 1,
		Entries: map[string]planStateEntry{
			"~/alpha/stable": {
				Module:    "alpha",
				Kind:      "symlink",
				Source:    "modules/alpha/stable",
				LinkDest:  stableSource,
				AppliedAt: "2026-07-19T00:00:00Z",
			},
			"~/alpha/orphan": {
				Module:    "alpha",
				Kind:      "symlink",
				Source:    "modules/alpha/obsolete",
				LinkDest:  "owned",
				AppliedAt: "2026-07-19T00:00:00Z",
			},
		},
		RunOnce: map[string]planRunOnce{},
	}
	writePlanState(t, home, state)
	return planCLIFixture{root: root, home: home, repository: repository}
}

func newNoOpPlanCLIFixture(t *testing.T) planCLIFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	makeDirectory(t, home)
	writeCLIFile(t, filepath.Join(home, ".config", "dot", "config.toml"), "profile = \"clean\"\n")
	writeCLIFile(t, filepath.Join(repository, "dot.toml"), `requires = ">=0.0.0"
[profiles]
clean = ["clean"]
`)
	writeCLIFile(t, filepath.Join(repository, "modules", "clean", "dot.toml"), `target = "~/clean"
`)
	writeCLIFile(t, filepath.Join(repository, "modules", "clean", "stable"), "stable\n")
	stableSource := filepath.Join(repository, "modules", "clean", "stable")
	stableTarget := filepath.Join(home, "clean", "stable")
	makeDirectory(t, filepath.Dir(stableTarget))
	if err := os.Symlink(stableSource, stableTarget); err != nil {
		t.Fatalf("os.Symlink(stable) error = %v", err)
	}
	writePlanState(t, home, planStateDocument{
		Version: 1,
		Entries: map[string]planStateEntry{
			"~/clean/stable": {
				Module:    "clean",
				Kind:      "symlink",
				Source:    "modules/clean/stable",
				LinkDest:  stableSource,
				AppliedAt: "2026-07-19T00:00:00Z",
			},
		},
		RunOnce: map[string]planRunOnce{},
	})
	return planCLIFixture{root: root, home: home, repository: repository}
}

func writePlanModule(t *testing.T, repository, module string, conflict bool) {
	t.Helper()
	moduleRoot := filepath.Join(repository, "modules", module)
	writeCLIFile(t, filepath.Join(moduleRoot, "dot.toml"), `target = "~/`+module+`"
[hooks]
run_once = ["hooks/setup.sh"]
`)
	if conflict {
		writeCLIFile(t, filepath.Join(moduleRoot, "conflict"), "managed source\n")
		writeCLIFile(t, filepath.Join(moduleRoot, "stable"), "stable\n")
	}
	writeCLIFile(t, filepath.Join(moduleRoot, "create"), "create\n")
	writeCLIFile(t, filepath.Join(moduleRoot, "hooks", "setup.sh"), "#!/bin/sh\nexit 99\n")
}

func (fixture planCLIFixture) run(t *testing.T, commandArgs ...string) (string, string, int) {
	t.Helper()
	fixture.redirectEnvironment(t)
	args := append([]string(nil), commandArgs...)
	args = append(args, "--home", fixture.home, "--repo", fixture.repository)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(args, environment{
		stdout:      &stdout,
		stderr:      &stderr,
		lookupEnv:   os.LookupEnv,
		userHomeDir: os.UserHomeDir,
		build:       buildinfo.Info{Version: "v0.0.0", Commit: "test", BuildTime: "test"},
		goos:        runtime.GOOS,
	})
	return stdout.String(), stderr.String(), code
}

func (fixture planCLIFixture) redirectEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", fixture.home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fixture.home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(fixture.home, ".local", "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(fixture.home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(fixture.home, ".cache"))
	t.Setenv("DOT_CONFIG", filepath.Join(fixture.home, ".config", "dot", "config.toml"))
	t.Setenv("DOT_REPO", fixture.repository)
}

type planStateDocument struct {
	Version int                       `json:"version"`
	Entries map[string]planStateEntry `json:"entries"`
	RunOnce map[string]planRunOnce    `json:"run_once"`
}

type planStateEntry struct {
	Module    string `json:"module"`
	Kind      string `json:"kind"`
	Source    string `json:"source"`
	LinkDest  string `json:"link_dest,omitempty"`
	AppliedAt string `json:"applied_at"`
}

type planRunOnce struct {
	Hash       string `json:"hash"`
	ExecutedAt string `json:"executed_at"`
}

func writePlanState(t *testing.T, home string, document planStateDocument) {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("json.Marshal(state) error = %v", err)
	}
	writeCLIFile(t, filepath.Join(home, ".local", "state", "dot", "state.json"), string(encoded))
}
