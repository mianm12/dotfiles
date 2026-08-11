package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/mutation"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/spf13/cobra"
)

func TestFinishMutationRendersForgetOnlyAfterSuccess(t *testing.T) {
	forget := planner.Step{
		ModuleID:    "old",
		PlacementID: "config",
		Kind:        state.KindLink,
		Decision:    planner.DecisionForget,
		Target:      "/tmp/home/.config",
		Reason:      `stale destination "changed"`,
	}
	result := mutation.Result{
		Plan:         planner.Plan{Steps: []planner.Step{forget}},
		StateChanged: true,
		Warnings:     []string{"synthetic input warning"},
	}

	for _, test := range []struct {
		name   string
		runErr error
	}{
		{name: "state commit failure", runErr: errors.New("synthetic state commit failure")},
		{name: "lock release failure", runErr: mutation.ErrLockIO},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			command := &cobra.Command{}
			command.SetOut(&stdout)
			command.SetErr(&stderr)

			err := finishMutation(command, result, test.runErr, "dot apply")

			if !errors.Is(err, test.runErr) {
				t.Fatalf("finishMutation() error = %v, want run error", err)
			}
			if stdout.Len() != 0 {
				t.Fatalf(
					"finishMutation() stdout = %q, want no action result",
					stdout.String(),
				)
			}
			if !strings.Contains(stderr.String(), "synthetic input warning") ||
				strings.Contains(stderr.String(), "forgot ownership") ||
				strings.Contains(stderr.String(), forget.Reason) {
				t.Fatalf(
					"finishMutation() stderr = %q, want only input warning",
					stderr.String(),
				)
			}
		})
	}

	t.Run("success", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		command := &cobra.Command{}
		command.SetOut(&stdout)
		command.SetErr(&stderr)

		err := finishMutation(command, result, nil, "dot apply")
		if err != nil {
			t.Fatalf("finishMutation() error = %v", err)
		}
		quotedReason := `reason="stale destination \"changed\""`
		if !strings.Contains(stdout.String(), "forget") ||
			!strings.Contains(stdout.String(), "old/config") ||
			!strings.Contains(stdout.String(), quotedReason) {
			t.Fatalf(
				"finishMutation() stdout = %q, want structured forget action",
				stdout.String(),
			)
		}
		if !strings.Contains(stderr.String(), "synthetic input warning") ||
			!strings.Contains(stderr.String(), "forgot ownership") ||
			!strings.Contains(stderr.String(), quotedReason) {
			t.Fatalf(
				"finishMutation() stderr = %q, want completed forget result",
				stderr.String(),
			)
		}
	})
}

func TestOperationAnalysisRendersEveryActionWithQuotedReason(t *testing.T) {
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	analysis := OperationAnalysis{
		Plan: planner.Plan{Steps: []planner.Step{
			{
				ModuleID:    "app",
				PlacementID: "new",
				Decision:    planner.DecisionCreateLink,
				Target:      "/tmp/home/.new",
			},
			{
				ModuleID:    "old",
				PlacementID: "stale",
				Decision:    planner.DecisionForget,
				Target:      "/tmp/home/.old",
				Reason:      `actual target is "user-owned"`,
			},
		}},
	}

	if err := printOperationAnalysis(command, analysis); err != nil {
		t.Fatalf("printOperationAnalysis() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "create-link") ||
		!strings.Contains(output, "forget") ||
		!strings.Contains(
			output,
			`reason="actual target is \"user-owned\""`,
		) {
		t.Fatalf(
			"printOperationAnalysis() stdout = %q, want every structured action",
			output,
		)
	}
}

func TestStatusAnalysisCompactsDefaultsAndAppendsDiagnosticActions(t *testing.T) {
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	analysis := OperationAnalysis{
		Modules: []ModuleAnalysis{{
			ID:            "old",
			Summary:       "stale",
			Selection:     "none",
			Applicability: "-",
			Convergence:   "pending",
			Variant:       "-",
		}},
		Plan: planner.Plan{Steps: []planner.Step{
			{
				ModuleID:    "app",
				PlacementID: "new",
				Decision:    planner.DecisionCreateLink,
				Target:      "/tmp/home/.new",
			},
			{
				ModuleID:    "old",
				PlacementID: "stale",
				Decision:    planner.DecisionForget,
				Target:      "/tmp/home/.old",
				Reason:      "stale target is absent",
			},
		}, Issues: []planner.Issue{
			{
				Kind:        planner.IssueConflict,
				ModuleID:    "app",
				PlacementID: "config",
				Target:      "/tmp/home/.app",
				Reason:      "actual target is regular file",
			},
			{
				Kind:     planner.IssueBlocked,
				ModuleID: "global",
				Reason:   "synthetic path conflict",
			},
		}},
	}

	if err := printStatusAnalysis(command, analysis); err != nil {
		t.Fatalf("printStatusAnalysis() error = %v", err)
	}

	output := stdout.String()
	if strings.Contains(output, "create-link") ||
		!strings.Contains(output, "old  stale convergence=pending\n") ||
		strings.Contains(output, "selection=none") ||
		strings.Contains(output, "applicability=-") ||
		strings.Contains(output, "variant=-") ||
		strings.Contains(output, "reason=-") ||
		!strings.Contains(output, "forget") ||
		!strings.Contains(output, `reason="stale target is absent"`) ||
		!strings.Contains(output, "conflict     app/config /tmp/home/.app") ||
		!strings.Contains(output, `reason="actual target is regular file"`) ||
		!strings.Contains(output, `blocked module=global reason="synthetic path conflict"`) {
		t.Fatalf(
			"printStatusAnalysis() stdout = %q, want compact module and diagnostic actions",
			output,
		)
	}
}

func TestStatusAnalysisRendersOnlyDistinctDimensions(t *testing.T) {
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	analysis := OperationAnalysis{Modules: []ModuleAnalysis{
		{
			ID:            "named",
			Summary:       "converged",
			Selection:     "profile",
			Applicability: "applicable",
			Convergence:   "converged",
			Variant:       "ubuntu",
			NamedVariant:  true,
		},
		{
			ID:            "gated",
			Summary:       "conflict",
			Selection:     "profile",
			Applicability: "indeterminate",
			Convergence:   "-",
			Variant:       "-",
			Reason:        "distribution is unavailable",
		},
		{
			ID:            "skipped",
			Summary:       "not-applicable",
			Selection:     "profile",
			Applicability: "not-applicable",
			Convergence:   "pending-cleanup",
			Variant:       "-",
			Reason:        "prune",
		},
	}}

	if err := printStatusAnalysis(command, analysis); err != nil {
		t.Fatalf("printStatusAnalysis() error = %v", err)
	}

	want := "gated  conflict selection=profile applicability=indeterminate " +
		"reason=\"distribution is unavailable\"\n" +
		"named  converged selection=profile variant=ubuntu\n" +
		"skipped  not-applicable selection=profile convergence=pending-cleanup " +
		"reason=\"prune\"\n"
	if output := stdout.String(); output != want {
		t.Fatalf("printStatusAnalysis() stdout = %q, want %q", output, want)
	}
}
