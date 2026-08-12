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
	report   Report
	loaded   state.Loaded
	controls corepaths.ResolvedControls
}

// Analyze reloads every convergence input and returns one complete read-only
// report. Expressible selection and path blockers are reported as Issues.
func Analyze(environment Environment) (Report, error) {
	prepared, err := analyzeEnvironment(environment)
	if err != nil {
		return Report{}, err
	}
	return prepared.report, nil
}

func analyzeEnvironment(environment Environment) (analysis, error) {
	environment, err := prepareMutationEnvironment(environment)
	if err != nil {
		return analysis{}, err
	}
	prepared, err := analyzePreparedEnvironment(environment)
	if err != nil {
		return analysis{}, newFailure(
			FailureStageAnalysis,
			false,
			recoveryForAnalysisFailure(err),
			nil,
			err,
		)
	}
	return prepared, nil
}

func analyzePreparedEnvironment(environment Environment) (analysis, error) {
	machine, err := requireMachine(environment.ConfigPath)
	if err != nil {
		return analysis{}, err
	}
	controlPaths := environmentControls(environment, machine.Repository)
	issues := make([]Issue, 0, 2)
	controls, err := resolveControls(controlPaths)
	if err != nil {
		if !errors.Is(err, corepaths.ErrControlTopology) {
			return analysis{}, err
		}
		issues = append(issues, Issue{
			Severity: IssueBlocker,
			Code:     IssueCodeControlTopology,
			Reason:   err.Error(),
			Recovery: RecoveryPaths,
		})
	}
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return analysis{}, err
	}
	platform, err := resolvePlatform(environment)
	if err != nil {
		return analysis{}, err
	}
	selection, err := resolveSelection(repository, machine, platform)
	if err != nil {
		return analysis{}, err
	}
	issues = append(issues, selection.issues...)
	loaded, err := loadState(environment.StatePath, environment.Home)
	if err != nil {
		return analysis{}, err
	}
	if loaded.Warning != "" {
		issues = append(issues, Issue{
			Severity: IssueWarning,
			Code:     IssueCodeStateMissing,
			Reason:   loaded.Warning,
			Recovery: RecoveryNone,
		})
	}
	plan, err := buildAnalysisPlan(
		environment.Home,
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
		statusModuleIDs(repository, machine, loaded.Snapshot),
	)
	return analysis{
		report:   report,
		loaded:   loaded,
		controls: controls,
	}, nil
}

func buildAnalysisPlan(
	home string,
	controls corepaths.ResolvedControls,
	resolvedModules []config.Module,
	snapshot state.Snapshot,
	issues []Issue,
) (Plan, error) {
	if slices.ContainsFunc(issues, func(issue Issue) bool {
		return issue.Severity == IssueBlocker
	}) {
		return planFromIssues(issues), nil
	}

	plan, err := buildPlan(planRequest{
		Home:     home,
		Controls: controls,
		Modules:  resolvedModules,
		State:    snapshot,
	})
	if err != nil {
		return Plan{}, err
	}
	plan.Issues = append(plan.Issues, issues...)
	sortIssues(plan.Issues)
	return plan, nil
}

func newReport(
	snapshot state.Snapshot,
	sources map[string]selectionSource,
	observations map[string]moduleObservation,
	plan Plan,
	moduleIDs []string,
) Report {
	return Report{
		Facts: buildModuleFacts(
			moduleIDs,
			sources,
			observations,
			snapshot,
		),
		Plan: plan,
	}
}

func buildModuleFacts(
	moduleIDs []string,
	sources map[string]selectionSource,
	observations map[string]moduleObservation,
	snapshot state.Snapshot,
) []ModuleFact {
	result := make([]ModuleFact, 0, len(moduleIDs))
	for _, moduleID := range moduleIDs {
		source := sources[moduleID]
		observation := observations[moduleID]
		fact := ModuleFact{
			ID:             moduleID,
			Selection:      source.label(),
			ManifestLoaded: observation.loaded,
			StatePresent:   stateHasModule(snapshot, moduleID),
		}
		if observation.loaded {
			fact.Applicability = string(observation.applicability.State)
			fact.Diagnostic = observation.applicability.Diagnostic
			if observation.applicability.State == config.ApplicabilityApplicable {
				fact.Variant = observation.variant
				if fact.Variant == "" {
					fact.Variant = "portable"
				}
			}
		}
		result = append(result, fact)
	}
	return result
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
	for key := range snapshot.Links {
		set[key.ModuleID] = true
	}
	return sortedAnalysisSet(set)
}

func stateHasModule(snapshot state.Snapshot, moduleID string) bool {
	for key := range snapshot.Links {
		if key.ModuleID == moduleID {
			return true
		}
	}
	return false
}

func sortedAnalysisSet(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}
