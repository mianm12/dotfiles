package planner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
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
	if action.Verb != ActionScaffold || action.Reason != ReasonOwnedLinkToScaffold {
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

type fileCompositionFixture struct {
	root        string
	home        string
	repository  string
	validated   manifest.ValidatedProfile
	context     manifest.RuntimeContext
	loadedState state.Loaded
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
