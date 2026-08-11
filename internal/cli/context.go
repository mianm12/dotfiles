package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/converge"
)

type controlContext struct {
	home       string
	configPath string
	statePath  string
	lockPath   string
}

type commandContext struct {
	controlContext
	platform config.Platform
}

func resolveControlContext(env environment) (controlContext, error) {
	if env.userHomeDir == nil {
		return controlContext{}, fmt.Errorf("HOME resolver is unavailable")
	}
	home, err := env.userHomeDir()
	if err != nil {
		return controlContext{}, fmt.Errorf("resolve current user HOME: %w", err)
	}
	if home == "" || !filepath.IsAbs(home) {
		return controlContext{}, fmt.Errorf("current user HOME must be a non-empty absolute path")
	}
	if strings.ContainsRune(home, '\x00') || !utf8.ValidString(home) {
		return controlContext{}, fmt.Errorf(
			"current user HOME must be a non-empty absolute path without NUL and with valid UTF-8",
		)
	}
	home = filepath.Clean(home)
	stateRoot := filepath.Join(home, ".local", "state", "dot")
	return controlContext{
		home:       home,
		configPath: filepath.Join(home, ".config", "dot", "config.toml"),
		statePath:  filepath.Join(stateRoot, "state.json"),
		lockPath:   filepath.Join(stateRoot, "lock"),
	}, nil
}

func resolveContext(env environment) (commandContext, error) {
	control, err := resolveControlContext(env)
	if err != nil {
		return commandContext{}, err
	}
	if env.platform == nil {
		return commandContext{}, fmt.Errorf("platform detector is unavailable")
	}
	return commandContext{
		controlContext: control,
		platform:       env.platform(),
	}, nil
}

func (context commandContext) environment() converge.Environment {
	return converge.Environment{
		Home:       context.home,
		ConfigPath: context.configPath,
		StatePath:  context.statePath,
		LockPath:   context.lockPath,
		Platform:   context.platform,
	}
}

func (context controlContext) environment() converge.Environment {
	return commandContext{controlContext: context}.environment()
}
