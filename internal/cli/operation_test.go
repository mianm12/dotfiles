package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/converge"
	"github.com/spf13/cobra"
)

func TestFinishMutationRendersForgetOnlyAfterSuccess(t *testing.T) {
	forget := converge.Action{
		ModuleID:    "old",
		PlacementID: "config",
		Kind:        converge.KindLink,
		Decision:    converge.DecisionForget,
		Target:      "/tmp/home/.config",
		Reason:      `stale destination "changed"`,
	}
	result := converge.ApplyResult{
		Report: converge.Report{
			Plan: converge.Plan{Transitions: []converge.Transition{{
				ModuleID:    forget.ModuleID,
				PlacementID: forget.PlacementID,
				Actions:     []converge.Action{forget},
			}}},
			Warnings: []string{"synthetic input warning"},
		},
		StateChanged: true,
	}

	for _, test := range []struct {
		name   string
		runErr error
	}{
		{name: "state commit failure", runErr: errors.New("synthetic state commit failure")},
		{name: "lock release failure", runErr: converge.ErrPartial},
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
			!strings.Contains(stdout.String(), "module=old placement=config") ||
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

func TestOperationReportRendersEveryActionWithQuotedReason(t *testing.T) {
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	analysis := converge.Report{
		Plan: converge.Plan{Transitions: []converge.Transition{
			{ModuleID: "app", PlacementID: "new", Actions: []converge.Action{{
				ModuleID:    "app",
				PlacementID: "new",
				Decision:    converge.DecisionCreateLink,
				Target:      "/tmp/home/.new",
			}}},
			{ModuleID: "old", PlacementID: "stale", Actions: []converge.Action{{
				ModuleID:    "old",
				PlacementID: "stale",
				Decision:    converge.DecisionForget,
				Target:      "/tmp/home/.old",
				Reason:      `actual target is "user-owned"`,
			}}},
		}},
	}

	if err := printOperationReport(command, analysis); err != nil {
		t.Fatalf("printOperationReport() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "create-link") ||
		!strings.Contains(output, "forget") ||
		!strings.Contains(
			output,
			`reason="actual target is \"user-owned\""`,
		) {
		t.Fatalf(
			"printOperationReport() stdout = %q, want every structured action",
			output,
		)
	}
}

func TestStatusAnalysisProjectsFactsActionsAndProblems(t *testing.T) {
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	analysis := converge.Report{
		Facts: []converge.ModuleFact{{
			ID:           "old",
			Selection:    "none",
			StatePresent: true,
		}},
		Plan: converge.Plan{Transitions: []converge.Transition{
			{ModuleID: "app", PlacementID: "new", Actions: []converge.Action{{
				ModuleID:    "app",
				PlacementID: "new",
				Decision:    converge.DecisionCreateLink,
				Target:      "/tmp/home/.new",
			}}},
			{ModuleID: "old", PlacementID: "stale", Actions: []converge.Action{{
				ModuleID:    "old",
				PlacementID: "stale",
				Decision:    converge.DecisionForget,
				Target:      "/tmp/home/.old",
				Reason:      "stale target is absent",
			}}},
		}, Problems: []converge.Problem{
			{
				Kind:        converge.ProblemConflict,
				ModuleID:    "app",
				PlacementID: "config",
				Target:      "/tmp/home/.app",
				Reason:      "actual target is regular file",
			},
			{
				Kind:     converge.ProblemBlocked,
				ModuleID: "global",
				Reason:   "synthetic path conflict",
			},
		}},
	}

	if err := printStatusAnalysis(command, analysis); err != nil {
		t.Fatalf("printStatusAnalysis() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "fact module=old selection=none state=present\n") ||
		!strings.Contains(output, "action kind=create-link module=app placement=new") ||
		!strings.Contains(output, "forget") ||
		!strings.Contains(output, `reason="stale target is absent"`) ||
		!strings.Contains(output, "problem kind=conflict module=app placement=config") ||
		!strings.Contains(output, `reason="actual target is regular file"`) ||
		!strings.Contains(output, `problem kind=blocked module=global reason="synthetic path conflict"`) {
		t.Fatalf(
			"printStatusAnalysis() stdout = %q, want facts, actions, and problems",
			output,
		)
	}
}

func TestStatusAnalysisRendersObjectiveModuleFacts(t *testing.T) {
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	analysis := converge.Report{Facts: []converge.ModuleFact{
		{
			ID:             "named",
			Selection:      "profile",
			ManifestLoaded: true,
			Applicability:  "applicable",
			Variant:        "ubuntu",
		},
		{
			ID:             "gated",
			Selection:      "profile",
			ManifestLoaded: true,
			Applicability:  "indeterminate",
			Diagnostic:     "distribution is unavailable",
		},
		{
			ID:             "skipped",
			Selection:      "profile",
			ManifestLoaded: true,
			Applicability:  "not-applicable",
			StatePresent:   true,
		},
	}}

	if err := printStatusAnalysis(command, analysis); err != nil {
		t.Fatalf("printStatusAnalysis() error = %v", err)
	}

	want := "fact module=gated selection=profile state=absent applicability=indeterminate " +
		"reason=\"distribution is unavailable\"\n" +
		"fact module=named selection=profile state=absent applicability=applicable variant=ubuntu\n" +
		"fact module=skipped selection=profile state=present applicability=not-applicable\n"
	if output := stdout.String(); output != want {
		t.Fatalf("printStatusAnalysis() stdout = %q, want %q", output, want)
	}
}
