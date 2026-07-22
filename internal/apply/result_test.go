package apply

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/planner"
)

func TestResult_HookOutcomesAreSealedAndSecondRunIsIdempotent(t *testing.T) {
	fixture := newRunIntegrationFixture(t)
	writeRunFile(t, filepath.Join(fixture.repository, "modules", "app", "dot.toml"), `target = "~"
[hooks]
run_once = ["hooks/first.sh", "hooks/second.sh"]
[files."config.template"]
kind = "scaffold"
mode = "0600"
`)
	writeRunFile(t, filepath.Join(fixture.repository, "modules", "app", "hooks", "first.sh"),
		"printf 'first\\n' >> \"$HOME/hook-log\"\n")
	writeRunFile(t, filepath.Join(fixture.repository, "modules", "app", "hooks", "second.sh"),
		"printf 'second\\n' >> \"$HOME/hook-log\"\n")
	options := fixture.options()
	rejected, err := Run(options)
	if !errors.Is(err, ErrExecutionProtocol) || !rejected.Valid(true) {
		t.Fatalf("Run() without hook streams = (%#v, %v), want planned protocol rejection", rejected, err)
	}
	for _, path := range []string{fixture.linkTarget, fixture.scaffoldTarget, fixture.stateFile, filepath.Join(fixture.home, "hook-log")} {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatalf("hook stream preflight changed %q: %v", path, statErr)
		}
	}
	options.Stdin = strings.NewReader("")
	options.Stdout = io.Discard
	options.Stderr = io.Discard

	valid, err := Run(options)
	if err != nil || !valid.Valid(false) || valid.HookAttempts() != 2 || valid.HookEffects() != 2 ||
		!valid.StateCommitted() {
		t.Fatalf("first hook Run() = (%#v, %v)", valid, err)
	}
	wantLog := "first\nsecond\n"
	logPath := filepath.Join(fixture.home, "hook-log")
	if content, readErr := os.ReadFile(logPath); readErr != nil || string(content) != wantLog {
		t.Fatalf("hook log = %q, %v", content, readErr)
	}

	clone := func() Result {
		result := valid
		result.fileOutcomes = append([]FileOutcome(nil), valid.fileOutcomes...)
		result.pruneOutcomes = append([]PruneOutcome(nil), valid.pruneOutcomes...)
		result.hookOutcomes = append([]HookOutcome(nil), valid.hookOutcomes...)
		return result
	}
	mutations := []struct {
		name            string
		hasRuntimeError bool
		mutate          func(*Result)
	}{
		{name: "wrong key", mutate: func(result *Result) { result.hookOutcomes[0].StateKey = "app/forged" }},
		{name: "success not attempted", mutate: func(result *Result) { result.hookOutcomes[0].attempted = false }},
		{name: "deferred without failure", hasRuntimeError: true, mutate: func(result *Result) {
			result.hookOutcomes[1] = HookOutcome{Index: 1, StateKey: result.hookOutcomes[1].StateKey, Status: ActionDeferred}
		}},
		{name: "suffix ran after failure", hasRuntimeError: true, mutate: func(result *Result) {
			result.hookOutcomes[0].Status = ActionFailed
			result.hookOutcomes[0].stateEffectReady = false
		}},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			result := clone()
			test.mutate(&result)
			if result.Valid(test.hasRuntimeError) {
				t.Fatalf("inconsistent hook result unexpectedly valid: %#v", result)
			}
		})
	}

	view := valid.HookOutcomes()
	view[0].StateKey = "app/forged"
	if reflect.DeepEqual(view, valid.HookOutcomes()) || !valid.Valid(false) {
		t.Fatal("HookOutcomes() leaked mutable storage or invalidated sealed result")
	}

	options.Stdin = strings.NewReader("")
	second, err := Run(options)
	wantSecond := []HookOutcome{
		{Index: 0, StateKey: "app/hooks/first.sh", Status: ActionSkipped},
		{Index: 1, StateKey: "app/hooks/second.sh", Status: ActionSkipped},
	}
	if err != nil || !second.Valid(false) || second.HookAttempts() != 0 || second.HookEffects() != 0 ||
		second.StateCommitted() || !reflect.DeepEqual(second.HookOutcomes(), wantSecond) {
		t.Fatalf("second hook Run() = (%#v, %v)", second, err)
	}
	if content, readErr := os.ReadFile(logPath); readErr != nil || string(content) != wantLog {
		t.Fatalf("idempotent hook log = %q, %v", content, readErr)
	}
	for index := range second.hookOutcomes {
		forged := second
		forged.hookOutcomes = append([]HookOutcome(nil), second.hookOutcomes...)
		forged.hookOutcomes[index].attempted = true
		if forged.Valid(false) {
			t.Fatalf("attempted HookSkip %d unexpectedly valid", index)
		}
	}
}

func TestResult_SealsExecutionFactsAndReturnsDetachedViews(t *testing.T) {
	fixture := newRunIntegrationFixture(t)
	result, err := Run(fixture.options())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Valid(false) || !result.ActionOutcomesReady() || !result.StateCommitted() {
		t.Fatalf("Run() result = %#v, want valid committed execution", result)
	}

	files := result.FileOutcomes()
	if len(files) == 0 {
		t.Fatal("Run() returned no executable file outcomes")
	}
	wantTarget := files[0].Target
	files[0].Target = "~/forged"
	if got := result.FileOutcomes(); len(got) == 0 || got[0].Target != wantTarget {
		t.Fatalf("FileOutcomes() leaked mutable storage: %#v", got)
	}

	plan := result.Plan()
	plannedFiles := plan.FileActions()
	plannedFiles[0].Target = "~/forged-plan"
	if got := result.Plan().FileActions()[0].Target; got == "~/forged-plan" {
		t.Fatal("Plan() leaked mutable file action storage")
	}
	if !result.Valid(false) {
		t.Fatal("mutating accessor copies invalidated sealed result")
	}
}

func TestResult_ValidRejectsInconsistentProtocolFacts(t *testing.T) {
	fixture := newRunIntegrationFixture(t)
	valid, err := Run(fixture.options())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !valid.Valid(false) {
		t.Fatalf("Run() result = %#v, want valid", valid)
	}

	clone := func() Result {
		result := valid
		result.fileOutcomes = append([]FileOutcome(nil), valid.fileOutcomes...)
		result.pruneOutcomes = append([]PruneOutcome(nil), valid.pruneOutcomes...)
		return result
	}
	tests := []struct {
		name            string
		hasRuntimeError bool
		mutate          func(*Result)
	}{
		{name: "missing seal", mutate: func(result *Result) { result.seal = nil }},
		{name: "wrong file target", mutate: func(result *Result) { result.fileOutcomes[0].Target = "~/wrong" }},
		{name: "duplicate file index", mutate: func(result *Result) { result.fileOutcomes[1].Index = result.fileOutcomes[0].Index }},
		{name: "failed without error", mutate: func(result *Result) {
			result.fileOutcomes[len(result.fileOutcomes)-1].Status = ActionFailed
		}},
		{name: "state effect without commit", mutate: func(result *Result) { result.stateCommitted = false }},
		{name: "backup on non-backup action", mutate: func(result *Result) { result.fileOutcomes[0].backupPath = "/tmp/forged" }},
		{name: "accepted confirmation was not requested", mutate: func(result *Result) { result.confirmAccepted = true }},
		{name: "planned stage carries outcomes", hasRuntimeError: true, mutate: func(result *Result) { result.stage = resultPlanned }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := clone()
			test.mutate(&result)
			if result.Valid(test.hasRuntimeError) {
				t.Fatalf("inconsistent result unexpectedly valid: %#v", result)
			}
		})
	}

	failedAfterCommit := clone()
	failedAfterCommit.fileOutcomes[len(failedAfterCommit.fileOutcomes)-1].Status = ActionFailed
	if !failedAfterCommit.Valid(true) {
		t.Fatal("post-commit cleanup failure cannot express retained successful state effect")
	}

	planned := newPlannedResult(valid.plan)
	if planned.Valid(false) || !planned.Valid(true) {
		t.Fatalf("planned result validity = no-error:%t error:%t, want false/true", planned.Valid(false), planned.Valid(true))
	}
	if (Result{}).Valid(true) || (Result{}).AdoptionEffects() != 0 {
		t.Fatal("zero Result is trusted or its derived accessor is unsafe")
	}
}

func TestResult_ValidRejectsCrossPhaseContradictions(t *testing.T) {
	fixture := newRunIntegrationFixture(t)
	writeRunFile(t, fixture.stateFile, `{
  "version": 1,
  "entries": {
    "~/orphan-a": {
      "module": "legacy",
      "kind": "scaffold",
      "source": "modules/legacy/orphan-a.template",
      "applied_at": "2026-07-18T00:00:00Z"
    },
    "~/orphan-b": {
      "module": "legacy",
      "kind": "scaffold",
      "source": "modules/legacy/orphan-b.template",
      "applied_at": "2026-07-18T00:00:00Z"
    },
    "~/orphan-c": {
      "module": "legacy",
      "kind": "scaffold",
      "source": "modules/legacy/orphan-c.template",
      "applied_at": "2026-07-18T00:00:00Z"
    }
  },
  "run_once": {}
}`)
	options := fixture.options()
	options.NoPrune = false
	options.Confirm = func(groups []planner.PruneConfirmationGroup) (bool, error) {
		if len(groups) != 1 || groups[0].Module != "legacy" {
			t.Fatalf("confirmation groups = %#v, want legacy", groups)
		}
		return true, nil
	}
	valid, err := Run(options)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !valid.Valid(false) || !valid.confirmRequested || !valid.confirmAccepted ||
		len(valid.pruneOutcomes) != 3 || valid.pruneOutcomes[0].Status != ActionSucceeded {
		t.Fatalf("Run() result = %#v, want confirmed valid prune", valid)
	}
	clone := func() Result {
		result := valid
		result.fileOutcomes = append([]FileOutcome(nil), valid.fileOutcomes...)
		result.pruneOutcomes = append([]PruneOutcome(nil), valid.pruneOutcomes...)
		return result
	}
	tests := []struct {
		name   string
		mutate func(*Result)
	}{
		{name: "prune ran without accepted confirmation", mutate: func(result *Result) {
			result.confirmAccepted = false
		}},
		{name: "prune ran after file conflict", mutate: func(result *Result) {
			last := &result.fileOutcomes[len(result.fileOutcomes)-1]
			last.Status = ActionConflict
			last.targetCommitted = false
			last.stateEffectReady = false
		}},
		{name: "accepted prune was never attempted", mutate: func(result *Result) {
			outcome := &result.pruneOutcomes[0]
			outcome.Status = ActionDeferred
			outcome.attempted = false
			outcome.targetCommitted = false
			outcome.stateEffectReady = false
		}},
		{name: "active prune resumed after deferred stop", mutate: func(result *Result) {
			outcome := &result.pruneOutcomes[1]
			outcome.Status = ActionDeferred
			outcome.attempted = false
			outcome.targetCommitted = false
			outcome.stateEffectReady = false
		}},
		{name: "prune ran without requesting confirmation", mutate: func(result *Result) {
			result.confirmRequested = false
			result.confirmAccepted = false
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := clone()
			test.mutate(&result)
			if result.Valid(false) {
				t.Fatalf("cross-phase contradiction unexpectedly valid: %#v", result)
			}
		})
	}
}

func TestValidFileOutcome_BackupInitializationFailureHasNoPerActionCommit(t *testing.T) {
	action := planner.FileAction{Verb: planner.FileBackupReplace}
	base := FileOutcome{Status: ActionFailed}
	if !validFileOutcome(action, base, true) {
		t.Fatal("backup batch initialization failure is not representable")
	}

	tests := []struct {
		name   string
		mutate func(*FileOutcome)
	}{
		{name: "target committed", mutate: func(outcome *FileOutcome) { outcome.targetCommitted = true }},
		{name: "state effect ready", mutate: func(outcome *FileOutcome) { outcome.stateEffectReady = true }},
		{name: "backup retained", mutate: func(outcome *FileOutcome) { outcome.backupPath = "/tmp/forged" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome := base
			test.mutate(&outcome)
			if validFileOutcome(action, outcome, true) {
				t.Fatalf("unattempted backup action accepted contradictory facts: %#v", outcome)
			}
		})
	}
}
