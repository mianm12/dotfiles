// Package manifest 负责仓库 manifest 的两阶段加载、profile 展开与 effective 配置解析。
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mianm12/dotfiles/internal/buildinfo"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/pelletier/go-toml/v2"
)

const filename = "dot.toml"

var (
	// ErrRepositoryUnavailable 表示配置的仓库尚未安装。
	ErrRepositoryUnavailable = errors.New("repository unavailable")
	// ErrInvalidRequirement 标记 manifest 中缺失或语法非法的 requires。
	// 错误仍保留原始上下文，供 doctor 区分可独立继续的 compatibility 诊断。
	ErrInvalidRequirement = errors.New("invalid manifest requires")
	requirementPattern    = regexp.MustCompile(`^>=([0-9]+)\.([0-9]+)\.([0-9]+)$`)
	releasePattern        = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)
)

type invalidRequirementError struct {
	err error
}

func (e invalidRequirementError) Error() string {
	return e.err.Error()
}

func (e invalidRequirementError) Unwrap() []error {
	return []error{ErrInvalidRequirement, e.err}
}

func markInvalidRequirement(err error) error {
	return invalidRequirementError{err: err}
}

// Requirement 表示已校验的最低 CLI 版本约束；零值无效。
// 应通过 ParseRequirement 或 ReadRequirement 创建。
type Requirement struct {
	raw     string
	minimum numericVersion
}

// String 返回用户声明的原始约束，用于稳定输出和错误信息。
func (r Requirement) String() string {
	return r.raw
}

// numericVersion 以规范化的十进制字符串保存三段版本，避免整数转换溢出。
type numericVersion [3]string

// ReadRequirement 对顶层 requires 做宽松预读，不执行完整 manifest 校验。
// repo 的解析不属于本函数职责，调用方应传入路径层已经解析的绝对路径。
func ReadRequirement(repo string) (Requirement, error) {
	info, err := os.Stat(repo)
	if err != nil {
		if paths.IsMissing(repo, err) {
			return Requirement{}, ErrRepositoryUnavailable
		}
		return Requirement{}, fmt.Errorf("inspect repository %q: %w", repo, err)
	}
	if !info.IsDir() {
		return Requirement{}, fmt.Errorf("repository path %q is not a directory", repo)
	}

	manifestPath := filepath.Join(repo, filename)
	file, err := openManifest(manifestPath)
	if err != nil {
		return Requirement{}, err
	}

	// version 命令只依赖 requires；其他字段留给完整 manifest loader 校验。
	var document struct {
		Requires *string `toml:"requires"`
	}
	// 不使用 defer，以便报告 Close 错误；Decode 与 Close 均失败时优先返回 Decode 错误。
	decodeErr := toml.NewDecoder(file).Decode(&document)
	closeErr := file.Close()
	if decodeErr != nil {
		return Requirement{}, fmt.Errorf("decode manifest %q for requires: %w", manifestPath, decodeErr)
	}
	if closeErr != nil {
		return Requirement{}, fmt.Errorf("close manifest %q after reading: %w", manifestPath, closeErr)
	}
	if document.Requires == nil {
		return Requirement{}, markInvalidRequirement(
			fmt.Errorf("manifest %q: required top-level requires is missing", manifestPath),
		)
	}

	requirement, err := ParseRequirement(*document.Requires)
	if err != nil {
		return Requirement{}, markInvalidRequirement(fmt.Errorf("manifest %q: %w", manifestPath, err))
	}
	return requirement, nil
}

// ParseRequirement 校验当前唯一支持的 >=MAJOR.MINOR.PATCH 约束语法。
func ParseRequirement(raw string) (Requirement, error) {
	match := requirementPattern.FindStringSubmatch(raw)
	if match == nil {
		return Requirement{}, fmt.Errorf("invalid requires %q: want >=MAJOR.MINOR.PATCH", raw)
	}
	return Requirement{
		raw:     raw,
		minimum: newNumericVersion(match[1], match[2], match[3]),
	}, nil
}

// Satisfies 判断 CLI 是否满足 requirement；dev 构建只跳过版本大小比较。
func Satisfies(cliVersion string, requirement Requirement) (satisfied, developmentBuild bool, err error) {
	if requirement.raw == "" || requirement.minimum == (numericVersion{}) {
		return false, false, errors.New("invalid zero-value requirement")
	}
	if cliVersion == buildinfo.DevelopmentVersion {
		return true, true, nil
	}

	match := releasePattern.FindStringSubmatch(cliVersion)
	if match == nil {
		return false, false, fmt.Errorf(
			"invalid CLI build version %q: want %s or vMAJOR.MINOR.PATCH",
			cliVersion,
			buildinfo.DevelopmentVersion,
		)
	}
	current := newNumericVersion(match[1], match[2], match[3])
	return current.compare(requirement.minimum) >= 0, false, nil
}

func newNumericVersion(major, minor, patch string) numericVersion {
	return numericVersion{
		normalizeVersionComponent(major),
		normalizeVersionComponent(minor),
		normalizeVersionComponent(patch),
	}
}

func normalizeVersionComponent(component string) string {
	component = strings.TrimLeft(component, "0")
	if component == "" {
		return "0"
	}
	return component
}

// compare 先比较位数再按字典序比较，使规范化十进制字符串保持任意精度整数的顺序。
func (v numericVersion) compare(other numericVersion) int {
	for i := range v {
		if len(v[i]) < len(other[i]) {
			return -1
		}
		if len(v[i]) > len(other[i]) {
			return 1
		}
		if v[i] < other[i] {
			return -1
		}
		if v[i] > other[i] {
			return 1
		}
	}
	return 0
}
