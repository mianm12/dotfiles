package add

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/mianm12/dotfiles/internal/state"
)

// ErrExecutionProtocol 表示 runner 依赖返回了无法安全解释的 plan 或执行结果。
var ErrExecutionProtocol = errors.New("add execution protocol violation")

// RunOptions 保存内部 add link runner 的严格 runtime 与原始请求。
type RunOptions struct {
	Runtime    dotruntime.Overrides
	CLIVersion string
	Request    Request
}

// OutcomeStatus 描述 locked plan 中每个 add item 的实际执行状态。
type OutcomeStatus string

const (
	// OutcomeSucceeded 表示 target 已越过提交点；即使后续 cleanup/state 失败也保持成功事实。
	OutcomeSucceeded OutcomeStatus = "succeeded"
	// OutcomeFailed 表示 item 在 target 提交点前失败。
	OutcomeFailed OutcomeStatus = "failed"
	// OutcomeDeferred 表示前项失败后未执行。
	OutcomeDeferred OutcomeStatus = "deferred"
)

// ItemOutcome 用稳定 plan index 与 target 标识执行结果。
type ItemOutcome struct {
	Index  int
	Target string
	Status OutcomeStatus
}

type runResultSeal struct{}

var successfulRunResultSeal = &runResultSeal{}

// Result 是可验证的内部 runner 摘要；零值无效，CLI 不得把无效结果投影为成功。
type Result struct {
	plan               BatchPlan
	outcomes           []ItemOutcome
	attempts           int
	sourcePublications int
	targetCommits      int
	stateCommitted     bool
	seal               *runResultSeal
}

// Valid 报告结果是否包含完整 locked plan 和自洽的逐项事实。
func (result Result) Valid() bool {
	items := result.plan.Items()
	if result.seal != successfulRunResultSeal || !result.plan.Valid() || len(result.outcomes) != len(items) ||
		result.attempts < 0 || result.attempts > len(items) || result.sourcePublications < result.targetCommits ||
		result.sourcePublications > result.attempts || result.targetCommits > result.attempts ||
		(result.stateCommitted && result.targetCommits == 0) {
		return false
	}
	for index, outcome := range result.outcomes {
		if outcome.Index != index || outcome.Target != items[index].Target() {
			return false
		}
		switch outcome.Status {
		case OutcomeSucceeded, OutcomeFailed:
			if index >= result.attempts {
				return false
			}
		case OutcomeDeferred:
			if index < result.attempts {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Plan 返回 sealed locked plan；无效结果返回零值。
func (result Result) Plan() BatchPlan {
	if !result.Valid() {
		return BatchPlan{}
	}
	return result.plan
}

// Outcomes 返回逐项结果的独立副本。
func (result Result) Outcomes() []ItemOutcome {
	if !result.Valid() {
		return nil
	}
	return append([]ItemOutcome(nil), result.outcomes...)
}

// Attempts 返回实际调用 item executor 的次数。
func (result Result) Attempts() int { return result.attempts }

// SourcePublications 返回 executor 报告 source 曾完整发布可用的次数。
func (result Result) SourcePublications() int { return result.sourcePublications }

// TargetCommits 返回越过 link target 提交点的次数。
func (result Result) TargetCommits() int { return result.targetCommits }

// StateCommitted 报告成功前缀 state 是否原子发布。
func (result Result) StateCommitted() bool { return result.stateCommitted }

type addMutationSession interface {
	load(string) (addLoadedMutation, error)
	close() error
}

type addLoadedMutation interface {
	inputs() dotruntime.LoadedInputs
	baseline() state.Loaded
	control() paths.ControlPlanePaths
	commit(state.Snapshot) error
}

type addRunOperations struct {
	begin     func(dotruntime.Overrides) (addMutationSession, error)
	preflight func(dotruntime.LoadedInputs, Request) (BatchPlan, error)
	execute   func(paths.ControlPlanePaths, ItemPlan) (linkItemResult, error)
	now       func() time.Time
}

// Run 在一个 mutation lock 周期内完成 strict load、exact-input batch preflight、link 执行与
// 成功前缀单次 state 提交。它不连接 CLI，也不实现 scaffold/template。
func Run(options RunOptions) (Result, error) {
	return runWithOperations(options, defaultAddRunOperations())
}

func runWithOperations(options RunOptions, operations addRunOperations) (result Result, resultErr error) {
	if operations.begin == nil || operations.preflight == nil || operations.execute == nil || operations.now == nil {
		return Result{}, fmt.Errorf("%w: runner operations are incomplete", ErrExecutionProtocol)
	}
	session, err := operations.begin(options.Runtime)
	if err != nil {
		return Result{}, fmt.Errorf("begin add mutation: %w", err)
	}
	if session == nil {
		return Result{}, fmt.Errorf("%w: begin returned nil mutation session", ErrExecutionProtocol)
	}
	defer func() {
		resultErr = errors.Join(resultErr, session.close())
	}()

	mutation, err := session.load(options.CLIVersion)
	if err != nil {
		return Result{}, fmt.Errorf("load add mutation inputs: %w", err)
	}
	if mutation == nil {
		return Result{}, fmt.Errorf("%w: load returned nil mutation capability", ErrExecutionProtocol)
	}
	plan, err := operations.preflight(mutation.inputs(), cloneRequest(options.Request))
	if err != nil {
		return Result{}, fmt.Errorf("plan locked add inputs: %w", err)
	}
	items := plan.Items()
	if !plan.Valid() || len(items) == 0 {
		return Result{}, fmt.Errorf("%w: preflight returned an invalid batch plan", ErrExecutionProtocol)
	}
	for index, item := range items {
		if !item.Valid() || item.Kind() != manifest.FileKindLink {
			return Result{}, fmt.Errorf("%w: plan item %d is not a validated link", ErrExecutionProtocol, index)
		}
	}
	result = Result{plan: plan, outcomes: make([]ItemOutcome, len(items)), seal: successfulRunResultSeal}
	for index, item := range items {
		result.outcomes[index] = ItemOutcome{Index: index, Target: item.Target(), Status: OutcomeDeferred}
	}

	updates := make([]state.EntryUpdate, 0, len(items))
	protocolViolation := false
	for index, item := range items {
		result.attempts++
		execution, executeErr := operations.execute(mutation.control(), item)
		committed, protocolErr := validateLinkExecutionResult(item, execution, executeErr)
		if protocolErr != nil {
			protocolViolation = true
			resultErr = fmt.Errorf(
				"execute add link item %d for %q: %w",
				index,
				item.Target(),
				errors.Join(executeErr, protocolErr),
			)
			break
		}
		if execution.sourcePublished {
			result.sourcePublications++
		}
		if committed {
			result.targetCommits++
			result.outcomes[index].Status = OutcomeSucceeded
			updates = append(updates, state.EntryUpdate{
				Key:       item.Target(),
				Module:    item.Module(),
				Kind:      state.KindSymlink,
				Source:    path.Join("modules", item.Module(), item.Source()),
				LinkDest:  item.SourcePath(),
				AppliedAt: operations.now().UTC().Format(time.RFC3339Nano),
			})
		} else {
			result.outcomes[index].Status = OutcomeFailed
		}
		if executeErr != nil {
			resultErr = fmt.Errorf("execute add link item %d for %q: %w", index, item.Target(), executeErr)
			break
		}
	}

	if len(updates) > 0 {
		candidate, changed, transitionErr := state.TransitionEntries(mutation.baseline(), updates)
		if transitionErr != nil {
			resultErr = errors.Join(resultErr, transitionErr)
			if protocolViolation {
				return Result{}, resultErr
			}
			return result, resultErr
		}
		if !changed {
			resultErr = errors.Join(resultErr, fmt.Errorf("%w: successful add prefix produced no state change", ErrExecutionProtocol))
			if protocolViolation {
				return Result{}, resultErr
			}
			return result, resultErr
		}
		if commitErr := mutation.commit(candidate); commitErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("commit add state: %w", commitErr))
			if protocolViolation {
				return Result{}, resultErr
			}
			return result, resultErr
		}
		result.stateCommitted = true
	}
	if protocolViolation {
		return Result{}, resultErr
	}
	if !result.Valid() {
		return Result{}, errors.Join(resultErr, fmt.Errorf("%w: runner produced an invalid result", ErrExecutionProtocol))
	}
	return result, resultErr
}

func validateLinkExecutionResult(
	expected ItemPlan,
	result linkItemResult,
	executeErr error,
) (committed bool, protocolErr error) {
	if !result.Valid() || !sameItemPlan(expected, result.item) {
		return false, fmt.Errorf("%w: executor returned an invalid or mismatched item result", ErrExecutionProtocol)
	}
	if result.targetCommitted && !result.sourcePublished {
		return false, fmt.Errorf("%w: target commit lacks a published source", ErrExecutionProtocol)
	}
	if executeErr == nil && !result.targetCommitted {
		return false, fmt.Errorf("%w: executor returned nil error without target commit", ErrExecutionProtocol)
	}
	return result.targetCommitted, nil
}

func sameItemPlan(left, right ItemPlan) bool {
	return left.Valid() && right.Valid() && left.Target() == right.Target() &&
		left.TargetPath() == right.TargetPath() && left.Module() == right.Module() &&
		left.Source() == right.Source() && left.SourcePath() == right.SourcePath() &&
		left.Kind() == right.Kind() && left.SourceExists() == right.SourceExists() &&
		left.snapshot.mode == right.snapshot.mode &&
		left.snapshot.identity.Equal(right.snapshot.identity) &&
		bytes.Equal(left.snapshot.content, right.snapshot.content)
}

func cloneRequest(request Request) Request {
	request.Paths = append([]string(nil), request.Paths...)
	return request
}

type runtimeAddMutationSession struct {
	session *dotruntime.MutationSession
}

func (session runtimeAddMutationSession) load(cliVersion string) (addLoadedMutation, error) {
	mutation, err := session.session.Load(cliVersion)
	if err != nil {
		return nil, err
	}
	return runtimeAddLoadedMutation{mutation: mutation}, nil
}

func (session runtimeAddMutationSession) close() error { return session.session.Close() }

type runtimeAddLoadedMutation struct {
	mutation *dotruntime.LoadedMutation
}

func (mutation runtimeAddLoadedMutation) inputs() dotruntime.LoadedInputs {
	return mutation.mutation.Inputs()
}

func (mutation runtimeAddLoadedMutation) baseline() state.Loaded {
	return mutation.mutation.Inputs().State()
}

func (mutation runtimeAddLoadedMutation) control() paths.ControlPlanePaths {
	return mutation.mutation.Inputs().Context().Control().Paths()
}

func (mutation runtimeAddLoadedMutation) commit(snapshot state.Snapshot) error {
	return mutation.mutation.CommitState(snapshot)
}

func defaultAddRunOperations() addRunOperations {
	return addRunOperations{
		begin: func(overrides dotruntime.Overrides) (addMutationSession, error) {
			session, err := dotruntime.BeginMutation(overrides)
			if err != nil {
				return nil, err
			}
			return runtimeAddMutationSession{session: session}, nil
		},
		preflight: Preflight,
		execute: func(control paths.ControlPlanePaths, item ItemPlan) (linkItemResult, error) {
			return executeLinkItem(control, item, defaultLinkOperations())
		},
		now: time.Now,
	}
}
