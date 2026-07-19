package apply

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mianm12/dotfiles/internal/executor"
	"github.com/mianm12/dotfiles/internal/lock"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/planner"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/mianm12/dotfiles/internal/state"
)

func TestRun_PersistsPostCommitCleanupResultBeforeReturningError(t *testing.T) {
	fixture := newRunSeamFixture(t)
	cleanupErr := errors.New("cleanup failed")
	secondStarted := false
	first := seamLinkAction("~/.first")
	second := seamLinkAction("~/.second")
	operations := fixture.operations(executionPlan{files: []planner.FileAction{first, second}})
	operations.execute = func(_ paths.ControlPlanePaths, action planner.FileAction) (executor.FileResult, error) {
		if action.Target == second.Target {
			secondStarted = true
		}
		return executor.FileResult{StateEffect: action.OnSuccess, TargetMutated: true}, cleanupErr
	}

	result, err := runWithOperations(Options{}, operations)
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("runWithOperations() error = %v, want cleanup error", err)
	}
	if secondStarted || result.Executed != 1 || result.TargetMutations != 1 || !result.StateCommitted {
		t.Fatalf("run result = %#v, secondStarted=%t", result, secondStarted)
	}
	if fixture.loaded.commitCalls != 1 {
		t.Fatalf("CommitState calls = %d, want 1", fixture.loaded.commitCalls)
	}
	entry, ok := fixture.loaded.committed.Entry(first.Target)
	if !ok || entry.AppliedAt() != "2026-07-20T01:02:03Z" {
		t.Fatalf("committed cleanup-success entry = (%#v, %t)", entry, ok)
	}
	if !fixture.session.closed {
		t.Fatal("session was not closed after cleanup error")
	}
}

func TestRun_PartialSuccessCommitsOnceAndJoinsExecutionCommitCloseErrors(t *testing.T) {
	fixture := newRunSeamFixture(t)
	executionErr := errors.New("precondition failed")
	commitErr := errors.New("store failed")
	closeErr := errors.New("unlock failed")
	fixture.loaded.commitErr = commitErr
	fixture.session.closeErr = closeErr
	first := seamLinkAction("~/.first")
	second := seamLinkAction("~/.second")
	operations := fixture.operations(executionPlan{files: []planner.FileAction{first, second}})
	operations.execute = func(_ paths.ControlPlanePaths, action planner.FileAction) (executor.FileResult, error) {
		if action.Target == first.Target {
			return executor.FileResult{StateEffect: action.OnSuccess, TargetMutated: true}, nil
		}
		return executor.FileResult{StateEffect: action.OnFailure}, executionErr
	}

	result, err := runWithOperations(Options{}, operations)
	for _, want := range []error{executionErr, commitErr, closeErr} {
		if !errors.Is(err, want) {
			t.Fatalf("runWithOperations() error = %v, want joined %v", err, want)
		}
	}
	if result.Executed != 2 || result.TargetMutations != 1 || result.StateCommitted {
		t.Fatalf("run result = %#v", result)
	}
	if fixture.loaded.commitCalls != 1 {
		t.Fatalf("CommitState calls = %d, want 1", fixture.loaded.commitCalls)
	}
	if _, ok := fixture.loaded.committed.Entry(first.Target); !ok {
		t.Fatal("successful first entry missing from attempted partial commit")
	}
	if _, ok := fixture.loaded.committed.Entry(second.Target); ok {
		t.Fatal("failed second entry appeared in attempted partial commit")
	}
	if !fixture.session.closed {
		t.Fatal("session close was not attempted")
	}
}

func TestRun_RejectsUnsupportedScopeBeforeExecutor(t *testing.T) {
	fixture := newRunSeamFixture(t)
	executed := false
	operations := fixture.operations(executionPlan{files: []planner.FileAction{
		seamLinkAction("~/.allowed"),
		{
			Verb:   planner.FileAdopt,
			Target: "~/.malformed",
			Desired: planner.Desired{
				Kind: planner.DesiredLink,
			},
		},
	}})
	operations.execute = func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error) {
		executed = true
		return executor.FileResult{}, nil
	}

	result, err := runWithOperations(Options{}, operations)
	if !errors.Is(err, ErrUnsupportedPlan) {
		t.Fatalf("runWithOperations() error = %v, want ErrUnsupportedPlan", err)
	}
	if executed || result.Executed != 0 || fixture.loaded.commitCalls != 0 {
		t.Fatalf("precheck executed=%t result=%#v commitCalls=%d", executed, result, fixture.loaded.commitCalls)
	}
	if !fixture.session.closed {
		t.Fatal("session was not closed after scope rejection")
	}
}

func TestRun_StoreFailureRecoversByAdoptThenConverges(t *testing.T) {
	fixture := newRunIntegrationFixture(t)
	operations := defaultRunOperations()
	storePublishErr := errors.New("injected state publish failure")
	storeCalls := 0
	publishCalls := 0
	operations.begin = func(overrides dotruntime.Overrides) (mutationSession, error) {
		session, err := dotruntime.BeginMutationWithStateStore(
			overrides,
			func(root, path string, snapshot state.Snapshot) error {
				storeCalls++
				return state.StoreWithPublisher(root, path, snapshot, func(prepared, destination string) error {
					publishCalls++
					if root != fixture.stateRoot || destination != fixture.stateFile || path != fixture.stateFile {
						t.Fatalf(
							"Store paths = root %q path %q destination %q",
							root,
							path,
							destination,
						)
					}
					info, statErr := os.Stat(prepared)
					if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
						t.Fatalf("prepared Store file = (%#v, %v), want closed regular 0600", info, statErr)
					}
					data, readErr := os.ReadFile(prepared)
					if readErr != nil {
						t.Fatalf("os.ReadFile(prepared Store file) error = %v", readErr)
					}
					preparedSnapshot, decodeErr := state.Decode(data)
					if decodeErr != nil {
						t.Fatalf("state.Decode(prepared Store file) error = %v", decodeErr)
					}
					for _, key := range []string{"~/config", "~/zshrc"} {
						if _, exists := preparedSnapshot.Entry(key); !exists {
							t.Fatalf("prepared Store Snapshot omits successful entry %q", key)
						}
					}
					return storePublishErr
				})
			},
		)
		if err != nil {
			return nil, err
		}
		return runtimeMutationSession{session: session}, nil
	}

	first, err := runWithOperations(fixture.options(), operations)
	if !errors.Is(err, storePublishErr) || !strings.Contains(err.Error(), "publish state") ||
		!strings.Contains(err.Error(), "commit runtime state") {
		t.Fatalf("first run error = %v, want identifiable Store publish failure", err)
	}
	if first.Executed != 2 || first.TargetMutations != 2 || first.StateCommitted ||
		storeCalls != 1 || publishCalls != 1 {
		t.Fatalf(
			"first run = %#v, storeCalls=%d publishCalls=%d; want two targets and one failed Store publish",
			first,
			storeCalls,
			publishCalls,
		)
	}
	assertRunTargets(t, fixture)
	if _, statErr := os.Lstat(fixture.stateFile); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("failed Store state file Lstat error = %v, want missing", statErr)
	}
	entries, readDirErr := os.ReadDir(fixture.stateRoot)
	if readDirErr != nil {
		t.Fatalf("os.ReadDir(state root) error = %v", readDirErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".state.json-") {
			t.Fatalf("failed Store left temporary file %q", entry.Name())
		}
	}

	second, err := Run(fixture.options())
	if err != nil {
		t.Fatalf("recovery Run() error = %v", err)
	}
	if second.Executed != 2 || second.Adoptions != 2 || second.TargetMutations != 0 || !second.StateCommitted {
		t.Fatalf("recovery result = %#v, want two state-only adopts", second)
	}
	stateBefore, err := os.ReadFile(fixture.stateFile)
	if err != nil {
		t.Fatalf("os.ReadFile(state before converged run) error = %v", err)
	}
	linkBefore, err := os.Lstat(fixture.linkTarget)
	if err != nil {
		t.Fatalf("os.Lstat(link before converged run) error = %v", err)
	}
	scaffoldBefore, err := os.Stat(fixture.scaffoldTarget)
	if err != nil {
		t.Fatalf("os.Stat(scaffold before converged run) error = %v", err)
	}

	third, err := Run(fixture.options())
	if err != nil {
		t.Fatalf("converged Run() error = %v", err)
	}
	if third.Executed != 0 || third.Adoptions != 0 || third.TargetMutations != 0 || third.StateCommitted {
		t.Fatalf("converged result = %#v, want zero mutation/adopt/Store", third)
	}
	stateAfter, err := os.ReadFile(fixture.stateFile)
	if err != nil {
		t.Fatalf("os.ReadFile(state after converged run) error = %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatal("converged run rewrote state bytes")
	}
	linkAfter, _ := os.Lstat(fixture.linkTarget)
	scaffoldAfter, _ := os.Stat(fixture.scaffoldTarget)
	if !os.SameFile(linkBefore, linkAfter) || !os.SameFile(scaffoldBefore, scaffoldAfter) {
		t.Fatal("converged run changed target identities")
	}
}

func TestRun_RealPreconditionFailureCommitsPriorSuccessAndPreservesOldEntry(t *testing.T) {
	fixture := newRunIntegrationFixture(t)
	oldAppliedAt := "2026-07-18T00:00:00Z"
	linkSource := filepath.Join(fixture.repository, "modules", "app", "zshrc")
	writeRunFile(t, fixture.stateFile, `{
  "version": 1,
  "entries": {
    "~/zshrc": {
      "module": "app",
      "kind": "symlink",
      "source": "modules/app/zshrc",
      "link_dest": "`+linkSource+`",
      "applied_at": "`+oldAppliedAt+`"
    }
  },
  "run_once": {
    "app/hooks/old": {
      "hash": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "executed_at": "2026-07-18T00:00:00Z"
    }
  }
}`)
	operations := defaultRunOperations()
	realExecute := operations.execute
	operations.execute = func(control paths.ControlPlanePaths, action planner.FileAction) (executor.FileResult, error) {
		if action.Target == "~/zshrc" {
			if err := os.WriteFile(fixture.linkTarget, []byte("concurrent user data"), 0o600); err != nil {
				t.Fatalf("os.WriteFile(concurrent target) error = %v", err)
			}
		}
		return realExecute(control, action)
	}

	result, err := runWithOperations(fixture.options(), operations)
	if !errors.Is(err, executor.ErrPrecondition) {
		t.Fatalf("runWithOperations() error = %v, want executor.ErrPrecondition", err)
	}
	if result.Executed != 2 || result.TargetMutations != 1 || !result.StateCommitted {
		t.Fatalf("partial result = %#v", result)
	}
	content, readErr := os.ReadFile(fixture.linkTarget)
	if readErr != nil || string(content) != "concurrent user data" {
		t.Fatalf("failed target = (%q, %v), want preserved concurrent data", content, readErr)
	}
	loaded, loadErr := state.Load(fixture.stateFile)
	if loadErr != nil {
		t.Fatalf("state.Load() error = %v", loadErr)
	}
	snapshot, ok := loaded.Snapshot()
	if !ok {
		t.Fatal("committed partial state has no Snapshot")
	}
	if entry, exists := snapshot.Entry("~/config"); !exists || entry.Kind() != state.KindScaffold {
		t.Fatalf("successful scaffold entry = (%#v, %t)", entry, exists)
	}
	if entry, exists := snapshot.Entry("~/zshrc"); !exists || entry.AppliedAt() != oldAppliedAt {
		t.Fatalf("failed link old entry = (%#v, %t), want preserved", entry, exists)
	}
	if _, exists := snapshot.RunOnce("app/hooks/old"); !exists {
		t.Fatal("partial state commit discarded unrelated run_once")
	}
}

func TestRun_HoldsMutationLockThroughExecutionAndClose(t *testing.T) {
	fixture := newRunIntegrationFixture(t)
	operations := defaultRunOperations()
	realExecute := operations.execute
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	operations.execute = func(control paths.ControlPlanePaths, action planner.FileAction) (executor.FileResult, error) {
		once.Do(func() {
			close(entered)
			<-release
		})
		return realExecute(control, action)
	}
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := runWithOperations(fixture.options(), operations)
		done <- outcome{result: result, err: err}
	}()
	<-entered
	owner, err := lock.Acquire(fixture.stateRoot, filepath.Join(fixture.stateRoot, "lock"))
	if owner != nil || !errors.Is(err, lock.ErrBusy) {
		t.Fatalf("concurrent lock.Acquire() = (%#v, %v), want ErrBusy", owner, err)
	}
	close(release)
	got := <-done
	if got.err != nil || !got.result.StateCommitted {
		t.Fatalf("runWithOperations() = (%#v, %v)", got.result, got.err)
	}
	owner, err = lock.Acquire(fixture.stateRoot, filepath.Join(fixture.stateRoot, "lock"))
	if err != nil {
		t.Fatalf("lock.Acquire() after Run error = %v", err)
	}
	if err := owner.Release(); err != nil {
		t.Fatalf("lock release after Run error = %v", err)
	}
}

type runSeamFixture struct {
	session *fakeMutationSession
	loaded  *fakeLoadedMutation
}

func newRunSeamFixture(t *testing.T) runSeamFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	config := filepath.Join(home, ".config", "dot", "config.toml")
	for _, directory := range []string{home, repository} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	control, err := paths.ResolveControlPlanePaths(home, repository, config)
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}
	loadedState, err := state.Load(filepath.Join(root, "missing-state.json"))
	if err != nil {
		t.Fatalf("state.Load(missing) error = %v", err)
	}
	loaded := &fakeLoadedMutation{baselineState: loadedState, controlPaths: control}
	return runSeamFixture{
		session: &fakeMutationSession{loaded: loaded},
		loaded:  loaded,
	}
}

func (fixture runSeamFixture) operations(plan executionPlan) runOperations {
	return runOperations{
		begin: func(dotruntime.Overrides) (mutationSession, error) { return fixture.session, nil },
		plan:  func(dotruntime.LoadedInputs, planner.ApplyScopeOptions) (executionPlan, error) { return plan, nil },
		execute: func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error) {
			return executor.FileResult{}, nil
		},
		now: func() time.Time { return time.Date(2026, 7, 20, 1, 2, 3, 0, time.UTC) },
	}
}

type fakeMutationSession struct {
	loaded   *fakeLoadedMutation
	loadErr  error
	closeErr error
	closed   bool
}

func (session *fakeMutationSession) load(string) (loadedMutation, error) {
	return session.loaded, session.loadErr
}

func (session *fakeMutationSession) close() error {
	session.closed = true
	return session.closeErr
}

type fakeLoadedMutation struct {
	baselineState state.Loaded
	controlPaths  paths.ControlPlanePaths
	commitErr     error
	commitCalls   int
	committed     state.Snapshot
}

func (mutation *fakeLoadedMutation) inputs() dotruntime.LoadedInputs {
	return dotruntime.LoadedInputs{}
}

func (mutation *fakeLoadedMutation) baseline() state.Loaded { return mutation.baselineState }

func (mutation *fakeLoadedMutation) control() paths.ControlPlanePaths {
	return mutation.controlPaths
}

func (mutation *fakeLoadedMutation) commit(snapshot state.Snapshot) error {
	mutation.commitCalls++
	mutation.committed = snapshot
	return mutation.commitErr
}

func seamLinkAction(target string) planner.FileAction {
	source := "/repo/modules/app/" + filepath.Base(target)
	targetPath := "/home/" + filepath.Base(target)
	return planner.FileAction{
		Verb:   planner.FileCreateLink,
		Target: target,
		Reason: planner.FileReasonTargetMissing,
		Desired: planner.Desired{
			Module:     "app",
			Source:     filepath.Base(target),
			SourcePath: source,
			Target:     target,
			TargetPath: targetPath,
			Kind:       planner.DesiredLink,
		},
		Precondition: planner.Precondition{
			TargetPath:           targetPath,
			Leaf:                 planner.LeafCondition{Kind: planner.LeafMissing},
			SourcePath:           source,
			RequireRegularSource: true,
		},
		OnSuccess: planner.StateEffect{
			Kind: planner.StateUpsert,
			Key:  target,
			Entry: planner.HistoricalState{
				Key:      target,
				Module:   "app",
				Kind:     planner.StateSymlink,
				Source:   "modules/app/" + filepath.Base(target),
				LinkDest: source,
			},
		},
		OnFailure: planner.StateEffect{Kind: planner.StatePreserve},
	}
}

func seamLinkAdoptAction(target string) planner.FileAction {
	action := seamLinkAction(target)
	action.Verb = planner.FileAdopt
	action.Reason = planner.FileReasonStateMetadata
	action.Precondition.Leaf = planner.LeafCondition{
		Kind:     planner.LeafExactSymlink,
		LinkDest: action.Desired.SourcePath,
	}
	action.Precondition.SourcePath = ""
	action.Precondition.RequireRegularSource = false
	return action
}

type runIntegrationFixture struct {
	root           string
	home           string
	repository     string
	stateRoot      string
	stateFile      string
	linkTarget     string
	scaffoldTarget string
}

func newRunIntegrationFixture(t *testing.T) runIntegrationFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	isolateRunMutationEnvironment(t, root, home, repository)
	writeRunFile(t, filepath.Join(home, ".config", "dot", "config.toml"), "profile = \"all\"\n")
	writeRunFile(t, filepath.Join(repository, "dot.toml"), `requires = ">=0.0.0"
[profiles]
all = ["app"]
`)
	writeRunFile(t, filepath.Join(repository, "modules", "app", "dot.toml"), `target = "~"
[files."config.template"]
kind = "scaffold"
mode = "0600"
`)
	writeRunFile(t, filepath.Join(repository, "modules", "app", "zshrc"), "link source\n")
	writeRunFile(t, filepath.Join(repository, "modules", "app", "config.template"), "scaffold content\n")
	return runIntegrationFixture{
		root:           root,
		home:           home,
		repository:     repository,
		stateRoot:      filepath.Join(home, ".local", "state", "dot"),
		stateFile:      filepath.Join(home, ".local", "state", "dot", "state.json"),
		linkTarget:     filepath.Join(home, "zshrc"),
		scaffoldTarget: filepath.Join(home, "config"),
	}
}

type runPathMetadata struct {
	exists  bool
	mode    fs.FileMode
	size    int64
	modTime time.Time
	info    fs.FileInfo
}

func isolateRunMutationEnvironment(t *testing.T, root, home, repository string) {
	t.Helper()
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}
	if realHome == "" || !filepath.IsAbs(realHome) || pathsContain(root, realHome) {
		t.Fatalf("real HOME %q is not a distinct absolute path outside fixture root %q", realHome, root)
	}
	realPaths := []string{
		realHome,
		filepath.Join(realHome, ".config", "dot", "config.toml"),
		filepath.Join(realHome, ".local", "state", "dot"),
		filepath.Join(realHome, ".local", "state", "dot", "state.json"),
		filepath.Join(realHome, ".local", "state", "dot", "lock"),
		filepath.Join(realHome, ".dotfiles"),
		filepath.Join(realHome, "config"),
		filepath.Join(realHome, "zshrc"),
	}
	before := make(map[string]runPathMetadata, len(realPaths))
	for _, path := range realPaths {
		before[path] = snapshotRunPathMetadata(t, path)
	}
	t.Cleanup(func() {
		for _, path := range realPaths {
			assertRunPathMetadataUnchanged(t, path, before[path], snapshotRunPathMetadata(t, path))
		}
	})

	environment := map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_STATE_HOME":  filepath.Join(home, ".local", "state"),
		"XDG_DATA_HOME":   filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
		"XDG_RUNTIME_DIR": filepath.Join(root, "xdg-runtime"),
		"DOT_CONFIG":      filepath.Join(home, ".config", "dot", "config.toml"),
		"DOT_REPO":        repository,
	}
	for key, value := range environment {
		t.Setenv(key, value)
		if !pathsContain(root, value) {
			t.Fatalf("isolated %s=%q is outside fixture root %q", key, value, root)
		}
	}
	if os.Getenv("HOME") != home ||
		os.Getenv("DOT_CONFIG") != filepath.Join(home, ".config", "dot", "config.toml") ||
		os.Getenv("DOT_REPO") != repository {
		t.Fatal("isolated environment contradicts runtime HOME/repository/config paths")
	}
}

func snapshotRunPathMetadata(t *testing.T, path string) runPathMetadata {
	t.Helper()
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return runPathMetadata{}
	}
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", path, err)
	}
	return runPathMetadata{
		exists:  true,
		mode:    info.Mode(),
		size:    info.Size(),
		modTime: info.ModTime(),
		info:    info,
	}
}

func assertRunPathMetadataUnchanged(t *testing.T, path string, before, after runPathMetadata) {
	t.Helper()
	if before.exists != after.exists {
		t.Errorf("real HOME path %q existence changed: before=%t after=%t", path, before.exists, after.exists)
		return
	}
	if !before.exists {
		return
	}
	if !os.SameFile(before.info, after.info) || before.mode != after.mode ||
		before.size != after.size || !before.modTime.Equal(after.modTime) {
		t.Errorf(
			"real HOME path %q metadata changed: before=(%v,%d,%v) after=(%v,%d,%v)",
			path,
			before.mode,
			before.size,
			before.modTime,
			after.mode,
			after.size,
			after.modTime,
		)
	}
}

func pathsContain(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !filepath.IsAbs(relative) &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (fixture runIntegrationFixture) options() Options {
	return Options{
		Runtime: dotruntime.Overrides{
			Home:       dotruntime.Override{Value: fixture.home, Set: true},
			Repository: dotruntime.Override{Value: fixture.repository, Set: true},
		},
		CLIVersion: "dev",
		NoPrune:    true,
	}
}

func assertRunTargets(t *testing.T, fixture runIntegrationFixture) {
	t.Helper()
	link, err := os.Readlink(fixture.linkTarget)
	if err != nil || link != filepath.Join(fixture.repository, "modules", "app", "zshrc") {
		t.Fatalf("link target = (%q, %v)", link, err)
	}
	content, err := os.ReadFile(fixture.scaffoldTarget)
	if err != nil || string(content) != "scaffold content\n" {
		t.Fatalf("scaffold target = (%q, %v)", content, err)
	}
	info, err := os.Stat(fixture.scaffoldTarget)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("scaffold mode = (%v, %v), want 0600", info, err)
	}
}

func writeRunFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
