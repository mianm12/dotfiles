// Package selection resolves a machine selection against one repository and platform.
package selection

import (
	"fmt"
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
)

// Source identifies how one module entered the effective selection.
type Source struct {
	Profile bool
	Extra   bool
}

// Observation records the loaded module and its platform applicability.
type Observation struct {
	Loaded        bool
	Applicability config.ModuleApplicability
	Variant       string
}

// Issue is a module-specific reason the effective selection cannot be applied.
type Issue struct {
	ModuleID string
	Reason   string
}

// Result is the complete resolution of one current machine selection.
type Result struct {
	Modules      []config.Module
	Sources      map[string]Source
	Observations map[string]Observation
	Issues       []Issue
}

// Resolve loads the complete effective selection. Profile not-applicability
// leaves the module out of desired so stale ownership can be cleaned; extra
// not-applicability and any indeterminate result block convergence.
func Resolve(
	repository config.Repository,
	machine config.Machine,
	platform config.Platform,
) (Result, error) {
	profileModules, err := repository.ProfileModules(machine.Profiles)
	if err != nil {
		return Result{}, err
	}
	sources := make(map[string]Source)
	for _, moduleID := range profileModules {
		source := sources[moduleID]
		source.Profile = true
		sources[moduleID] = source
	}
	for _, moduleID := range machine.ExtraModules {
		source := sources[moduleID]
		source.Extra = true
		sources[moduleID] = source
	}
	ids := make([]string, 0, len(sources))
	for moduleID := range sources {
		ids = append(ids, moduleID)
	}
	slices.Sort(ids)
	result := Result{
		Modules:      make([]config.Module, 0, len(ids)),
		Sources:      sources,
		Observations: make(map[string]Observation, len(ids)),
	}
	for _, moduleID := range ids {
		module, exists, applicability, inspectErr := repository.InspectModule(
			moduleID,
			platform,
		)
		if inspectErr != nil {
			return Result{}, inspectErr
		}
		if !exists {
			result.Issues = append(result.Issues, Issue{
				ModuleID: moduleID,
				Reason:   fmt.Sprintf("selected module %q does not exist", moduleID),
			})
			continue
		}
		observation := Observation{
			Loaded:        true,
			Applicability: applicability,
		}
		if applicability.State == config.ApplicabilityApplicable {
			observation.Variant = module.Variant
		}
		result.Observations[moduleID] = observation

		source := sources[moduleID]
		switch applicability.State {
		case config.ApplicabilityApplicable:
			result.Modules = append(result.Modules, module)
		case config.ApplicabilityNotApplicable:
			if source.Extra {
				result.Issues = append(result.Issues, Issue{
					ModuleID: moduleID,
					Reason:   fmt.Sprintf("module %q is not applicable", moduleID),
				})
			}
		case config.ApplicabilityIndeterminate:
			result.Issues = append(result.Issues, Issue{
				ModuleID: moduleID,
				Reason: fmt.Sprintf(
					"module %q applicability is indeterminate: %s",
					moduleID,
					applicability.Diagnostic,
				),
			})
		default:
			return Result{}, fmt.Errorf(
				"module %q returned invalid applicability %q",
				moduleID,
				applicability.State,
			)
		}
	}
	return result, nil
}
