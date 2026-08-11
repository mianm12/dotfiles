package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/converge"
	"github.com/spf13/cobra"
)

func printPlan(command *cobra.Command, plan converge.Plan, warnings []string) error {
	if err := printWarnings(command, projectWarnings(plan, warnings)); err != nil {
		return err
	}
	if len(plan.Steps) == 0 && len(plan.Issues) == 0 {
		if _, err := fmt.Fprintln(command.OutOrStdout(), "converged"); err != nil {
			return fmt.Errorf("write plan: %w", err)
		}
		return nil
	}
	for _, action := range plan.Steps {
		if err := printAction(command, action); err != nil {
			return fmt.Errorf("write plan: %w", err)
		}
	}
	return nil
}

func printAction(command *cobra.Command, action converge.Step) error {
	if _, err := fmt.Fprintf(
		command.OutOrStdout(),
		"%-12s %s/%s %s",
		action.Decision,
		action.ModuleID,
		action.PlacementID,
		action.Target,
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

func printWarnings(command *cobra.Command, warnings []string) error {
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "warning: %s\n", warning); err != nil {
			return fmt.Errorf("write warning: %w", err)
		}
	}
	return nil
}

func printOperationReport(
	command *cobra.Command,
	report converge.Report,
) error {
	if err := printWarnings(command, projectWarnings(report.Plan, report.Warnings)); err != nil {
		return err
	}
	printed := false
	for _, action := range report.Plan.Steps {
		if err := printAction(command, action); err != nil {
			return fmt.Errorf("write operation action: %w", err)
		}
		printed = true
	}
	for _, issue := range report.Plan.Issues {
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
	reason := issue.Reason
	if issue.Code == converge.IssueCodeControlTopology ||
		issue.Code == converge.IssueCodeControlBoundary {
		reason += "; run `dot paths`"
	}
	if isConcretePlacementIssue(issue) {
		if _, err := fmt.Fprintf(
			command.OutOrStdout(),
			"%-12s %s/%s %s",
			issue.Kind,
			issue.ModuleID,
			issue.PlacementID,
			issue.Target,
		); err != nil {
			return err
		}
		_, err := fmt.Fprintf(
			command.OutOrStdout(),
			" reason=%s\n",
			strconv.Quote(reason),
		)
		return err
	}
	if _, err := fmt.Fprint(command.OutOrStdout(), issue.Kind); err != nil {
		return err
	}
	if issue.ModuleID != "" {
		if _, err := fmt.Fprintf(command.OutOrStdout(), " module=%s", issue.ModuleID); err != nil {
			return err
		}
	}
	if issue.PlacementID != "" {
		if _, err := fmt.Fprintf(command.OutOrStdout(), " placement=%s", issue.PlacementID); err != nil {
			return err
		}
	}
	if issue.Target != "" {
		if _, err := fmt.Fprintf(command.OutOrStdout(), " target=%s", issue.Target); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(command.OutOrStdout(), " reason=%s\n", strconv.Quote(reason))
	return err
}

func isConcretePlacementIssue(issue converge.Issue) bool {
	return issue.PlacementID != "" && issue.Target != ""
}

func printResult(
	command *cobra.Command,
	result converge.ApplyResult,
) error {
	if err := printPlan(command, result.Report.Plan, result.Report.Warnings); err != nil {
		return err
	}
	if result.StateChanged {
		if err := printForgotOwnership(command, result.Report.Plan.Steps); err != nil {
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
	actions []converge.Step,
) error {
	for _, action := range actions {
		if action.Decision != converge.DecisionForget {
			continue
		}
		evidence := "ownership"
		if action.Kind == converge.KindLocal {
			evidence = "provenance"
		}
		if _, err := fmt.Fprintf(
			command.ErrOrStderr(),
			"warning: forgot %s for %s/%s %s",
			evidence,
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
	if err := printWarnings(command, projectWarnings(report.Plan, report.Warnings)); err != nil {
		return fmt.Errorf("write status warning: %w", err)
	}
	modules := append([]converge.ModuleReport(nil), report.Modules...)
	slices.SortFunc(modules, func(left, right converge.ModuleReport) int {
		return strings.Compare(left.ID, right.ID)
	})
	for _, module := range modules {
		line := module.ID + "  " + module.Summary
		if module.Selection != "none" {
			line += " selection=" + module.Selection
		}
		if module.Applicability != "-" &&
			module.Applicability != "applicable" &&
			(module.Summary != "not-applicable" ||
				module.Applicability != "not-applicable") {
			line += " applicability=" + module.Applicability
		}
		if module.Convergence != "-" && module.Convergence != module.Summary {
			line += " convergence=" + module.Convergence
		}
		if module.NamedVariant {
			line += " variant=" + module.Variant
		}
		if module.Reason != "" {
			line += " reason=" + strconv.Quote(module.Reason)
		}
		if _, err := fmt.Fprintln(command.OutOrStdout(), line); err != nil {
			return fmt.Errorf("write status: %w", err)
		}
	}
	if len(modules) == 0 {
		if _, err := fmt.Fprintln(command.OutOrStdout(), "no modules"); err != nil {
			return fmt.Errorf("write status: %w", err)
		}
	}
	for _, action := range report.Plan.Steps {
		if action.Decision != converge.DecisionForget {
			continue
		}
		if err := printAction(command, action); err != nil {
			return fmt.Errorf("write status action: %w", err)
		}
	}
	for _, issue := range report.Plan.Issues {
		if err := printIssue(command, issue); err != nil {
			return fmt.Errorf("write status issue: %w", err)
		}
	}
	return nil
}

func projectWarnings(plan converge.Plan, warnings []string) []string {
	projected := append([]string(nil), warnings...)
	for index, warning := range projected {
		for _, issue := range plan.Issues {
			if issue.Code == converge.IssueCodeControlBoundary &&
				issue.Reason == warning {
				projected[index] += "; run `dot paths`"
				break
			}
		}
	}
	return projected
}
