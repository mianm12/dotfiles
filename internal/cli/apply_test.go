package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	applyrunner "github.com/mianm12/dotfiles/internal/apply"
	"github.com/mianm12/dotfiles/internal/buildinfo"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/spf13/cobra"
)

func TestApply_MutatesAndConvergesWithoutRepeatWrites(t *testing.T) {
	fixture := newMutationCLIFixture(t)
	realHomeBefore := snapshotCLIPath(t, fixture.realHome)

	stdout, stderr, code := fixture.run(t, nil, "apply")
	if code != exitOK || stderr != "" {
		t.Fatalf("first apply = stdout %q, stderr %q, exit %d; want success", stdout, stderr, code)
	}
	wantContext := "repo=" + fixture.repository + " profile=all os=" + runtime.GOOS + "\n"
	if !strings.HasPrefix(stdout, wantContext) || !strings.Contains(stdout, "link  ~/alpha/file  (target-missing)\n") {
		t.Fatalf("first apply stdout = %q, want context and link action", stdout)
	}
	target := filepath.Join(fixture.home, "alpha", "file")
	if destination, err := os.Readlink(target); err != nil || destination != filepath.Join(fixture.repository, "modules", "alpha", "file") {
		t.Fatalf("created link = %q, %v", destination, err)
	}
	statePath := filepath.Join(fixture.home, ".local", "state", "dot", "state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state was not stored: %v", err)
	}
	afterFirst := snapshotCLITree(t, fixture.root)

	stdout, stderr, code = fixture.run(t, nil, "apply")
	if code != exitOK || stderr != "" || !strings.HasSuffix(stdout, "Already up to date.\n") {
		t.Fatalf("second apply = stdout %q, stderr %q, exit %d; want converged", stdout, stderr, code)
	}
	if afterSecond := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(afterSecond, afterFirst) {
		t.Fatalf("converged apply changed isolated tree\nafter first=%v\nafter second=%v", afterFirst, afterSecond)
	}
	if realHomeAfter := snapshotCLIPath(t, fixture.realHome); !reflect.DeepEqual(realHomeAfter, realHomeBefore) {
		t.Fatalf("apply changed real HOME sentinel: before=%v after=%v", realHomeBefore, realHomeAfter)
	}
}

func TestApply_ForceReportsExactBackupPath(t *testing.T) {
	fixture := newMutationCLIFixture(t)
	target := filepath.Join(fixture.home, "alpha", "file")
	makeDirectory(t, filepath.Dir(target))
	writeCLIFile(t, target, "user data\n")

	stdout, stderr, code := fixture.run(t, nil, "apply", "--force")
	if code != exitOK || stderr != "" {
		t.Fatalf("force apply = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	line := findLineWithPrefix(stdout, "backup  ")
	if line == "" {
		t.Fatalf("force apply stdout = %q, want exact backup line", stdout)
	}
	backupPath := strings.TrimPrefix(line, "backup  ")
	content, err := os.ReadFile(backupPath)
	if err != nil || string(content) != "user data\n" {
		t.Fatalf("reported backup %q = %q, %v", backupPath, content, err)
	}
	if !strings.HasPrefix(backupPath, filepath.Join(fixture.home, ".local", "state", "dot", "backup")+string(os.PathSeparator)) {
		t.Fatalf("reported backup %q is outside isolated backup root", backupPath)
	}
	afterFirst := snapshotCLITree(t, fixture.root)
	stdout, stderr, code = fixture.run(t, nil, "apply", "--force")
	if code != exitOK || stderr != "" || !strings.HasSuffix(stdout, "Already up to date.\n") || strings.Contains(stdout, "backup  ") {
		t.Fatalf("converged force apply = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	if afterSecond := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(afterSecond, afterFirst) {
		t.Fatalf("converged force apply changed tree\nafter first=%v\nafter second=%v", afterFirst, afterSecond)
	}
}

func TestApply_ConfirmationAcceptsYesAndRejectsEOF(t *testing.T) {
	for _, test := range []struct {
		name       string
		open       func() (io.ReadCloser, error)
		wantCode   int
		wantPruned bool
	}{
		{name: "yes", open: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("y\n")), nil
		}, wantCode: exitOK, wantPruned: true},
		{name: "EOF", open: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("")), nil
		}, wantCode: exitActionable, wantPruned: false},
		{name: "no terminal", open: func() (io.ReadCloser, error) {
			return nil, os.ErrNotExist
		}, wantCode: exitActionable, wantPruned: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWholeModulePruneCLIFixture(t)
			stdout, stderr, code := fixture.run(t, test.open, "apply")
			if code != test.wantCode || !strings.Contains(stderr, "Remove orphaned modules?") {
				t.Fatalf("apply confirmation = stdout %q, stderr %q, exit %d", stdout, stderr, code)
			}
			if test.wantPruned {
				if !strings.Contains(stderr, "old:") || !strings.Contains(stderr, "delete target") {
					t.Fatalf("confirmation summary = %q, want module and deletion effect", stderr)
				}
				if _, err := os.Lstat(filepath.Join(fixture.home, "old")); !os.IsNotExist(err) {
					t.Fatalf("accepted prune retained target: %v", err)
				}
			} else {
				if !strings.Contains(stdout, "prune (deferred)  ~/old") {
					t.Fatalf("refused prune stdout = %q, want deferred action", stdout)
				}
				if _, err := os.Lstat(filepath.Join(fixture.home, "old")); err != nil {
					t.Fatalf("refused prune changed target: %v", err)
				}
			}
		})
	}
}

func TestApply_ConfirmationEOFReportsTerminalCloseError(t *testing.T) {
	fixture := newWholeModulePruneCLIFixture(t)
	closeErr := errors.New("terminal close failed")
	stdout, stderr, code := fixture.run(t, func() (io.ReadCloser, error) {
		return closeErrorReadCloser{Reader: strings.NewReader(""), err: closeErr}, nil
	}, "apply")
	if code != exitError || !strings.Contains(stderr, closeErr.Error()) ||
		!strings.Contains(stderr, "confirmation input ended; prune deferred") ||
		!strings.Contains(stdout, "prune (deferred)  ~/old") {
		t.Fatalf("EOF close error = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	if _, err := os.Lstat(filepath.Join(fixture.home, "old")); err != nil {
		t.Fatalf("EOF close error changed prune target: %v", err)
	}
	if _, exists := readPlanState(t, fixture.home).Entries["~/old"]; !exists {
		t.Fatal("EOF close error removed orphan state")
	}
}

func TestConfirmationCallback_EOFJoinsWarningAndCloseErrors(t *testing.T) {
	warningErr := errors.New("warning write failed")
	closeErr := errors.New("terminal close failed")
	writer := &failOnSubstringWriter{
		needle: "confirmation input ended",
		err:    warningErr,
	}
	command := &cobra.Command{}
	command.SetErr(writer)
	confirm := confirmationCallback(command, false, func() (io.ReadCloser, error) {
		return closeErrorReadCloser{Reader: strings.NewReader(""), err: closeErr}, nil
	})

	accepted, err := confirm(nil)
	if accepted || !errors.Is(err, warningErr) || !errors.Is(err, closeErr) {
		t.Fatalf("confirmation = accepted %t, error %v; want joined warning/close errors", accepted, err)
	}
}

func TestApply_ConflictStillCommitsIndependentSuccess(t *testing.T) {
	fixture := newMutationCLIFixture(t)
	writeCLIFile(t, filepath.Join(fixture.repository, "modules", "alpha", "conflict"), "managed conflict\n")
	conflictTarget := filepath.Join(fixture.home, "alpha", "conflict")
	writeCLIFile(t, conflictTarget, "user data\n")

	stdout, stderr, code := fixture.run(t, nil, "apply")
	if code != exitConflict || stderr != "" || !strings.Contains(stdout, "CONFLICT  ~/alpha/conflict") {
		t.Fatalf("conflicted apply = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	if content, err := os.ReadFile(conflictTarget); err != nil || string(content) != "user data\n" {
		t.Fatalf("conflict target = %q, %v; want preserved", content, err)
	}
	created := filepath.Join(fixture.home, "alpha", "file")
	if _, err := os.Readlink(created); err != nil {
		t.Fatalf("independent action did not commit: %v", err)
	}
	state := readPlanState(t, fixture.home)
	if _, ok := state.Entries["~/alpha/file"]; !ok {
		t.Fatal("partial success was not persisted")
	}
	if _, ok := state.Entries["~/alpha/conflict"]; ok {
		t.Fatal("conflict was incorrectly persisted")
	}
}

func TestApply_PartialScopeAndCanonicalPruneEffects(t *testing.T) {
	t.Run("partial scope", func(t *testing.T) {
		fixture := newMutationCLIFixture(t)
		writeCLIFile(t, filepath.Join(fixture.repository, "dot.toml"), "requires = \">=0.0.0\"\n[profiles]\nall = [\"alpha\", \"beta\"]\n")
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "beta", "dot.toml"), "target = \"~/beta\"\n")
		writeCLIFile(t, filepath.Join(fixture.repository, "modules", "beta", "file"), "beta\n")
		stdout, stderr, code := fixture.run(t, nil, "apply", "alpha")
		if code != exitOK || stderr != "" || strings.Contains(stdout, "~/beta/") {
			t.Fatalf("partial apply = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
		if _, err := os.Lstat(filepath.Join(fixture.home, "beta", "file")); !os.IsNotExist(err) {
			t.Fatalf("partial apply touched beta: %v", err)
		}
	})

	t.Run("P1 P2 P3", func(t *testing.T) {
		fixture := newMutationCLIFixture(t)
		owned := filepath.Join(fixture.home, "owned")
		unowned := filepath.Join(fixture.home, "unowned")
		scaffold := filepath.Join(fixture.home, "scaffold")
		if err := os.Symlink("owned-destination", owned); err != nil {
			t.Fatalf("create owned orphan: %v", err)
		}
		if err := os.Symlink("changed-destination", unowned); err != nil {
			t.Fatalf("create unowned orphan: %v", err)
		}
		writeCLIFile(t, scaffold, "user scaffold\n")
		writePlanState(t, fixture.home, planStateDocument{
			Version: 1,
			Entries: map[string]planStateEntry{
				"~/owned":    {Module: "old", Kind: "symlink", Source: "modules/old/owned", LinkDest: "owned-destination", AppliedAt: "2026-07-20T00:00:00Z"},
				"~/unowned":  {Module: "old", Kind: "symlink", Source: "modules/old/unowned", LinkDest: "original-destination", AppliedAt: "2026-07-20T00:00:00Z"},
				"~/scaffold": {Module: "old", Kind: "scaffold", Source: "modules/old/scaffold.template", AppliedAt: "2026-07-20T00:00:00Z"},
			},
			RunOnce: map[string]planRunOnce{},
		})

		stdout, stderr, code := fixture.run(t, nil, "apply", "--yes")
		if code != exitActionable || !strings.Contains(stderr, "orphan target is no longer owned") {
			t.Fatalf("P1/P2/P3 apply = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
		if _, err := os.Lstat(owned); !os.IsNotExist(err) {
			t.Fatalf("P2 retained owned target: %v", err)
		}
		for _, retained := range []string{unowned, scaffold} {
			if _, err := os.Lstat(retained); err != nil {
				t.Fatalf("state-only prune changed %q: %v", retained, err)
			}
		}
		state := readPlanState(t, fixture.home)
		for _, key := range []string{"~/owned", "~/unowned", "~/scaffold"} {
			if _, ok := state.Entries[key]; ok {
				t.Errorf("successful prune retained state key %q", key)
			}
		}
	})
}

func TestApply_M1UnsupportedInputsFailClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, mutationCLIFixture)
		want   string
	}{
		{
			name: "managed desired",
			mutate: func(t *testing.T, fixture mutationCLIFixture) {
				writeCLIFile(t, filepath.Join(fixture.repository, "modules", "alpha", "managed.tmpl"), "managed\n")
			},
			want: "managed",
		},
		{
			name: "rendered state",
			mutate: func(t *testing.T, fixture mutationCLIFixture) {
				writeCLIFile(t, filepath.Join(fixture.home, ".local", "state", "dot", "state.json"), `{"version":1,"entries":{"~/old":{"module":"old","kind":"rendered","source":"modules/old/file.tmpl","hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","applied_at":"2026-07-20T00:00:00Z"}},"run_once":{}}`)
			},
			want: "rendered",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMutationCLIFixture(t)
			test.mutate(t, fixture)
			statePath := filepath.Join(fixture.home, ".local", "state", "dot", "state.json")
			beforeState, beforeStateErr := os.ReadFile(statePath)
			stdout, stderr, code := fixture.run(t, nil, "apply")
			if code != exitError || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("unsupported apply = stdout %q, stderr %q, exit %d", stdout, stderr, code)
			}
			if _, err := os.Lstat(filepath.Join(fixture.home, "alpha")); !os.IsNotExist(err) {
				t.Fatalf("unsupported apply changed target tree: %v", err)
			}
			afterState, afterStateErr := os.ReadFile(statePath)
			if !errorsMatchNotExist(beforeStateErr, afterStateErr) || !bytes.Equal(afterState, beforeState) {
				t.Fatalf("unsupported apply changed state: before=%q/%v after=%q/%v", beforeState, beforeStateErr, afterState, afterStateErr)
			}
			if _, err := os.Stat(filepath.Join(fixture.home, ".local", "state", "dot", "backup")); !os.IsNotExist(err) {
				t.Fatalf("unsupported apply created backup: %v", err)
			}
		})
	}
}

func TestApply_YesSkipsTerminalAndHookGatePrecedesMutation(t *testing.T) {
	t.Run("yes skips terminal", func(t *testing.T) {
		fixture := newWholeModulePruneCLIFixture(t)
		opened := false
		stdout, stderr, code := fixture.run(t, func() (io.ReadCloser, error) {
			opened = true
			return nil, os.ErrNotExist
		}, "apply", "--yes")
		if code != exitOK || opened || stderr != "" || strings.Contains(stdout, "deferred") {
			t.Fatalf("apply --yes = stdout %q, stderr %q, exit %d, opened=%t", stdout, stderr, code, opened)
		}
	})

	t.Run("scoped hook fails closed", func(t *testing.T) {
		fixture := newPlanCLIFixture(t)
		statePath := filepath.Join(fixture.home, ".local", "state", "dot", "state.json")
		beforeState, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("read baseline state: %v", err)
		}
		beforeTargets := snapshotCLITree(t, filepath.Join(fixture.home, "alpha"))
		stdout, stderr, code := fixture.run(t, "apply", "alpha", "--force")
		if code != exitError || !strings.Contains(stderr, "before hook execution is available") {
			t.Fatalf("hook-gated apply = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
		if afterTargets := snapshotCLITree(t, filepath.Join(fixture.home, "alpha")); !reflect.DeepEqual(afterTargets, beforeTargets) {
			t.Fatalf("hook-gated apply changed targets\nbefore=%v\nafter=%v", beforeTargets, afterTargets)
		}
		if afterState, readErr := os.ReadFile(statePath); readErr != nil || !bytes.Equal(afterState, beforeState) {
			t.Fatalf("hook-gated apply changed state: after=%q error=%v", afterState, readErr)
		}
		if _, statErr := os.Stat(filepath.Join(fixture.home, ".local", "state", "dot", "backup")); !os.IsNotExist(statErr) {
			t.Fatalf("hook-gated apply created backup root: %v", statErr)
		}
	})
}

func TestApply_NoPruneSkipsConfirmationAndPreservesOrphan(t *testing.T) {
	fixture := newWholeModulePruneCLIFixture(t)
	opened := false
	stdout, stderr, code := fixture.run(t, func() (io.ReadCloser, error) {
		opened = true
		return nil, os.ErrNotExist
	}, "apply", "--no-prune")
	if code != exitOK || stderr != "" || opened || strings.Contains(stdout, "prune") {
		t.Fatalf("apply --no-prune = stdout %q, stderr %q, exit %d, opened=%t", stdout, stderr, code, opened)
	}
	if _, err := os.Lstat(filepath.Join(fixture.home, "old")); err != nil {
		t.Fatalf("--no-prune changed orphan target: %v", err)
	}
	if _, exists := readPlanState(t, fixture.home).Entries["~/old"]; !exists {
		t.Fatal("--no-prune removed orphan state")
	}
}

func TestApply_OutputFailureHasErrorPriorityAfterCommittedMutation(t *testing.T) {
	fixture := newMutationCLIFixture(t)
	t.Setenv("HOME", fixture.realHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fixture.home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(fixture.home, ".local", "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(fixture.home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(fixture.home, ".cache"))
	t.Setenv("DOT_CONFIG", filepath.Join(fixture.home, ".config", "dot", "config.toml"))
	t.Setenv("DOT_REPO", fixture.repository)
	var stderr bytes.Buffer
	code := run([]string{"apply", "--home", fixture.home, "--repo", fixture.repository}, environment{
		stdout:      failingWriter{err: os.ErrClosed},
		stderr:      &stderr,
		lookupEnv:   os.LookupEnv,
		userHomeDir: os.UserHomeDir,
		build:       buildinfo.Info{Version: "v0.0.0"},
		goos:        runtime.GOOS,
	})
	if code != exitError || !strings.Contains(stderr.String(), "write stdout") {
		t.Fatalf("failed apply output = stderr %q, exit %d", stderr.String(), code)
	}
	if _, err := os.Readlink(filepath.Join(fixture.home, "alpha", "file")); err != nil {
		t.Fatalf("output failure falsely rolled back committed target: %v", err)
	}
	if _, exists := readPlanState(t, fixture.home).Entries["~/alpha/file"]; !exists {
		t.Fatal("output failure falsely rolled back committed state")
	}
}

func TestApply_InvalidRunnerPlanPreservesErrorOrFailsProtocol(t *testing.T) {
	fixture := newMutationCLIFixture(t)

	t.Run("existing runner error", func(t *testing.T) {
		runtimeErr := errors.New("existing runner failure")
		stdout, stderr, code := runInjectedApply(t, fixture, applyrunner.Result{}, runtimeErr)
		if code != exitError || stdout != "" || !strings.Contains(stderr, runtimeErr.Error()) ||
			strings.Contains(stderr, "apply execution protocol violation") {
			t.Fatalf("invalid plan with error = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
	})

	t.Run("nil runner error", func(t *testing.T) {
		stdout, stderr, code := runInjectedApply(t, fixture, applyrunner.Result{}, nil)
		if code != exitError || stdout != "" || !strings.Contains(stderr, "apply execution protocol violation") {
			t.Fatalf("invalid plan without error = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
	})
}

func TestApply_SealedExecutedResultPreservesRuntimeErrorPriority(t *testing.T) {
	fixture := newMutationCLIFixture(t)
	fixture.setEnvironment(t)
	result, err := applyrunner.Run(applyrunner.Options{
		Runtime: dotruntime.Overrides{
			Home:       dotruntime.Override{Value: fixture.home, Set: true},
			Repository: dotruntime.Override{Value: fixture.repository, Set: true},
		},
		CLIVersion: "v0.0.0",
	})
	if err != nil || !result.Valid(false) || !result.StateCommitted() {
		t.Fatalf("applyrunner.Run() = (%#v, %v), want valid committed result", result, err)
	}

	runtimeErr := errors.New("injected post-execution failure")
	stdout, stderr, code := runInjectedApply(t, fixture, result, runtimeErr)
	if code != exitError || !strings.Contains(stderr, runtimeErr.Error()) ||
		!strings.Contains(stdout, "link  ~/alpha/file  (target-missing)") {
		t.Fatalf("sealed result with runtime error = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
}

type mutationCLIFixture struct {
	root       string
	home       string
	repository string
	realHome   string
}

func newMutationCLIFixture(t *testing.T) mutationCLIFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	realHome := filepath.Join(root, "real-home")
	makeDirectory(t, home)
	makeDirectory(t, realHome)
	writeCLIFile(t, filepath.Join(realHome, "sentinel"), "unchanged\n")
	writeCLIFile(t, filepath.Join(home, ".config", "dot", "config.toml"), "profile = \"all\"\n")
	writeCLIFile(t, filepath.Join(repository, "dot.toml"), "requires = \">=0.0.0\"\n[profiles]\nall = [\"alpha\"]\n")
	writeCLIFile(t, filepath.Join(repository, "modules", "alpha", "dot.toml"), "target = \"~/alpha\"\n")
	writeCLIFile(t, filepath.Join(repository, "modules", "alpha", "file"), "managed\n")
	return mutationCLIFixture{root: root, home: home, repository: repository, realHome: realHome}
}

func newWholeModulePruneCLIFixture(t *testing.T) mutationCLIFixture {
	t.Helper()
	fixture := newMutationCLIFixture(t)
	orphan := filepath.Join(fixture.home, "old")
	if err := os.Symlink("owned-destination", orphan); err != nil {
		t.Fatalf("os.Symlink(orphan) error = %v", err)
	}
	writePlanState(t, fixture.home, planStateDocument{
		Version: 1,
		Entries: map[string]planStateEntry{
			"~/old": {
				Module:    "old",
				Kind:      "symlink",
				Source:    "modules/old/file",
				LinkDest:  "owned-destination",
				AppliedAt: "2026-07-20T00:00:00Z",
			},
		},
		RunOnce: map[string]planRunOnce{},
	})
	return fixture
}

func (fixture mutationCLIFixture) run(
	t *testing.T,
	openTerminal func() (io.ReadCloser, error),
	commandArgs ...string,
) (string, string, int) {
	t.Helper()
	fixture.setEnvironment(t)
	args := append([]string(nil), commandArgs...)
	args = append(args, "--home", fixture.home, "--repo", fixture.repository)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(args, environment{
		stdout:       &stdout,
		stderr:       &stderr,
		lookupEnv:    os.LookupEnv,
		userHomeDir:  os.UserHomeDir,
		build:        buildinfo.Info{Version: "v0.0.0", Commit: "test", BuildTime: "test"},
		goos:         runtime.GOOS,
		openTerminal: openTerminal,
	})
	return stdout.String(), stderr.String(), code
}

func (fixture mutationCLIFixture) setEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", fixture.realHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fixture.home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(fixture.home, ".local", "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(fixture.home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(fixture.home, ".cache"))
	t.Setenv("DOT_CONFIG", filepath.Join(fixture.home, ".config", "dot", "config.toml"))
	t.Setenv("DOT_REPO", fixture.repository)
}

func runInjectedApply(
	t *testing.T,
	fixture mutationCLIFixture,
	result applyrunner.Result,
	runErr error,
) (string, string, int) {
	t.Helper()
	fixture.setEnvironment(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"apply", "--home", fixture.home, "--repo", fixture.repository}, environment{
		stdout:      &stdout,
		stderr:      &stderr,
		lookupEnv:   os.LookupEnv,
		userHomeDir: os.UserHomeDir,
		build:       buildinfo.Info{Version: "v0.0.0"},
		goos:        runtime.GOOS,
		applyRun: func(applyrunner.Options) (applyrunner.Result, error) {
			return result, runErr
		},
	})
	return stdout.String(), stderr.String(), code
}

func findLineWithPrefix(output, prefix string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func readPlanState(t *testing.T, home string) planStateDocument {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(home, ".local", "state", "dot", "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var document planStateDocument
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	return document
}

func snapshotCLIPath(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(path, "sentinel"))
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	return content
}

func errorsMatchNotExist(left, right error) bool {
	return left == nil && right == nil || os.IsNotExist(left) && os.IsNotExist(right)
}

type closeErrorReadCloser struct {
	io.Reader
	err error
}

func (closer closeErrorReadCloser) Close() error { return closer.err }

type failOnSubstringWriter struct {
	buffer bytes.Buffer
	needle string
	err    error
}

func (writer *failOnSubstringWriter) Write(data []byte) (int, error) {
	if strings.Contains(string(data), writer.needle) {
		return 0, writer.err
	}
	return writer.buffer.Write(data)
}
