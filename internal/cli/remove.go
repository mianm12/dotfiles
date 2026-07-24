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
		prepared, _, err := prepareRemove(context, machine, moduleID)
		if err != nil {
			return err
		}
		return printPlan(command, prepared.plan, preparedWarnings(prepared))
	}

	machine, err := loadRequiredMachine(context)
	if err != nil {
		return err
	}
	preflight, _, err := prepareRemove(context, machine, moduleID)
	if err != nil {
		return err
	}
	if err := rejectConflicts(preflight.plan); err != nil {
		return err
	}

	return withMutationLock(context, func(owner mutationOwner) error {
		machine, err := loadRequiredMachine(context)
		if err != nil {
			return err
		}
		prepared, selectionNeeded, err := prepareRemove(context, machine, moduleID)
		if err != nil {
			return err
		}
		if err := rejectConflicts(prepared.plan); err != nil {
			return err
		}
		selectionChanged, err := publishSelection(context, prepared.machine, selectionNeeded)
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
			prepared.loaded.Missing &&
			len(prepared.plan.Actions) == 0 {
			return printResult(command, executor.Result{
				Plan:     prepared.plan,
				Warnings: preparedWarnings(prepared),
			}, false)
		}
		result, runErr := executePrepared(context, prepared, owner.ownership())
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
