package cli

import (
	"errors"
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/planner"
	coreselection "github.com/mianm12/dotfiles/internal/core/selection"
	"github.com/mianm12/dotfiles/internal/core/state"
)

// ModuleAnalysis is the orthogonal status projection for one inventory module.
type ModuleAnalysis struct {
	ID            string
	Summary       string
	Selection     string
	Applicability string
	Convergence   string
	Variant       string
	NamedVariant  bool
	Reason        string
}

// OperationAnalysis is a complete read-only projection of one CLI operation.
// It is never accepted as executor input.
type OperationAnalysis struct {
	Machine config.Machine
	Modules []ModuleAnalysis
	Plan    planner.Plan
	// Warnings contains input diagnostics, never action outcomes.
	Warnings []string
}

type selectionSource struct {
	profile bool
	extra   bool
}

func (source selectionSource) String() string {
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

type moduleObservation struct {
	loaded        bool
	applicability config.ModuleApplicability
	variant       string
}

type analysisInputs struct {
	repository config.Repository
	loaded     state.Loaded
	issues     []planner.Issue
}

func analyzeApply(
	context commandContext,
	current config.Machine,
) (OperationAnalysis, error) {
	current = cloneMachine(current)
	inputs, err := loadAnalysisInputs(context, current)
	if err != nil {
		return OperationAnalysis{}, err
	}

	resolvedModules, sources, observations, selectionBlockers, err := resolveSelection(
		inputs.repository,
		current,
		context.platform,
	)
	if err != nil {
		return OperationAnalysis{}, err
	}
	issues := append(append([]planner.Issue(nil), inputs.issues...), selectionBlockers...)
	plan, inputWarnings, err := buildAnalysisPlan(
		context,
		current,
		resolvedModules,
		inputs.loaded.Snapshot,
		issues,
	)
	if err != nil {
		return OperationAnalysis{}, err
	}
	return newOperationAnalysis(
		current,
		inputs.loaded.Snapshot,
		sources,
		observations,
		plan,
		appendWarning(inputs.loaded.Warning, inputWarnings),
		analysisModuleIDs(sources, inputs.loaded.Snapshot, plan.Issues),
	), nil
}

func analyzeStatus(
	context commandContext,
	current config.Machine,
) (OperationAnalysis, error) {
	current = cloneMachine(current)
	inputs, err := loadAnalysisInputs(context, current)
	if err != nil {
		return OperationAnalysis{}, err
	}
	resolvedModules, sources, observations, selectionBlockers, err := resolveSelection(
		inputs.repository,
		current,
		context.platform,
	)
	if err != nil {
		return OperationAnalysis{}, err
	}
	issues := append(
		append([]planner.Issue(nil), inputs.issues...),
		selectionBlockers...,
	)
	plan, inputWarnings, err := buildAnalysisPlan(
		context,
		current,
		resolvedModules,
		inputs.loaded.Snapshot,
		issues,
	)
	if err != nil {
		return OperationAnalysis{}, err
	}
	return newOperationAnalysis(
		current,
		inputs.loaded.Snapshot,
		sources,
		observations,
		plan,
		appendWarning(inputs.loaded.Warning, inputWarnings),
		statusModuleIDs(inputs.repository, current, inputs.loaded.Snapshot),
	), nil
}

func loadAnalysisInputs(
	context commandContext,
	machine config.Machine,
) (analysisInputs, error) {
	issues := make([]planner.Issue, 0, 1)
	if err := validateOperationControls(context, machine); err != nil {
		if !errors.Is(err, corepaths.ErrControlTopology) {
			return analysisInputs{}, err
		}
		issues = append(issues, planner.Issue{Kind: planner.IssueBlocked, Reason: err.Error()})
	}
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return analysisInputs{}, err
	}
	loaded, err := state.Load(context.statePath, context.home)
	if err != nil {
		return analysisInputs{}, err
	}
	return analysisInputs{
		repository: repository,
		loaded:     loaded,
		issues:     issues,
	}, nil
}

func resolveSelection(
	repository config.Repository,
	machine config.Machine,
	platform config.Platform,
) (
	[]config.Module,
	map[string]selectionSource,
	map[string]moduleObservation,
	[]planner.Issue,
	error,
) {
	resolved, err := coreselection.Resolve(repository, machine, platform)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	sources := make(map[string]selectionSource, len(resolved.Sources))
	for moduleID, source := range resolved.Sources {
		sources[moduleID] = selectionSource{profile: source.Profile, extra: source.Extra}
	}
	observations := make(map[string]moduleObservation, len(resolved.Observations))
	for moduleID, observation := range resolved.Observations {
		observations[moduleID] = moduleObservation{
			loaded:        observation.Loaded,
			applicability: observation.Applicability,
			variant:       observation.Variant,
		}
	}
	issues := make([]planner.Issue, len(resolved.Issues))
	for index, issue := range resolved.Issues {
		issues[index] = planner.Issue{
			Kind:     planner.IssueBlocked,
			ModuleID: issue.ModuleID,
			Reason:   issue.Reason,
		}
	}
	return resolved.Modules, sources, observations, issues, nil
}

func buildAnalysisPlan(
	context commandContext,
	machine config.Machine,
	resolvedModules []config.Module,
	snapshot state.Snapshot,
	issues []planner.Issue,
) (planner.Plan, []string, error) {
	if len(issues) != 0 {
		return planner.Plan{
			Complete: false,
			Issues:   append([]planner.Issue(nil), issues...),
		}, nil, nil
	}

	plan, err := planner.Build(planner.Request{
		Home:     context.home,
		Controls: context.controls(machine.Repository),
		Modules:  resolvedModules,
		State:    snapshot,
	})
	if err != nil {
		return planner.Plan{}, nil, err
	}
	warnings := make([]string, 0)
	for _, issue := range plan.Issues {
		if issue.Code == planner.IssueCodeControlBoundary &&
			!slices.Contains(warnings, issue.Reason) {
			warnings = append(warnings, issue.Reason)
		}
	}
	return plan, warnings, nil
}

func newOperationAnalysis(
	machine config.Machine,
	snapshot state.Snapshot,
	sources map[string]selectionSource,
	observations map[string]moduleObservation,
	plan planner.Plan,
	warnings []string,
	moduleIDs []string,
) OperationAnalysis {
	return OperationAnalysis{
		Machine: cloneMachine(machine),
		Modules: buildModuleAnalyses(
			moduleIDs,
			sources,
			observations,
			snapshot,
			plan,
		),
		Plan: planner.Plan{
			Complete: plan.Complete,
			Steps:    append([]planner.Step(nil), plan.Steps...),
			Issues:   append([]planner.Issue(nil), plan.Issues...),
		},
		Warnings: append([]string(nil), warnings...),
	}
}

func buildModuleAnalyses(
	moduleIDs []string,
	sources map[string]selectionSource,
	observations map[string]moduleObservation,
	snapshot state.Snapshot,
	plan planner.Plan,
) []ModuleAnalysis {
	result := make([]ModuleAnalysis, 0, len(moduleIDs))
	for _, moduleID := range moduleIDs {
		source := sources[moduleID]
		observation := observations[moduleID]
		_, statePresent := snapshot.Modules[moduleID]
		issueReason, directIssue := analysisIssueForModule(
			moduleID,
			source != (selectionSource{}) || statePresent,
			plan.Issues,
		)
		analysis := ModuleAnalysis{
			ID:            moduleID,
			Summary:       "inactive",
			Selection:     source.String(),
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
			if issue.Kind == planner.IssueConflict || !plan.Complete {
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
				action.Decision == planner.DecisionKeep &&
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

func isConcretePlacementIssue(issue planner.Issue) bool {
	return issue.PlacementID != "" && issue.Target != ""
}

func analysisIssueForModule(
	moduleID string,
	includeGlobal bool,
	issues []planner.Issue,
) (string, bool) {
	for _, issue := range issues {
		if issue.ModuleID == moduleID {
			if issue.Kind == planner.IssueConflict && isConcretePlacementIssue(issue) {
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

func analysisModuleIDs(
	sources map[string]selectionSource,
	snapshot state.Snapshot,
	issues []planner.Issue,
) []string {
	set := make(map[string]bool)
	for _, issue := range issues {
		if issue.ModuleID != "" {
			set[issue.ModuleID] = true
		}
	}
	for moduleID := range sources {
		set[moduleID] = true
	}
	for moduleID := range snapshot.Modules {
		set[moduleID] = true
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

func cloneMachine(machine config.Machine) config.Machine {
	machine.Profiles = append([]string(nil), machine.Profiles...)
	machine.ExtraModules = append([]string(nil), machine.ExtraModules...)
	return machine
}
