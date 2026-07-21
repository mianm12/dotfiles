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

// Snapshot 保存输入普通文件在 plan 时的全部 M1 可迁移证据。
type Snapshot struct {
	Content  []byte
	Mode     fs.FileMode
	Identity paths.TargetIdentity
}

// ItemPlan 是单个输入的自包含只读 add 计划。
type ItemPlan struct {
	Target       string
	TargetPath   string
	Module       string
	Source       string
	SourcePath   string
	Kind         manifest.FileKind
	Snapshot     Snapshot
	SourceExists bool
}

// BatchPlan 只有在全部输入通过时才包含 Items；零值不是可执行计划。
type BatchPlan struct {
	Profile    string
	Home       string
	Repository string
	Items      []ItemPlan
}
