package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/converge"
	"github.com/spf13/cobra"
)

func printLines(command *cobra.Command, lines []converge.Line) error {
	if len(lines) == 0 {
		if _, err := fmt.Fprintln(command.OutOrStdout(), "converged"); err != nil {
			return fmt.Errorf("write lines: %w", err)
		}
		return nil
	}
	for _, line := range lines {
		if err := printLine(command, line); err != nil {
			return fmt.Errorf("write lines: %w", err)
		}
	}
	return nil
}

func printLine(command *cobra.Command, line converge.Line) error {
	if line.Op == converge.OpChmod {
		_, err := fmt.Fprintf(
			command.OutOrStdout(),
			"chmod control=%s path=%s mode=%s\n",
			line.Control,
			strconv.Quote(line.Path),
			line.Mode,
		)
		return err
	}
	if _, err := fmt.Fprintf(
		command.OutOrStdout(),
		"%s module=%s",
		line.Op,
		line.ModuleID,
	); err != nil {
		return err
	}
	if line.PlacementID != "" {
		if _, err := fmt.Fprintf(command.OutOrStdout(), " placement=%s", line.PlacementID); err != nil {
			return err
		}
	}
	if line.Target != "" {
		if _, err := fmt.Fprintf(
			command.OutOrStdout(),
			" target=%s",
			strconv.Quote(line.Target),
		); err != nil {
			return err
		}
	}
	if line.Reason != "" {
		if _, err := fmt.Fprintf(
			command.OutOrStdout(),
			" reason=%s",
			strconv.Quote(line.Reason),
		); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(command.OutOrStdout())
	return err
}

func printStateWarning(command *cobra.Command, warning string) error {
	if warning == "" {
		return nil
	}
	_, err := fmt.Fprintf(command.ErrOrStderr(), "warning: %s\n", warning)
	if err != nil {
		return fmt.Errorf("write warning: %w", err)
	}
	return nil
}

func printOperationReport(
	command *cobra.Command,
	report converge.Report,
) error {
	if err := printStateWarning(command, report.StateWarning); err != nil {
		return err
	}
	return printLines(command, report.Lines)
}

func printStatusAnalysis(
	command *cobra.Command,
	report converge.Report,
) error {
	if err := printStateWarning(command, report.StateWarning); err != nil {
		return fmt.Errorf("write status warning: %w", err)
	}
	facts := append([]converge.ModuleFact(nil), report.Facts...)
	slices.SortFunc(facts, func(left, right converge.ModuleFact) int {
		return strings.Compare(left.ID, right.ID)
	})
	for _, fact := range facts {
		stateFact := "absent"
		if fact.StatePresent {
			stateFact = "present"
		}
		line := "fact module=" + fact.ID +
			" selection=" + fact.Selection +
			" state=" + stateFact
		if fact.ManifestLoaded {
			line += " applicability=" + fact.Applicability
		}
		if fact.Variant != "" {
			line += " variant=" + fact.Variant
		}
		if fact.Diagnostic != "" {
			line += " reason=" + strconv.Quote(fact.Diagnostic)
		}
		if _, err := fmt.Fprintln(command.OutOrStdout(), line); err != nil {
			return fmt.Errorf("write status: %w", err)
		}
	}
	if len(facts) == 0 {
		if _, err := fmt.Fprintln(command.OutOrStdout(), "no modules"); err != nil {
			return fmt.Errorf("write status: %w", err)
		}
	}
	for _, line := range report.Lines {
		if err := printLine(command, line); err != nil {
			return fmt.Errorf("write status line: %w", err)
		}
	}
	return nil
}

func printMutationResult(
	command *cobra.Command,
	result converge.ApplyResult,
	rerun string,
) error {
	err := printLines(command, result.Done)
	if err == nil ||
		(!result.TargetsChanged && !result.StateChanged && !result.ControlsChanged) {
		return err
	}
	return fmt.Errorf(
		"mutation may be partially complete after result output failed: %w; rerun %s",
		err,
		rerun,
	)
}

func printCompletedLines(command *cobra.Command, lines []converge.Line) error {
	for _, line := range lines {
		if err := printLine(command, line); err != nil {
			return err
		}
	}
	return nil
}
