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
	if report.HasSkip() {
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
		warningErr := printStateWarning(command, result.Report.StateWarning)
		printErr := printCompletedLines(command, result.Done)
		return errors.Join(runErr, warningErr, printErr)
	}
	if result.Report.HasSkip() {
		if err := printOperationReport(command, result.Report); err != nil {
			return err
		}
		return errAnalysisBlocked
	}
	warningErr := printStateWarning(command, result.Report.StateWarning)
	resultErr := printMutationResult(
		command,
		result,
		rerun,
	)
	return errors.Join(warningErr, resultErr)
}
