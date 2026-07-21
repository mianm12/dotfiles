package add

import (
	"errors"
	"os"
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
	if !errors.Is(err, ErrExecutionProtocol) || !result.Valid() || result.TargetCommits() != 0 || seam.loaded.commitCalls != 0 {
		t.Fatalf("runWithOperations() = (%#v, %v), commits=%d", result, err, seam.loaded.commitCalls)
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
				item: item, sourcePublished: true, targetCommitted: true, seal: successfulLinkExecutionSeal,
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
				return linkItemResult{item: item, sourcePublished: true, targetCommitted: true, seal: successfulLinkExecutionSeal}, nil
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
