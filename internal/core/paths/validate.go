package paths

import (
	"errors"
	"fmt"
	"path/filepath"
)

var (
	// ErrTargetConflict reports duplicate targets or a directory-link target
	// containing another placement.
	ErrTargetConflict = errors.New("target paths conflict")
	// ErrControlBoundary reports a target equal to or below a protected path.
	ErrControlBoundary = errors.New("target overlaps a control path")
)

// Controls contains the protected paths named by design baseline section 7.3.
type Controls struct {
	Repository string
	Config     string
	State      string
	Lock       string
}

// Placement is the path information needed before manifest/planner construction.
// DirectoryLink is true only when a link placement's source is a directory.
type Placement struct {
	Label         string
	Target        string
	DirectoryLink bool
}

// ResolvedPlacement is a validated placement with both path representations.
type ResolvedPlacement struct {
	Label         string
	Target        Target
	DirectoryLink bool
}

type resolvedControl struct {
	label    string
	lexical  string
	entry    string
	resolved string
}

// Validate resolves and validates a complete placement set. It is read-only and
// returns no partial result when any path is invalid or conflicting.
func Validate(home string, controls Controls, placements []Placement) ([]ResolvedPlacement, error) {
	resolvedControls, err := resolveControls(controls)
	if err != nil {
		return nil, err
	}

	resolved := make([]ResolvedPlacement, len(placements))
	for index, placement := range placements {
		if placement.Label == "" {
			return nil, fmt.Errorf("%w: placement %d has an empty label", ErrInvalidPath, index)
		}
		target, resolveErr := ResolveTarget(home, placement.Target)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve placement %q: %w", placement.Label, resolveErr)
		}
		resolved[index] = ResolvedPlacement{
			Label:         placement.Label,
			Target:        target,
			DirectoryLink: placement.DirectoryLink,
		}
	}

	if err := validateTargetSet(resolved); err != nil {
		return nil, err
	}
	if err := validateControlBoundaries(resolvedControls, resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}

func resolveControls(controls Controls) ([]resolvedControl, error) {
	inputs := []struct {
		label string
		path  string
	}{
		{label: "repository", path: controls.Repository},
		{label: "machine config", path: controls.Config},
		{label: "state", path: controls.State},
		{label: "lock", path: controls.Lock},
	}
	resolved := make([]resolvedControl, len(inputs))
	for index, input := range inputs {
		lexical, err := cleanAbsolute(input.label, input.path)
		if err != nil {
			return nil, err
		}
		entry, err := resolveEntry(lexical)
		if err != nil {
			return nil, fmt.Errorf("resolve %s path entry %q: %w", input.label, input.path, err)
		}
		actual, err := resolvePath(lexical)
		if err != nil {
			return nil, fmt.Errorf("resolve %s path %q: %w", input.label, input.path, err)
		}
		resolved[index] = resolvedControl{
			label:    input.label,
			lexical:  lexical,
			entry:    entry,
			resolved: actual,
		}
	}
	return resolved, nil
}

func validateTargetSet(placements []ResolvedPlacement) error {
	for leftIndex := range placements {
		left := placements[leftIndex]
		for rightIndex := leftIndex + 1; rightIndex < len(placements); rightIndex++ {
			right := placements[rightIndex]
			if sameTarget(left.Target, right.Target) {
				return fmt.Errorf(
					"%w: placements %q and %q resolve to the same target",
					ErrTargetConflict,
					left.Label,
					right.Label,
				)
			}
			if directoryContains(left, right) {
				return fmt.Errorf(
					"%w: directory link placement %q contains placement %q",
					ErrTargetConflict,
					left.Label,
					right.Label,
				)
			}
			if directoryContains(right, left) {
				return fmt.Errorf(
					"%w: directory link placement %q contains placement %q",
					ErrTargetConflict,
					right.Label,
					left.Label,
				)
			}
		}
	}
	return nil
}

func sameTarget(left, right Target) bool {
	return left.lexical == right.lexical || left.resolved == right.resolved
}

func directoryContains(parent, child ResolvedPlacement) bool {
	return parent.DirectoryLink &&
		(strictDescendant(parent.Target.lexical, child.Target.lexical) ||
			strictDescendant(parent.Target.resolved, child.Target.resolved))
}

func validateControlBoundaries(controls []resolvedControl, placements []ResolvedPlacement) error {
	for _, placement := range placements {
		for _, control := range controls {
			if overlapsControl(control, placement.Target) {
				return fmt.Errorf(
					"%w: placement %q target %q is inside %s path %q",
					ErrControlBoundary,
					placement.Label,
					placement.Target.lexical,
					control.label,
					filepath.Clean(control.lexical),
				)
			}
		}
	}
	return nil
}

func overlapsControl(control resolvedControl, target Target) bool {
	controls := [...]string{control.lexical, control.entry, control.resolved}
	targets := [...]string{target.lexical, target.resolved}
	for _, controlPath := range controls {
		for _, targetPath := range targets {
			if sameOrDescendant(controlPath, targetPath) {
				return true
			}
		}
	}
	return false
}
