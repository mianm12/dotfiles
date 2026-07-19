// Package state 负责 state 持久格式的严格校验与只读加载。
package state

import (
	"errors"
	"slices"
)

var (
	// ErrCorrupt 表示 state 不是当前版本语义合法、可安全消费的持久文档。
	ErrCorrupt = errors.New("state is corrupt")
	// ErrTooNew 表示 state 版本高于当前 CLI 支持的版本。
	ErrTooNew = errors.New("state version is newer than this CLI")
	// ErrUnsupportedRendered 表示 state v1 合法，但含有 M1 尚不能消费的 rendered 记录。
	ErrUnsupportedRendered = errors.New("rendered state entries are not supported in M1")
)

// Kind 是 state 记录的产物类型，不等同于 manifest 的 desired kind。
type Kind string

const (
	// KindSymlink 表示由 link desired 产生的 symlink 记录。
	KindSymlink Kind = "symlink"
	// KindRendered 表示由 managed desired 产生的普通文件记录；M1 只校验、不消费。
	KindRendered Kind = "rendered"
	// KindScaffold 表示一次性 scaffold 生命周期记录，不携带所有权证据。
	KindScaffold Kind = "scaffold"
)

// Snapshot 是完整通过 state v1 持久格式校验的只读值。
// 零值无效；调用方只能通过 Decode 或 Load 获得有效值。
type Snapshot struct {
	version int
	entries map[string]Entry
	runOnce map[string]RunOnceRecord
	valid   bool
}

// Version 返回持久格式版本；零值 Snapshot 返回 0。
func (snapshot Snapshot) Version() int {
	return snapshot.version
}

// EntryKeys 返回按字节序排列的 target 展示键。
func (snapshot Snapshot) EntryKeys() []string {
	keys := make([]string, 0, len(snapshot.entries))
	for key := range snapshot.entries {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// Entry 返回指定 target 展示键对应的记录副本。
func (snapshot Snapshot) Entry(key string) (Entry, bool) {
	entry, ok := snapshot.entries[key]
	return entry, ok
}

// RunOnceKeys 返回按字节序排列的 run_once 键。
func (snapshot Snapshot) RunOnceKeys() []string {
	keys := make([]string, 0, len(snapshot.runOnce))
	for key := range snapshot.runOnce {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// RunOnce 返回指定 hook 键对应的记录副本。
func (snapshot Snapshot) RunOnce(key string) (RunOnceRecord, bool) {
	record, ok := snapshot.runOnce[key]
	return record, ok
}

// Entry 是一个通过 v1 schema 与永久语义校验的 state entry。
type Entry struct {
	module    string
	kind      Kind
	source    string
	linkDest  string
	hash      string
	appliedAt string
}

// Module 返回记录所属模块。
func (entry Entry) Module() string { return entry.module }

// Kind 返回 state 产物类型。
func (entry Entry) Kind() Kind { return entry.kind }

// Source 返回规范化的模块相对 source。
func (entry Entry) Source() string { return entry.source }

// LinkDest 返回 symlink 的原始链接文本存证；其他 kind 返回空字符串。
func (entry Entry) LinkDest() string { return entry.linkDest }

// Hash 返回 rendered 的摘要存证；其他 kind 返回空字符串。
func (entry Entry) Hash() string { return entry.hash }

// AppliedAt 返回已校验的 RFC3339 诊断时间字符串。
func (entry Entry) AppliedAt() string { return entry.appliedAt }

// RunOnceRecord 是一个通过 v1 schema 与永久语义校验的 hook 指纹记录。
type RunOnceRecord struct {
	hash       string
	executedAt string
}

// Hash 返回 hook 指纹摘要。
func (record RunOnceRecord) Hash() string { return record.hash }

// ExecutedAt 返回已校验的 RFC3339 诊断时间字符串。
func (record RunOnceRecord) ExecutedAt() string { return record.executedAt }
