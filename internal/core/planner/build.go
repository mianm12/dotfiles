package planner

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

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
	plan := Plan{
		Actions:  make([]Action, 0, len(desired)),
		Warnings: make([]string, 0),
	}
	for _, placement := range desired {
		if !scope.includes(placement.key.moduleID) {
			continue
		}
		action, used, planErr := planActive(placement, request.State)
		if planErr != nil {
			return Plan{}, planErr
		}
		if used {
			usedState[placement.key] = true
		}
		plan.Actions = append(plan.Actions, action)
	}

	cleanup, warnings, err := planStale(
		home,
		desired,
		request.State,
		usedState,
		scope,
	)
	if err != nil {
		return Plan{}, err
	}
	plan.Actions = append(plan.Actions, cleanup...)
	plan.Warnings = append(plan.Warnings, warnings...)
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
				Label:         label,
				Target:        link.Target,
				DirectoryLink: link.SourceMode.IsDir(),
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
	if owner, exists := otherModuleOwner(snapshot, desired); exists {
		base.Decision = DecisionConflict
		base.Reason = fmt.Sprintf(
			"target is owned by module %q placement %q",
			owner.moduleID,
			owner.placementID,
		)
		return base, false, nil
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
	actual, err := observe(desired.target.Lexical())
	if err != nil {
		return Action{}, false, err
	}
	if desired.kind == state.KindLocal {
		return planLocal(base, actual), recordApplies, nil
	}
	return planLink(base, actual, record, recordApplies), recordApplies, nil
}

func planLocal(base Action, actual actual) Action {
	if actual.kind == actualAbsent {
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
	desired []desiredPlacement,
	snapshot state.Snapshot,
	used map[placementKey]bool,
	scope moduleScope,
) ([]Action, []string, error) {
	keys := stateKeys(snapshot)
	actions := make([]Action, 0)
	warnings := make([]string, 0)
	for _, key := range keys {
		if used[key] || !scope.includes(key.moduleID) {
			continue
		}
		record, _ := statePlacement(snapshot, key)
		action, warning, err := planOneStale(home, desired, key, record)
		if err != nil {
			return nil, nil, err
		}
		actions = append(actions, action)
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	return actions, warnings, nil
}

func planOneStale(
	home string,
	desired []desiredPlacement,
	key placementKey,
	record state.Placement,
) (Action, string, error) {
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
		base.Decision = DecisionForget
		warning := fmt.Sprintf(
			"local %s left desired; keeping target and forgetting provenance",
			placementLabel(key.moduleID, key.placementID),
		)
		return base, warning, nil
	}

	current, err := resolveStateTarget(home, record.Target)
	if err != nil {
		if !isSafeStaleResolutionDrift(err) {
			return Action{}, "", fmt.Errorf(
				"resolve stale link %s: %w",
				placementLabel(key.moduleID, key.placementID),
				err,
			)
		}
		base.Decision = DecisionForget
		base.Reason = "stale target cannot be resolved safely"
		return base, staleWarning(key, base.Reason), nil
	}
	base.ResolvedTarget = current.Resolved()

	if targetUsedByDesired(current, desired) {
		base.Decision = DecisionForget
		base.Reason = "stale target is reused by desired configuration"
		return base, staleWarning(key, base.Reason), nil
	}
	if current.Resolved() != record.ResolvedTarget {
		base.Decision = DecisionForget
		base.Reason = "stale resolved target changed"
		return base, staleWarning(key, base.Reason), nil
	}

	actual, err := observe(current.Lexical())
	if err != nil {
		return Action{}, "", fmt.Errorf(
			"inspect stale link %s: %w",
			placementLabel(key.moduleID, key.placementID),
			err,
		)
	}
	if actual.kind == actualAbsent {
		base.Decision = DecisionForget
		return base, "", nil
	}
	if actual.kind == actualSymlink &&
		actual.linkDestination == record.LinkDestination &&
		current.Resolved() == record.ResolvedTarget {
		if staleLinkContainsDesired(record, current, desired) {
			base.Decision = DecisionConflict
			base.Reason = "stale directory link contains an active desired target"
			return base, "", nil
		}
		base.Decision = DecisionPrune
		return base, "", nil
	}

	base.Decision = DecisionForget
	base.Reason = staleDriftReason(actual, current.Resolved(), record)
	return base, staleWarning(key, base.Reason), nil
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
		return target.Lexical() == placement.target.Lexical() ||
			target.Resolved() == placement.target.Resolved()
	})
}

func staleLinkContainsDesired(
	record state.Placement,
	target corepaths.Target,
	desired []desiredPlacement,
) bool {
	return slices.ContainsFunc(desired, func(placement desiredPlacement) bool {
		return strictDescendant(target.Lexical(), placement.target.Lexical()) ||
			strictDescendant(record.LinkDestination, placement.target.Resolved())
	})
}

func strictDescendant(parent, candidate string) bool {
	relative, err := filepath.Rel(parent, candidate)
	return err == nil &&
		relative != "." &&
		relative != ".." &&
		!filepath.IsAbs(relative) &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func resolveStateTarget(home, target string) (corepaths.Target, error) {
	relative, err := filepath.Rel(home, target)
	if err != nil ||
		relative == "." ||
		relative == ".." ||
		filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return corepaths.Target{}, fmt.Errorf("state target %q is outside HOME %q", target, home)
	}
	return corepaths.ResolveTarget(home, "~/"+filepath.ToSlash(relative))
}

func isSafeStaleResolutionDrift(err error) bool {
	if errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, syscall.ENOTDIR) ||
		errors.Is(err, syscall.ELOOP) {
		return true
	}
	if !errors.Is(err, corepaths.ErrPathBlocked) {
		return false
	}
	var pathError *fs.PathError
	return !errors.As(err, &pathError)
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

func staleWarning(key placementKey, reason string) string {
	return fmt.Sprintf(
		"stale link %s is not safe to prune (%s); keeping target and forgetting ownership",
		placementLabel(key.moduleID, key.placementID),
		reason,
	)
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
