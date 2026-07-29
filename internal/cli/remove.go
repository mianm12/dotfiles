package cli

import (
	"fmt"

	"github.com/mianm12/dotfiles/internal/core/executor"
	"github.com/spf13/cobra"
)

func newRemoveCommand(env environment) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "remove MODULE",
		Short: "Remove MODULE from extra selection and converge managed targets",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runRemove(command, args[0], dryRun, env)
		},
	}
	command.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "print the plan without mutation")
	return command
}

func runRemove(
	command *cobra.Command,
	moduleID string,
	dryRun bool,
	env environment,
) error {
	context, err := resolveContext(env)
	if err != nil {
		return err
	}
	if dryRun {
		machine, err := loadRequiredMachine(context)
		if err != nil {
			return err
		}
		analysis, err := analyzeRemove(context, machine, moduleID)
		if err != nil {
			return err
		}
		return printDryRunAnalysis(command, analysis)
	}

	machine, err := loadRequiredMachine(context)
	if err != nil {
		return err
	}
	preflight, err := analyzeRemove(context, machine, moduleID)
	if err != nil {
		return err
	}
	if err := rejectAnalysis(preflight); err != nil {
		return err
	}
	if env.afterPreflight != nil {
		env.afterPreflight()
	}

	rerun := "dot apply"
	outcome, runErr := runMutationSession(
		context,
		preflight.ProspectiveMachine.Repository,
		rerun,
		func(session *executor.Session, outcome *mutationOutcome) error {
			machine, err := loadRequiredMachine(context)
			if err != nil {
				return err
			}
			locked, err := analyzeRemove(context, machine, moduleID)
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
					"machine selection was saved before cleanup was interrupted: %w; rerun %s",
					err,
					rerun,
				)
			}
			if !selectionChanged &&
				locked.loaded.Missing &&
				len(locked.Actions) == 0 {
				outcome.result = executor.Result{
					Warnings: locked.Warnings,
				}
				return nil
			}
			result, convergeErr := session.Converge(
				locked.resolvedModules,
				locked.scope,
			)
			outcome.result = result
			if convergeErr != nil {
				if selectionChanged {
					return fmt.Errorf(
						"machine selection was saved before cleanup failed: %w; rerun %s",
						convergeErr,
						rerun,
					)
				}
				return convergeErr
			}
			return nil
		},
	)
	return finishMutation(
		command,
		outcome,
		runErr,
		rerun,
	)
}
