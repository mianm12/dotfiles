package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/executor"
	"github.com/mianm12/dotfiles/internal/core/planner"
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
		if _, err := fmt.Fprintf(
			command.OutOrStdout(),
			"%-12s %s/%s %s\n",
			action.Decision,
			action.ModuleID,
			action.PlacementID,
			action.Target,
		); err != nil {
			return fmt.Errorf("write plan: %w", err)
		}
	}
	return nil
}

func printWarnings(command *cobra.Command, warnings []string) error {
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "warning: %s\n", warning); err != nil {
			return fmt.Errorf("write warning: %w", err)
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

type moduleStatus struct {
	id      string
	variant string
	status  string
}

func statusForModule(
	moduleID string,
	effective, notApplicable map[string]bool,
	variants map[string]string,
	statePresent bool,
	plan planner.Plan,
) moduleStatus {
	status := "inactive"
	switch {
	case notApplicable[moduleID]:
		status = "not-applicable"
	case effective[moduleID]:
		status = "converged"
	case statePresent:
		status = "stale"
	}
	for _, action := range plan.Actions {
		if action.ModuleID != moduleID {
			continue
		}
		switch action.Decision {
		case planner.DecisionConflict:
			status = "conflict"
		default:
			if status != "conflict" {
				status = "pending"
			}
		}
	}
	return moduleStatus{
		id:      moduleID,
		variant: variants[moduleID],
		status:  status,
	}
}

func printStatus(command *cobra.Command, statuses []moduleStatus, warnings []string) error {
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "warning: %s\n", warning); err != nil {
			return fmt.Errorf("write status warning: %w", err)
		}
	}
	slices.SortFunc(statuses, func(left, right moduleStatus) int {
		return strings.Compare(left.id, right.id)
	})
	for _, module := range statuses {
		variant := ""
		if module.variant != "" {
			variant = " variant=" + module.variant
		}
		if _, err := fmt.Fprintf(
			command.OutOrStdout(),
			"%s  %s%s\n",
			module.id,
			module.status,
			variant,
		); err != nil {
			return fmt.Errorf("write status: %w", err)
		}
	}
	if len(statuses) == 0 {
		if _, err := fmt.Fprintln(command.OutOrStdout(), "no modules"); err != nil {
			return fmt.Errorf("write status: %w", err)
		}
	}
	return nil
}
