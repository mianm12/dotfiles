package cli

import (
	"errors"
	"fmt"
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
)

const initMissingStateWarning = "state is missing; this is expected on first init; " +
	"if this HOME was previously managed by dot, links removed from desired configuration " +
	"cannot be discovered"

// SelectionDeltaKind identifies one prospective machine-selection change.
type SelectionDeltaKind string

const (
	// SelectionDeltaNone means the current selection is unchanged.
	SelectionDeltaNone SelectionDeltaKind = "none"
	// SelectionDeltaCreate means init would create the machine selection.
	SelectionDeltaCreate SelectionDeltaKind = "create"
	// SelectionDeltaAddExtra means apply would add one extra module.
	SelectionDeltaAddExtra SelectionDeltaKind = "add-extra"
	// SelectionDeltaRemoveExtra means remove would delete one extra module.
	SelectionDeltaRemoveExtra SelectionDeltaKind = "remove-extra"
)

// SelectionDelta describes the requested change to machine selection.
type SelectionDelta struct {
	Kind     SelectionDeltaKind
	ModuleID string
}

// Changes reports whether publishing the prospective selection is required.
func (delta SelectionDelta) Changes() bool {
	return delta.Kind != SelectionDeltaNone
}

// AnalysisBlocker is a complete, non-placement reason an operation cannot run.
type AnalysisBlocker struct {
	ModuleID string
	Reason   string
}

// ModuleAnalysis is the orthogonal status projection for one inventory module.
type ModuleAnalysis struct {
	ID            string
	Summary       string
	Selection     string
	Applicability string
	Convergence   string
	Variant       string
	Reason        string
}

// OperationAnalysis is a complete read-only projection of one CLI operation.
// It is never accepted as executor input.
type OperationAnalysis struct {
	ProspectiveMachine config.Machine
	SelectionDelta     SelectionDelta
	Modules            []ModuleAnalysis
	Actions            []planner.Action
	// Warnings contains input diagnostics, never action outcomes.
	Warnings []string
	Blockers []AnalysisBlocker

	resolvedModules []config.Module
	scope           []string
	loaded          state.Loaded
}

type selectionSource struct {
	profile bool
	extra   bool
}

func (source selectionSource) String() string {
	switch {
	case source.profile && source.extra:
		return "profile+extra"
	case source.profile:
		return "profile"
	case source.extra:
		return "extra"
	default:
		return "none"
	}
}

type moduleObservation struct {
	loaded        bool
	applicability config.ModuleApplicability
	variant       string
}

type analysisInputs struct {
	repository config.Repository
	loaded     state.Loaded
	blockers   []AnalysisBlocker
}

func analyzeInit(
	context commandContext,
	prospective config.Machine,
) (OperationAnalysis, error) {
	prospective = cloneMachine(prospective)
	blockers := make([]AnalysisBlocker, 0, 1)
	_, exists, err := config.LoadMachine(context.configPath)
	if err != nil {
		return OperationAnalysis{}, err
	}
	if exists {
		blockers = append(blockers, AnalysisBlocker{
			Reason: fmt.Sprintf(
				"machine is already initialized at %q",
				context.configPath,
			),
		})
	}

	inputs, err := loadAnalysisInputs(context, prospective)
	if err != nil {
		return OperationAnalysis{}, err
	}
	blockers = append(blockers, inputs.blockers...)
	resolvedModules, sources, observations, selectionBlockers, err := resolveSelection(
		inputs.repository,
		prospective,
		context.platform,
		nil,
		false,
	)
	if err != nil {
		return OperationAnalysis{}, err
	}
	blockers = append(blockers, selectionBlockers...)
	analysis, err := buildMutationAnalysis(
		context,
		prospective,
		SelectionDelta{Kind: SelectionDeltaCreate},
		nil,
		inputs.loaded,
		resolvedModules,
		sources,
		observations,
		blockers,
	)
	if err != nil {
		return OperationAnalysis{}, err
	}
	if inputs.loaded.Missing {
		analysis.Warnings = []string{initMissingStateWarning}
	}
	return analysis, nil
}

func analyzeApply(
	context commandContext,
	current config.Machine,
	moduleID *string,
) (OperationAnalysis, error) {
	prospective := cloneMachine(current)
	inputs, err := loadAnalysisInputs(context, prospective)
	if err != nil {
		return OperationAnalysis{}, err
	}

	delta := SelectionDelta{Kind: SelectionDeltaNone}
	required := make(map[string]bool)
	var plannerScope []string
	if moduleID != nil {
		requested := *moduleID
		profileModules, profileErr := inputs.repository.ProfileModules(prospective.Profiles)
		if profileErr != nil {
			return OperationAnalysis{}, profileErr
		}
		if !slices.Contains(profileModules, requested) &&
			!slices.Contains(prospective.ExtraModules, requested) {
			prospective.ExtraModules = append(
				append([]string(nil), prospective.ExtraModules...),
				requested,
			)
			slices.Sort(prospective.ExtraModules)
			delta = SelectionDelta{
				Kind:     SelectionDeltaAddExtra,
				ModuleID: requested,
			}
		}
		required[requested] = true
		plannerScope = []string{requested}
	}

	resolvedModules, sources, observations, selectionBlockers, err := resolveSelection(
		inputs.repository,
		prospective,
		context.platform,
		required,
		false,
	)
	if err != nil {
		return OperationAnalysis{}, err
	}
	blockers := append(
		append([]AnalysisBlocker(nil), inputs.blockers...),
		selectionBlockers...,
	)
	return buildMutationAnalysis(
		context,
		prospective,
		delta,
		plannerScope,
		inputs.loaded,
		resolvedModules,
		sources,
		observations,
		blockers,
	)
}

func analyzeRemove(
	context commandContext,
	current config.Machine,
	moduleID string,
) (OperationAnalysis, error) {
	prospective := cloneMachine(current)
	inputs, err := loadAnalysisInputs(context, prospective)
	if err != nil {
		return OperationAnalysis{}, err
	}
	profileModules, err := inputs.repository.ProfileModules(prospective.Profiles)
	if err != nil {
		return OperationAnalysis{}, err
	}

	blockers := append([]AnalysisBlocker(nil), inputs.blockers...)
	profileSelected := slices.Contains(profileModules, moduleID)
	knownAsExtra := slices.Contains(prospective.ExtraModules, moduleID)
	_, knownInState := inputs.loaded.Snapshot.Modules[moduleID]
	module, exists, applicability, err := inputs.repository.InspectModule(
		moduleID,
		context.platform,
	)
	if err != nil {
		return OperationAnalysis{}, err
	}
	targetObservation := moduleObservation{
		loaded:        exists,
		applicability: applicability,
	}
	if applicability.State == config.ApplicabilityApplicable {
		targetObservation.variant = module.Variant
	}

	switch {
	case profileSelected && !knownAsExtra:
		blockers = append(blockers, AnalysisBlocker{
			ModuleID: moduleID,
			Reason: fmt.Sprintf(
				"module %q is selected by an active profile; remove it from the repository profile first",
				moduleID,
			),
		})
	case knownAsExtra && !profileSelected &&
		applicability.State == config.ApplicabilityNotApplicable:
		blockers = append(blockers, AnalysisBlocker{
			ModuleID: moduleID,
			Reason:   fmt.Sprintf("module %q is not applicable", moduleID),
		})
	case knownAsExtra && !profileSelected &&
		applicability.State == config.ApplicabilityIndeterminate:
		blockers = append(blockers, AnalysisBlocker{
			ModuleID: moduleID,
			Reason: fmt.Sprintf(
				"module %q applicability is indeterminate: %s",
				moduleID,
				applicability.Diagnostic,
			),
		})
	case !exists && !knownAsExtra && !knownInState:
		blockers = append(blockers, AnalysisBlocker{
			ModuleID: moduleID,
			Reason:   fmt.Sprintf("unknown module %q", moduleID),
		})
	}

	delta := SelectionDelta{Kind: SelectionDeltaNone}
	if knownAsExtra {
		prospective.ExtraModules = slices.DeleteFunc(
			append([]string(nil), prospective.ExtraModules...),
			func(candidate string) bool { return candidate == moduleID },
		)
		delta = SelectionDelta{
			Kind:     SelectionDeltaRemoveExtra,
			ModuleID: moduleID,
		}
	}

	resolvedModules, sources, observations, selectionBlockers, err := resolveSelection(
		inputs.repository,
		prospective,
		context.platform,
		nil,
		true,
	)
	if err != nil {
		return OperationAnalysis{}, err
	}
	if targetObservation.loaded {
		observations[moduleID] = targetObservation
	}
	blockers = append(blockers, selectionBlockers...)
	return buildMutationAnalysis(
		context,
		prospective,
		delta,
		[]string{moduleID},
		inputs.loaded,
		resolvedModules,
		sources,
		observations,
		blockers,
	)
}

func analyzeStatus(
	context commandContext,
	current config.Machine,
	moduleID *string,
) (OperationAnalysis, error) {
	current = cloneMachine(current)
	inputs, err := loadAnalysisInputs(context, current)
	if err != nil {
		return OperationAnalysis{}, err
	}
	resolvedModules, sources, observations, selectionBlockers, err := resolveSelection(
		inputs.repository,
		current,
		context.platform,
		nil,
		false,
	)
	if err != nil {
		return OperationAnalysis{}, err
	}
	blockers := append(
		append([]AnalysisBlocker(nil), inputs.blockers...),
		selectionBlockers...,
	)
	ids := statusModuleIDs(
		moduleID,
		inputs.repository,
		current,
		inputs.loaded.Snapshot,
	)
	if moduleID != nil {
		requested := *moduleID
		if _, inspected := observations[requested]; !inspected {
			module, exists, applicability, inspectErr := inputs.repository.InspectModule(
				requested,
				context.platform,
			)
			if inspectErr != nil {
				return OperationAnalysis{}, inspectErr
			}
			if exists {
				observations[requested] = moduleObservation{
					loaded:        true,
					applicability: applicability,
					variant:       module.Variant,
				}
			} else if _, stateKnown := inputs.loaded.Snapshot.Modules[requested]; !stateKnown &&
				sources[requested] == (selectionSource{}) {
				blockers = append(blockers, AnalysisBlocker{
					ModuleID: requested,
					Reason:   fmt.Sprintf("unknown module %q", requested),
				})
			}
		}
	}

	actions, inputWarnings, planned, err := buildStatusActions(
		context,
		current,
		resolvedModules,
		inputs.loaded.Snapshot,
		ids,
		blockers,
	)
	if err != nil {
		return OperationAnalysis{}, err
	}
	return newOperationAnalysis(
		current,
		SelectionDelta{Kind: SelectionDeltaNone},
		nil,
		inputs.loaded,
		resolvedModules,
		sources,
		observations,
		actions,
		appendWarning(inputs.loaded.Warning, inputWarnings),
		blockers,
		ids,
		planned,
	), nil
}

func loadAnalysisInputs(
	context commandContext,
	machine config.Machine,
) (analysisInputs, error) {
	blockers := make([]AnalysisBlocker, 0, 1)
	if err := validateOperationControls(context, machine); err != nil {
		if !isPathAnalysisBlocker(err) {
			return analysisInputs{}, err
		}
		blockers = append(blockers, AnalysisBlocker{Reason: err.Error()})
	}
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return analysisInputs{}, err
	}
	loaded, err := state.Load(context.statePath, context.home)
	if err != nil {
		return analysisInputs{}, err
	}
	return analysisInputs{
		repository: repository,
		loaded:     loaded,
		blockers:   blockers,
	}, nil
}

func resolveSelection(
	repository config.Repository,
	machine config.Machine,
	platform config.Platform,
	required map[string]bool,
	ignoreMissingExtras bool,
) (
	[]config.Module,
	map[string]selectionSource,
	map[string]moduleObservation,
	[]AnalysisBlocker,
	error,
) {
	profileModules, err := repository.ProfileModules(machine.Profiles)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	sources := make(map[string]selectionSource)
	for _, moduleID := range profileModules {
		source := sources[moduleID]
		source.profile = true
		sources[moduleID] = source
	}
	for _, moduleID := range machine.ExtraModules {
		source := sources[moduleID]
		source.extra = true
		sources[moduleID] = source
	}
	for moduleID := range required {
		if _, exists := sources[moduleID]; !exists {
			sources[moduleID] = selectionSource{}
		}
	}

	ids := sortedAnalysisKeys(sources)
	resolvedModules := make([]config.Module, 0, len(ids))
	observations := make(map[string]moduleObservation, len(ids))
	blockers := make([]AnalysisBlocker, 0)
	for _, moduleID := range ids {
		module, exists, applicability, inspectErr := repository.InspectModule(
			moduleID,
			platform,
		)
		if inspectErr != nil {
			return nil, nil, nil, nil, inspectErr
		}
		source := sources[moduleID]
		if !exists {
			if ignoreMissingExtras && source.extra && !required[moduleID] {
				continue
			}
			blockers = append(blockers, AnalysisBlocker{
				ModuleID: moduleID,
				Reason:   fmt.Sprintf("required module %q does not exist", moduleID),
			})
			continue
		}
		observations[moduleID] = moduleObservation{
			loaded:        true,
			applicability: applicability,
			variant:       module.Variant,
		}
		switch applicability.State {
		case config.ApplicabilityApplicable:
			resolvedModules = append(resolvedModules, module)
		case config.ApplicabilityNotApplicable:
			if source.extra || required[moduleID] {
				blockers = append(blockers, AnalysisBlocker{
					ModuleID: moduleID,
					Reason:   fmt.Sprintf("module %q is not applicable", moduleID),
				})
			}
		case config.ApplicabilityIndeterminate:
			blockers = append(blockers, AnalysisBlocker{
				ModuleID: moduleID,
				Reason: fmt.Sprintf(
					"module %q applicability is indeterminate: %s",
					moduleID,
					applicability.Diagnostic,
				),
			})
		default:
			return nil, nil, nil, nil, fmt.Errorf(
				"module %q returned invalid applicability %q",
				moduleID,
				applicability.State,
			)
		}
	}
	return resolvedModules, sources, observations, blockers, nil
}

func buildMutationAnalysis(
	context commandContext,
	machine config.Machine,
	delta SelectionDelta,
	scope []string,
	loaded state.Loaded,
	resolvedModules []config.Module,
	sources map[string]selectionSource,
	observations map[string]moduleObservation,
	blockers []AnalysisBlocker,
) (OperationAnalysis, error) {
	actions := make([]planner.Action, 0)
	planningComplete := false
	if len(blockers) == 0 {
		plan, err := planner.Build(planner.Request{
			Home:     context.home,
			Controls: context.controls(machine.Repository),
			Modules:  resolvedModules,
			Scope:    scope,
			State:    loaded.Snapshot,
		})
		if err != nil {
			if !isPathAnalysisBlocker(err) {
				return OperationAnalysis{}, err
			}
			blockers = append(blockers, AnalysisBlocker{Reason: err.Error()})
		} else {
			actions = append(actions, plan.Actions...)
			planningComplete = true
		}
	}
	ids := analysisModuleIDs(sources, loaded.Snapshot, blockers, scope)
	planned := make(map[string]bool)
	if planningComplete {
		if len(scope) == 0 {
			for _, moduleID := range ids {
				planned[moduleID] = true
			}
		} else {
			for _, moduleID := range scope {
				planned[moduleID] = true
			}
		}
	}
	return newOperationAnalysis(
		machine,
		delta,
		scope,
		loaded,
		resolvedModules,
		sources,
		observations,
		actions,
		appendWarning(loaded.Warning, nil),
		blockers,
		ids,
		planned,
	), nil
}

func buildStatusActions(
	context commandContext,
	machine config.Machine,
	resolvedModules []config.Module,
	snapshot state.Snapshot,
	moduleIDs []string,
	blockers []AnalysisBlocker,
) ([]planner.Action, []string, map[string]bool, error) {
	blocked := make(map[string]bool)
	globalBlocker := false
	for _, blocker := range blockers {
		if blocker.ModuleID == "" {
			globalBlocker = true
			continue
		}
		blocked[blocker.ModuleID] = true
	}
	if globalBlocker {
		return []planner.Action{}, []string{}, map[string]bool{}, nil
	}

	actions := make([]planner.Action, 0)
	warnings := make([]string, 0)
	planned := make(map[string]bool)
	for _, moduleID := range moduleIDs {
		if blocked[moduleID] {
			continue
		}
		scoped, err := planner.Build(planner.Request{
			Home:     context.home,
			Controls: context.controls(machine.Repository),
			Modules:  resolvedModules,
			Scope:    []string{moduleID},
			State:    snapshot,
		})
		if err != nil {
			if !isPathAnalysisBlocker(err) {
				return nil, nil, nil, err
			}
			actions = append(actions, planner.Action{
				ModuleID: moduleID,
				Decision: planner.DecisionConflict,
				Reason:   err.Error(),
			})
			if errors.Is(err, corepaths.ErrControlBoundary) {
				warnings = append(warnings, err.Error())
			}
			planned[moduleID] = true
			continue
		}
		actions = append(actions, scoped.Actions...)
		planned[moduleID] = true
	}
	return actions, warnings, planned, nil
}

func newOperationAnalysis(
	machine config.Machine,
	delta SelectionDelta,
	scope []string,
	loaded state.Loaded,
	resolvedModules []config.Module,
	sources map[string]selectionSource,
	observations map[string]moduleObservation,
	actions []planner.Action,
	warnings []string,
	blockers []AnalysisBlocker,
	moduleIDs []string,
	planned map[string]bool,
) OperationAnalysis {
	return OperationAnalysis{
		ProspectiveMachine: cloneMachine(machine),
		SelectionDelta:     delta,
		Modules: buildModuleAnalyses(
			moduleIDs,
			sources,
			observations,
			loaded.Snapshot,
			actions,
			blockers,
			planned,
		),
		Actions:         append([]planner.Action(nil), actions...),
		Warnings:        append([]string(nil), warnings...),
		Blockers:        append([]AnalysisBlocker(nil), blockers...),
		resolvedModules: append([]config.Module(nil), resolvedModules...),
		scope:           append([]string(nil), scope...),
		loaded:          loaded,
	}
}

func buildModuleAnalyses(
	moduleIDs []string,
	sources map[string]selectionSource,
	observations map[string]moduleObservation,
	snapshot state.Snapshot,
	actions []planner.Action,
	blockers []AnalysisBlocker,
	planned map[string]bool,
) []ModuleAnalysis {
	result := make([]ModuleAnalysis, 0, len(moduleIDs))
	for _, moduleID := range moduleIDs {
		source := sources[moduleID]
		observation := observations[moduleID]
		_, statePresent := snapshot.Modules[moduleID]
		blockerReason, blocked := analysisBlockerForModule(
			moduleID,
			source != (selectionSource{}) || statePresent,
			blockers,
		)
		analysis := ModuleAnalysis{
			ID:            moduleID,
			Summary:       "inactive",
			Selection:     source.String(),
			Applicability: "-",
			Convergence:   "-",
			Variant:       "-",
			Reason:        "",
		}
		if observation.loaded {
			analysis.Applicability = string(observation.applicability.State)
			if observation.applicability.State == config.ApplicabilityApplicable {
				analysis.Variant = observation.variant
				if analysis.Variant == "" {
					analysis.Variant = "portable"
				}
			}
		}

		switch {
		case observation.loaded &&
			observation.applicability.State == config.ApplicabilityNotApplicable &&
			source != (selectionSource{}):
			analysis.Summary = "not-applicable"
		case source != (selectionSource{}):
			if planned[moduleID] {
				analysis.Summary = "converged"
				analysis.Convergence = "converged"
			} else if blocked {
				analysis.Summary = "conflict"
			} else {
				analysis.Summary = "pending"
			}
		case statePresent:
			if planned[moduleID] {
				analysis.Summary = "stale"
				analysis.Convergence = "pending"
			} else if blocked {
				analysis.Summary = "conflict"
			} else {
				analysis.Summary = "stale"
			}
		}

		for _, action := range actions {
			if action.ModuleID != moduleID {
				continue
			}
			if action.Decision == planner.DecisionConflict {
				analysis.Summary = "conflict"
				analysis.Convergence = "conflict"
				if !isConcretePlacementConflict(action) {
					analysis.Reason = action.Reason
				}
				continue
			}
			if analysis.Convergence == "conflict" {
				continue
			}
			if source != (selectionSource{}) &&
				observation.applicability.State == config.ApplicabilityApplicable &&
				action.Decision == planner.DecisionKeep &&
				keepStateRecorded(snapshot, action) {
				continue
			}
			if analysis.Summary == "not-applicable" {
				analysis.Convergence = "pending-cleanup"
			} else {
				analysis.Convergence = "pending"
			}
			if analysis.Summary != "stale" &&
				analysis.Summary != "not-applicable" {
				analysis.Summary = "pending"
			}
			if analysis.Reason == "" && action.Reason != "" {
				analysis.Reason = action.Reason
			} else if analysis.Reason == "" &&
				analysis.Summary == "not-applicable" {
				analysis.Reason = string(action.Decision)
			}
		}
		if analysis.Reason == "" && blocked {
			analysis.Reason = blockerReason
		}
		if analysis.Reason == "" &&
			observation.applicability.State == config.ApplicabilityIndeterminate {
			analysis.Reason = observation.applicability.Diagnostic
		}
		result = append(result, analysis)
	}
	return result
}

func isConcretePlacementConflict(action planner.Action) bool {
	return action.Decision == planner.DecisionConflict &&
		action.PlacementID != "" &&
		action.Target != ""
}

func analysisBlockerForModule(
	moduleID string,
	includeGlobal bool,
	blockers []AnalysisBlocker,
) (string, bool) {
	for _, blocker := range blockers {
		if blocker.ModuleID == moduleID {
			return blocker.Reason, true
		}
	}
	if includeGlobal {
		for _, blocker := range blockers {
			if blocker.ModuleID == "" {
				return blocker.Reason, true
			}
		}
	}
	return "", false
}

func analysisModuleIDs(
	sources map[string]selectionSource,
	snapshot state.Snapshot,
	blockers []AnalysisBlocker,
	scope []string,
) []string {
	set := make(map[string]bool)
	for _, blocker := range blockers {
		if blocker.ModuleID != "" {
			set[blocker.ModuleID] = true
		}
	}
	if len(scope) != 0 {
		for _, moduleID := range scope {
			set[moduleID] = true
		}
		return sortedAnalysisSet(set)
	}
	for moduleID := range sources {
		set[moduleID] = true
	}
	for moduleID := range snapshot.Modules {
		set[moduleID] = true
	}
	return sortedAnalysisSet(set)
}

func sortedAnalysisKeys(values map[string]selectionSource) []string {
	set := make(map[string]bool, len(values))
	for value := range values {
		set[value] = true
	}
	return sortedAnalysisSet(set)
}

func sortedAnalysisSet(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func isPathAnalysisBlocker(err error) bool {
	return errors.Is(err, corepaths.ErrTargetConflict) ||
		errors.Is(err, corepaths.ErrControlBoundary) ||
		errors.Is(err, corepaths.ErrControlTopology)
}

func cloneMachine(machine config.Machine) config.Machine {
	machine.Profiles = append([]string(nil), machine.Profiles...)
	machine.ExtraModules = append([]string(nil), machine.ExtraModules...)
	return machine
}
