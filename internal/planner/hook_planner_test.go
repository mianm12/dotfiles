package planner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/state"
)

func TestPlanHook_BuildsSelfContainedActionWithoutExecution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mode        os.FileMode
		wantMode    HookExecutionMode
		wantProgram string
		wantArgs    func(string) []string
	}{
		{
			name:        "non executable uses sh",
			mode:        0o644,
			wantMode:    HookExecutionShell,
			wantProgram: "sh",
			wantArgs:    func(script string) []string { return []string{script} },
		},
		{
			name:        "user executable runs directly",
			mode:        0o744,
			wantMode:    HookExecutionDirect,
			wantProgram: "",
			wantArgs:    func(string) []string { return nil },
		},
		{
			name:        "group executable runs directly",
			mode:        0o650,
			wantMode:    HookExecutionDirect,
			wantProgram: "",
			wantArgs:    func(string) []string { return nil },
		},
		{
			name:        "other executable runs directly",
			mode:        0o601,
			wantMode:    HookExecutionDirect,
			wantProgram: "",
			wantArgs:    func(string) []string { return nil },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			home := filepath.Join(root, "home")
			repository := filepath.Join(root, "repo")
			modulePath := filepath.Join(repository, "modules", "alpha")
			scriptPath := filepath.Join(modulePath, "hooks", "setup.sh")
			marker := filepath.Join(root, "must-not-exist")
			if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
				t.Fatalf("MkdirAll(script parent) error = %v", err)
			}
			content := []byte("#!/bin/sh\ntouch " + marker + "\n")
			if err := os.WriteFile(scriptPath, content, test.mode); err != nil {
				t.Fatalf("WriteFile(script) error = %v", err)
			}
			descriptor := manifest.HookDescriptor{
				Module:         "alpha",
				ModulePath:     modulePath,
				Script:         "hooks/setup.sh",
				ScriptPath:     scriptPath,
				TargetRoot:     "~/.config/alpha",
				TargetRootPath: filepath.Join(home, ".config", "alpha"),
			}
			runtime := hookRuntime{
				Profile:    "work",
				GOOS:       "darwin",
				Home:       home,
				Repository: repository,
			}

			action, err := planHook(descriptor, runtime, "", false)
			if err != nil {
				t.Fatalf("planHook() error = %v", err)
			}
			if _, err := os.Lstat(marker); !os.IsNotExist(err) {
				t.Fatalf("planning executed hook or marker check failed: %v", err)
			}
			wantProgram := test.wantProgram
			if wantProgram == "" {
				wantProgram = scriptPath
			}
			want := HookAction{
				Verb:           HookRun,
				StateKey:       "alpha/hooks/setup.sh",
				Module:         "alpha",
				Script:         "hooks/setup.sh",
				ScriptPath:     scriptPath,
				WorkingDir:     modulePath,
				TargetRoot:     "~/.config/alpha",
				TargetRootPath: filepath.Join(home, ".config", "alpha"),
				Profile:        "work",
				GOOS:           "darwin",
				Repository:     repository,
				Invocation: HookInvocation{
					Mode:      test.wantMode,
					Program:   wantProgram,
					Arguments: test.wantArgs(scriptPath),
				},
				Environment: HookEnvironment{
					Home:          home,
					XDGConfigHome: filepath.Join(home, ".config"),
					XDGStateHome:  filepath.Join(home, ".local", "state"),
					XDGDataHome:   filepath.Join(home, ".local", "share"),
					DotModule:     "alpha",
					DotOS:         "darwin",
					DotProfile:    "work",
					DotRepo:       repository,
					DotTarget:     filepath.Join(home, ".config", "alpha"),
				},
				Fingerprint: action.Fingerprint,
				OnSuccess: HookStateEffect{
					Kind:        HookStateUpsert,
					Key:         "alpha/hooks/setup.sh",
					Fingerprint: action.Fingerprint,
				},
				OnFailure: HookStateEffect{Kind: HookStatePreserve},
			}
			if !reflect.DeepEqual(action, want) {
				t.Fatalf("planHook() = %#v, want %#v", action, want)
			}
			if !strings.HasPrefix(action.Fingerprint, "sha256:") || len(action.Fingerprint) != len("sha256:")+64 {
				t.Fatalf("fingerprint = %q, want canonical sha256", action.Fingerprint)
			}
		})
	}
}

func TestHookFingerprint_VersionedGoldenAndInputs(t *testing.T) {
	t.Parallel()

	script := []byte("#!/bin/sh\necho hello\n")
	shell := HookFingerprint(HookExecutionShell, script)
	if want := "sha256:47af394c22dca5efb8c3b5b9971d8442a1363640516202d27523780e00512195"; shell != want {
		t.Fatalf("shell fingerprint = %q, want golden %q", shell, want)
	}
	if direct := HookFingerprint(HookExecutionDirect, script); direct == shell {
		t.Fatal("execution classification did not change fingerprint")
	}
	if changed := HookFingerprint(HookExecutionShell, append(script, '#')); changed == shell {
		t.Fatal("script bytes did not change fingerprint")
	}
	if repeated := HookFingerprint(HookExecutionShell, append([]byte(nil), script...)); repeated != shell {
		t.Fatalf("same inputs produced %q, want %q", repeated, shell)
	}
}

func TestPlanHook_RejectsNonRegularOrMissingScript(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	regular := filepath.Join(root, "regular")
	if err := os.WriteFile(regular, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(regular) error = %v", err)
	}
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatalf("Mkdir(directory) error = %v", err)
	}
	symlink := filepath.Join(root, "symlink")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatalf("Symlink(regular) error = %v", err)
	}

	for _, test := range []struct {
		name string
		path string
	}{
		{name: "missing", path: filepath.Join(root, "missing")},
		{name: "directory", path: directory},
		{name: "symlink", path: symlink},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			descriptor := manifest.HookDescriptor{
				Module:         "alpha",
				ModulePath:     root,
				Script:         "hooks/setup.sh",
				ScriptPath:     test.path,
				TargetRoot:     "~",
				TargetRootPath: root,
			}
			action, err := planHook(descriptor, hookRuntime{
				Profile:    "work",
				GOOS:       "darwin",
				Home:       root,
				Repository: root,
			}, "", false)
			if err == nil {
				t.Fatal("planHook() error = nil, want failure")
			}
			if !reflect.DeepEqual(action, HookAction{}) {
				t.Fatalf("planHook() action = %#v, want zero action", action)
			}
		})
	}
}

func TestHookActionClone_DoesNotShareArguments(t *testing.T) {
	t.Parallel()

	action := HookAction{Invocation: HookInvocation{Arguments: []string{"script"}}}
	cloned := action.Clone()
	cloned.Invocation.Arguments[0] = "changed"
	if action.Invocation.Arguments[0] != "script" {
		t.Fatalf("mutating clone changed source action: %#v", action)
	}
}

func TestPlanHooks_PreservesScopeOrderAndRunOnceHistory(t *testing.T) {
	t.Parallel()

	fixture := newHookPlanFixture(t)
	missing := loadMissingHookState(t, fixture.root)
	initial, err := PlanHooks(fixture.full, missing, fixture.repository)
	if err != nil {
		t.Fatalf("PlanHooks(missing state) error = %v", err)
	}
	initialActions := initial.Actions()
	wantKeys := []string{
		"alpha/hooks/second.sh",
		"alpha/hooks/first.sh",
		"beta/hooks/only.sh",
	}
	if got := hookActionKeys(initialActions); !reflect.DeepEqual(got, wantKeys) {
		t.Fatalf("initial hook order = %v, want %v", got, wantKeys)
	}
	for _, action := range initialActions {
		if action.Verb != HookRun || action.OnSuccess.Kind != HookStateUpsert || action.OnFailure.Kind != HookStatePreserve {
			t.Fatalf("missing-state action = %#v, want run/upsert/preserve", action)
		}
	}

	loaded := loadHookState(t, fixture.root, map[string]string{
		"alpha/hooks/second.sh": initialActions[0].Fingerprint,
		"alpha/hooks/first.sh":  "sha256:" + strings.Repeat("0", 64),
		"legacy/hooks/old.sh":   "sha256:" + strings.Repeat("1", 64),
	})
	planned, err := PlanHooks(fixture.full, loaded, fixture.repository)
	if err != nil {
		t.Fatalf("PlanHooks(loaded state) error = %v", err)
	}
	actions := planned.Actions()
	if got := hookActionKeys(actions); !reflect.DeepEqual(got, wantKeys) {
		t.Fatalf("loaded hook order = %v, want %v", got, wantKeys)
	}
	wantVerbs := []HookVerb{HookSkip, HookRun, HookRun}
	for index, action := range actions {
		if action.Verb != wantVerbs[index] {
			t.Fatalf("action %q verb = %q, want %q", action.StateKey, action.Verb, wantVerbs[index])
		}
		if action.OnFailure.Kind != HookStatePreserve {
			t.Fatalf("action %q failure effect = %#v, want preserve", action.StateKey, action.OnFailure)
		}
	}
	if actions[0].OnSuccess.Kind != HookStatePreserve {
		t.Fatalf("same-fingerprint success effect = %#v, want preserve", actions[0].OnSuccess)
	}
	for _, action := range actions[1:] {
		if action.OnSuccess.Kind != HookStateUpsert || action.OnSuccess.Key != action.StateKey || action.OnSuccess.Fingerprint != action.Fingerprint {
			t.Fatalf("run action state effect is incomplete: %#v", action)
		}
	}
	// 历史 legacy key 不生成 delete action；当前 scope 只有三个 manifest hook。
	if len(actions) != 3 {
		t.Fatalf("PlanHooks() action count = %d, want 3 without historical cleanup", len(actions))
	}
}

func TestPlanHooks_PartialScopeExcludesOtherModule(t *testing.T) {
	t.Parallel()

	fixture := newHookPlanFixture(t)
	loaded := loadHookState(t, fixture.root, map[string]string{
		"beta/hooks/only.sh": "sha256:" + strings.Repeat("0", 64),
	})
	planned, err := PlanHooks(fixture.alpha, loaded, fixture.repository)
	if err != nil {
		t.Fatalf("PlanHooks(alpha) error = %v", err)
	}
	if got, want := hookActionKeys(planned.Actions()), []string{
		"alpha/hooks/second.sh",
		"alpha/hooks/first.sh",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("partial hook keys = %v, want %v", got, want)
	}
}

func TestPlanHooks_FailFastReturnsNoPartialActions(t *testing.T) {
	t.Parallel()

	fixture := newHookPlanFixture(t)
	// alpha 的声明顺序是 second、first；让第二项 first 在 plan 前失效，证明已形成的首项不泄漏。
	if err := os.Remove(filepath.Join(fixture.repository, "modules", "alpha", "hooks", "first.sh")); err != nil {
		t.Fatalf("Remove(second hook candidate) error = %v", err)
	}
	planned, err := PlanHooks(fixture.alpha, loadMissingHookState(t, fixture.root), fixture.repository)
	if err == nil {
		t.Fatal("PlanHooks() error = nil, want fail-fast script error")
	}
	if actions := planned.Actions(); actions != nil {
		t.Fatalf("failed PlanHooks() actions = %#v, want nil", actions)
	}
}

func TestPlanHooks_RejectsInvalidStateWithoutReadingScripts(t *testing.T) {
	t.Parallel()

	fixture := newHookPlanFixture(t)
	script := filepath.Join(fixture.repository, "modules", "alpha", "hooks", "second.sh")
	if err := os.Remove(script); err != nil {
		t.Fatalf("Remove(script) error = %v", err)
	}
	planned, err := PlanHooks(fixture.alpha, state.Loaded{}, fixture.repository)
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("PlanHooks(invalid state) error = %v, want state error", err)
	}
	if actions := planned.Actions(); actions != nil {
		t.Fatalf("failed PlanHooks() actions = %#v, want nil", actions)
	}
}

type hookPlanFixture struct {
	root       string
	home       string
	repository string
	full       manifest.ScopedProfile
	alpha      manifest.ScopedProfile
}

func newHookPlanFixture(t *testing.T) hookPlanFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repository")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll(home) error = %v", err)
	}
	writeHookManifestFile(t, filepath.Join(repository, "dot.toml"), `requires = ">=0.0.0"
[profiles]
all = ["beta", "alpha"]
`)
	writeHookModule(t, repository, "alpha", []string{"hooks/second.sh", "hooks/first.sh"})
	writeHookModule(t, repository, "beta", []string{"hooks/only.sh"})

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
		Hostname: "test-host",
		Profile:  "all",
		Home:     home,
	}
	full, err := validated.RenderScope(nil, context)
	if err != nil {
		t.Fatalf("RenderScope(full) error = %v", err)
	}
	alpha, err := validated.RenderScope([]string{"alpha"}, context)
	if err != nil {
		t.Fatalf("RenderScope(alpha) error = %v", err)
	}
	return hookPlanFixture{root: root, home: home, repository: repository, full: full, alpha: alpha}
}

func writeHookModule(t *testing.T, repository, module string, scripts []string) {
	t.Helper()
	modulePath := filepath.Join(repository, "modules", module)
	manifestText := "target = \"~/" + module + "\"\n[hooks]\nrun_once = ["
	for index, script := range scripts {
		if index > 0 {
			manifestText += ", "
		}
		manifestText += "\"" + script + "\""
	}
	manifestText += "]\n"
	writeHookManifestFile(t, filepath.Join(modulePath, "dot.toml"), manifestText)
	for index, script := range scripts {
		mode := os.FileMode(0o644)
		if index%2 == 0 {
			mode = 0o755
		}
		writeHookManifestFile(t, filepath.Join(modulePath, filepath.FromSlash(script)), "#!/bin/sh\nexit 99\n")
		if err := os.Chmod(filepath.Join(modulePath, filepath.FromSlash(script)), mode); err != nil {
			t.Fatalf("Chmod(%s/%s) error = %v", module, script, err)
		}
	}
}

func writeHookManifestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func loadMissingHookState(t *testing.T, root string) state.Loaded {
	t.Helper()
	loaded, err := state.Load(filepath.Join(root, "missing-state.json"))
	if err != nil {
		t.Fatalf("state.Load(missing) error = %v", err)
	}
	return loaded
}

func loadHookState(t *testing.T, root string, records map[string]string) state.Loaded {
	t.Helper()
	type wireRecord struct {
		Hash       string `json:"hash"`
		ExecutedAt string `json:"executed_at"`
	}
	document := struct {
		Version int                   `json:"version"`
		Entries map[string]any        `json:"entries"`
		RunOnce map[string]wireRecord `json:"run_once"`
	}{Version: 1, Entries: map[string]any{}, RunOnce: make(map[string]wireRecord, len(records))}
	for key, fingerprint := range records {
		document.RunOnce[key] = wireRecord{Hash: fingerprint, ExecutedAt: "2026-07-19T00:00:00Z"}
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("json.Marshal(state) error = %v", err)
	}
	path := filepath.Join(root, "state.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("WriteFile(state) error = %v", err)
	}
	loaded, err := state.Load(path)
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}
	return loaded
}

func hookActionKeys(actions []HookAction) []string {
	keys := make([]string, len(actions))
	for index, action := range actions {
		keys[index] = action.StateKey
	}
	return keys
}
