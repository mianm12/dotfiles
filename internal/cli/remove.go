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
		Short: "Deactivate an extra module and clean owned links",
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
		return printOperationAnalysis(command, analysis)
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

	outcome, runErr := runMutationSession(
		context,
		preflight.ProspectiveMachine.Repository,
		fmt.Sprintf("dot remove %s", moduleID),
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
					"machine selection was saved before cleanup was interrupted: %w; rerun dot remove %s",
					err,
					moduleID,
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
				locked.resolution.Modules,
				locked.scope,
			)
			outcome.result = result
			if convergeErr != nil {
				if selectionChanged {
					return fmt.Errorf(
						"machine selection was saved before cleanup failed: %w; rerun dot remove %s",
						convergeErr,
						moduleID,
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
		fmt.Sprintf("dot remove %s", moduleID),
	)
}
