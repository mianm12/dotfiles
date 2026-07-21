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

// ActionOutcomeStatus 描述 runner 对一个可执行计划动作的实际处置，不让调用方从聚合计数猜测。
type ActionOutcomeStatus string

const (
	// ActionSucceeded 表示动作已成功提交其计划效果。
	ActionSucceeded ActionOutcomeStatus = "succeeded"
	// ActionConflict 表示最终 Precondition 失配，动作未提交且需要用户消解。
	ActionConflict ActionOutcomeStatus = "conflict"
	// ActionDeferred 表示动作因前置收敛或确认门禁未被尝试。
	ActionDeferred ActionOutcomeStatus = "deferred"
	// ActionFailed 表示动作遭遇非 conflict 运行或协议错误。
	ActionFailed ActionOutcomeStatus = "failed"
)

// FileOutcome 以原 file plan 的 index 和 target 标识一次可执行 file 动作的结果。
// 未尝试的可执行后缀保持 deferred；plan-only skip/conflict 不重复记录。
type FileOutcome struct {
	Index  int
	Target string
	Status ActionOutcomeStatus
}

// PruneOutcome 以原 prune plan 的 index 和 target 标识每个 prune 动作的结果。
type PruneOutcome struct {
	Index  int
	Target string
	Status ActionOutcomeStatus
}

// Result 保存内部 runner 的可验证摘要，不定义 CLI 输出或退出码。FileAttempts 统计 executor
// 调用，TargetCommits 统计 executor 报告已越过的 target 提交点，AdoptionEffects 统计已接受的
// adopt OnSuccess effect；这些事实即使最终 state Store 失败也保留。StateCommitted 只表示候选
// state 已成功原子发布。
type Result struct {
	Plan                planner.ApplyPlan
	ActionOutcomesReady bool
	FileOutcomes        []FileOutcome
	PruneOutcomes       []PruneOutcome
	FileAttempts        int
	AdoptionEffects     int
	TargetCommits       int
	PruneAttempts       int
	PruneEffects        int
	PruneCommits        int
	PruneDeferred       bool
	UnresolvedConflicts int
	ConfirmRequested    bool
	ConfirmAccepted     bool
	StateCommitted      bool
	BackupPaths         []string
}

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

type executionPlan struct {
	value  planner.ApplyPlan
	files  []planner.FileAction
	prune  []planner.PruneAction
	groups []planner.PruneConfirmationGroup
	hooks  []planner.HookAction
}

type runOperations struct {
	begin         func(dotruntime.Overrides) (mutationSession, error)
	plan          func(dotruntime.LoadedInputs, planner.ApplyScopeOptions) (executionPlan, error)
	execute       func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error)
	backup        func(string) (*backup.Batch, error)
	executeBackup func(paths.ControlPlanePaths, planner.FileAction, *backup.Batch) (executor.FileResult, error)
	pruneExecute  func(paths.ControlPlanePaths, planner.PruneAction) (executor.PruneResult, error)
	now           func() time.Time
}

// Run 在一个 mutation lock 周期内完成 strict load、exact-input plan、CP5 scope gate、file、
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
	}()

	mutation, err := session.load(options.CLIVersion)
	if err != nil {
		return Result{}, fmt.Errorf("load apply mutation inputs: %w", err)
	}
	if mutation == nil {
		return Result{}, fmt.Errorf("%w: load returned nil mutation capability", ErrExecutionProtocol)
	}
	planned, err := operations.plan(mutation.inputs(), planner.ApplyScopeOptions{
		Modules: options.Modules,
		Force:   options.Force,
		NoPrune: options.NoPrune,
	})
	if err != nil {
		return Result{}, fmt.Errorf("plan locked apply inputs: %w", err)
	}
	result.Plan = planned.value
	if err := validateExecutionScope(planned.files, planned.prune, planned.hooks); err != nil {
		return result, err
	}
	result.ActionOutcomesReady = true
	fileOutcomePositions := make(map[int]int)
	for index, action := range planned.files {
		if action.Verb.ExecutionClass() == planner.FilePlanOnly {
			continue
		}
		fileOutcomePositions[index] = len(result.FileOutcomes)
		result.FileOutcomes = append(result.FileOutcomes, FileOutcome{
			Index: index, Target: action.Target, Status: ActionDeferred,
		})
	}
	result.PruneOutcomes = make([]PruneOutcome, len(planned.prune))
	for index, action := range planned.prune {
		result.PruneOutcomes[index] = PruneOutcome{
			Index: index, Target: action.Target, Status: ActionDeferred,
		}
	}

	updates := make([]state.EntryUpdate, 0, len(planned.files))
	deletes := make([]string, 0, len(planned.prune))
	filesConverged := true
	var backupBatch *backup.Batch
	for index, action := range planned.files {
		if action.Verb.ExecutionClass() == planner.FilePlanOnly {
			continue
		}
		if action.Verb == planner.FileBackupReplace && backupBatch == nil {
			backupBatch, err = operations.backup(mutation.control().BackupRoot())
			if err != nil {
				result.FileOutcomes[fileOutcomePositions[index]].Status = ActionFailed
				resultErr = fmt.Errorf("begin force backup batch: %w", err)
				filesConverged = false
				break
			}
		}
		result.FileAttempts++
		var fileResult executor.FileResult
		var executeErr error
		if action.Verb == planner.FileBackupReplace {
			fileResult, executeErr = operations.executeBackup(mutation.control(), action, backupBatch)
		} else {
			fileResult, executeErr = operations.execute(mutation.control(), action)
		}
		if fileResult.BackupPath != "" {
			result.BackupPaths = append(result.BackupPaths, fileResult.BackupPath)
		}
		if fileResult.TargetMutated {
			result.TargetCommits++
		}

		success, failure, protocolErr := validateFileResult(action, fileResult, executeErr)
		if protocolErr != nil {
			executeErr = errors.Join(executeErr, protocolErr)
		}
		switch {
		case protocolErr != nil:
			// 矛盾结果不能形成 state update；已报告的物理提交留给既定收养路径恢复。
		case success:
			update, updateErr := entryUpdate(action.OnSuccess, operations.now())
			if updateErr != nil {
				executeErr = errors.Join(executeErr, updateErr)
			} else {
				updates = append(updates, update)
				if action.Verb == planner.FileAdopt {
					result.AdoptionEffects++
				}
			}
		case failure:
			if executeErr == nil {
				executeErr = fmt.Errorf(
					"%w: file action %d for %q returned failure effect without error",
					ErrExecutionProtocol,
					index,
					action.Target,
				)
			}
		}
		if executeErr != nil {
			if protocolErr == nil && executor.IsPurePreconditionMismatch(executeErr) {
				result.UnresolvedConflicts++
				result.FileOutcomes[fileOutcomePositions[index]].Status = ActionConflict
			} else {
				result.FileOutcomes[fileOutcomePositions[index]].Status = ActionFailed
				resultErr = fmt.Errorf("execute file action %d for %q: %w", index, action.Target, executeErr)
			}
			filesConverged = false
			break
		}
		result.FileOutcomes[fileOutcomePositions[index]].Status = ActionSucceeded
	}

	activePrune := false
	for _, action := range planned.prune {
		if !action.Deferred {
			activePrune = true
			break
		}
	}
	if !filesConverged || !activePrune {
		result.PruneDeferred = len(planned.prune) > 0
	} else {
		confirmed := true
		if len(planned.groups) > 0 {
			result.ConfirmRequested = true
			if options.Confirm == nil {
				confirmed = false
			} else {
				accepted, confirmErr := options.Confirm(cloneConfirmationGroups(planned.groups))
				if confirmErr != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("confirm whole-module prune: %w", confirmErr))
					confirmed = false
				} else {
					confirmed = accepted
					result.ConfirmAccepted = accepted
				}
			}
		}
		if !confirmed {
			result.PruneDeferred = true
		} else {
			for index, action := range planned.prune {
				if action.Deferred {
					result.PruneDeferred = true
					continue
				}
				result.PruneAttempts++
				pruneResult, pruneErr := operations.pruneExecute(mutation.control(), action)
				if pruneResult.TargetMutated {
					result.PruneCommits++
				}
				success, failure, protocolErr := validatePruneResult(action, pruneResult, pruneErr)
				if protocolErr != nil {
					pruneErr = errors.Join(pruneErr, protocolErr)
				}
				switch {
				case protocolErr != nil:
				case success:
					deletes = append(deletes, action.OnSuccess.Key)
					result.PruneEffects++
				case failure:
					if pruneErr == nil {
						pruneErr = fmt.Errorf("%w: prune action %d for %q returned failure effect without error", ErrExecutionProtocol, index, action.Target)
					}
				}
				if pruneErr != nil {
					if protocolErr == nil && executor.IsPurePreconditionMismatch(pruneErr) {
						result.UnresolvedConflicts++
						result.PruneOutcomes[index].Status = ActionConflict
					} else {
						result.PruneOutcomes[index].Status = ActionFailed
						resultErr = errors.Join(resultErr, fmt.Errorf("execute prune action %d for %q: %w", index, action.Target, pruneErr))
					}
					result.PruneDeferred = true
					break
				}
				result.PruneOutcomes[index].Status = ActionSucceeded
			}
		}
	}

	if len(updates) == 0 && len(deletes) == 0 {
		return result, resultErr
	}
	candidate, changed, transitionErr := state.TransitionEntries(mutation.baseline(), updates, deletes...)
	if transitionErr != nil {
		return result, errors.Join(resultErr, transitionErr)
	}
	if !changed {
		return result, resultErr
	}
	if commitErr := mutation.commit(candidate); commitErr != nil {
		return result, errors.Join(resultErr, fmt.Errorf("commit apply state: %w", commitErr))
	}
	result.StateCommitted = true
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
		plan: func(
			inputs dotruntime.LoadedInputs,
			options planner.ApplyScopeOptions,
		) (executionPlan, error) {
			plan, err := planner.PlanLoadedApply(inputs, options)
			if err != nil {
				return executionPlan{}, err
			}
			return executionPlan{
				value:  plan,
				files:  plan.FileActions(),
				prune:  plan.Prune().Actions(),
				groups: plan.Prune().ConfirmationGroups(),
				hooks:  plan.Hooks().Actions(),
			}, nil
		},
		execute:       executor.ExecuteFile,
		backup:        backup.NewBatch,
		executeBackup: executor.ExecuteFileWithBackup,
		pruneExecute:  executor.ExecutePrune,
		now:           time.Now,
	}
}
