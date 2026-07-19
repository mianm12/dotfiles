// Package planner 形成 dot 只读计划阶段的自包含事实与动作模型。
package planner

import "io/fs"

// DesiredKind 描述 M1 planner 支持的期望文件行为。
type DesiredKind string

const (
	DesiredLink     DesiredKind = "link"
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
	ObjectMissing   ObjectKind = "missing"
	ObjectSymlink   ObjectKind = "symlink"
	ObjectRegular   ObjectKind = "regular"
	ObjectDirectory ObjectKind = "directory"
	ObjectSpecial   ObjectKind = "special"
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
	StateSymlink  StateKind = "symlink"
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

// ActionVerb 是纯计划中的稳定动作词汇；它不提供执行能力。
type ActionVerb string

const (
	ActionSkip          ActionVerb = "skip"
	ActionCreateLink    ActionVerb = "create-link"
	ActionScaffold      ActionVerb = "scaffold"
	ActionAdopt         ActionVerb = "adopt"
	ActionBackupReplace ActionVerb = "backup-replace"
	ActionPrune         ActionVerb = "prune"
	ActionRunHook       ActionVerb = "run-hook"
	ActionConflict      ActionVerb = "conflict"
)

// Precondition 固定一个动作提交前必须仍成立的 target 快照。
type Precondition struct {
	TargetPath string
	Observed   Observation
}

// StateEffectKind 描述动作成功后的 state 处置；preserve 也用于 skip/conflict/deferred/失败。
type StateEffectKind string

const (
	StatePreserve StateEffectKind = "preserve"
	StateUpsert   StateEffectKind = "upsert"
	StateDelete   StateEffectKind = "delete"
)

// StateEffect 保存动作成功后的 state 处置。Entry 只对 upsert 有效，Key 对 upsert/delete 有效。
type StateEffect struct {
	Kind  StateEffectKind
	Key   string
	Entry HistoricalState
}

// Action 是 planner 与未来 executor 之间的自包含值；当前 package 只形成和展示它。
type Action struct {
	Verb         ActionVerb
	Target       string
	Reason       string
	Desired      Desired
	HasDesired   bool
	Precondition Precondition
	StateEffect  StateEffect
}

// Clone 返回不共享 desired/observed bytes 的动作副本。
func (action Action) Clone() Action {
	action.Desired = action.Desired.Clone()
	action.Precondition.Observed = action.Precondition.Observed.Clone()
	return action
}
