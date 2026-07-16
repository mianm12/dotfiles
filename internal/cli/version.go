package cli

import (
	"errors"
	"fmt"

	"github.com/ghstlnx/dotfiles/internal/config"
	"github.com/ghstlnx/dotfiles/internal/manifest"
	"github.com/ghstlnx/dotfiles/internal/paths"
	"github.com/spf13/cobra"
)

type versionOptions struct {
	home    string
	homeSet bool
	repo    string
	repoSet bool
}

func newVersionCommand(env environment, global *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show build and repository compatibility information",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runVersion(command, versionOptions{
				home:    global.home,
				homeSet: command.Flags().Changed("home"),
				repo:    global.repo,
				repoSet: command.Flags().Changed("repo"),
			}, env)
		},
	}
}

func runVersion(command *cobra.Command, options versionOptions, env environment) error {
	command.Printf("version=%s\n", env.build.Version)
	command.Printf("commit=%s\n", env.build.Commit)
	command.Printf("build_time=%s\n", env.build.BuildTime)

	home, err := paths.EffectiveHome(options.home, options.homeSet, env.userHomeDir)
	if err != nil {
		return reportVersionError(command, err)
	}
	configPath, err := paths.Config(home, env.lookupEnv)
	if err != nil {
		return reportVersionError(command, err)
	}
	machine, exists, err := config.Load(configPath)
	if err != nil {
		return reportVersionError(command, err)
	}
	if exists && machine.Repo != nil {
		if _, err := paths.ResolveControlPath(*machine.Repo, home); err != nil {
			return reportVersionError(command, fmt.Errorf("machine config repo: %w", err))
		}
	}

	repo, err := paths.Repository(home, options.repo, options.repoSet, env.lookupEnv, machine.Repo)
	if err != nil {
		return reportVersionError(command, err)
	}
	requirement, err := manifest.ReadRequirement(repo)
	if errors.Is(err, manifest.ErrRepositoryUnavailable) {
		command.Println("requires=unavailable")
		return nil
	}
	if err != nil {
		return reportVersionError(command, err)
	}

	command.Printf("requires=%s\n", requirement.Raw)
	satisfied, development, err := manifest.Satisfies(env.build.Version, requirement)
	if err != nil {
		command.Println("satisfied=error")
		return err
	}
	command.Printf("satisfied=%t\n", satisfied)
	if development {
		command.Println("compatibility=development-build")
		command.PrintErrln("warning: development build skipped the requires version comparison")
		return nil
	}
	if !satisfied {
		return fmt.Errorf("CLI %s does not satisfy %s; run dot self-update", env.build.Version, requirement.Raw)
	}
	return nil
}

func reportVersionError(command *cobra.Command, err error) error {
	command.Println("requires=error")
	return err
}
