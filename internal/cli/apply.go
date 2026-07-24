package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

func newApplyCommand(env environment) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "apply [MODULE]",
		Short: "Converge all effective modules or one module",
		Args:  maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			var moduleID *string
			if len(args) == 1 {
				moduleID = &args[0]
			}
			return runApply(command, moduleID, dryRun, env)
		},
	}
	command.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "print the plan without mutation")
	return command
}

func runApply(
	command *cobra.Command,
	moduleID *string,
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
		prepared, _, err := prepareApply(context, machine, moduleID)
		if err != nil {
			return err
		}
		return printPlan(command, prepared.plan, preparedWarnings(prepared))
	}

	machine, err := loadRequiredMachine(context)
	if err != nil {
		return err
	}
	preflight, _, err := prepareApply(context, machine, moduleID)
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
		prepared, selectionNeeded, err := prepareApply(context, machine, moduleID)
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
				"machine selection was saved before convergence was interrupted: %w; rerun dot apply",
				err,
			)
		}
		if env.beforeExecution != nil {
			env.beforeExecution()
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
