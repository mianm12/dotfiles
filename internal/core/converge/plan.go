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
	kind        state.Kind
	target      corepaths.Target
	source      string
	destination string
}

type stalePrune struct {
	draft  *transitionDraft
	target corepaths.Target
}

type transitionDraft struct {
	key            state.Key
	desired        *desiredPlacement
	active         *Action
	activeProblem  *Problem
	cleanup        *Action
	cleanupProblem *Problem
	scheduled      []Action
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

	desiredByKey := make(map[state.Key]desiredPlacement, len(desired))
	for _, placement := range desired {
		desiredByKey[placement.key] = placement
	}
	keys := transitionKeys(desired, request.State)
	drafts := make([]*transitionDraft, 0, len(keys))
	for _, key := range keys {
		placement, hasDesired := desiredByKey[key]
		var desiredInput *desiredPlacement
		if hasDesired {
			desiredInput = &placement
		}
		record, hasRecord := request.State.Records[key]
		var recordInput *state.Record
		if hasRecord {
			recordInput = &record
		}
		draft, planErr := planTransition(
			home,
			request.Controls,
			desired,
			key,
			desiredInput,
			recordInput,
		)
		if planErr != nil {
			return Plan{}, planErr
		}
		drafts = append(drafts, draft)
	}
	if err := validateTransitionDependencies(home, desired, request.State, drafts); err != nil {
		return Plan{}, err
	}
	cleanupOrder := make([]*transitionDraft, 0, len(request.State.Records))
	for _, draft := range drafts {
		if draft.cleanup != nil {
			cleanupOrder = append(cleanupOrder, draft)
		}
	}
	cleanupOrder, err = normalizeStalePrunes(home, cleanupOrder)
	if err != nil {
		return Plan{}, err
	}
	return finalizeDrafts(request.State, drafts, cleanupOrder), nil
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

func transitionKeys(
	desired []desiredPlacement,
	snapshot state.Snapshot,
) []state.Key {
	keys := make([]state.Key, 0, len(desired)+len(snapshot.Records))
	seen := make(map[state.Key]bool, cap(keys))
	for _, placement := range desired {
		if seen[placement.key] {
			continue
		}
		seen[placement.key] = true
		keys = append(keys, placement.key)
	}
	for _, key := range stateKeys(snapshot) {
		if seen[key] {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func planTransition(
	home string,
	controls corepaths.ResolvedControls,
	allDesired []desiredPlacement,
	key state.Key,
	desired *desiredPlacement,
	record *state.Record,
) (*transitionDraft, error) {
	draft := &transitionDraft{
		key:     key,
		desired: desired,
	}
	recordUsed := false
	if desired != nil {
		action, used, problem, err := planActive(*desired, record)
		if err != nil {
			return nil, err
		}
		draft.active = &action
		draft.activeProblem = problem
		recordUsed = used
	}
	if record == nil || recordUsed {
		return draft, nil
	}
	action, problem, err := planOneStale(
		home,
		controls,
		allDesired,
		key,
		*record,
	)
	if err != nil {
		return nil, err
	}
	draft.cleanup = &action
	draft.cleanupProblem = problem
	return draft, nil
}

func validateTransitionDependencies(
	home string,
	desired []desiredPlacement,
	snapshot state.Snapshot,
	drafts []*transitionDraft,
) error {
	for _, draft := range drafts {
		if draft.active == nil || draft.activeProblem != nil {
			continue
		}
		if draft.active.Kind == state.KindLink {
			owner, exists := otherModuleOwner(snapshot, *draft.desired)
			if exists {
				draft.active.Reason = fmt.Sprintf(
					"target is owned by module %q placement %q",
					owner.ModuleID,
					owner.PlacementID,
				)
				draft.activeProblem = problemForAction(*draft.active)
				continue
			}
		}
		owner, owned, err := stateOwnedParentLink(
			home,
			draft.desired.target,
			snapshot,
		)
		if err != nil {
			return err
		}
		if owned {
			draft.active.Reason = fmt.Sprintf(
				"target traverses state-owned link from module %q placement %q",
				owner.ModuleID,
				owner.PlacementID,
			)
			draft.activeProblem = problemForAction(*draft.active)
			continue
		}
		if draft.active.Kind != state.KindLink {
			continue
		}
		dependent, traversed, err := desiredTraversingLink(
			desired,
			draft.active.ResolvedTarget,
		)
		if err != nil {
			return fmt.Errorf(
				"compare active link %s with desired targets: %w",
				placementLabel(draft.active.ModuleID, draft.active.PlacementID),
				err,
			)
		}
		if !traversed {
			continue
		}
		guard, err := activeLinkNeedsProspectiveGuard(home, *draft.active, snapshot)
		if err != nil {
			return fmt.Errorf(
				"inspect current ownership for active link %s: %w",
				placementLabel(draft.active.ModuleID, draft.active.PlacementID),
				err,
			)
		}
		if guard {
			draft.active.Reason = fmt.Sprintf(
				"active link cannot be owned or changed while traversed by "+
					"effective module %q placement %q",
				dependent.ModuleID,
				dependent.PlacementID,
			)
			draft.activeProblem = problemForAction(*draft.active)
		}
	}

	active := executableActiveActions(drafts)
	for _, draft := range drafts {
		if draft.cleanup == nil ||
			draft.cleanupProblem != nil ||
			draft.cleanup.Decision != DecisionPrune {
			continue
		}
		dependent, traversed, err := desiredTraversingLink(
			desired,
			draft.cleanup.ResolvedTarget,
		)
		if err != nil {
			return fmt.Errorf(
				"compare stale link %s with desired targets: %w",
				placementLabel(draft.cleanup.ModuleID, draft.cleanup.PlacementID),
				err,
			)
		}
		if traversed {
			draft.cleanup.Reason = fmt.Sprintf(
				"state-owned link is traversed by active module %q placement %q",
				dependent.ModuleID,
				dependent.PlacementID,
			)
			draft.cleanupProblem = problemForAction(*draft.cleanup)
			continue
		}
		current, err := resolveStateTarget(home, draft.cleanup.Target)
		if err != nil {
			return fmt.Errorf(
				"re-resolve stale link %s for active dependencies: %w",
				placementLabel(draft.cleanup.ModuleID, draft.cleanup.PlacementID),
				err,
			)
		}
		parent, traversed, err := updatedParentLink(current, active)
		if err != nil {
			return fmt.Errorf(
				"compare stale link %s with active updates: %w",
				placementLabel(draft.cleanup.ModuleID, draft.cleanup.PlacementID),
				err,
			)
		}
		if traversed {
			draft.cleanup.Reason = fmt.Sprintf(
				"stale state-owned link cleanup would be invalidated by "+
					"active link update from module %q placement %q",
				parent.ModuleID,
				parent.PlacementID,
			)
			draft.cleanupProblem = problemForAction(*draft.cleanup)
		}
	}
	return nil
}

func executableActiveActions(drafts []*transitionDraft) []Action {
	actions := make([]Action, 0, len(drafts))
	for _, draft := range drafts {
		if draft.active == nil || draft.activeProblem != nil {
			continue
		}
		actions = append(actions, *draft.active)
	}
	return actions
}

func finalizeDrafts(
	snapshot state.Snapshot,
	drafts []*transitionDraft,
	cleanupOrder []*transitionDraft,
) Plan {
	plan := Plan{
		Transitions: make([]Transition, 0, len(drafts)),
		Problems:    make([]Problem, 0),
	}
	sequence := 0
	for _, draft := range drafts {
		if draft.activeProblem != nil {
			plan.Problems = append(plan.Problems, *draft.activeProblem)
		}
		if draft.active == nil || draft.activeProblem != nil {
			continue
		}
		if draft.active.Decision == DecisionKeep &&
			stateAlreadyFinal(snapshot, draft.key, recordForDesired(*draft.desired)) {
			continue
		}
		draft.active.Order = sequence
		sequence++
		draft.scheduled = append(draft.scheduled, *draft.active)
	}
	for _, draft := range cleanupOrder {
		if draft.cleanupProblem != nil {
			plan.Problems = append(plan.Problems, *draft.cleanupProblem)
			continue
		}
		draft.cleanup.Order = sequence
		sequence++
		draft.scheduled = append(draft.scheduled, *draft.cleanup)
	}
	for _, draft := range drafts {
		transition := Transition{
			ModuleID:    draft.key.ModuleID,
			PlacementID: draft.key.PlacementID,
			Actions:     append([]Action(nil), draft.scheduled...),
		}
		if draft.desired != nil {
			transition.Desired = true
			transition.FinalRecord = recordForDesired(*draft.desired)
		}
		plan.Transitions = append(plan.Transitions, transition)
	}
	plan.finalState = finalState(snapshot, plan.Transitions)
	return plan
}

func stateAlreadyFinal(
	snapshot state.Snapshot,
	key state.Key,
	record state.Record,
) bool {
	current, exists := snapshot.Records[key]
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
			key := state.Key{ModuleID: module.ID, PlacementID: link.ID}
			desired = append(desired, desiredPlacement{
				key:         key,
				kind:        state.KindLink,
				target:      targets[placementLabel(module.ID, link.ID)],
				source:      link.SourcePath,
				destination: link.SourcePath,
			})
		}
		for _, local := range module.Locals {
			key := state.Key{ModuleID: module.ID, PlacementID: local.ID}
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
	record *state.Record,
) (Action, bool, *Problem, error) {
	base := actionForDesired(desired)
	if record != nil && record.Kind != desired.kind {
		base.Reason = fmt.Sprintf(
			"state kind %q differs from desired kind %q",
			record.Kind,
			desired.kind,
		)
		return base, true, problemForAction(base), nil
	}

	recordApplies := record != nil && samePlacementTarget(desired, *record)
	if desired.kind == state.KindLocal {
		exists, err := observeLocal(desired.target.Lexical())
		if err != nil {
			return Action{}, false, nil, err
		}
		return planLocal(base, exists), recordApplies, nil, nil
	}

	actual, err := observeLink(desired.target.Lexical())
	if err != nil {
		return Action{}, false, nil, err
	}
	currentRecord := state.Record{}
	if record != nil {
		currentRecord = *record
	}
	action, problem := planLink(base, actual, currentRecord, recordApplies)
	return action, recordApplies, problem, nil
}

func problemForAction(action Action) *Problem {
	return &Problem{
		Kind:        ProblemConflict,
		ModuleID:    action.ModuleID,
		PlacementID: action.PlacementID,
		Target:      action.Target,
		Reason:      action.Reason,
	}
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
) (Action, *Problem) {
	if actual.kind != actualAbsent && actual.kind != actualSymlink {
		base.Reason = fmt.Sprintf("actual target is %s", actual.kind)
		return base, problemForAction(base)
	}
	if actual.kind == actualAbsent {
		base.Decision = DecisionCreateLink
		return base, nil
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
		return base, nil
	}

	if !hasState || actual.linkDestination != record.LinkDestination {
		base.Reason = "actual symlink is not explained by desired or state"
		return base, problemForAction(base)
	}
	if base.ResolvedTarget != record.ResolvedTarget {
		base.Reason = "resolved target changed since state was recorded"
		return base, problemForAction(base)
	}

	base.Decision = DecisionUpdate
	base.ExpectedResolvedTarget = record.ResolvedTarget
	base.ExpectedLinkDestination = record.LinkDestination
	return base, nil
}

func normalizeStalePrunes(
	home string,
	results []*transitionDraft,
) ([]*transitionDraft, error) {
	slots := make([]int, 0)
	prunes := make([]stalePrune, 0)
	for index := range results {
		action := results[index].cleanup
		if action == nil || action.Decision != DecisionPrune {
			continue
		}
		current, err := resolveStateTarget(home, action.Target)
		if err != nil {
			return nil, fmt.Errorf(
				"re-resolve stale prune %s: %w",
				placementLabel(action.ModuleID, action.PlacementID),
				err,
			)
		}
		if current.Resolved() != action.ResolvedTarget {
			return nil, fmt.Errorf(
				"stale prune %s changed resolved target while ordering",
				placementLabel(action.ModuleID, action.PlacementID),
			)
		}

		duplicate := slices.IndexFunc(prunes, func(existing stalePrune) bool {
			return corepaths.TargetsEqual(current, existing.target)
		})
		if duplicate >= 0 {
			representative := prunes[duplicate].draft.cleanup
			if action.ExpectedLinkDestination !=
				representative.ExpectedLinkDestination {
				return nil, fmt.Errorf(
					"duplicate stale prune %s has inconsistent link destination",
					placementLabel(
						action.ModuleID,
						action.PlacementID,
					),
				)
			}
			action.Decision = DecisionForget
			action.Reason = fmt.Sprintf(
				"stale target shares ownership with module %q placement %q; "+
					"that action represents cleanup",
				representative.ModuleID,
				representative.PlacementID,
			)
			continue
		}
		slots = append(slots, index)
		prunes = append(prunes, stalePrune{
			draft:  results[index],
			target: current,
		})
	}

	ordered, err := orderStalePrunes(prunes)
	if err != nil {
		return nil, err
	}
	for index, slot := range slots {
		results[slot] = ordered[index].draft
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
				prunes[parent].draft.cleanup.ResolvedTarget,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"compare stale prunes %s and %s: %w",
					placementLabel(
						prunes[child].draft.cleanup.ModuleID,
						prunes[child].draft.cleanup.PlacementID,
					),
					placementLabel(
						prunes[parent].draft.cleanup.ModuleID,
						prunes[parent].draft.cleanup.PlacementID,
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
	key state.Key,
	record state.Record,
) (Action, *Problem, error) {
	switch record.Kind {
	case state.KindLink, state.KindLocal:
	default:
		return Action{}, nil, fmt.Errorf(
			"%w: stale placement %s has unsupported kind %q",
			state.ErrInvalid,
			placementLabel(key.ModuleID, key.PlacementID),
			record.Kind,
		)
	}

	base := Action{
		ModuleID:                key.ModuleID,
		PlacementID:             key.PlacementID,
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
				return Action{}, nil, fmt.Errorf(
					"resolve stale local %s: %w",
					placementLabel(key.ModuleID, key.PlacementID),
					err,
				)
			}
		} else {
			overlaps, overlapErr := controls.TargetOverlaps(current)
			if overlapErr != nil {
				return Action{}, nil, overlapErr
			}
			if overlaps {
				base.Decision = DecisionForget
				base.Reason = "stale target overlaps a protected control path"
				return base, nil, nil
			}
		}
		base.Decision = DecisionForget
		base.Reason = "local left desired; local targets are never pruned"
		return base, nil, nil
	}

	current, err := resolveStateTarget(home, record.Target)
	if err != nil {
		if !isSafeStaleResolutionDrift(err) {
			return Action{}, nil, fmt.Errorf(
				"resolve stale link %s: %w",
				placementLabel(key.ModuleID, key.PlacementID),
				err,
			)
		}
		base.Decision = DecisionForget
		base.Reason = "stale target cannot be resolved safely"
		return base, nil, nil
	}
	base.ResolvedTarget = current.Resolved()
	overlaps, err := controls.TargetOverlaps(current)
	if err != nil {
		return Action{}, nil, err
	}
	if overlaps {
		base.Decision = DecisionForget
		base.Reason = "stale target overlaps a protected control path"
		return base, nil, nil
	}

	if targetUsedByDesired(current, desired) {
		base.Decision = DecisionForget
		base.Reason = "stale target is reused by desired configuration"
		return base, nil, nil
	}
	if current.Resolved() != record.ResolvedTarget {
		base.Decision = DecisionForget
		base.Reason = "stale resolved target changed"
		return base, nil, nil
	}

	actual, err := observeLink(current.Lexical())
	if err != nil {
		return Action{}, nil, fmt.Errorf(
			"inspect stale link %s: %w",
			placementLabel(key.ModuleID, key.PlacementID),
			err,
		)
	}
	if actual.kind == actualAbsent {
		base.Decision = DecisionForget
		base.Reason = "stale target is absent"
		return base, nil, nil
	}
	if stateOwnsLink(record, current, actual) {
		base.Decision = DecisionPrune
		return base, nil, nil
	}

	base.Decision = DecisionForget
	base.Reason = staleDriftReason(actual, current.Resolved(), record)
	return base, nil, nil
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
		ModuleID:        desired.key.ModuleID,
		PlacementID:     desired.key.PlacementID,
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
) (state.Key, bool) {
	for _, key := range stateKeys(snapshot) {
		if key.ModuleID == desired.key.ModuleID {
			continue
		}
		record, _ := statePlacement(snapshot, key)
		if record.Kind == state.KindLink && samePlacementTarget(desired, record) {
			return key, true
		}
	}
	return state.Key{}, false
}

func stateOwnedParentLink(
	home string,
	target corepaths.Target,
	snapshot state.Snapshot,
) (state.Key, bool, error) {
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
			return state.Key{}, false, fmt.Errorf(
				"compare target with state-owned link %s: %w",
				placementLabel(key.ModuleID, key.PlacementID),
				err,
			)
		}
		if !traverses {
			continue
		}

		owned, err := stateRecordOwnsLink(home, record)
		if err != nil {
			return state.Key{}, false, fmt.Errorf(
				"inspect state-owned link %s: %w",
				placementLabel(key.ModuleID, key.PlacementID),
				err,
			)
		}
		if owned {
			return key, true, nil
		}
	}
	return state.Key{}, false, nil
}

func activeLinkNeedsProspectiveGuard(
	home string,
	action Action,
	snapshot state.Snapshot,
) (bool, error) {
	if action.Decision != DecisionKeep {
		return true, nil
	}
	record, exists := statePlacement(snapshot, state.Key{
		ModuleID:    action.ModuleID,
		PlacementID: action.PlacementID,
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
) (state.Key, bool, error) {
	for _, placement := range desired {
		traverses, err := corepaths.TargetParentTraversesLink(
			placement.target,
			linkEntry,
		)
		if err != nil {
			return state.Key{}, false, err
		}
		if traverses {
			return placement.key, true, nil
		}
	}
	return state.Key{}, false, nil
}

func updatedParentLink(
	target corepaths.Target,
	active []Action,
) (state.Key, bool, error) {
	for _, action := range active {
		if action.Decision != DecisionUpdate || action.Kind != state.KindLink {
			continue
		}
		traverses, err := corepaths.TargetParentTraversesLink(
			target,
			action.ResolvedTarget,
		)
		if err != nil {
			return state.Key{}, false, err
		}
		if traverses {
			return state.Key{
				ModuleID:    action.ModuleID,
				PlacementID: action.PlacementID,
			}, true, nil
		}
	}
	return state.Key{}, false, nil
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
	key state.Key,
) (state.Record, bool) {
	record, exists := snapshot.Records[key]
	return record, exists
}

func stateKeys(snapshot state.Snapshot) []state.Key {
	keys := make([]state.Key, 0, len(snapshot.Records))
	for key := range snapshot.Records {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(left, right state.Key) int {
		if byModule := strings.Compare(left.ModuleID, right.ModuleID); byModule != 0 {
			return byModule
		}
		return strings.Compare(left.PlacementID, right.PlacementID)
	})
	return keys
}

func placementLabel(moduleID, placementID string) string {
	return moduleID + "/" + placementID
}
