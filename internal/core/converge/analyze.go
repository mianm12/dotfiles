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
	loaded      state.Loaded
	controls    corepaths.ResolvedControls
	fingerprint []byte
}

// Analyze reloads every convergence input and returns one complete read-only
// report. Expressible selection and path blockers are reported as Problems.
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
	if err := validateEnvironmentControls(environment); err != nil {
		return analysis{}, err
	}
	machine, err := requireMachine(environment.ConfigPath)
	if err != nil {
		return analysis{}, err
	}
	controlPaths := environmentControls(environment, machine.Repository)
	problems := make([]Problem, 0, 1)
	controls, err := resolveControls(controlPaths)
	if err != nil {
		if !errors.Is(err, corepaths.ErrControlTopology) {
			return analysis{}, err
		}
		problems = append(problems, Problem{
			Kind:   ProblemBlocked,
			Code:   ProblemCodeControlTopology,
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
		problems = append(problems, Problem{
			Kind:     ProblemBlocked,
			ModuleID: issue.moduleID,
			Reason:   issue.reason,
		})
	}
	loaded, err := loadState(environment.StatePath, environment.Home)
	if err != nil {
		return analysis{}, err
	}
	plan, inputWarnings, err := buildAnalysisPlan(
		environment.Home,
		controls,
		selection.modules,
		loaded.Snapshot,
		problems,
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
		loaded:      loaded,
		controls:    controls,
		fingerprint: fingerprint,
	}, nil
}

func buildAnalysisPlan(
	home string,
	controls corepaths.ResolvedControls,
	resolvedModules []config.Module,
	snapshot state.Snapshot,
	problems []Problem,
) (Plan, []string, error) {
	if len(problems) != 0 {
		return Plan{
			Problems:   append([]Problem(nil), problems...),
			finalState: cloneSnapshot(snapshot),
		}, nil, nil
	}

	plan, err := buildPlan(planRequest{
		Home:     home,
		Controls: controls,
		Modules:  resolvedModules,
		State:    snapshot,
	})
	if err != nil {
		return Plan{}, nil, err
	}
	warnings := make([]string, 0)
	for _, problem := range plan.Problems {
		if problem.Code == ProblemCodeControlBoundary &&
			!slices.Contains(warnings, problem.Reason) {
			warnings = append(warnings, problem.Reason)
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
		Facts: buildModuleFacts(
			moduleIDs,
			sources,
			observations,
			snapshot,
		),
		Plan:     clonePlan(plan),
		Warnings: append([]string(nil), warnings...),
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
	for key := range snapshot.Records {
		set[key.ModuleID] = true
	}
	return sortedAnalysisSet(set)
}

func stateHasModule(snapshot state.Snapshot, moduleID string) bool {
	for key := range snapshot.Records {
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

func appendWarning(warning string, warnings []string) []string {
	result := make([]string, 0, len(warnings)+1)
	if warning != "" {
		result = append(result, warning)
	}
	return append(result, warnings...)
}

func cloneReport(report Report) Report {
	return Report{
		Facts:    append([]ModuleFact(nil), report.Facts...),
		Plan:     clonePlan(report.Plan),
		Warnings: append([]string(nil), report.Warnings...),
	}
}

func clonePlan(plan Plan) Plan {
	cloned := Plan{
		Transitions: make([]Transition, len(plan.Transitions)),
		Problems:    append([]Problem(nil), plan.Problems...),
		finalState:  cloneSnapshot(plan.finalState),
	}
	for index, transition := range plan.Transitions {
		cloned.Transitions[index] = transition
		cloned.Transitions[index].Actions = append(
			[]Action(nil),
			transition.Actions...,
		)
	}
	return cloned
}
