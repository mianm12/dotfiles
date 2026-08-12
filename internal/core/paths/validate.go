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

// ResolvedControls is one immutable control-path topology snapshot. Its path
// identities are resolved exactly once and reused for every target check in
// the same assessment.
type ResolvedControls struct {
	paths    Controls
	topology controlTopology
	valid    bool
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

type coordinationPaths struct {
	configRoot string
	config     string
	stateRoot  string
	state      string
	lock       string
}

type coordinationTopology struct {
	configRoot pathIdentity
	config     pathIdentity
	stateRoot  pathIdentity
	state      pathIdentity
	lock       pathIdentity
}

type controlTopology struct {
	repository   pathIdentity
	coordination coordinationTopology
}

// ResolveControls resolves and validates one control topology without resolving
// placement targets. The returned snapshot owns the resolved identities used by
// later validation and overlap checks.
func ResolveControls(controls Controls) (ResolvedControls, error) {
	topology, err := resolveControlTopology(controls)
	if err != nil {
		return ResolvedControls{}, err
	}
	return ResolvedControls{
		paths: Controls{
			Repository: filepath.Clean(controls.Repository),
			Config:     filepath.Clean(controls.Config),
			State:      filepath.Clean(controls.State),
			Lock:       filepath.Clean(controls.Lock),
		},
		topology: topology,
		valid:    true,
	}, nil
}

// ValidateLockBoundary validates only the config/state coordination topology
// needed before a mutation may create and acquire its advisory lock. It does
// not read machine configuration or require a repository path.
func ValidateLockBoundary(config, state, lock string) error {
	paths, err := cleanCoordinationPaths(config, state, lock)
	if err != nil {
		return err
	}
	if err := validateCoordinationTopology(paths.lexicalTopology()); err != nil {
		return err
	}
	topology, err := resolveCoordinationTopology(paths)
	if err != nil {
		return err
	}
	return validateCoordinationTopology(topology)
}

// Paths returns the cleaned lexical paths captured by this snapshot.
func (controls ResolvedControls) Paths() (Controls, error) {
	if err := controls.validate(); err != nil {
		return Controls{}, err
	}
	return controls.paths, nil
}

// Validate resolves and validates a complete placement set against this
// snapshot. It does not re-resolve the control topology.
func (controls ResolvedControls) Validate(
	home string,
	placements []Placement,
) ([]ResolvedPlacement, error) {
	if err := controls.validate(); err != nil {
		return nil, err
	}
	return validatePlacements(home, controls.topology, placements)
}

// TargetOverlaps reports whether target intersects a protected control family
// captured by this snapshot. It does not consult the filesystem.
func (controls ResolvedControls) TargetOverlaps(target Target) (bool, error) {
	if err := controls.validate(); err != nil {
		return false, err
	}
	for _, family := range controls.topology.families() {
		for _, control := range family.paths {
			if identityOverlapsTarget(control, target) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (controls ResolvedControls) validate() error {
	if !controls.valid {
		return fmt.Errorf("%w: control snapshot is unresolved", ErrControlTopology)
	}
	return nil
}

func validatePlacements(
	home string,
	topology controlTopology,
	placements []Placement,
) ([]ResolvedPlacement, error) {
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
	repository, err := cleanAbsolute("repository", controls.Repository)
	if err != nil {
		return controlTopology{}, err
	}
	coordination, err := cleanCoordinationPaths(controls.Config, controls.State, controls.Lock)
	if err != nil {
		return controlTopology{}, err
	}
	lexical := controlTopology{
		repository:   lexicalIdentity("repository", repository),
		coordination: coordination.lexicalTopology(),
	}
	if err := validateControlTopology(lexical); err != nil {
		return controlTopology{}, err
	}

	resolvedRepository, err := resolveIdentity("repository", repository)
	if err != nil {
		return controlTopology{}, err
	}
	resolvedCoordination, err := resolveCoordinationTopology(coordination)
	if err != nil {
		return controlTopology{}, err
	}
	topology := controlTopology{
		repository:   resolvedRepository,
		coordination: resolvedCoordination,
	}
	if err := validateControlTopology(topology); err != nil {
		return controlTopology{}, err
	}
	return topology, nil
}

func cleanCoordinationPaths(config, state, lock string) (coordinationPaths, error) {
	cleanConfig, err := cleanAbsolute("machine config", config)
	if err != nil {
		return coordinationPaths{}, err
	}
	cleanState, err := cleanAbsolute("state", state)
	if err != nil {
		return coordinationPaths{}, err
	}
	cleanLock, err := cleanAbsolute("lock", lock)
	if err != nil {
		return coordinationPaths{}, err
	}

	paths := coordinationPaths{
		configRoot: filepath.Dir(cleanConfig),
		config:     cleanConfig,
		stateRoot:  filepath.Dir(cleanState),
		state:      cleanState,
		lock:       cleanLock,
	}
	if !directChild(paths.configRoot, paths.config) {
		return coordinationPaths{}, fmt.Errorf(
			"%w: machine config %q must be a direct child of config root %q",
			ErrControlTopology,
			paths.config,
			paths.configRoot,
		)
	}
	if paths.stateRoot != filepath.Dir(paths.lock) ||
		!directChild(paths.stateRoot, paths.state) ||
		!directChild(paths.stateRoot, paths.lock) ||
		paths.state == paths.lock {
		return coordinationPaths{}, fmt.Errorf(
			"%w: state %q and lock %q must be distinct siblings under one state root",
			ErrControlTopology,
			paths.state,
			paths.lock,
		)
	}
	return paths, nil
}

func (paths coordinationPaths) lexicalTopology() coordinationTopology {
	return coordinationTopology{
		configRoot: lexicalIdentity("machine config root", paths.configRoot),
		config:     lexicalIdentity("machine config", paths.config),
		stateRoot:  lexicalIdentity("state root", paths.stateRoot),
		state:      lexicalIdentity("state", paths.state),
		lock:       lexicalIdentity("lock", paths.lock),
	}
}

func resolveCoordinationTopology(paths coordinationPaths) (coordinationTopology, error) {
	config, err := resolveIdentity("machine config", paths.config)
	if err != nil {
		return coordinationTopology{}, err
	}
	state, err := resolveIdentity("state", paths.state)
	if err != nil {
		return coordinationTopology{}, err
	}
	lock, err := resolveIdentity("lock", paths.lock)
	if err != nil {
		return coordinationTopology{}, err
	}
	configRoot, err := resolveIdentity("machine config root", paths.configRoot)
	if err != nil {
		return coordinationTopology{}, err
	}
	stateRoot, err := resolveIdentity("state root", paths.stateRoot)
	if err != nil {
		return coordinationTopology{}, err
	}
	return coordinationTopology{
		configRoot: configRoot,
		config:     config,
		stateRoot:  stateRoot,
		state:      state,
		lock:       lock,
	}, nil
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

func (topology coordinationTopology) families() []controlFamily {
	return []controlFamily{
		{paths: []pathIdentity{topology.configRoot, topology.config}},
		{paths: []pathIdentity{topology.stateRoot, topology.state, topology.lock}},
	}
}

func (topology controlTopology) families() []controlFamily {
	return []controlFamily{
		{paths: []pathIdentity{topology.repository}},
		{paths: []pathIdentity{
			topology.coordination.configRoot,
			topology.coordination.config,
		}},
		{paths: []pathIdentity{
			topology.coordination.stateRoot,
			topology.coordination.state,
			topology.coordination.lock,
		}},
	}
}

func validateCoordinationTopology(topology coordinationTopology) error {
	if err := validateStateLockIdentity(topology.state, topology.lock); err != nil {
		return err
	}
	return validateControlFamilies(topology.families())
}

func validateControlTopology(topology controlTopology) error {
	if err := validateStateLockIdentity(
		topology.coordination.state,
		topology.coordination.lock,
	); err != nil {
		return err
	}
	return validateControlFamilies(topology.families())
}

func validateStateLockIdentity(state, lock pathIdentity) error {
	if !identitiesOverlap(state, lock) {
		return nil
	}
	return fmt.Errorf(
		"%w: state %q and lock %q do not identify distinct siblings",
		ErrControlTopology,
		state.lexical,
		lock.lexical,
	)
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
		for _, family := range topology.families() {
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
