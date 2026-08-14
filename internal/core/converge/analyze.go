package converge

import (
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
	planned  []planned
	loaded   state.Loaded
	controls corepaths.ResolvedControls
}

// Analyze reloads every convergence input and returns one complete read-only
// report. Expressible selection and path refusals are reported as skip lines.
func Analyze(environment Environment) (Report, error) {
	prepared, err := analyzeEnvironment(environment)
	if err != nil {
		return Report{}, err
	}
	return prepared.report, nil
}

func analyzeEnvironment(environment Environment) (analysis, error) {
	normalized, err := normalizeEnvironment(environment)
	if err != nil {
		return analysis{}, newFailure(false, nil, err)
	}
	prepared, err := analyzePreparedEnvironment(normalized)
	if err != nil {
		return analysis{}, newFailure(false, nil, err)
	}
	return prepared, nil
}

func analyzePreparedEnvironment(environment Environment) (analysis, error) {
	machine, err := requireMachine(environment.ConfigPath)
	if err != nil {
		return analysis{}, err
	}
	controlPaths := environmentControls(environment, machine.Repository)
	controls, err := validateControls(controlPaths)
	if err != nil {
		return analysis{}, err
	}
	paths, err := controls.Paths()
	if err != nil {
		return analysis{}, err
	}
	controlLines, err := planControlModes(paths)
	if err != nil {
		return analysis{}, err
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
	loaded, err := loadState(environment.StatePath, environment.Home)
	if err != nil {
		return analysis{}, err
	}
	planned, err := buildAnalysisLines(
		environment.Home,
		controls,
		selection.modules,
		loaded.Snapshot,
		selection.skips,
		selection.incompleteModules,
	)
	if err != nil {
		return analysis{}, err
	}
	planned = append(controlLines, planned...)
	sortPlanned(planned)
	report := Report{
		Facts: newReportFacts(
			loaded.Snapshot,
			selection.sources,
			selection.observations,
			statusModuleIDs(repository, machine, loaded.Snapshot),
		),
		Lines:        publicLines(planned),
		StateWarning: loaded.Warning,
	}
	return analysis{
		report:   report,
		planned:  planned,
		loaded:   loaded,
		controls: controls,
	}, nil
}

func buildAnalysisLines(
	home string,
	controls corepaths.ResolvedControls,
	resolvedModules []config.Module,
	snapshot state.Snapshot,
	skips []planned,
	incompleteModules map[string]struct{},
) ([]planned, error) {
	loopLines, err := buildLines(planRequest{
		Home:              home,
		Controls:          controls,
		Modules:           resolvedModules,
		State:             snapshot,
		IncompleteModules: incompleteModules,
	})
	if err != nil {
		return nil, err
	}
	lines := append(append([]planned(nil), skips...), loopLines...)
	sortPlanned(lines)
	return lines, nil
}

func newReportFacts(
	snapshot state.Snapshot,
	sources map[string]selectionSource,
	observations map[string]moduleObservation,
	moduleIDs []string,
) []ModuleFact {
	return buildModuleFacts(moduleIDs, sources, observations, snapshot)
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
