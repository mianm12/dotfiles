package converge

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

type desiredPlacement struct {
	key         state.Key
	kind        placementKind
	target      corepaths.Target
	source      string
	destination string
}

type placementKind uint8

const (
	placementLink placementKind = iota
	placementLocal
)

type activeObservation struct {
	localExists bool
	actual      actual
}

type stateTargetResolution uint8

const (
	stateTargetResolved stateTargetResolution = iota
	stateTargetObstructed
)

type stateLinkObservation struct {
	resolution     stateTargetResolution
	target         corepaths.Target
	actual         actual
	controlOverlap bool
}

type targetRole uint8

const (
	targetDesired targetRole = iota
	targetState
)

type targetRef struct {
	role targetRole
	key  state.Key
}

type relationKey struct {
	from targetRef
	to   targetRef
}

type relationFacts struct {
	equal     bool
	contains  bool
	traverses bool
}

type reconcileInput struct {
	desired    []desiredPlacement
	state      state.Snapshot
	active     map[state.Key]activeObservation
	stateLinks map[state.Key]stateLinkObservation
	relations  map[relationKey]relationFacts
}

type observedTarget struct {
	ref    targetRef
	target corepaths.Target
}

// buildPlan resolves and observes the filesystem once, then hands immutable
// facts to the pure reconcile decision pass.
func buildPlan(request planRequest) (Plan, error) {
	home, err := validatePlanRequest(request)
	if err != nil {
		return Plan{}, err
	}

	desired, err := resolveDesired(home, request.Controls, request.Modules)
	if err != nil {
		if isPlanningIssue(err) {
			return planFromIssues(pathIssues(err)), nil
		}
		return Plan{}, err
	}

	input, err := observeReconcileInput(request.Controls, desired, request.State)
	if err != nil {
		return Plan{}, err
	}
	return reconcile(input), nil
}

func observeReconcileInput(
	controls corepaths.ResolvedControls,
	desired []desiredPlacement,
	snapshot state.Snapshot,
) (reconcileInput, error) {
	input := reconcileInput{
		desired:    append([]desiredPlacement(nil), desired...),
		state:      snapshot,
		active:     make(map[state.Key]activeObservation, len(desired)),
		stateLinks: make(map[state.Key]stateLinkObservation, len(snapshot.Links)),
		relations:  make(map[relationKey]relationFacts),
	}

	for _, placement := range desired {
		if placement.kind == placementLocal {
			exists, err := observeLocal(placement.target.Lexical())
			if err != nil {
				return reconcileInput{}, err
			}
			input.active[placement.key] = activeObservation{localExists: exists}
			continue
		}
		observed, err := observeLink(placement.target.Lexical())
		if err != nil {
			return reconcileInput{}, err
		}
		input.active[placement.key] = activeObservation{actual: observed}
	}

	for _, key := range stateKeys(snapshot) {
		record := snapshot.Links[key]
		current, err := resolveStateTarget(snapshot.Home, record.Target)
		if err != nil {
			if !isSafeStaleResolutionDrift(err) {
				return reconcileInput{}, fmt.Errorf(
					"resolve state link %s: %w",
					placementLabel(key.ModuleID, key.PlacementID),
					err,
				)
			}
			input.stateLinks[key] = stateLinkObservation{
				resolution: stateTargetObstructed,
			}
			continue
		}
		overlaps, err := controls.TargetOverlaps(current)
		if err != nil {
			return reconcileInput{}, err
		}
		observed, err := observeLink(current.Lexical())
		if err != nil {
			return reconcileInput{}, fmt.Errorf(
				"inspect state link %s: %w",
				placementLabel(key.ModuleID, key.PlacementID),
				err,
			)
		}
		input.stateLinks[key] = stateLinkObservation{
			resolution:     stateTargetResolved,
			target:         current,
			actual:         observed,
			controlOverlap: overlaps,
		}
	}

	targets := make([]observedTarget, 0, len(desired)+len(input.stateLinks))
	for _, placement := range desired {
		targets = append(targets, observedTarget{
			ref:    targetRef{role: targetDesired, key: placement.key},
			target: placement.target,
		})
	}
	for _, key := range stateKeys(snapshot) {
		observed := input.stateLinks[key]
		if observed.resolution != stateTargetResolved {
			continue
		}
		targets = append(targets, observedTarget{
			ref:    targetRef{role: targetState, key: key},
			target: observed.target,
		})
	}
	for left := range targets {
		for right := left + 1; right < len(targets); right++ {
			leftFacts, rightFacts, err := observeTargetRelation(
				targets[left].target,
				targets[right].target,
			)
			if err != nil {
				return reconcileInput{}, fmt.Errorf(
					"compare %s and %s targets: %w",
					targetRefLabel(targets[left].ref),
					targetRefLabel(targets[right].ref),
					err,
				)
			}
			input.relations[relationKey{
				from: targets[left].ref,
				to:   targets[right].ref,
			}] = leftFacts
			input.relations[relationKey{
				from: targets[right].ref,
				to:   targets[left].ref,
			}] = rightFacts
		}
	}
	return input, nil
}

func observeTargetRelation(
	left corepaths.Target,
	right corepaths.Target,
) (relationFacts, relationFacts, error) {
	leftTraverses, err := corepaths.TargetParentTraversesLink(left, right.Resolved())
	if err != nil {
		return relationFacts{}, relationFacts{}, err
	}
	rightTraverses, err := corepaths.TargetParentTraversesLink(right, left.Resolved())
	if err != nil {
		return relationFacts{}, relationFacts{}, err
	}
	equal := corepaths.TargetsEqual(left, right)
	return relationFacts{
			equal:     equal,
			contains:  corepaths.TargetStrictlyContains(left, right),
			traverses: leftTraverses,
		}, relationFacts{
			equal:     equal,
			contains:  corepaths.TargetStrictlyContains(right, left),
			traverses: rightTraverses,
		}, nil
}

func resolveDesired(
	home string,
	controls corepaths.ResolvedControls,
	modules []config.Module,
) ([]desiredPlacement, error) {
	pathInputs := make([]corepaths.Placement, 0)
	for _, module := range modules {
		for _, link := range module.Links {
			pathInputs = append(pathInputs, corepaths.Placement{
				Label:  placementLabel(module.ID, link.ID),
				Target: link.Target,
			})
		}
		for _, local := range module.Locals {
			pathInputs = append(pathInputs, corepaths.Placement{
				Label:  placementLabel(module.ID, local.ID),
				Target: local.Target,
			})
		}
	}

	resolved, err := controls.Validate(home, pathInputs)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]corepaths.Target, len(resolved))
	for _, placement := range resolved {
		targets[placement.Label] = placement.Target
	}

	desired := make([]desiredPlacement, 0, len(resolved))
	for _, module := range modules {
		for _, link := range module.Links {
			desired = append(desired, desiredPlacement{
				key:         state.Key{ModuleID: module.ID, PlacementID: link.ID},
				kind:        placementLink,
				target:      targets[placementLabel(module.ID, link.ID)],
				source:      link.SourcePath,
				destination: link.SourcePath,
			})
		}
		for _, local := range module.Locals {
			desired = append(desired, desiredPlacement{
				key:    state.Key{ModuleID: module.ID, PlacementID: local.ID},
				kind:   placementLocal,
				target: targets[placementLabel(module.ID, local.ID)],
				source: local.ExamplePath,
			})
		}
	}
	slices.SortFunc(desired, compareDesired)
	return desired, nil
}

func compareDesired(left, right desiredPlacement) int {
	if byModule := strings.Compare(left.key.ModuleID, right.key.ModuleID); byModule != 0 {
		return byModule
	}
	if byPlacement := strings.Compare(left.key.PlacementID, right.key.PlacementID); byPlacement != 0 {
		return byPlacement
	}
	return strings.Compare(left.target.Lexical(), right.target.Lexical())
}

func validatePlanRequest(request planRequest) (string, error) {
	if request.Home == "" || !filepath.IsAbs(request.Home) {
		return "", fmt.Errorf("planner HOME must be a non-empty absolute path")
	}
	home := filepath.Clean(request.Home)
	if request.State.Home != home {
		return "", fmt.Errorf(
			"planner state HOME %q does not match %q",
			request.State.Home,
			home,
		)
	}
	return home, nil
}

func isPlanningIssue(err error) bool {
	return errors.Is(err, corepaths.ErrTargetConflict) ||
		errors.Is(err, corepaths.ErrControlBoundary) ||
		errors.Is(err, corepaths.ErrControlTopology)
}

func pathIssues(err error) []Issue {
	code, recovery := pathIssueCode(err)
	labels := corepaths.PlacementLabels(err)
	if len(labels) == 0 {
		return []Issue{{
			Severity: IssueBlocker,
			Code:     code,
			Reason:   err.Error(),
			Recovery: recovery,
		}}
	}
	issues := make([]Issue, 0, len(labels))
	for _, label := range labels {
		moduleID, placementID, _ := strings.Cut(label, "/")
		issues = append(issues, Issue{
			Severity:    IssueBlocker,
			Code:        code,
			ModuleID:    moduleID,
			PlacementID: placementID,
			Reason:      err.Error(),
			Recovery:    recovery,
		})
	}
	return issues
}

func pathIssueCode(err error) (IssueCode, Recovery) {
	switch {
	case errors.Is(err, corepaths.ErrControlBoundary):
		return IssueCodeControlBoundary, RecoveryPaths
	case errors.Is(err, corepaths.ErrControlTopology):
		return IssueCodeControlTopology, RecoveryPaths
	default:
		return IssueCodeTargetConflict, RecoveryManualMigration
	}
}

func resolveStateTarget(home, target string) (corepaths.Target, error) {
	resolved, err := corepaths.ResolveAbsoluteTarget(home, target)
	if err != nil {
		return corepaths.Target{}, fmt.Errorf("resolve state target %q: %w", target, err)
	}
	return resolved, nil
}

func isSafeStaleResolutionDrift(err error) bool {
	class, classified := corepaths.ClassifyResolutionError(err)
	return classified && class == corepaths.ResolutionObstructed
}

func stateKeys(snapshot state.Snapshot) []state.Key {
	keys := make([]state.Key, 0, len(snapshot.Links))
	for key := range snapshot.Links {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareStateKey)
	return keys
}

func compareStateKey(left, right state.Key) int {
	if byModule := strings.Compare(left.ModuleID, right.ModuleID); byModule != 0 {
		return byModule
	}
	return strings.Compare(left.PlacementID, right.PlacementID)
}

func targetRefLabel(ref targetRef) string {
	role := "desired"
	if ref.role == targetState {
		role = "state"
	}
	return role + " " + placementLabel(ref.key.ModuleID, ref.key.PlacementID)
}

func placementLabel(moduleID, placementID string) string {
	return moduleID + "/" + placementID
}
