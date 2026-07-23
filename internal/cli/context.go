package cli

import (
	"fmt"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
)

type commandContext struct {
	home       string
	configPath string
	statePath  string
	lockPath   string
	platform   config.Platform
}

func resolveContext(env environment) (commandContext, error) {
	if env.userHomeDir == nil {
		return commandContext{}, fmt.Errorf("HOME resolver is unavailable")
	}
	home, err := env.userHomeDir()
	if err != nil {
		return commandContext{}, fmt.Errorf("resolve current user HOME: %w", err)
	}
	if home == "" || !filepath.IsAbs(home) {
		return commandContext{}, fmt.Errorf("current user HOME must be a non-empty absolute path")
	}
	if env.platform == nil {
		return commandContext{}, fmt.Errorf("platform detector is unavailable")
	}
	home = filepath.Clean(home)
	stateRoot := filepath.Join(home, ".local", "state", "dot")
	return commandContext{
		home:       home,
		configPath: filepath.Join(home, ".config", "dot", "config.toml"),
		statePath:  filepath.Join(stateRoot, "state.json"),
		lockPath:   filepath.Join(stateRoot, "lock"),
		platform:   env.platform(),
	}, nil
}

func (context commandContext) controls(repository string) corepaths.Controls {
	return corepaths.Controls{
		Repository: filepath.Clean(repository),
		Config:     context.configPath,
		State:      context.statePath,
		Lock:       context.lockPath,
	}
}
