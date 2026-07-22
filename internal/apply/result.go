package apply

import (
	"github.com/mianm12/dotfiles/internal/planner"
)

// ActionOutcomeStatus 描述 runner 对一个计划动作的实际处置，不让调用方从聚合计数猜测。
type ActionOutcomeStatus string

const (
	// ActionSucceeded 表示动作已成功形成计划要求的 state effect。
	ActionSucceeded ActionOutcomeStatus = "succeeded"
	// ActionConflict 表示最终 Precondition 失配，动作未提交且需要用户消解。
	ActionConflict ActionOutcomeStatus = "conflict"
	// ActionDeferred 表示动作因前置收敛或确认门禁未被尝试。
	ActionDeferred ActionOutcomeStatus = "deferred"
	// ActionFailed 表示动作遭遇非 conflict 运行或协议错误。
	ActionFailed ActionOutcomeStatus = "failed"
	// ActionSkipped 表示 canonical HookSkip 未启动子进程且不形成新 state effect。
	ActionSkipped ActionOutcomeStatus = "skipped"
)

// FileOutcome 以 canonical plan index 与 target 标识一个可执行 file 动作的结果。
// 物理提交、state effect 与 backup 事实由 Result 封装，不暴露第二套可变摘要。
type FileOutcome struct {
	Index  int
	Target string
	Status ActionOutcomeStatus

	attempted        bool
	targetCommitted  bool
	stateEffectReady bool
	backupPath       string
}

// PruneOutcome 以 canonical plan index 与 target 标识一个 prune 动作的结果。
type PruneOutcome struct {
	Index  int
	Target string
	Status ActionOutcomeStatus

	attempted        bool
	targetCommitted  bool
	stateEffectReady bool
}

// HookOutcome 以 canonical plan index 与 run_once state key 标识一个 hook 动作的结果。
type HookOutcome struct {
	Index    int
	StateKey string
	Status   ActionOutcomeStatus

	attempted        bool
	stateEffectReady bool
}

type resultStage uint8

const (
	resultPlanned resultStage = iota + 1
	resultExecuted
)

type resultSeal struct{}

var successfulResultSeal = &resultSeal{}

// Result 是 apply runner 的密封事实模型。零值无效；CLI 必须先用 Valid 校验，再投影其内容。
type Result struct {
	plan             planner.ApplyPlan
	stage            resultStage
	fileOutcomes     []FileOutcome
	pruneOutcomes    []PruneOutcome
	hookOutcomes     []HookOutcome
	confirmRequested bool
	confirmAccepted  bool
	stateCommitted   bool
	seal             *resultSeal
}

func newPlannedResult(plan planner.ApplyPlan) Result {
	return Result{plan: plan, stage: resultPlanned, seal: successfulResultSeal}
}

func (result *Result) beginExecution(
	files []planner.FileAction,
	prune []planner.PruneAction,
	hooks []planner.HookAction,
) map[int]int {
	result.stage = resultExecuted
	positions := make(map[int]int)
	for index, action := range files {
		if action.Verb.ExecutionClass() == planner.FilePlanOnly {
			continue
		}
		positions[index] = len(result.fileOutcomes)
		result.fileOutcomes = append(result.fileOutcomes, FileOutcome{
			Index: index, Target: action.Target, Status: ActionDeferred,
		})
	}
	result.pruneOutcomes = make([]PruneOutcome, len(prune))
	for index, action := range prune {
		result.pruneOutcomes[index] = PruneOutcome{
			Index: index, Target: action.Target, Status: ActionDeferred,
		}
	}
	result.hookOutcomes = make([]HookOutcome, len(hooks))
	for index, action := range hooks {
		status := ActionDeferred
		if action.Verb == planner.HookSkip {
			status = ActionSkipped
		}
		result.hookOutcomes[index] = HookOutcome{Index: index, StateKey: action.StateKey, Status: status}
	}
	return positions
}

// Valid 报告 Result 是否与 canonical plan、逐项物理事实和本次 error 状态自洽。
func (result Result) Valid(hasRuntimeError bool) bool {
	if result.seal != successfulResultSeal || !result.plan.Valid() {
		return false
	}
	if result.stage == resultPlanned {
		return hasRuntimeError && len(result.fileOutcomes) == 0 && len(result.pruneOutcomes) == 0 &&
			len(result.hookOutcomes) == 0 &&
			!result.confirmRequested && !result.confirmAccepted && !result.stateCommitted
	}
	if result.stage != resultExecuted || !result.validFileOutcomes(hasRuntimeError) ||
		!result.validPruneOutcomes(hasRuntimeError) || !result.validHookOutcomes(hasRuntimeError) ||
		!result.validExecutionGates() ||
		result.confirmAccepted && !result.confirmRequested {
		return false
	}
	groups := result.plan.Prune().ConfirmationGroups()
	if (len(groups) == 0 && result.confirmRequested) ||
		(result.stateCommitted && result.stateEffectCount() == 0) ||
		(!hasRuntimeError && result.stateEffectCount() > 0 && !result.stateCommitted) {
		return false
	}
	return true
}

func (result Result) validExecutionGates() bool {
	filesConverged := true
	for _, outcome := range result.fileOutcomes {
		filesConverged = filesConverged && outcome.Status == ActionSucceeded
	}

	prune := result.plan.Prune().Actions()
	activeCount := 0
	activeAttempted := false
	firstActiveAttempted := false
	for index, action := range prune {
		if action.Deferred {
			continue
		}
		outcome := result.pruneOutcomes[index]
		if activeCount == 0 {
			firstActiveAttempted = outcome.attempted
		}
		activeCount++
		activeAttempted = activeAttempted || outcome.attempted
		if !filesConverged && outcome.Status != ActionDeferred {
			return false
		}
	}

	groups := result.plan.Prune().ConfirmationGroups()
	if activeCount == 0 {
		return !result.confirmRequested && !result.confirmAccepted
	}
	if !filesConverged {
		return !activeAttempted && !result.confirmRequested && !result.confirmAccepted
	}
	if len(groups) == 0 {
		return !result.confirmRequested && !result.confirmAccepted && firstActiveAttempted
	}
	if !result.confirmRequested {
		return false
	}
	if !result.confirmAccepted {
		return !activeAttempted
	}
	return firstActiveAttempted
}

func (result Result) validFileOutcomes(hasRuntimeError bool) bool {
	files := result.plan.FileActions()
	expected := 0
	stopped := false
	for index, action := range files {
		if action.Verb.ExecutionClass() == planner.FilePlanOnly {
			continue
		}
		if expected >= len(result.fileOutcomes) {
			return false
		}
		outcome := result.fileOutcomes[expected]
		expected++
		if outcome.Index != index || outcome.Target != action.Target ||
			!validFileOutcome(action, outcome, hasRuntimeError) {
			return false
		}
		if stopped && outcome.Status != ActionDeferred {
			return false
		}
		switch outcome.Status {
		case ActionConflict, ActionFailed:
			stopped = true
		case ActionDeferred:
			if !stopped {
				return false
			}
		}
	}
	return expected == len(result.fileOutcomes)
}

func validFileOutcome(action planner.FileAction, outcome FileOutcome, hasRuntimeError bool) bool {
	if action.Verb != planner.FileBackupReplace && outcome.backupPath != "" {
		return false
	}
	switch outcome.Status {
	case ActionSucceeded:
		return outcome.attempted && outcome.stateEffectReady &&
			outcome.targetCommitted == (action.Verb.ExecutionClass() == planner.FileTargetMutation) &&
			(action.Verb != planner.FileBackupReplace || outcome.backupPath != "")
	case ActionConflict:
		return outcome.attempted && !outcome.targetCommitted && !outcome.stateEffectReady
	case ActionDeferred:
		return !outcome.attempted && !outcome.targetCommitted && !outcome.stateEffectReady && outcome.backupPath == ""
	case ActionFailed:
		if !hasRuntimeError || outcome.stateEffectReady && !outcome.attempted {
			return false
		}
		if !outcome.attempted {
			return action.Verb == planner.FileBackupReplace && !outcome.targetCommitted &&
				outcome.backupPath == ""
		}
		if action.Verb.ExecutionClass() == planner.FileStateOnly && outcome.targetCommitted {
			return false
		}
		if outcome.stateEffectReady &&
			(action.Verb.ExecutionClass() != planner.FileTargetMutation || !outcome.targetCommitted) {
			return false
		}
		return true
	default:
		return false
	}
}

func (result Result) validPruneOutcomes(hasRuntimeError bool) bool {
	prune := result.plan.Prune().Actions()
	if len(result.pruneOutcomes) != len(prune) {
		return false
	}
	stopped := false
	for index, action := range prune {
		outcome := result.pruneOutcomes[index]
		if outcome.Index != index || outcome.Target != action.Target ||
			!validPruneOutcome(action, outcome, hasRuntimeError) {
			return false
		}
		if action.Deferred {
			continue
		}
		if stopped && outcome.attempted {
			return false
		}
		switch outcome.Status {
		case ActionConflict, ActionDeferred, ActionFailed:
			stopped = true
		}
	}
	return true
}

func validPruneOutcome(action planner.PruneAction, outcome PruneOutcome, hasRuntimeError bool) bool {
	if action.Deferred {
		return outcome.Status == ActionDeferred && !outcome.attempted &&
			!outcome.targetCommitted && !outcome.stateEffectReady
	}
	switch outcome.Status {
	case ActionSucceeded:
		return outcome.attempted && outcome.stateEffectReady &&
			outcome.targetCommitted == (action.Mode == planner.PruneTargetAndState)
	case ActionConflict:
		return outcome.attempted && !outcome.targetCommitted && !outcome.stateEffectReady
	case ActionDeferred:
		return !outcome.attempted && !outcome.targetCommitted && !outcome.stateEffectReady
	case ActionFailed:
		return hasRuntimeError && outcome.attempted && !outcome.stateEffectReady
	default:
		return false
	}
}

func (result Result) validHookOutcomes(hasRuntimeError bool) bool {
	hooks := result.plan.Hooks().Actions()
	if len(result.hookOutcomes) != len(hooks) {
		return false
	}
	stopped := false
	for index, action := range hooks {
		outcome := result.hookOutcomes[index]
		if outcome.Index != index || outcome.StateKey != action.StateKey {
			return false
		}
		if action.Verb == planner.HookSkip {
			if outcome.Status != ActionSkipped || outcome.attempted || outcome.stateEffectReady {
				return false
			}
			continue
		}
		if stopped && outcome.Status != ActionDeferred {
			return false
		}
		switch outcome.Status {
		case ActionSucceeded:
			if !outcome.attempted || !outcome.stateEffectReady {
				return false
			}
		case ActionFailed:
			if !hasRuntimeError || !outcome.attempted || outcome.stateEffectReady {
				return false
			}
			stopped = true
		case ActionDeferred:
			if !stopped || outcome.attempted || outcome.stateEffectReady {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (result Result) stateEffectCount() int {
	count := 0
	for _, outcome := range result.fileOutcomes {
		if outcome.stateEffectReady {
			count++
		}
	}
	for _, outcome := range result.pruneOutcomes {
		if outcome.stateEffectReady {
			count++
		}
	}
	for _, outcome := range result.hookOutcomes {
		if outcome.stateEffectReady {
			count++
		}
	}
	return count
}

// Plan 返回 sealed locked plan；零值或伪造结果返回零 plan。
func (result Result) Plan() planner.ApplyPlan {
	if result.seal != successfulResultSeal || !result.plan.Valid() {
		return planner.ApplyPlan{}
	}
	return result.plan
}

// ActionOutcomesReady 报告 runner 是否已经越过 execution scope gate 并建立逐项结果。
func (result Result) ActionOutcomesReady() bool { return result.stage == resultExecuted }

// FileOutcomes 返回不共享 backing array 的逐 file 结果。
func (result Result) FileOutcomes() []FileOutcome {
	outcomes := make([]FileOutcome, len(result.fileOutcomes))
	for index, outcome := range result.fileOutcomes {
		outcomes[index] = FileOutcome{Index: outcome.Index, Target: outcome.Target, Status: outcome.Status}
	}
	return outcomes
}

// PruneOutcomes 返回不共享 backing array 的逐 prune 结果。
func (result Result) PruneOutcomes() []PruneOutcome {
	outcomes := make([]PruneOutcome, len(result.pruneOutcomes))
	for index, outcome := range result.pruneOutcomes {
		outcomes[index] = PruneOutcome{Index: outcome.Index, Target: outcome.Target, Status: outcome.Status}
	}
	return outcomes
}

// HookOutcomes 返回不共享 backing array 的逐 hook 结果。
func (result Result) HookOutcomes() []HookOutcome {
	outcomes := make([]HookOutcome, len(result.hookOutcomes))
	for index, outcome := range result.hookOutcomes {
		outcomes[index] = HookOutcome{Index: outcome.Index, StateKey: outcome.StateKey, Status: outcome.Status}
	}
	return outcomes
}

// FileAttempts 返回实际调用 file executor 的次数。
func (result Result) FileAttempts() int {
	count := 0
	for _, outcome := range result.fileOutcomes {
		if outcome.attempted {
			count++
		}
	}
	return count
}

// AdoptionEffects 返回已接受的 adopt state effects 数量。
func (result Result) AdoptionEffects() int {
	if !result.plan.Valid() {
		return 0
	}
	files := result.plan.FileActions()
	count := 0
	for _, outcome := range result.fileOutcomes {
		if outcome.Index >= 0 && outcome.Index < len(files) && outcome.stateEffectReady &&
			files[outcome.Index].Verb == planner.FileAdopt {
			count++
		}
	}
	return count
}

// TargetCommits 返回 file executor 报告越过 target 提交点的次数。
func (result Result) TargetCommits() int {
	count := 0
	for _, outcome := range result.fileOutcomes {
		if outcome.targetCommitted {
			count++
		}
	}
	return count
}

// PruneAttempts 返回实际调用 prune executor 的次数。
func (result Result) PruneAttempts() int {
	count := 0
	for _, outcome := range result.pruneOutcomes {
		if outcome.attempted {
			count++
		}
	}
	return count
}

// HookAttempts 返回实际调用 hook executor 的次数；HookSkip 与失败后的后缀不计入。
func (result Result) HookAttempts() int {
	count := 0
	for _, outcome := range result.hookOutcomes {
		if outcome.attempted {
			count++
		}
	}
	return count
}

// HookEffects 返回已接受、将进入同一 ChangeSet 的 run_once upsert 数量。
func (result Result) HookEffects() int {
	count := 0
	for _, outcome := range result.hookOutcomes {
		if outcome.stateEffectReady {
			count++
		}
	}
	return count
}

// PruneEffects 返回已接受的 prune state delete 数量。
func (result Result) PruneEffects() int {
	count := 0
	for _, outcome := range result.pruneOutcomes {
		if outcome.stateEffectReady {
			count++
		}
	}
	return count
}

// PruneCommits 返回 prune executor 报告越过 target 删除提交点的次数。
func (result Result) PruneCommits() int {
	count := 0
	for _, outcome := range result.pruneOutcomes {
		if outcome.targetCommitted {
			count++
		}
	}
	return count
}

// PruneDeferred 报告是否仍有未成功的 prune 动作。
func (result Result) PruneDeferred() bool {
	for _, outcome := range result.pruneOutcomes {
		if outcome.Status != ActionSucceeded {
			return true
		}
	}
	return false
}

// UnresolvedConflicts 返回运行期 file/prune Precondition conflict 数量。
func (result Result) UnresolvedConflicts() int {
	count := 0
	for _, outcome := range result.fileOutcomes {
		if outcome.Status == ActionConflict {
			count++
		}
	}
	for _, outcome := range result.pruneOutcomes {
		if outcome.Status == ActionConflict {
			count++
		}
	}
	return count
}

// ConfirmRequested 报告 runner 是否请求过 whole-module prune 确认。
func (result Result) ConfirmRequested() bool { return result.confirmRequested }

// ConfirmAccepted 报告 whole-module prune 确认是否被接受。
func (result Result) ConfirmAccepted() bool { return result.confirmAccepted }

// StateCommitted 报告成功 effect 的候选 state 是否已原子发布。
func (result Result) StateCommitted() bool { return result.stateCommitted }

// BackupPaths 返回本次已完整保留、应报告给用户的 backup 路径。
func (result Result) BackupPaths() []string {
	paths := make([]string, 0)
	for _, outcome := range result.fileOutcomes {
		if outcome.backupPath != "" {
			paths = append(paths, outcome.backupPath)
		}
	}
	return paths
}
