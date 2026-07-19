package apply

import (
	"errors"
	"fmt"
	"time"

	"github.com/mianm12/dotfiles/internal/executor"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/planner"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/mianm12/dotfiles/internal/state"
)

// ErrExecutionProtocol 表示 file executor 返回值无法按计划的成功/失败 effect 解释。
var ErrExecutionProtocol = errors.New("file execution result violates apply protocol")

// Options 保存内部 M1 link/scaffold runner 的严格 runtime 与 scope 输入。
type Options struct {
	Runtime    dotruntime.Overrides
	CLIVersion string
	Modules    []string
	Force      bool
	NoPrune    bool
}

// Result 保存内部 runner 的可验证摘要，不定义 CLI 输出或退出码。FileAttempts 统计 executor
// 调用，TargetCommits 统计 executor 报告已越过的 target 提交点，AdoptionEffects 统计已接受的
// adopt OnSuccess effect；这些事实即使最终 state Store 失败也保留。StateCommitted 只表示候选
// state 已成功原子发布。
type Result struct {
	Plan            planner.ApplyPlan
	FileAttempts    int
	AdoptionEffects int
	TargetCommits   int
	StateCommitted  bool
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
	value planner.ApplyPlan
	files []planner.FileAction
	prune []planner.PruneAction
	hooks []planner.HookAction
}

type runOperations struct {
	begin   func(dotruntime.Overrides) (mutationSession, error)
	plan    func(dotruntime.LoadedInputs, planner.ApplyScopeOptions) (executionPlan, error)
	execute func(paths.ControlPlanePaths, planner.FileAction) (executor.FileResult, error)
	now     func() time.Time
}

// Run 在一个 mutation lock 周期内完成 strict load、exact-input plan、CP4 scope gate、file
// execution 和一次 state commit。它不连接 CLI，也不执行 backup/prune/hooks。
func Run(options Options) (Result, error) {
	return runWithOperations(options, defaultRunOperations())
}

func runWithOperations(options Options, operations runOperations) (result Result, resultErr error) {
	if operations.begin == nil || operations.plan == nil || operations.execute == nil || operations.now == nil {
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

	updates := make([]state.EntryUpdate, 0, len(planned.files))
	for index, action := range planned.files {
		if action.Verb.ExecutionClass() == planner.FilePlanOnly {
			continue
		}
		result.FileAttempts++
		fileResult, executeErr := operations.execute(mutation.control(), action)
		if fileResult.TargetMutated {
			result.TargetCommits++
		}

		success, failure, protocolErr := validateFileResult(action, fileResult)
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
			resultErr = fmt.Errorf("execute file action %d for %q: %w", index, action.Target, executeErr)
			break
		}
	}

	if len(updates) == 0 {
		return result, resultErr
	}
	candidate, changed, transitionErr := state.TransitionEntries(mutation.baseline(), updates)
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

func validateFileResult(
	action planner.FileAction,
	result executor.FileResult,
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

	switch action.Verb.ExecutionClass() {
	case planner.FileStateOnly:
		if result.TargetMutated {
			return false, false, fmt.Errorf(
				"%w: state-only file action %q reported a target commit",
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
				value: plan,
				files: plan.FileActions(),
				prune: plan.Prune().Actions(),
				hooks: plan.Hooks().Actions(),
			}, nil
		},
		execute: executor.ExecuteFile,
		now:     time.Now,
	}
}
