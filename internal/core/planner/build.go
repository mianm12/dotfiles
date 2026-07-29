package planner

import (
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

// Build observes the filesystem without mutating it and returns a deterministic
// plan. Active decisions always precede stale cleanup decisions.
func Build(request Request) (Plan, error) {
	home, err := validateRequest(request)
	if err != nil {
		return Plan{}, err
	}

	scope := newScope(request.Scope)
	desired, err := resolveDesired(home, request.Controls, request.Modules, scope)
	if err != nil {
		return Plan{}, err
	}

	usedState := make(map[placementKey]bool)
	plan := Plan{Actions: make([]Action, 0, len(desired))}
	for _, placement := range desired {
		if !scope.includes(placement.key.moduleID) {
			continue
		}
		action, used, planErr := planActive(placement, request.State)
		if planErr != nil {
			return Plan{}, planErr
		}
		if action.Decision != DecisionConflict {
			owner, owned, ownershipErr := stateOwnedParentLink(
				home,
				placement.target,
				request.State,
			)
			if ownershipErr != nil {
				return Plan{}, ownershipErr
			}
			if owned {
				action.Decision = DecisionConflict
				action.Reason = fmt.Sprintf(
					"target traverses state-owned link from module %q placement %q",
					owner.moduleID,
					owner.placementID,
				)
			}
		}
		if action.Decision != DecisionConflict && action.Kind == state.KindLink {
			dependent, traversed, traversalErr := desiredTraversingLink(
				desired,
				action.ResolvedTarget,
			)
			if traversalErr != nil {
				return Plan{}, fmt.Errorf(
					"compare active link %s with desired targets: %w",
					placementLabel(action.ModuleID, action.PlacementID),
					traversalErr,
				)
			}
			if traversed {
				guard, guardErr := activeLinkNeedsProspectiveGuard(
					home,
					action,
					request.State,
				)
				if guardErr != nil {
					return Plan{}, fmt.Errorf(
						"inspect current ownership for active link %s: %w",
						placementLabel(action.ModuleID, action.PlacementID),
						guardErr,
					)
				}
				if guard {
					action.Decision = DecisionConflict
					action.Reason = fmt.Sprintf(
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
		plan.Actions = append(plan.Actions, action)
	}

	cleanup, err := planStale(
		home,
		request.Controls,
		desired,
		plan.Actions,
		request.State,
		usedState,
		scope,
	)
	if err != nil {
		return Plan{}, err
	}
	plan.Actions = append(plan.Actions, cleanup...)
	return plan, nil
}

func validateRequest(request Request) (string, error) {
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
	controls corepaths.Controls,
	modules []config.Module,
	scope moduleScope,
) ([]desiredPlacement, error) {
	pathInputs := make([]corepaths.Placement, 0)
	selectedLabels := make([]string, 0)
	for _, module := range modules {
		for _, link := range module.Links {
			label := placementLabel(module.ID, link.ID)
			pathInputs = append(pathInputs, corepaths.Placement{
				Label:  label,
				Target: link.Target,
			})
			if scope.includes(module.ID) {
				selectedLabels = append(selectedLabels, label)
			}
		}
		for _, local := range module.Locals {
			label := placementLabel(module.ID, local.ID)
			pathInputs = append(pathInputs, corepaths.Placement{
				Label:  label,
				Target: local.Target,
			})
			if scope.includes(module.ID) {
				selectedLabels = append(selectedLabels, label)
			}
		}
	}

	var (
		resolved []corepaths.ResolvedPlacement
		err      error
	)
	if scope.all {
		resolved, err = corepaths.Validate(home, controls, pathInputs)
	} else {
		resolved, err = corepaths.ValidateScoped(
			home,
			controls,
			pathInputs,
			selectedLabels,
		)
	}
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
) (Action, bool, error) {
	base := actionForDesired(desired)
	if desired.kind == state.KindLink {
		owner, exists := otherModuleOwner(snapshot, desired)
		if exists {
			base.Decision = DecisionConflict
			base.Reason = fmt.Sprintf(
				"target is owned by module %q placement %q",
				owner.moduleID,
				owner.placementID,
			)
			return base, false, nil
		}
	}

	record, exists := statePlacement(snapshot, desired.key)
	if exists && record.Kind != desired.kind {
		base.Decision = DecisionConflict
		base.Reason = fmt.Sprintf(
			"state kind %q differs from desired kind %q",
			record.Kind,
			desired.kind,
		)
		return base, true, nil
	}

	recordApplies := exists && samePlacementTarget(desired, record)
	if desired.kind == state.KindLocal {
		exists, err := observeLocal(desired.target.Lexical())
		if err != nil {
			return Action{}, false, err
		}
		return planLocal(base, exists), recordApplies, nil
	}

	actual, err := observeLink(desired.target.Lexical())
	if err != nil {
		return Action{}, false, err
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
	record state.Placement,
	hasState bool,
) Action {
	if actual.kind != actualAbsent && actual.kind != actualSymlink {
		base.Decision = DecisionConflict
		base.Reason = fmt.Sprintf("actual target is %s", actual.kind)
		return base
	}
	if actual.kind == actualAbsent {
		base.Decision = DecisionCreateLink
		return base
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
		return base
	}

	if !hasState || actual.linkDestination != record.LinkDestination {
		base.Decision = DecisionConflict
		base.Reason = "actual symlink is not explained by desired or state"
		return base
	}
	if base.ResolvedTarget != record.ResolvedTarget {
		base.Decision = DecisionConflict
		base.Reason = "resolved target changed since state was recorded"
		return base
	}

	base.Decision = DecisionUpdate
	base.ExpectedResolvedTarget = record.ResolvedTarget
	base.ExpectedLinkDestination = record.LinkDestination
	return base
}

func planStale(
	home string,
	controls corepaths.Controls,
	desired []desiredPlacement,
	active []Action,
	snapshot state.Snapshot,
	used map[placementKey]bool,
	scope moduleScope,
) ([]Action, error) {
	keys := stateKeys(snapshot)
	actions := make([]Action, 0)
	for _, key := range keys {
		if used[key] || !scope.includes(key.moduleID) {
			continue
		}
		record, _ := statePlacement(snapshot, key)
		action, err := planOneStale(
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
		actions = append(actions, action)
	}
	return normalizeStalePrunes(home, actions)
}

func normalizeStalePrunes(
	home string,
	actions []Action,
) ([]Action, error) {
	slots := make([]int, 0)
	prunes := make([]stalePrune, 0)
	for index := range actions {
		if actions[index].Decision != DecisionPrune {
			continue
		}
		current, err := resolveStateTarget(home, actions[index].Target)
		if err != nil {
			return nil, fmt.Errorf(
				"re-resolve stale prune %s: %w",
				placementLabel(actions[index].ModuleID, actions[index].PlacementID),
				err,
			)
		}
		if current.Resolved() != actions[index].ResolvedTarget {
			return nil, fmt.Errorf(
				"stale prune %s changed resolved target while ordering",
				placementLabel(actions[index].ModuleID, actions[index].PlacementID),
			)
		}

		duplicate := slices.IndexFunc(prunes, func(candidate stalePrune) bool {
			return corepaths.TargetsEqual(current, candidate.target)
		})
		if duplicate >= 0 {
			representative := prunes[duplicate].action
			if actions[index].ExpectedLinkDestination !=
				representative.ExpectedLinkDestination {
				return nil, fmt.Errorf(
					"duplicate stale prune %s has inconsistent link destination",
					placementLabel(
						actions[index].ModuleID,
						actions[index].PlacementID,
					),
				)
			}
			actions[index].Decision = DecisionForget
			actions[index].Reason = fmt.Sprintf(
				"stale target shares ownership with module %q placement %q; "+
					"that action represents cleanup",
				representative.ModuleID,
				representative.PlacementID,
			)
			continue
		}
		slots = append(slots, index)
		prunes = append(prunes, stalePrune{
			action: actions[index],
			target: current,
		})
	}

	ordered, err := orderStalePrunes(prunes)
	if err != nil {
		return nil, err
	}
	for index, slot := range slots {
		actions[slot] = ordered[index].action
	}
	return actions, nil
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
	controls corepaths.Controls,
	desired []desiredPlacement,
	active []Action,
	key placementKey,
	record state.Placement,
) (Action, error) {
	switch record.Kind {
	case state.KindLink, state.KindLocal:
	default:
		return Action{}, fmt.Errorf(
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
				return Action{}, fmt.Errorf(
					"resolve stale local %s: %w",
					placementLabel(key.moduleID, key.placementID),
					err,
				)
			}
		} else {
			overlaps, overlapErr := corepaths.TargetOverlapsControls(controls, current)
			if overlapErr != nil {
				return Action{}, overlapErr
			}
			if overlaps {
				base.Decision = DecisionForget
				base.Reason = "stale target overlaps a protected control path"
				return base, nil
			}
		}
		base.Decision = DecisionForget
		base.Reason = "local left desired; local targets are never pruned"
		return base, nil
	}

	current, err := resolveStateTarget(home, record.Target)
	if err != nil {
		if !isSafeStaleResolutionDrift(err) {
			return Action{}, fmt.Errorf(
				"resolve stale link %s: %w",
				placementLabel(key.moduleID, key.placementID),
				err,
			)
		}
		base.Decision = DecisionForget
		base.Reason = "stale target cannot be resolved safely"
		return base, nil
	}
	base.ResolvedTarget = current.Resolved()
	overlaps, err := corepaths.TargetOverlapsControls(controls, current)
	if err != nil {
		return Action{}, err
	}
	if overlaps {
		base.Decision = DecisionForget
		base.Reason = "stale target overlaps a protected control path"
		return base, nil
	}

	if targetUsedByDesired(current, desired) {
		base.Decision = DecisionForget
		base.Reason = "stale target is reused by desired configuration"
		return base, nil
	}
	if current.Resolved() != record.ResolvedTarget {
		base.Decision = DecisionForget
		base.Reason = "stale resolved target changed"
		return base, nil
	}

	actual, err := observeLink(current.Lexical())
	if err != nil {
		return Action{}, fmt.Errorf(
			"inspect stale link %s: %w",
			placementLabel(key.moduleID, key.placementID),
			err,
		)
	}
	if actual.kind == actualAbsent {
		base.Decision = DecisionForget
		base.Reason = "stale target is absent"
		return base, nil
	}
	if stateOwnsLink(record, current, actual) {
		dependent, traversed, err := desiredTraversingLink(
			desired,
			current.Resolved(),
		)
		if err != nil {
			return Action{}, fmt.Errorf(
				"compare stale link %s with desired targets: %w",
				placementLabel(key.moduleID, key.placementID),
				err,
			)
		}
		if traversed {
			base.Decision = DecisionConflict
			base.Reason = fmt.Sprintf(
				"state-owned link is traversed by active module %q placement %q",
				dependent.moduleID,
				dependent.placementID,
			)
			return base, nil
		}
		parent, traversed, err := updatedParentLink(current, active)
		if err != nil {
			return Action{}, fmt.Errorf(
				"compare stale link %s with active updates: %w",
				placementLabel(key.moduleID, key.placementID),
				err,
			)
		}
		if traversed {
			base.Decision = DecisionConflict
			base.Reason = fmt.Sprintf(
				"stale state-owned link cleanup would be invalidated by "+
					"active link update from module %q placement %q",
				parent.moduleID,
				parent.placementID,
			)
			return base, nil
		}
		base.Decision = DecisionPrune
		return base, nil
	}

	base.Decision = DecisionForget
	base.Reason = staleDriftReason(actual, current.Resolved(), record)
	return base, nil
}

func staleDriftReason(
	actual actual,
	resolved string,
	record state.Placement,
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
	record state.Placement,
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
	record state.Placement,
	current corepaths.Target,
	actual actual,
) bool {
	return stateLinkTargetMatches(record, current) &&
		actual.kind == actualSymlink &&
		actual.linkDestination == record.LinkDestination
}

func stateLinkTargetMatches(
	record state.Placement,
	current corepaths.Target,
) bool {
	return current.Resolved() == record.ResolvedTarget
}

func samePlacementTarget(
	desired desiredPlacement,
	record state.Placement,
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
) (state.Placement, bool) {
	module, exists := snapshot.Modules[key.moduleID]
	if !exists {
		return state.Placement{}, false
	}
	placement, exists := module.Placements[key.placementID]
	return placement, exists
}

func stateKeys(snapshot state.Snapshot) []placementKey {
	keys := make([]placementKey, 0)
	for moduleID, module := range snapshot.Modules {
		for placementID := range module.Placements {
			keys = append(keys, placementKey{
				moduleID:    moduleID,
				placementID: placementID,
			})
		}
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

type moduleScope struct {
	all     bool
	modules map[string]bool
}

func newScope(modules []string) moduleScope {
	if modules == nil {
		return moduleScope{all: true}
	}
	selected := make(map[string]bool, len(modules))
	for _, module := range modules {
		selected[module] = true
	}
	return moduleScope{modules: selected}
}

func (scope moduleScope) includes(moduleID string) bool {
	return scope.all || scope.modules[moduleID]
}
