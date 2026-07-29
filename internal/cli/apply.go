package cli

import (
	"fmt"

	"github.com/mianm12/dotfiles/internal/core/executor"
	"github.com/spf13/cobra"
)

func newApplyCommand(env environment) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "apply [MODULE]",
		Short: "Converge effective modules; persistently activate MODULE when inactive",
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
	rerun := "dot apply"
	if moduleID != nil {
		rerun = fmt.Sprintf("dot apply %s", *moduleID)
	}
	if dryRun {
		machine, err := loadRequiredMachine(context)
		if err != nil {
			return err
		}
		analysis, err := analyzeApply(context, machine, moduleID)
		if err != nil {
			return err
		}
		return printOperationAnalysis(command, analysis)
	}

	machine, err := loadRequiredMachine(context)
	if err != nil {
		return err
	}
	preflight, err := analyzeApply(context, machine, moduleID)
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
		rerun,
		func(session *executor.Session, outcome *mutationOutcome) error {
			machine, err := loadRequiredMachine(context)
			if err != nil {
				return err
			}
			locked, err := analyzeApply(context, machine, moduleID)
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
					"machine selection was saved before convergence was interrupted: %w; rerun %s",
					err,
					rerun,
				)
			}
			if env.beforeExecution != nil {
				env.beforeExecution()
			}
			result, convergeErr := session.Converge(
				locked.resolvedModules,
				locked.scope,
			)
			outcome.result = result
			if convergeErr != nil {
				if selectionChanged {
					return fmt.Errorf(
						"machine selection was saved before convergence failed: %w; rerun %s",
						convergeErr,
						rerun,
					)
				}
				return convergeErr
			}
			return nil
		},
	)
	return finishMutation(command, outcome, runErr, rerun)
}
