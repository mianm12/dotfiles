// Package planner 形成 dot 只读计划阶段的自包含事实与动作模型。
package planner

import (
	"io/fs"

	"github.com/mianm12/dotfiles/internal/paths"
)

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

// Observation 是 target leaf 的显式只读事实。LinkDest 只对 symlink 有效；Hash 只在调用方
// 明确请求 regular digest 时有效。Mode 保存 Lstat 报告的完整 mode。
type Observation struct {
	Kind     ObjectKind
	Mode     fs.FileMode
	LinkDest string
	Hash     string
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
	Desired    Desired
	Resolution paths.TargetResolution
	Observed   Observation
	State      HistoricalState
	HasState   bool
}

// OrphanTarget 保存不匹配任何 current desired 的历史 entry 及其 current leaf 快照。
type OrphanTarget struct {
	TargetPath string
	Resolution paths.TargetResolution
	State      HistoricalState
	Observed   Observation
}

// ObservedProfile 是完整 desired 与 strict state 的只读 identity join 结果。
type ObservedProfile struct {
	targets []ObservedTarget
	orphans []OrphanTarget
}

// Targets 返回不共享 desired bytes 的副本。Resolution 是 paths 提供的不可变值快照，
// 值复制不会向调用方暴露其 identity/ancestor 内部存储。
func (profile ObservedProfile) Targets() []ObservedTarget {
	cloned := append([]ObservedTarget(nil), profile.targets...)
	for index := range cloned {
		cloned[index].Desired = cloned[index].Desired.Clone()
	}
	return cloned
}

// Orphans 返回独立的 orphan slice。
func (profile ObservedProfile) Orphans() []OrphanTarget {
	return append([]OrphanTarget(nil), profile.orphans...)
}

// FileVerb 是 file decision 中的稳定动作词汇；它不提供执行能力。
type FileVerb string

const (
	// FileSkip 不触碰 target 或 state。
	FileSkip FileVerb = "skip"
	// FileCreateLink 创建或重建精确 symlink。
	FileCreateLink FileVerb = "create-link"
	// FileScaffold 写入一次性渲染蓝本。
	FileScaffold FileVerb = "scaffold"
	// FileAdopt 只更新 state，不触碰 target。
	FileAdopt FileVerb = "adopt"
	// FileBackupReplace 先建立可用备份再替换普通对象。
	FileBackupReplace FileVerb = "backup-replace"
	// FileConflict 保留 unresolved 用户决策。
	FileConflict FileVerb = "conflict"
)

// FileExecutionClass 描述 file action 在编排层的执行职责。
type FileExecutionClass string

const (
	// FilePlanOnly 表示 action 只供计划和展示使用，不进入 executor。
	FilePlanOnly FileExecutionClass = "plan-only"
	// FileStateOnly 表示 action 复核 target 前提后只形成 state effect。
	FileStateOnly FileExecutionClass = "state-only"
	// FileTargetMutation 表示 action 可能越过 target mutation 提交点。
	FileTargetMutation FileExecutionClass = "target-mutation"
)

// ExecutionClass 返回 verb 的封闭执行职责；未知 verb 返回空值并由消费方拒绝。
func (verb FileVerb) ExecutionClass() FileExecutionClass {
	switch verb {
	case FileSkip, FileConflict:
		return FilePlanOnly
	case FileAdopt:
		return FileStateOnly
	case FileCreateLink, FileScaffold, FileBackupReplace:
		return FileTargetMutation
	default:
		return ""
	}
}

// FileReason 是供 presentation/status 稳定分类的 file decision 原因，不把人类文案当内部协议。
type FileReason string

const (
	// FileReasonTargetMissing 表示 target 缺失，需要创建 desired 产物。
	FileReasonTargetMissing FileReason = "target-missing"
	// FileReasonExpectedLink 表示 target 已是精确期望 symlink。
	FileReasonExpectedLink FileReason = "expected-link"
	// FileReasonStateMetadata 表示 target 证据已满足，只需刷新非所有权 metadata。
	FileReasonStateMetadata FileReason = "state-metadata"
	// FileReasonOwnedLinkStale 表示 target 仍是 owned 旧链，但期望 source 已变化。
	FileReasonOwnedLinkStale FileReason = "owned-link-stale"
	// FileReasonLinkDrift 表示有 symlink 记录，但 target 已被改指。
	FileReasonLinkDrift FileReason = "link-drift"
	// FileReasonUnownedLink 表示未被当前记录拥有的 symlink 阻挡 link desired。
	FileReasonUnownedLink FileReason = "unowned-link"
	// FileReasonRegularConflict 表示普通文件阻挡 link desired。
	FileReasonRegularConflict FileReason = "regular-conflict"
	// FileReasonDirectoryConflict 表示目录阻挡 link desired，不能 force 替换。
	FileReasonDirectoryConflict FileReason = "directory-conflict"
	// FileReasonSpecialConflict 表示特殊对象阻挡 link desired，不能 force 替换。
	FileReasonSpecialConflict FileReason = "special-conflict"
	// FileReasonScaffoldPresent 表示 target 已满足 scaffold 的一次性生命周期。
	FileReasonScaffoldPresent FileReason = "scaffold-present"
	// FileReasonScaffoldDeleted 表示已有 scaffold 记录且 target 缺失，应保留用户删除决定。
	FileReasonScaffoldDeleted FileReason = "scaffold-deleted"
	// FileReasonScaffoldRebuild 表示显式 force 要求重建仍缺失的 scaffold。
	FileReasonScaffoldRebuild FileReason = "scaffold-rebuild"
	// FileReasonOwnedLinkToScaffold 表示把仍 owned 的 symlink 转成独立 scaffold 文件。
	FileReasonOwnedLinkToScaffold FileReason = "owned-link-to-scaffold"
	// FileReasonReleaseOwnershipToScaffold 表示只改记 scaffold，立即释放旧所有权。
	FileReasonReleaseOwnershipToScaffold FileReason = "release-ownership-to-scaffold"
)

// LeafConditionKind 描述 action 执行前必须仍成立的 leaf 谓词。
type LeafConditionKind string

const (
	// LeafAny 不约束 leaf 形态；target identity 与路径边界仍必须复核。
	LeafAny LeafConditionKind = "any"
	// LeafMissing 要求 leaf 仍安全缺失。
	LeafMissing LeafConditionKind = "missing"
	// LeafPresent 要求 leaf 存在，但不约束其 kind、内容或 mode。
	LeafPresent LeafConditionKind = "present"
	// LeafExactSymlink 要求 raw symlink destination 精确相等。
	LeafExactSymlink LeafConditionKind = "exact-symlink"
	// LeafNotOwnedSymlink 要求 leaf 不是指向记录目标的 owned symlink。
	LeafNotOwnedSymlink LeafConditionKind = "not-owned-symlink"
	// LeafExactRegular 要求 regular digest 与普通权限位精确相等。
	LeafExactRegular LeafConditionKind = "exact-regular"
)

// LeafCondition 保存一个封闭 leaf 谓词所需的最小证据。LinkDest 只用于 symlink 条件；Hash
// 与 Permissions 只用于 exact-regular。
type LeafCondition struct {
	Kind        LeafConditionKind
	LinkDest    string
	Hash        string
	Permissions fs.FileMode
}

// Valid 报告条件字段组合是否封闭且无歧义。
func (condition LeafCondition) Valid() bool {
	switch condition.Kind {
	case LeafAny, LeafMissing, LeafPresent:
		return condition.LinkDest == "" && condition.Hash == "" && condition.Permissions == 0
	case LeafExactSymlink, LeafNotOwnedSymlink:
		return condition.LinkDest != "" && condition.Hash == "" && condition.Permissions == 0
	case LeafExactRegular:
		return condition.LinkDest == "" && condition.Hash != "" && condition.Permissions&^fs.ModePerm == 0
	default:
		return false
	}
}

// RequiresRegularDigest 报告复核该条件是否必须读取 regular 内容摘要。
func (condition LeafCondition) RequiresRegularDigest() bool {
	return condition.Kind == LeafExactRegular
}

// Matches 报告可信 leaf observation 是否满足当前条件。无效条件或未知 observation 永不匹配。
func (condition LeafCondition) Matches(observed Observation) bool {
	if !condition.Valid() || !validObjectKind(observed.Kind) {
		return false
	}
	switch condition.Kind {
	case LeafAny:
		return true
	case LeafMissing:
		return observed.Kind == ObjectMissing
	case LeafPresent:
		return observed.Kind != ObjectMissing
	case LeafExactSymlink:
		return observed.Kind == ObjectSymlink && observed.LinkDest == condition.LinkDest
	case LeafNotOwnedSymlink:
		return observed.Kind != ObjectSymlink || observed.LinkDest != condition.LinkDest
	case LeafExactRegular:
		return observed.Kind == ObjectRegular &&
			observed.Hash == condition.Hash &&
			observed.Mode.Perm() == condition.Permissions
	default:
		return false
	}
}

func validObjectKind(kind ObjectKind) bool {
	switch kind {
	case ObjectMissing, ObjectSymlink, ObjectRegular, ObjectDirectory, ObjectSpecial:
		return true
	default:
		return false
	}
}

// Precondition 固定一个动作提交前必须仍成立的 target identity 与最小 leaf 谓词。executor 必须
// 重新解析 TargetPath、比较 TargetResolution 并重做 control-plane boundary 校验；leaf 条件成立
// 不能替代祖先拓扑证明。
type Precondition struct {
	TargetPath           string
	TargetResolution     paths.TargetResolution
	Leaf                 LeafCondition
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
// upsert Entry 的 AppliedAt 由 executor 在动作成功时填入，不参与计划决策。
type StateEffect struct {
	Kind        StateEffectKind
	Key         string
	PreviousKey string
	Entry       HistoricalState
}

// FileAction 是 file planner 与 executor 之间的自包含值；planner 只形成和校验它。
type FileAction struct {
	Verb         FileVerb
	Target       string
	Reason       FileReason
	Desired      Desired
	Precondition Precondition
	OnSuccess    StateEffect
	OnFailure    StateEffect
}

// Clone 返回不共享 desired bytes 的动作副本。
func (action FileAction) Clone() FileAction {
	action.Desired = action.Desired.Clone()
	return action
}
