package converge

import (
	"fmt"
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
)

type selectionSource struct {
	profile bool
	extra   bool
}

// moduleObservation records the loaded module and its platform applicability.
type moduleObservation struct {
	loaded        bool
	applicability config.ModuleApplicability
	variant       string
}

// selectionResolution is the complete resolution of one machine selection.
type selectionResolution struct {
	modules      []config.Module
	sources      map[string]selectionSource
	observations map[string]moduleObservation
	skips        []planned
}

// resolveSelection loads the complete effective selection. Profile not-applicability
// leaves the module out of desired so stale ownership can be cleaned; extra
// not-applicability and any indeterminate result block convergence.
func resolveSelection(
	repository config.Repository,
	machine config.Machine,
	platform config.Platform,
) (selectionResolution, error) {
	profileModules, err := repository.ProfileModules(machine.Profiles)
	if err != nil {
		return selectionResolution{}, err
	}
	sources := make(map[string]selectionSource)
	for _, moduleID := range profileModules {
		source := sources[moduleID]
		source.profile = true
		sources[moduleID] = source
	}
	for _, moduleID := range machine.ExtraModules {
		source := sources[moduleID]
		source.extra = true
		sources[moduleID] = source
	}
	ids := make([]string, 0, len(sources))
	for moduleID := range sources {
		ids = append(ids, moduleID)
	}
	slices.Sort(ids)
	result := selectionResolution{
		modules:      make([]config.Module, 0, len(ids)),
		sources:      sources,
		observations: make(map[string]moduleObservation, len(ids)),
	}
	for _, moduleID := range ids {
		module, exists, applicability, inspectErr := repository.InspectModule(
			moduleID,
			platform,
		)
		if inspectErr != nil {
			return selectionResolution{}, inspectErr
		}
		if !exists {
			result.skips = append(result.skips, skipLine(placementSubject{
				moduleID: moduleID,
			}, fmt.Sprintf("selected module %q does not exist", moduleID)))
			continue
		}
		observation := moduleObservation{
			loaded:        true,
			applicability: applicability,
		}
		if applicability.State == config.ApplicabilityApplicable {
			observation.variant = module.Variant
		}
		result.observations[moduleID] = observation

		source := sources[moduleID]
		switch applicability.State {
		case config.ApplicabilityApplicable:
			result.modules = append(result.modules, module)
		case config.ApplicabilityNotApplicable:
			if source.extra {
				result.skips = append(result.skips, skipLine(placementSubject{
					moduleID: moduleID,
				}, fmt.Sprintf("module %q is not applicable", moduleID)))
			}
		case config.ApplicabilityIndeterminate:
			result.skips = append(result.skips, skipLine(placementSubject{
				moduleID: moduleID,
			}, fmt.Sprintf(
				"module %q applicability is indeterminate: %s",
				moduleID,
				applicability.Diagnostic,
			)))
		default:
			return selectionResolution{}, fmt.Errorf(
				"module %q returned invalid applicability %q",
				moduleID,
				applicability.State,
			)
		}
	}
	return result, nil
}
