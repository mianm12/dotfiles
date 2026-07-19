package planner

import (
	"fmt"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/state"
)

func planScopedFiles(
	validated manifest.ValidatedProfile,
	scoped manifest.ScopedProfile,
	loaded state.Loaded,
	options DecisionOptions,
) (ObservedProfile, []Action, error) {
	if err := validateScopedProfile(validated, scoped); err != nil {
		return ObservedProfile{}, nil, err
	}
	entries, err := mergeScopedEntries(validated.Entries(), scoped)
	if err != nil {
		return ObservedProfile{}, nil, err
	}
	observed, err := ObserveProfileTargets(validated.Home(), entries, loaded)
	if err != nil {
		return ObservedProfile{}, nil, fmt.Errorf("observe complete profile: %w", err)
	}

	selected := stringSet(scoped.Modules())
	actions := make([]Action, 0, len(scoped.Entries()))
	for _, target := range observed.Targets() {
		if _, ok := selected[target.Desired.Module]; !ok {
			continue
		}
		action, err := Decide(target, options)
		if err != nil {
			return ObservedProfile{}, nil, fmt.Errorf(
				"decide module %q source %q: %w",
				target.Desired.Module,
				target.Desired.Source,
				err,
			)
		}
		actions = append(actions, action)
	}
	return observed, actions, nil
}

func validateScopedProfile(validated manifest.ValidatedProfile, scoped manifest.ScopedProfile) error {
	if validated.Name() == "" || scoped.Name() == "" {
		return fmt.Errorf("apply plan profile is invalid")
	}
	if scoped.Name() != validated.Name() ||
		scoped.GOOS() != validated.GOOS() ||
		scoped.Home() != validated.Home() {
		return fmt.Errorf("apply plan scope does not match validated profile")
	}
	effective := stringSet(validated.Modules())
	for _, module := range scoped.Modules() {
		if _, ok := effective[module]; !ok {
			return fmt.Errorf("apply plan scope contains non-effective module %q", module)
		}
	}
	if scoped.Full() && len(scoped.Modules()) != len(effective) {
		return fmt.Errorf("full apply plan scope omits effective modules")
	}
	if !scoped.Full() && len(scoped.Modules()) == 0 {
		return fmt.Errorf("partial apply plan scope is empty")
	}
	return nil
}

func mergeScopedEntries(
	complete []manifest.DesiredEntry,
	scoped manifest.ScopedProfile,
) ([]manifest.DesiredEntry, error) {
	selected := stringSet(scoped.Modules())
	rendered := make(map[string]manifest.DesiredEntry, len(scoped.Entries()))
	for _, entry := range scoped.Entries() {
		if _, ok := selected[entry.Module]; !ok {
			return nil, fmt.Errorf(
				"scoped desired module %q source %q is outside selected modules",
				entry.Module,
				entry.Source,
			)
		}
		key := desiredEntryKey(entry)
		if _, exists := rendered[key]; exists {
			return nil, fmt.Errorf(
				"scoped desired duplicates module %q source %q",
				entry.Module,
				entry.Source,
			)
		}
		rendered[key] = cloneManifestDesired(entry)
	}

	merged := make([]manifest.DesiredEntry, len(complete))
	for index, entry := range complete {
		merged[index] = cloneManifestDesired(entry)
		if _, ok := selected[entry.Module]; !ok {
			continue
		}
		key := desiredEntryKey(entry)
		scopedEntry, exists := rendered[key]
		if !exists {
			return nil, fmt.Errorf(
				"scoped desired is missing module %q source %q",
				entry.Module,
				entry.Source,
			)
		}
		if !sameDesiredStructure(entry, scopedEntry) {
			return nil, fmt.Errorf(
				"scoped desired changed structure for module %q source %q",
				entry.Module,
				entry.Source,
			)
		}
		merged[index] = scopedEntry
		delete(rendered, key)
	}
	if len(rendered) != 0 {
		return nil, fmt.Errorf("scoped desired contains entries outside complete profile")
	}
	return merged, nil
}

func sameDesiredStructure(left, right manifest.DesiredEntry) bool {
	return left.Module == right.Module &&
		left.Source == right.Source &&
		left.SourcePath == right.SourcePath &&
		left.Target == right.Target &&
		left.TargetPath == right.TargetPath &&
		left.Kind == right.Kind &&
		left.Mode == right.Mode
}

func desiredEntryKey(entry manifest.DesiredEntry) string {
	return entry.Module + "\x00" + entry.Source
}

func cloneManifestDesired(entry manifest.DesiredEntry) manifest.DesiredEntry {
	entry.Content = append([]byte(nil), entry.Content...)
	return entry
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
