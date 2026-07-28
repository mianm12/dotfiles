package cli

import (
	"errors"
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

	return withMutationLock(context.controls(preflight.ProspectiveMachine.Repository), func(owner mutationOwner) error {
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
		selectionChanged, err := publishSelection(
			context,
			locked.ProspectiveMachine,
			locked.SelectionDelta.Changes(),
		)
		if err != nil {
			return err
		}
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
			return printResult(command, executor.Result{
				Warnings: locked.Warnings,
			}, false)
		}
		result, runErr := executeResolved(
			context,
			locked.ProspectiveMachine.Repository,
			locked.resolution,
			locked.scope,
			owner.ownership(),
		)
		if runErr != nil {
			if warningErr := printWarnings(command, result.Warnings); warningErr != nil {
				return errors.Join(runErr, warningErr)
			}
			if selectionChanged {
				return fmt.Errorf(
					"machine selection was saved before cleanup failed: %w; rerun dot remove %s",
					runErr,
					moduleID,
				)
			}
			return runErr
		}
		return printMutationResult(
			command,
			result,
			selectionChanged,
			fmt.Sprintf("dot remove %s", moduleID),
		)
	})
}
