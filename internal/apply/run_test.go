package apply

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

func TestRun_RejectsInvalidCanonicalPlanBeforeMutation(t *testing.T) {
	fixture := newRunSeamFixture(t)
	operations := fixture.operations(executionPlan{}).runOperations
	operations.begin = func(dotruntime.Overrides) (mutationSession, error) {
		return fixture.session, nil
	}
	operations.plan = func(dotruntime.LoadedInputs, planner.ApplyScopeOptions) (planner.ApplyPlan, error) {
		return planner.ApplyPlan{}, nil
	}
	executed := false
	operations.execute = func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error) {
		executed = true
		return executor.FileResult{}, nil
	}
	pruneExecuted := false
	operations.pruneExecute = func(paths.ControlPlanePaths, planner.PruneAction) (executor.PruneResult, error) {
		pruneExecuted = true
		return executor.PruneResult{}, nil
	}
	confirmed := false

	result, err := runWithOperations(Options{Confirm: func([]planner.PruneConfirmationGroup) (bool, error) {
		confirmed = true
		return true, nil
	}}, operations)
	if !errors.Is(err, ErrExecutionProtocol) {
		t.Fatalf("runWithOperations() error = %v, want ErrExecutionProtocol", err)
	}
	if result.Valid(true) || executed || pruneExecuted || confirmed ||
		fixture.loaded.commitCalls != 0 {
		t.Fatalf(
			"invalid plan escaped gate: result=%#v execute=%t prune=%t confirm=%t commit=%d",
			result,
			executed,
			pruneExecuted,
			confirmed,
			fixture.loaded.commitCalls,
		)
	}
	if !fixture.session.closed {
		t.Fatal("session was not closed after invalid plan rejection")
	}
}

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

	result, err := operations.run(Options{})
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("runWithOperations() error = %v, want cleanup error", err)
	}
	if secondStarted || result.FileAttempts() != 1 || result.TargetCommits() != 1 || !result.StateCommitted() {
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

	result, err := operations.run(Options{})
	for _, want := range []error{executionErr, commitErr, closeErr} {
		if !errors.Is(err, want) {
			t.Fatalf("runWithOperations() error = %v, want joined %v", err, want)
		}
	}
	if result.FileAttempts() != 2 || result.TargetCommits() != 1 || result.StateCommitted() {
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

	result, err := operations.run(Options{})
	if !errors.Is(err, ErrUnsupportedPlan) {
		t.Fatalf("runWithOperations() error = %v, want ErrUnsupportedPlan", err)
	}
	if executed || result.FileAttempts() != 0 || fixture.loaded.commitCalls != 0 {
		t.Fatalf("precheck executed=%t result=%#v commitCalls=%d", executed, result, fixture.loaded.commitCalls)
	}
	if !fixture.session.closed {
		t.Fatal("session was not closed after scope rejection")
	}
}

func TestRun_RejectsMalformedLinkUpsertBeforeExecutor(t *testing.T) {
	fixture := newRunSeamFixture(t)
	action := seamLinkAction("~/.malformed-upsert")
	action.OnSuccess.Entry.Key = ""
	executed := false
	operations := fixture.operations(executionPlan{files: []planner.FileAction{action}})
	operations.execute = func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error) {
		executed = true
		return executor.FileResult{}, nil
	}

	result, err := operations.run(Options{})
	if !errors.Is(err, ErrUnsupportedPlan) {
		t.Fatalf("runWithOperations() error = %v, want ErrUnsupportedPlan", err)
	}
	if executed || result.FileAttempts() != 0 || fixture.loaded.commitCalls != 0 {
		t.Fatalf("preflight executed=%t result=%#v commitCalls=%d", executed, result, fixture.loaded.commitCalls)
	}
}

func TestRun_HooksIgnoreFileAndPruneConvergenceGates(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, runSeamFixture, *executionPlan, *seamOperations, *Options)
	}{
		{
			name: "file conflict",
			prepare: func(_ *testing.T, _ runSeamFixture, plan *executionPlan, operations *seamOperations, _ *Options) {
				file := seamLinkAction("~/.conflict")
				plan.files = []planner.FileAction{file}
				operations.execute = func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error) {
					return executor.FileResult{StateEffect: file.OnFailure}, executor.ErrPreconditionMismatch
				}
			},
		},
		{
			name: "prune deferred",
			prepare: func(_ *testing.T, _ runSeamFixture, plan *executionPlan, _ *seamOperations, _ *Options) {
				plan.prune = []planner.PruneAction{{Deferred: true}}
			},
		},
		{
			name: "confirmation refused",
			prepare: func(t *testing.T, fixture runSeamFixture, plan *executionPlan, _ *seamOperations, options *Options) {
				plan.prune = []planner.PruneAction{seamPruneAction(t, fixture.loaded.controlPaths, "~/.orphan", planner.PruneReasonScaffold)}
				plan.groups = []planner.PruneConfirmationGroup{{Module: "old"}}
				options.Confirm = func([]planner.PruneConfirmationGroup) (bool, error) { return false, nil }
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunSeamFixture(t)
			hook := seamHookAction(fixture, "app/hooks/setup.sh", planner.HookRun)
			plan := executionPlan{hooks: []planner.HookAction{hook}}
			operations := fixture.operations(plan)
			options := Options{}
			test.prepare(t, fixture, &plan, &operations, &options)
			operations.plan = plan
			hookCalls := 0
			operations.executeHook = func(action planner.HookAction, _ executor.HookStreams) (executor.HookResult, error) {
				hookCalls++
				return executor.HookResult{StateEffect: action.OnSuccess}, nil
			}

			result, err := operations.run(options)
			if err != nil {
				t.Fatalf("runExecution() error = %v", err)
			}
			if hookCalls != 1 || result.HookAttempts() != 1 || result.HookEffects() != 1 ||
				!result.StateCommitted() || fixture.loaded.commitCalls != 1 {
				t.Fatalf("hook gate result = %#v, calls=%d commit=%d", result, hookCalls, fixture.loaded.commitCalls)
			}
			if _, exists := fixture.loaded.committed.RunOnce(hook.StateKey); !exists {
				t.Fatal("successful hook effect was not committed")
			}
		})
	}
}

func TestRun_HookFailureStopsSuffixAndCommitsSuccessfulPrefix(t *testing.T) {
	fixture := newRunSeamFixture(t)
	fixture.loadBaseline(t, `{"version":1,"entries":{},"run_once":{"app/hooks/second.sh":{"hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","executed_at":"2026-07-19T00:00:00Z"}}}`)
	first := seamHookAction(fixture, "app/hooks/first.sh", planner.HookRun)
	second := seamHookAction(fixture, "app/hooks/second.sh", planner.HookRun)
	third := seamHookAction(fixture, "app/hooks/third.sh", planner.HookRun)
	failure := errors.New("hook exited 23")
	operations := fixture.operations(executionPlan{hooks: []planner.HookAction{first, second, third}})
	var executed []string
	operations.executeHook = func(action planner.HookAction, _ executor.HookStreams) (executor.HookResult, error) {
		executed = append(executed, action.StateKey)
		if action.StateKey == second.StateKey {
			return executor.HookResult{StateEffect: action.OnFailure}, failure
		}
		return executor.HookResult{StateEffect: action.OnSuccess}, nil
	}

	result, err := operations.run(Options{})
	if !errors.Is(err, failure) {
		t.Fatalf("runExecution() error = %v, want hook failure", err)
	}
	if !reflect.DeepEqual(executed, []string{first.StateKey, second.StateKey}) {
		t.Fatalf("executed hooks = %#v, want successful prefix plus failure", executed)
	}
	wantOutcomes := []HookOutcome{
		{Index: 0, StateKey: first.StateKey, Status: ActionSucceeded},
		{Index: 1, StateKey: second.StateKey, Status: ActionFailed},
		{Index: 2, StateKey: third.StateKey, Status: ActionDeferred},
	}
	if !reflect.DeepEqual(result.HookOutcomes(), wantOutcomes) || result.HookAttempts() != 2 ||
		result.HookEffects() != 1 || !result.StateCommitted() || fixture.loaded.commitCalls != 1 {
		t.Fatalf("hook failure result = %#v commit=%d", result, fixture.loaded.commitCalls)
	}
	firstRecord, firstExists := fixture.loaded.committed.RunOnce(first.StateKey)
	secondRecord, secondExists := fixture.loaded.committed.RunOnce(second.StateKey)
	if !firstExists || firstRecord.Hash() != first.Fingerprint {
		t.Fatalf("first hook record = (%#v, %t)", firstRecord, firstExists)
	}
	if !secondExists || secondRecord.Hash() != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("failed hook old record = (%#v, %t)", secondRecord, secondExists)
	}
	if _, exists := fixture.loaded.committed.RunOnce(third.StateKey); exists {
		t.Fatal("unattempted suffix received a run_once record")
	}
}

func TestRun_OrdersFilesConfirmationPruneAndStoresMixedEffectsOnce(t *testing.T) {
	fixture := newRunSeamFixture(t)
	fixture.loadBaseline(t, `{
  "version": 1,
  "entries": {"~/.orphan":{"module":"old","kind":"scaffold","source":"modules/old/file.template","applied_at":"2026-07-19T00:00:00Z"}},
  "run_once": {"keep/hooks/done":{"hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","executed_at":"2026-07-19T00:00:00Z"}}
}`)
	file := seamLinkAction("~/.created")
	prune := seamPruneAction(t, fixture.loaded.controlPaths, "~/.orphan", planner.PruneReasonScaffold)
	hook := seamHookAction(fixture, "app/hooks/setup.sh", planner.HookRun)
	skip := seamHookAction(fixture, "app/hooks/done.sh", planner.HookSkip)
	order := make([]string, 0, 4)
	operations := fixture.operations(executionPlan{
		files:  []planner.FileAction{file},
		prune:  []planner.PruneAction{prune},
		groups: []planner.PruneConfirmationGroup{{Module: "old"}},
		hooks:  []planner.HookAction{hook, skip},
	})
	operations.execute = func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error) {
		order = append(order, "file")
		return executor.FileResult{StateEffect: file.OnSuccess, TargetMutated: true}, nil
	}
	operations.pruneExecute = func(paths.ControlPlanePaths, planner.PruneAction) (executor.PruneResult, error) {
		order = append(order, "prune")
		return executor.PruneResult{StateEffect: prune.OnSuccess}, nil
	}
	operations.executeHook = func(action planner.HookAction, _ executor.HookStreams) (executor.HookResult, error) {
		order = append(order, "hook")
		return executor.HookResult{StateEffect: action.OnSuccess}, nil
	}

	result, err := operations.run(Options{Confirm: func([]planner.PruneConfirmationGroup) (bool, error) {
		order = append(order, "confirm")
		return true, nil
	}})
	if err != nil {
		t.Fatalf("runWithOperations() error = %v", err)
	}
	if got := strings.Join(order, ","); got != "file,confirm,prune,hook" {
		t.Fatalf("execution order = %q", got)
	}
	if result.FileAttempts() != 1 || result.PruneAttempts() != 1 || result.PruneEffects() != 1 ||
		result.HookAttempts() != 1 || result.HookEffects() != 1 ||
		!result.ConfirmRequested() || !result.ConfirmAccepted() || !result.StateCommitted() || fixture.loaded.commitCalls != 1 {
		t.Fatalf("run result = %#v commitCalls=%d", result, fixture.loaded.commitCalls)
	}
	if _, exists := fixture.loaded.committed.Entry("~/.orphan"); exists {
		t.Fatal("pruned entry remains in committed state")
	}
	if _, exists := fixture.loaded.committed.Entry("~/.created"); !exists {
		t.Fatal("successful file upsert missing from committed state")
	}
	if _, exists := fixture.loaded.committed.RunOnce("keep/hooks/done"); !exists {
		t.Fatal("unrelated run_once was not preserved")
	}
	if record, exists := fixture.loaded.committed.RunOnce(hook.StateKey); !exists || record.Hash() != hook.Fingerprint {
		t.Fatalf("successful hook record = (%#v, %t)", record, exists)
	}
	wantHookOutcomes := []HookOutcome{
		{Index: 0, StateKey: hook.StateKey, Status: ActionSucceeded},
		{Index: 1, StateKey: skip.StateKey, Status: ActionSkipped},
	}
	if !reflect.DeepEqual(result.HookOutcomes(), wantHookOutcomes) {
		t.Fatalf("hook outcomes = %#v, want %#v", result.HookOutcomes(), wantHookOutcomes)
	}
}

func TestRun_HookStoreFailureRetriesAtLeastOnce(t *testing.T) {
	fixture := newRunSeamFixture(t)
	hook := seamHookAction(fixture, "app/hooks/setup.sh", planner.HookRun)
	storeErr := errors.New("store failed")
	fixture.loaded.commitErr = storeErr
	operations := fixture.operations(executionPlan{hooks: []planner.HookAction{hook}})
	hookCalls := 0
	operations.executeHook = func(action planner.HookAction, _ executor.HookStreams) (executor.HookResult, error) {
		hookCalls++
		return executor.HookResult{StateEffect: action.OnSuccess}, nil
	}

	first, err := operations.run(Options{})
	if !errors.Is(err, storeErr) || hookCalls != 1 || first.HookEffects() != 1 ||
		first.StateCommitted() || fixture.loaded.commitCalls != 1 {
		t.Fatalf("first run = (%#v, %v), hookCalls=%d commitCalls=%d", first, err, hookCalls, fixture.loaded.commitCalls)
	}
	fixture.loaded.commitErr = nil
	second, err := operations.run(Options{})
	if err != nil || hookCalls != 2 || second.HookEffects() != 1 ||
		!second.StateCommitted() || fixture.loaded.commitCalls != 2 {
		t.Fatalf("retry = (%#v, %v), hookCalls=%d commitCalls=%d", second, err, hookCalls, fixture.loaded.commitCalls)
	}
	if record, exists := fixture.loaded.committed.RunOnce(hook.StateKey); !exists || record.Hash() != hook.Fingerprint {
		t.Fatalf("retried hook record = (%#v, %t)", record, exists)
	}
}

func TestRun_HookSkipDoesNotExecuteOrCommit(t *testing.T) {
	fixture := newRunSeamFixture(t)
	skip := seamHookAction(fixture, "app/hooks/setup.sh", planner.HookSkip)
	operations := fixture.operations(executionPlan{hooks: []planner.HookAction{skip}})
	hookCalls := 0
	operations.executeHook = func(planner.HookAction, executor.HookStreams) (executor.HookResult, error) {
		hookCalls++
		return executor.HookResult{}, nil
	}

	result, err := operations.run(Options{})
	want := []HookOutcome{{Index: 0, StateKey: skip.StateKey, Status: ActionSkipped}}
	if err != nil || hookCalls != 0 || result.HookAttempts() != 0 || result.HookEffects() != 0 ||
		result.StateCommitted() || fixture.loaded.commitCalls != 0 || !reflect.DeepEqual(result.HookOutcomes(), want) {
		t.Fatalf("skip result = (%#v, %v), calls=%d commit=%d", result, err, hookCalls, fixture.loaded.commitCalls)
	}
}

func TestRun_RejectsHookResultsThatContradictAction(t *testing.T) {
	for _, test := range []struct {
		name       string
		effect     func(planner.HookAction) planner.HookStateEffect
		executeErr error
	}{
		{
			name:       "success with error",
			effect:     func(action planner.HookAction) planner.HookStateEffect { return action.OnSuccess },
			executeErr: errors.New("post-success error"),
		},
		{
			name:   "failure without error",
			effect: func(action planner.HookAction) planner.HookStateEffect { return action.OnFailure },
		},
		{
			name: "unknown effect",
			effect: func(planner.HookAction) planner.HookStateEffect {
				return planner.HookStateEffect{Kind: planner.HookStateEffectKind("future")}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunSeamFixture(t)
			hook := seamHookAction(fixture, "app/hooks/setup.sh", planner.HookRun)
			operations := fixture.operations(executionPlan{hooks: []planner.HookAction{hook}})
			operations.executeHook = func(action planner.HookAction, _ executor.HookStreams) (executor.HookResult, error) {
				return executor.HookResult{StateEffect: test.effect(action)}, test.executeErr
			}

			result, err := operations.run(Options{})
			if !errors.Is(err, ErrExecutionProtocol) || result.HookAttempts() != 1 || result.HookEffects() != 0 ||
				result.StateCommitted() || fixture.loaded.commitCalls != 0 {
				t.Fatalf("contradictory hook result = (%#v, %v), commit=%d", result, err, fixture.loaded.commitCalls)
			}
			want := []HookOutcome{{Index: 0, StateKey: hook.StateKey, Status: ActionFailed}}
			if !reflect.DeepEqual(result.HookOutcomes(), want) {
				t.Fatalf("hook outcomes = %#v, want %#v", result.HookOutcomes(), want)
			}
		})
	}
}

func TestRun_ConfirmationRefusalDefersAllPruneButStoresFileSuccess(t *testing.T) {
	fixture := newRunSeamFixture(t)
	fixture.loadBaseline(t, `{"version":1,"entries":{"~/.orphan":{"module":"old","kind":"scaffold","source":"modules/old/file.template","applied_at":"2026-07-19T00:00:00Z"}},"run_once":{}}`)
	file := seamLinkAction("~/.created")
	prune := seamPruneAction(t, fixture.loaded.controlPaths, "~/.orphan", planner.PruneReasonScaffold)
	operations := fixture.operations(executionPlan{
		files: []planner.FileAction{file}, prune: []planner.PruneAction{prune},
		groups: []planner.PruneConfirmationGroup{{Module: "old"}},
	})
	operations.execute = func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error) {
		return executor.FileResult{StateEffect: file.OnSuccess, TargetMutated: true}, nil
	}
	pruneExecuted := false
	operations.pruneExecute = func(paths.ControlPlanePaths, planner.PruneAction) (executor.PruneResult, error) {
		pruneExecuted = true
		return executor.PruneResult{}, nil
	}

	result, err := operations.run(Options{Confirm: func([]planner.PruneConfirmationGroup) (bool, error) {
		return false, nil
	}})
	if err != nil {
		t.Fatalf("runWithOperations() error = %v", err)
	}
	if pruneExecuted || !result.PruneDeferred() || !result.ConfirmRequested() || result.ConfirmAccepted() ||
		result.PruneAttempts() != 0 || !result.StateCommitted() || fixture.loaded.commitCalls != 1 {
		t.Fatalf("run result = %#v pruneExecuted=%t commitCalls=%d", result, pruneExecuted, fixture.loaded.commitCalls)
	}
	if _, exists := fixture.loaded.committed.Entry("~/.orphan"); !exists {
		t.Fatal("confirmation refusal removed orphan state")
	}
}

func TestRun_MissingConfirmationDefersWholeModulePrune(t *testing.T) {
	fixture := newRunSeamFixture(t)
	prune := seamPruneAction(t, fixture.loaded.controlPaths, "~/.orphan", planner.PruneReasonScaffold)
	operations := fixture.operations(executionPlan{
		prune:  []planner.PruneAction{prune},
		groups: []planner.PruneConfirmationGroup{{Module: "old"}},
	})
	pruneExecuted := false
	operations.pruneExecute = func(paths.ControlPlanePaths, planner.PruneAction) (executor.PruneResult, error) {
		pruneExecuted = true
		return executor.PruneResult{}, nil
	}

	result, err := operations.run(Options{})
	if err != nil {
		t.Fatalf("runWithOperations() error = %v", err)
	}
	if pruneExecuted || !result.PruneDeferred() || !result.ConfirmRequested() || result.ConfirmAccepted() ||
		result.PruneAttempts() != 0 || result.StateCommitted() || fixture.loaded.commitCalls != 0 {
		t.Fatalf("run result = %#v pruneExecuted=%t commitCalls=%d", result, pruneExecuted, fixture.loaded.commitCalls)
	}
}

func TestRun_PrunePreconditionBecomesConflictAndCommitsPriorPrune(t *testing.T) {
	fixture := newRunSeamFixture(t)
	fixture.loadBaseline(t, `{
  "version":1,
  "entries":{
    "~/.first":{"module":"old","kind":"scaffold","source":"modules/old/first.template","applied_at":"2026-07-19T00:00:00Z"},
    "~/.second":{"module":"old","kind":"symlink","source":"modules/old/second","link_dest":"/old/second","applied_at":"2026-07-19T00:00:00Z"}
  },
  "run_once":{}
}`)
	first := seamPruneAction(t, fixture.loaded.controlPaths, "~/.first", planner.PruneReasonScaffold)
	second := seamPruneAction(t, fixture.loaded.controlPaths, "~/.second", planner.PruneReasonOwned)
	operations := fixture.operations(executionPlan{prune: []planner.PruneAction{first, second}})
	operations.pruneExecute = func(_ paths.ControlPlanePaths, action planner.PruneAction) (executor.PruneResult, error) {
		if action.Target == first.Target {
			return executor.PruneResult{StateEffect: action.OnSuccess}, nil
		}
		return executor.PruneResult{StateEffect: action.OnFailure}, executor.ErrPreconditionMismatch
	}

	result, err := operations.run(Options{})
	if err != nil {
		t.Fatalf("runWithOperations() error = %v, want downgraded conflict", err)
	}
	if result.PruneAttempts() != 2 || result.PruneEffects() != 1 || result.UnresolvedConflicts() != 1 ||
		!result.PruneDeferred() || !result.StateCommitted() || fixture.loaded.commitCalls != 1 {
		t.Fatalf("run result = %#v commitCalls=%d", result, fixture.loaded.commitCalls)
	}
	wantOutcomes := []PruneOutcome{
		{Index: 0, Target: first.Target, Status: ActionSucceeded},
		{Index: 1, Target: second.Target, Status: ActionConflict},
	}
	if !reflect.DeepEqual(result.PruneOutcomes(), wantOutcomes) {
		t.Fatalf("prune outcomes = %#v, want %#v", result.PruneOutcomes(), wantOutcomes)
	}
	if _, exists := fixture.loaded.committed.Entry(first.Target); exists {
		t.Fatal("successful first prune was not persisted")
	}
	if _, exists := fixture.loaded.committed.Entry(second.Target); !exists {
		t.Fatal("failed second prune removed state")
	}
}

func TestRun_PreconditionClassificationRequiresPureMismatch(t *testing.T) {
	runtimeErr := errors.New("observation IO failed")
	cleanupErr := errors.New("cleanup failed")
	tests := []struct {
		name           string
		prune          bool
		executeErr     error
		wantConflict   int
		wantRuntimeErr error
	}{
		{name: "file pure mismatch", executeErr: executor.ErrPreconditionMismatch, wantConflict: 1},
		{name: "file IO", executeErr: fmt.Errorf("%w: %w", executor.ErrPrecondition, runtimeErr), wantRuntimeErr: runtimeErr},
		{name: "file mismatch plus cleanup", executeErr: errors.Join(executor.ErrPreconditionMismatch, cleanupErr), wantRuntimeErr: cleanupErr},
		{name: "prune pure mismatch", prune: true, executeErr: executor.ErrPreconditionMismatch, wantConflict: 1},
		{name: "prune IO", prune: true, executeErr: fmt.Errorf("%w: %w", executor.ErrPrecondition, runtimeErr), wantRuntimeErr: runtimeErr},
		{name: "prune mismatch plus cleanup", prune: true, executeErr: errors.Join(executor.ErrPreconditionMismatch, cleanupErr), wantRuntimeErr: cleanupErr},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunSeamFixture(t)
			var operations seamOperations
			if test.prune {
				action := seamPruneAction(t, fixture.loaded.controlPaths, "~/.orphan", planner.PruneReasonScaffold)
				operations = fixture.operations(executionPlan{prune: []planner.PruneAction{action}})
				operations.pruneExecute = func(paths.ControlPlanePaths, planner.PruneAction) (executor.PruneResult, error) {
					return executor.PruneResult{StateEffect: action.OnFailure}, test.executeErr
				}
			} else {
				action := seamLinkAction("~/.file")
				operations = fixture.operations(executionPlan{files: []planner.FileAction{action}})
				operations.execute = func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error) {
					return executor.FileResult{StateEffect: action.OnFailure}, test.executeErr
				}
			}

			result, err := operations.run(Options{})
			if test.wantRuntimeErr == nil {
				if err != nil {
					t.Fatalf("runWithOperations() error = %v, want downgraded conflict", err)
				}
			} else if !errors.Is(err, test.wantRuntimeErr) {
				t.Fatalf("runWithOperations() error = %v, want runtime error %v", err, test.wantRuntimeErr)
			}
			if result.UnresolvedConflicts() != test.wantConflict || result.StateCommitted() || fixture.loaded.commitCalls != 0 {
				t.Fatalf("run result = %#v commitCalls=%d", result, fixture.loaded.commitCalls)
			}
			if test.prune {
				wantStatus := ActionFailed
				if test.wantConflict == 1 {
					wantStatus = ActionConflict
				}
				if got := result.PruneOutcomes(); len(got) != 1 || got[0].Status != wantStatus || got[0].Index != 0 || got[0].Target != "~/.orphan" {
					t.Fatalf("prune outcomes = %#v, want %q for index 0", got, wantStatus)
				}
			} else {
				wantStatus := ActionFailed
				if test.wantConflict == 1 {
					wantStatus = ActionConflict
				}
				if got := result.FileOutcomes(); len(got) != 1 || got[0].Status != wantStatus || got[0].Index != 0 || got[0].Target != "~/.file" {
					t.Fatalf("file outcomes = %#v, want %q for index 0", got, wantStatus)
				}
			}
		})
	}
}

func TestRun_FilePreconditionMismatchDefersAllPrune(t *testing.T) {
	fixture := newRunSeamFixture(t)
	file := seamLinkAction("~/.file")
	prune := seamPruneAction(t, fixture.loaded.controlPaths, "~/.orphan", planner.PruneReasonScaffold)
	operations := fixture.operations(executionPlan{
		files: []planner.FileAction{file},
		prune: []planner.PruneAction{prune},
	})
	pruneExecuted := false
	operations.execute = func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error) {
		return executor.FileResult{StateEffect: file.OnFailure}, executor.ErrPreconditionMismatch
	}
	operations.pruneExecute = func(paths.ControlPlanePaths, planner.PruneAction) (executor.PruneResult, error) {
		pruneExecuted = true
		return executor.PruneResult{StateEffect: prune.OnSuccess}, nil
	}

	result, err := operations.run(Options{})
	if err != nil {
		t.Fatalf("runWithOperations() error = %v, want downgraded conflict", err)
	}
	if pruneExecuted || result.UnresolvedConflicts() != 1 || !result.PruneDeferred() || result.StateCommitted() ||
		fixture.loaded.commitCalls != 0 {
		t.Fatalf("run result = %#v pruneExecuted=%t commitCalls=%d", result, pruneExecuted, fixture.loaded.commitCalls)
	}
	if got := result.FileOutcomes(); len(got) != 1 || got[0].Status != ActionConflict || got[0].Target != file.Target {
		t.Fatalf("file outcomes = %#v, want exact conflict", got)
	}
	if got := result.PruneOutcomes(); len(got) != 1 || got[0].Status != ActionDeferred || got[0].Target != prune.Target {
		t.Fatalf("prune outcomes = %#v, want exact deferred", got)
	}
}

func TestRun_PruneOutcomesMarkUnattemptedSuffixDeferred(t *testing.T) {
	fixture := newRunSeamFixture(t)
	fixture.loadBaseline(t, `{
  "version":1,
  "entries":{
    "~/.first":{"module":"old","kind":"scaffold","source":"modules/old/first.template","applied_at":"2026-07-19T00:00:00Z"},
    "~/.second":{"module":"old","kind":"symlink","source":"modules/old/second","link_dest":"/old/second","applied_at":"2026-07-19T00:00:00Z"},
    "~/.third":{"module":"old","kind":"symlink","source":"modules/old/third","link_dest":"/old/third","applied_at":"2026-07-19T00:00:00Z"}
  },
  "run_once":{}
}`)
	first := seamPruneAction(t, fixture.loaded.controlPaths, "~/.first", planner.PruneReasonScaffold)
	second := seamPruneAction(t, fixture.loaded.controlPaths, "~/.second", planner.PruneReasonOwned)
	third := seamPruneAction(t, fixture.loaded.controlPaths, "~/.third", planner.PruneReasonUnowned)
	operations := fixture.operations(executionPlan{prune: []planner.PruneAction{first, second, third}})
	operations.pruneExecute = func(_ paths.ControlPlanePaths, action planner.PruneAction) (executor.PruneResult, error) {
		if action.Target == first.Target {
			return executor.PruneResult{StateEffect: action.OnSuccess}, nil
		}
		return executor.PruneResult{StateEffect: action.OnFailure}, executor.ErrPreconditionMismatch
	}

	result, err := operations.run(Options{})
	if err != nil {
		t.Fatalf("runWithOperations() error = %v", err)
	}
	want := []PruneOutcome{
		{Index: 0, Target: first.Target, Status: ActionSucceeded},
		{Index: 1, Target: second.Target, Status: ActionConflict},
		{Index: 2, Target: third.Target, Status: ActionDeferred},
	}
	if !result.ActionOutcomesReady() || !reflect.DeepEqual(result.PruneOutcomes(), want) {
		t.Fatalf("prune outcomes ready=%t got=%#v want=%#v", result.ActionOutcomesReady(), result.PruneOutcomes(), want)
	}
}

func TestRun_RejectsExecutionResultsThatContradictActionClass(t *testing.T) {
	type effectChoice uint8
	const (
		failureEffect effectChoice = iota
		successEffect
		unknownEffect
	)

	tests := []struct {
		name          string
		stateOnly     bool
		targetMutated bool
		effect        effectChoice
		executeErr    error
	}{
		{
			name:   "target success without commit",
			effect: successEffect,
		},
		{
			name:          "state-only reports target commit",
			stateOnly:     true,
			targetMutated: true,
			effect:        successEffect,
		},
		{
			name:       "state-only success with error",
			stateOnly:  true,
			effect:     successEffect,
			executeErr: errors.New("state-only executor failure"),
		},
		{
			name:          "target commit with failure effect",
			targetMutated: true,
			effect:        failureEffect,
			executeErr:    errors.New("executor failure"),
		},
		{
			name:   "failure effect without error",
			effect: failureEffect,
		},
		{
			name:   "unknown state effect",
			effect: unknownEffect,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunSeamFixture(t)
			action := seamLinkAction("~/.result-contract")
			if test.stateOnly {
				action = seamLinkAdoptAction("~/.result-contract")
			}
			var effect planner.StateEffect
			switch test.effect {
			case failureEffect:
				effect = action.OnFailure
			case successEffect:
				effect = action.OnSuccess
			case unknownEffect:
				effect = planner.StateEffect{Kind: planner.StateEffectKind("future")}
			default:
				t.Fatalf("test effect choice = %d, want closed test enum", test.effect)
			}
			operations := fixture.operations(executionPlan{files: []planner.FileAction{action}})
			operations.execute = func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error) {
				return executor.FileResult{
					StateEffect:   effect,
					TargetMutated: test.targetMutated,
				}, test.executeErr
			}

			result, err := operations.run(Options{})
			if !errors.Is(err, ErrExecutionProtocol) {
				t.Fatalf("runWithOperations() error = %v, want ErrExecutionProtocol", err)
			}
			wantTargetCommits := 0
			if test.targetMutated {
				wantTargetCommits = 1
			}
			if result.FileAttempts() != 1 || result.TargetCommits() != wantTargetCommits ||
				result.AdoptionEffects() != 0 || result.StateCommitted() || fixture.loaded.commitCalls != 0 {
				t.Fatalf("inconsistent result = %#v commitCalls=%d", result, fixture.loaded.commitCalls)
			}
		})
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
	if first.FileAttempts() != 2 || first.TargetCommits() != 2 || first.StateCommitted() ||
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
	if second.FileAttempts() != 2 || second.AdoptionEffects() != 2 || second.TargetCommits() != 0 || !second.StateCommitted() {
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
	if third.FileAttempts() != 0 || third.AdoptionEffects() != 0 || third.TargetCommits() != 0 || third.StateCommitted() {
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

func TestRunWithMutationSession_ConsumesExistingOwnershipWithoutSecondLock(t *testing.T) {
	fixture := newRunIntegrationFixture(t)
	options := fixture.options()
	session, err := dotruntime.BeginMutation(options.Runtime)
	if err != nil {
		t.Fatalf("BeginMutation() error = %v", err)
	}

	result, err := RunWithMutationSession(options, session)
	if err != nil {
		t.Fatalf("RunWithMutationSession() error = %v", err)
	}
	if result.FileAttempts() != 2 || result.TargetCommits() != 2 || !result.StateCommitted() {
		t.Fatalf("RunWithMutationSession() result = %#v", result)
	}
	assertRunTargets(t, fixture)
	if err := session.Close(); !errors.Is(err, dotruntime.ErrSessionClosed) {
		t.Fatalf("consumed session Close() error = %v, want ErrSessionClosed", err)
	}

	second, err := dotruntime.BeginMutation(options.Runtime)
	if err != nil {
		t.Fatalf("BeginMutation() after consumed session error = %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second session Close() error = %v", err)
	}
}

func TestRunWithMutationSession_RejectsNilAndClosesAlreadyLoadedSession(t *testing.T) {
	if _, err := RunWithMutationSession(Options{}, nil); !errors.Is(err, ErrExecutionProtocol) {
		t.Fatalf("RunWithMutationSession(nil) error = %v, want ErrExecutionProtocol", err)
	}

	fixture := newRunIntegrationFixture(t)
	options := fixture.options()
	session, err := dotruntime.BeginMutation(options.Runtime)
	if err != nil {
		t.Fatalf("BeginMutation() error = %v", err)
	}
	if _, err := session.Load(); err != nil {
		t.Fatalf("MutationSession.Load() error = %v", err)
	}
	if _, err := RunWithMutationSession(options, session); !errors.Is(err, dotruntime.ErrSessionOrder) {
		t.Fatalf("RunWithMutationSession(already loaded) error = %v, want ErrSessionOrder", err)
	}
	if err := session.Close(); !errors.Is(err, dotruntime.ErrSessionClosed) {
		t.Fatalf("already loaded session Close() error = %v, want ErrSessionClosed", err)
	}
}

func TestRun_RejectsSelfTraversingEffectiveTargetBeforeExecutor(t *testing.T) {
	tests := []struct {
		name        string
		includeGood bool
		modules     []string
	}{
		{
			name:        "partial scope cannot hide invalid module",
			includeGood: true,
			modules:     []string{"good"},
		},
		{
			name: "S1b state-only cannot bypass global topology",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunSelfTraversalFixture(t, test.includeGood)
			options := fixture.options()
			options.Modules = test.modules

			for attempt := 1; attempt <= 2; attempt++ {
				result, err := Run(options)
				if !errors.Is(err, paths.ErrTargetOverlap) ||
					!strings.Contains(err.Error(), "module \"bad\"") ||
					!strings.Contains(err.Error(), "traverses its own leaf") {
					t.Fatalf("Run() attempt %d error = %v, want full-profile self-traversal rejection", attempt, err)
				}
				if result.FileAttempts() != 0 || result.TargetCommits() != 0 ||
					result.AdoptionEffects() != 0 || result.StateCommitted() {
					t.Fatalf("Run() attempt %d result = %#v, want zero mutation", attempt, result)
				}
				if destination, readErr := os.Readlink(fixture.bridge); readErr != nil || destination != "real" {
					t.Fatalf("Run() attempt %d bridge = (%q, %v), want unchanged", attempt, destination, readErr)
				}
				if _, statErr := os.Lstat(fixture.goodTarget); !errors.Is(statErr, fs.ErrNotExist) {
					t.Fatalf("Run() attempt %d good target Lstat error = %v, want missing", attempt, statErr)
				}
				if _, statErr := os.Lstat(fixture.stateFile); !errors.Is(statErr, fs.ErrNotExist) {
					t.Fatalf("Run() attempt %d state Lstat error = %v, want missing", attempt, statErr)
				}
			}
		})
	}
}

func TestRun_RejectsUnsafeCandidateStateTopologyBeforeExecutor(t *testing.T) {
	fixture := newRunCandidateTopologyFixture(t)
	target := filepath.Join(fixture.home, "parent")
	writeRunFile(t, filepath.Join(fixture.repository, "modules", "app", "parent"), "link source\n")
	writeRunFile(t, fixture.stateFile, `{
  "version": 1,
  "entries": {
    "~/parent/child": {
      "module": "legacy",
      "kind": "scaffold",
      "source": "modules/legacy/child.template",
      "applied_at": "2026-07-19T00:00:00Z"
    }
  },
  "run_once": {}
}`)
	stateBefore, err := os.ReadFile(fixture.stateFile)
	if err != nil {
		t.Fatalf("os.ReadFile(state before Run) error = %v", err)
	}
	metadataBefore := snapshotRunPathMetadata(t, fixture.stateFile)

	for attempt := 1; attempt <= 2; attempt++ {
		result, runErr := Run(fixture.options())
		if !errors.Is(runErr, paths.ErrTargetOverlap) ||
			errors.Is(runErr, state.ErrPathValidation) ||
			!strings.Contains(runErr.Error(), "target mutation") ||
			!strings.Contains(runErr.Error(), "persisted state target") {
			t.Fatalf("Run() attempt %d error = %v, want planner target overlap", attempt, runErr)
		}
		if result.FileAttempts() != 0 || result.TargetCommits() != 0 ||
			result.AdoptionEffects() != 0 || result.StateCommitted() {
			t.Fatalf("Run() attempt %d result = %#v, want zero mutation", attempt, result)
		}
		if _, statErr := os.Lstat(target); !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatalf("Run() attempt %d target Lstat error = %v, want missing", attempt, statErr)
		}
		stateAfter, readErr := os.ReadFile(fixture.stateFile)
		if readErr != nil {
			t.Fatalf("os.ReadFile(state after Run %d) error = %v", attempt, readErr)
		}
		if string(stateAfter) != string(stateBefore) {
			t.Fatalf("Run() attempt %d changed state bytes", attempt)
		}
		assertRunPathMetadataUnchanged(
			t,
			fixture.stateFile,
			metadataBefore,
			snapshotRunPathMetadata(t, fixture.stateFile),
		)
	}
}

func TestRun_RejectsMatchedAliasCandidateStateTopologyBeforeExecutor(t *testing.T) {
	fixture := newRunCandidateTopologyFixture(t)
	writeRunFile(t, filepath.Join(fixture.repository, "modules", "app", "dot.toml"), `target = "~"
[files.alias]
kind = "scaffold"
`)
	realRoot := filepath.Join(fixture.home, "real")
	if err := os.MkdirAll(realRoot, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(real root) error = %v", err)
	}
	if err := os.Symlink("real", filepath.Join(fixture.home, "alias")); err != nil {
		t.Fatalf("os.Symlink(alias) error = %v", err)
	}
	writeRunFile(t, filepath.Join(realRoot, "child"), "user data\n")
	writeRunFile(t, filepath.Join(fixture.repository, "modules", "app", "00"), "first link\n")
	writeRunFile(t, filepath.Join(fixture.repository, "modules", "app", "alias"), "scaffold\n")
	writeRunFile(t, filepath.Join(fixture.repository, "modules", "app", "real", "child"), "wanted link\n")
	writeRunFile(t, fixture.stateFile, `{
  "version": 1,
  "entries": {
    "~/alias/child": {
      "module": "app",
      "kind": "scaffold",
      "source": "modules/app/old-child.template",
      "applied_at": "2026-07-19T00:00:00Z"
    }
  },
  "run_once": {}
}`)
	stateBefore, err := os.ReadFile(fixture.stateFile)
	if err != nil {
		t.Fatalf("os.ReadFile(state before Run) error = %v", err)
	}
	metadataBefore := snapshotRunPathMetadata(t, fixture.stateFile)
	firstTarget := filepath.Join(fixture.home, "00")

	for attempt := 1; attempt <= 2; attempt++ {
		result, runErr := Run(fixture.options())
		if !errors.Is(runErr, paths.ErrTargetOverlap) ||
			errors.Is(runErr, state.ErrPathValidation) ||
			!strings.Contains(runErr.Error(), "file state upsert") ||
			!strings.Contains(runErr.Error(), "~/alias/child") {
			t.Fatalf("Run() attempt %d error = %v, want matched-history target overlap", attempt, runErr)
		}
		if result.FileAttempts() != 0 || result.TargetCommits() != 0 ||
			result.AdoptionEffects() != 0 || result.StateCommitted() {
			t.Fatalf("Run() attempt %d result = %#v, want zero mutation", attempt, result)
		}
		if _, statErr := os.Lstat(firstTarget); !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatalf("Run() attempt %d first target Lstat error = %v, want missing", attempt, statErr)
		}
		stateAfter, readErr := os.ReadFile(fixture.stateFile)
		if readErr != nil {
			t.Fatalf("os.ReadFile(state after Run %d) error = %v", attempt, readErr)
		}
		if string(stateAfter) != string(stateBefore) {
			t.Fatalf("Run() attempt %d changed state bytes", attempt)
		}
		assertRunPathMetadataUnchanged(
			t,
			fixture.stateFile,
			metadataBefore,
			snapshotRunPathMetadata(t, fixture.stateFile),
		)
	}
}

func TestRun_RejectsSelfTraversingPersistedStateBeforePlanning(t *testing.T) {
	fixture := newRunCandidateTopologyFixture(t)
	if err := os.MkdirAll(filepath.Join(fixture.home, "real"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(real) error = %v", err)
	}
	bridge := filepath.Join(fixture.home, "bridge")
	if err := os.Symlink("real", bridge); err != nil {
		t.Fatalf("os.Symlink(bridge) error = %v", err)
	}
	if err := os.Symlink(filepath.FromSlash("bridge/.."), filepath.Join(fixture.home, "detour")); err != nil {
		t.Fatalf("os.Symlink(detour) error = %v", err)
	}
	writeRunFile(t, filepath.Join(fixture.repository, "modules", "app", "bridge"), "new link source\n")
	writeRunFile(t, fixture.stateFile, `{
  "version": 1,
  "entries": {
    "~/detour/bridge": {
      "module": "app",
      "kind": "symlink",
      "source": "modules/app/old-bridge",
      "link_dest": "real",
      "applied_at": "2026-07-19T00:00:00Z"
    }
  },
  "run_once": {}
}`)
	stateBefore, err := os.ReadFile(fixture.stateFile)
	if err != nil {
		t.Fatalf("os.ReadFile(state before Run) error = %v", err)
	}
	metadataBefore := snapshotRunPathMetadata(t, fixture.stateFile)
	storeErr := errors.New("injected state publish failure")
	storeCalls := 0
	operations := defaultRunOperations()
	operations.begin = func(overrides dotruntime.Overrides) (mutationSession, error) {
		session, beginErr := dotruntime.BeginMutationWithStateStore(
			overrides,
			func(root, path string, snapshot state.Snapshot) error {
				storeCalls++
				return state.StoreWithPublisher(root, path, snapshot, func(string, string) error {
					return storeErr
				})
			},
		)
		if beginErr != nil {
			return nil, beginErr
		}
		return runtimeMutationSession{session: session}, nil
	}

	result, runErr := runWithOperations(fixture.options(), operations)
	if !errors.Is(runErr, state.ErrPathValidation) ||
		!errors.Is(runErr, paths.ErrTargetOverlap) ||
		errors.Is(runErr, storeErr) ||
		!strings.Contains(runErr.Error(), "state target") ||
		!strings.Contains(runErr.Error(), "traverses its own leaf") {
		t.Fatalf("runWithOperations() error = %v, want strict state self-traversal rejection", runErr)
	}
	if result.FileAttempts() != 0 || result.TargetCommits() != 0 ||
		result.AdoptionEffects() != 0 || result.StateCommitted() || storeCalls != 0 {
		t.Fatalf("run result = %#v storeCalls=%d, want zero mutation/Store", result, storeCalls)
	}
	if destination, readErr := os.Readlink(bridge); readErr != nil || destination != "real" {
		t.Fatalf("bridge destination = (%q, %v), want original directory link", destination, readErr)
	}
	stateAfter, err := os.ReadFile(fixture.stateFile)
	if err != nil {
		t.Fatalf("os.ReadFile(state after Run) error = %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatal("rejected Run changed state bytes")
	}
	assertRunPathMetadataUnchanged(
		t,
		fixture.stateFile,
		metadataBefore,
		snapshotRunPathMetadata(t, fixture.stateFile),
	)

	repeated, repeatedErr := Run(fixture.options())
	if !errors.Is(repeatedErr, state.ErrPathValidation) || !errors.Is(repeatedErr, paths.ErrTargetOverlap) {
		t.Fatalf("repeated Run() error = %v, want same strict state target overlap", repeatedErr)
	}
	if repeated.FileAttempts() != 0 || repeated.TargetCommits() != 0 || repeated.StateCommitted() {
		t.Fatalf("repeated Run() result = %#v, want zero mutation", repeated)
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
	if err != nil {
		t.Fatalf("runWithOperations() error = %v, want downgraded conflict", err)
	}
	if result.FileAttempts() != 2 || result.TargetCommits() != 1 || result.UnresolvedConflicts() != 1 ||
		!result.StateCommitted() {
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
	if got.err != nil || !got.result.StateCommitted() {
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

type executionPlan struct {
	files  []planner.FileAction
	prune  []planner.PruneAction
	groups []planner.PruneConfirmationGroup
	hooks  []planner.HookAction
}

type seamOperations struct {
	runOperations
	fixture runSeamFixture
	plan    executionPlan
}

func (operations seamOperations) run(options Options) (Result, error) {
	result := Result{}
	if err := validateExecutionScope(operations.plan.files, operations.plan.prune, operations.plan.hooks); err != nil {
		return result, errors.Join(err, operations.fixture.session.close())
	}
	err := runExecution(
		options,
		operations.fixture.loaded,
		executionScope{
			files:  operations.plan.files,
			prune:  operations.plan.prune,
			groups: operations.plan.groups,
			hooks:  operations.plan.hooks,
		},
		operations.runOperations,
		&result,
	)
	return result, errors.Join(err, operations.fixture.session.close())
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

func (fixture runSeamFixture) operations(plan executionPlan) seamOperations {
	return seamOperations{
		fixture: fixture,
		plan:    plan,
		runOperations: runOperations{
			execute: func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error) {
				return executor.FileResult{}, nil
			},
			pruneExecute: func(paths.ControlPlanePaths, planner.PruneAction) (executor.PruneResult, error) {
				return executor.PruneResult{}, nil
			},
			executeHook: func(planner.HookAction, executor.HookStreams) (executor.HookResult, error) {
				return executor.HookResult{}, nil
			},
			now: func() time.Time { return time.Date(2026, 7, 20, 1, 2, 3, 0, time.UTC) },
		},
	}
}

func (fixture runSeamFixture) loadBaseline(t *testing.T, raw string) {
	t.Helper()
	path := filepath.Join(fixture.loaded.controlPaths.EffectiveHome(), "baseline.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("os.WriteFile(baseline) error = %v", err)
	}
	loaded, err := state.Load(path)
	if err != nil {
		t.Fatalf("state.Load(baseline) error = %v", err)
	}
	fixture.loaded.baselineState = loaded
}

type fakeMutationSession struct {
	loaded   *fakeLoadedMutation
	loadErr  error
	closeErr error
	closed   bool
}

func (session *fakeMutationSession) load() (loadedMutation, error) {
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

func seamHookAction(fixture runSeamFixture, key string, verb planner.HookVerb) planner.HookAction {
	module, script, found := strings.Cut(key, "/")
	if !found {
		panic("seam hook key must contain a module")
	}
	home := fixture.loaded.controlPaths.EffectiveHome()
	repository := fixture.loaded.controlPaths.Repository()
	modulePath := filepath.Join(repository, "modules", module)
	scriptPath := filepath.Join(modulePath, filepath.FromSlash(script))
	content := []byte("printf seam-hook\n")
	fingerprint := planner.HookFingerprint(planner.HookExecutionShell, content)
	action := planner.HookAction{
		Verb:           verb,
		StateKey:       key,
		Module:         module,
		Script:         script,
		ScriptPath:     scriptPath,
		WorkingDir:     modulePath,
		TargetRoot:     "~",
		TargetRootPath: home,
		Profile:        "all",
		GOOS:           runtime.GOOS,
		Repository:     repository,
		Invocation: planner.HookInvocation{
			Mode: planner.HookExecutionShell, Program: "sh", Arguments: []string{scriptPath},
		},
		Environment: planner.HookEnvironment{
			Home:          home,
			XDGConfigHome: filepath.Join(home, ".config"),
			XDGStateHome:  filepath.Join(home, ".local", "state"),
			XDGDataHome:   filepath.Join(home, ".local", "share"),
			DotModule:     module,
			DotOS:         runtime.GOOS,
			DotProfile:    "all",
			DotRepo:       repository,
			DotTarget:     home,
		},
		Fingerprint: fingerprint,
		OnSuccess: planner.HookStateEffect{
			Kind: planner.HookStateUpsert, Key: key, Fingerprint: fingerprint,
		},
		OnFailure: planner.HookStateEffect{Kind: planner.HookStatePreserve},
	}
	if verb == planner.HookSkip {
		action.OnSuccess = planner.HookStateEffect{Kind: planner.HookStatePreserve}
	}
	return action
}

func seamPruneAction(
	t *testing.T,
	control paths.ControlPlanePaths,
	key string,
	reason planner.PruneReason,
) planner.PruneAction {
	t.Helper()
	targetPath := filepath.Join(control.EffectiveHome(), filepath.Base(key))
	resolution, err := paths.ResolveTarget(targetPath)
	if err != nil {
		t.Fatalf("paths.ResolveTarget(prune) error = %v", err)
	}
	action := planner.PruneAction{
		Mode:   planner.PruneStateOnly,
		Target: key,
		Module: "old",
		Reason: reason,
		Precondition: planner.Precondition{
			TargetPath: targetPath, TargetResolution: resolution, Leaf: planner.LeafCondition{Kind: planner.LeafAny},
		},
		OnSuccess: planner.StateEffect{Kind: planner.StateDelete, Key: key},
		OnFailure: planner.StateEffect{Kind: planner.StatePreserve},
	}
	switch reason {
	case planner.PruneReasonUnowned:
		action.Warning = true
		action.Precondition.Leaf = planner.LeafCondition{Kind: planner.LeafNotOwnedSymlink, LinkDest: "/old"}
	case planner.PruneReasonOwned:
		action.Mode = planner.PruneTargetAndState
		action.Precondition.Leaf = planner.LeafCondition{Kind: planner.LeafExactSymlink, LinkDest: "/old/second"}
	}
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

type runCandidateTopologyFixture struct {
	home       string
	repository string
	stateFile  string
}

type runSelfTraversalFixture struct {
	home       string
	repository string
	stateFile  string
	bridge     string
	goodTarget string
}

func newRunSelfTraversalFixture(t *testing.T, includeGood bool) runSelfTraversalFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	isolateRunMutationEnvironment(t, root, home, repository)
	if err := os.MkdirAll(filepath.Join(home, "real"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(real) error = %v", err)
	}
	bridge := filepath.Join(home, "bridge")
	if err := os.Symlink("real", bridge); err != nil {
		t.Fatalf("os.Symlink(bridge) error = %v", err)
	}
	if err := os.Symlink(filepath.FromSlash("bridge/.."), filepath.Join(home, "detour")); err != nil {
		t.Fatalf("os.Symlink(detour) error = %v", err)
	}

	writeRunFile(t, filepath.Join(home, ".config", "dot", "config.toml"), "profile = \"all\"\n")
	profileModules := "all = [\"bad\"]"
	if includeGood {
		profileModules = "all = [\"bad\", \"good\"]"
	}
	writeRunFile(t, filepath.Join(repository, "dot.toml"), `[profiles]
`+profileModules+"\n")
	writeRunFile(t, filepath.Join(repository, "modules", "bad", "dot.toml"), `target = "~/detour"
[files.bridge]
kind = "scaffold"
`)
	writeRunFile(t, filepath.Join(repository, "modules", "bad", "bridge"), "scaffold content\n")
	if includeGood {
		writeRunFile(t, filepath.Join(repository, "modules", "good", "dot.toml"), "target = \"~\"\n")
		writeRunFile(t, filepath.Join(repository, "modules", "good", "good"), "link source\n")
	}
	return runSelfTraversalFixture{
		home:       home,
		repository: repository,
		stateFile:  filepath.Join(home, ".local", "state", "dot", "state.json"),
		bridge:     bridge,
		goodTarget: filepath.Join(home, "good"),
	}
}

func (fixture runSelfTraversalFixture) options() Options {
	return Options{
		Runtime: dotruntime.Overrides{
			Home:       dotruntime.Override{Value: fixture.home, Set: true},
			Repository: dotruntime.Override{Value: fixture.repository, Set: true},
		},
		NoPrune: true,
	}
}

func newRunCandidateTopologyFixture(t *testing.T) runCandidateTopologyFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	isolateRunMutationEnvironment(t, root, home, repository)
	writeRunFile(t, filepath.Join(home, ".config", "dot", "config.toml"), "profile = \"all\"\n")
	writeRunFile(t, filepath.Join(repository, "dot.toml"), `[profiles]
all = ["app"]
`)
	writeRunFile(t, filepath.Join(repository, "modules", "app", "dot.toml"), `target = "~"
`)
	return runCandidateTopologyFixture{
		home:       home,
		repository: repository,
		stateFile:  filepath.Join(home, ".local", "state", "dot", "state.json"),
	}
}

func (fixture runCandidateTopologyFixture) options() Options {
	return Options{
		Runtime: dotruntime.Overrides{
			Home:       dotruntime.Override{Value: fixture.home, Set: true},
			Repository: dotruntime.Override{Value: fixture.repository, Set: true},
		},
		NoPrune: true,
	}
}

func newRunIntegrationFixture(t *testing.T) runIntegrationFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	isolateRunMutationEnvironment(t, root, home, repository)
	writeRunFile(t, filepath.Join(home, ".config", "dot", "config.toml"), "profile = \"all\"\n")
	writeRunFile(t, filepath.Join(repository, "dot.toml"), `[profiles]
all = ["app"]
`)
	writeRunFile(t, filepath.Join(repository, "modules", "app", "dot.toml"), `target = "~"
[files.config]
kind = "scaffold"
mode = "0600"
`)
	writeRunFile(t, filepath.Join(repository, "modules", "app", "zshrc"), "link source\n")
	writeRunFile(t, filepath.Join(repository, "modules", "app", "config"), "scaffold content\n")
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
		NoPrune: true,
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
