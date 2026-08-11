package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/mutation"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/spf13/cobra"
)

func printPlan(command *cobra.Command, plan planner.Plan, warnings []string) error {
	if err := printWarnings(command, warnings); err != nil {
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

func printAction(command *cobra.Command, action planner.Step) error {
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

func printOperationAnalysis(
	command *cobra.Command,
	analysis OperationAnalysis,
) error {
	if err := printWarnings(command, analysis.Warnings); err != nil {
		return err
	}
	printed := false
	for _, action := range analysis.Plan.Steps {
		if err := printAction(command, action); err != nil {
			return fmt.Errorf("write operation action: %w", err)
		}
		printed = true
	}
	for _, issue := range analysis.Plan.Issues {
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

func printIssue(command *cobra.Command, issue planner.Issue) error {
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
			strconv.Quote(issue.Reason),
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
	_, err := fmt.Fprintf(command.OutOrStdout(), " reason=%s\n", strconv.Quote(issue.Reason))
	return err
}

func printResult(
	command *cobra.Command,
	result mutation.Result,
) error {
	if err := printPlan(command, result.Plan, result.Warnings); err != nil {
		return err
	}
	if result.StateChanged {
		if err := printForgotOwnership(command, result.Plan.Steps); err != nil {
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
	actions []planner.Step,
) error {
	for _, action := range actions {
		if action.Decision != planner.DecisionForget {
			continue
		}
		evidence := "ownership"
		if action.Kind == state.KindLocal {
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
	result mutation.Result,
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

func keepStateRecorded(snapshot state.Snapshot, action planner.Step) bool {
	module, exists := snapshot.Modules[action.ModuleID]
	if !exists {
		return false
	}
	placement, exists := module.Placements[action.PlacementID]
	if !exists ||
		placement.Kind != action.Kind ||
		placement.Target != action.Target {
		return false
	}
	if action.Kind == state.KindLink {
		return placement.ResolvedTarget == action.ResolvedTarget &&
			placement.LinkDestination == action.LinkDestination
	}
	return true
}

func printStatusAnalysis(
	command *cobra.Command,
	analysis OperationAnalysis,
) error {
	if err := printWarnings(command, analysis.Warnings); err != nil {
		return fmt.Errorf("write status warning: %w", err)
	}
	modules := append([]ModuleAnalysis(nil), analysis.Modules...)
	slices.SortFunc(modules, func(left, right ModuleAnalysis) int {
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
	for _, action := range analysis.Plan.Steps {
		if action.Decision != planner.DecisionForget {
			continue
		}
		if err := printAction(command, action); err != nil {
			return fmt.Errorf("write status action: %w", err)
		}
	}
	for _, issue := range analysis.Plan.Issues {
		if err := printIssue(command, issue); err != nil {
			return fmt.Errorf("write status issue: %w", err)
		}
	}
	return nil
}
