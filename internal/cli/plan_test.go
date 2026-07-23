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

	applyrunner "github.com/mianm12/dotfiles/internal/apply"
	"github.com/mianm12/dotfiles/internal/buildinfo"
	"github.com/mianm12/dotfiles/internal/lock"
	"github.com/mianm12/dotfiles/internal/planner"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
)

func TestProjectApplyPlanWithOutcomes_MapsRuntimeStatuses(t *testing.T) {
	fixture := newPlanCLIFixture(t)
	fixture.redirectEnvironment(t)
	plan, err := planner.PlanApply(planner.ApplyOptions{
		Runtime: dotruntime.Overrides{
			Home:       dotruntime.Override{Value: fixture.home, Set: true},
			Repository: dotruntime.Override{Value: fixture.repository, Set: true},
		},
		Modules: []string{"alpha"},
	})
	if err != nil {
		t.Fatalf("planner.PlanApply() error = %v", err)
	}
	files := plan.FileActions()
	prune := plan.Prune().Actions()
	if len(prune) != 1 {
		t.Fatalf("prune actions = %#v, want one", prune)
	}
	fileOutcomes := make(map[int]applyrunner.ActionOutcomeStatus)
	for index, action := range files {
		switch action.Target {
		case "~/alpha/create":
			fileOutcomes[index] = applyrunner.ActionConflict
		}
	}

	projection, err := projectApplyPlanWithOutcomes(
		plan,
		false,
		fileOutcomes,
		map[int]applyrunner.ActionOutcomeStatus{0: applyrunner.ActionDeferred},
	)
	if err != nil {
		t.Fatalf("projectApplyPlanWithOutcomes() error = %v", err)
	}
	joined := strings.Join(projection.actionLines, "\n")
	if projection.exitCode != exitConflict {
		t.Errorf("runtime file conflict exit = %d, want %d", projection.exitCode, exitConflict)
	}
	for _, want := range []string{
		"CONFLICT  ~/alpha/conflict",
		"CONFLICT  ~/alpha/create",
		"prune (deferred)  ~/alpha/orphan",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("runtime projection = %q, want %q", joined, want)
		}
	}

	fileOutcomes = nil
	for _, test := range []struct {
		name   string
		status applyrunner.ActionOutcomeStatus
		verb   string
	}{
		{name: "conflict", status: applyrunner.ActionConflict, verb: "CONFLICT"},
	} {
		t.Run(test.name, func(t *testing.T) {
			projection, projectErr := projectApplyPlanWithOutcomes(
				plan,
				false,
				fileOutcomes,
				map[int]applyrunner.ActionOutcomeStatus{0: test.status},
			)
			if projectErr != nil {
				t.Fatalf("projectApplyPlanWithOutcomes() error = %v", projectErr)
			}
			want := test.verb + "  ~/alpha/orphan"
			if joined := strings.Join(projection.actionLines, "\n"); !strings.Contains(joined, want) {
				t.Fatalf("runtime projection = %q, want %q", joined, want)
			}
		})
	}

	fullPlan, err := planner.PlanApply(planner.ApplyOptions{
		Runtime: dotruntime.Overrides{
			Home:       dotruntime.Override{Value: fixture.home, Set: true},
			Repository: dotruntime.Override{Value: fixture.repository, Set: true},
		},
	})
	if err != nil {
		t.Fatalf("planner.PlanApply(full) error = %v", err)
	}
	projection, err = projectApplyPlanWithAllOutcomes(
		fullPlan,
		false,
		nil,
		nil,
		map[int]applyrunner.ActionOutcomeStatus{
			0: applyrunner.ActionFailed,
			1: applyrunner.ActionDeferred,
		},
	)
	if err != nil {
		t.Fatalf("projectApplyPlanWithAllOutcomes() error = %v", err)
	}
	joined = strings.Join(projection.actionLines, "\n")
	for _, want := range []string{
		"run-hook  alpha/hooks/setup.sh  (execution-failed)",
		"run-hook  beta/hooks/setup.sh  (earlier-hook-failed)",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("hook runtime projection = %q, want %q", joined, want)
		}
	}
}

func TestApplyDryRun_PrintsStablePlanWithoutMutation(t *testing.T) {
	fixture := newPlanCLIFixture(t)
	before := snapshotCLITree(t, fixture.root)

	stdout, stderr, code := fixture.run(t, "apply", "alpha", "--dry-run", "--verbose")
	wantStdout := "repo=" + fixture.repository + " profile=all os=" + runtime.GOOS + "\n" +
		"CONFLICT  ~/alpha/conflict  (regular-conflict)\n" +
		"link  ~/alpha/create  (target-missing)\n" +
		"skip  ~/alpha/stable  (expected-link)\n" +
		"prune (deferred)  ~/alpha/orphan  (owned-orphan)\n" +
		"run-hook  alpha/hooks/setup.sh  (pending-run-once)\n"
	if code != exitConflict || stdout != wantStdout || stderr != "" {
		t.Fatalf("apply dry-run = stdout %q, stderr %q, exit %d; want stdout %q, empty stderr, exit %d", stdout, stderr, code, wantStdout, exitConflict)
	}
	if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("apply dry-run changed isolated tree\nbefore=%v\nafter=%v", before, after)
	}

	stdout, stderr, code = fixture.run(t, "apply", "alpha", "--dry-run")
	if code != exitConflict || stderr != "" || strings.Contains(stdout, "skip  ~/alpha/stable") {
		t.Fatalf("non-verbose dry-run = stdout %q, stderr %q, exit %d; want conflict without skip", stdout, stderr, code)
	}

	noPrune, noPruneStderr, noPruneCode := fixture.run(t, "apply", "alpha", "--dry-run", "--no-prune")
	pruneFalse, pruneFalseStderr, pruneFalseCode := fixture.run(t, "apply", "alpha", "--dry-run", "--prune=false")
	if noPruneCode != pruneFalseCode || noPrune != pruneFalse || noPruneStderr != pruneFalseStderr {
		t.Fatalf("--prune=false = (%q, %q, %d), want --no-prune projection (%q, %q, %d)", pruneFalse, pruneFalseStderr, pruneFalseCode, noPrune, noPruneStderr, noPruneCode)
	}
}

func TestApplyDryRun_NoOpAndPlannerError(t *testing.T) {
	t.Run("no-op", func(t *testing.T) {
		fixture := newNoOpPlanCLIFixture(t)
		stdout, stderr, code := fixture.run(t, "apply", "--dry-run")
		want := "repo=" + fixture.repository + " profile=clean os=" + runtime.GOOS + "\nAlready up to date.\n"
		if code != exitOK || stdout != want || stderr != "" {
			t.Fatalf("no-op dry-run = stdout %q, stderr %q, exit %d; want %q, empty, 0", stdout, stderr, code, want)
		}

		verbose, stderr, code := fixture.run(t, "apply", "--dry-run", "--verbose")
		if code != exitOK || stderr != "" ||
			!strings.Contains(verbose, "skip  ~/clean/stable  (expected-link)\n") ||
			!strings.HasSuffix(verbose, "Already up to date.\n") {
			t.Fatalf("verbose no-op dry-run = stdout %q, stderr %q, exit %d", verbose, stderr, code)
		}
	})

	t.Run("runtime error wins", func(t *testing.T) {
		fixture := newPlanCLIFixture(t)
		writeCLIFile(t, filepath.Join(fixture.repository, "dot.toml"), "unknown = true\n")
		before := snapshotCLITree(t, fixture.root)
		stdout, stderr, code := fixture.run(t, "apply", "alpha", "--dry-run")
		if code != exitError || stdout != "" || !strings.Contains(stderr, "error:") {
			t.Fatalf("invalid dry-run = stdout %q, stderr %q, exit %d; want error-only exit 1", stdout, stderr, code)
		}
		for _, forbidden := range []string{"repo=", "Already up to date.", "CONFLICT", "run-hook"} {
			if strings.Contains(stdout+stderr, forbidden) {
				t.Errorf("invalid dry-run output %q contains forbidden success text %q", stdout+stderr, forbidden)
			}
		}
		if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
			t.Fatalf("invalid dry-run changed isolated tree\nbefore=%v\nafter=%v", before, after)
		}
	})
}

func TestApply_RejectsAdoptBeforeRuntime(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
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

func TestApply_ReportsRuntimeErrors(t *testing.T) {
	fixture := newPlanCLIFixture(t)
	writeCLIFile(t, filepath.Join(fixture.home, ".config", "dot", "config.toml"), "invalid = [")
	stdout, stderr, code := fixture.run(t, "apply")
	if code != exitError || stdout != "" || !strings.Contains(stderr, "decode machine config") {
		t.Fatalf("invalid mutation apply = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
}

func TestReadOnlyPlan_RejectsConflictingPruneFlagsBeforeRuntime(t *testing.T) {
	fixture := newPlanCLIFixture(t)
	writeCLIFile(t, filepath.Join(fixture.home, ".config", "dot", "config.toml"), "invalid = [")
	before := snapshotCLITree(t, fixture.root)
	stdout, stderr, code := fixture.run(t, "apply", "--dry-run", "--prune", "--no-prune")
	if code != exitError || stdout != "" || !strings.Contains(stderr, "--prune and --no-prune must not be used together") {
		t.Fatalf("conflicting prune = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	if strings.Contains(stderr, "machine config") {
		t.Fatalf("conflicting prune read runtime: %q", stderr)
	}
	if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("conflicting prune changed isolated tree\nbefore=%v\nafter=%v", before, after)
	}
}

func TestPlanCommands_RegisterSpecifiedFlags(t *testing.T) {
	root, err := newRootCommand(environment{})
	if err != nil {
		t.Fatalf("newRootCommand() error = %v", err)
	}
	apply, _, _ := root.Find([]string{"apply"})
	for _, flagName := range []string{pruneFlagName, noPruneFlagName} {
		if apply.Flags().Lookup(flagName) == nil {
			t.Errorf("apply flag %q is not registered", flagName)
		}
	}
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

	args := []string{"apply", "alpha", "--dry-run"}
	stdout, stderr, code := fixture.run(t, args...)
	if code != exitConflict || stderr != "" || !strings.Contains(stdout, "CONFLICT") {
		t.Fatalf("occupied-lock %v = stdout %q, stderr %q, exit %d; want normal conflict plan", args, stdout, stderr, code)
	}
	if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("occupied-lock %v changed isolated tree\nbefore=%v\nafter=%v", args, before, after)
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

	args := []string{"apply", "alpha", "--dry-run"}
	stdout, stderr, code := fixture.run(t, args...)
	if code != exitConflict || stderr != "" || !strings.Contains(stdout, "link  ~/alpha/create") {
		t.Fatalf("missing-state %v = stdout %q, stderr %q, exit %d; want conflict plan", args, stdout, stderr, code)
	}
	if _, err := os.Lstat(stateRoot); !os.IsNotExist(err) {
		t.Fatalf("missing-state %v created state root: %v", args, err)
	}
	if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("missing-state %v changed isolated tree\nbefore=%v\nafter=%v", args, before, after)
	}
}

func TestReadOnlyPlan_ErrorAndRefusalDoNotCreateMissingStateRoot(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		mutate func(*testing.T, planCLIFixture)
	}{
		{
			name: "planner error",
			args: []string{"apply", "alpha", "--dry-run"},
			mutate: func(t *testing.T, fixture planCLIFixture) {
				writeCLIFile(t, filepath.Join(fixture.repository, "dot.toml"), "invalid = [")
			},
		},
		{
			name: "mutation refusal",
			args: []string{"apply", "alpha"},
			mutate: func(t *testing.T, fixture planCLIFixture) {
				writeCLIFile(t, filepath.Join(fixture.home, ".config", "dot", "config.toml"), "invalid = [")
			},
		},
		{
			name: "adopt refusal",
			args: []string{"apply", "alpha", "--dry-run", "--adopt"},
			mutate: func(t *testing.T, fixture planCLIFixture) {
				writeCLIFile(t, filepath.Join(fixture.home, ".config", "dot", "config.toml"), "invalid = [")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlanCLIFixture(t)
			stateRoot := filepath.Join(fixture.home, ".local", "state", "dot")
			if err := os.Remove(filepath.Join(stateRoot, "state.json")); err != nil {
				t.Fatalf("os.Remove(state) error = %v", err)
			}
			if err := os.Remove(stateRoot); err != nil {
				t.Fatalf("os.Remove(state root) error = %v", err)
			}
			test.mutate(t, fixture)
			before := snapshotCLITree(t, fixture.root)

			stdout, _, code := fixture.run(t, test.args...)
			if code != exitError || stdout != "" {
				t.Fatalf("%s = stdout %q, exit %d; want error without success output", test.name, stdout, code)
			}
			if _, err := os.Lstat(stateRoot); !os.IsNotExist(err) {
				t.Fatalf("%s created state root: %v", test.name, err)
			}
			if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
				t.Fatalf("%s changed isolated tree\nbefore=%v\nafter=%v", test.name, before, after)
			}
		})
	}
}

func TestReadOnlyPlan_OutputErrorOverridesConflict(t *testing.T) {
	fixture := newPlanCLIFixture(t)
	fixture.redirectEnvironment(t)
	var stderr bytes.Buffer
	code := run([]string{
		"apply", "alpha", "--dry-run", "--home", fixture.home, "--repo", fixture.repository,
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

func TestApplyDryRun_ReportsScaffoldDeletedAndUnownedPruneWarnings(t *testing.T) {
	t.Run("scaffold deleted", func(t *testing.T) {
		fixture := newNoOpPlanCLIFixture(t)
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "dot.toml"), `target = "~/clean"
[files.deleted]
kind = "scaffold"
`)
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "deleted"), "literal\n")
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
					Source:    "modules/clean/deleted",
					AppliedAt: "2026-07-19T00:00:00Z",
				},
			},
			RunOnce: map[string]planRunOnce{},
		})

		stdout, stderr, code := fixture.run(t, "apply", "--dry-run")
		wantStdout := "repo=" + fixture.repository + " profile=clean os=" + runtime.GOOS + "\n"
		if code != exitActionable || stdout != wantStdout || !strings.Contains(stderr, "warning: ~/clean/deleted: scaffold target was deleted") {
			t.Fatalf("scaffold-deleted dry-run = stdout %q, stderr %q, exit %d", stdout, stderr, code)
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
		stdout, stderr, code := fixture.run(t, "apply", "alpha", "--dry-run")
		if code != exitConflict || !strings.Contains(stdout, "prune (deferred)  ~/alpha/orphan  (unowned-orphan)") ||
			!strings.Contains(stderr, "warning: ~/alpha/orphan: orphan target is no longer owned") {
			t.Fatalf("unowned-orphan dry-run = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
	})
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
	writeCLIFile(t, filepath.Join(repository, "dot.toml"), `[profiles]
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
	writeCLIFile(t, filepath.Join(repository, "dot.toml"), `[profiles]
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
		stdin:       strings.NewReader(""),
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
