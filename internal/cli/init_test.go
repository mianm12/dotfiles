package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	applyrunner "github.com/mianm12/dotfiles/internal/apply"
	"github.com/mianm12/dotfiles/internal/buildinfo"
	"github.com/mianm12/dotfiles/internal/config"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
)

func TestResolveInitDecisions_YesUsesProfileWithoutTerminal(t *testing.T) {
	fixture := newInitDecisionFixture(t, `[profiles]
mac = []
`, "")
	inputs := fixture.prepare(t, dotruntime.Override{Value: "mac", Set: true})
	before := snapshotCLITree(t, fixture.root)
	opened := false
	decisions, err := resolveInitDecisions(
		inputs,
		true,
		func() (io.ReadWriteCloser, error) {
			opened = true
			return nil, errors.New("must not open")
		},
	)
	if err != nil {
		t.Fatalf("resolveInitDecisions() error = %v", err)
	}
	if opened {
		t.Fatal("--yes with unambiguous values opened a user terminal")
	}
	if !decisions.apply {
		t.Fatal("--yes did not select immediate apply")
	}
	candidate, err := inputs.BuildCandidate(decisions.selection)
	if err != nil {
		t.Fatalf("BuildCandidate() error = %v", err)
	}
	machine := candidate.Machine()
	if machine.Profile != "mac" {
		t.Fatalf("candidate machine = %#v", machine)
	}
	fixture.assertUnchanged(t, before)
}

func TestResolveInitDecisions_NoTerminalLeavesAllMutationPathsMissing(t *testing.T) {
	fixture := newInitDecisionFixture(t, `[profiles]
linux = []
mac = []
`, "")
	inputs := fixture.prepare(t, dotruntime.Override{})
	before := snapshotCLITree(t, fixture.root)
	_, err := resolveInitDecisions(inputs, true, func() (io.ReadWriteCloser, error) {
		return nil, os.ErrNotExist
	})
	if err == nil || !strings.Contains(err.Error(), "open user terminal") {
		t.Fatalf("resolveInitDecisions() error = %v, want no terminal", err)
	}
	fixture.assertUnchanged(t, before)
	for _, path := range []string{fixture.config, fixture.state, fixture.lock} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("mutation path %q exists or cannot be inspected: %v", path, err)
		}
	}
}

func TestResolveInitDecisions_InteractiveUsesTTYAndRepairsStaleProfile(t *testing.T) {
	fixture := newInitDecisionFixture(t, `[profiles]
linux = []
mac = []
`, `profile = "retired"`)
	inputs := fixture.prepare(t, dotruntime.Override{})
	before := snapshotCLITree(t, fixture.root)
	terminal := newInitTestTerminal("linux\nn\n")
	decisions, err := resolveInitDecisions(inputs, false, func() (io.ReadWriteCloser, error) {
		return terminal, nil
	})
	if err != nil {
		t.Fatalf("resolveInitDecisions() error = %v", err)
	}
	if decisions.apply {
		t.Fatal("interactive no answer selected apply")
	}
	candidate, err := inputs.BuildCandidate(decisions.selection)
	if err != nil {
		t.Fatalf("BuildCandidate() error = %v", err)
	}
	machine := candidate.Machine()
	if machine.Profile != "linux" {
		t.Fatalf("candidate machine = %#v", machine)
	}
	for _, want := range []string{"Profiles:\n  linux\n  mac\n", "Profile: ", "Apply now? [Y/n] "} {
		if !strings.Contains(terminal.written.String(), want) {
			t.Fatalf("terminal output = %q, want %q", terminal.written.String(), want)
		}
	}
	if !terminal.closed {
		t.Fatal("init terminal was not closed")
	}
	fixture.assertUnchanged(t, before)
}

func TestInit_NoTerminalWithIncompleteInputsIsZeroWrite(t *testing.T) {
	fixture := newInitDecisionFixture(t, `[profiles]
linux = []
mac = []
`, "")
	before := snapshotCLITree(t, fixture.root)
	stdout, stderr, code := fixture.run(
		t,
		"command stdin must not be used\n",
		func() (io.ReadWriteCloser, error) { return nil, os.ErrNotExist },
		"init", "--yes",
	)
	if code != exitError || stdout != "" || !strings.Contains(stderr, "open user terminal") {
		t.Fatalf("init no TTY = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	fixture.assertUnchanged(t, before)
	for _, path := range []string{fixture.config, fixture.state, fixture.lock} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("mutation path %q exists or cannot be inspected: %v", path, err)
		}
	}
}

func TestInit_NoTerminalWithoutApplyDecisionIsZeroWrite(t *testing.T) {
	fixture := newInitDecisionFixture(t, `[profiles]
mac = []
`, "")
	before := snapshotCLITree(t, fixture.root)
	stdout, stderr, code := fixture.run(
		t,
		"command stdin must not be used\n",
		func() (io.ReadWriteCloser, error) { return nil, os.ErrNotExist },
		"init", "--profile", "mac",
	)
	if code != exitError || stdout != "" || !strings.Contains(stderr, "open user terminal") {
		t.Fatalf("init without apply decision = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	fixture.assertUnchanged(t, before)
	for _, path := range []string{fixture.config, fixture.state, fixture.lock} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("mutation path %q exists or cannot be inspected: %v", path, err)
		}
	}
}

func TestInit_ConfigOnlyPersistsRepoAndProfile(t *testing.T) {
	fixture := newInitDecisionFixture(t, `[profiles]
mac = []
`, "")
	terminal := newInitTestTerminal("n\n")
	stdout, stderr, code := fixture.run(
		t,
		"must not be read\n",
		func() (io.ReadWriteCloser, error) { return terminal, nil },
		"init", "--profile", "mac",
	)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "config  "+fixture.config+"  (updated)") {
		t.Fatalf("config-only init = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	assertInitContextOrder(t, stdout, fixture.repo, "mac", "config  "+fixture.config)
	snapshot, err := config.LoadSnapshot(fixture.config)
	if err != nil {
		t.Fatalf("LoadSnapshot(config) error = %v", err)
	}
	machine := snapshot.Machine()
	if machine.Profile != "mac" || machine.Repo == nil || *machine.Repo != fixture.repo {
		t.Fatalf("machine config = %#v", machine)
	}
	if snapshot.Precondition().Mode() != 0o600 {
		t.Fatalf("config mode = %04o, want 0600", snapshot.Precondition().Mode())
	}
	if _, err := os.Lstat(fixture.state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config-only init state error = %v, want missing", err)
	}
	if !strings.Contains(terminal.written.String(), "Apply now? [Y/n] ") {
		t.Fatalf("terminal output = %q", terminal.written.String())
	}

	if err := os.Unsetenv("DOT_REPO"); err != nil {
		t.Fatalf("os.Unsetenv(DOT_REPO) error = %v", err)
	}
	prepared, err := dotruntime.PrepareInit(dotruntime.Overrides{
		Home: dotruntime.Override{Value: fixture.home, Set: true},
	})
	if err != nil {
		t.Fatalf("PrepareInit(after removing repo override) error = %v", err)
	}
	if got := prepared.Context().Control().RepositoryPath(); got != fixture.repo {
		t.Fatalf("persisted repository = %q, want %q", got, fixture.repo)
	}
}

func TestInit_YesRunsNestedApplyHooksAndSecondRunConverges(t *testing.T) {
	fixture := newHookMutationCLIFixture(t)
	realHomeBefore := snapshotCLIPath(t, fixture.realHome)
	stdout, stderr, code := runMutationInit(
		t,
		fixture,
		"from-init-stdin\n",
		nil,
		nil,
		"init", "--profile", "all", "--yes",
	)
	if code != exitOK || !strings.Contains(stdout, "config  ") ||
		!strings.Contains(stdout, "hook-out:from-init-stdin") ||
		!strings.Contains(stdout, "run-hook  alpha/hooks/setup.sh  (succeeded)") ||
		!strings.Contains(stderr, "hook-err:from-init-stdin") {
		t.Fatalf("first init apply = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	assertInitContextOrder(t, stdout, fixture.repository, "all", "config  ", "link  ", "run-hook  ")
	logPath := filepath.Join(fixture.home, "hook-log")
	if content, err := os.ReadFile(logPath); err != nil || string(content) != "from-init-stdin\n" {
		t.Fatalf("hook log = %q, %v", content, err)
	}
	afterFirst := snapshotCLITree(t, fixture.root)

	stdout, stderr, code = runMutationInit(
		t,
		fixture,
		"must-not-be-read\n",
		nil,
		nil,
		"init", "--profile", "all", "--yes",
	)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "(unchanged)") ||
		!strings.HasSuffix(stdout, "Already up to date.\n") || strings.Contains(stdout, "hook-out:") {
		t.Fatalf("second init apply = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	assertInitContextOrder(t, stdout, fixture.repository, "all", "config  ", "Already up to date.")
	if afterSecond := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(afterSecond, afterFirst) {
		t.Fatalf("second init changed synthetic tree\nfirst=%v\nsecond=%v", afterFirst, afterSecond)
	}
	if content, err := os.ReadFile(logPath); err != nil || string(content) != "from-init-stdin\n" {
		t.Fatalf("second init reran hook: %q, %v", content, err)
	}
	if realHomeAfter := snapshotCLIPath(t, fixture.realHome); !reflect.DeepEqual(realHomeAfter, realHomeBefore) {
		t.Fatalf("init changed real HOME sentinel: before=%v after=%v", realHomeBefore, realHomeAfter)
	}
}

func TestInit_ConfigCommitSurvivesCorruptStateApplyFailure(t *testing.T) {
	fixture := newMutationCLIFixture(t)
	statePath := filepath.Join(fixture.home, ".local", "state", "dot", "state.json")
	writeCLIFile(t, statePath, "{")
	stdout, stderr, code := runMutationInit(
		t,
		fixture,
		"",
		nil,
		nil,
		"init", "--profile", "all", "--yes",
	)
	if code != exitError || !strings.Contains(stdout, "config  ") ||
		!strings.Contains(stderr, "load apply mutation inputs") {
		t.Fatalf("corrupt-state init = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	snapshot, err := config.LoadSnapshot(filepath.Join(fixture.home, ".config", "dot", "config.toml"))
	if err != nil {
		t.Fatalf("LoadSnapshot(config) error = %v", err)
	}
	machine := snapshot.Machine()
	if machine.Repo == nil || *machine.Repo != fixture.repository || machine.Profile != "all" {
		t.Fatalf("committed config after state failure = %#v", machine)
	}
	if content, err := os.ReadFile(statePath); err != nil || string(content) != "{" {
		t.Fatalf("corrupt state after init = %q, %v", content, err)
	}
	if _, err := os.Lstat(filepath.Join(fixture.home, "alpha", "file")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target after corrupt state error = %v, want missing", err)
	}
}

func TestInit_PruneConfirmationAndYesAuthorization(t *testing.T) {
	t.Run("interactive refusal", func(t *testing.T) {
		fixture := newWholeModulePruneCLIFixture(t)
		initTTY := newInitTestTerminal("\n")
		pruneTTY := func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("n\n")), nil
		}
		stdout, stderr, code := runMutationInit(
			t,
			fixture,
			"command stdin must not answer init\n",
			func() (io.ReadWriteCloser, error) { return initTTY, nil },
			pruneTTY,
			"init", "--profile", "all",
		)
		if code != exitActionable || !strings.Contains(stdout, "config  ") ||
			!strings.Contains(stderr, "Remove orphaned modules? [y/N]") {
			t.Fatalf("interactive prune refusal = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
		if _, err := os.Lstat(filepath.Join(fixture.home, "old")); err != nil {
			t.Fatalf("refused prune removed orphan: %v", err)
		}
	})

	t.Run("yes confirms prune without terminal", func(t *testing.T) {
		fixture := newWholeModulePruneCLIFixture(t)
		opened := false
		failInitTTY := func() (io.ReadWriteCloser, error) {
			opened = true
			return nil, errors.New("must not open")
		}
		failPruneTTY := func() (io.ReadCloser, error) {
			opened = true
			return nil, errors.New("must not open")
		}
		stdout, stderr, code := runMutationInit(
			t,
			fixture,
			"",
			failInitTTY,
			failPruneTTY,
			"init", "--profile", "all", "--yes",
		)
		if code != exitOK || stderr != "" || !strings.Contains(stdout, "prune  ~/old") {
			t.Fatalf("init --yes prune = stdout %q, stderr %q, exit %d", stdout, stderr, code)
		}
		if opened {
			t.Fatal("init --yes opened a terminal")
		}
		if _, err := os.Lstat(filepath.Join(fixture.home, "old")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("init --yes orphan error = %v, want missing", err)
		}
	})
}

func TestInit_YesPreservesConflicts(t *testing.T) {
	fixture := newMutationCLIFixture(t)
	target := filepath.Join(fixture.home, "alpha", "file")
	writeCLIFile(t, target, "user-owned\n")
	stdout, stderr, code := runMutationInit(
		t,
		fixture,
		"",
		nil,
		nil,
		"init", "--profile", "all", "--yes",
	)
	if code != exitConflict || stderr != "" || !strings.Contains(stdout, "CONFLICT  ~/alpha/file") {
		t.Fatalf("init --yes conflict = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "user-owned\n" {
		t.Fatalf("init --yes changed conflict target: %q, %v", content, err)
	}
}

func TestFinishInitClose_PreservesExitCodesAndAggregatesErrors(t *testing.T) {
	closeErr := errors.New("unlock failed")
	runErr := errors.New("apply failed")
	for _, code := range []int{exitActionable, exitConflict} {
		result := finishInitClose(commandExit(code), nil)
		var requested commandExitError
		if !errors.As(result, &requested) || requested.code != code {
			t.Fatalf("finishInitClose(exit %d, nil) = %v, want same command exit", code, result)
		}
		result = finishInitClose(commandExit(code), closeErr)
		if errors.As(result, &requested) || !errors.Is(result, closeErr) ||
			!strings.Contains(result.Error(), "command exit "+strconv.Itoa(code)) {
			t.Fatalf("finishInitClose(exit %d, close) = %v, want close error without commandExitError", code, result)
		}
	}

	result := finishInitClose(runErr, closeErr)
	if !errors.Is(result, runErr) || !errors.Is(result, closeErr) {
		t.Fatalf("finishInitClose(run error, close error) = %v, want both causes", result)
	}

	wrappedExit := fmt.Errorf("wrapped action result: %w", commandExit(exitActionable))
	result = finishInitClose(wrappedExit, closeErr)
	var requested commandExitError
	if !errors.Is(result, wrappedExit) || !errors.Is(result, closeErr) ||
		!errors.As(result, &requested) || requested.code != exitActionable {
		t.Fatalf("finishInitClose(wrapped exit, close error) = %v, want both wrapped causes", result)
	}
}

func TestInit_CloseFailureOverridesActionableAndConflictExit(t *testing.T) {
	closeErr := errors.New("injected init unlock failure")
	for _, test := range []struct {
		name      string
		fixture   func(*testing.T) mutationCLIFixture
		initTTY   func() (io.ReadWriteCloser, error)
		pruneTTY  func() (io.ReadCloser, error)
		prepare   func(*testing.T, mutationCLIFixture)
		args      []string
		wantPrior int
	}{
		{
			name:    "actionable prune refusal",
			fixture: newWholeModulePruneCLIFixture,
			initTTY: func() (io.ReadWriteCloser, error) { return newInitTestTerminal("\n"), nil },
			pruneTTY: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("n\n")), nil
			},
			args:      []string{"init", "--profile", "all"},
			wantPrior: exitActionable,
		},
		{
			name:    "conflict",
			fixture: newMutationCLIFixture,
			prepare: func(t *testing.T, fixture mutationCLIFixture) {
				writeCLIFile(t, filepath.Join(fixture.home, "alpha", "file"), "user-owned\n")
			},
			args:      []string{"init", "--profile", "all", "--yes"},
			wantPrior: exitConflict,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := test.fixture(t)
			if test.prepare != nil {
				test.prepare(t, fixture)
			}
			stdout, stderr, code := runMutationInitWithClose(
				t,
				fixture,
				"",
				test.initTTY,
				test.pruneTTY,
				func(session *dotruntime.InitSession) error {
					return errors.Join(session.Close(), closeErr)
				},
				test.args...,
			)
			if code != exitError || !strings.Contains(stderr, closeErr.Error()) ||
				!strings.Contains(stderr, "command exit "+strconv.Itoa(test.wantPrior)) {
				t.Fatalf("init close failure = stdout %q, stderr %q, exit %d", stdout, stderr, code)
			}
			assertInitLockReleased(t, fixture)
		})
	}
}

func TestInit_ContextWriteFailureStopsBeforeConfigOutputAndNestedApply(t *testing.T) {
	fixture := newMutationCLIFixture(t)
	fixture.setEnvironment(t)
	writeErr := errors.New("injected init context write failure")
	stdout := &failOnSubstringWriter{needle: "repo=", err: writeErr}
	var stderr bytes.Buffer
	applyCalled := false
	code := run(
		[]string{"init", "--profile", "all", "--yes", "--home", fixture.home, "--repo", fixture.repository},
		environment{
			stdin:       strings.NewReader(""),
			stdout:      stdout,
			stderr:      &stderr,
			lookupEnv:   os.LookupEnv,
			userHomeDir: os.UserHomeDir,
			build:       buildinfo.Info{Version: "v0.0.0", Commit: "test", BuildTime: "test"},
			goos:        runtime.GOOS,
			applyNested: func(applyrunner.Options, *dotruntime.MutationSession) (applyrunner.Result, error) {
				applyCalled = true
				return applyrunner.Result{}, errors.New("must not run nested apply")
			},
		},
	)
	if code != exitError || !strings.Contains(stderr.String(), "write init context") ||
		!strings.Contains(stderr.String(), writeErr.Error()) {
		t.Fatalf("context write failure = stdout %q, stderr %q, exit %d", stdout.buffer.String(), stderr.String(), code)
	}
	if stdout.buffer.Len() != 0 || applyCalled {
		t.Fatalf("context write failure emitted stdout %q or called apply=%t", stdout.buffer.String(), applyCalled)
	}
	snapshot, err := config.LoadSnapshot(filepath.Join(fixture.home, ".config", "dot", "config.toml"))
	if err != nil {
		t.Fatalf("LoadSnapshot(config) error = %v", err)
	}
	machine := snapshot.Machine()
	if machine.Profile != "all" || machine.Repo == nil || *machine.Repo != fixture.repository {
		t.Fatalf("config after context failure = %#v, want committed profile/repo", machine)
	}
	if _, err := os.Lstat(filepath.Join(fixture.home, "alpha", "file")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("context failure target error = %v, want missing", err)
	}
	assertInitLockReleased(t, fixture)
}

type initDecisionFixture struct {
	root     string
	home     string
	realHome string
	repo     string
	config   string
	state    string
	lock     string
}

func newInitDecisionFixture(t *testing.T, manifest, machine string) initDecisionFixture {
	t.Helper()
	root := t.TempDir()
	fixture := initDecisionFixture{
		root:     root,
		home:     filepath.Join(root, "synthetic-home"),
		realHome: filepath.Join(root, "real-home-sentinel"),
		repo:     filepath.Join(root, "repo"),
	}
	fixture.config = filepath.Join(fixture.home, ".config", "dot", "config.toml")
	fixture.state = filepath.Join(fixture.home, ".local", "state", "dot", "state.json")
	fixture.lock = filepath.Join(fixture.home, ".local", "state", "dot", "lock")
	writeCLIFile(t, filepath.Join(fixture.repo, "dot.toml"), manifest)
	writeCLIFile(t, filepath.Join(fixture.realHome, "sentinel"), "unchanged\n")
	if machine != "" {
		writeCLIFile(t, fixture.config, machine)
		if err := os.Chmod(fixture.config, 0o600); err != nil {
			t.Fatalf("os.Chmod(config) error = %v", err)
		}
	}
	t.Setenv("HOME", fixture.realHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fixture.home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(fixture.home, ".local", "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(fixture.home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(fixture.home, ".cache"))
	t.Setenv("DOT_CONFIG", fixture.config)
	t.Setenv("DOT_REPO", fixture.repo)
	return fixture
}

func (fixture initDecisionFixture) prepare(t *testing.T, profile dotruntime.Override) dotruntime.InitInputs {
	t.Helper()
	inputs, err := dotruntime.PrepareInit(dotruntime.Overrides{
		Home:       dotruntime.Override{Value: fixture.home, Set: true},
		Repository: dotruntime.Override{Value: fixture.repo, Set: true},
		Profile:    profile,
	})
	if err != nil {
		t.Fatalf("PrepareInit() error = %v", err)
	}
	return inputs
}

func (fixture initDecisionFixture) run(
	t *testing.T,
	stdin string,
	openInitTTY func() (io.ReadWriteCloser, error),
	args ...string,
) (string, string, int) {
	t.Helper()
	commandArgs := append([]string(nil), args...)
	commandArgs = append(commandArgs, "--home", fixture.home, "--repo", fixture.repo)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(commandArgs, environment{
		stdin:        strings.NewReader(stdin),
		stdout:       &stdout,
		stderr:       &stderr,
		lookupEnv:    os.LookupEnv,
		userHomeDir:  os.UserHomeDir,
		openInitTTY:  openInitTTY,
		openTerminal: func() (io.ReadCloser, error) { return nil, os.ErrNotExist },
		build:        buildinfo.Info{Version: "v0.0.0", Commit: "test", BuildTime: "test"},
		goos:         runtime.GOOS,
	})
	return stdout.String(), stderr.String(), code
}

func (fixture initDecisionFixture) assertUnchanged(t *testing.T, before map[string]cliTreeEntry) {
	t.Helper()
	if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("init decision phase changed synthetic tree\nbefore=%v\nafter=%v", before, after)
	}
	content, err := os.ReadFile(filepath.Join(fixture.realHome, "sentinel"))
	if err != nil || string(content) != "unchanged\n" {
		t.Fatalf("real HOME sentinel = %q, %v", content, err)
	}
}

type initTestTerminal struct {
	input   *strings.Reader
	written bytes.Buffer
	closed  bool
}

func newInitTestTerminal(input string) *initTestTerminal {
	return &initTestTerminal{input: strings.NewReader(input)}
}

func (terminal *initTestTerminal) Read(buffer []byte) (int, error) {
	return terminal.input.Read(buffer)
}

func (terminal *initTestTerminal) Write(buffer []byte) (int, error) {
	return terminal.written.Write(buffer)
}

func (terminal *initTestTerminal) Close() error {
	terminal.closed = true
	return nil
}

func runMutationInit(
	t *testing.T,
	fixture mutationCLIFixture,
	stdin string,
	openInitTTY func() (io.ReadWriteCloser, error),
	openPruneTTY func() (io.ReadCloser, error),
	args ...string,
) (string, string, int) {
	return runMutationInitWithClose(t, fixture, stdin, openInitTTY, openPruneTTY, nil, args...)
}

func runMutationInitWithClose(
	t *testing.T,
	fixture mutationCLIFixture,
	stdin string,
	openInitTTY func() (io.ReadWriteCloser, error),
	openPruneTTY func() (io.ReadCloser, error),
	closeSession closeInit,
	args ...string,
) (string, string, int) {
	t.Helper()
	fixture.setEnvironment(t)
	commandArgs := append([]string(nil), args...)
	commandArgs = append(commandArgs, "--home", fixture.home, "--repo", fixture.repository)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(commandArgs, environment{
		stdin:        strings.NewReader(stdin),
		stdout:       &stdout,
		stderr:       &stderr,
		lookupEnv:    os.LookupEnv,
		userHomeDir:  os.UserHomeDir,
		openInitTTY:  openInitTTY,
		openTerminal: openPruneTTY,
		closeInit:    closeSession,
		build:        buildinfo.Info{Version: "v0.0.0", Commit: "test", BuildTime: "test"},
		goos:         runtime.GOOS,
	})
	return stdout.String(), stderr.String(), code
}

func assertInitLockReleased(t *testing.T, fixture mutationCLIFixture) {
	t.Helper()
	session, err := dotruntime.BeginMutation(dotruntime.Overrides{
		Home:       dotruntime.Override{Value: fixture.home, Set: true},
		Repository: dotruntime.Override{Value: fixture.repository, Set: true},
	})
	if err != nil {
		t.Fatalf("BeginMutation() after init close error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("MutationSession.Close() after init close error = %v", err)
	}
}

func assertInitContextOrder(t *testing.T, stdout, repository, profile string, later ...string) {
	t.Helper()
	contextLine := fmt.Sprintf("repo=%s profile=%s os=%s\n", repository, profile, runtime.GOOS)
	if strings.Count(stdout, contextLine) != 1 {
		t.Fatalf("init stdout context count = %d in %q, want exactly one %q", strings.Count(stdout, contextLine), stdout, contextLine)
	}
	contextIndex := strings.Index(stdout, contextLine)
	for _, marker := range later {
		markerIndex := strings.Index(stdout, marker)
		if markerIndex < 0 || contextIndex >= markerIndex {
			t.Fatalf("init stdout = %q, want context before %q", stdout, marker)
		}
	}
}
