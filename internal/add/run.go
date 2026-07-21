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
	// OutcomeSucceeded 表示 item 已越过其提交点；link 为 target 替换，scaffold 为 state 提交。
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
		result.sourcePublications > result.attempts || result.targetCommits > result.attempts {
		return false
	}
	succeeded := 0
	scaffoldSucceeded := 0
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
		if outcome.Status == OutcomeSucceeded {
			succeeded++
			if items[index].Kind() == manifest.FileKindScaffold {
				scaffoldSucceeded++
			}
		}
	}
	if result.targetCommits > succeeded || (result.stateCommitted && succeeded == 0) ||
		(scaffoldSucceeded > 0 && !result.stateCommitted) ||
		(len(items) > 0 && items[0].Kind() == manifest.FileKindScaffold && result.targetCommits != 0) {
		return false
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
	begin              func(dotruntime.Overrides) (addMutationSession, error)
	preflight          func(dotruntime.LoadedInputs, Request) (BatchPlan, error)
	execute            func(paths.ControlPlanePaths, ItemPlan) (linkItemResult, error)
	revalidateScaffold func(paths.ControlPlanePaths, ItemPlan, linkItemResult) error
	cleanupScaffold    func(linkItemResult) error
	now                func() time.Time
}

// Run 在一个 mutation lock 周期内完成 strict load、exact-input batch preflight、link/scaffold
// 执行与成功前缀单次 state 提交。它不连接 CLI，也不实现 managed/template。
func Run(options RunOptions) (Result, error) {
	return runWithOperations(options, defaultAddRunOperations())
}

func runWithOperations(options RunOptions, operations addRunOperations) (result Result, resultErr error) {
	if options.Request.Mode == ModeTemplate {
		return Result{}, ErrTemplateUnsupported
	}
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
	expectedKind, kindErr := requestedKind(options.Request.Mode)
	if kindErr != nil {
		return Result{}, errors.Join(kindErr, fmt.Errorf("%w: request mode has no executable M1 kind", ErrExecutionProtocol))
	}
	for index, item := range items {
		if !item.Valid() || item.Kind() != expectedKind {
			return Result{}, fmt.Errorf("%w: plan item %d does not match request kind %q", ErrExecutionProtocol, index, expectedKind)
		}
	}
	if expectedKind == manifest.FileKindScaffold &&
		(operations.revalidateScaffold == nil || operations.cleanupScaffold == nil) {
		return Result{}, fmt.Errorf("%w: scaffold commit operations are incomplete", ErrExecutionProtocol)
	}
	result = Result{plan: plan, outcomes: make([]ItemOutcome, len(items)), seal: successfulRunResultSeal}
	for index, item := range items {
		result.outcomes[index] = ItemOutcome{Index: index, Target: item.Target(), Status: OutcomeDeferred}
	}

	updates := make([]state.EntryUpdate, 0, len(items))
	preparedScaffolds := make([]int, 0, len(items))
	executions := make([]linkItemResult, len(items))
	protocolViolation := false
	for index, item := range items {
		result.attempts++
		execution, executeErr := operations.execute(mutation.control(), item)
		executions[index] = execution
		stateReady, protocolErr := validateItemExecutionResult(item, execution, executeErr)
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
		if stateReady {
			update := state.EntryUpdate{
				Key:       item.Target(),
				Module:    item.Module(),
				Source:    path.Join("modules", item.Module(), item.Source()),
				AppliedAt: operations.now().UTC().Format(time.RFC3339Nano),
			}
			switch item.Kind() {
			case manifest.FileKindLink:
				result.targetCommits++
				result.outcomes[index].Status = OutcomeSucceeded
				update.Kind = state.KindSymlink
				update.LinkDest = item.SourcePath()
			case manifest.FileKindScaffold:
				// scaffold 只有 state Store 成功后才越过提交点；此前保持 failed 投影。
				result.outcomes[index].Status = OutcomeFailed
				update.Kind = state.KindScaffold
				preparedScaffolds = append(preparedScaffolds, index)
			}
			updates = append(updates, update)
		} else {
			result.outcomes[index].Status = OutcomeFailed
		}
		if executeErr != nil {
			resultErr = fmt.Errorf("execute add link item %d for %q: %w", index, item.Target(), executeErr)
			break
		}
	}
	if len(preparedScaffolds) > 0 {
		validPrepared := len(preparedScaffolds)
		for position, index := range preparedScaffolds {
			if err := operations.revalidateScaffold(mutation.control(), items[index], executions[index]); err != nil {
				validPrepared = position
				resultErr = errors.Join(resultErr, fmt.Errorf(
					"revalidate add scaffold item %d for state commit: %w", index, err,
				))
				break
			}
		}
		if validPrepared < len(preparedScaffolds) {
			for _, index := range preparedScaffolds[validPrepared:] {
				resultErr = errors.Join(resultErr, operations.cleanupScaffold(executions[index]))
			}
			preparedScaffolds = preparedScaffolds[:validPrepared]
			updates = updates[:validPrepared]
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
		for _, index := range preparedScaffolds {
			result.outcomes[index].Status = OutcomeSucceeded
		}
	}
	if protocolViolation {
		return Result{}, resultErr
	}
	if !result.Valid() {
		return Result{}, errors.Join(resultErr, fmt.Errorf("%w: runner produced an invalid result", ErrExecutionProtocol))
	}
	return result, resultErr
}

func validateItemExecutionResult(
	expected ItemPlan,
	result linkItemResult,
	executeErr error,
) (committed bool, protocolErr error) {
	if !result.Valid() || !sameItemPlan(expected, result.item) {
		return false, fmt.Errorf("%w: executor returned an invalid or mismatched item result", ErrExecutionProtocol)
	}
	if result.stateReady && !result.sourcePublished {
		return false, fmt.Errorf("%w: state-ready item lacks a published source", ErrExecutionProtocol)
	}
	if result.targetCommitted && !result.stateReady {
		return false, fmt.Errorf("%w: target commit lacks a state-ready effect", ErrExecutionProtocol)
	}
	switch expected.Kind() {
	case manifest.FileKindLink:
		if result.stateReady != result.targetCommitted {
			return false, fmt.Errorf("%w: link state effect does not match target commit", ErrExecutionProtocol)
		}
	case manifest.FileKindScaffold:
		if result.targetCommitted {
			return false, fmt.Errorf("%w: scaffold executor reported a target commit", ErrExecutionProtocol)
		}
	default:
		return false, fmt.Errorf("%w: unsupported executable item kind %q", ErrExecutionProtocol, expected.Kind())
	}
	if executeErr == nil && !result.stateReady {
		return false, fmt.Errorf("%w: executor returned nil error without state-ready effect", ErrExecutionProtocol)
	}
	return result.stateReady, nil
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
			switch item.Kind() {
			case manifest.FileKindLink:
				return executeLinkItem(control, item, defaultLinkOperations())
			case manifest.FileKindScaffold:
				return executeScaffoldItem(control, item, defaultPublicationOperations())
			default:
				return linkItemResult{}, fmt.Errorf("%w: unsupported add item kind %q", ErrExecutionProtocol, item.Kind())
			}
		},
		revalidateScaffold: func(control paths.ControlPlanePaths, item ItemPlan, result linkItemResult) error {
			return revalidateScaffoldStatePrecondition(control, item, result, defaultPublicationOperations())
		},
		cleanupScaffold: func(result linkItemResult) error {
			return cleanupUncommittedScaffold(result, defaultPublicationOperations())
		},
		now: time.Now,
	}
}
