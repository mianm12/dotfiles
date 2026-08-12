package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/converge"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/spf13/cobra"
)

func TestFinishMutationRendersForgetOnlyAfterSuccess(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeMachine(t, []string{"base"}, nil)
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "old", PlacementID: "config"}: {
				Target:          filepath.Join(fixture.home, ".config", "old"),
				ResolvedTarget:  filepath.Join(fixture.home, ".config", "old"),
				LinkDestination: filepath.Join(fixture.repository, "modules", "old", "config"),
			},
		},
	})
	report := analyzeOperationReport(t, fixture)
	report.Warnings = []string{"synthetic input warning"}
	actions := report.Plan.Actions()
	if len(actions) != 1 || actions[0].Decision != converge.DecisionForget {
		t.Fatalf("Analyze() actions = %#v, want one forget", actions)
	}
	forget := actions[0]
	result := converge.ApplyResult{
		Report:       report,
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
		quotedReason := "reason=" + strconv.Quote(forget.Reason)
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
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "new"
source = "new"
target = "~/.new"
`, map[string]string{"new": "portable"})
	fixture.writeMachine(t, []string{"base"}, nil)
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "old", PlacementID: "stale"}: {
				Target:          filepath.Join(fixture.home, ".old"),
				ResolvedTarget:  filepath.Join(fixture.home, ".old"),
				LinkDestination: filepath.Join(fixture.repository, "modules", "old", "stale"),
			},
		},
	})
	analysis := analyzeOperationReport(t, fixture)
	actions := analysis.Plan.Actions()
	if len(actions) != 2 ||
		actions[0].Decision != converge.DecisionCreateLink ||
		actions[1].Decision != converge.DecisionForget {
		t.Fatalf("Analyze() actions = %#v, want create then forget", actions)
	}
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)

	if err := printOperationReport(command, analysis); err != nil {
		t.Fatalf("printOperationReport() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "create-link") ||
		!strings.Contains(output, "forget") ||
		!strings.Contains(
			output,
			"reason="+strconv.Quote(actions[1].Reason),
		) {
		t.Fatalf(
			"printOperationReport() stdout = %q, want every structured action",
			output,
		)
	}
}

func TestStatusAnalysisProjectsFactsActionsAndProblems(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app", "new"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
	fixture.writeModule(t, "new", `
[[links]]
id = "config"
source = "config"
target = "~/.new"
`, map[string]string{"config": "portable"})
	fixture.writeMachine(t, []string{"base"}, nil)
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "old", PlacementID: "stale"}: {
				Target:          filepath.Join(fixture.home, ".old"),
				ResolvedTarget:  filepath.Join(fixture.home, ".old"),
				LinkDestination: filepath.Join(fixture.repository, "modules", "old", "stale"),
			},
		},
	})
	writeCLIFile(t, filepath.Join(fixture.home, ".app"), "personal")
	analysis := analyzeOperationReport(t, fixture)
	if analysis.Plan.Executable() {
		t.Fatal("Analyze() plan is executable, want app target conflict")
	}
	actions := analysis.Plan.Actions()
	if len(actions) != 2 || actions[1].Decision != converge.DecisionForget {
		t.Fatalf("Analyze() actions = %#v, want create then forget", actions)
	}
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)

	if err := printStatusAnalysis(command, analysis); err != nil {
		t.Fatalf("printStatusAnalysis() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "fact module=old selection=none state=present\n") ||
		!strings.Contains(output, "action kind=create-link module=new placement=config") ||
		!strings.Contains(output, "forget") ||
		!strings.Contains(output, "reason="+strconv.Quote(actions[1].Reason)) ||
		!strings.Contains(output, "problem kind=conflict module=app placement=config") ||
		!strings.Contains(output, `reason="actual target is regular file"`) {
		t.Fatalf(
			"printStatusAnalysis() stdout = %q, want facts, actions, and problems",
			output,
		)
	}
}

func analyzeOperationReport(t *testing.T, fixture *cliTestEnv) converge.Report {
	t.Helper()
	context, err := resolveContext(fixture.env)
	if err != nil {
		t.Fatalf("resolveContext() error = %v", err)
	}
	report, err := converge.Analyze(context.environment())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	return report
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
