package cli

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/spf13/cobra"
)

func newInitCommand(env environment) *cobra.Command {
	var profiles []string
	var dryRun bool
	command := &cobra.Command{
		Use:   "init [REPOSITORY]",
		Short: "Initialize this machine and converge selected profiles",
		Args:  maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if len(profiles) == 0 {
				return usagef("dot init requires at least one --profile")
			}
			return runInit(command, args, profiles, dryRun, env)
		},
	}
	command.Flags().StringArrayVar(
		&profiles,
		"profile",
		nil,
		"activate a repository profile (repeatable)",
	)
	command.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "print the plan without mutation")
	return command
}

func runInit(
	command *cobra.Command,
	args, profiles []string,
	dryRun bool,
	env environment,
) error {
	context, err := resolveContext(env)
	if err != nil {
		return err
	}
	repository, err := initRepository(args, env)
	if err != nil {
		return err
	}
	machine := config.Machine{
		Version:      1,
		Repository:   repository,
		Profiles:     append([]string(nil), profiles...),
		ExtraModules: []string{},
	}

	if dryRun {
		if err := requireUninitialized(context); err != nil {
			return err
		}
		prepared, err := prepareInit(context, machine)
		if err != nil {
			return err
		}
		return printPlan(command, prepared.plan, preparedWarnings(prepared))
	}

	if err := requireUninitialized(context); err != nil {
		return err
	}
	preflight, err := prepareInit(context, machine)
	if err != nil {
		return err
	}
	if err := rejectConflicts(preflight.plan); err != nil {
		return err
	}

	return withMutationLock(context, func(owner mutationOwner) error {
		if err := requireUninitialized(context); err != nil {
			return err
		}
		prepared, err := prepareInit(context, machine)
		if err != nil {
			return err
		}
		if err := rejectConflicts(prepared.plan); err != nil {
			return err
		}
		selectionChanged, err := config.PublishMachine(context.configPath, machine)
		if err != nil {
			return err
		}
		if err := afterSelectionPublished(env, selectionChanged); err != nil {
			return fmt.Errorf(
				"machine selection was saved before convergence was interrupted: %w; rerun dot apply",
				err,
			)
		}
		result, runErr := executePrepared(context, prepared, owner.ownership())
		if runErr != nil {
			if warningErr := printWarnings(command, result.Warnings); warningErr != nil {
				return errors.Join(runErr, warningErr)
			}
			if selectionChanged {
				return fmt.Errorf(
					"machine selection was saved before convergence failed: %w; rerun dot apply",
					runErr,
				)
			}
			return runErr
		}
		return printMutationResult(command, result, selectionChanged, "dot apply")
	})
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

func requireUninitialized(context commandContext) error {
	_, exists, err := config.LoadMachine(context.configPath)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("machine is already initialized at %q", context.configPath)
	}
	return nil
}
