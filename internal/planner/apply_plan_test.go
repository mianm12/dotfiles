package planner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/mianm12/dotfiles/internal/state"
)

func TestPlanScopedFiles_UsesCompleteObservationAndScopeDecisions(t *testing.T) {
	t.Parallel()

	fixture := newFileCompositionFixture(t)
	before := snapshotObservationTree(t, fixture.root)
	scoped, err := fixture.validated.RenderScope([]string{"alpha"}, fixture.context)
	if err != nil {
		t.Fatalf("RenderScope(alpha) error = %v", err)
	}

	observed, actions, err := planScopedFiles(
		fixture.validated,
		scoped,
		fixture.loadedState,
		DecisionOptions{},
	)
	if err != nil {
		t.Fatalf("planScopedFiles() error = %v", err)
	}
	if got := len(observed.Targets()); got != 2 {
		t.Fatalf("complete observed targets = %d, want 2", got)
	}
	if got := len(observed.Orphans()); got != 0 {
		t.Fatalf("complete observed orphans = %d, want alias matched", got)
	}
	if got := len(actions); got != 1 {
		t.Fatalf("scope file actions = %d, want 1", got)
	}
	action := actions[0]
	if action.Desired.Module != "alpha" || action.Desired.Source != "item.template" {
		t.Fatalf("scope action desired = %#v, want alpha/item.template", action.Desired)
	}
	if got, want := string(action.Desired.Content), "profile=all"; got != want {
		t.Fatalf("scope scaffold content = %q, want %q", got, want)
	}
	if action.Verb != FileScaffold || action.Reason != FileReasonOwnedLinkToScaffold {
		t.Fatalf("alias migration action = (%q, %q), want scaffold/owned-link-to-scaffold", action.Verb, action.Reason)
	}
	if action.OnSuccess.PreviousKey != "~/real/item" || action.OnSuccess.Key != "~/alias/item" {
		t.Fatalf("alias migration state effect = %#v", action.OnSuccess)
	}

	// beta 的 scaffold 故意包含非法模板；完整 scope 会失败，但 alpha partial 不渲染它。
	if full, fullErr := fixture.validated.RenderScope(nil, fixture.context); fullErr == nil {
		t.Fatalf("RenderScope(full) = %#v, nil; want beta template failure", full)
	}
	if after := snapshotObservationTree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("planScopedFiles() changed fixture tree\nbefore=%v\nafter=%v", before, after)
	}
}

func TestPlanScopedFiles_RejectsInvalidScopeWithoutPartialResult(t *testing.T) {
	t.Parallel()

	fixture := newFileCompositionFixture(t)
	observed, actions, err := planScopedFiles(
		fixture.validated,
		manifest.ScopedProfile{},
		fixture.loadedState,
		DecisionOptions{},
	)
	if err == nil {
		t.Fatal("planScopedFiles(invalid scope) error = nil")
	}
	if targets, orphans := observed.Targets(), observed.Orphans(); targets != nil || orphans != nil {
		t.Fatalf("failed observed profile = targets %#v orphans %#v, want zero", targets, orphans)
	}
	if actions != nil {
		t.Fatalf("failed actions = %#v, want nil", actions)
	}
}

func TestPlanApply_ComposesDeterministicFullAndPartialPlansWithoutWrites(t *testing.T) {
	fixture := newApplyIntegrationFixture(t)
	fixture.redirectEnvironment(t)
	before := snapshotObservationTree(t, fixture.root)

	options := fixture.options()
	options.Modules = []string{"alpha"}
	partial, err := PlanApply(options)
	if err != nil {
		t.Fatalf("PlanApply(partial alpha) error = %v", err)
	}
	if !partial.Valid() {
		t.Fatal("PlanApply(partial alpha) returned invalid plan")
	}
	context := partial.Context()
	if context.Profile != "all" || context.Full || !reflect.DeepEqual(context.Modules, []string{"alpha"}) {
		t.Fatalf("partial context = %#v", context)
	}
	if got, want := context.UnassignedModules, []string{"unused"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unassigned modules = %v, want %v", got, want)
	}
	fileActions := partial.FileActions()
	if got, want := applyFileActionKeys(fileActions), []string{
		"alpha/conflict.txt",
		"alpha/stable.txt",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("partial file action order = %v, want %v", got, want)
	}
	if fileActions[0].Verb != FileConflict {
		t.Fatalf("conflicting file action = %#v", fileActions[0])
	}
	pruneActions := partial.Prune().Actions()
	if len(pruneActions) != 1 || pruneActions[0].Target != "~/alpha/obsolete" || !pruneActions[0].Deferred {
		t.Fatalf("partial prune actions = %#v, want deferred alpha orphan", pruneActions)
	}
	if groups := partial.Prune().ConfirmationGroups(); groups != nil {
		t.Fatalf("partial confirmation groups = %#v, want no whole-module group", groups)
	}
	hooks := partial.Hooks().Actions()
	if len(hooks) != 1 || hooks[0].Module != "alpha" || hooks[0].Verb != HookRun {
		t.Fatalf("partial hooks = %#v, want alpha run-hook despite conflict", hooks)
	}

	forceOptions := options
	forceOptions.Force = true
	forced, err := PlanApply(forceOptions)
	if err != nil {
		t.Fatalf("PlanApply(partial force) error = %v", err)
	}
	if forced.FileActions()[0].Verb != FileBackupReplace ||
		forced.FileActions()[0].Precondition.Leaf.Kind != LeafExactRegular ||
		forced.FileActions()[0].Precondition.Leaf.Hash == "" ||
		forced.Prune().Actions()[0].Deferred {
		t.Fatalf("forced file/prune plan = file %#v prune %#v", forced.FileActions(), forced.Prune().Actions())
	}
	noPruneOptions := options
	noPruneOptions.NoPrune = true
	withoutPrune, err := PlanApply(noPruneOptions)
	if err != nil {
		t.Fatalf("PlanApply(no-prune) error = %v", err)
	}
	if withoutPrune.Context().PruneEnabled || withoutPrune.Prune().Actions() != nil || len(withoutPrune.Hooks().Actions()) != 1 {
		t.Fatalf("no-prune plan = context %#v prune %#v hooks %#v", withoutPrune.Context(), withoutPrune.Prune().Actions(), withoutPrune.Hooks().Actions())
	}

	repeated, err := PlanApply(options)
	if err != nil {
		t.Fatalf("PlanApply(partial repeated) error = %v", err)
	}
	if !reflect.DeepEqual(partial.Context(), repeated.Context()) ||
		!reflect.DeepEqual(partial.Observed().Targets(), repeated.Observed().Targets()) ||
		!reflect.DeepEqual(partial.Observed().Orphans(), repeated.Observed().Orphans()) ||
		!reflect.DeepEqual(partial.FileActions(), repeated.FileActions()) ||
		!reflect.DeepEqual(partial.Prune().Actions(), repeated.Prune().Actions()) ||
		!reflect.DeepEqual(partial.Hooks().Actions(), repeated.Hooks().Actions()) {
		t.Fatal("repeated PlanApply(partial) changed deterministic plan")
	}

	fullOptions := fixture.options()
	full, err := PlanApply(fullOptions)
	if err != nil {
		t.Fatalf("PlanApply(full) error = %v", err)
	}
	if !full.Context().Full || !reflect.DeepEqual(full.Context().Modules, []string{"alpha", "beta"}) {
		t.Fatalf("full context = %#v", full.Context())
	}
	if got, want := applyFileActionKeys(full.FileActions()), []string{
		"alpha/conflict.txt",
		"alpha/stable.txt",
		"beta/other.txt",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("full file action order = %v, want %v", got, want)
	}
	if got, want := applyHookActionKeys(full.Hooks().Actions()), []string{
		"alpha/hooks/setup.sh",
		"beta/hooks/setup.sh",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("full hook order = %v, want %v", got, want)
	}
	groups := full.Prune().ConfirmationGroups()
	if len(groups) != 1 || groups[0].Module != "legacy" || len(groups[0].Targets) != 1 {
		t.Fatalf("full confirmation groups = %#v, want legacy whole-module group", groups)
	}

	// ApplyPlan 的组合 getters 不能让 presentation 调用方反向修改 plan。
	mutableContext := partial.Context()
	mutableContext.Modules[0] = "changed"
	mutableObserved := partial.Observed().Targets()
	mutableObserved[0].Observed.Hash = "changed"
	mutableFiles := partial.FileActions()
	mutableFiles[0].Precondition.Leaf.Kind = LeafPresent
	mutableHooks := full.Hooks().Actions()
	mutableHooks[1].Invocation.Arguments[0] = "changed"
	if partial.Context().Modules[0] != "alpha" ||
		partial.Observed().Targets()[0].Observed.Hash != "" ||
		partial.FileActions()[0].Precondition.Leaf.Kind != LeafAny ||
		full.Hooks().Actions()[1].Invocation.Arguments[0] == "changed" {
		t.Fatal("mutating ApplyPlan getter result changed stored plan")
	}
	if after := snapshotObservationTree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("PlanApply() changed fixture tree\nbefore=%v\nafter=%v", before, after)
	}
	if _, err := os.Lstat(filepath.Join(fixture.home, ".local", "state", "dot", "lock")); !os.IsNotExist(err) {
		t.Fatalf("PlanApply() lock Lstat error = %v, want missing", err)
	}
}

func TestPlanLoadedApply_UsesExactLoadedStateWithoutReload(t *testing.T) {
	fixture := newApplyIntegrationFixture(t)
	fixture.redirectEnvironment(t)
	options := fixture.options()
	inputs, err := dotruntime.LoadReadOnly(options.Runtime, options.CLIVersion)
	if err != nil {
		t.Fatalf("LoadReadOnly() error = %v", err)
	}

	target := filepath.Join(fixture.home, "alpha", "stable.txt")
	source := filepath.Join(fixture.repository, "modules", "alpha", "stable.txt")
	if err := os.Symlink(source, target); err != nil {
		t.Fatalf("os.Symlink(expected target) error = %v", err)
	}
	writeApplyState(t, fixture.statePath, map[string]applyStateEntry{
		"~/alpha/stable.txt": {
			Module:   "alpha",
			Kind:     "symlink",
			Source:   "modules/alpha/stable.txt",
			LinkDest: source,
		},
	}, nil)

	loadedPlan, err := PlanLoadedApply(inputs, ApplyScopeOptions{
		Modules: []string{"alpha"},
		NoPrune: true,
	})
	if err != nil {
		t.Fatalf("PlanLoadedApply() error = %v", err)
	}
	loadedAction := loadedPlan.FileActions()[1]
	if loadedAction.Desired.Source != "stable.txt" ||
		loadedAction.Verb != FileAdopt || loadedAction.Reason != FileReasonStateMetadata {
		t.Fatalf("exact-input stable action = %#v, want L2 adopt from originally loaded missing record", loadedAction)
	}

	freshOptions := options
	freshOptions.Modules = []string{"alpha"}
	freshOptions.NoPrune = true
	freshPlan, err := PlanApply(freshOptions)
	if err != nil {
		t.Fatalf("PlanApply(fresh) error = %v", err)
	}
	freshAction := freshPlan.FileActions()[1]
	if freshAction.Desired.Source != "stable.txt" || freshAction.Verb != FileSkip {
		t.Fatalf("fresh stable action = %#v, want skip from reloaded state", freshAction)
	}
}

func TestPlanApply_PartialScopeSkipsUnrequestedTemplateRenderButNotFullCollision(t *testing.T) {
	t.Run("unrequested template failure", func(t *testing.T) {
		fixture := newApplyIntegrationFixture(t)
		fixture.redirectEnvironment(t)
		writeApplyFile(t, filepath.Join(fixture.repository, "modules", "beta", "broken.template"), `{{`)
		before := snapshotObservationTree(t, fixture.root)

		partialOptions := fixture.options()
		partialOptions.Modules = []string{"alpha"}
		if plan, err := PlanApply(partialOptions); err != nil || !plan.Valid() {
			t.Fatalf("PlanApply(partial alpha) = (%#v, %v), want valid plan", plan, err)
		}
		full, err := PlanApply(fixture.options())
		if err == nil || !strings.Contains(err.Error(), "broken.template") {
			t.Fatalf("PlanApply(full) error = %v, want beta template failure", err)
		}
		if !reflect.DeepEqual(full, ApplyPlan{}) {
			t.Fatalf("failed full plan = %#v, want zero", full)
		}
		if after := snapshotObservationTree(t, fixture.root); !reflect.DeepEqual(after, before) {
			t.Fatalf("template failure planning changed fixture\nbefore=%v\nafter=%v", before, after)
		}
	})

	t.Run("complete profile collision", func(t *testing.T) {
		fixture := newApplyIntegrationFixture(t)
		fixture.redirectEnvironment(t)
		writeApplyFile(t, filepath.Join(fixture.repository, "modules", "beta", "dot.toml"), `target = "~/alpha"
[hooks]
run_once = ["hooks/setup.sh"]
`)
		writeApplyFile(t, filepath.Join(fixture.repository, "modules", "beta", "stable.txt"), "beta\n")
		options := fixture.options()
		options.Modules = []string{"alpha"}
		plan, err := PlanApply(options)
		if err == nil || !strings.Contains(err.Error(), "target") {
			t.Fatalf("PlanApply(partial collision) error = %v, want complete target collision", err)
		}
		if !reflect.DeepEqual(plan, ApplyPlan{}) {
			t.Fatalf("failed collision plan = %#v, want zero", plan)
		}
	})
}

func TestPlanApply_ReadsRegularDigestOnlyWhenRequired(t *testing.T) {
	t.Run("force ignores unrequested regular payload", func(t *testing.T) {
		fixture := newApplyIntegrationFixture(t)
		fixture.redirectEnvironment(t)
		target := filepath.Join(fixture.home, "beta", "other.txt")
		writeApplyFile(t, target, "private beta data\n")
		makeUnreadableRegular(t, target)

		options := fixture.options()
		options.Modules = []string{"alpha"}
		options.Force = true
		plan, err := PlanApply(options)
		if err != nil {
			t.Fatalf("PlanApply(partial alpha) error = %v", err)
		}
		if !plan.Valid() || len(plan.FileActions()) != 2 {
			t.Fatalf("PlanApply(partial alpha) = %#v, want valid alpha-only plan", plan)
		}
	})

	t.Run("non-force L6 regular", func(t *testing.T) {
		fixture := newApplyIntegrationFixture(t)
		fixture.redirectEnvironment(t)
		target := filepath.Join(fixture.home, "alpha", "conflict.txt")
		makeUnreadableRegular(t, target)

		options := fixture.options()
		options.Modules = []string{"alpha"}
		options.NoPrune = true
		plan, err := PlanApply(options)
		if err != nil {
			t.Fatalf("PlanApply(non-force L6) error = %v", err)
		}
		action := plan.FileActions()[0]
		if action.Verb != FileConflict || action.Reason != FileReasonRegularConflict {
			t.Fatalf("L6 action = %q/%q, want regular conflict", action.Verb, action.Reason)
		}
	})

	t.Run("force L6 requires regular digest", func(t *testing.T) {
		fixture := newApplyIntegrationFixture(t)
		fixture.redirectEnvironment(t)
		target := filepath.Join(fixture.home, "alpha", "conflict.txt")
		makeUnreadableRegular(t, target)

		options := fixture.options()
		options.Modules = []string{"alpha"}
		options.NoPrune = true
		options.Force = true
		plan, err := PlanApply(options)
		if err == nil || !strings.Contains(err.Error(), "digest") {
			t.Fatalf("PlanApply(force L6) = (%#v, %v), want digest read failure", plan, err)
		}
		if !reflect.DeepEqual(plan, ApplyPlan{}) {
			t.Fatalf("failed force plan = %#v, want zero", plan)
		}
	})

	t.Run("S1b scaffold present", func(t *testing.T) {
		fixture := newApplyIntegrationFixture(t)
		fixture.redirectEnvironment(t)
		writeApplyFile(
			t,
			filepath.Join(fixture.repository, "modules", "alpha", "local.template"),
			"scaffold bytes\n",
		)
		target := filepath.Join(fixture.home, "alpha", "local")
		writeApplyFile(t, target, "private user data\n")
		makeUnreadableRegular(t, target)

		options := fixture.options()
		options.Modules = []string{"alpha"}
		options.NoPrune = true
		plan, err := PlanApply(options)
		if err != nil {
			t.Fatalf("PlanApply(S1b) error = %v", err)
		}
		var found *FileAction
		for _, action := range plan.FileActions() {
			if action.Desired.Source == "local.template" {
				candidate := action
				found = &candidate
				break
			}
		}
		if found == nil || found.Verb != FileAdopt || found.Reason != FileReasonScaffoldPresent {
			t.Fatalf("S1b action = %#v, want scaffold-present adopt", found)
		}
	})
}

func TestPlanApply_FailsClosedWithZeroPlan(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, applyIntegrationFixture)
		want   string
	}{
		{
			name: "invalid manifest",
			mutate: func(t *testing.T, fixture applyIntegrationFixture) {
				writeApplyFile(t, filepath.Join(fixture.repository, "dot.toml"), `requires = ">=0.0.0"
unknown = true
[profiles]
all = ["alpha"]
`)
			},
			want: "manifest",
		},
		{
			name: "invalid state",
			mutate: func(t *testing.T, fixture applyIntegrationFixture) {
				writeApplyFile(t, fixture.statePath, `{`)
			},
			want: "state",
		},
		{
			name: "rendered state",
			mutate: func(t *testing.T, fixture applyIntegrationFixture) {
				writeApplyFile(t, fixture.statePath, `{"version":1,"entries":{"~/alpha/rendered":{"module":"alpha","kind":"rendered","source":"modules/alpha/rendered.tmpl","hash":"sha256:0000000000000000000000000000000000000000000000000000000000000000","applied_at":"2026-07-19T00:00:00Z"}},"run_once":{}}`)
			},
			want: "rendered",
		},
		{
			name: "managed desired outside scope",
			mutate: func(t *testing.T, fixture applyIntegrationFixture) {
				writeApplyFile(t, filepath.Join(fixture.repository, "modules", "beta", "managed.tmpl"), "managed\n")
			},
			want: "managed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newApplyIntegrationFixture(t)
			fixture.redirectEnvironment(t)
			test.mutate(t, fixture)
			before := snapshotObservationTree(t, fixture.root)
			options := fixture.options()
			options.Modules = []string{"alpha"}
			plan, err := PlanApply(options)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) {
				t.Fatalf("PlanApply() error = %v, want substring %q", err, test.want)
			}
			if !reflect.DeepEqual(plan, ApplyPlan{}) {
				t.Fatalf("failed PlanApply() plan = %#v, want zero", plan)
			}
			if after := snapshotObservationTree(t, fixture.root); !reflect.DeepEqual(after, before) {
				t.Fatalf("failed PlanApply() changed fixture\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestPlanApply_MissingStateDoesNotCreateStateOrLock(t *testing.T) {
	fixture := newApplyIntegrationFixture(t)
	fixture.redirectEnvironment(t)
	if err := os.Remove(fixture.statePath); err != nil {
		t.Fatalf("Remove(state) error = %v", err)
	}
	stateRoot := filepath.Dir(fixture.statePath)
	if err := os.Remove(stateRoot); err != nil {
		t.Fatalf("Remove(state root) error = %v", err)
	}
	before := snapshotObservationTree(t, fixture.root)
	plan, err := PlanApply(fixture.options())
	if err != nil || !plan.Valid() {
		t.Fatalf("PlanApply(missing state) = (%#v, %v), want valid", plan, err)
	}
	if after := snapshotObservationTree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("PlanApply(missing state) changed fixture\nbefore=%v\nafter=%v", before, after)
	}
	if _, err := os.Lstat(stateRoot); !os.IsNotExist(err) {
		t.Fatalf("state root Lstat error = %v, want missing", err)
	}
}

func TestPlanApply_RejectsUnknownScopeWithZeroPlan(t *testing.T) {
	fixture := newApplyIntegrationFixture(t)
	fixture.redirectEnvironment(t)
	before := snapshotObservationTree(t, fixture.root)
	options := fixture.options()
	options.Modules = []string{"missing"}
	plan, err := PlanApply(options)
	if err == nil || !strings.Contains(err.Error(), "not in the effective profile") {
		t.Fatalf("PlanApply(unknown scope) error = %v", err)
	}
	if !reflect.DeepEqual(plan, ApplyPlan{}) {
		t.Fatalf("failed unknown-scope plan = %#v, want zero", plan)
	}
	if after := snapshotObservationTree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("unknown-scope planning changed fixture\nbefore=%v\nafter=%v", before, after)
	}
}

func TestPlanApply_RejectsFileUpsertOverlappingRetainedOrphan(t *testing.T) {
	tests := []struct {
		name          string
		noPrune       bool
		partial       bool
		childConflict bool
		scaffold      bool
	}{
		{
			name:    "no-prune retains orphan",
			noPrune: true,
		},
		{
			name:     "scaffold upsert retains orphan",
			noPrune:  true,
			scaffold: true,
		},
		{
			name:          "conflict defers orphan prune",
			childConflict: true,
		},
		{
			name:    "partial scope retains outside orphan",
			partial: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFileUpsertStateTopologyFixture(t, test.childConflict, test.scaffold)
			fixture.redirectEnvironment(t)
			before := snapshotObservationTree(t, fixture.root)
			options := fixture.options()
			options.NoPrune = test.noPrune
			if test.partial {
				options.Modules = []string{"app"}
			}

			plan, err := PlanApply(options)
			if !errors.Is(err, paths.ErrTargetOverlap) ||
				!strings.Contains(err.Error(), "file state upsert") ||
				!strings.Contains(err.Error(), "retained state target") {
				t.Fatalf("PlanApply() error = %v, want file-upsert/orphan target overlap", err)
			}
			if !reflect.DeepEqual(plan, ApplyPlan{}) {
				t.Fatalf("failed PlanApply() plan = %#v, want zero", plan)
			}
			if after := snapshotObservationTree(t, fixture.root); !reflect.DeepEqual(after, before) {
				t.Fatalf("failed PlanApply() changed fixture\nbefore=%v\nafter=%v", before, after)
			}
		})
	}

	t.Run("state-only adopt below retained orphan", func(t *testing.T) {
		fixture := newFileAdoptStateTopologyFixture(t)
		fixture.redirectEnvironment(t)
		before := snapshotObservationTree(t, fixture.root)
		options := fixture.options()
		options.NoPrune = true

		plan, err := PlanApply(options)
		if !errors.Is(err, paths.ErrTargetOverlap) ||
			!strings.Contains(err.Error(), "retained state target") ||
			!strings.Contains(err.Error(), "ancestor of file state upsert") {
			t.Fatalf("PlanApply() error = %v, want orphan-ancestor state-only upsert rejection", err)
		}
		if !reflect.DeepEqual(plan, ApplyPlan{}) {
			t.Fatalf("failed PlanApply() plan = %#v, want zero", plan)
		}
		if after := snapshotObservationTree(t, fixture.root); !reflect.DeepEqual(after, before) {
			t.Fatalf("failed PlanApply() changed fixture\nbefore=%v\nafter=%v", before, after)
		}
	})
}

func TestValidateApplyPlan_RejectsInconsistentAction(t *testing.T) {
	fixture := newApplyIntegrationFixture(t)
	fixture.redirectEnvironment(t)
	plan, err := PlanApply(fixture.options())
	if err != nil {
		t.Fatalf("PlanApply() error = %v", err)
	}
	plan.fileActions[0].OnFailure = StateEffect{Kind: StateUpsert}
	if err := validateApplyPlan(plan); err == nil {
		t.Fatal("validateApplyPlan(inconsistent failure effect) error = nil")
	}
}

func TestValidateApplyPlan_RejectsInvalidActionShape(t *testing.T) {
	tests := []struct {
		name   string
		force  bool
		mutate func(*testing.T, *ApplyPlan)
		want   string
	}{
		{
			name: "create-link without regular source requirement",
			mutate: func(t *testing.T, plan *ApplyPlan) {
				index := fileActionIndex(t, plan.fileActions, FileCreateLink)
				plan.fileActions[index].Precondition.SourcePath = ""
				plan.fileActions[index].Precondition.RequireRegularSource = false
			},
			want: "regular source",
		},
		{
			name:  "backup-replace with mismatched source",
			force: true,
			mutate: func(t *testing.T, plan *ApplyPlan) {
				index := fileActionIndex(t, plan.fileActions, FileBackupReplace)
				plan.fileActions[index].Precondition.SourcePath = filepath.Join(plan.context.Repository, "wrong-source")
			},
			want: "regular source",
		},
		{
			name: "conflict with source requirement",
			mutate: func(t *testing.T, plan *ApplyPlan) {
				index := fileActionIndex(t, plan.fileActions, FileConflict)
				plan.fileActions[index].Precondition.SourcePath = plan.fileActions[index].Desired.SourcePath
				plan.fileActions[index].Precondition.RequireRegularSource = true
			},
			want: "must not require source",
		},
		{
			name: "unknown file reason",
			mutate: func(t *testing.T, plan *ApplyPlan) {
				index := fileActionIndex(t, plan.fileActions, FileConflict)
				plan.fileActions[index].Reason = FileReason("future")
			},
			want: "unsupported reason",
		},
		{
			name: "closed enums forming impossible decision",
			mutate: func(t *testing.T, plan *ApplyPlan) {
				index := fileActionIndex(t, plan.fileActions, FileConflict)
				plan.fileActions[index].Verb = FileSkip
				plan.fileActions[index].Reason = FileReasonTargetMissing
			},
			want: "does not match canonical decision",
		},
		{
			name: "file verb crossing desired kind",
			mutate: func(t *testing.T, plan *ApplyPlan) {
				index := fileActionIndex(t, plan.fileActions, FileCreateLink)
				plan.fileActions[index].Verb = FileScaffold
				plan.fileActions[index].Precondition.SourcePath = ""
				plan.fileActions[index].Precondition.RequireRegularSource = false
			},
			want: "does not match canonical decision",
		},
		{
			name: "state payload diverging from decision",
			mutate: func(t *testing.T, plan *ApplyPlan) {
				index := fileActionIndex(t, plan.fileActions, FileCreateLink)
				plan.fileActions[index].OnSuccess.Entry.Source = "modules/wrong-source"
			},
			want: "state effects do not match canonical decision",
		},
		{
			name: "prune with source requirement",
			mutate: func(t *testing.T, plan *ApplyPlan) {
				t.Helper()
				if len(plan.prune.actions) == 0 {
					t.Fatal("fixture has no prune action")
				}
				plan.prune.actions[0].Precondition.SourcePath = filepath.Join(plan.context.Repository, "unexpected-source")
				plan.prune.actions[0].Precondition.RequireRegularSource = true
			},
			want: "must not require source",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newApplyIntegrationFixture(t)
			fixture.redirectEnvironment(t)
			options := fixture.options()
			options.Force = test.force
			plan, err := PlanApply(options)
			if err != nil {
				t.Fatalf("PlanApply() error = %v", err)
			}
			test.mutate(t, &plan)
			if err := validateApplyPlan(plan); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateApplyPlan() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidateApplyPlan_RejectsNonCanonicalPrunePlan(t *testing.T) {
	standardPlan := func(t *testing.T) ApplyPlan {
		t.Helper()
		fixture := newApplyIntegrationFixture(t)
		fixture.redirectEnvironment(t)
		plan, err := PlanApply(fixture.options())
		if err != nil {
			t.Fatalf("PlanApply() error = %v", err)
		}
		return plan
	}
	unownedPlan := func(t *testing.T) ApplyPlan {
		t.Helper()
		fixture := newApplyIntegrationFixture(t)
		fixture.redirectEnvironment(t)
		legacyTarget := filepath.Join(fixture.home, "legacy", "old")
		if err := os.Remove(legacyTarget); err != nil {
			t.Fatalf("Remove(legacy target) error = %v", err)
		}
		if err := os.Symlink(filepath.Join(fixture.root, "user-owned"), legacyTarget); err != nil {
			t.Fatalf("Symlink(unowned legacy target) error = %v", err)
		}
		options := fixture.options()
		options.Force = true
		plan, err := PlanApply(options)
		if err != nil {
			t.Fatalf("PlanApply(unowned orphan) error = %v", err)
		}
		return plan
	}

	tests := []struct {
		name   string
		plan   func(*testing.T) ApplyPlan
		mutate func(*testing.T, *ApplyPlan)
	}{
		{
			name: "P3 promoted to target delete",
			plan: unownedPlan,
			mutate: func(t *testing.T, plan *ApplyPlan) {
				index := pruneActionIndex(t, plan.prune.actions, PruneReasonUnowned)
				action := &plan.prune.actions[index]
				action.Mode = PruneTargetAndState
				action.Reason = PruneReasonOwned
				action.Warning = false
			},
		},
		{
			name: "deferred action made active",
			plan: standardPlan,
			mutate: func(t *testing.T, plan *ApplyPlan) {
				t.Helper()
				for index := range plan.prune.actions {
					action := &plan.prune.actions[index]
					if !action.Deferred {
						continue
					}
					action.Deferred = false
					action.DeferredReason = PruneDeferredNone
					action.OnSuccess = StateEffect{Kind: StateDelete, Key: action.Target}
					return
				}
				t.Fatal("fixture has no deferred prune action")
			},
		},
		{
			name: "action omitted",
			plan: standardPlan,
			mutate: func(t *testing.T, plan *ApplyPlan) {
				t.Helper()
				if len(plan.prune.actions) < 2 {
					t.Fatalf("fixture prune actions = %d, want at least 2", len(plan.prune.actions))
				}
				plan.prune.actions = plan.prune.actions[:len(plan.prune.actions)-1]
			},
		},
		{
			name: "confirmation group omitted",
			plan: standardPlan,
			mutate: func(t *testing.T, plan *ApplyPlan) {
				t.Helper()
				if len(plan.prune.groups) == 0 {
					t.Fatal("fixture has no prune confirmation group")
				}
				plan.prune.groups = nil
			},
		},
		{
			name: "confirmation deletion summary changed",
			plan: unownedPlan,
			mutate: func(t *testing.T, plan *ApplyPlan) {
				t.Helper()
				if len(plan.prune.groups) != 1 || len(plan.prune.groups[0].Targets) != 1 {
					t.Fatalf("fixture prune groups = %#v, want one target", plan.prune.groups)
				}
				plan.prune.groups[0].Targets[0].WouldDeleteTarget = true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := test.plan(t)
			test.mutate(t, &plan)
			if err := validateApplyPlan(plan); err == nil || !strings.Contains(err.Error(), "canonical prune plan") {
				t.Fatalf("validateApplyPlan() error = %v, want canonical prune plan rejection", err)
			}
		})
	}
}

func TestPlanApply_RejectsActivePruneAncestorOfCompleteDesired(t *testing.T) {
	tests := []struct {
		name        string
		stateModule string
		modules     []string
	}{
		{
			name:        "full scope",
			stateModule: "legacy",
		},
		{
			name:        "partial scope protects unrequested desired",
			stateModule: "cleanup",
			modules:     []string{"cleanup"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPruneAncestorFixture(t, applyStateEntry{
				Module:   test.stateModule,
				Kind:     "symlink",
				Source:   "modules/" + test.stateModule + "/old.txt",
				LinkDest: "owned",
			}, false)
			fixture.redirectEnvironment(t)
			options := fixture.options()
			options.Modules = test.modules
			before := snapshotObservationTree(t, fixture.root)
			plan, err := PlanApply(options)
			if err == nil || !strings.Contains(err.Error(), "prune") || !strings.Contains(err.Error(), "ancestor") {
				t.Fatalf("PlanApply(active ancestor prune) error = %v, want prune ancestor rejection", err)
			}
			if !reflect.DeepEqual(plan, ApplyPlan{}) {
				t.Fatalf("failed topology plan = %#v, want zero", plan)
			}
			if after := snapshotObservationTree(t, fixture.root); !reflect.DeepEqual(after, before) {
				t.Fatalf("topology rejection changed fixture\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestPlanApply_FileUpsertTopologyDependsOnStateEffectNotPruneMode(t *testing.T) {
	tests := []struct {
		name          string
		state         applyStateEntry
		childConflict bool
		noPrune       bool
		wantMode      PruneMode
		wantDeferred  bool
		wantRejected  bool
	}{
		{
			name: "P1 state-only",
			state: applyStateEntry{
				Module: "legacy",
				Kind:   "scaffold",
				Source: "modules/legacy/old.template",
			},
			wantMode:     PruneStateOnly,
			wantRejected: true,
		},
		{
			name: "P3 warning state-only",
			state: applyStateEntry{
				Module:   "legacy",
				Kind:     "symlink",
				Source:   "modules/legacy/old.txt",
				LinkDest: "changed",
			},
			wantMode:     PruneStateOnly,
			wantRejected: true,
		},
		{
			name: "deferred P2",
			state: applyStateEntry{
				Module:   "legacy",
				Kind:     "symlink",
				Source:   "modules/legacy/old.txt",
				LinkDest: "owned",
			},
			childConflict: true,
			wantMode:      PruneTargetAndState,
			wantDeferred:  true,
		},
		{
			name: "no-prune",
			state: applyStateEntry{
				Module:   "legacy",
				Kind:     "symlink",
				Source:   "modules/legacy/old.txt",
				LinkDest: "owned",
			},
			noPrune:      true,
			wantRejected: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPruneAncestorFixture(t, test.state, test.childConflict)
			fixture.redirectEnvironment(t)
			options := fixture.options()
			options.NoPrune = test.noPrune
			plan, err := PlanApply(options)
			if test.wantRejected {
				if !errors.Is(err, paths.ErrTargetOverlap) ||
					!strings.Contains(err.Error(), "file state upsert") ||
					!strings.Contains(err.Error(), "retained state target") {
					t.Fatalf("PlanApply(%s) error = %v, want file-upsert/orphan rejection", test.name, err)
				}
				if !reflect.DeepEqual(plan, ApplyPlan{}) {
					t.Fatalf("failed PlanApply(%s) plan = %#v, want zero", test.name, plan)
				}
				return
			}
			if err != nil || !plan.Valid() {
				t.Fatalf("PlanApply(%s) = (%#v, %v), want valid", test.name, plan, err)
			}
			actions := plan.Prune().Actions()
			if test.noPrune {
				if actions != nil {
					t.Fatalf("no-prune actions = %#v, want nil", actions)
				}
				return
			}
			if len(actions) != 1 || actions[0].Mode != test.wantMode || actions[0].Deferred != test.wantDeferred || actions[0].DeletesTarget() {
				t.Fatalf("non-active-delete prune actions = %#v", actions)
			}
		})
	}
}

func TestValidateActivePruneTopology_RejectsEqualIdentity(t *testing.T) {
	t.Parallel()

	targetPath := filepath.Join(t.TempDir(), "target")
	resolution, err := paths.ResolveTarget(targetPath)
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	profile := ObservedProfile{targets: []ObservedTarget{{
		Desired:    Desired{Module: "app", Source: "file", Target: "~/target", TargetPath: targetPath},
		Resolution: resolution,
	}}}
	prune := PrunePlan{actions: []PruneAction{{
		Mode:     PruneTargetAndState,
		Target:   "~/alias",
		Deferred: false,
		Precondition: Precondition{
			TargetPath:       targetPath,
			TargetResolution: resolution,
		},
	}}}
	if err := validateActivePruneTopology(profile, prune); err == nil || !strings.Contains(err.Error(), "same identity") {
		t.Fatalf("validateActivePruneTopology(equal identity) error = %v", err)
	}
}

func TestValidateFileStateTopology_OrphanRelations(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	parentPath := filepath.Join(root, "parent")
	childPath := filepath.Join(parentPath, "child")
	unrelatedPath := filepath.Join(root, "unrelated")
	resolve := func(path string) paths.TargetResolution {
		t.Helper()
		resolution, err := paths.ResolveTarget(path)
		if err != nil {
			t.Fatalf("ResolveTarget(%q) error = %v", path, err)
		}
		return resolution
	}
	parent := resolve(parentPath)
	child := resolve(childPath)
	unrelated := resolve(unrelatedPath)

	tests := []struct {
		name             string
		effect           StateEffectKind
		actionResolution paths.TargetResolution
		orphanResolution paths.TargetResolution
		wantRelation     string
	}{
		{
			name:             "equal identity",
			effect:           StateUpsert,
			actionResolution: parent,
			orphanResolution: parent,
			wantRelation:     "same identity",
		},
		{
			name:             "upsert is ancestor",
			effect:           StateUpsert,
			actionResolution: parent,
			orphanResolution: child,
			wantRelation:     "ancestor of retained state target",
		},
		{
			name:             "orphan is ancestor",
			effect:           StateUpsert,
			actionResolution: child,
			orphanResolution: parent,
			wantRelation:     "is an ancestor of file state upsert",
		},
		{
			name:             "unrelated upsert",
			effect:           StateUpsert,
			actionResolution: unrelated,
			orphanResolution: parent,
		},
		{
			name:             "preserve does not add state",
			effect:           StatePreserve,
			actionResolution: parent,
			orphanResolution: child,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := ObservedProfile{orphans: []OrphanTarget{{
				Resolution: test.orphanResolution,
				State:      HistoricalState{Key: "~/orphan"},
			}}}
			actions := []FileAction{{
				Target:       "~/action",
				Precondition: Precondition{TargetResolution: test.actionResolution},
				OnSuccess:    StateEffect{Kind: test.effect},
			}}

			err := validateFileStateTopology(profile, actions)
			if test.wantRelation == "" {
				if err != nil {
					t.Fatalf("validateFileStateTopology() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, paths.ErrTargetOverlap) || !strings.Contains(err.Error(), test.wantRelation) {
				t.Fatalf(
					"validateFileStateTopology() error = %v, want ErrTargetOverlap containing %q",
					err,
					test.wantRelation,
				)
			}
		})
	}
}

func TestValidateFileStateTopology_MatchedHistoryPrefixes(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "home")
	realRoot := filepath.Join(home, "real")
	if err := os.MkdirAll(realRoot, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(real root) error = %v", err)
	}
	if err := os.Symlink("real", filepath.Join(home, "alias")); err != nil {
		t.Fatalf("os.Symlink(alias) error = %v", err)
	}
	writeApplyFile(t, filepath.Join(realRoot, "child"), "user data\n")
	resolve := func(path string) paths.TargetResolution {
		t.Helper()
		resolution, err := paths.ResolveTarget(path)
		if err != nil {
			t.Fatalf("ResolveTarget(%q) error = %v", path, err)
		}
		return resolution
	}
	alias := resolve(filepath.Join(home, "alias"))
	historicalChild := resolve(filepath.Join(home, "alias", "child"))
	desiredChild := resolve(filepath.Join(realRoot, "child"))
	unrelated := resolve(filepath.Join(home, "00"))
	if !historicalChild.Equal(desiredChild) ||
		alias.IsAncestorOf(desiredChild) || !alias.IsAncestorOf(historicalChild) {
		t.Fatal("fixture does not distinguish matched historical and desired ancestor trails")
	}

	profile := ObservedProfile{targets: []ObservedTarget{{
		Desired:              Desired{Target: "~/real/child"},
		Resolution:           desiredChild,
		State:                HistoricalState{Key: "~/alias/child"},
		HistoricalResolution: historicalChild,
		HasState:             true,
	}}}
	upsert := func(key, previousKey string, resolution paths.TargetResolution) FileAction {
		return FileAction{
			Target:       key,
			Precondition: Precondition{TargetResolution: resolution},
			OnSuccess: StateEffect{
				Kind:        StateUpsert,
				Key:         key,
				PreviousKey: previousKey,
			},
		}
	}
	preserve := FileAction{Target: "~/real/child", OnSuccess: StateEffect{Kind: StatePreserve}}
	parentUpsert := upsert("~/alias", "", alias)
	childMigration := upsert("~/real/child", "~/alias/child", desiredChild)
	unrelatedUpsert := upsert("~/00", "", unrelated)

	tests := []struct {
		name        string
		actions     []FileAction
		wantOverlap bool
	}{
		{
			name:        "matched preserve retains alias key",
			actions:     []FileAction{parentUpsert, preserve},
			wantOverlap: true,
		},
		{
			name:        "partial scope omits matched action",
			actions:     []FileAction{parentUpsert},
			wantOverlap: true,
		},
		{
			name:        "late migration cannot repair unsafe prefix",
			actions:     []FileAction{unrelatedUpsert, parentUpsert, childMigration},
			wantOverlap: true,
		},
		{
			name:    "early migration removes alias key",
			actions: []FileAction{childMigration, parentUpsert},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateFileStateTopology(profile, test.actions)
			if test.wantOverlap {
				if !errors.Is(err, paths.ErrTargetOverlap) ||
					!strings.Contains(err.Error(), "~/alias") ||
					!strings.Contains(err.Error(), "~/alias/child") {
					t.Fatalf("validateFileStateTopology() error = %v, want matched alias overlap", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateFileStateTopology() error = %v, want nil", err)
			}
		})
	}
}

type fileCompositionFixture struct {
	root        string
	home        string
	repository  string
	validated   manifest.ValidatedProfile
	context     manifest.RuntimeContext
	loadedState state.Loaded
}

type applyIntegrationFixture struct {
	root       string
	home       string
	repository string
	configPath string
	statePath  string
}

func newPruneAncestorFixture(
	t *testing.T,
	historical applyStateEntry,
	childConflict bool,
) applyIntegrationFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	configPath := filepath.Join(home, ".config", "dot", "config.toml")
	statePath := filepath.Join(home, ".local", "state", "dot", "state.json")
	writeApplyFile(t, configPath, "profile = \"all\"\n")
	writeApplyFile(t, filepath.Join(repository, "dot.toml"), `requires = ">=0.0.0"
[profiles]
all = ["cleanup", "app"]
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "cleanup", "dot.toml"), `target = "~"
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "app", "dot.toml"), `target = "~/dir"
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "app", "child"), "wanted\n")
	if err := os.MkdirAll(filepath.Join(home, "owned"), 0o755); err != nil {
		t.Fatalf("MkdirAll(home/owned) error = %v", err)
	}
	if err := os.Symlink("owned", filepath.Join(home, "dir")); err != nil {
		t.Fatalf("Symlink(home/dir) error = %v", err)
	}
	if childConflict {
		writeApplyFile(t, filepath.Join(home, "owned", "child"), "user data\n")
	}
	writeApplyState(t, statePath, map[string]applyStateEntry{"~/dir": historical}, nil)
	return applyIntegrationFixture{
		root:       root,
		home:       home,
		repository: repository,
		configPath: configPath,
		statePath:  statePath,
	}
}

func newFileUpsertStateTopologyFixture(
	t *testing.T,
	childConflict bool,
	scaffold bool,
) applyIntegrationFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	configPath := filepath.Join(home, ".config", "dot", "config.toml")
	statePath := filepath.Join(home, ".local", "state", "dot", "state.json")
	writeApplyFile(t, configPath, "profile = \"all\"\n")
	writeApplyFile(t, filepath.Join(repository, "dot.toml"), `requires = ">=0.0.0"
[profiles]
all = ["app"]
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "app", "dot.toml"), `target = "~"
`)
	parentSource := "parent"
	if scaffold {
		parentSource = "parent.template"
	}
	writeApplyFile(t, filepath.Join(repository, "modules", "app", parentSource), "source content\n")
	if childConflict {
		writeApplyFile(t, filepath.Join(repository, "modules", "app", "blocked"), "wanted\n")
		writeApplyFile(t, filepath.Join(home, "blocked"), "user data\n")
	}
	writeApplyState(t, statePath, map[string]applyStateEntry{
		"~/parent/child": {
			Module: "legacy",
			Kind:   "scaffold",
			Source: "modules/legacy/child.template",
		},
	}, nil)
	return applyIntegrationFixture{
		root:       root,
		home:       home,
		repository: repository,
		configPath: configPath,
		statePath:  statePath,
	}
}

func newFileAdoptStateTopologyFixture(t *testing.T) applyIntegrationFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	configPath := filepath.Join(home, ".config", "dot", "config.toml")
	statePath := filepath.Join(home, ".local", "state", "dot", "state.json")
	writeApplyFile(t, configPath, "profile = \"all\"\n")
	writeApplyFile(t, filepath.Join(repository, "dot.toml"), `requires = ">=0.0.0"
[profiles]
all = ["app"]
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "app", "dot.toml"), `target = "~/parent"
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "app", "child.template"), "scaffold source\n")
	writeApplyFile(t, filepath.Join(home, "parent", "child"), "existing user data\n")
	writeApplyState(t, statePath, map[string]applyStateEntry{
		"~/parent": {
			Module: "legacy",
			Kind:   "scaffold",
			Source: "modules/legacy/parent.template",
		},
	}, nil)
	return applyIntegrationFixture{
		root:       root,
		home:       home,
		repository: repository,
		configPath: configPath,
		statePath:  statePath,
	}
}

func newApplyIntegrationFixture(t *testing.T) applyIntegrationFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	configPath := filepath.Join(home, ".config", "dot", "config.toml")
	statePath := filepath.Join(home, ".local", "state", "dot", "state.json")
	writeApplyFile(t, configPath, "profile = \"all\"\n")
	writeApplyFile(t, filepath.Join(repository, "dot.toml"), `requires = ">=0.0.0"
[profiles]
all = ["beta", "alpha"]
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "alpha", "dot.toml"), `target = "~/alpha"
[hooks]
run_once = ["hooks/setup.sh"]
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "alpha", "conflict.txt"), "wanted conflict\n")
	writeApplyFile(t, filepath.Join(repository, "modules", "alpha", "stable.txt"), "stable\n")
	writeApplyFile(t, filepath.Join(repository, "modules", "alpha", "hooks", "setup.sh"), "#!/bin/sh\nexit 99\n")
	writeApplyFile(t, filepath.Join(repository, "modules", "alpha", "hooks", "old.txt"), "old\n")
	if err := os.Chmod(filepath.Join(repository, "modules", "alpha", "hooks", "setup.sh"), 0o755); err != nil {
		t.Fatalf("Chmod(alpha hook) error = %v", err)
	}

	writeApplyFile(t, filepath.Join(repository, "modules", "beta", "dot.toml"), `target = "~/beta"
[hooks]
run_once = ["hooks/setup.sh"]
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "beta", "other.txt"), "other\n")
	writeApplyFile(t, filepath.Join(repository, "modules", "beta", "hooks", "setup.sh"), "#!/bin/sh\nexit 98\n")

	// 未被任何 profile 引用，只供后续 status/presentation input 使用。
	writeApplyFile(t, filepath.Join(repository, "modules", "unused", "note.txt"), "unused\n")
	if err := os.MkdirAll(filepath.Join(home, "alpha"), 0o755); err != nil {
		t.Fatalf("MkdirAll(home/alpha) error = %v", err)
	}
	writeApplyFile(t, filepath.Join(home, "alpha", "conflict.txt"), "user data\n")
	alphaOldSource := filepath.Join(repository, "modules", "alpha", "hooks", "old.txt")
	if err := os.Symlink(alphaOldSource, filepath.Join(home, "alpha", "obsolete")); err != nil {
		t.Fatalf("Symlink(alpha orphan) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, "legacy"), 0o755); err != nil {
		t.Fatalf("MkdirAll(home/legacy) error = %v", err)
	}
	legacyDest := filepath.Join(repository, "modules", "unused", "note.txt")
	if err := os.Symlink(legacyDest, filepath.Join(home, "legacy", "old")); err != nil {
		t.Fatalf("Symlink(legacy orphan) error = %v", err)
	}
	writeApplyState(t, statePath, map[string]applyStateEntry{
		"~/alpha/obsolete": {
			Module:   "alpha",
			Kind:     "symlink",
			Source:   "modules/alpha/hooks/old.txt",
			LinkDest: alphaOldSource,
		},
		"~/legacy/old": {
			Module:   "legacy",
			Kind:     "symlink",
			Source:   "modules/legacy/old.txt",
			LinkDest: legacyDest,
		},
	}, map[string]applyRunOnce{
		"alpha/hooks/setup.sh": {Hash: "sha256:" + strings.Repeat("0", 64)},
		"beta/hooks/setup.sh":  {Hash: "sha256:" + strings.Repeat("1", 64)},
	})
	return applyIntegrationFixture{
		root:       root,
		home:       home,
		repository: repository,
		configPath: configPath,
		statePath:  statePath,
	}
}

func (fixture applyIntegrationFixture) redirectEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DOT_CONFIG", fixture.configPath)
	t.Setenv("DOT_REPO", fixture.repository)
}

func (fixture applyIntegrationFixture) options() ApplyOptions {
	return ApplyOptions{
		Runtime: dotruntime.Overrides{
			Home:       dotruntime.Override{Value: fixture.home, Set: true},
			Repository: dotruntime.Override{Value: fixture.repository, Set: true},
		},
		CLIVersion: "dev",
	}
}

func applyFileActionKeys(actions []FileAction) []string {
	keys := make([]string, len(actions))
	for index, action := range actions {
		keys[index] = action.Desired.Module + "/" + action.Desired.Source
	}
	return keys
}

func fileActionIndex(t *testing.T, actions []FileAction, verb FileVerb) int {
	t.Helper()
	for index, action := range actions {
		if action.Verb == verb {
			return index
		}
	}
	t.Fatalf("fixture has no %q file action", verb)
	return -1
}

func pruneActionIndex(t *testing.T, actions []PruneAction, reason PruneReason) int {
	t.Helper()
	for index, action := range actions {
		if action.Reason == reason {
			return index
		}
	}
	t.Fatalf("fixture has no %q prune action", reason)
	return -1
}

func applyHookActionKeys(actions []HookAction) []string {
	keys := make([]string, len(actions))
	for index, action := range actions {
		keys[index] = action.StateKey
	}
	return keys
}

func newFileCompositionFixture(t *testing.T) fileCompositionFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(home, "real"), 0o755); err != nil {
		t.Fatalf("MkdirAll(home/real) error = %v", err)
	}
	if err := os.Symlink("real", filepath.Join(home, "alias")); err != nil {
		t.Fatalf("Symlink(home/alias) error = %v", err)
	}

	writeApplyFile(t, filepath.Join(repository, "dot.toml"), `requires = ">=0.0.0"
[profiles]
all = ["beta", "alpha"]
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "alpha", "dot.toml"), `target = "~/alias"
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "alpha", "item.template"), `profile={{ .Profile }}`)
	writeApplyFile(t, filepath.Join(repository, "modules", "alpha", "hooks", "old.txt"), "old\n")
	writeApplyFile(t, filepath.Join(repository, "modules", "beta", "dot.toml"), `target = "~/beta"
`)
	writeApplyFile(t, filepath.Join(repository, "modules", "beta", "broken.template"), `{{`)

	oldSource := filepath.Join(repository, "modules", "alpha", "hooks", "old.txt")
	if err := os.Symlink(oldSource, filepath.Join(home, "real", "item")); err != nil {
		t.Fatalf("Symlink(target item) error = %v", err)
	}
	loadedState := writeApplyState(t, filepath.Join(root, "state.json"), map[string]applyStateEntry{
		"~/real/item": {
			Module:   "alpha",
			Kind:     "symlink",
			Source:   "modules/alpha/hooks/old.txt",
			LinkDest: oldSource,
		},
	}, nil)

	loaded, err := manifest.Load(repository)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	resolved, err := loaded.Resolve("all", runtime.GOOS)
	if err != nil {
		t.Fatalf("Repository.Resolve() error = %v", err)
	}
	control, err := paths.ResolveControlPlanePaths(
		home,
		repository,
		filepath.Join(home, ".config", "dot", "config.toml"),
	)
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}
	validated, err := resolved.ValidatePathBoundaries(control)
	if err != nil {
		t.Fatalf("ValidatePathBoundaries() error = %v", err)
	}
	context := manifest.RuntimeContext{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Hostname: "apply-test",
		Profile:  "all",
		Home:     home,
		Data:     map[string]string{},
	}
	return fileCompositionFixture{
		root:        root,
		home:        home,
		repository:  repository,
		validated:   validated,
		context:     context,
		loadedState: loadedState,
	}
}

type applyStateEntry struct {
	Module   string
	Kind     string
	Source   string
	LinkDest string
	Hash     string
}

type applyRunOnce struct {
	Hash string
}

func writeApplyState(
	t *testing.T,
	path string,
	entries map[string]applyStateEntry,
	runOnce map[string]applyRunOnce,
) state.Loaded {
	t.Helper()
	type wireEntry struct {
		Module    string  `json:"module"`
		Kind      string  `json:"kind"`
		Source    string  `json:"source"`
		LinkDest  *string `json:"link_dest,omitempty"`
		Hash      *string `json:"hash,omitempty"`
		AppliedAt string  `json:"applied_at"`
	}
	type wireRunOnce struct {
		Hash       string `json:"hash"`
		ExecutedAt string `json:"executed_at"`
	}
	document := struct {
		Version int                    `json:"version"`
		Entries map[string]wireEntry   `json:"entries"`
		RunOnce map[string]wireRunOnce `json:"run_once"`
	}{
		Version: 1,
		Entries: make(map[string]wireEntry, len(entries)),
		RunOnce: make(map[string]wireRunOnce, len(runOnce)),
	}
	for target, entry := range entries {
		wire := wireEntry{
			Module:    entry.Module,
			Kind:      entry.Kind,
			Source:    entry.Source,
			AppliedAt: "2026-07-19T00:00:00Z",
		}
		if entry.LinkDest != "" {
			wire.LinkDest = &entry.LinkDest
		}
		if entry.Hash != "" {
			wire.Hash = &entry.Hash
		}
		document.Entries[target] = wire
	}
	for key, record := range runOnce {
		document.RunOnce[key] = wireRunOnce{
			Hash:       record.Hash,
			ExecutedAt: "2026-07-19T00:00:00Z",
		}
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("json.Marshal(state) error = %v", err)
	}
	writeApplyFile(t, path, string(encoded))
	loaded, err := state.Load(path)
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}
	return loaded
}

func writeApplyFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func makeUnreadableRegular(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", path, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("fixture %q mode = %v, want regular", path, info.Mode())
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatalf("os.Chmod(%q, 0) error = %v", path, err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, info.Mode().Perm()); err != nil && !os.IsNotExist(err) {
			t.Errorf("restore %q mode: %v", path, err)
		}
	})
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("current process can read mode-000 files; unreadable payload scenario unavailable")
	}
}
