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
	// ErrControlBoundary reports a target overlapping a protected path.
	ErrControlBoundary = errors.New("target overlaps a control path")
	// ErrControlTopology reports control families that are not isolated.
	ErrControlTopology = errors.New("control paths conflict")
)

// Controls contains the protected paths named by the placement specification.
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
	return validate(home, controls, placements, nil)
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

// ValidateScoped applies target-set and control-boundary checks only when at
// least one participating placement label is selected. All placements are
// still resolved so selected targets can be compared with the complete
// effective desired set.
func ValidateScoped(
	home string,
	controls Controls,
	placements []Placement,
	selectedLabels []string,
) ([]ResolvedPlacement, error) {
	selected := make(map[string]bool, len(selectedLabels))
	for _, label := range selectedLabels {
		selected[label] = true
	}
	return validate(home, controls, placements, selected)
}

func validate(
	home string,
	controls Controls,
	placements []Placement,
	selected map[string]bool,
) ([]ResolvedPlacement, error) {
	topology, err := resolveControlTopology(controls)
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

	if err := validateTargetSet(resolved, selected); err != nil {
		return nil, err
	}
	if err := validateControlBoundaries(topology, resolved, selected); err != nil {
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
			"%w: machine config %q must be a direct child of config root %q; %s",
			ErrControlTopology,
			cleaned["machine config"],
			configRootPath,
			controlPathHint,
		)
	}
	if stateRootPath != filepath.Dir(cleaned["lock"]) ||
		!directChild(stateRootPath, cleaned["state"]) ||
		!directChild(stateRootPath, cleaned["lock"]) ||
		cleaned["state"] == cleaned["lock"] {
		return controlTopology{}, fmt.Errorf(
			"%w: state %q and lock %q must be distinct siblings under one state root; %s",
			ErrControlTopology,
			cleaned["state"],
			cleaned["lock"],
			controlPathHint,
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
			"%w: state %q and lock %q do not identify distinct siblings; %s",
			ErrControlTopology,
			topology.state.lexical,
			topology.lock.lexical,
			controlPathHint,
		)
	}
	if err := validateControlFamilies(topology.families); err != nil {
		return controlTopology{}, err
	}
	return topology, nil
}

const controlPathHint = "run `dot paths` to inspect the active control paths"

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
						"%w: %s %q (resolved %q) overlaps %s %q (resolved %q); %s",
						ErrControlTopology,
						left.label,
						left.lexical,
						left.resolved,
						right.label,
						right.lexical,
						right.resolved,
						controlPathHint,
					)
				}
			}
		}
	}
	return nil
}

func validateTargetSet(
	placements []ResolvedPlacement,
	selected map[string]bool,
) error {
	for leftIndex := range placements {
		left := placements[leftIndex]
		for rightIndex := leftIndex + 1; rightIndex < len(placements); rightIndex++ {
			right := placements[rightIndex]
			if !participates(selected, left.Label, right.Label) {
				continue
			}
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

func validateControlBoundaries(
	topology controlTopology,
	placements []ResolvedPlacement,
	selected map[string]bool,
) error {
	for _, placement := range placements {
		if selected != nil && !selected[placement.Label] {
			continue
		}
		for _, family := range topology.families {
			for _, control := range family.paths {
				if identityOverlapsTarget(control, placement.Target) {
					return fmt.Errorf(
						"%w: placement %q target %q overlaps %s %q; %s",
						ErrControlBoundary,
						placement.Label,
						placement.Target.lexical,
						control.label,
						filepath.Clean(control.lexical),
						controlPathHint,
					)
				}
			}
		}
	}
	return nil
}

func participates(selected map[string]bool, labels ...string) bool {
	if selected == nil {
		return true
	}
	for _, label := range labels {
		if selected[label] {
			return true
		}
	}
	return false
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
