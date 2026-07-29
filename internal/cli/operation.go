package cli

import (
	"errors"
	"fmt"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/executor"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/spf13/cobra"
)

type mutationOutcome struct {
	result           executor.Result
	selectionChanged bool
}

var errDryRunBlocked = errors.New("dry-run blocked")

func printDryRunAnalysis(
	command *cobra.Command,
	analysis OperationAnalysis,
) error {
	if err := printOperationAnalysis(command, analysis); err != nil {
		return err
	}
	if rejectAnalysis(analysis) != nil {
		return errDryRunBlocked
	}
	return nil
}

func validateOperationControls(context commandContext, machine config.Machine) error {
	return executor.ValidateMutationControls(context.controls(machine.Repository))
}

func runMutationSession(
	context commandContext,
	repository string,
	rerun string,
	run func(*executor.Session, *mutationOutcome) error,
) (outcome mutationOutcome, err error) {
	session, err := executor.OpenSession(
		context.home,
		context.controls(repository),
	)
	if err != nil {
		return mutationOutcome{}, err
	}
	defer func() {
		err = joinMutationSessionErrors(err, session.Close(), rerun)
	}()
	err = run(session, &outcome)
	return outcome, err
}

func joinMutationSessionErrors(runErr, closeErr error, rerun string) error {
	if closeErr != nil {
		closeErr = fmt.Errorf(
			"release mutation lock: %w; mutation may already be applied; rerun %s",
			closeErr,
			rerun,
		)
	}
	return errors.Join(runErr, closeErr)
}

func finishMutation(
	command *cobra.Command,
	outcome mutationOutcome,
	runErr error,
	rerun string,
) error {
	if runErr != nil {
		// Executor warnings are input diagnostics. Never project the plan or
		// completed forget results from a failed mutation.
		if warningErr := printWarnings(command, outcome.result.Warnings); warningErr != nil {
			return errors.Join(runErr, warningErr)
		}
		return runErr
	}
	return printMutationResult(
		command,
		outcome.result,
		outcome.selectionChanged,
		rerun,
	)
}

func rejectAnalysis(analysis OperationAnalysis) error {
	if len(analysis.Blockers) != 0 {
		blocker := analysis.Blockers[0]
		if blocker.ModuleID != "" {
			return fmt.Errorf(
				"operation blocked for module %q: %s",
				blocker.ModuleID,
				blocker.Reason,
			)
		}
		return fmt.Errorf("operation blocked: %s", blocker.Reason)
	}
	for _, action := range analysis.Actions {
		if action.Decision == planner.DecisionConflict {
			return fmt.Errorf(
				"plan conflict for %s/%s: %s",
				action.ModuleID,
				action.PlacementID,
				action.Reason,
			)
		}
	}
	return nil
}

func loadRequiredMachine(context commandContext) (config.Machine, error) {
	machine, exists, err := config.LoadMachine(context.configPath)
	if err != nil {
		return config.Machine{}, err
	}
	if !exists {
		return config.Machine{}, fmt.Errorf(
			"machine config %q is missing; run dot init",
			context.configPath,
		)
	}
	return machine, nil
}

func afterSelectionPublished(env environment, changed bool) error {
	if !changed || env.afterSelectionPublish == nil {
		return nil
	}
	return env.afterSelectionPublish()
}

func appendWarning(warning string, warnings []string) []string {
	result := make([]string, 0, len(warnings)+1)
	if warning != "" {
		result = append(result, warning)
	}
	return append(result, warnings...)
}
