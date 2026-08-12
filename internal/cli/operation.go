package cli

import (
	"errors"

	"github.com/mianm12/dotfiles/internal/core/converge"
	"github.com/spf13/cobra"
)

var errAnalysisBlocked = errors.New("analysis blocked")

func printDryRunAnalysis(
	command *cobra.Command,
	report converge.Report,
) error {
	if err := printOperationReport(command, report); err != nil {
		return err
	}
	if !report.Plan.Executable() {
		return errAnalysisBlocked
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
		// Never project planned actions or completed forget results from a
		// failed mutation. Warning Issues remain input diagnostics.
		if warningErr := printWarningIssues(command, result.Report.Plan.Issues); warningErr != nil {
			return errors.Join(runErr, warningErr)
		}
		return runErr
	}
	if result.Status == converge.ApplyStatusBlocked {
		if err := printOperationReport(command, result.Report); err != nil {
			return err
		}
		return errAnalysisBlocked
	}
	return printMutationResult(
		command,
		result,
		rerun,
	)
}
