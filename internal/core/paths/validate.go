package paths

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

var (
	// ErrTargetConflict reports placement targets that are equal or nested.
	ErrTargetConflict = errors.New("target paths conflict")
	// ErrControlBoundary reports a target overlapping a protected path.
	ErrControlBoundary = errors.New("target overlaps a control path")
	// ErrControlTopology reports control families that are not isolated.
	ErrControlTopology = errors.New("control paths conflict")
)

type placementError struct {
	err    error
	labels []string
}

func (err *placementError) Error() string { return err.err.Error() }

func (err *placementError) Unwrap() error { return err.err }

// PlacementLabels returns the placement labels attached to a validation error.
func PlacementLabels(err error) []string {
	var placed *placementError
	if !errors.As(err, &placed) {
		return nil
	}
	return slices.Clone(placed.labels)
}

func withPlacementLabels(err error, labels ...string) error {
	return &placementError{err: err, labels: slices.Clone(labels)}
}

// Controls contains the protected paths named by the placement specification.
type Controls struct {
	Repository string
	Config     string
	State      string
	Lock       string
}

// Placement is the path information needed before manifest/planner construction.
type Placement struct {
	Label  string
	Target string
}

// ResolvedPlacement is a validated placement with both path representations.
type ResolvedPlacement struct {
	Label  string
	Target Target
}

type pathIdentity struct {
	label    string
	lexical  string
	entry    string
	resolved string
}

type controlFamily struct {
	paths []pathIdentity
}

type controlTopology struct {
	families []controlFamily
	state    pathIdentity
	lock     pathIdentity
}

// Validate resolves and validates a complete placement set. It is read-only and
// returns no partial result when any path is invalid or conflicting.
func Validate(home string, controls Controls, placements []Placement) ([]ResolvedPlacement, error) {
	return validate(home, controls, placements)
}

// ValidateControlTopology validates the control families without resolving any
// placement targets. It is read-only and safe to call before lock acquisition.
func ValidateControlTopology(controls Controls) error {
	_, err := resolveControlTopology(controls)
	return err
}

// TargetOverlapsControls reports whether target intersects a protected control
// family. Invalid control topology is returned as an error.
func TargetOverlapsControls(controls Controls, target Target) (bool, error) {
	topology, err := resolveControlTopology(controls)
	if err != nil {
		return false, err
	}
	for _, family := range topology.families {
		for _, control := range family.paths {
			if identityOverlapsTarget(control, target) {
				return true, nil
			}
		}
	}
	return false, nil
}

func validate(
	home string,
	controls Controls,
	placements []Placement,
) ([]ResolvedPlacement, error) {
	topology, err := resolveControlTopology(controls)
	if err != nil {
		return nil, err
	}
	cleanHome, err := cleanAbsolute("HOME", home)
	if err != nil {
		return nil, err
	}

	// Diagnose relationships that depend only on declared paths before an
	// obstructed target ancestor can hide the complete target-set decision.
	lexical := make([]ResolvedPlacement, len(placements))
	for index, placement := range placements {
		if placement.Label == "" {
			return nil, fmt.Errorf("%w: placement %d has an empty label", ErrInvalidPath, index)
		}
		path, expandErr := expandTarget(cleanHome, placement.Target)
		if expandErr != nil {
			return nil, fmt.Errorf("resolve placement %q: %w", placement.Label, expandErr)
		}
		lexical[index] = ResolvedPlacement{
			Label: placement.Label,
			Target: Target{
				lexical:  path,
				resolved: path,
			},
		}
	}
	if err := validateTargetSet(lexical); err != nil {
		return nil, err
	}
	if err := validateControlBoundaries(topology, lexical); err != nil {
		return nil, err
	}

	resolved := make([]ResolvedPlacement, len(placements))
	for index, placement := range placements {
		target, resolveErr := ResolveTarget(cleanHome, placement.Target)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve placement %q: %w", placement.Label, resolveErr)
		}
		resolved[index] = ResolvedPlacement{
			Label:  placement.Label,
			Target: target,
		}
	}

	if err := validateTargetSet(resolved); err != nil {
		return nil, err
	}
	if err := validateControlBoundaries(topology, resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}

func resolveControlTopology(controls Controls) (controlTopology, error) {
	inputs := []struct {
		label string
		path  string
	}{
		{label: "repository", path: controls.Repository},
		{label: "machine config", path: controls.Config},
		{label: "state", path: controls.State},
		{label: "lock", path: controls.Lock},
	}
	cleaned := make(map[string]string, len(inputs))
	for _, input := range inputs {
		lexical, err := cleanAbsolute(input.label, input.path)
		if err != nil {
			return controlTopology{}, err
		}
		cleaned[input.label] = lexical
	}

	configRootPath := filepath.Dir(cleaned["machine config"])
	stateRootPath := filepath.Dir(cleaned["state"])
	if !directChild(configRootPath, cleaned["machine config"]) {
		return controlTopology{}, fmt.Errorf(
			"%w: machine config %q must be a direct child of config root %q",
			ErrControlTopology,
			cleaned["machine config"],
			configRootPath,
		)
	}
	if stateRootPath != filepath.Dir(cleaned["lock"]) ||
		!directChild(stateRootPath, cleaned["state"]) ||
		!directChild(stateRootPath, cleaned["lock"]) ||
		cleaned["state"] == cleaned["lock"] {
		return controlTopology{}, fmt.Errorf(
			"%w: state %q and lock %q must be distinct siblings under one state root",
			ErrControlTopology,
			cleaned["state"],
			cleaned["lock"],
		)
	}
	if err := validateControlFamilies(controlFamilies(
		lexicalIdentity("repository", cleaned["repository"]),
		lexicalIdentity("machine config root", configRootPath),
		lexicalIdentity("machine config", cleaned["machine config"]),
		lexicalIdentity("state root", stateRootPath),
		lexicalIdentity("state", cleaned["state"]),
		lexicalIdentity("lock", cleaned["lock"]),
	)); err != nil {
		return controlTopology{}, err
	}

	resolved := make(map[string]pathIdentity, len(inputs)+2)
	for _, input := range inputs {
		lexical := cleaned[input.label]
		entry, err := resolveEntry(lexical)
		if err != nil {
			return controlTopology{}, fmt.Errorf(
				"resolve %s path entry %q: %w",
				input.label,
				input.path,
				err,
			)
		}
		actual, err := resolvePath(lexical)
		if err != nil {
			return controlTopology{}, fmt.Errorf(
				"resolve %s path %q: %w",
				input.label,
				input.path,
				err,
			)
		}
		resolved[input.label] = pathIdentity{
			label:    input.label,
			lexical:  lexical,
			entry:    entry,
			resolved: actual,
		}
	}

	configRoot, err := resolveIdentity("machine config root", configRootPath)
	if err != nil {
		return controlTopology{}, err
	}
	stateRoot, err := resolveIdentity("state root", stateRootPath)
	if err != nil {
		return controlTopology{}, err
	}
	topology := controlTopology{
		families: controlFamilies(
			resolved["repository"],
			configRoot,
			resolved["machine config"],
			stateRoot,
			resolved["state"],
			resolved["lock"],
		),
		state: resolved["state"],
		lock:  resolved["lock"],
	}
	if identitiesOverlap(topology.state, topology.lock) {
		return controlTopology{}, fmt.Errorf(
			"%w: state %q and lock %q do not identify distinct siblings",
			ErrControlTopology,
			topology.state.lexical,
			topology.lock.lexical,
		)
	}
	if err := validateControlFamilies(topology.families); err != nil {
		return controlTopology{}, err
	}
	return topology, nil
}

func resolveIdentity(label, path string) (pathIdentity, error) {
	lexical, err := cleanAbsolute(label, path)
	if err != nil {
		return pathIdentity{}, err
	}
	entry, err := resolveEntry(lexical)
	if err != nil {
		return pathIdentity{}, fmt.Errorf("resolve %s entry %q: %w", label, path, err)
	}
	resolved, err := resolvePath(lexical)
	if err != nil {
		return pathIdentity{}, fmt.Errorf("resolve %s %q: %w", label, path, err)
	}
	return pathIdentity{
		label:    label,
		lexical:  lexical,
		entry:    entry,
		resolved: resolved,
	}, nil
}

func lexicalIdentity(label, path string) pathIdentity {
	return pathIdentity{
		label:    label,
		lexical:  path,
		entry:    path,
		resolved: path,
	}
}

func controlFamilies(
	repository, configRoot, config, stateRoot, state, lock pathIdentity,
) []controlFamily {
	return []controlFamily{
		{paths: []pathIdentity{repository}},
		{paths: []pathIdentity{configRoot, config}},
		{paths: []pathIdentity{stateRoot, state, lock}},
	}
}

func validateControlFamilies(families []controlFamily) error {
	for leftIndex, leftFamily := range families {
		for _, rightFamily := range families[leftIndex+1:] {
			for _, left := range leftFamily.paths {
				for _, right := range rightFamily.paths {
					if !identitiesOverlap(left, right) {
						continue
					}
					return fmt.Errorf(
						"%w: %s %q (resolved %q) overlaps %s %q (resolved %q)",
						ErrControlTopology,
						left.label,
						left.lexical,
						left.resolved,
						right.label,
						right.lexical,
						right.resolved,
					)
				}
			}
		}
	}
	return nil
}

func validateTargetSet(placements []ResolvedPlacement) error {
	for leftIndex := range placements {
		left := placements[leftIndex]
		for rightIndex := leftIndex + 1; rightIndex < len(placements); rightIndex++ {
			right := placements[rightIndex]
			if TargetsEqual(left.Target, right.Target) {
				return withPlacementLabels(fmt.Errorf(
					"%w: placements %q target %q and %q target %q resolve to the same target",
					ErrTargetConflict,
					left.Label,
					left.Target.Lexical(),
					right.Label,
					right.Target.Lexical(),
				), left.Label, right.Label)
			}
			if TargetStrictlyContains(left.Target, right.Target) {
				return withPlacementLabels(fmt.Errorf(
					"%w: placement %q target %q contains placement %q target %q",
					ErrTargetConflict,
					left.Label,
					left.Target.Lexical(),
					right.Label,
					right.Target.Lexical(),
				), left.Label, right.Label)
			}
			if TargetStrictlyContains(right.Target, left.Target) {
				return withPlacementLabels(fmt.Errorf(
					"%w: placement %q target %q contains placement %q target %q",
					ErrTargetConflict,
					right.Label,
					right.Target.Lexical(),
					left.Label,
					left.Target.Lexical(),
				), right.Label, left.Label)
			}
		}
	}
	return nil
}

// TargetsEqual reports equality in either the lexical or resolved target
// representation.
func TargetsEqual(left, right Target) bool {
	return left.lexical == right.lexical || left.resolved == right.resolved
}

// TargetStrictlyContains reports strict ancestry in either the lexical or
// resolved target representation.
func TargetStrictlyContains(parent, child Target) bool {
	return strictDescendant(parent.lexical, child.lexical) ||
		strictDescendant(parent.resolved, child.resolved)
}

func validateControlBoundaries(
	topology controlTopology,
	placements []ResolvedPlacement,
) error {
	for _, placement := range placements {
		for _, family := range topology.families {
			for _, control := range family.paths {
				if identityOverlapsTarget(control, placement.Target) {
					return withPlacementLabels(fmt.Errorf(
						"%w: placement %q target %q overlaps %s %q",
						ErrControlBoundary,
						placement.Label,
						placement.Target.lexical,
						control.label,
						filepath.Clean(control.lexical),
					), placement.Label)
				}
			}
		}
	}
	return nil
}

func identityOverlapsTarget(control pathIdentity, target Target) bool {
	controls := identityPaths(control)
	targets := [...]string{target.lexical, target.resolved}
	for _, controlPath := range controls {
		for _, targetPath := range targets {
			if sameOrDescendant(controlPath, targetPath) ||
				sameOrDescendant(targetPath, controlPath) {
				return true
			}
		}
	}
	return false
}

func identitiesOverlap(left, right pathIdentity) bool {
	for _, leftPath := range identityPaths(left) {
		for _, rightPath := range identityPaths(right) {
			if sameOrDescendant(leftPath, rightPath) ||
				sameOrDescendant(rightPath, leftPath) {
				return true
			}
		}
	}
	return false
}

func identityPaths(identity pathIdentity) [3]string {
	return [3]string{identity.lexical, identity.entry, identity.resolved}
}

func directChild(parent, child string) bool {
	return parent != child && filepath.Dir(child) == parent
}
