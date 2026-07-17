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
	value := ""
	source := ""

	switch {
	case flagSet:
		value = flagValue
		source = "--repo"
	default:
		if environmentValue, ok := lookupEnv(RepoEnvironment); ok {
			value = environmentValue
			source = RepoEnvironment
		} else if configuredValue != nil {
			value = *configuredValue
			source = "machine config repo"
		} else {
			return filepath.Join(home, ".local", "share", "dot", "repo"), nil
		}
	}

	path, err := ResolveControlPath(value, home)
	if err != nil {
		return "", fmt.Errorf("%s: %w", source, err)
	}
	return path, nil
}
