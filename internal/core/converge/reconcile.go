package converge

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/state"
)

// reconcile is the pure decision pass. Every filesystem and path-traversal
// fact it consumes was captured by observeReconcileInput.
func reconcile(input reconcileInput) Plan {
	usedRecords := recordUsage(input.desired, input.state)
	actions := make([]Action, 0, len(input.desired)+len(input.state.Links))
	issues := make([]Issue, 0)

	for _, desired := range input.desired {
		action, issue := reconcileDesired(input, usedRecords, desired)
		if action != nil {
			actions = append(actions, *action)
		}
		if issue != nil {
			issues = append(issues, *issue)
		}
	}
	for _, key := range stateKeys(input.state) {
		if usedRecords[key] {
			continue
		}
		action, warning := reconcileStale(input, usedRecords, key)
		actions = append(actions, action)
		if warning != nil {
			issues = append(issues, *warning)
		}
	}

	sortActions(actions)
	sortIssues(issues)
	plan := Plan{Actions: actions, Issues: issues}
	if plan.Executable() {
		plan.nextState = nextStateForDesired(input.state.Home, input.desired)
	}
	return plan
}

func reconcileDesired(
	input reconcileInput,
	usedRecords map[state.Key]bool,
	desired desiredPlacement,
) (*Action, *Issue) {
	record, hasRecord := input.state.Links[desired.key]
	base := actionForDesired(desired)
	if desired.kind == placementLocal && hasRecord {
		base.Reason = "existing link ownership conflicts with desired local"
		return nil, issueForAction(base, IssueCodePlacementTypeChange)
	}

	if owner, exists := crossModuleOwner(input.state, desired); exists {
		base.Reason = fmt.Sprintf(
			"target is owned by module %q placement %q",
			owner.ModuleID,
			owner.PlacementID,
		)
		return nil, issueForAction(base, IssueCodeOwnershipConflict)
	}
	if owner, exists := managedParentOwner(input, desired); exists {
		base.Reason = fmt.Sprintf(
			"target traverses state-owned link from module %q placement %q",
			owner.ModuleID,
			owner.PlacementID,
		)
		return nil, issueForAction(base, IssueCodeTopologyConflict)
	}

	if hasRecord && !usedRecords[desired.key] {
		from := targetRef{role: targetDesired, key: desired.key}
		to := targetRef{role: targetState, key: desired.key}
		if relation, related := describeComplexRelation(input, from, to); related {
			base.Reason = "desired target move " + relation + " its previous target"
			return nil, issueForAction(base, IssueCodeTopologyConflict)
		}
	}

	observed := input.active[desired.key]
	action, issue := decideActive(desired, observed, record, hasRecord && usedRecords[desired.key])
	if issue != nil || action == nil {
		return action, issue
	}
	if action.Decision != DecisionAdopt &&
		action.Decision != DecisionRepairState &&
		action.Decision != DecisionUpdate {
		return action, nil
	}
	if dependent, exists := namespaceDependent(input, usedRecords, desired.key); exists {
		action.Reason = fmt.Sprintf(
			"active link cannot be owned or changed while traversed by %s",
			targetRefLabel(dependent),
		)
		return nil, issueForAction(*action, IssueCodeTopologyConflict)
	}
	return action, nil
}

func decideActive(
	desired desiredPlacement,
	observed activeObservation,
	record state.LinkRecord,
	hasState bool,
) (*Action, *Issue) {
	base := actionForDesired(desired)
	if desired.kind == placementLocal {
		if observed.localExists {
			return nil, nil
		}
		base.Decision = DecisionCreateLocal
		return &base, nil
	}

	actual := observed.actual
	if actual.kind != actualAbsent && actual.kind != actualSymlink {
		base.Reason = fmt.Sprintf("actual target is %s", actual.kind)
		return nil, issueForAction(base, IssueCodeTargetConflict)
	}
	if actual.kind == actualAbsent {
		base.Decision = DecisionCreateLink
		return &base, nil
	}
	if actual.linkDestination == base.LinkDestination {
		switch {
		case !hasState:
			base.Decision = DecisionAdopt
			return &base, nil
		case record == recordForDesired(desired):
			return nil, nil
		default:
			base.Decision = DecisionRepairState
			return &base, nil
		}
	}
	if !hasState || actual.linkDestination != record.LinkDestination {
		base.Reason = "actual symlink is not explained by desired or state"
		return nil, issueForAction(base, IssueCodeTargetConflict)
	}
	if base.ResolvedTarget != record.ResolvedTarget {
		base.Reason = "resolved target changed since state was recorded"
		return nil, issueForAction(base, IssueCodeTopologyConflict)
	}

	base.Decision = DecisionUpdate
	base.ExpectedResolvedTarget = record.ResolvedTarget
	base.ExpectedLinkDestination = record.LinkDestination
	return &base, nil
}

func reconcileStale(
	input reconcileInput,
	usedRecords map[state.Key]bool,
	key state.Key,
) (Action, *Issue) {
	record := input.state.Links[key]
	base := Action{
		ModuleID:                key.ModuleID,
		PlacementID:             key.PlacementID,
		Target:                  record.Target,
		ResolvedTarget:          record.ResolvedTarget,
		LinkDestination:         record.LinkDestination,
		ExpectedResolvedTarget:  record.ResolvedTarget,
		ExpectedLinkDestination: record.LinkDestination,
	}
	observed := input.stateLinks[key]
	if observed.resolution == stateTargetObstructed {
		base.Decision = DecisionForget
		base.Reason = "stale target cannot be resolved safely"
		return base, nil
	}
	base.ResolvedTarget = observed.target.Resolved()
	if observed.actual.kind == actualAbsent {
		base.Decision = DecisionForget
		base.Reason = "stale target is absent"
		return base, nil
	}
	if observed.controlOverlap {
		base.Decision = DecisionForget
		base.Reason = "stale target overlaps a protected control path"
		return base, stalePreservedIssue(base)
	}
	if relation, related := firstStaleRelation(input, usedRecords, key); related {
		base.Decision = DecisionForget
		base.Reason = "stale target " + relation
		return base, stalePreservedIssue(base)
	}
	if observed.target.Resolved() != record.ResolvedTarget {
		base.Decision = DecisionForget
		base.Reason = "stale resolved target changed"
		return base, nil
	}
	if stateLinkOwned(record, observed) {
		base.Decision = DecisionPrune
		return base, nil
	}

	base.Decision = DecisionForget
	base.Reason = staleDriftReason(observed.actual, observed.target.Resolved(), record)
	return base, nil
}

func recordUsage(
	desired []desiredPlacement,
	snapshot state.Snapshot,
) map[state.Key]bool {
	used := make(map[state.Key]bool, len(snapshot.Links))
	for _, placement := range desired {
		record, exists := snapshot.Links[placement.key]
		if !exists {
			continue
		}
		if placement.kind == placementLocal || samePlacementTarget(placement, record) {
			used[placement.key] = true
		}
	}
	return used
}

func crossModuleOwner(
	snapshot state.Snapshot,
	desired desiredPlacement,
) (state.Key, bool) {
	for _, key := range stateKeys(snapshot) {
		if key.ModuleID == desired.key.ModuleID {
			continue
		}
		if samePlacementTarget(desired, snapshot.Links[key]) {
			return key, true
		}
	}
	return state.Key{}, false
}

func managedParentOwner(
	input reconcileInput,
	desired desiredPlacement,
) (state.Key, bool) {
	from := targetRef{role: targetDesired, key: desired.key}
	for _, key := range stateKeys(input.state) {
		observed := input.stateLinks[key]
		if !stateLinkOwned(input.state.Links[key], observed) {
			continue
		}
		facts := input.relations[relationKey{
			from: from,
			to:   targetRef{role: targetState, key: key},
		}]
		if facts.traverses {
			return key, true
		}
	}
	return state.Key{}, false
}

func namespaceDependent(
	input reconcileInput,
	usedRecords map[state.Key]bool,
	activeKey state.Key,
) (targetRef, bool) {
	active := targetRef{role: targetDesired, key: activeKey}
	for _, placement := range input.desired {
		if placement.key == activeKey {
			continue
		}
		candidate := targetRef{role: targetDesired, key: placement.key}
		if input.relations[relationKey{from: candidate, to: active}].traverses {
			return candidate, true
		}
	}
	for _, key := range stateKeys(input.state) {
		if usedRecords[key] {
			continue
		}
		candidate := targetRef{role: targetState, key: key}
		if input.relations[relationKey{from: candidate, to: active}].traverses {
			return candidate, true
		}
	}
	return targetRef{}, false
}

func firstStaleRelation(
	input reconcileInput,
	usedRecords map[state.Key]bool,
	key state.Key,
) (string, bool) {
	stale := targetRef{role: targetState, key: key}
	for _, placement := range input.desired {
		other := targetRef{role: targetDesired, key: placement.key}
		if relation, related := describeComplexRelation(input, stale, other); related {
			return relation + " " + targetRefLabel(other), true
		}
	}
	for _, otherKey := range stateKeys(input.state) {
		if otherKey == key || usedRecords[otherKey] {
			continue
		}
		other := targetRef{role: targetState, key: otherKey}
		if relation, related := describeComplexRelation(input, stale, other); related {
			return relation + " " + targetRefLabel(other), true
		}
	}
	return "", false
}

func describeComplexRelation(
	input reconcileInput,
	from targetRef,
	to targetRef,
) (string, bool) {
	forward := input.relations[relationKey{from: from, to: to}]
	reverse := input.relations[relationKey{from: to, to: from}]
	switch {
	case forward.equal:
		return "shares lexical or resolved identity with", true
	case forward.contains:
		return "contains", true
	case reverse.contains:
		return "is contained by", true
	case forward.traverses:
		return "traverses", true
	case reverse.traverses:
		return "is traversed by", true
	default:
		return "", false
	}
}

func stateLinkOwned(
	record state.LinkRecord,
	observed stateLinkObservation,
) bool {
	return observed.resolution == stateTargetResolved &&
		observed.target.Resolved() == record.ResolvedTarget &&
		observed.actual.kind == actualSymlink &&
		observed.actual.linkDestination == record.LinkDestination
}

func samePlacementTarget(
	desired desiredPlacement,
	record state.LinkRecord,
) bool {
	return desired.target.Lexical() == record.Target ||
		desired.target.Resolved() == record.ResolvedTarget
}

func actionForDesired(desired desiredPlacement) Action {
	return Action{
		ModuleID:        desired.key.ModuleID,
		PlacementID:     desired.key.PlacementID,
		Target:          desired.target.Lexical(),
		ResolvedTarget:  desired.target.Resolved(),
		Source:          desired.source,
		LinkDestination: desired.destination,
	}
}

func recordForDesired(desired desiredPlacement) state.LinkRecord {
	return state.LinkRecord{
		Target:          desired.target.Lexical(),
		ResolvedTarget:  desired.target.Resolved(),
		LinkDestination: desired.destination,
	}
}

func nextStateForDesired(home string, desired []desiredPlacement) state.Snapshot {
	next := state.Snapshot{
		Home:  home,
		Links: make(map[state.Key]state.LinkRecord),
	}
	for _, placement := range desired {
		if placement.kind == placementLink {
			next.Links[placement.key] = recordForDesired(placement)
		}
	}
	return next
}

func issueForAction(action Action, code IssueCode) *Issue {
	return &Issue{
		Severity:    IssueBlocker,
		Code:        code,
		ModuleID:    action.ModuleID,
		PlacementID: action.PlacementID,
		Target:      action.Target,
		Reason:      action.Reason,
		Recovery:    recoveryForIssue(code),
	}
}

func stalePreservedIssue(action Action) *Issue {
	return &Issue{
		Severity:    IssueWarning,
		Code:        IssueCodeStalePreserved,
		ModuleID:    action.ModuleID,
		PlacementID: action.PlacementID,
		Target:      action.Target,
		Reason:      action.Reason + "; actual preserved",
		Recovery:    RecoveryManualMigration,
	}
}

func recoveryForIssue(code IssueCode) Recovery {
	switch code {
	case IssueCodeStateMissing:
		return RecoveryNone
	case IssueCodeControlTopology, IssueCodeControlBoundary:
		return RecoveryPaths
	case IssueCodeSelectionIndeterminate, IssueCodeSelectionNotApplicable:
		return RecoveryNone
	default:
		return RecoveryManualMigration
	}
}

func staleDriftReason(
	actual actual,
	resolved string,
	record state.LinkRecord,
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

func planFromIssues(issues []Issue) Plan {
	result := Plan{Issues: append([]Issue(nil), issues...)}
	sortIssues(result.Issues)
	return result
}

func sortActions(actions []Action) {
	slices.SortStableFunc(actions, func(left, right Action) int {
		if byPhase := actionPhase(left.Decision) - actionPhase(right.Decision); byPhase != 0 {
			return byPhase
		}
		if byModule := strings.Compare(left.ModuleID, right.ModuleID); byModule != 0 {
			return byModule
		}
		if byPlacement := strings.Compare(left.PlacementID, right.PlacementID); byPlacement != 0 {
			return byPlacement
		}
		if byTarget := strings.Compare(left.Target, right.Target); byTarget != 0 {
			return byTarget
		}
		return strings.Compare(string(left.Decision), string(right.Decision))
	})
}

func actionPhase(decision Decision) int {
	switch decision {
	case DecisionCreateLocal, DecisionCreateLink:
		return 0
	case DecisionUpdate:
		return 1
	case DecisionAdopt, DecisionRepairState:
		return 2
	case DecisionPrune, DecisionForget:
		return 3
	default:
		return 4
	}
}

func sortIssues(issues []Issue) {
	slices.SortStableFunc(issues, func(left, right Issue) int {
		if bySeverity := issueSeverityOrder(left.Severity) - issueSeverityOrder(right.Severity); bySeverity != 0 {
			return bySeverity
		}
		if byCode := strings.Compare(string(left.Code), string(right.Code)); byCode != 0 {
			return byCode
		}
		if byModule := strings.Compare(left.ModuleID, right.ModuleID); byModule != 0 {
			return byModule
		}
		if byPlacement := strings.Compare(left.PlacementID, right.PlacementID); byPlacement != 0 {
			return byPlacement
		}
		if byTarget := strings.Compare(left.Target, right.Target); byTarget != 0 {
			return byTarget
		}
		if byReason := strings.Compare(left.Reason, right.Reason); byReason != 0 {
			return byReason
		}
		return strings.Compare(string(left.Recovery), string(right.Recovery))
	})
}

func issueSeverityOrder(severity IssueSeverity) int {
	if severity == IssueWarning {
		return 0
	}
	return 1
}
