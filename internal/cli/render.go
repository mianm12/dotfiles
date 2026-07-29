package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/executor"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/spf13/cobra"
)

func printPlan(command *cobra.Command, plan planner.Plan, warnings []string) error {
	if err := printWarnings(command, warnings); err != nil {
		return err
	}
	if len(plan.Actions) == 0 {
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

func printAction(command *cobra.Command, action planner.Action) error {
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
	if analysis.SelectionDelta.Changes() {
		if _, err := fmt.Fprintf(
			command.OutOrStdout(),
			"selection-delta %s",
			analysis.SelectionDelta.Kind,
		); err != nil {
			return fmt.Errorf("write selection delta: %w", err)
		}
		if analysis.SelectionDelta.ModuleID != "" {
			if _, err := fmt.Fprintf(
				command.OutOrStdout(),
				" module=%s",
				analysis.SelectionDelta.ModuleID,
			); err != nil {
				return fmt.Errorf("write selection delta: %w", err)
			}
		}
		if _, err := fmt.Fprintln(command.OutOrStdout()); err != nil {
			return fmt.Errorf("write selection delta: %w", err)
		}
		printed = true
	}
	for _, action := range analysis.Actions {
		if err := printAction(command, action); err != nil {
			return fmt.Errorf("write operation action: %w", err)
		}
		printed = true
	}
	for _, blocker := range analysis.Blockers {
		if _, err := fmt.Fprint(command.OutOrStdout(), "blocked"); err != nil {
			return fmt.Errorf("write operation blocker: %w", err)
		}
		if blocker.ModuleID != "" {
			if _, err := fmt.Fprintf(
				command.OutOrStdout(),
				" module=%s",
				blocker.ModuleID,
			); err != nil {
				return fmt.Errorf("write operation blocker: %w", err)
			}
		}
		if _, err := fmt.Fprintf(
			command.OutOrStdout(),
			" reason=%s\n",
			strconv.Quote(blocker.Reason),
		); err != nil {
			return fmt.Errorf("write operation blocker: %w", err)
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

func printResult(
	command *cobra.Command,
	result executor.Result,
	selectionChanged bool,
) error {
	if err := printPlan(command, result.Plan, result.Warnings); err != nil {
		return err
	}
	if result.StateChanged {
		if err := printForgotOwnership(command, result.Plan.Actions); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(
		command.OutOrStdout(),
		"selection_changed=%t targets_changed=%t state_changed=%t\n",
		selectionChanged,
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
	actions []planner.Action,
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
	result executor.Result,
	selectionChanged bool,
	rerun string,
) error {
	err := printResult(command, result, selectionChanged)
	if err == nil ||
		(!selectionChanged && !result.TargetsChanged && !result.StateChanged) {
		return err
	}
	return fmt.Errorf(
		"mutation may be partially complete after result output failed: %w; rerun %s",
		err,
		rerun,
	)
}

func keepStateRecorded(snapshot state.Snapshot, action planner.Action) bool {
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
	for _, action := range analysis.Actions {
		if action.Decision != planner.DecisionForget &&
			!isConcretePlacementConflict(action) {
			continue
		}
		if err := printAction(command, action); err != nil {
			return fmt.Errorf("write status action: %w", err)
		}
	}
	for _, blocker := range analysis.Blockers {
		if _, err := fmt.Fprint(command.OutOrStdout(), "blocked"); err != nil {
			return fmt.Errorf("write status blocker: %w", err)
		}
		if blocker.ModuleID != "" {
			if _, err := fmt.Fprintf(
				command.OutOrStdout(),
				" module=%s",
				blocker.ModuleID,
			); err != nil {
				return fmt.Errorf("write status blocker: %w", err)
			}
		}
		if _, err := fmt.Fprintf(
			command.OutOrStdout(),
			" reason=%s\n",
			strconv.Quote(blocker.Reason),
		); err != nil {
			return fmt.Errorf("write status blocker: %w", err)
		}
	}
	return nil
}
