package cli

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/converge"
	"github.com/spf13/cobra"
)

func printPlan(command *cobra.Command, plan converge.Plan) error {
	if err := printWarningIssues(command, plan.Issues); err != nil {
		return err
	}
	if len(plan.Actions) == 0 && !hasBlockerIssue(plan.Issues) {
		if _, err := fmt.Fprintln(command.OutOrStdout(), "converged"); err != nil {
			return fmt.Errorf("write plan: %w", err)
		}
		return nil
	}
	for _, action := range plan.Actions {
		if err := printAction(command, action); err != nil {
			return fmt.Errorf("write plan: %w", err)
		}
	}
	return nil
}

func printAction(command *cobra.Command, action converge.Action) error {
	if _, err := fmt.Fprintf(
		command.OutOrStdout(),
		"action kind=%s module=%s placement=%s target=%s",
		action.Decision,
		action.ModuleID,
		action.PlacementID,
		strconv.Quote(action.Target),
	); err != nil {
		return err
	}
	if action.Reason != "" {
		if _, err := fmt.Fprintf(
			command.OutOrStdout(),
			" reason=%s",
			strconv.Quote(action.Reason),
		); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(command.OutOrStdout())
	return err
}

func printWarningIssues(command *cobra.Command, issues []converge.Issue) error {
	for _, issue := range issues {
		if issue.Severity != converge.IssueWarning {
			continue
		}
		if err := writeIssue(command.ErrOrStderr(), issue); err != nil {
			return fmt.Errorf("write warning: %w", err)
		}
	}
	return nil
}

func printOperationReport(
	command *cobra.Command,
	report converge.Report,
) error {
	if err := printWarningIssues(command, report.Plan.Issues); err != nil {
		return err
	}
	printed := false
	for _, action := range report.Plan.Actions {
		if err := printAction(command, action); err != nil {
			return fmt.Errorf("write operation action: %w", err)
		}
		printed = true
	}
	for _, issue := range report.Plan.Issues {
		if issue.Severity != converge.IssueBlocker {
			continue
		}
		if err := printIssue(command, issue); err != nil {
			return fmt.Errorf("write operation issue: %w", err)
		}
		printed = true
	}
	if !printed {
		if _, err := fmt.Fprintln(command.OutOrStdout(), "converged"); err != nil {
			return fmt.Errorf("write operation analysis: %w", err)
		}
	}
	return nil
}

func printIssue(command *cobra.Command, issue converge.Issue) error {
	return writeIssue(command.OutOrStdout(), issue)
}

func writeIssue(writer io.Writer, issue converge.Issue) error {
	if _, err := fmt.Fprintf(
		writer,
		"issue severity=%s code=%s",
		issue.Severity,
		issue.Code,
	); err != nil {
		return err
	}
	if issue.ModuleID != "" {
		if _, err := fmt.Fprintf(writer, " module=%s", issue.ModuleID); err != nil {
			return err
		}
	}
	if issue.PlacementID != "" {
		if _, err := fmt.Fprintf(writer, " placement=%s", issue.PlacementID); err != nil {
			return err
		}
	}
	if issue.Target != "" {
		if _, err := fmt.Fprintf(writer, " target=%s", strconv.Quote(issue.Target)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(
		writer,
		" reason=%s recovery=%s\n",
		strconv.Quote(issue.Reason),
		issue.Recovery,
	)
	return err
}

func hasBlockerIssue(issues []converge.Issue) bool {
	return slices.ContainsFunc(issues, func(issue converge.Issue) bool {
		return issue.Severity == converge.IssueBlocker
	})
}

func printResult(
	command *cobra.Command,
	result converge.ApplyResult,
) error {
	if err := printPlan(command, result.Report.Plan); err != nil {
		return err
	}
	if result.StateChanged {
		if err := printForgotOwnership(command, result.Report.Plan.Actions); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(
		command.OutOrStdout(),
		"targets_changed=%t state_changed=%t\n",
		result.TargetsChanged,
		result.StateChanged,
	)
	if err != nil {
		return fmt.Errorf("write mutation result: %w", err)
	}
	return nil
}

func printForgotOwnership(
	command *cobra.Command,
	actions []converge.Action,
) error {
	for _, action := range actions {
		if action.Decision != converge.DecisionForget {
			continue
		}
		if _, err := fmt.Fprintf(
			command.ErrOrStderr(),
			"warning: forgot ownership for %s/%s %s",
			action.ModuleID,
			action.PlacementID,
			action.Target,
		); err != nil {
			return fmt.Errorf("write forgot ownership result: %w", err)
		}
		if action.Reason != "" {
			if _, err := fmt.Fprintf(
				command.ErrOrStderr(),
				" reason=%s",
				strconv.Quote(action.Reason),
			); err != nil {
				return fmt.Errorf("write forgot ownership result: %w", err)
			}
		}
		if _, err := fmt.Fprintln(command.ErrOrStderr()); err != nil {
			return fmt.Errorf("write forgot ownership result: %w", err)
		}
	}
	return nil
}

func printMutationResult(
	command *cobra.Command,
	result converge.ApplyResult,
	rerun string,
) error {
	err := printResult(command, result)
	if err == nil ||
		(!result.TargetsChanged && !result.StateChanged) {
		return err
	}
	return fmt.Errorf(
		"mutation may be partially complete after result output failed: %w; rerun %s",
		err,
		rerun,
	)
}

func printStatusAnalysis(
	command *cobra.Command,
	report converge.Report,
) error {
	if err := printWarningIssues(command, report.Plan.Issues); err != nil {
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
	for _, action := range report.Plan.Actions {
		if err := printAction(command, action); err != nil {
			return fmt.Errorf("write status action: %w", err)
		}
	}
	for _, issue := range report.Plan.Issues {
		if issue.Severity != converge.IssueBlocker {
			continue
		}
		if err := printIssue(command, issue); err != nil {
			return fmt.Errorf("write status issue: %w", err)
		}
	}
	return nil
}
