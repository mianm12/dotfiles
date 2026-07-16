package paths

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ConfigEnvironment and RepoEnvironment name the supported path override variables.
const (
	ConfigEnvironment = "DOT_CONFIG"
	RepoEnvironment   = "DOT_REPO"
)

// EffectiveHome resolves the home used by all dot paths.
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

// ResolveControlPath resolves one user-provided control-plane path.
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

// Config returns the effective machine configuration path.
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

// Repository returns the effective repository path using the documented priority order.
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
