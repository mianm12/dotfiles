package apply

import (
	"errors"
	"fmt"
	"time"

	"github.com/mianm12/dotfiles/internal/backup"
	"github.com/mianm12/dotfiles/internal/executor"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/planner"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/mianm12/dotfiles/internal/state"
)

// ErrExecutionProtocol 表示 runner 或 executor 的返回值无法按 apply 执行协议解释。
var ErrExecutionProtocol = errors.New("apply execution protocol violation")

// Options 保存内部 M1 link/scaffold runner 的严格 runtime 与 scope 输入。
type Options struct {
	Runtime    dotruntime.Overrides
	CLIVersion string
	Modules    []string
	Force      bool
	NoPrune    bool
	Confirm    ConfirmPrune
}

// ConfirmPrune 请求一次 whole-module prune 汇总确认。accepted=false 表示用户拒绝；error 表示
// 确认 IO 失败。调用方不得在 callback 中执行 target/state mutation。
type ConfirmPrune func([]planner.PruneConfirmationGroup) (accepted bool, err error)

type mutationSession interface {
	load(string) (loadedMutation, error)
	close() error
}

type loadedMutation interface {
	inputs() dotruntime.LoadedInputs
	baseline() state.Loaded
	control() paths.ControlPlanePaths
	commit(state.Snapshot) error
}

type runOperations struct {
	begin         func(dotruntime.Overrides) (mutationSession, error)
	plan          func(dotruntime.LoadedInputs, planner.ApplyScopeOptions) (planner.ApplyPlan, error)
	execute       func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error)
	backup        func(string) (*backup.Batch, error)
	executeBackup func(paths.ControlPlanePaths, planner.FileAction, *backup.Batch) (executor.FileResult, error)
	pruneExecute  func(paths.ControlPlanePaths, planner.PruneAction) (executor.PruneResult, error)
	now           func() time.Time
}

type executionScope struct {
	files  []planner.FileAction
	prune  []planner.PruneAction
	groups []planner.PruneConfirmationGroup
}

// Run 在一个 mutation lock 周期内完成 strict load、exact-input plan、scope gate、file、
// force backup、confirmation、prune execution 和一次 state commit。它不连接 CLI，也不执行 hooks。
func Run(options Options) (Result, error) {
	return runWithOperations(options, defaultRunOperations())
}

func runWithOperations(options Options, operations runOperations) (result Result, resultErr error) {
	if operations.begin == nil || operations.plan == nil || operations.execute == nil ||
		operations.backup == nil || operations.executeBackup == nil ||
		operations.pruneExecute == nil || operations.now == nil {
		return Result{}, fmt.Errorf("%w: apply runner operations are incomplete", ErrExecutionProtocol)
	}
	session, err := operations.begin(options.Runtime)
	if err != nil {
		return Result{}, fmt.Errorf("begin apply mutation: %w", err)
	}
	if session == nil {
		return Result{}, fmt.Errorf("%w: begin returned nil mutation session", ErrExecutionProtocol)
	}
	defer func() {
		resultErr = errors.Join(resultErr, session.close())
		if result.seal == successfulResultSeal && !result.Valid(resultErr != nil) {
			resultErr = errors.Join(resultErr, fmt.Errorf("%w: runner returned inconsistent result", ErrExecutionProtocol))
		}
	}()

	mutation, err := session.load(options.CLIVersion)
	if err != nil {
		return Result{}, fmt.Errorf("load apply mutation inputs: %w", err)
	}
	if mutation == nil {
		return Result{}, fmt.Errorf("%w: load returned nil mutation capability", ErrExecutionProtocol)
	}
	plan, err := operations.plan(mutation.inputs(), planner.ApplyScopeOptions{
		Modules: options.Modules,
		Force:   options.Force,
		NoPrune: options.NoPrune,
	})
	if err != nil {
		return Result{}, fmt.Errorf("plan locked apply inputs: %w", err)
	}
	if !plan.Valid() {
		return Result{}, fmt.Errorf("%w: planner returned an invalid canonical plan", ErrExecutionProtocol)
	}
	result = newPlannedResult(plan)
	files := plan.FileActions()
	prune := plan.Prune().Actions()
	groups := plan.Prune().ConfirmationGroups()
	hooks := plan.Hooks().Actions()
	if err := validateExecutionScope(files, prune, hooks); err != nil {
		return result, err
	}
	resultErr = runExecution(
		options,
		mutation,
		executionScope{files: files, prune: prune, groups: groups},
		operations,
		&result,
	)
	return result, resultErr
}

func validatePruneResult(
	action planner.PruneAction,
	result executor.PruneResult,
	executeErr error,
) (success, failure bool, err error) {
	success = result.StateEffect == action.OnSuccess
	failure = result.StateEffect == action.OnFailure
	if !success && !failure {
		return false, false, fmt.Errorf("%w: prune action %q returned an unknown state effect", ErrExecutionProtocol, action.Target)
	}
	if result.TargetMutated != (success && action.Mode == planner.PruneTargetAndState) {
		return false, false, fmt.Errorf("%w: prune action %q returned inconsistent target commit", ErrExecutionProtocol, action.Target)
	}
	if success && executeErr != nil {
		return false, false, fmt.Errorf("%w: prune action %q returned success with an error", ErrExecutionProtocol, action.Target)
	}
	return success, failure, nil
}

func cloneConfirmationGroups(groups []planner.PruneConfirmationGroup) []planner.PruneConfirmationGroup {
	cloned := append([]planner.PruneConfirmationGroup(nil), groups...)
	for index := range cloned {
		cloned[index].Targets = append([]planner.PruneConfirmationTarget(nil), cloned[index].Targets...)
	}
	return cloned
}

func validateFileResult(
	action planner.FileAction,
	result executor.FileResult,
	executeErr error,
) (success, failure bool, err error) {
	success = result.StateEffect == action.OnSuccess
	failure = result.StateEffect == action.OnFailure
	if !success && !failure {
		return false, false, fmt.Errorf(
			"%w: file action %q returned an unknown state effect",
			ErrExecutionProtocol,
			action.Target,
		)
	}
	if action.Verb == planner.FileBackupReplace {
		if success && result.BackupPath == "" {
			return false, false, fmt.Errorf(
				"%w: backup-replace action %q returned success without a backup path",
				ErrExecutionProtocol,
				action.Target,
			)
		}
	} else if result.BackupPath != "" {
		return false, false, fmt.Errorf(
			"%w: non-backup file action %q reported a backup path",
			ErrExecutionProtocol,
			action.Target,
		)
	}

	switch action.Verb.ExecutionClass() {
	case planner.FileStateOnly:
		if result.TargetMutated {
			return false, false, fmt.Errorf(
				"%w: state-only file action %q reported a target commit",
				ErrExecutionProtocol,
				action.Target,
			)
		}
		if success && executeErr != nil {
			return false, false, fmt.Errorf(
				"%w: state-only file action %q returned success with an error",
				ErrExecutionProtocol,
				action.Target,
			)
		}
	case planner.FileTargetMutation:
		if result.TargetMutated != success {
			return false, false, fmt.Errorf(
				"%w: target-mutation file action %q returned inconsistent commit and state effect",
				ErrExecutionProtocol,
				action.Target,
			)
		}
	default:
		return false, false, fmt.Errorf(
			"%w: file action %q has non-executable class",
			ErrExecutionProtocol,
			action.Target,
		)
	}
	return success, failure, nil
}

func entryUpdate(effect planner.StateEffect, appliedAt time.Time) (state.EntryUpdate, error) {
	if effect.Kind != planner.StateUpsert || effect.Key == "" || effect.Entry.Key != effect.Key {
		return state.EntryUpdate{}, fmt.Errorf("%w: successful file effect is not a complete upsert", ErrExecutionProtocol)
	}
	var kind state.Kind
	switch effect.Entry.Kind {
	case planner.StateSymlink:
		kind = state.KindSymlink
	case planner.StateScaffold:
		kind = state.KindScaffold
	default:
		return state.EntryUpdate{}, fmt.Errorf(
			"%w: successful file effect uses state kind %q",
			ErrExecutionProtocol,
			effect.Entry.Kind,
		)
	}
	return state.EntryUpdate{
		Key:         effect.Key,
		PreviousKey: effect.PreviousKey,
		Module:      effect.Entry.Module,
		Kind:        kind,
		Source:      effect.Entry.Source,
		LinkDest:    effect.Entry.LinkDest,
		AppliedAt:   appliedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

type runtimeMutationSession struct {
	session *dotruntime.MutationSession
}

func (session runtimeMutationSession) load(cliVersion string) (loadedMutation, error) {
	mutation, err := session.session.Load(cliVersion)
	if err != nil {
		return nil, err
	}
	return runtimeLoadedMutation{mutation: mutation}, nil
}

func (session runtimeMutationSession) close() error { return session.session.Close() }

type runtimeLoadedMutation struct {
	mutation *dotruntime.LoadedMutation
}

func (mutation runtimeLoadedMutation) inputs() dotruntime.LoadedInputs {
	return mutation.mutation.Inputs()
}

func (mutation runtimeLoadedMutation) baseline() state.Loaded {
	return mutation.mutation.Inputs().State()
}

func (mutation runtimeLoadedMutation) control() paths.ControlPlanePaths {
	return mutation.mutation.Inputs().Context().Control().Paths()
}

func (mutation runtimeLoadedMutation) commit(snapshot state.Snapshot) error {
	return mutation.mutation.CommitState(snapshot)
}

func defaultRunOperations() runOperations {
	return runOperations{
		begin: func(overrides dotruntime.Overrides) (mutationSession, error) {
			session, err := dotruntime.BeginMutation(overrides)
			if err != nil {
				return nil, err
			}
			return runtimeMutationSession{session: session}, nil
		},
		plan:          planner.PlanLoadedApply,
		execute:       executor.ExecuteFile,
		backup:        backup.NewBatch,
		executeBackup: executor.ExecuteFileWithBackup,
		pruneExecute:  executor.ExecutePrune,
		now:           time.Now,
	}
}
