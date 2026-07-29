package cli

import (
	"fmt"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/executor"
	"github.com/spf13/cobra"
)

func newInitCommand(env environment) *cobra.Command {
	var profiles []string
	var dryRun bool
	command := &cobra.Command{
		Use:   "init [REPOSITORY]",
		Short: "Initialize this machine and converge its selection",
		Args:  maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
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
		analysis, err := analyzeInit(context, machine)
		if err != nil {
			return err
		}
		return printOperationAnalysis(command, analysis)
	}

	preflight, err := analyzeInit(context, machine)
	if err != nil {
		return err
	}
	if err := rejectAnalysis(preflight); err != nil {
		return err
	}
	if env.afterPreflight != nil {
		env.afterPreflight()
	}

	outcome, runErr := runMutationSession(
		context,
		preflight.ProspectiveMachine.Repository,
		"dot apply",
		func(session *executor.Session, outcome *mutationOutcome) error {
			locked, err := analyzeInit(context, machine)
			if err != nil {
				return err
			}
			if err := rejectAnalysis(locked); err != nil {
				return err
			}
			selectionChanged, err := session.PublishSelection(locked.ProspectiveMachine)
			if err != nil {
				return err
			}
			outcome.selectionChanged = selectionChanged
			if err := afterSelectionPublished(env, selectionChanged); err != nil {
				return fmt.Errorf(
					"machine selection was saved before convergence was interrupted: %w; rerun dot apply",
					err,
				)
			}
			result, convergeErr := session.Converge(
				locked.resolvedModules,
				locked.scope,
			)
			if locked.loaded.Missing {
				result.Warnings = []string{initMissingStateWarning}
			}
			outcome.result = result
			if convergeErr != nil {
				if selectionChanged {
					return fmt.Errorf(
						"machine selection was saved before convergence failed: %w; rerun dot apply",
						convergeErr,
					)
				}
				return convergeErr
			}
			return nil
		},
	)
	return finishMutation(command, outcome, runErr, "dot apply")
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
