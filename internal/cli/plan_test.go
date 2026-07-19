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
