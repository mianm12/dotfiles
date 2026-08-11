package cli

import (
	"errors"
	"fmt"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/mutation"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/spf13/cobra"
)

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
	return mutation.ValidateControls(context.controls(machine.Repository))
}

func finishMutation(
	command *cobra.Command,
	result mutation.Result,
	runErr error,
	rerun string,
) error {
	if runErr != nil {
		// Executor warnings are input diagnostics. Never project the plan or
		// completed forget results from a failed mutation.
		if warningErr := printWarnings(command, result.Warnings); warningErr != nil {
			return errors.Join(runErr, warningErr)
		}
		return runErr
	}
	return printMutationResult(
		command,
		result,
		rerun,
	)
}

func rejectAnalysis(analysis OperationAnalysis) error {
	if analysis.Plan.Executable() {
		return nil
	}
	if len(analysis.Plan.Issues) != 0 {
		issue := analysis.Plan.Issues[0]
		if issue.Kind == planner.IssueConflict && issue.PlacementID != "" {
			return fmt.Errorf(
				"plan conflict for %s/%s: %s",
				issue.ModuleID,
				issue.PlacementID,
				issue.Reason,
			)
		}
		if issue.ModuleID != "" {
			return fmt.Errorf(
				"operation blocked for module %q: %s",
				issue.ModuleID,
				issue.Reason,
			)
		}
		return fmt.Errorf("operation blocked: %s", issue.Reason)
	}
	return fmt.Errorf("operation blocked: convergence planning is incomplete")
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

func appendWarning(warning string, warnings []string) []string {
	result := make([]string, 0, len(warnings)+1)
	if warning != "" {
		result = append(result, warning)
	}
	return append(result, warnings...)
}
