// Package planner 形成 dot 只读计划阶段的自包含事实与动作模型。
package planner

import "io/fs"

// DesiredKind 描述 M1 planner 支持的期望文件行为。
type DesiredKind string

const (
	// DesiredLink 要求 target 是指向 source 的精确 symlink。
	DesiredLink DesiredKind = "link"
	// DesiredScaffold 要求 target 仅在首次缺失时接收渲染蓝本。
	DesiredScaffold DesiredKind = "scaffold"
)

// Desired 保存 decision 所需的完整期望值。Content 只对 scaffold 有效。
type Desired struct {
	Module     string
	Source     string
	SourcePath string
	Target     string
	TargetPath string
	Kind       DesiredKind
	Mode       fs.FileMode
	Content    []byte
}

// Clone 返回不共享 Content backing array 的副本。
func (desired Desired) Clone() Desired {
	desired.Content = append([]byte(nil), desired.Content...)
	return desired
}

// ObjectKind 描述 target leaf 的现势类型。
type ObjectKind string

const (
	// ObjectMissing 表示 target leaf 安全缺失。
	ObjectMissing ObjectKind = "missing"
	// ObjectSymlink 表示 target leaf 是 symlink。
	ObjectSymlink ObjectKind = "symlink"
	// ObjectRegular 表示 target leaf 是普通文件。
	ObjectRegular ObjectKind = "regular"
	// ObjectDirectory 表示 target leaf 是目录。
	ObjectDirectory ObjectKind = "directory"
	// ObjectSpecial 表示 target leaf 是 fifo、socket 或设备等特殊对象。
	ObjectSpecial ObjectKind = "special"
)

// Observation 是 target leaf 的显式只读快照。LinkDest 只对 symlink 有效；Content 与 Hash
// 只对 regular 有效。Mode 保存 Lstat 报告的完整 mode，供最终 Precond 精确复核。
type Observation struct {
	Kind     ObjectKind
	Mode     fs.FileMode
	LinkDest string
	Content  []byte
	Hash     string
}

// Clone 返回不共享 Content backing array 的副本。
func (observed Observation) Clone() Observation {
	observed.Content = append([]byte(nil), observed.Content...)
	return observed
}

// StateKind 描述 planner 可消费的 M1 历史产物类型。
type StateKind string

const (
	// StateSymlink 表示历史条目携带 symlink 所有权存证。
	StateSymlink StateKind = "symlink"
	// StateScaffold 表示历史条目只记录一次性 scaffold 生命周期。
	StateScaffold StateKind = "scaffold"
)

// HistoricalState 是 decision 所需的严格 state entry 副本。
type HistoricalState struct {
	Key       string
	Module    string
	Kind      StateKind
	Source    string
	LinkDest  string
	AppliedAt string
}

// ObservedTarget 把一个完整 desired 与其 current leaf 快照、可选历史 state 对齐。
type ObservedTarget struct {
	Desired  Desired
	Observed Observation
	State    HistoricalState
	HasState bool
}

// OrphanTarget 保存不匹配任何 current desired 的历史 entry 及其 current leaf 快照。
type OrphanTarget struct {
	TargetPath string
	State      HistoricalState
	Observed   Observation
}

// ObservedProfile 是完整 desired 与 strict state 的只读 identity join 结果。
type ObservedProfile struct {
	targets []ObservedTarget
	orphans []OrphanTarget
}

// Targets 返回不共享 desired/observed bytes 的副本。
func (profile ObservedProfile) Targets() []ObservedTarget {
	cloned := append([]ObservedTarget(nil), profile.targets...)
	for index := range cloned {
		cloned[index].Desired = cloned[index].Desired.Clone()
		cloned[index].Observed = cloned[index].Observed.Clone()
	}
	return cloned
}

// Orphans 返回不共享 observed bytes 的副本。
func (profile ObservedProfile) Orphans() []OrphanTarget {
	cloned := append([]OrphanTarget(nil), profile.orphans...)
	for index := range cloned {
		cloned[index].Observed = cloned[index].Observed.Clone()
	}
	return cloned
}

// ActionVerb 是纯计划中的稳定动作词汇；它不提供执行能力。
type ActionVerb string

const (
	// ActionSkip 不触碰 target 或 state。
	ActionSkip ActionVerb = "skip"
	// ActionCreateLink 创建或重建精确 symlink。
	ActionCreateLink ActionVerb = "create-link"
	// ActionScaffold 写入一次性渲染蓝本。
	ActionScaffold ActionVerb = "scaffold"
	// ActionAdopt 只更新 state，不触碰 target。
	ActionAdopt ActionVerb = "adopt"
	// ActionBackupReplace 先建立可用备份再替换普通对象。
	ActionBackupReplace ActionVerb = "backup-replace"
	// ActionPrune 清理历史孤儿 target 或 state 条目。
	ActionPrune ActionVerb = "prune"
	// ActionRunHook 表示待执行的 run_once hook。
	ActionRunHook ActionVerb = "run-hook"
	// ActionConflict 保留 unresolved 用户决策。
	ActionConflict ActionVerb = "conflict"
)

// ActionReason 是供 presentation/status 稳定分类的决策原因，不把人类文案当内部协议。
type ActionReason string

const (
	// ReasonTargetMissing 表示 target 缺失，需要创建 desired 产物。
	ReasonTargetMissing ActionReason = "target-missing"
	// ReasonExpectedLink 表示 target 已是精确期望 symlink。
	ReasonExpectedLink ActionReason = "expected-link"
	// ReasonStateMetadata 表示 target 证据已满足，只需刷新非所有权 metadata。
	ReasonStateMetadata ActionReason = "state-metadata"
	// ReasonOwnedLinkStale 表示 target 仍是 owned 旧链，但期望 source 已变化。
	ReasonOwnedLinkStale ActionReason = "owned-link-stale"
	// ReasonLinkDrift 表示有 symlink 记录，但 target 已被改指。
	ReasonLinkDrift ActionReason = "link-drift"
	// ReasonUnownedLink 表示未被当前记录拥有的 symlink 阻挡 link desired。
	ReasonUnownedLink ActionReason = "unowned-link"
	// ReasonRegularConflict 表示普通文件阻挡 link desired。
	ReasonRegularConflict ActionReason = "regular-conflict"
	// ReasonDirectoryConflict 表示目录阻挡 link desired，不能 force 替换。
	ReasonDirectoryConflict ActionReason = "directory-conflict"
	// ReasonSpecialConflict 表示特殊对象阻挡 link desired，不能 force 替换。
	ReasonSpecialConflict ActionReason = "special-conflict"
	// ReasonScaffoldPresent 表示 target 已满足 scaffold 的一次性生命周期。
	ReasonScaffoldPresent ActionReason = "scaffold-present"
	// ReasonScaffoldDeleted 表示已有 scaffold 记录且 target 缺失，应保留用户删除决定。
	ReasonScaffoldDeleted ActionReason = "scaffold-deleted"
	// ReasonScaffoldRebuild 表示显式 force 要求重建仍缺失的 scaffold。
	ReasonScaffoldRebuild ActionReason = "scaffold-rebuild"
	// ReasonOwnedLinkToScaffold 表示把仍 owned 的 symlink 转成独立 scaffold 文件。
	ReasonOwnedLinkToScaffold ActionReason = "owned-link-to-scaffold"
	// ReasonReleaseOwnershipToScaffold 表示只改记 scaffold，立即释放旧所有权。
	ReasonReleaseOwnershipToScaffold ActionReason = "release-ownership-to-scaffold"
)

// Precondition 固定一个动作提交前必须仍成立的 target 快照。
type Precondition struct {
	TargetPath           string
	Observed             Observation
	SourcePath           string
	RequireRegularSource bool
}

// StateEffectKind 描述动作成功后的 state 处置；preserve 也用于 skip/conflict/deferred/失败。
type StateEffectKind string

const (
	// StatePreserve 保留原 state 不变。
	StatePreserve StateEffectKind = "preserve"
	// StateUpsert 在动作成功后写入或刷新 state 条目。
	StateUpsert StateEffectKind = "upsert"
	// StateDelete 在活动 prune 成功后删除 state 条目。
	StateDelete StateEffectKind = "delete"
)

// StateEffect 保存一个结果分支的 state 处置。Entry 只对 upsert 有效，Key 对 upsert/delete
// 有效；PreviousKey 在 alias 展示 key 变化时要求同一提交摘除旧 key，避免留下重复 identity。
// upsert Entry 的 AppliedAt 由未来 executor 在动作成功时填入，不参与计划决策。
type StateEffect struct {
	Kind        StateEffectKind
	Key         string
	PreviousKey string
	Entry       HistoricalState
}

// Action 是 planner 与未来 executor 之间的自包含值；当前 package 只形成和展示它。
type Action struct {
	Verb         ActionVerb
	Target       string
	Reason       ActionReason
	Desired      Desired
	HasDesired   bool
	Precondition Precondition
	OnSuccess    StateEffect
	OnFailure    StateEffect
}

// Clone 返回不共享 desired/observed bytes 的动作副本。
func (action Action) Clone() Action {
	action.Desired = action.Desired.Clone()
	action.Precondition.Observed = action.Precondition.Observed.Clone()
	return action
}
