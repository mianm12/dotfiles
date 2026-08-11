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

type placementKey struct {
	moduleID    string
	placementID string
}

type desiredPlacement struct {
	key         placementKey
	kind        state.Kind
	target      corepaths.Target
	source      string
	destination string
}

type stalePrune struct {
	action Action
	target corepaths.Target
}

type candidate struct {
	action   Action
	conflict bool
}

// buildPlan observes the filesystem without mutating it and returns a deterministic
// plan. Active decisions always precede stale cleanup decisions.
func buildPlan(request planRequest) (Plan, error) {
	home, err := validatePlanRequest(request)
	if err != nil {
		return Plan{}, err
	}

	desired, err := resolveDesired(home, request.Controls, request.Modules)
	if err != nil {
		if isPlanningProblem(err) {
			return Plan{
				Problems:   pathProblems(err),
				finalState: cloneSnapshot(request.State),
			}, nil
		}
		return Plan{}, err
	}

	usedState := make(map[placementKey]bool)
	candidates := make([]candidate, 0, len(desired))
	for _, placement := range desired {
		planned, used, planErr := planActive(placement, request.State)
		if planErr != nil {
			return Plan{}, planErr
		}
		if !planned.conflict {
			owner, owned, ownershipErr := stateOwnedParentLink(
				home,
				placement.target,
				request.State,
			)
			if ownershipErr != nil {
				return Plan{}, ownershipErr
			}
			if owned {
				planned.conflict = true
				planned.action.Reason = fmt.Sprintf(
					"target traverses state-owned link from module %q placement %q",
					owner.moduleID,
					owner.placementID,
				)
			}
		}
		if !planned.conflict && planned.action.Kind == state.KindLink {
			dependent, traversed, traversalErr := desiredTraversingLink(
				desired,
				planned.action.ResolvedTarget,
			)
			if traversalErr != nil {
				return Plan{}, fmt.Errorf(
					"compare active link %s with desired targets: %w",
					placementLabel(planned.action.ModuleID, planned.action.PlacementID),
					traversalErr,
				)
			}
			if traversed {
				guard, guardErr := activeLinkNeedsProspectiveGuard(
					home,
					planned.action,
					request.State,
				)
				if guardErr != nil {
					return Plan{}, fmt.Errorf(
						"inspect current ownership for active link %s: %w",
						placementLabel(planned.action.ModuleID, planned.action.PlacementID),
						guardErr,
					)
				}
				if guard {
					planned.conflict = true
					planned.action.Reason = fmt.Sprintf(
						"active link cannot be owned or changed while traversed by "+
							"effective module %q placement %q",
						dependent.moduleID,
						dependent.placementID,
					)
				}
			}
		}
		if used {
			usedState[placement.key] = true
		}
		candidates = append(candidates, planned)
	}
	active := executableActions(candidates)

	cleanup, err := planStale(
		home,
		request.Controls,
		desired,
		active,
		request.State,
		usedState,
	)
	if err != nil {
		return Plan{}, err
	}
	candidates = append(candidates, cleanup...)
	return finalize(request.State, desired, candidates), nil
}

func isPlanningProblem(err error) bool {
	return errors.Is(err, corepaths.ErrTargetConflict) ||
		errors.Is(err, corepaths.ErrControlBoundary) ||
		errors.Is(err, corepaths.ErrControlTopology)
}

func pathProblems(err error) []Problem {
	labels := corepaths.PlacementLabels(err)
	if len(labels) == 0 {
		return []Problem{{
			Kind:   ProblemBlocked,
			Code:   pathProblemCode(err),
			Reason: err.Error(),
		}}
	}
	problems := make([]Problem, 0, len(labels))
	for _, label := range labels {
		moduleID, placementID, _ := strings.Cut(label, "/")
		problems = append(problems, Problem{
			Kind:        ProblemBlocked,
			Code:        pathProblemCode(err),
			ModuleID:    moduleID,
			PlacementID: placementID,
			Reason:      err.Error(),
		})
	}
	return problems
}

func pathProblemCode(err error) ProblemCode {
	if errors.Is(err, corepaths.ErrControlBoundary) {
		return ProblemCodeControlBoundary
	}
	return ""
}

func executableActions(candidates []candidate) []Action {
	actions := make([]Action, 0, len(candidates))
	for _, planned := range candidates {
		if planned.conflict {
			continue
		}
		actions = append(actions, planned.action)
	}
	return actions
}

func finalize(
	snapshot state.Snapshot,
	desired []desiredPlacement,
	candidates []candidate,
) Plan {
	desiredByKey := make(map[placementKey]desiredPlacement, len(desired))
	for _, placement := range desired {
		desiredByKey[placement.key] = placement
	}
	plan := Plan{
		Transitions: make([]Transition, 0, len(candidates)),
		Problems:    make([]Problem, 0),
	}
	transitionByKey := make(map[placementKey]int, len(candidates))
	for sequence, planned := range candidates {
		key := placementKey{
			moduleID:    planned.action.ModuleID,
			placementID: planned.action.PlacementID,
		}
		index, exists := transitionByKey[key]
		if !exists {
			transition := Transition{
				ModuleID:    key.moduleID,
				PlacementID: key.placementID,
			}
			if placement, desiredExists := desiredByKey[key]; desiredExists {
				transition.Desired = true
				transition.FinalRecord = recordForDesired(placement)
			}
			plan.Transitions = append(plan.Transitions, transition)
			index = len(plan.Transitions) - 1
			transitionByKey[key] = index
		}
		if planned.conflict {
			plan.Problems = append(plan.Problems, Problem{
				Kind:        ProblemConflict,
				ModuleID:    planned.action.ModuleID,
				PlacementID: planned.action.PlacementID,
				Target:      planned.action.Target,
				Reason:      planned.action.Reason,
			})
			continue
		}
		if planned.action.Decision == DecisionKeep &&
			stateAlreadyFinal(snapshot, key, plan.Transitions[index].FinalRecord) {
			continue
		}
		planned.action.Order = sequence
		plan.Transitions[index].Actions = append(
			plan.Transitions[index].Actions,
			planned.action,
		)
	}
	plan.finalState = finalState(snapshot, plan.Transitions)
	return plan
}

func stateAlreadyFinal(
	snapshot state.Snapshot,
	key placementKey,
	record state.Record,
) bool {
	current, exists := snapshot.Records[state.Key{
		ModuleID:    key.moduleID,
		PlacementID: key.placementID,
	}]
	return exists && current == record
}

func recordForDesired(desired desiredPlacement) state.Record {
	record := state.Record{
		Kind:   desired.kind,
		Target: desired.target.Lexical(),
	}
	if desired.kind == state.KindLink {
		record.ResolvedTarget = desired.target.Resolved()
		record.LinkDestination = desired.destination
	}
	return record
}

func finalState(snapshot state.Snapshot, transitions []Transition) state.Snapshot {
	result := cloneSnapshot(snapshot)
	for _, transition := range transitions {
		key := state.Key{
			ModuleID:    transition.ModuleID,
			PlacementID: transition.PlacementID,
		}
		if transition.Desired {
			result.Records[key] = transition.FinalRecord
			continue
		}
		delete(result.Records, key)
	}
	return result
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

func resolveDesired(
	home string,
	controls corepaths.ResolvedControls,
	modules []config.Module,
) ([]desiredPlacement, error) {
	pathInputs := make([]corepaths.Placement, 0)
	for _, module := range modules {
		for _, link := range module.Links {
			label := placementLabel(module.ID, link.ID)
			pathInputs = append(pathInputs, corepaths.Placement{
				Label:  label,
				Target: link.Target,
			})
		}
		for _, local := range module.Locals {
			label := placementLabel(module.ID, local.ID)
			pathInputs = append(pathInputs, corepaths.Placement{
				Label:  label,
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
			key := placementKey{moduleID: module.ID, placementID: link.ID}
			desired = append(desired, desiredPlacement{
				key:         key,
				kind:        state.KindLink,
				target:      targets[placementLabel(module.ID, link.ID)],
				source:      link.SourcePath,
				destination: link.SourcePath,
			})
		}
		for _, local := range module.Locals {
			key := placementKey{moduleID: module.ID, placementID: local.ID}
			desired = append(desired, desiredPlacement{
				key:    key,
				kind:   state.KindLocal,
				target: targets[placementLabel(module.ID, local.ID)],
				source: local.ExamplePath,
			})
		}
	}
	return desired, nil
}

func planActive(
	desired desiredPlacement,
	snapshot state.Snapshot,
) (candidate, bool, error) {
	base := actionForDesired(desired)
	if desired.kind == state.KindLink {
		owner, exists := otherModuleOwner(snapshot, desired)
		if exists {
			base.Reason = fmt.Sprintf(
				"target is owned by module %q placement %q",
				owner.moduleID,
				owner.placementID,
			)
			return candidate{action: base, conflict: true}, false, nil
		}
	}

	record, exists := statePlacement(snapshot, desired.key)
	if exists && record.Kind != desired.kind {
		base.Reason = fmt.Sprintf(
			"state kind %q differs from desired kind %q",
			record.Kind,
			desired.kind,
		)
		return candidate{action: base, conflict: true}, true, nil
	}

	recordApplies := exists && samePlacementTarget(desired, record)
	if desired.kind == state.KindLocal {
		exists, err := observeLocal(desired.target.Lexical())
		if err != nil {
			return candidate{}, false, err
		}
		return candidate{action: planLocal(base, exists)}, recordApplies, nil
	}

	actual, err := observeLink(desired.target.Lexical())
	if err != nil {
		return candidate{}, false, err
	}
	return planLink(base, actual, record, recordApplies), recordApplies, nil
}

func planLocal(base Action, exists bool) Action {
	if !exists {
		base.Decision = DecisionCreateLocal
		return base
	}
	base.Decision = DecisionKeep
	return base
}

func planLink(
	base Action,
	actual actual,
	record state.Record,
	hasState bool,
) candidate {
	if actual.kind != actualAbsent && actual.kind != actualSymlink {
		base.Reason = fmt.Sprintf("actual target is %s", actual.kind)
		return candidate{action: base, conflict: true}
	}
	if actual.kind == actualAbsent {
		base.Decision = DecisionCreateLink
		return candidate{action: base}
	}

	if actual.linkDestination == base.LinkDestination {
		switch {
		case !hasState:
			base.Decision = DecisionAdopt
		case record.LinkDestination == base.LinkDestination:
			base.Decision = DecisionKeep
		default:
			base.Decision = DecisionRepairState
		}
		return candidate{action: base}
	}

	if !hasState || actual.linkDestination != record.LinkDestination {
		base.Reason = "actual symlink is not explained by desired or state"
		return candidate{action: base, conflict: true}
	}
	if base.ResolvedTarget != record.ResolvedTarget {
		base.Reason = "resolved target changed since state was recorded"
		return candidate{action: base, conflict: true}
	}

	base.Decision = DecisionUpdate
	base.ExpectedResolvedTarget = record.ResolvedTarget
	base.ExpectedLinkDestination = record.LinkDestination
	return candidate{action: base}
}

func planStale(
	home string,
	controls corepaths.ResolvedControls,
	desired []desiredPlacement,
	active []Action,
	snapshot state.Snapshot,
	used map[placementKey]bool,
) ([]candidate, error) {
	keys := stateKeys(snapshot)
	results := make([]candidate, 0)
	for _, key := range keys {
		if used[key] {
			continue
		}
		record, _ := statePlacement(snapshot, key)
		planned, err := planOneStale(
			home,
			controls,
			desired,
			active,
			key,
			record,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, planned)
	}
	return normalizeStalePrunes(home, results)
}

func normalizeStalePrunes(
	home string,
	results []candidate,
) ([]candidate, error) {
	slots := make([]int, 0)
	prunes := make([]stalePrune, 0)
	for index := range results {
		if results[index].action.Decision != DecisionPrune {
			continue
		}
		current, err := resolveStateTarget(home, results[index].action.Target)
		if err != nil {
			return nil, fmt.Errorf(
				"re-resolve stale prune %s: %w",
				placementLabel(results[index].action.ModuleID, results[index].action.PlacementID),
				err,
			)
		}
		if current.Resolved() != results[index].action.ResolvedTarget {
			return nil, fmt.Errorf(
				"stale prune %s changed resolved target while ordering",
				placementLabel(results[index].action.ModuleID, results[index].action.PlacementID),
			)
		}

		duplicate := slices.IndexFunc(prunes, func(candidate stalePrune) bool {
			return corepaths.TargetsEqual(current, candidate.target)
		})
		if duplicate >= 0 {
			representative := prunes[duplicate].action
			if results[index].action.ExpectedLinkDestination !=
				representative.ExpectedLinkDestination {
				return nil, fmt.Errorf(
					"duplicate stale prune %s has inconsistent link destination",
					placementLabel(
						results[index].action.ModuleID,
						results[index].action.PlacementID,
					),
				)
			}
			results[index].action.Decision = DecisionForget
			results[index].action.Reason = fmt.Sprintf(
				"stale target shares ownership with module %q placement %q; "+
					"that action represents cleanup",
				representative.ModuleID,
				representative.PlacementID,
			)
			continue
		}
		slots = append(slots, index)
		prunes = append(prunes, stalePrune{
			action: results[index].action,
			target: current,
		})
	}

	ordered, err := orderStalePrunes(prunes)
	if err != nil {
		return nil, err
	}
	for index, slot := range slots {
		results[slot].action = ordered[index].action
	}
	return results, nil
}

func orderStalePrunes(prunes []stalePrune) ([]stalePrune, error) {
	edges := make([][]int, len(prunes))
	incoming := make([]int, len(prunes))
	for child := range prunes {
		for parent := range prunes {
			if child == parent {
				continue
			}
			traverses, err := corepaths.TargetParentTraversesLink(
				prunes[child].target,
				prunes[parent].action.ResolvedTarget,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"compare stale prunes %s and %s: %w",
					placementLabel(
						prunes[child].action.ModuleID,
						prunes[child].action.PlacementID,
					),
					placementLabel(
						prunes[parent].action.ModuleID,
						prunes[parent].action.PlacementID,
					),
					err,
				)
			}
			if traverses {
				edges[child] = append(edges[child], parent)
				incoming[parent]++
			}
		}
	}

	ordered := make([]stalePrune, 0, len(prunes))
	used := make([]bool, len(prunes))
	for len(ordered) < len(prunes) {
		next := -1
		for index := range prunes {
			if !used[index] && incoming[index] == 0 {
				next = index
				break
			}
		}
		if next < 0 {
			return nil, fmt.Errorf("stale prune traversal dependencies form a cycle")
		}
		used[next] = true
		ordered = append(ordered, prunes[next])
		for _, parent := range edges[next] {
			incoming[parent]--
		}
	}
	return ordered, nil
}

func planOneStale(
	home string,
	controls corepaths.ResolvedControls,
	desired []desiredPlacement,
	active []Action,
	key placementKey,
	record state.Record,
) (candidate, error) {
	switch record.Kind {
	case state.KindLink, state.KindLocal:
	default:
		return candidate{}, fmt.Errorf(
			"%w: stale placement %s has unsupported kind %q",
			state.ErrInvalid,
			placementLabel(key.moduleID, key.placementID),
			record.Kind,
		)
	}

	base := Action{
		ModuleID:                key.moduleID,
		PlacementID:             key.placementID,
		Kind:                    record.Kind,
		Target:                  record.Target,
		ResolvedTarget:          record.ResolvedTarget,
		LinkDestination:         record.LinkDestination,
		ExpectedResolvedTarget:  record.ResolvedTarget,
		ExpectedLinkDestination: record.LinkDestination,
	}

	if record.Kind == state.KindLocal {
		current, err := resolveStateTarget(home, record.Target)
		if err != nil {
			if !isSafeStaleResolutionDrift(err) {
				return candidate{}, fmt.Errorf(
					"resolve stale local %s: %w",
					placementLabel(key.moduleID, key.placementID),
					err,
				)
			}
		} else {
			overlaps, overlapErr := controls.TargetOverlaps(current)
			if overlapErr != nil {
				return candidate{}, overlapErr
			}
			if overlaps {
				base.Decision = DecisionForget
				base.Reason = "stale target overlaps a protected control path"
				return candidate{action: base}, nil
			}
		}
		base.Decision = DecisionForget
		base.Reason = "local left desired; local targets are never pruned"
		return candidate{action: base}, nil
	}

	current, err := resolveStateTarget(home, record.Target)
	if err != nil {
		if !isSafeStaleResolutionDrift(err) {
			return candidate{}, fmt.Errorf(
				"resolve stale link %s: %w",
				placementLabel(key.moduleID, key.placementID),
				err,
			)
		}
		base.Decision = DecisionForget
		base.Reason = "stale target cannot be resolved safely"
		return candidate{action: base}, nil
	}
	base.ResolvedTarget = current.Resolved()
	overlaps, err := controls.TargetOverlaps(current)
	if err != nil {
		return candidate{}, err
	}
	if overlaps {
		base.Decision = DecisionForget
		base.Reason = "stale target overlaps a protected control path"
		return candidate{action: base}, nil
	}

	if targetUsedByDesired(current, desired) {
		base.Decision = DecisionForget
		base.Reason = "stale target is reused by desired configuration"
		return candidate{action: base}, nil
	}
	if current.Resolved() != record.ResolvedTarget {
		base.Decision = DecisionForget
		base.Reason = "stale resolved target changed"
		return candidate{action: base}, nil
	}

	actual, err := observeLink(current.Lexical())
	if err != nil {
		return candidate{}, fmt.Errorf(
			"inspect stale link %s: %w",
			placementLabel(key.moduleID, key.placementID),
			err,
		)
	}
	if actual.kind == actualAbsent {
		base.Decision = DecisionForget
		base.Reason = "stale target is absent"
		return candidate{action: base}, nil
	}
	if stateOwnsLink(record, current, actual) {
		dependent, traversed, err := desiredTraversingLink(
			desired,
			current.Resolved(),
		)
		if err != nil {
			return candidate{}, fmt.Errorf(
				"compare stale link %s with desired targets: %w",
				placementLabel(key.moduleID, key.placementID),
				err,
			)
		}
		if traversed {
			base.Reason = fmt.Sprintf(
				"state-owned link is traversed by active module %q placement %q",
				dependent.moduleID,
				dependent.placementID,
			)
			return candidate{action: base, conflict: true}, nil
		}
		parent, traversed, err := updatedParentLink(current, active)
		if err != nil {
			return candidate{}, fmt.Errorf(
				"compare stale link %s with active updates: %w",
				placementLabel(key.moduleID, key.placementID),
				err,
			)
		}
		if traversed {
			base.Reason = fmt.Sprintf(
				"stale state-owned link cleanup would be invalidated by "+
					"active link update from module %q placement %q",
				parent.moduleID,
				parent.placementID,
			)
			return candidate{action: base, conflict: true}, nil
		}
		base.Decision = DecisionPrune
		return candidate{action: base}, nil
	}

	base.Decision = DecisionForget
	base.Reason = staleDriftReason(actual, current.Resolved(), record)
	return candidate{action: base}, nil
}

func staleDriftReason(
	actual actual,
	resolved string,
	record state.Record,
) string {
	if actual.kind != actualSymlink {
		return fmt.Sprintf("stale target is now %s", actual.kind)
	}
	if actual.linkDestination != record.LinkDestination {
		return "stale symlink destination changed"
	}
	if resolved != record.ResolvedTarget {
		return "stale resolved target changed"
	}
	return "stale ownership evidence no longer matches"
}

func actionForDesired(desired desiredPlacement) Action {
	return Action{
		ModuleID:        desired.key.moduleID,
		PlacementID:     desired.key.placementID,
		Kind:            desired.kind,
		Target:          desired.target.Lexical(),
		ResolvedTarget:  desired.target.Resolved(),
		Source:          desired.source,
		LinkDestination: desired.destination,
	}
}

func otherModuleOwner(
	snapshot state.Snapshot,
	desired desiredPlacement,
) (placementKey, bool) {
	for _, key := range stateKeys(snapshot) {
		if key.moduleID == desired.key.moduleID {
			continue
		}
		record, _ := statePlacement(snapshot, key)
		if record.Kind == state.KindLink && samePlacementTarget(desired, record) {
			return key, true
		}
	}
	return placementKey{}, false
}

func stateOwnedParentLink(
	home string,
	target corepaths.Target,
	snapshot state.Snapshot,
) (placementKey, bool, error) {
	for _, key := range stateKeys(snapshot) {
		record, _ := statePlacement(snapshot, key)
		if record.Kind != state.KindLink {
			continue
		}
		traverses, err := corepaths.TargetParentTraversesLink(
			target,
			record.ResolvedTarget,
		)
		if err != nil {
			return placementKey{}, false, fmt.Errorf(
				"compare target with state-owned link %s: %w",
				placementLabel(key.moduleID, key.placementID),
				err,
			)
		}
		if !traverses {
			continue
		}

		owned, err := stateRecordOwnsLink(home, record)
		if err != nil {
			return placementKey{}, false, fmt.Errorf(
				"inspect state-owned link %s: %w",
				placementLabel(key.moduleID, key.placementID),
				err,
			)
		}
		if owned {
			return key, true, nil
		}
	}
	return placementKey{}, false, nil
}

func activeLinkNeedsProspectiveGuard(
	home string,
	action Action,
	snapshot state.Snapshot,
) (bool, error) {
	if action.Decision != DecisionKeep {
		return true, nil
	}
	record, exists := statePlacement(snapshot, placementKey{
		moduleID:    action.ModuleID,
		placementID: action.PlacementID,
	})
	if !exists {
		return true, nil
	}
	if record.Target == action.Target &&
		record.ResolvedTarget == action.ResolvedTarget &&
		record.LinkDestination == action.LinkDestination {
		return false, nil
	}
	owned, err := stateRecordOwnsLink(home, record)
	if err != nil {
		return false, err
	}
	return !owned ||
		action.ResolvedTarget != record.ResolvedTarget ||
		action.LinkDestination != record.LinkDestination, nil
}

func stateRecordOwnsLink(
	home string,
	record state.Record,
) (bool, error) {
	current, err := resolveStateTarget(home, record.Target)
	if err != nil {
		if isSafeStaleResolutionDrift(err) {
			return false, nil
		}
		return false, err
	}
	if !stateLinkTargetMatches(record, current) {
		return false, nil
	}
	actual, err := observeLink(current.Lexical())
	if err != nil {
		return false, err
	}
	return stateOwnsLink(record, current, actual), nil
}

func stateOwnsLink(
	record state.Record,
	current corepaths.Target,
	actual actual,
) bool {
	return stateLinkTargetMatches(record, current) &&
		actual.kind == actualSymlink &&
		actual.linkDestination == record.LinkDestination
}

func stateLinkTargetMatches(
	record state.Record,
	current corepaths.Target,
) bool {
	return current.Resolved() == record.ResolvedTarget
}

func samePlacementTarget(
	desired desiredPlacement,
	record state.Record,
) bool {
	return desired.target.Lexical() == record.Target ||
		(record.Kind == state.KindLink &&
			desired.target.Resolved() == record.ResolvedTarget)
}

func targetUsedByDesired(
	target corepaths.Target,
	desired []desiredPlacement,
) bool {
	return slices.ContainsFunc(desired, func(placement desiredPlacement) bool {
		return corepaths.TargetsEqual(target, placement.target)
	})
}

func desiredTraversingLink(
	desired []desiredPlacement,
	linkEntry string,
) (placementKey, bool, error) {
	for _, placement := range desired {
		traverses, err := corepaths.TargetParentTraversesLink(
			placement.target,
			linkEntry,
		)
		if err != nil {
			return placementKey{}, false, err
		}
		if traverses {
			return placement.key, true, nil
		}
	}
	return placementKey{}, false, nil
}

func updatedParentLink(
	target corepaths.Target,
	active []Action,
) (placementKey, bool, error) {
	for _, action := range active {
		if action.Decision != DecisionUpdate || action.Kind != state.KindLink {
			continue
		}
		traverses, err := corepaths.TargetParentTraversesLink(
			target,
			action.ResolvedTarget,
		)
		if err != nil {
			return placementKey{}, false, err
		}
		if traverses {
			return placementKey{
				moduleID:    action.ModuleID,
				placementID: action.PlacementID,
			}, true, nil
		}
	}
	return placementKey{}, false, nil
}

func resolveStateTarget(
	home string,
	target string,
) (corepaths.Target, error) {
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

func statePlacement(
	snapshot state.Snapshot,
	key placementKey,
) (state.Record, bool) {
	record, exists := snapshot.Records[state.Key{
		ModuleID:    key.moduleID,
		PlacementID: key.placementID,
	}]
	return record, exists
}

func stateKeys(snapshot state.Snapshot) []placementKey {
	keys := make([]placementKey, 0, len(snapshot.Records))
	for key := range snapshot.Records {
		keys = append(keys, placementKey{
			moduleID:    key.ModuleID,
			placementID: key.PlacementID,
		})
	}
	slices.SortFunc(keys, func(left, right placementKey) int {
		if byModule := strings.Compare(left.moduleID, right.moduleID); byModule != 0 {
			return byModule
		}
		return strings.Compare(left.placementID, right.placementID)
	})
	return keys
}

func placementLabel(moduleID, placementID string) string {
	return moduleID + "/" + placementID
}
