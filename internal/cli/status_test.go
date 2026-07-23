package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/mianm12/dotfiles/internal/buildinfo"
	"github.com/mianm12/dotfiles/internal/lock"
	"github.com/mianm12/dotfiles/internal/planner"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
)

func TestStatus_PrintsStableHealthSectionsAndReturnsActionable(t *testing.T) {
	fixture := newPlanCLIFixture(t)
	stable := filepath.Join(fixture.home, "alpha", "stable")
	if err := os.Remove(stable); err != nil {
		t.Fatalf("os.Remove(stable) error = %v", err)
	}
	if err := os.Symlink("changed", stable); err != nil {
		t.Fatalf("os.Symlink(changed) error = %v", err)
	}
	before := snapshotCLITree(t, fixture.root)

	stdout, stderr, code := fixture.run(t, "status")
	want := "Profile: all (2 modules, 4 files managed)\n" +
		"\nDRIFT (2)\n" +
		"  ~/alpha/conflict                regular file blocks desired link\n" +
		"  ~/alpha/stable                  symlink re-pointed elsewhere\n" +
		"\nPENDING (4)\n" +
		"  ~/alpha/create                  desired symlink missing\n" +
		"  ~/beta/create                   desired symlink missing\n" +
		"  alpha/hooks/setup.sh            run_once pending execution\n" +
		"  beta/hooks/setup.sh             run_once pending execution\n" +
		"\nORPHAN / PENDING PRUNE (1)\n" +
		"  ~/alpha/orphan                  owned orphan from previous profile; prune deferred by file conflict\n"
	if code != exitActionable || stdout != want || stderr != "" {
		t.Fatalf("status = stdout %q, stderr %q, exit %d; want stdout %q, empty stderr, exit %d", stdout, stderr, code, want, exitActionable)
	}
	if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("status changed isolated tree\nbefore=%v\nafter=%v", before, after)
	}

	planOut, planErr, planCode := fixture.run(t, "apply", "alpha", "--dry-run")
	if planCode != exitConflict || planErr != "" || !strings.Contains(planOut, "CONFLICT  ~/alpha/stable  (link-drift)") {
		t.Fatalf("dry-run after status = stdout %q, stderr %q, exit %d; want existing conflict/3 contract", planOut, planErr, planCode)
	}
}

func TestStatus_AllFileConflictsAreDriftRegardlessOfHistory(t *testing.T) {
	tests := []struct {
		name        string
		object      string
		description string
	}{
		{name: "unowned symlink", object: "symlink", description: "unowned symlink blocks desired link"},
		{name: "regular file", object: "regular", description: "regular file blocks desired link"},
		{name: "directory", object: "directory", description: "directory blocks desired link"},
		{name: "special file", object: "special", description: "special file blocks desired link"},
	}
	for _, test := range tests {
		for _, historical := range []bool{false, true} {
			name := test.name + "/without history"
			description := test.description
			if historical {
				name = test.name + "/with history"
				if test.object == "symlink" {
					description = "symlink re-pointed elsewhere"
				}
			}
			t.Run(name, func(t *testing.T) {
				fixture := newConflictStatusFixture(t, test.object, historical)
				before := snapshotCLITree(t, fixture.root)

				stdout, stderr, code := fixture.run(t, "status")
				want := "Profile: clean (1 modules, 2 files managed)\n" +
					"\nDRIFT (1)\n" +
					"  ~/clean/blocker                 " + description + "\n"
				if code != exitActionable || stdout != want || stderr != "" {
					t.Fatalf("conflict status = stdout %q, stderr %q, exit %d; want %q, empty stderr, 2", stdout, stderr, code, want)
				}
				if strings.Contains(stdout, "PENDING") {
					t.Errorf("conflict status misclassified blocker as pending: %q", stdout)
				}
				if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
					t.Fatalf("conflict status changed isolated tree\nbefore=%v\nafter=%v", before, after)
				}
			})
		}
	}
}

func TestStatus_CleanAndUnassignedOnlyRemainExitZero(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		fixture := newNoOpPlanCLIFixture(t)
		stdout, stderr, code := fixture.run(t, "status")
		want := "Profile: clean (1 modules, 1 files managed)\n\nClean.\n"
		if code != exitOK || stdout != want || stderr != "" {
			t.Fatalf("clean status = stdout %q, stderr %q, exit %d; want %q, empty stderr, 0", stdout, stderr, code, want)
		}
	})

	t.Run("unassigned only", func(t *testing.T) {
		fixture := newNoOpPlanCLIFixture(t)
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "unused", "note"), "unused\n")
		stdout, stderr, code := fixture.run(t, "status")
		want := "Profile: clean (1 modules, 1 files managed)\n" +
			"\nUNASSIGNED MODULES (1)\n" +
			"  unused                          not referenced by any profile\n" +
			"\nClean.\n"
		if code != exitOK || stdout != want || stderr != "" {
			t.Fatalf("unassigned status = stdout %q, stderr %q, exit %d; want %q, empty stderr, 0", stdout, stderr, code, want)
		}
	})
}

func TestStatus_InvalidRuntimeDoesNotClaimClean(t *testing.T) {
	fixture := newNoOpPlanCLIFixture(t)
	writeCLIFile(t, filepath.Join(fixture.repository, "dot.toml"), "unknown = true\n")
	before := snapshotCLITree(t, fixture.root)

	stdout, stderr, code := fixture.run(t, "status")
	if code != exitError || stdout != "" || !strings.Contains(stderr, "error:") {
		t.Fatalf("invalid status = stdout %q, stderr %q, exit %d; want error-only exit 1", stdout, stderr, code)
	}
	for _, forbidden := range []string{"Profile:", "DRIFT", "PENDING", "UNASSIGNED", "Clean."} {
		if strings.Contains(stdout+stderr, forbidden) {
			t.Errorf("invalid status output %q contains forbidden result text %q", stdout+stderr, forbidden)
		}
	}
	if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("invalid status changed isolated tree\nbefore=%v\nafter=%v", before, after)
	}
}

func TestStatus_HasNoModuleScopeOrPlanFlags(t *testing.T) {
	fixture := newNoOpPlanCLIFixture(t)
	stdout, stderr, code := fixture.run(t, "status", "clean")
	if code != exitError || stdout != "" || !strings.Contains(stderr, "unknown command") && !strings.Contains(stderr, "accepts 0 arg") {
		t.Fatalf("scoped status = stdout %q, stderr %q, exit %d; want argument error", stdout, stderr, code)
	}

	root, err := newRootCommand(environment{goos: runtime.GOOS})
	if err != nil {
		t.Fatalf("newRootCommand() error = %v", err)
	}
	command, _, err := root.Find([]string{"status"})
	if err != nil || command.Name() != "status" {
		t.Fatalf("root.Find(status) = (%#v, %v)", command, err)
	}
	for _, flag := range []string{pruneFlagName, noPruneFlagName, dryRunFlagName, adoptFlagName, yesFlagName} {
		if command.Flags().Lookup(flag) != nil {
			t.Errorf("status unexpectedly registers local flag %q", flag)
		}
	}
}

func TestStatus_ScaffoldLifecycleDistinguishesCleanSkipsFromPending(t *testing.T) {
	t.Run("user-owned scaffold missing and modified are clean", func(t *testing.T) {
		fixture := newNoOpPlanCLIFixture(t)
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "dot.toml"), `target = "~/clean"
[files.deleted]
kind = "scaffold"
[files.modified]
kind = "scaffold"
`)
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "deleted"), "literal\n")
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "modified"), "literal\n")
		writeCLIFile(t, filepath.Join(fixture.home, "clean", "modified"), "user changed bytes\n")
		stableSource := filepath.Join(fixture.repository, "modules", "clean", "stable")
		writePlanState(t, fixture.home, planStateDocument{
			Version: 1,
			Entries: map[string]planStateEntry{
				"~/clean/deleted": {
					Module: "clean", Kind: "scaffold", Source: "modules/clean/deleted", AppliedAt: "2026-07-19T00:00:00Z",
				},
				"~/clean/modified": {
					Module: "clean", Kind: "scaffold", Source: "modules/clean/modified", AppliedAt: "2026-07-19T00:00:00Z",
				},
				"~/clean/stable": {
					Module: "clean", Kind: "symlink", Source: "modules/clean/stable", LinkDest: stableSource, AppliedAt: "2026-07-19T00:00:00Z",
				},
			},
			RunOnce: map[string]planRunOnce{},
		})

		stdout, stderr, code := fixture.run(t, "status")
		want := "Profile: clean (1 modules, 3 files managed)\n\nClean.\n"
		if code != exitOK || stdout != want || stderr != "" {
			t.Fatalf("scaffold skips status = stdout %q, stderr %q, exit %d; want %q, empty stderr, 0", stdout, stderr, code, want)
		}
	})

	t.Run("new scaffold is pending", func(t *testing.T) {
		fixture := newNoOpPlanCLIFixture(t)
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "dot.toml"), `target = "~/clean"
[files.fresh]
kind = "scaffold"
`)
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "fresh"), "literal\n")

		stdout, stderr, code := fixture.run(t, "status")
		want := "Profile: clean (1 modules, 2 files managed)\n" +
			"\nPENDING (1)\n" +
			"  ~/clean/fresh                   scaffold not yet created\n"
		if code != exitActionable || stdout != want || stderr != "" {
			t.Fatalf("fresh scaffold status = stdout %q, stderr %q, exit %d; want %q, empty stderr, 2", stdout, stderr, code, want)
		}
	})
}

func TestStatus_KindMigrationAndAliasUseTheirOwnTaxonomy(t *testing.T) {
	t.Run("kind migrations", func(t *testing.T) {
		fixture := newNoOpPlanCLIFixture(t)
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "dot.toml"), `target = "~/clean"
[files.to-scaffold]
kind = "scaffold"
`)
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "from-scaffold"), "desired link\n")
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "to-scaffold"), "literal\n")
		writeCLIFile(t, filepath.Join(fixture.home, "clean", "from-scaffold"), "user file\n")
		stableSource := filepath.Join(fixture.repository, "modules", "clean", "stable")
		if err := os.Symlink(stableSource, filepath.Join(fixture.home, "clean", "to-scaffold")); err != nil {
			t.Fatalf("os.Symlink(to-scaffold) error = %v", err)
		}
		writePlanState(t, fixture.home, planStateDocument{
			Version: 1,
			Entries: map[string]planStateEntry{
				"~/clean/from-scaffold": {
					Module: "clean", Kind: "scaffold", Source: "modules/clean/from-scaffold", AppliedAt: "2026-07-19T00:00:00Z",
				},
				"~/clean/stable": {
					Module: "clean", Kind: "symlink", Source: "modules/clean/stable", LinkDest: stableSource, AppliedAt: "2026-07-19T00:00:00Z",
				},
				"~/clean/to-scaffold": {
					Module: "clean", Kind: "symlink", Source: "modules/clean/old", LinkDest: stableSource, AppliedAt: "2026-07-19T00:00:00Z",
				},
			},
			RunOnce: map[string]planRunOnce{},
		})

		stdout, stderr, code := fixture.run(t, "status")
		want := "Profile: clean (1 modules, 3 files managed)\n" +
			"\nDRIFT (1)\n" +
			"  ~/clean/from-scaffold           regular file blocks desired link\n" +
			"\nPENDING (1)\n" +
			"  ~/clean/to-scaffold             owned symlink pending scaffold migration\n"
		if code != exitActionable || stdout != want || stderr != "" || strings.Contains(stdout, "ORPHAN") {
			t.Fatalf("kind migration status = stdout %q, stderr %q, exit %d; want %q, empty stderr, 2", stdout, stderr, code, want)
		}
	})

	t.Run("historical path alias", func(t *testing.T) {
		fixture := newAliasStatusFixture(t)
		stdout, stderr, code := fixture.run(t, "status")
		want := "Profile: alias (1 modules, 1 files managed)\n" +
			"\nPENDING (1)\n" +
			"  ~/alias/item                    state metadata needs refresh\n"
		if code != exitActionable || stdout != want || stderr != "" || strings.Contains(stdout, "ORPHAN") {
			t.Fatalf("alias status = stdout %q, stderr %q, exit %d; want pending without orphan", stdout, stderr, code)
		}
	})
}

func TestStatus_ReportsEveryOrphanClass(t *testing.T) {
	fixture := newNoOpPlanCLIFixture(t)
	stableSource := filepath.Join(fixture.repository, "modules", "clean", "stable")
	writeCLIFile(t, filepath.Join(fixture.home, "clean", "scaffold-old"), "user data\n")
	if err := os.Symlink("owned", filepath.Join(fixture.home, "clean", "owned-old")); err != nil {
		t.Fatalf("os.Symlink(owned-old) error = %v", err)
	}
	if err := os.Symlink("changed", filepath.Join(fixture.home, "clean", "unowned-old")); err != nil {
		t.Fatalf("os.Symlink(unowned-old) error = %v", err)
	}
	writePlanState(t, fixture.home, planStateDocument{
		Version: 1,
		Entries: map[string]planStateEntry{
			"~/clean/owned-old": {
				Module: "clean", Kind: "symlink", Source: "modules/clean/old", LinkDest: "owned", AppliedAt: "2026-07-19T00:00:00Z",
			},
			"~/clean/scaffold-old": {
				Module: "clean", Kind: "scaffold", Source: "modules/clean/old.template", AppliedAt: "2026-07-19T00:00:00Z",
			},
			"~/clean/stable": {
				Module: "clean", Kind: "symlink", Source: "modules/clean/stable", LinkDest: stableSource, AppliedAt: "2026-07-19T00:00:00Z",
			},
			"~/clean/unowned-old": {
				Module: "clean", Kind: "symlink", Source: "modules/clean/old", LinkDest: "owned", AppliedAt: "2026-07-19T00:00:00Z",
			},
		},
		RunOnce: map[string]planRunOnce{},
	})

	stdout, stderr, code := fixture.run(t, "status")
	want := "Profile: clean (1 modules, 1 files managed)\n" +
		"\nORPHAN / PENDING PRUNE (3)\n" +
		"  ~/clean/owned-old               owned orphan from previous profile\n" +
		"  ~/clean/scaffold-old            scaffold orphan pending state cleanup\n" +
		"  ~/clean/unowned-old             unowned orphan pending state cleanup\n"
	if code != exitActionable || stdout != want || stderr != "" {
		t.Fatalf("orphan status = stdout %q, stderr %q, exit %d; want %q, empty stderr, 2", stdout, stderr, code, want)
	}
}

func TestStatus_HookSkipAndProfileOverrideUsePlannerScope(t *testing.T) {
	t.Run("current fingerprints are omitted", func(t *testing.T) {
		fixture := newPlanCLIFixture(t)
		fixture.redirectEnvironment(t)
		plan, err := planner.PlanApply(planner.ApplyOptions{
			Runtime: dotruntime.Overrides{
				Home:       dotruntime.Override{Value: fixture.home, Set: true},
				Repository: dotruntime.Override{Value: fixture.repository, Set: true},
			},
		})
		if err != nil {
			t.Fatalf("planner.PlanApply() error = %v", err)
		}
		runOnce := make(map[string]planRunOnce)
		for _, action := range plan.Hooks().Actions() {
			runOnce[action.StateKey] = planRunOnce{Hash: action.Fingerprint, ExecutedAt: "2026-07-19T00:00:00Z"}
		}
		stableSource := filepath.Join(fixture.repository, "modules", "alpha", "stable")
		writePlanState(t, fixture.home, planStateDocument{
			Version: 1,
			Entries: map[string]planStateEntry{
				"~/alpha/orphan": {
					Module: "alpha", Kind: "symlink", Source: "modules/alpha/obsolete", LinkDest: "owned", AppliedAt: "2026-07-19T00:00:00Z",
				},
				"~/alpha/stable": {
					Module: "alpha", Kind: "symlink", Source: "modules/alpha/stable", LinkDest: stableSource, AppliedAt: "2026-07-19T00:00:00Z",
				},
			},
			RunOnce: runOnce,
		})

		stdout, stderr, code := fixture.run(t, "status")
		if code != exitActionable || stderr != "" || strings.Contains(stdout, "run_once pending execution") || strings.Contains(stdout, "hooks/setup.sh") {
			t.Fatalf("hook-skip status = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
	})

	t.Run("global profile selects a complete profile", func(t *testing.T) {
		fixture := newPlanCLIFixture(t)
		writeCLIFile(t, filepath.Join(fixture.repository, "dot.toml"), `[profiles]
all = ["beta", "alpha"]
alpha = ["alpha"]
`)
		stdout, stderr, code := fixture.run(t, "status", "--profile", "alpha")
		if code != exitActionable || stderr != "" || !strings.HasPrefix(stdout, "Profile: alpha (1 modules, 3 files managed)\n") {
			t.Fatalf("profile status = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
		if strings.Contains(stdout, "~/beta/") || strings.Contains(stdout, "beta/hooks") {
			t.Errorf("profile status leaked another profile's actions: %q", stdout)
		}
	})
}

func TestStatus_InvalidConfigAndStateAreErrorOnly(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, planCLIFixture)
	}{
		{
			name: "config",
			mutate: func(t *testing.T, fixture planCLIFixture) {
				writeCLIFile(t, filepath.Join(fixture.home, ".config", "dot", "config.toml"), "invalid = [")
			},
		},
		{
			name: "state",
			mutate: func(t *testing.T, fixture planCLIFixture) {
				writeCLIFile(t, filepath.Join(fixture.home, ".local", "state", "dot", "state.json"), "{")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNoOpPlanCLIFixture(t)
			test.mutate(t, fixture)
			before := snapshotCLITree(t, fixture.root)
			stdout, stderr, code := fixture.run(t, "status")
			if code != exitError || stdout != "" || !strings.Contains(stderr, "error:") || strings.Contains(stderr, "Clean.") {
				t.Fatalf("invalid %s status = stdout %q, stderr %q, exit %d", test.name, stdout, stderr, code)
			}
			if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid %s status changed tree\nbefore=%v\nafter=%v", test.name, before, after)
			}
		})
	}
}

func TestStatus_MissingStateAndHeldLockRemainReadOnly(t *testing.T) {
	t.Run("missing state root", func(t *testing.T) {
		fixture := newNoOpPlanCLIFixture(t)
		stateRoot := filepath.Join(fixture.home, ".local", "state", "dot")
		if err := os.Remove(filepath.Join(stateRoot, "state.json")); err != nil {
			t.Fatalf("os.Remove(state) error = %v", err)
		}
		if err := os.Remove(stateRoot); err != nil {
			t.Fatalf("os.Remove(state root) error = %v", err)
		}
		before := snapshotCLITree(t, fixture.root)

		stdout, stderr, code := fixture.run(t, "status")
		if code != exitActionable || stderr != "" || !strings.Contains(stdout, "state metadata needs refresh") {
			t.Fatalf("missing-state status = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
		if _, err := os.Lstat(stateRoot); !os.IsNotExist(err) {
			t.Fatalf("status created missing state root: %v", err)
		}
		if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
			t.Fatalf("missing-state status changed tree\nbefore=%v\nafter=%v", before, after)
		}
	})

	t.Run("held mutation lock", func(t *testing.T) {
		fixture := newNoOpPlanCLIFixture(t)
		stateRoot := filepath.Join(fixture.home, ".local", "state", "dot")
		owner, err := lock.Acquire(stateRoot, filepath.Join(stateRoot, "lock"))
		if err != nil {
			t.Fatalf("lock.Acquire() error = %v", err)
		}
		t.Cleanup(func() {
			if err := owner.Release(); err != nil {
				t.Errorf("lock Ownership.Release() error = %v", err)
			}
		})
		before := snapshotCLITree(t, fixture.root)

		stdout, stderr, code := fixture.run(t, "status")
		if code != exitOK || stderr != "" || !strings.HasSuffix(stdout, "Clean.\n") {
			t.Fatalf("held-lock status = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
		if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
			t.Fatalf("held-lock status changed tree\nbefore=%v\nafter=%v", before, after)
		}
	})
}

func TestStatus_OutputErrorOverridesClean(t *testing.T) {
	fixture := newNoOpPlanCLIFixture(t)
	fixture.redirectEnvironment(t)
	var stderr bytes.Buffer
	code := run([]string{"status", "--home", fixture.home, "--repo", fixture.repository}, environment{
		stdout:      failingWriter{err: os.ErrClosed},
		stderr:      &stderr,
		lookupEnv:   os.LookupEnv,
		userHomeDir: os.UserHomeDir,
		build:       buildinfo.Info{Version: "v0.0.0"},
		goos:        runtime.GOOS,
	})
	if code != exitError || !strings.Contains(stderr.String(), "write stdout") {
		t.Fatalf("status output failure = stderr %q, exit %d; want output error priority 1", stderr.String(), code)
	}
}

func newAliasStatusFixture(t *testing.T) planCLIFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	writeCLIFile(t, filepath.Join(home, ".config", "dot", "config.toml"), "profile = \"alias\"\n")
	writeCLIFile(t, filepath.Join(repository, "dot.toml"), `[profiles]
alias = ["alias"]
`)
	writeCLIFile(t, filepath.Join(repository, "modules", "alias", "dot.toml"), "target = \"~/alias\"\n")
	writeCLIFile(t, filepath.Join(repository, "modules", "alias", "item"), "item\n")
	makeDirectory(t, filepath.Join(home, "real"))
	if err := os.Symlink("real", filepath.Join(home, "alias")); err != nil {
		t.Fatalf("os.Symlink(alias) error = %v", err)
	}
	source := filepath.Join(repository, "modules", "alias", "item")
	if err := os.Symlink(source, filepath.Join(home, "real", "item")); err != nil {
		t.Fatalf("os.Symlink(item) error = %v", err)
	}
	writePlanState(t, home, planStateDocument{
		Version: 1,
		Entries: map[string]planStateEntry{
			"~/real/item": {
				Module: "alias", Kind: "symlink", Source: "modules/alias/item", LinkDest: source, AppliedAt: "2026-07-19T00:00:00Z",
			},
		},
		RunOnce: map[string]planRunOnce{},
	})
	return planCLIFixture{root: root, home: home, repository: repository}
}

func newConflictStatusFixture(t *testing.T, object string, historical bool) planCLIFixture {
	t.Helper()
	fixture := newNoOpPlanCLIFixture(t)
	writeCLIFile(t, filepath.Join(fixture.repository, "modules", "clean", "blocker"), "desired\n")
	target := filepath.Join(fixture.home, "clean", "blocker")
	switch object {
	case "symlink":
		if err := os.Symlink("elsewhere", target); err != nil {
			t.Fatalf("os.Symlink(blocker) error = %v", err)
		}
	case "regular":
		writeCLIFile(t, target, "user data\n")
	case "directory":
		makeDirectory(t, target)
	case "special":
		if err := syscall.Mkfifo(target, 0o600); err != nil {
			t.Fatalf("syscall.Mkfifo(blocker) error = %v", err)
		}
	default:
		t.Fatalf("unknown conflict object %q", object)
	}

	stableSource := filepath.Join(fixture.repository, "modules", "clean", "stable")
	entries := map[string]planStateEntry{
		"~/clean/stable": {
			Module: "clean", Kind: "symlink", Source: "modules/clean/stable", LinkDest: stableSource, AppliedAt: "2026-07-19T00:00:00Z",
		},
	}
	if historical {
		entries["~/clean/blocker"] = planStateEntry{
			Module: "clean", Kind: "symlink", Source: "modules/clean/old", LinkDest: "historical", AppliedAt: "2026-07-19T00:00:00Z",
		}
	}
	writePlanState(t, fixture.home, planStateDocument{
		Version: 1,
		Entries: entries,
		RunOnce: map[string]planRunOnce{},
	})
	return fixture
}
