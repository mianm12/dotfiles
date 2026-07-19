package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
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
		"\nDRIFT (1)\n" +
		"  ~/alpha/stable                  symlink re-pointed elsewhere\n" +
		"\nPENDING (5)\n" +
		"  ~/alpha/conflict                regular file blocks desired link\n" +
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

	diffOut, diffErr, diffCode := fixture.run(t, "diff", "alpha")
	if diffCode != exitConflict || diffErr != "" || !strings.Contains(diffOut, "CONFLICT  ~/alpha/stable  (link-drift)") {
		t.Fatalf("diff after status = stdout %q, stderr %q, exit %d; want existing conflict/3 contract", diffOut, diffErr, diffCode)
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
	for _, flag := range []string{forceFlagName, pruneFlagName, noPruneFlagName, dryRunFlagName, adoptFlagName, yesFlagName} {
		if command.Flags().Lookup(flag) != nil {
			t.Errorf("status unexpectedly registers local flag %q", flag)
		}
	}
}
