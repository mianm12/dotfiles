package converge

import (
	"errors"
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

func (source selectionSource) label() string {
	switch {
	case source.profile && source.extra:
		return "profile+extra"
	case source.profile:
		return "profile"
	case source.extra:
		return "extra"
	default:
		return "none"
	}
}

type analysis struct {
	report      Report
	machine     config.Machine
	repository  config.Repository
	selection   selectionResolution
	loaded      state.Loaded
	controls    corepaths.Controls
	fingerprint []byte
}

// Analyze reloads every convergence input and returns one complete read-only
// report. Expressible selection and path blockers are reported in Plan.Issues.
func Analyze(environment Environment) (Report, error) {
	prepared, err := analyzeEnvironment(environment)
	if err != nil {
		return Report{}, err
	}
	return cloneReport(prepared.report), nil
}

func analyzeEnvironment(environment Environment) (analysis, error) {
	environment, err := normalizeEnvironment(environment)
	if err != nil {
		return analysis{}, err
	}
	machine, err := requireMachine(environment.ConfigPath)
	if err != nil {
		return analysis{}, err
	}
	controls := environmentControls(environment, machine.Repository)
	issues := make([]Issue, 0, 1)
	if err := validateControls(controls); err != nil {
		if !errors.Is(err, corepaths.ErrControlTopology) {
			return analysis{}, err
		}
		issues = append(issues, Issue{
			Kind:   IssueBlocked,
			Code:   IssueCodeControlTopology,
			Reason: err.Error(),
		})
	}
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return analysis{}, err
	}
	selection, err := resolveSelection(repository, machine, environment.Platform)
	if err != nil {
		return analysis{}, err
	}
	for _, issue := range selection.issues {
		issues = append(issues, Issue{
			Kind:     IssueBlocked,
			ModuleID: issue.moduleID,
			Reason:   issue.reason,
		})
	}
	loaded, err := loadState(environment.StatePath, environment.Home)
	if err != nil {
		return analysis{}, err
	}
	plan, inputWarnings, err := buildAnalysisPlan(
		environment,
		controls,
		selection.modules,
		loaded.Snapshot,
		issues,
	)
	if err != nil {
		return analysis{}, err
	}
	report := newReport(
		loaded.Snapshot,
		selection.sources,
		selection.observations,
		plan,
		appendWarning(loaded.Warning, inputWarnings),
		statusModuleIDs(repository, machine, loaded.Snapshot),
	)
	fingerprint, err := machineFingerprint(machine)
	if err != nil {
		return analysis{}, err
	}
	return analysis{
		report:      report,
		machine:     cloneMachine(machine),
		repository:  repository,
		selection:   selection,
		loaded:      loaded,
		controls:    controls,
		fingerprint: fingerprint,
	}, nil
}

func buildAnalysisPlan(
	environment Environment,
	controls corepaths.Controls,
	resolvedModules []config.Module,
	snapshot state.Snapshot,
	issues []Issue,
) (Plan, []string, error) {
	if len(issues) != 0 {
		return Plan{
			Complete: false,
			Issues:   append([]Issue(nil), issues...),
		}, nil, nil
	}

	plan, err := buildPlan(planRequest{
		Home:     environment.Home,
		Controls: controls,
		Modules:  resolvedModules,
		State:    snapshot,
	})
	if err != nil {
		return Plan{}, nil, err
	}
	warnings := make([]string, 0)
	for _, issue := range plan.Issues {
		if issue.Code == IssueCodeControlBoundary &&
			!slices.Contains(warnings, issue.Reason) {
			warnings = append(warnings, issue.Reason)
		}
	}
	return plan, warnings, nil
}

func newReport(
	snapshot state.Snapshot,
	sources map[string]selectionSource,
	observations map[string]moduleObservation,
	plan Plan,
	warnings []string,
	moduleIDs []string,
) Report {
	return Report{
		Modules: buildModuleReports(
			moduleIDs,
			sources,
			observations,
			snapshot,
			plan,
		),
		Plan: Plan{
			Complete: plan.Complete,
			Steps:    append([]Step(nil), plan.Steps...),
			Issues:   append([]Issue(nil), plan.Issues...),
		},
		Warnings: append([]string(nil), warnings...),
	}
}

func buildModuleReports(
	moduleIDs []string,
	sources map[string]selectionSource,
	observations map[string]moduleObservation,
	snapshot state.Snapshot,
	plan Plan,
) []ModuleReport {
	result := make([]ModuleReport, 0, len(moduleIDs))
	for _, moduleID := range moduleIDs {
		source := sources[moduleID]
		observation := observations[moduleID]
		_, statePresent := snapshot.Modules[moduleID]
		issueReason, directIssue := analysisIssueForModule(
			moduleID,
			source != (selectionSource{}) || statePresent,
			plan.Issues,
		)
		analysis := ModuleReport{
			ID:            moduleID,
			Summary:       "inactive",
			Selection:     source.label(),
			Applicability: "-",
			Convergence:   "-",
			Variant:       "-",
			Reason:        "",
		}
		if observation.loaded {
			analysis.Applicability = string(observation.applicability.State)
			if observation.applicability.State == config.ApplicabilityApplicable {
				analysis.Variant = observation.variant
				analysis.NamedVariant = observation.variant != ""
				if analysis.Variant == "" {
					analysis.Variant = "portable"
				}
			}
		}

		switch {
		case observation.loaded &&
			observation.applicability.State == config.ApplicabilityNotApplicable &&
			source != (selectionSource{}):
			analysis.Summary = "not-applicable"
		case source != (selectionSource{}):
			if plan.Complete {
				analysis.Summary = "converged"
				analysis.Convergence = "converged"
			} else {
				analysis.Summary = "pending"
				analysis.Convergence = "unknown"
			}
		case statePresent:
			if plan.Complete {
				analysis.Summary = "stale"
				analysis.Convergence = "pending"
			} else {
				analysis.Summary = "stale"
				analysis.Convergence = "unknown"
			}
		}
		if !plan.Complete && directIssue {
			analysis.Summary = "conflict"
			analysis.Convergence = "conflict"
		} else if !plan.Complete &&
			(source != (selectionSource{}) || statePresent) {
			analysis.Convergence = "unknown"
		}

		for _, issue := range plan.Issues {
			if issue.ModuleID != moduleID {
				continue
			}
			if issue.Kind == IssueConflict || !plan.Complete {
				analysis.Summary = "conflict"
				analysis.Convergence = "conflict"
				if !isConcretePlacementIssue(issue) {
					analysis.Reason = issue.Reason
				}
			}
		}
		for _, action := range plan.Steps {
			if action.ModuleID != moduleID {
				continue
			}
			if analysis.Convergence == "conflict" {
				continue
			}
			if source != (selectionSource{}) &&
				observation.applicability.State == config.ApplicabilityApplicable &&
				action.Decision == DecisionKeep &&
				keepStateRecorded(snapshot, action) {
				continue
			}
			if analysis.Summary == "not-applicable" {
				analysis.Convergence = "pending-cleanup"
			} else {
				analysis.Convergence = "pending"
			}
			if analysis.Summary != "stale" &&
				analysis.Summary != "not-applicable" {
				analysis.Summary = "pending"
			}
			if analysis.Reason == "" && action.Reason != "" {
				analysis.Reason = action.Reason
			} else if analysis.Reason == "" &&
				analysis.Summary == "not-applicable" {
				analysis.Reason = string(action.Decision)
			}
		}
		if analysis.Reason == "" && directIssue {
			analysis.Reason = issueReason
		}
		if analysis.Reason == "" &&
			observation.applicability.State == config.ApplicabilityIndeterminate {
			analysis.Reason = observation.applicability.Diagnostic
		}
		result = append(result, analysis)
	}
	return result
}

func keepStateRecorded(snapshot state.Snapshot, action Step) bool {
	module, exists := snapshot.Modules[action.ModuleID]
	if !exists {
		return false
	}
	placement, exists := module.Placements[action.PlacementID]
	if !exists || placement.Kind != action.Kind || placement.Target != action.Target {
		return false
	}
	if action.Kind == state.KindLink {
		return placement.ResolvedTarget == action.ResolvedTarget &&
			placement.LinkDestination == action.LinkDestination
	}
	return true
}

func isConcretePlacementIssue(issue Issue) bool {
	return issue.PlacementID != "" && issue.Target != ""
}

func analysisIssueForModule(
	moduleID string,
	includeGlobal bool,
	issues []Issue,
) (string, bool) {
	for _, issue := range issues {
		if issue.ModuleID == moduleID {
			if issue.Kind == IssueConflict && isConcretePlacementIssue(issue) {
				continue
			}
			return issue.Reason, true
		}
	}
	if includeGlobal {
		for _, issue := range issues {
			if issue.ModuleID == "" {
				return issue.Reason, false
			}
		}
	}
	return "", false
}

func statusModuleIDs(
	repository config.Repository,
	machine config.Machine,
	snapshot state.Snapshot,
) []string {
	set := make(map[string]bool)
	for _, id := range repository.ModuleIDs() {
		set[id] = true
	}
	for _, id := range machine.ExtraModules {
		set[id] = true
	}
	for id := range snapshot.Modules {
		set[id] = true
	}
	return sortedAnalysisSet(set)
}

func sortedAnalysisSet(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func appendWarning(warning string, warnings []string) []string {
	result := make([]string, 0, len(warnings)+1)
	if warning != "" {
		result = append(result, warning)
	}
	return append(result, warnings...)
}

func cloneReport(report Report) Report {
	return Report{
		Modules: append([]ModuleReport(nil), report.Modules...),
		Plan: Plan{
			Complete: report.Plan.Complete,
			Steps:    append([]Step(nil), report.Plan.Steps...),
			Issues:   append([]Issue(nil), report.Plan.Issues...),
		},
		Warnings: append([]string(nil), report.Warnings...),
	}
}
