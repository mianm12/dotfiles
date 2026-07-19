package planner

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"

	"github.com/mianm12/dotfiles/internal/manifest"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/mianm12/dotfiles/internal/state"
)

// ErrApplyPlan 表示只读 pipeline 无法形成完整、可信的 M1 apply plan。
var ErrApplyPlan = errors.New("apply plan failed")

// ApplyOptions 保存唯一 apply-plan 入口的显式运行与 M1 决策选项。
type ApplyOptions struct {
	Runtime    dotruntime.Overrides
	CLIVersion string
	Modules    []string
	Force      bool
	NoPrune    bool
}

// ApplyContext 保存 presentation/status 所需的稳定运行上下文。
type ApplyContext struct {
	Profile           string
	GOOS              string
	GOARCH            string
	Hostname          string
	Home              string
	Repository        string
	Requirement       string
	DevelopmentBuild  bool
	Full              bool
	Modules           []string
	UnassignedModules []string
	Force             bool
	PruneEnabled      bool
}

// Clone 返回不共享 scope 与 unassigned slice 的副本。
func (context ApplyContext) Clone() ApplyContext {
	context.Modules = append([]string(nil), context.Modules...)
	context.UnassignedModules = append([]string(nil), context.UnassignedModules...)
	return context
}

// ApplyPlan 是完整 observation、file/prune/hook action 与 presentation context 的不可变组合。
// 零值不是可信计划；只有 PlanApply 成功返回的值才 Valid。
type ApplyPlan struct {
	valid       bool
	context     ApplyContext
	observed    ObservedProfile
	fileActions []FileAction
	prune       PrunePlan
	hooks       HookPlan
}

// Valid 报告 plan 是否完整通过组合与结构校验。
func (plan ApplyPlan) Valid() bool { return plan.valid }

// Context 返回不共享 slice 的运行上下文副本。
func (plan ApplyPlan) Context() ApplyContext { return plan.context.Clone() }

// Observed 返回不共享 desired/observation bytes 的完整 profile 快照。
func (plan ApplyPlan) Observed() ObservedProfile { return cloneObservedProfile(plan.observed) }

// FileActions 返回不共享 desired/Precondition bytes 的 scope file action。
func (plan ApplyPlan) FileActions() []FileAction {
	actions := append([]FileAction(nil), plan.fileActions...)
	for index := range actions {
		actions[index] = actions[index].Clone()
	}
	return actions
}

// Prune 返回不共享 action observation 与确认组 slice 的 prune plan。
func (plan ApplyPlan) Prune() PrunePlan { return clonePrunePlan(plan.prune) }

// Hooks 返回不共享 invocation 参数的 hook plan。
func (plan ApplyPlan) Hooks() HookPlan { return cloneHookPlan(plan.hooks) }

// PlanApply 是 M1 apply/diff/dry-run/status 的唯一纯只读组合入口。它复用 runtime strict load，
// 不获取 lock；任一阶段失败都返回零值 plan。
func PlanApply(options ApplyOptions) (ApplyPlan, error) {
	inputs, err := dotruntime.LoadReadOnly(options.Runtime, options.CLIVersion)
	if err != nil {
		return ApplyPlan{}, fmt.Errorf("%w: load runtime: %w", ErrApplyPlan, err)
	}
	plan, err := planApplyInputs(inputs, options)
	if err != nil {
		return ApplyPlan{}, err
	}
	return plan, nil
}

func planApplyInputs(inputs dotruntime.LoadedInputs, options ApplyOptions) (ApplyPlan, error) {
	runContext := inputs.Context()
	control := runContext.Control()
	resolved, err := inputs.Manifest().Resolve(runContext.Profile(), goruntime.GOOS)
	if err != nil {
		return ApplyPlan{}, fmt.Errorf("%w: resolve profile: %w", ErrApplyPlan, err)
	}
	validated, err := resolved.ValidatePathBoundaries(control.Paths())
	if err != nil {
		return ApplyPlan{}, fmt.Errorf("%w: validate complete profile: %w", ErrApplyPlan, err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		return ApplyPlan{}, fmt.Errorf("%w: read hostname: %w", ErrApplyPlan, err)
	}
	renderContext := manifest.RuntimeContext{
		OS:       goruntime.GOOS,
		Arch:     goruntime.GOARCH,
		Hostname: hostname,
		Profile:  runContext.Profile(),
		Home:     control.Home(),
		Data:     runContext.Data(),
	}
	scoped, err := validated.RenderScope(options.Modules, renderContext)
	if err != nil {
		return ApplyPlan{}, fmt.Errorf("%w: render scope: %w", ErrApplyPlan, err)
	}
	observed, fileActions, err := planScopedFiles(
		validated,
		scoped,
		inputs.State(),
		DecisionOptions{Force: options.Force},
	)
	if err != nil {
		return ApplyPlan{}, fmt.Errorf("%w: plan files: %w", ErrApplyPlan, err)
	}
	pruneOptions := PruneOptions{
		Enabled: !options.NoPrune,
		Full:    scoped.Full(),
	}
	if !scoped.Full() {
		pruneOptions.Modules = scoped.Modules()
	}
	prune, err := PlanPrune(observed, fileActions, pruneOptions)
	if err != nil {
		return ApplyPlan{}, fmt.Errorf("%w: plan prune: %w", ErrApplyPlan, err)
	}
	hooks, err := PlanHooks(scoped, inputs.State(), control.RepositoryPath())
	if err != nil {
		return ApplyPlan{}, fmt.Errorf("%w: plan hooks: %w", ErrApplyPlan, err)
	}
	compatibility := inputs.Compatibility()
	plan := ApplyPlan{
		valid: true,
		context: ApplyContext{
			Profile:           runContext.Profile(),
			GOOS:              goruntime.GOOS,
			GOARCH:            goruntime.GOARCH,
			Hostname:          hostname,
			Home:              control.Home(),
			Repository:        control.RepositoryPath(),
			Requirement:       compatibility.Requirement().String(),
			DevelopmentBuild:  compatibility.DevelopmentBuild(),
			Full:              scoped.Full(),
			Modules:           scoped.Modules(),
			UnassignedModules: inputs.Manifest().UnassignedModules(),
			Force:             options.Force,
			PruneEnabled:      !options.NoPrune,
		},
		observed:    cloneObservedProfile(observed),
		fileActions: cloneFileActions(fileActions),
		prune:       clonePrunePlan(prune),
		hooks:       cloneHookPlan(hooks),
	}
	if err := validateApplyPlan(plan); err != nil {
		return ApplyPlan{}, fmt.Errorf("%w: validate combined plan: %w", ErrApplyPlan, err)
	}
	return plan, nil
}

func planScopedFiles(
	validated manifest.ValidatedProfile,
	scoped manifest.ScopedProfile,
	loaded state.Loaded,
	options DecisionOptions,
) (ObservedProfile, []FileAction, error) {
	if err := validateScopedProfile(validated, scoped); err != nil {
		return ObservedProfile{}, nil, err
	}
	entries, err := mergeScopedEntries(validated.Entries(), scoped)
	if err != nil {
		return ObservedProfile{}, nil, err
	}
	observed, err := ObserveProfileTargets(validated.Home(), entries, loaded)
	if err != nil {
		return ObservedProfile{}, nil, fmt.Errorf("observe complete profile: %w", err)
	}

	selected := stringSet(scoped.Modules())
	actions := make([]FileAction, 0, len(scoped.Entries()))
	for _, target := range observed.Targets() {
		if _, ok := selected[target.Desired.Module]; !ok {
			continue
		}
		action, err := Decide(target, options)
		if err != nil {
			return ObservedProfile{}, nil, fmt.Errorf(
				"decide module %q source %q: %w",
				target.Desired.Module,
				target.Desired.Source,
				err,
			)
		}
		actions = append(actions, action)
	}
	return observed, actions, nil
}

func validateScopedProfile(validated manifest.ValidatedProfile, scoped manifest.ScopedProfile) error {
	if validated.Name() == "" || scoped.Name() == "" {
		return fmt.Errorf("apply plan profile is invalid")
	}
	if scoped.Name() != validated.Name() ||
		scoped.GOOS() != validated.GOOS() ||
		scoped.Home() != validated.Home() {
		return fmt.Errorf("apply plan scope does not match validated profile")
	}
	effective := stringSet(validated.Modules())
	for _, module := range scoped.Modules() {
		if _, ok := effective[module]; !ok {
			return fmt.Errorf("apply plan scope contains non-effective module %q", module)
		}
	}
	if scoped.Full() && len(scoped.Modules()) != len(effective) {
		return fmt.Errorf("full apply plan scope omits effective modules")
	}
	if !scoped.Full() && len(scoped.Modules()) == 0 {
		return fmt.Errorf("partial apply plan scope is empty")
	}
	return nil
}

func mergeScopedEntries(
	complete []manifest.DesiredEntry,
	scoped manifest.ScopedProfile,
) ([]manifest.DesiredEntry, error) {
	selected := stringSet(scoped.Modules())
	rendered := make(map[string]manifest.DesiredEntry, len(scoped.Entries()))
	for _, entry := range scoped.Entries() {
		if _, ok := selected[entry.Module]; !ok {
			return nil, fmt.Errorf(
				"scoped desired module %q source %q is outside selected modules",
				entry.Module,
				entry.Source,
			)
		}
		key := desiredEntryKey(entry)
		if _, exists := rendered[key]; exists {
			return nil, fmt.Errorf(
				"scoped desired duplicates module %q source %q",
				entry.Module,
				entry.Source,
			)
		}
		rendered[key] = cloneManifestDesired(entry)
	}

	merged := make([]manifest.DesiredEntry, len(complete))
	for index, entry := range complete {
		merged[index] = cloneManifestDesired(entry)
		if _, ok := selected[entry.Module]; !ok {
			continue
		}
		key := desiredEntryKey(entry)
		scopedEntry, exists := rendered[key]
		if !exists {
			return nil, fmt.Errorf(
				"scoped desired is missing module %q source %q",
				entry.Module,
				entry.Source,
			)
		}
		if !sameDesiredStructure(entry, scopedEntry) {
			return nil, fmt.Errorf(
				"scoped desired changed structure for module %q source %q",
				entry.Module,
				entry.Source,
			)
		}
		merged[index] = scopedEntry
		delete(rendered, key)
	}
	if len(rendered) != 0 {
		return nil, fmt.Errorf("scoped desired contains entries outside complete profile")
	}
	return merged, nil
}

func sameDesiredStructure(left, right manifest.DesiredEntry) bool {
	return left.Module == right.Module &&
		left.Source == right.Source &&
		left.SourcePath == right.SourcePath &&
		left.Target == right.Target &&
		left.TargetPath == right.TargetPath &&
		left.Kind == right.Kind &&
		left.Mode == right.Mode
}

func desiredEntryKey(entry manifest.DesiredEntry) string {
	return entry.Module + "\x00" + entry.Source
}

func cloneManifestDesired(entry manifest.DesiredEntry) manifest.DesiredEntry {
	entry.Content = append([]byte(nil), entry.Content...)
	return entry
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func cloneObservedProfile(profile ObservedProfile) ObservedProfile {
	return ObservedProfile{targets: profile.Targets(), orphans: profile.Orphans()}
}

func cloneFileActions(actions []FileAction) []FileAction {
	cloned := append([]FileAction(nil), actions...)
	for index := range cloned {
		cloned[index] = cloned[index].Clone()
	}
	return cloned
}

func clonePrunePlan(plan PrunePlan) PrunePlan {
	return PrunePlan{actions: plan.Actions(), groups: plan.ConfirmationGroups()}
}

func cloneHookPlan(plan HookPlan) HookPlan {
	return HookPlan{actions: plan.Actions()}
}

func validateApplyPlan(plan ApplyPlan) error {
	if !plan.valid {
		return fmt.Errorf("plan is not marked valid")
	}
	if err := validateApplyContext(plan.context); err != nil {
		return err
	}
	if err := validateObservedOrder(plan.observed); err != nil {
		return err
	}
	if err := validateFileActions(plan.context, plan.observed, plan.fileActions); err != nil {
		return err
	}
	if err := validatePrunePlan(plan.context, plan.observed, plan.prune); err != nil {
		return err
	}
	if err := validateActivePruneTopology(plan.observed, plan.prune); err != nil {
		return err
	}
	return validateHookPlan(plan.context, plan.hooks)
}

func validateApplyContext(context ApplyContext) error {
	if context.Profile == "" || (context.GOOS != "darwin" && context.GOOS != "linux") {
		return fmt.Errorf("plan context has invalid profile or OS")
	}
	if context.GOARCH != "arm64" && context.GOARCH != "amd64" {
		return fmt.Errorf("plan context has unsupported architecture %q", context.GOARCH)
	}
	if context.Home == "" || !filepath.IsAbs(context.Home) ||
		context.Repository == "" || !filepath.IsAbs(context.Repository) {
		return fmt.Errorf("plan context HOME and repository must be absolute")
	}
	if context.Requirement == "" {
		return fmt.Errorf("plan context requirement is empty")
	}
	if !strictlySorted(context.Modules) || !strictlySorted(context.UnassignedModules) {
		return fmt.Errorf("plan context module lists are not strictly sorted")
	}
	if !context.Full && len(context.Modules) == 0 {
		return fmt.Errorf("partial plan context has empty module scope")
	}
	return nil
}

func validateObservedOrder(profile ObservedProfile) error {
	targets := profile.Targets()
	for index := 1; index < len(targets); index++ {
		previous := targets[index-1].Desired
		current := targets[index].Desired
		if previous.Module > current.Module ||
			(previous.Module == current.Module && previous.Source >= current.Source) {
			return fmt.Errorf("observed desired targets are not strictly ordered")
		}
	}
	orphans := profile.Orphans()
	for index := 1; index < len(orphans); index++ {
		if orphans[index-1].State.Key >= orphans[index].State.Key {
			return fmt.Errorf("observed orphan targets are not strictly ordered")
		}
	}
	return nil
}

func validateFileActions(context ApplyContext, profile ObservedProfile, actions []FileAction) error {
	selected := stringSet(context.Modules)
	expected := make([]ObservedTarget, 0, len(actions))
	for _, target := range profile.Targets() {
		if _, ok := selected[target.Desired.Module]; ok {
			expected = append(expected, target)
		}
	}
	if len(actions) != len(expected) {
		return fmt.Errorf("file action count %d does not match scope target count %d", len(actions), len(expected))
	}
	for index, action := range actions {
		target := expected[index]
		if !samePlannerDesired(action.Desired, target.Desired) {
			return fmt.Errorf("file action %d does not match scoped desired", index)
		}
		if action.Target != target.Desired.Target {
			return fmt.Errorf("file action %d has invalid target", index)
		}
		if !isSupportedFileReason(action.Reason) {
			return fmt.Errorf("file action %q uses unsupported reason %q", action.Target, action.Reason)
		}
		if action.Precondition.TargetPath != target.Desired.TargetPath ||
			!action.Precondition.TargetResolution.Equal(target.Resolution) ||
			!sameObservation(action.Precondition.Observed, target.Observed) {
			return fmt.Errorf("file action %q has inconsistent Precondition", action.Target)
		}
		if err := validateFileSourcePrecondition(action); err != nil {
			return err
		}
		if action.OnFailure.Kind != StatePreserve {
			return fmt.Errorf("file action %q failure effect must preserve state", action.Target)
		}
		switch action.Verb {
		case FileSkip, FileConflict:
			if action.OnSuccess.Kind != StatePreserve {
				return fmt.Errorf("file action %q %q must preserve state", action.Target, action.Verb)
			}
		case FileCreateLink, FileScaffold, FileAdopt, FileBackupReplace:
			if action.OnSuccess.Kind != StateUpsert || action.OnSuccess.Key != action.Desired.Target {
				return fmt.Errorf("file action %q %q has invalid state upsert", action.Target, action.Verb)
			}
		default:
			return fmt.Errorf("file action %q uses unsupported verb %q", action.Target, action.Verb)
		}
	}
	return nil
}

func validateFileSourcePrecondition(action FileAction) error {
	switch action.Verb {
	case FileCreateLink, FileBackupReplace:
		if !action.Precondition.RequireRegularSource ||
			action.Precondition.SourcePath != action.Desired.SourcePath ||
			!filepath.IsAbs(action.Precondition.SourcePath) {
			return fmt.Errorf(
				"file action %q %q must require its desired regular source",
				action.Target,
				action.Verb,
			)
		}
	case FileSkip, FileScaffold, FileAdopt, FileConflict:
		if action.Precondition.RequireRegularSource || action.Precondition.SourcePath != "" {
			return fmt.Errorf("file action %q %q must not require source", action.Target, action.Verb)
		}
	}
	return nil
}

func isSupportedFileReason(reason FileReason) bool {
	switch reason {
	case FileReasonTargetMissing,
		FileReasonExpectedLink,
		FileReasonStateMetadata,
		FileReasonOwnedLinkStale,
		FileReasonLinkDrift,
		FileReasonUnownedLink,
		FileReasonRegularConflict,
		FileReasonDirectoryConflict,
		FileReasonSpecialConflict,
		FileReasonScaffoldPresent,
		FileReasonScaffoldDeleted,
		FileReasonScaffoldRebuild,
		FileReasonOwnedLinkToScaffold,
		FileReasonReleaseOwnershipToScaffold:
		return true
	default:
		return false
	}
}

func validatePrunePlan(context ApplyContext, profile ObservedProfile, plan PrunePlan) error {
	actions := plan.Actions()
	if !context.PruneEnabled {
		if len(actions) != 0 || len(plan.ConfirmationGroups()) != 0 {
			return fmt.Errorf("disabled prune plan is not empty")
		}
		return nil
	}
	orphans := make(map[string]OrphanTarget, len(profile.Orphans()))
	for _, orphan := range profile.Orphans() {
		orphans[orphan.State.Key] = orphan
	}
	selected := stringSet(context.Modules)
	for index, action := range actions {
		if index > 0 && actions[index-1].Target >= action.Target {
			return fmt.Errorf("prune actions are not strictly ordered")
		}
		orphan, ok := orphans[action.Target]
		if !ok || action.Module != orphan.State.Module {
			return fmt.Errorf("prune action %q does not match an observed orphan", action.Target)
		}
		if !context.Full {
			if _, ok := selected[action.Module]; !ok {
				return fmt.Errorf("prune action %q is outside partial scope", action.Target)
			}
		}
		if action.Precondition.TargetPath != orphan.TargetPath ||
			!action.Precondition.TargetResolution.Equal(orphan.Resolution) ||
			!sameObservation(action.Precondition.Observed, orphan.Observed) {
			return fmt.Errorf("prune action %q has inconsistent Precondition", action.Target)
		}
		if action.Precondition.RequireRegularSource || action.Precondition.SourcePath != "" {
			return fmt.Errorf("prune action %q must not require source", action.Target)
		}
		if action.OnFailure.Kind != StatePreserve {
			return fmt.Errorf("prune action %q failure effect must preserve state", action.Target)
		}
		if action.Deferred {
			if action.DeferredReason == "" || action.OnSuccess.Kind != StatePreserve {
				return fmt.Errorf("deferred prune action %q has inconsistent state effect", action.Target)
			}
		} else if action.OnSuccess.Kind != StateDelete || action.OnSuccess.Key != action.Target {
			return fmt.Errorf("active prune action %q must delete its state key", action.Target)
		}
	}
	return validatePruneGroups(plan.ConfirmationGroups())
}

func validatePruneGroups(groups []PruneConfirmationGroup) error {
	for index, group := range groups {
		if group.Module == "" || (index > 0 && groups[index-1].Module >= group.Module) {
			return fmt.Errorf("prune confirmation groups are not strictly ordered")
		}
		for targetIndex, target := range group.Targets {
			if target.Target == "" ||
				(targetIndex > 0 && group.Targets[targetIndex-1].Target >= target.Target) {
				return fmt.Errorf("prune confirmation group %q targets are not strictly ordered", group.Module)
			}
		}
	}
	return nil
}

func validateActivePruneTopology(profile ObservedProfile, plan PrunePlan) error {
	desiredTargets := profile.Targets()
	for _, action := range plan.Actions() {
		if !action.DeletesTarget() {
			continue
		}
		pruneResolution := action.Precondition.TargetResolution
		for _, desired := range desiredTargets {
			switch {
			case pruneResolution.Equal(desired.Resolution):
				return fmt.Errorf(
					"active prune target %q has the same identity as desired target %q",
					action.Target,
					desired.Desired.Target,
				)
			case pruneResolution.IsAncestorOf(desired.Resolution):
				return fmt.Errorf(
					"active prune target %q is an ancestor of desired target %q",
					action.Target,
					desired.Desired.Target,
				)
			}
		}
	}
	return nil
}

func validateHookPlan(context ApplyContext, plan HookPlan) error {
	selected := stringSet(context.Modules)
	actions := plan.Actions()
	seen := make(map[string]struct{}, len(actions))
	previousModule := ""
	for _, action := range actions {
		if _, ok := selected[action.Module]; !ok {
			return fmt.Errorf("hook %q is outside module scope", action.StateKey)
		}
		if previousModule > action.Module {
			return fmt.Errorf("hook actions are not ordered by module")
		}
		previousModule = action.Module
		if _, exists := seen[action.StateKey]; exists {
			return fmt.Errorf("hook action key %q is duplicated", action.StateKey)
		}
		seen[action.StateKey] = struct{}{}
		if action.StateKey != action.Module+"/"+action.Script ||
			action.Profile != context.Profile || action.GOOS != context.GOOS ||
			action.Repository != context.Repository {
			return fmt.Errorf("hook %q has inconsistent identity or runtime", action.StateKey)
		}
		if action.ScriptPath == "" || !filepath.IsAbs(action.ScriptPath) ||
			action.WorkingDir == "" || !filepath.IsAbs(action.WorkingDir) ||
			action.TargetRootPath == "" || !filepath.IsAbs(action.TargetRootPath) {
			return fmt.Errorf("hook %q has non-absolute execution paths", action.StateKey)
		}
		if action.Environment != (HookEnvironment{
			Home:          context.Home,
			XDGConfigHome: filepath.Join(context.Home, ".config"),
			XDGStateHome:  filepath.Join(context.Home, ".local", "state"),
			XDGDataHome:   filepath.Join(context.Home, ".local", "share"),
			DotModule:     action.Module,
			DotOS:         context.GOOS,
			DotProfile:    context.Profile,
			DotRepo:       context.Repository,
			DotTarget:     action.TargetRootPath,
		}) {
			return fmt.Errorf("hook %q has incomplete environment", action.StateKey)
		}
		if err := validateHookInvocation(action); err != nil {
			return err
		}
		if !strings.HasPrefix(action.Fingerprint, "sha256:") || len(action.Fingerprint) != len("sha256:")+64 {
			return fmt.Errorf("hook %q has unsupported fingerprint", action.StateKey)
		}
		if action.OnFailure.Kind != HookStatePreserve {
			return fmt.Errorf("hook %q failure effect must preserve state", action.StateKey)
		}
		switch action.Verb {
		case HookSkip:
			if action.OnSuccess.Kind != HookStatePreserve {
				return fmt.Errorf("skipped hook %q must preserve state", action.StateKey)
			}
		case HookRun:
			if action.OnSuccess.Kind != HookStateUpsert ||
				action.OnSuccess.Key != action.StateKey ||
				action.OnSuccess.Fingerprint != action.Fingerprint {
				return fmt.Errorf("run hook %q has invalid state upsert", action.StateKey)
			}
		default:
			return fmt.Errorf("hook %q has unsupported verb %q", action.StateKey, action.Verb)
		}
	}
	return nil
}

func validateHookInvocation(action HookAction) error {
	switch action.Invocation.Mode {
	case HookExecutionDirect:
		if action.Invocation.Program != action.ScriptPath || len(action.Invocation.Arguments) != 0 {
			return fmt.Errorf("direct hook %q has invalid invocation", action.StateKey)
		}
	case HookExecutionShell:
		if action.Invocation.Program != "sh" ||
			!slices.Equal(action.Invocation.Arguments, []string{action.ScriptPath}) {
			return fmt.Errorf("shell hook %q has invalid invocation", action.StateKey)
		}
	default:
		return fmt.Errorf("hook %q has unsupported execution mode %q", action.StateKey, action.Invocation.Mode)
	}
	return nil
}

func samePlannerDesired(left, right Desired) bool {
	return left.Module == right.Module &&
		left.Source == right.Source &&
		left.SourcePath == right.SourcePath &&
		left.Target == right.Target &&
		left.TargetPath == right.TargetPath &&
		left.Kind == right.Kind &&
		left.Mode == right.Mode &&
		bytes.Equal(left.Content, right.Content)
}

func sameObservation(left, right Observation) bool {
	return left.Kind == right.Kind &&
		left.Mode == right.Mode &&
		left.LinkDest == right.LinkDest &&
		left.Hash == right.Hash &&
		bytes.Equal(left.Content, right.Content)
}

func strictlySorted(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] >= values[index] {
			return false
		}
	}
	return true
}
