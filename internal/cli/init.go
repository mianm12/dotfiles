package cli

import (
	"fmt"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/core/converge"
	"github.com/spf13/cobra"
)

func newInitCommand(env environment) *cobra.Command {
	var profiles []string
	command := &cobra.Command{
		Use:   "init [REPOSITORY]",
		Short: "Initialize this machine without changing managed targets",
		Args:  maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runInit(command, args, profiles, env)
		},
	}
	command.Flags().StringArrayVar(
		&profiles,
		"profile",
		nil,
		"activate a repository profile (repeatable)",
	)
	return command
}

func runInit(
	command *cobra.Command,
	args, profiles []string,
	env environment,
) error {
	control, err := resolveControlContext(env)
	if err != nil {
		return err
	}
	repository, err := initRepository(args, env)
	if err != nil {
		return err
	}
	result, runErr := converge.Initialize(
		control.environment(),
		repository,
		profiles,
	)
	if runErr != nil {
		return runErr
	}
	return printSelectionResult(
		command,
		"machine initialized; run dot apply",
		result.Changed,
		"dot init",
	)
}

func initRepository(args []string, env environment) (string, error) {
	var repository string
	if len(args) == 0 {
		if env.getwd == nil {
			return "", fmt.Errorf("working-directory resolver is unavailable")
		}
		current, err := env.getwd()
		if err != nil {
			return "", fmt.Errorf("resolve current directory: %w", err)
		}
		repository = current
	} else {
		repository = args[0]
	}
	if repository == "" {
		return "", fmt.Errorf("repository must be a non-empty path")
	}
	absolute, err := filepath.Abs(repository)
	if err != nil {
		return "", fmt.Errorf("resolve repository %q: %w", repository, err)
	}
	return filepath.Clean(absolute), nil
}
