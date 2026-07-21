package add

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mianm12/dotfiles/internal/apply"
	"github.com/mianm12/dotfiles/internal/paths"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/mianm12/dotfiles/internal/state"
)

func TestRun_LinkCommitsTargetAndState(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o640)
	options := fixture.runOptions(target)

	result, err := Run(options)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Valid() || result.Attempts() != 1 || result.SourcePublications() != 1 ||
		result.TargetCommits() != 1 || !result.StateCommitted() || result.Outcomes()[0].Status != OutcomeSucceeded {
		t.Fatalf("Run() result = %#v", result)
	}
	item := result.Plan().Items()[0]
	if link, readErr := os.Readlink(target); readErr != nil || link != item.SourcePath() {
		t.Fatalf("target Readlink() = (%q, %v), want %q", link, readErr, item.SourcePath())
	}
	loaded := fixture.load(t).State()
	snapshot, ok := loaded.Snapshot()
	if !ok {
		t.Fatal("state snapshot missing after successful Run")
	}
	entry, ok := snapshot.Entry(item.Target())
	if !ok || entry.Kind() != state.KindSymlink || entry.Module() != "app" ||
		entry.Source() != "modules/app/config" || entry.LinkDest() != item.SourcePath() {
		t.Fatalf("state entry = (%#v, %t)", entry, ok)
	}
}

func TestRun_RebuildsWholePreflightBeforeAnyExecution(t *testing.T) {
	fixture, item := newLinkItemFixture(t)
	seam := newRunSeam(t, fixture, []ItemPlan{item})
	injected := errors.New("locked preflight failed")
	seam.operations.preflight = func(dotruntime.LoadedInputs, Request) (BatchPlan, error) {
		return BatchPlan{}, injected
	}
	executed := false
	seam.operations.execute = func(paths.ControlPlanePaths, ItemPlan) (linkItemResult, error) {
		executed = true
		return linkItemResult{}, nil
	}

	result, err := runWithOperations(RunOptions{Request: Request{Mode: ModeLink}}, seam.operations)
	if !errors.Is(err, injected) || result.Valid() || executed || seam.loaded.commitCalls != 0 {
		t.Fatalf("runWithOperations() = (%#v, %v), executed=%t commits=%d", result, err, executed, seam.loaded.commitCalls)
	}
}

func TestRun_FailsClosedOnZeroExecutorResultWithNilError(t *testing.T) {
	fixture, item := newLinkItemFixture(t)
	seam := newRunSeam(t, fixture, []ItemPlan{item})
	seam.operations.execute = func(paths.ControlPlanePaths, ItemPlan) (linkItemResult, error) {
		return linkItemResult{}, nil
	}

	result, err := runWithOperations(RunOptions{}, seam.operations)
	if !errors.Is(err, ErrExecutionProtocol) || result.Valid() || result.Plan().Valid() || result.Outcomes() != nil || seam.loaded.commitCalls != 0 {
		t.Fatalf("runWithOperations() = (%#v, %v), commits=%d", result, err, seam.loaded.commitCalls)
	}
}

func TestRun_FailsClosedWithoutProjectableResultForExecutorProtocolViolations(t *testing.T) {
	tests := []struct {
		name   string
		result func(ItemPlan) linkItemResult
	}{
		{name: "mismatched item", result: func(item ItemPlan) linkItemResult {
			mismatched := item
			mismatched.target = "~/.different"
			return linkItemResult{item: mismatched, seal: successfulLinkExecutionSeal}
		}},
		{name: "target without source", result: func(item ItemPlan) linkItemResult {
			return linkItemResult{item: item, targetCommitted: true, seal: successfulLinkExecutionSeal}
		}},
		{name: "invalid seal", result: func(item ItemPlan) linkItemResult {
			return linkItemResult{item: item}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, item := newLinkItemFixture(t)
			seam := newRunSeam(t, fixture, []ItemPlan{item})
			seam.operations.execute = func(paths.ControlPlanePaths, ItemPlan) (linkItemResult, error) {
				return test.result(item), errors.New("executor error")
			}

			result, err := runWithOperations(RunOptions{}, seam.operations)
			if !errors.Is(err, ErrExecutionProtocol) || result.Valid() || result.Plan().Valid() ||
				result.Outcomes() != nil || seam.loaded.commitCalls != 0 {
				t.Fatalf("runWithOperations() = (%#v, %v), commits=%d", result, err, seam.loaded.commitCalls)
			}
		})
	}
}

func TestRun_PartialSuccessCommitsPrefixOnce(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	firstTarget := fixture.writeTarget(t, "a", "first", 0o600)
	secondTarget := fixture.writeTarget(t, "b", "second", 0o640)
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{firstTarget, secondTarget}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatal(err)
	}
	items := plan.Items()
	seam := newRunSeam(t, fixture, items)
	injected := errors.New("second failed")
	seam.operations.execute = func(_ paths.ControlPlanePaths, item ItemPlan) (linkItemResult, error) {
		if item.Target() == items[0].Target() {
			return linkItemResult{
				item: item, sourcePublished: true, stateReady: true, targetCommitted: true, seal: successfulLinkExecutionSeal,
			}, nil
		}
		return linkItemResult{item: item, seal: successfulLinkExecutionSeal}, injected
	}

	result, err := runWithOperations(RunOptions{}, seam.operations)
	if !errors.Is(err, injected) || !result.Valid() || result.Attempts() != 2 || result.TargetCommits() != 1 ||
		!result.StateCommitted() || seam.loaded.commitCalls != 1 {
		t.Fatalf("runWithOperations() = (%#v, %v), commits=%d", result, err, seam.loaded.commitCalls)
	}
	if result.Outcomes()[0].Status != OutcomeSucceeded || result.Outcomes()[1].Status != OutcomeFailed {
		t.Fatalf("outcomes = %#v", result.Outcomes())
	}
	if _, ok := seam.loaded.committed.Entry(items[0].Target()); !ok {
		t.Fatal("successful prefix missing from committed state")
	}
	if _, ok := seam.loaded.committed.Entry(items[1].Target()); ok {
		t.Fatal("failed suffix appeared in committed state")
	}
}

func TestRun_StateStoreFailurePreservesSourceAndLinkForApplyRecovery(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o600)
	options := fixture.runOptions(target)
	injected := errors.New("store failed")
	operations := defaultAddRunOperations()
	operations.begin = func(overrides dotruntime.Overrides) (addMutationSession, error) {
		session, err := dotruntime.BeginMutationWithStateStore(overrides, func(string, string, state.Snapshot) error {
			return injected
		})
		if err != nil {
			return nil, err
		}
		return runtimeAddMutationSession{session: session}, nil
	}

	result, err := runWithOperations(options, operations)
	if !errors.Is(err, injected) || !result.Valid() || result.TargetCommits() != 1 || result.StateCommitted() {
		t.Fatalf("runWithOperations() = (%#v, %v)", result, err)
	}
	item := result.Plan().Items()[0]
	if link, readErr := os.Readlink(target); readErr != nil || link != item.SourcePath() {
		t.Fatalf("retained target = (%q, %v)", link, readErr)
	}
	assertRegularFile(t, item.SourcePath(), "content", 0o600)
	if _, statErr := os.Lstat(fixture.control.StateFile()); !os.IsNotExist(statErr) {
		t.Fatalf("state file Lstat() error = %v, want missing", statErr)
	}

	recovered, err := apply.Run(apply.Options{Runtime: options.Runtime, CLIVersion: "dev", NoPrune: true})
	if err != nil {
		t.Fatalf("apply.Run(recovery) error = %v", err)
	}
	if recovered.AdoptionEffects != 1 || recovered.TargetCommits != 0 || !recovered.StateCommitted {
		t.Fatalf("apply recovery result = %#v, want one L2 state-only adoption", recovered)
	}
	converged, err := apply.Run(apply.Options{Runtime: options.Runtime, CLIVersion: "dev", NoPrune: true})
	if err != nil || converged.AdoptionEffects != 0 || converged.TargetCommits != 0 || converged.StateCommitted {
		t.Fatalf("second apply result/error = (%#v, %v), want no mutation/adopt", converged, err)
	}
}

func TestRun_PostCommitCleanupErrorKeepsSucceededOutcomeAndCommitsStateOnce(t *testing.T) {
	fixture, item := newLinkItemFixture(t)
	seam := newRunSeam(t, fixture, []ItemPlan{item})
	injected := errors.New("post-commit cleanup failed")
	seam.operations.execute = func(control paths.ControlPlanePaths, planned ItemPlan) (linkItemResult, error) {
		operations := defaultLinkOperations()
		realRemove := operations.remove
		operations.remove = func(path string) error {
			if filepath.Base(path) != targetTemporaryLinkName {
				return injected
			}
			return realRemove(path)
		}
		return executeLinkItem(control, planned, operations)
	}

	result, err := runWithOperations(RunOptions{}, seam.operations)
	if !errors.Is(err, injected) || !result.Valid() || result.TargetCommits() != 1 ||
		!result.StateCommitted() || seam.loaded.commitCalls != 1 ||
		result.Outcomes()[0].Status != OutcomeSucceeded {
		t.Fatalf("runWithOperations() = (%#v, %v), commits=%d", result, err, seam.loaded.commitCalls)
	}
	if link, readErr := os.Readlink(item.TargetPath()); readErr != nil || link != item.SourcePath() {
		t.Fatalf("committed target = (%q, %v)", link, readErr)
	}
	assertRegularFile(t, item.SourcePath(), "content", 0o600)
	if _, ok := seam.loaded.committed.Entry(item.Target()); !ok {
		t.Fatal("committed state is missing post-cleanup-error target")
	}
}

func TestRun_ResumesEquivalentSourcePublishedBeforeTargetCommit(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o600)
	options := fixture.runOptions(target)
	firstPlan, err := Preflight(fixture.load(t), options.Request)
	if err != nil {
		t.Fatal(err)
	}
	item := firstPlan.Items()[0]
	publication, err := publishSource(item, defaultPublicationOperations())
	if err != nil {
		t.Fatalf("publishSource(interrupted run) error = %v", err)
	}
	if !publication.Created() {
		t.Fatal("interrupted publication did not create source")
	}
	assertRegularFile(t, target, "content", 0o600)

	result, err := Run(options)
	if err != nil {
		t.Fatalf("Run(resume) error = %v", err)
	}
	if !result.Valid() || result.TargetCommits() != 1 || !result.StateCommitted() || !result.Plan().Items()[0].SourceExists() {
		t.Fatalf("Run(resume) result = %#v", result)
	}
	if link, readErr := os.Readlink(target); readErr != nil || link != item.SourcePath() {
		t.Fatalf("resumed target = (%q, %v)", link, readErr)
	}
}

func TestRun_ScaffoldCommitsStateWithoutTargetMutationAndApplyStaysConverged(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o644)
	before, _ := os.Lstat(target)
	options := fixture.runOptions(target)
	options.Request.Mode = ModeScaffold

	result, err := Run(options)
	if err != nil || !result.Valid() || result.TargetCommits() != 0 || !result.StateCommitted() ||
		result.Outcomes()[0].Status != OutcomeSucceeded {
		t.Fatalf("Run(scaffold) = (%#v, %v)", result, err)
	}
	item := result.Plan().Items()[0]
	after, _ := os.Lstat(target)
	if !os.SameFile(before, after) {
		t.Fatal("scaffold Run changed target identity")
	}
	assertRegularFile(t, target, "content", 0o644)
	assertRegularFile(t, item.SourcePath(), "content", 0o644)
	snapshot, ok := fixture.load(t).State().Snapshot()
	entry, exists := snapshot.Entry(item.Target())
	if !ok || !exists || entry.Kind() != state.KindScaffold || entry.LinkDest() != "" {
		t.Fatalf("scaffold state entry = (%#v, %t, %t)", entry, ok, exists)
	}
	for run := 0; run < 2; run++ {
		converged, applyErr := apply.Run(apply.Options{Runtime: options.Runtime, CLIVersion: "dev", NoPrune: true})
		if applyErr != nil || converged.TargetCommits != 0 || converged.AdoptionEffects != 0 || converged.StateCommitted {
			t.Fatalf("apply run %d = (%#v, %v), want no mutation/adopt", run+1, converged, applyErr)
		}
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	deleted, err := apply.Run(apply.Options{Runtime: options.Runtime, CLIVersion: "dev", NoPrune: true})
	if err != nil || deleted.TargetCommits != 0 || deleted.AdoptionEffects != 0 {
		t.Fatalf("apply(deleted scaffold) = (%#v, %v)", deleted, err)
	}
	if _, statErr := os.Lstat(target); !os.IsNotExist(statErr) {
		t.Fatalf("deleted scaffold target was rebuilt: %v", statErr)
	}
}

func TestRun_ScaffoldStateStoreFailurePreservesSourceForS1bRecovery(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o644)
	options := fixture.runOptions(target)
	options.Request.Mode = ModeScaffold
	injected := errors.New("store failed")
	operations := defaultAddRunOperations()
	operations.begin = func(overrides dotruntime.Overrides) (addMutationSession, error) {
		session, err := dotruntime.BeginMutationWithStateStore(overrides, func(string, string, state.Snapshot) error {
			return injected
		})
		if err != nil {
			return nil, err
		}
		return runtimeAddMutationSession{session: session}, nil
	}

	result, err := runWithOperations(options, operations)
	if !errors.Is(err, injected) || !result.Valid() || result.StateCommitted() ||
		result.TargetCommits() != 0 || result.Outcomes()[0].Status != OutcomeFailed {
		t.Fatalf("runWithOperations(scaffold store failure) = (%#v, %v)", result, err)
	}
	item := result.Plan().Items()[0]
	assertRegularFile(t, target, "content", 0o644)
	assertRegularFile(t, item.SourcePath(), "content", 0o644)
	recovered, err := apply.Run(apply.Options{Runtime: options.Runtime, CLIVersion: "dev", NoPrune: true})
	if err != nil || recovered.AdoptionEffects != 1 || recovered.TargetCommits != 0 || !recovered.StateCommitted {
		t.Fatalf("apply S1b recovery = (%#v, %v)", recovered, err)
	}
	converged, err := apply.Run(apply.Options{Runtime: options.Runtime, CLIVersion: "dev", NoPrune: true})
	if err != nil || converged.AdoptionEffects != 0 || converged.TargetCommits != 0 || converged.StateCommitted {
		t.Fatalf("second apply = (%#v, %v)", converged, err)
	}
}

func TestRun_TemplateRejectedBeforeMutationBegin(t *testing.T) {
	beginCalled := false
	operations := addRunOperations{
		begin: func(dotruntime.Overrides) (addMutationSession, error) {
			beginCalled = true
			return nil, nil
		},
		preflight: func(dotruntime.LoadedInputs, Request) (BatchPlan, error) { return BatchPlan{}, nil },
		execute:   func(paths.ControlPlanePaths, ItemPlan) (linkItemResult, error) { return linkItemResult{}, nil },
		now:       time.Now,
	}
	result, err := runWithOperations(RunOptions{Request: Request{Mode: ModeTemplate}}, operations)
	if !errors.Is(err, ErrTemplateUnsupported) || result.Valid() || beginCalled {
		t.Fatalf("runWithOperations(template) = (%#v, %v), beginCalled=%t", result, err, beginCalled)
	}
}

func TestRun_ScaffoldPartialSuccessCommitsOnlyPreparedPrefix(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	firstTarget := fixture.writeTarget(t, "a", "first", 0o644)
	secondTarget := fixture.writeTarget(t, "b", "second", 0o644)
	options := fixture.runOptions(firstTarget, secondTarget)
	options.Request.Mode = ModeScaffold
	operations := defaultAddRunOperations()
	injected := errors.New("second scaffold failed")
	executions := 0
	operations.execute = func(control paths.ControlPlanePaths, item ItemPlan) (linkItemResult, error) {
		executions++
		if executions == 2 {
			return linkItemResult{item: item, seal: successfulLinkExecutionSeal}, injected
		}
		return executeScaffoldItem(control, item, defaultPublicationOperations())
	}

	result, err := runWithOperations(options, operations)
	if !errors.Is(err, injected) || !result.Valid() || result.Attempts() != 2 || result.TargetCommits() != 0 ||
		!result.StateCommitted() || result.Outcomes()[0].Status != OutcomeSucceeded ||
		result.Outcomes()[1].Status != OutcomeFailed {
		t.Fatalf("runWithOperations(partial scaffold) = (%#v, %v)", result, err)
	}
	items := result.Plan().Items()
	assertRegularFile(t, items[0].SourcePath(), "first", 0o644)
	if _, statErr := os.Lstat(items[1].SourcePath()); !os.IsNotExist(statErr) {
		t.Fatalf("failed suffix source Lstat() error = %v, want missing", statErr)
	}
	snapshot, ok := fixture.load(t).State().Snapshot()
	if !ok {
		t.Fatal("state snapshot missing")
	}
	if _, exists := snapshot.Entry(items[0].Target()); !exists {
		t.Fatal("prepared scaffold prefix missing from state")
	}
	if _, exists := snapshot.Entry(items[1].Target()); exists {
		t.Fatal("failed scaffold suffix appeared in state")
	}
}

func TestRun_ScaffoldRevalidatesTargetImmediatelyBeforeStateCommit(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o644)
	options := fixture.runOptions(target)
	options.Request.Mode = ModeScaffold
	operations := defaultAddRunOperations()
	realRevalidate := operations.revalidateScaffold
	operations.revalidateScaffold = func(control paths.ControlPlanePaths, item ItemPlan, result linkItemResult) error {
		replacement := filepath.Join(fixture.home, "replacement-before-state")
		writeAddFile(t, replacement, "content", 0o644)
		if err := os.Rename(replacement, target); err != nil {
			return err
		}
		return realRevalidate(control, item, result)
	}

	result, err := runWithOperations(options, operations)
	if err == nil || !result.Valid() || result.StateCommitted() || result.Outcomes()[0].Status != OutcomeFailed {
		t.Fatalf("runWithOperations(final target change) = (%#v, %v)", result, err)
	}
	item := result.Plan().Items()[0]
	if _, statErr := os.Lstat(item.SourcePath()); !os.IsNotExist(statErr) {
		t.Fatalf("uncommitted source Lstat() error = %v, want missing", statErr)
	}
	if _, statErr := os.Lstat(fixture.control.StateFile()); !os.IsNotExist(statErr) {
		t.Fatalf("state file Lstat() error = %v, want missing", statErr)
	}
	assertRegularFile(t, target, "content", 0o644)
}

func TestRun_ScaffoldExecutorProtocolViolationsFailClosed(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o644)
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeScaffold})
	if err != nil {
		t.Fatal(err)
	}
	item := plan.Items()[0]
	valid, err := executeScaffoldItem(fixture.control, item, defaultPublicationOperations())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		result linkItemResult
	}{
		{name: "zero", result: linkItemResult{}},
		{name: "state ready without evidence", result: linkItemResult{
			item: item, sourcePublished: true, stateReady: true, seal: successfulLinkExecutionSeal,
		}},
		{name: "contradictory target commit", result: func() linkItemResult {
			contradictory := valid
			contradictory.targetCommitted = true
			return contradictory
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			seam := newRunSeam(t, fixture, []ItemPlan{item})
			seam.operations.revalidateScaffold = func(paths.ControlPlanePaths, ItemPlan, linkItemResult) error { return nil }
			seam.operations.cleanupScaffold = func(linkItemResult) error { return nil }
			seam.operations.execute = func(paths.ControlPlanePaths, ItemPlan) (linkItemResult, error) {
				return test.result, nil
			}
			result, runErr := runWithOperations(RunOptions{Request: Request{Mode: ModeScaffold}}, seam.operations)
			if !errors.Is(runErr, ErrExecutionProtocol) || result.Valid() || seam.loaded.commitCalls != 0 {
				t.Fatalf("runWithOperations() = (%#v, %v), commits=%d", result, runErr, seam.loaded.commitCalls)
			}
		})
	}
}

type runSeam struct {
	operations addRunOperations
	loaded     *runSeamLoaded
}

type runSeamSession struct {
	loaded *runSeamLoaded
	closed bool
}

func (session *runSeamSession) load(string) (addLoadedMutation, error) { return session.loaded, nil }
func (session *runSeamSession) close() error {
	session.closed = true
	return nil
}

type runSeamLoaded struct {
	loaded      dotruntime.LoadedInputs
	controlPath paths.ControlPlanePaths
	commitCalls int
	committed   state.Snapshot
}

func (loaded *runSeamLoaded) inputs() dotruntime.LoadedInputs  { return loaded.loaded }
func (loaded *runSeamLoaded) baseline() state.Loaded           { return loaded.loaded.State() }
func (loaded *runSeamLoaded) control() paths.ControlPlanePaths { return loaded.controlPath }
func (loaded *runSeamLoaded) commit(snapshot state.Snapshot) error {
	loaded.commitCalls++
	loaded.committed = snapshot
	return nil
}

func newRunSeam(t *testing.T, fixture *addFixture, items []ItemPlan) runSeam {
	t.Helper()
	loaded := &runSeamLoaded{loaded: fixture.load(t), controlPath: fixture.control}
	session := &runSeamSession{loaded: loaded}
	plan := sealBatchPlan("base", fixture.home, fixture.repo, items)
	return runSeam{
		loaded: loaded,
		operations: addRunOperations{
			begin:     func(dotruntime.Overrides) (addMutationSession, error) { return session, nil },
			preflight: func(dotruntime.LoadedInputs, Request) (BatchPlan, error) { return plan, nil },
			execute: func(_ paths.ControlPlanePaths, item ItemPlan) (linkItemResult, error) {
				return linkItemResult{item: item, sourcePublished: true, stateReady: true, targetCommitted: true, seal: successfulLinkExecutionSeal}, nil
			},
			now: func() time.Time { return time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC) },
		},
	}
}

func (fixture *addFixture) runOptions(paths ...string) RunOptions {
	return RunOptions{
		Runtime: dotruntime.Overrides{
			Home:       dotruntime.Override{Value: fixture.home, Set: true},
			Repository: dotruntime.Override{Value: fixture.repo, Set: true},
			Profile:    dotruntime.Override{Value: "base", Set: true},
		},
		CLIVersion: "dev",
		Request:    Request{Paths: append([]string(nil), paths...), Module: "app", Mode: ModeLink},
	}
}
