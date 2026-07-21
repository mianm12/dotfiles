// Package add 负责安全 add 的只读计划与后续执行协议。
package add

import (
	"errors"
	"io/fs"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
)

var (
	// ErrModuleAmbiguous 表示零个或多个候选使 module/source 无法保守唯一确定。
	ErrModuleAmbiguous = errors.New("add module selection is ambiguous")
	// ErrTemplateUnsupported 表示 M1 明确拒绝 managed/template add。
	ErrTemplateUnsupported = errors.New("add --template requires M2")
)

// Mode 表示 add 请求的 source/desired 模式。
type Mode string

const (
	// ModeLink 请求默认 link 收编。
	ModeLink Mode = "link"
	// ModeScaffold 请求一次性 scaffold 蓝本收编。
	ModeScaffold Mode = "scaffold"
	// ModeTemplate 表示 M1 必须硬拒绝的 managed/template 请求。
	ModeTemplate Mode = "template"
)

// Request 是一次 add batch 的只读计划请求。Module 为空表示保守推断。
type Request struct {
	Paths  []string
	Module string
	Mode   Mode
}

type validationSeal struct{}

var successfulPreflightSeal = &validationSeal{}

// Snapshot 保存输入普通文件在 plan 时的全部 M1 可迁移证据。
// 字段保持私有，只有成功 Preflight 返回的 sealed Snapshot 才可供 runner 消费。
type Snapshot struct {
	content  []byte
	mode     fs.FileMode
	identity paths.TargetIdentity
	seal     *validationSeal
}

// Valid 报告 snapshot 是否来自成功的完整 batch preflight。
func (snapshot Snapshot) Valid() bool { return snapshot.seal == successfulPreflightSeal }

// Content 返回输入 bytes 的独立副本。
func (snapshot Snapshot) Content() []byte { return append([]byte(nil), snapshot.content...) }

// Mode 返回输入的九位普通权限。
func (snapshot Snapshot) Mode() fs.FileMode { return snapshot.mode }

// MatchesTargetIdentity 让后续 runner 比较重新解析的 opaque identity，不暴露或复制 identity 算法。
func (snapshot Snapshot) MatchesTargetIdentity(candidate paths.TargetIdentity) bool {
	return snapshot.Valid() && snapshot.identity.Equal(candidate)
}

// ItemPlan 是单个输入的自包含只读 add 计划。
type ItemPlan struct {
	target       string
	targetPath   string
	module       string
	source       string
	sourcePath   string
	kind         manifest.FileKind
	snapshot     Snapshot
	sourceExists bool
	seal         *validationSeal
}

// Valid 报告 item 是否来自成功的完整 batch preflight。
func (item ItemPlan) Valid() bool {
	return item.seal == successfulPreflightSeal && item.snapshot.Valid()
}

// Target 返回规范化的 ~/ 展示 target。
func (item ItemPlan) Target() string { return item.target }

// TargetPath 返回绝对 target path。
func (item ItemPlan) TargetPath() string { return item.targetPath }

// Module 返回 effective module 名。
func (item ItemPlan) Module() string { return item.module }

// Source 返回 module-relative source。
func (item ItemPlan) Source() string { return item.source }

// SourcePath 返回绝对 prospective source path。
func (item ItemPlan) SourcePath() string { return item.sourcePath }

// Kind 返回已经由 manifest 证明的 desired kind。
func (item ItemPlan) Kind() manifest.FileKind { return item.kind }

// Snapshot 返回输入证据的深副本。
func (item ItemPlan) Snapshot() Snapshot { return cloneSnapshot(item.snapshot) }

// SourceExists 报告 source 是否为已验证等价的遗留普通文件。
func (item ItemPlan) SourceExists() bool { return item.sourceExists }

// BatchPlan 只有在全部输入通过时才 sealed；零值或局部构造值不可执行。
type BatchPlan struct {
	profile    string
	home       string
	repository string
	items      []ItemPlan
	seal       *validationSeal
}

// Valid 报告计划是否由成功的完整 batch preflight sealed。
func (plan BatchPlan) Valid() bool {
	if plan.seal != successfulPreflightSeal || plan.profile == "" || plan.home == "" ||
		plan.repository == "" || len(plan.items) == 0 {
		return false
	}
	for _, item := range plan.items {
		if !item.Valid() {
			return false
		}
	}
	return true
}

// Profile 返回 effective profile。
func (plan BatchPlan) Profile() string { return plan.profile }

// Home 返回 effective HOME。
func (plan BatchPlan) Home() string { return plan.home }

// Repository 返回 effective repository path。
func (plan BatchPlan) Repository() string { return plan.repository }

// Items 返回稳定排序 item 的深副本；无效计划返回 nil。
func (plan BatchPlan) Items() []ItemPlan {
	if !plan.Valid() {
		return nil
	}
	return cloneItems(plan.items)
}

func sealBatchPlan(profile, home, repository string, items []ItemPlan) BatchPlan {
	sealed := cloneItems(items)
	for index := range sealed {
		sealed[index].snapshot.seal = successfulPreflightSeal
		sealed[index].seal = successfulPreflightSeal
	}
	return BatchPlan{
		profile: profile, home: home, repository: repository,
		items: sealed, seal: successfulPreflightSeal,
	}
}

func cloneItems(items []ItemPlan) []ItemPlan {
	cloned := append([]ItemPlan(nil), items...)
	for index := range cloned {
		cloned[index].snapshot = cloneSnapshot(cloned[index].snapshot)
	}
	return cloned
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.content = append([]byte(nil), snapshot.content...)
	return snapshot
}
