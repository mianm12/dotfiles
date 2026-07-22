// Package paths 解析 dot 的 control-plane 路径、target 文件系统身份与祖先拓扑，
// 并区分正常缺失与不可忽略的路径错误。
package paths

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ConfigEnvironment 和 RepoEnvironment 是支持覆盖 control-plane 路径的环境变量名。
const (
	ConfigEnvironment = "DOT_CONFIG"
	RepoEnvironment   = "DOT_REPO"
)

// RepositorySource 标识 effective repo 的最高优先级来源。
type RepositorySource string

const (
	RepositorySourceFlag        RepositorySource = "flag"
	RepositorySourceEnvironment RepositorySource = "environment"
	RepositorySourceConfig      RepositorySource = "config"
	RepositorySourceDefault     RepositorySource = "default"
)

// RepositoryResolution 保存已解析的绝对 repo 路径及其来源。
type RepositoryResolution struct {
	path   string
	source RepositorySource
}

// Path 返回已解析的绝对 repo 路径。
func (resolution RepositoryResolution) Path() string { return resolution.path }

// Source 返回 repo 的决策来源。
func (resolution RepositoryResolution) Source() RepositorySource { return resolution.source }

// EffectiveHome 解析所有 dot 路径共同使用的 HOME；无论来源，结果都必须是非空绝对路径。
func EffectiveHome(override string, overrideSet bool, userHomeDir func() (string, error)) (string, error) {
	if overrideSet {
		if override == "" || !filepath.IsAbs(override) {
			return "", fmt.Errorf("--home must be a non-empty absolute path")
		}
		return filepath.Clean(override), nil
	}

	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve current user home: %w", err)
	}
	if home == "" || !filepath.IsAbs(home) {
		return "", fmt.Errorf("current user home must be a non-empty absolute path")
	}
	return filepath.Clean(home), nil
}

// ResolveControlPath 将用户提供的 control-plane 路径解析为绝对路径。
// 仅接受绝对路径、~ 或 ~/ 前缀；home 必须是 EffectiveHome 的结果。
func ResolveControlPath(value, home string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("path must not be empty")
	}

	switch {
	case value == "~":
		return home, nil
	case strings.HasPrefix(value, "~/"):
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(value, "~/"))), nil
	case strings.HasPrefix(value, "~"):
		return "", fmt.Errorf("unsupported home-relative path %q", value)
	case filepath.IsAbs(value):
		return filepath.Clean(value), nil
	default:
		return "", fmt.Errorf("path %q must be absolute or start with ~/", value)
	}
}

// Config 返回生效的机器配置路径，DOT_CONFIG 优先于默认路径。
// home 必须是 EffectiveHome 的结果。
func Config(home string, lookupEnv func(string) (string, bool)) (string, error) {
	if value, ok := lookupEnv(ConfigEnvironment); ok {
		path, err := ResolveControlPath(value, home)
		if err != nil {
			return "", fmt.Errorf("%s: %w", ConfigEnvironment, err)
		}
		return path, nil
	}
	return filepath.Join(home, ".config", "dot", "config.toml"), nil
}

// Repository 按 --repo、DOT_REPO、机器配置、默认值的顺序返回生效仓库路径。
// home 必须是 EffectiveHome 的结果。
func Repository(
	home string,
	flagValue string,
	flagSet bool,
	lookupEnv func(string) (string, bool),
	configuredValue *string,
) (string, error) {
	resolution, err := ResolveRepository(home, flagValue, flagSet, lookupEnv, configuredValue)
	if err != nil {
		return "", err
	}
	return resolution.Path(), nil
}

// ResolveRepository 按 --repo、DOT_REPO、机器配置、默认值的顺序解析 repo，
// 并保留实际选中来源供 init 决定是否持久化 override。
func ResolveRepository(
	home string,
	flagValue string,
	flagSet bool,
	lookupEnv func(string) (string, bool),
	configuredValue *string,
) (RepositoryResolution, error) {
	value := ""
	errorSource := ""
	selectedSource := RepositorySourceDefault

	switch {
	case flagSet:
		value = flagValue
		errorSource = "--repo"
		selectedSource = RepositorySourceFlag
	default:
		if environmentValue, ok := lookupEnv(RepoEnvironment); ok {
			value = environmentValue
			errorSource = RepoEnvironment
			selectedSource = RepositorySourceEnvironment
		} else if configuredValue != nil {
			value = *configuredValue
			errorSource = "machine config repo"
			selectedSource = RepositorySourceConfig
		} else {
			return RepositoryResolution{
				path:   filepath.Join(home, ".local", "share", "dot", "repo"),
				source: RepositorySourceDefault,
			}, nil
		}
	}

	path, err := ResolveControlPath(value, home)
	if err != nil {
		return RepositoryResolution{}, fmt.Errorf("%s: %w", errorSource, err)
	}
	return RepositoryResolution{path: path, source: selectedSource}, nil
}
