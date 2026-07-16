// Package manifest 负责仓库 manifest 的 requires 预读与版本兼容性校验。
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var (
	// ErrRepositoryUnavailable 表示配置的仓库尚未安装。
	ErrRepositoryUnavailable = errors.New("repository unavailable")
	requirementPattern       = regexp.MustCompile(`^>=([0-9]+)\.([0-9]+)\.([0-9]+)$`)
	releasePattern           = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)
)

// Requirement 表示已校验的最低 CLI 版本约束。
type Requirement struct {
	// Raw 保留用户声明的原始约束，用于稳定输出和错误信息。
	Raw     string
	minimum numericVersion
}

// numericVersion 以规范化的十进制字符串保存三段版本，避免整数转换溢出。
type numericVersion [3]string

// ReadRequirement 对顶层 requires 做宽松预读，不执行完整 manifest 校验。
func ReadRequirement(repo string) (Requirement, error) {
	info, err := os.Stat(repo)
	if err != nil {
		if os.IsNotExist(err) {
			return Requirement{}, ErrRepositoryUnavailable
		}
		return Requirement{}, fmt.Errorf("inspect repository %q: %w", repo, err)
	}
	if !info.IsDir() {
		return Requirement{}, fmt.Errorf("repository path %q is not a directory", repo)
	}

	manifestPath := filepath.Join(repo, "dot.toml")
	file, err := os.Open(manifestPath)
	if err != nil {
		return Requirement{}, fmt.Errorf("open manifest %q: %w", manifestPath, err)
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
		return Requirement{}, fmt.Errorf("manifest %q: required top-level requires is missing", manifestPath)
	}

	return ParseRequirement(*document.Requires)
}

// ParseRequirement 校验当前唯一支持的 >=MAJOR.MINOR.PATCH 约束语法。
func ParseRequirement(raw string) (Requirement, error) {
	match := requirementPattern.FindStringSubmatch(raw)
	if match == nil {
		return Requirement{}, fmt.Errorf("invalid requires %q: want >=MAJOR.MINOR.PATCH", raw)
	}
	return Requirement{
		Raw:     raw,
		minimum: numericVersion{normalize(match[1]), normalize(match[2]), normalize(match[3])},
	}, nil
}

// Satisfies 判断 CLI 是否满足 requirement；dev 构建只跳过版本大小比较。
func Satisfies(cliVersion string, requirement Requirement) (satisfied, development bool, err error) {
	if cliVersion == "dev" {
		return true, true, nil
	}

	match := releasePattern.FindStringSubmatch(cliVersion)
	if match == nil {
		return false, false, fmt.Errorf("invalid CLI build version %q: want dev or vMAJOR.MINOR.PATCH", cliVersion)
	}
	current := numericVersion{normalize(match[1]), normalize(match[2]), normalize(match[3])}
	return compare(current, requirement.minimum) >= 0, false, nil
}

func normalize(component string) string {
	component = strings.TrimLeft(component, "0")
	if component == "" {
		return "0"
	}
	return component
}

// compare 先比较位数再按字典序比较，使规范化十进制字符串保持任意精度整数的顺序。
func compare(left, right numericVersion) int {
	for index := range left {
		if len(left[index]) < len(right[index]) {
			return -1
		}
		if len(left[index]) > len(right[index]) {
			return 1
		}
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
