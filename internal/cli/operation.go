package cli

import (
	"errors"
	"fmt"

	"github.com/mianm12/dotfiles/internal/core/converge"
	"github.com/spf13/cobra"
)

var errDryRunBlocked = errors.New("dry-run blocked")

func printDryRunAnalysis(
	command *cobra.Command,
	report converge.Report,
) error {
	if err := printOperationReport(command, report); err != nil {
		return err
	}
	if rejectAnalysis(report) != nil {
		return errDryRunBlocked
	}
	return nil
}

func finishMutation(
	command *cobra.Command,
	result converge.ApplyResult,
	runErr error,
	rerun string,
) error {
	if runErr != nil {
		// Core warnings are input diagnostics. Never project the plan or
		// completed forget results from a failed mutation.
		if warningErr := printWarnings(
			command,
			projectWarnings(result.Report.Plan, result.Report.Warnings),
		); warningErr != nil {
			return errors.Join(runErr, warningErr)
		}
		if errors.Is(runErr, converge.ErrPartial) {
			return fmt.Errorf("%w; rerun %s", runErr, rerun)
		}
		if hasControlIssue(result.Report.Plan) {
			return fmt.Errorf("%w; run `dot paths`", runErr)
		}
		return withCoreRecovery(runErr)
	}
	return printMutationResult(
		command,
		result,
		rerun,
	)
}

func finishSelectionMutation(runErr error, rerun string) error {
	if runErr == nil {
		return nil
	}
	if errors.Is(runErr, converge.ErrPartial) {
		return fmt.Errorf("%w; rerun %s", runErr, rerun)
	}
	return withCoreRecovery(runErr)
}

func withCoreRecovery(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, converge.ErrUninitialized):
		return fmt.Errorf("%w; run `dot init`", err)
	case errors.Is(err, converge.ErrControl), errors.Is(err, converge.ErrState):
		return fmt.Errorf("%w; run `dot paths`", err)
	default:
		return err
	}
}

func hasControlIssue(plan converge.Plan) bool {
	for _, issue := range plan.Issues {
		if issue.Code == converge.IssueCodeControlTopology ||
			issue.Code == converge.IssueCodeControlBoundary {
			return true
		}
	}
	return false
}

func rejectAnalysis(report converge.Report) error {
	if report.Plan.Executable() {
		return nil
	}
	if len(report.Plan.Issues) != 0 {
		issue := report.Plan.Issues[0]
		if issue.Kind == converge.IssueConflict && issue.PlacementID != "" {
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
