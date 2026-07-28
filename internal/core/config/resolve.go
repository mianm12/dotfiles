package config

import (
	"fmt"
	"slices"
)

// Resolve decodes and validates only modules selected by scope.
func (repository Repository) Resolve(scope Scope, platform Platform) (Resolution, error) {
	if !repository.valid {
		return Resolution{}, fmt.Errorf("%w: repository is invalid", ErrInvalidConfiguration)
	}
	if err := validateIDs("profile", scope.Profiles); err != nil {
		return Resolution{}, err
	}
	if err := validateIDs("extra module", scope.ExtraModules); err != nil {
		return Resolution{}, err
	}
	if err := validateIDs("required module", scope.RequiredModules); err != nil {
		return Resolution{}, err
	}

	required := make(map[string]bool)
	for _, profile := range scope.Profiles {
		members, exists := repository.profiles[profile]
		if !exists {
			return Resolution{}, fmt.Errorf(
				"%w: unknown profile %q",
				ErrInvalidConfiguration,
				profile,
			)
		}
		for _, module := range members {
			exists, err := repository.inspectModule(module)
			if err != nil {
				return Resolution{}, err
			}
			if !exists {
				return Resolution{}, fmt.Errorf(
					"%w: active profile %q references missing module %q",
					ErrInvalidConfiguration,
					profile,
					module,
				)
			}
			if _, exists := required[module]; !exists {
				required[module] = false
			}
		}
	}
	for _, module := range append(
		append([]string(nil), scope.ExtraModules...),
		scope.RequiredModules...,
	) {
		exists, err := repository.inspectModule(module)
		if err != nil {
			return Resolution{}, err
		}
		if !exists {
			return Resolution{}, fmt.Errorf(
				"%w: required module %q does not exist",
				ErrInvalidConfiguration,
				module,
			)
		}
		required[module] = true
	}

	ids := sortedKeys(required)
	resolution := Resolution{
		Modules:       make([]Module, 0, len(ids)),
		NotApplicable: make([]string, 0),
	}
	for _, id := range ids {
		module, applicability, err := loadModule(id, repository.modules[id], platform)
		if err != nil {
			return Resolution{}, err
		}
		switch applicability.State {
		case ApplicabilityApplicable:
			resolution.Modules = append(resolution.Modules, module)
		case ApplicabilityNotApplicable:
			if required[id] {
				return Resolution{}, fmt.Errorf("%w: module %q", ErrNotApplicable, id)
			}
			resolution.NotApplicable = append(resolution.NotApplicable, id)
		case ApplicabilityIndeterminate:
			return Resolution{}, fmt.Errorf(
				"%w: module %q: %s",
				ErrIndeterminate,
				id,
				applicability.Diagnostic,
			)
		default:
			return Resolution{}, fmt.Errorf(
				"%w: module %q returned invalid applicability %q",
				ErrInvalidConfiguration,
				id,
				applicability.State,
			)
		}
	}

	slices.Sort(resolution.NotApplicable)
	return resolution, nil
}
