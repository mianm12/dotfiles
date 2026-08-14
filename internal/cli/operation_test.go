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

func TestFinishMutationPrintsCompletedLinesOnFailure(t *testing.T) {
	result := converge.ApplyResult{
		Report: converge.Report{StateWarning: "state is missing; links removed from desired configuration cannot be discovered"},
		Done: []converge.Line{{
			Op:          converge.OpLink,
			ModuleID:    "app",
			PlacementID: "config",
			Target:      "/tmp/home/.app",
		}},
	}
	var stdout, stderr bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	runErr := errors.New("synthetic execute failure")

	err := finishMutation(command, result, runErr, "dot apply")
	if !errors.Is(err, runErr) {
		t.Fatalf("finishMutation() error = %v, want run error", err)
	}
	if !strings.Contains(stdout.String(), "link module=app placement=config") {
		t.Fatalf("finishMutation() stdout = %q, want completed line", stdout.String())
	}
	if !strings.Contains(stderr.String(), "warning: state is missing") {
		t.Fatalf("finishMutation() stderr = %q, want state warning", stderr.String())
	}
}

func TestFinishMutationPrintsStateWarningOnSuccess(t *testing.T) {
	result := converge.ApplyResult{
		Report: converge.Report{StateWarning: "state is missing; links removed from desired configuration cannot be discovered"},
		Done: []converge.Line{{
			Op:          converge.OpLink,
			ModuleID:    "app",
			PlacementID: "config",
			Target:      "/tmp/home/.app",
		}},
		TargetsChanged: true,
		StateChanged:   true,
	}
	var stdout, stderr bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	command.SetErr(&stderr)

	if err := finishMutation(command, result, nil, "dot apply"); err != nil {
		t.Fatalf("finishMutation() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "link module=app placement=config") {
		t.Fatalf("finishMutation() stdout = %q, want completed line", stdout.String())
	}
	if !strings.Contains(stderr.String(), "warning: state is missing") {
		t.Fatalf("finishMutation() stderr = %q, want state warning", stderr.String())
	}
}

func TestFinishMutationProjectsSkipWithoutDuplicateError(t *testing.T) {
	result := converge.ApplyResult{
		Report: converge.Report{
			StateWarning: "state is missing; links removed from desired configuration cannot be discovered",
			Lines: []converge.Line{{
				Op:          converge.OpSkip,
				ModuleID:    "app",
				PlacementID: "config",
				Target:      "/home/user/.app",
				Reason:      "actual target is a regular file",
			}},
		},
	}
	var stdout, stderr bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	command.SetErr(&stderr)

	err := finishMutation(command, result, nil, "dot apply")
	if !errors.Is(err, errAnalysisBlocked) {
		t.Fatalf("finishMutation(skip) error = %v, want silent sentinel", err)
	}
	if !strings.Contains(stdout.String(), "skip module=app placement=config") ||
		!strings.Contains(stdout.String(), strconv.Quote("actual target is a regular file")) {
		t.Fatalf("blocked stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "warning: state is missing") {
		t.Fatalf("blocked stderr = %q", stderr.String())
	}
}

func TestPrintLineRendersChmodControlShape(t *testing.T) {
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	line := converge.Line{
		Op:      converge.OpChmod,
		Control: "state-root",
		Path:    "/tmp/home/.local/state/dot",
		Mode:    "0700",
	}
	if err := printLine(command, line); err != nil {
		t.Fatalf("printLine(chmod) error = %v", err)
	}
	want := "chmod control=state-root path=\"/tmp/home/.local/state/dot\" mode=0700\n"
	if got := stdout.String(); got != want {
		t.Fatalf("printLine(chmod) = %q, want %q", got, want)
	}
}

func TestRecoveryInstructionClassifiesErrorsAtCLIBoundary(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "ordinary", err: errors.New("ordinary"), want: ""},
		{name: "uninitialized", err: converge.ErrMachineUninitialized, want: "; run `dot init`"},
		{name: "control paths", err: converge.ErrControlPaths, want: "; run `dot paths`"},
		{name: "invalid state", err: state.ErrInvalid, want: "; locate state with `dot paths`, then archive or remove it outside dot"},
		{name: "legacy state", err: state.ErrLegacyVersion, want: "; locate state with `dot paths`, then archive or remove it outside dot"},
		{name: "home mismatch", err: state.ErrHomeMismatch, want: "; locate state with `dot paths`, then archive or remove it outside dot"},
		{name: "future state", err: state.ErrTooNew, want: "; preserve state and use a newer `dot` version that supports it"},
		{
			name: "mutation may have changed",
			err:  &converge.Failure{MayHaveChanged: true, Cause: errors.New("mutation")},
			want: "; rerun the complete command",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := recoveryInstruction(test.err); got != test.want {
				t.Fatalf("recoveryInstruction(%v) = %q, want %q", test.err, got, test.want)
			}
		})
	}
}

func TestIncompleteAnalysisRendersTypedFailureOnStderr(t *testing.T) {
	fixture := newCLITestEnv(t, "base = []")
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runInjected("status")
	if code != exitError || stdout != "" ||
		!strings.Contains(stderr, "failure may_have_changed=false") ||
		!strings.Contains(stderr, "run `dot init`") {
		t.Fatalf("status before init = (%d, %q, %q), want typed analysis failure", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)
}

func TestOperationReportRendersLoopLines(t *testing.T) {
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
				Target: ".old",
				Dest:   filepath.Join(fixture.repository, "modules", "old", "stale"),
			},
		},
	})
	analysis := analyzeOperationReport(t, fixture)
	if len(analysis.Lines) != 2 ||
		analysis.Lines[0].Op != converge.OpLink ||
		analysis.Lines[1].Op != converge.OpForget {
		t.Fatalf("Analyze() lines = %#v, want link then forget", analysis.Lines)
	}
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	if err := printOperationReport(command, analysis); err != nil {
		t.Fatalf("printOperationReport() error = %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "link module=app") ||
		!strings.Contains(output, "forget module=old") ||
		!strings.Contains(output, "reason="+strconv.Quote(analysis.Lines[1].Reason)) {
		t.Fatalf("printOperationReport() stdout = %q", output)
	}
}

func TestStatusAnalysisProjectsFactsAndLines(t *testing.T) {
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
				Target: ".old",
				Dest:   filepath.Join(fixture.repository, "modules", "old", "stale"),
			},
		},
	})
	writeCLIFile(t, filepath.Join(fixture.home, ".app"), "personal")
	analysis := analyzeOperationReport(t, fixture)
	if !analysis.HasSkip() {
		t.Fatal("Analyze() has no skip, want app target conflict")
	}
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	if err := printStatusAnalysis(command, analysis); err != nil {
		t.Fatalf("printStatusAnalysis() error = %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "fact module=old selection=none state=present\n") ||
		!strings.Contains(output, "link module=new placement=config") ||
		!strings.Contains(output, "forget") ||
		!strings.Contains(output, "skip module=app") {
		t.Fatalf("printStatusAnalysis() stdout = %q", output)
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
