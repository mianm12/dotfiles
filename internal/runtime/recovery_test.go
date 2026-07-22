package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/lock"
	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/state"
)

func TestInitSession_ConfigMissingLoadsManifestAfterLockAndSkipsState(t *testing.T) {
	fixture := newLoadingFixture(t, false)
	writeState(t, fixture, "{")

	operations := defaultLoadingOperations()
	events := wrapInitEvents(&operations)
	session, err := beginInit(fixture.overrides, operations)
	if err != nil {
		t.Fatalf("beginInit() error = %v", err)
	}
	t.Cleanup(func() { closeInitSession(t, session) })
	result, err := session.Load("v1.0.0")
	if err != nil {
		t.Fatalf("InitSession.Load() error = %v", err)
	}
	if !result.Context().ConfigMissing() {
		t.Fatal("ConfigMissing() = false, want true")
	}
	if got := result.Compatibility().Requirement().String(); got != ">=1.0.0" {
		t.Fatalf("Requirement = %q", got)
	}
	want := []string{"init-preflight", "acquire", "requires", "satisfies", "manifest", "satisfies"}
	if !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
}

func TestBeginInit_ExistingInvalidConfigFailsBeforeLock(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeFile(t, fixture.config, []byte("unknown = true\n"), 0o600)

	session, err := BeginInit(fixture.overrides)
	if err == nil {
		t.Fatal("BeginInit() error = nil")
	}
	if session != nil {
		t.Fatal("invalid config returned an init session")
	}
	assertMissing(t, fixture.paths.StateRoot())
}

func TestInitSession_ManifestFailureKeepsLockAndSkipsState(t *testing.T) {
	fixture := newLoadingFixture(t, false)
	writeManifest(t, fixture.repo, ">=1.0.0", "unknown = true\n")
	writeState(t, fixture, "{")

	session, err := BeginInit(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginInit() error = %v", err)
	}
	_, err = session.Load("v1.0.0")
	if err == nil || errors.Is(err, state.ErrCorrupt) {
		t.Fatalf("InitSession.Load() error = %v, want manifest error before state", err)
	}
	assertLockBusy(t, fixture)
	closeInitSession(t, session)
	assertLockAvailable(t, fixture)
}

func TestRecoverySession_SkipsManifestAndStateButHoldsLock(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeManifest(t, fixture.repo, "invalid", "unknown = true\n")
	writeState(t, fixture, "{")

	operations := defaultLoadingOperations()
	events := wrapRecoveryEvents(&operations)
	session, err := beginRecovery(fixture.overrides, operations)
	if err != nil {
		t.Fatalf("beginRecovery() error = %v", err)
	}
	t.Cleanup(func() { closeRecoverySession(t, session) })
	context, err := session.Context()
	if err != nil {
		t.Fatalf("RecoverySession.Context() error = %v", err)
	}
	if context.RepositoryPath() != fixture.repo {
		t.Fatalf("RepositoryPath() = %q, want %q", context.RepositoryPath(), fixture.repo)
	}
	if want := []string{"repository-preflight", "acquire"}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
	assertLockBusy(t, fixture)
}

func TestBeginRecovery_StateFailClosedVariantsDoNotBlockRecovery(t *testing.T) {
	tests := []struct {
		name  string
		state string
	}{
		{name: "corrupt", state: "{"},
		{name: "too new", state: `{"version":2,"entries":{},"run_once":{}}`},
		{
			name:  "unsupported rendered",
			state: `{"version":1,"entries":{"~/.config/app/config":{"module":"app","kind":"rendered","source":"modules/app/.config/app/config.tmpl","hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","applied_at":"2026-07-19T00:00:00Z"}},"run_once":{}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLoadingFixture(t, false)
			writeState(t, fixture, test.state)

			session, err := BeginRecovery(fixture.overrides)
			if err != nil {
				t.Fatalf("BeginRecovery() error = %v", err)
			}
			closeRecoverySession(t, session)
		})
	}
}

func TestBeginRecovery_AllowsMissingConfig(t *testing.T) {
	fixture := newLoadingFixture(t, false)

	session, err := BeginRecovery(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginRecovery() error = %v", err)
	}
	t.Cleanup(func() { closeRecoverySession(t, session) })
	context, err := session.Context()
	if err != nil {
		t.Fatalf("RecoverySession.Context() error = %v", err)
	}
	if context.RepositoryPath() != fixture.repo {
		t.Fatalf("RepositoryPath() = %q, want %q", context.RepositoryPath(), fixture.repo)
	}
}

func TestBeginRecovery_ExistingInvalidConfigFailsBeforeLock(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeFile(t, fixture.config, []byte("unknown = true\n"), 0o600)

	session, err := BeginRecovery(fixture.overrides)
	if err == nil {
		t.Fatal("BeginRecovery() error = nil")
	}
	if session != nil {
		t.Fatal("invalid config returned a recovery session")
	}
	assertMissing(t, fixture.paths.StateRoot())
}

func TestLoadControlRecovery_AllowsMissingConfigAndDoesNotLockOrReadState(t *testing.T) {
	fixture := newLoadingFixture(t, false)
	writeManifest(t, fixture.repo, "invalid", "unknown = true\n")
	writeState(t, fixture, "{")
	before, err := os.ReadFile(fixture.paths.StateFile())
	if err != nil {
		t.Fatalf("os.ReadFile(state) error = %v", err)
	}
	beforeTree := snapshotFixtureTree(t, fixture.root)

	context, err := LoadControlRecovery(fixture.overrides)
	if err != nil {
		t.Fatalf("LoadControlRecovery() error = %v", err)
	}
	if context.RepositoryPath() != fixture.repo {
		t.Fatalf("RepositoryPath() = %q, want %q", context.RepositoryPath(), fixture.repo)
	}
	after, err := os.ReadFile(fixture.paths.StateFile())
	if err != nil {
		t.Fatalf("os.ReadFile(state) after error = %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("control recovery changed state bytes")
	}
	afterTree := snapshotFixtureTree(t, fixture.root)
	if !reflect.DeepEqual(afterTree, beforeTree) {
		t.Fatalf("LoadControlRecovery() changed fixture tree\nbefore: %#v\nafter:  %#v", beforeTree, afterTree)
	}
	assertMissing(t, fixture.paths.StateLock())
}

func TestLoadControlRecovery_ExistingInvalidConfigFailsWithoutLock(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeFile(t, fixture.config, []byte("unknown = true\n"), 0o600)

	_, err := LoadControlRecovery(fixture.overrides)
	if err == nil {
		t.Fatal("LoadControlRecovery() error = nil")
	}
	assertMissing(t, fixture.paths.StateRoot())
}

func TestPrepareInit_LoadsStrictConfigAndManifestWithoutLockOrState(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeFile(t, fixture.config, []byte("profile = \"mac\"\nrepo = \""+fixture.repo+"\"\n[data]\nold = \"kept\"\n"), 0o640)
	writeManifest(t, fixture.repo, ">=1.0.0", "[data.email]\ndefault = \"\"\n")
	writeState(t, fixture, "{")
	before := snapshotFixtureTree(t, fixture.root)

	prepared, err := PrepareInit(fixture.overrides, "v1.0.0")
	if err != nil {
		t.Fatalf("PrepareInit() error = %v", err)
	}
	machine, ok := prepared.Context().ExistingMachine()
	if !ok || machine.Profile() != "mac" || machine.Data()["old"] != "kept" {
		t.Fatalf("ExistingMachine() = (%#v, %t), want complete old machine", machine, ok)
	}
	if repo, ok := machine.Repo(); !ok || repo != fixture.repo {
		t.Fatalf("ExistingMachine().Repo() = (%q, %t), want existing repo", repo, ok)
	}
	if prepared.Context().RepositorySource() != paths.RepositorySourceFlag {
		t.Fatalf("RepositorySource() = %q, want flag", prepared.Context().RepositorySource())
	}
	declarations := prepared.Manifest().DataDeclarations()
	if len(declarations) != 1 || declarations[0].Key() != "email" {
		t.Fatalf("manifest declarations = %#v, want only email", declarations)
	}
	if after := snapshotFixtureTree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("PrepareInit() changed fixture tree\nbefore: %#v\nafter: %#v", before, after)
	}
	assertMissing(t, fixture.paths.StateLock())
}

func TestPrepareInit_InvalidConfigDoesNotFallback(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeFile(t, fixture.config, []byte("unknown = true\n"), 0o600)
	if _, err := PrepareInit(fixture.overrides, "v1.0.0"); err == nil {
		t.Fatal("PrepareInit() error = nil, want strict config error")
	}
	assertMissing(t, fixture.paths.StateRoot())
}

func TestInitInputs_BuildCandidateMergesSelectionsAndPreservesOldFields(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeFile(t, fixture.config, []byte("profile = \"mac\"\nrepo = \"~/old-repo\"\n[data]\nemail = \"old@example.com\"\nlegacy = \"kept\"\n"), 0o600)
	writeManifest(t, fixture.repo, ">=1.0.0", "[data.email]\n[data.empty]\ndefault = \"fallback\"\n")
	prepared, err := PrepareInit(fixture.overrides, "v1.0.0")
	if err != nil {
		t.Fatalf("PrepareInit() error = %v", err)
	}
	candidate, err := prepared.BuildCandidate(InitSelection{Data: map[string]Override{
		"empty": {Value: "", Set: true},
	}})
	if err != nil {
		t.Fatalf("BuildCandidate() error = %v", err)
	}
	machine := candidate.Machine()
	if machine.Profile != "mac" || machine.Repo == nil || *machine.Repo != fixture.repo {
		t.Fatalf("candidate machine = %#v, want mac with explicit override repo %q", machine, fixture.repo)
	}
	if machine.Data["email"] != "old@example.com" || machine.Data["empty"] != "" || machine.Data["legacy"] != "kept" {
		t.Fatalf("candidate data = %#v, want old/default selections plus legacy", machine.Data)
	}
	if strings.Contains(string(candidate.Bytes()), "old-repo") {
		t.Fatalf("candidate bytes retained old repo despite explicit runtime override: %s", candidate.Bytes())
	}
}

func TestInitInputs_BuildCandidateDefaultsAndRejectsIncompleteOrUnknown(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(home, ".local", "share", "dot", "repo")
	configPath := filepath.Join(root, "machine", "config.toml")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(repo) error = %v", err)
	}
	writeManifest(t, repo, ">=1.0.0", "[data.optional]\ndefault = \"value\"\n[data.required]\n")
	resolver := NewResolver(lookup(map[string]string{paths.ConfigEnvironment: configPath}), fixedHome(home))
	prepared, err := resolver.PrepareInit(Overrides{Home: Override{Value: home, Set: true}}, "v1.0.0")
	if err != nil {
		t.Fatalf("PrepareInit() error = %v", err)
	}

	if _, err := prepared.BuildCandidate(InitSelection{}); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("BuildCandidate(missing profile) error = %v", err)
	}
	selection := InitSelection{
		Profile: Override{Value: "mac", Set: true},
		Data:    map[string]Override{"required": {Value: "set", Set: true}},
	}
	candidate, err := prepared.BuildCandidate(selection)
	if err != nil {
		t.Fatalf("BuildCandidate(defaults) error = %v", err)
	}
	machine := candidate.Machine()
	if machine.Repo != nil || machine.Data["optional"] != "value" || machine.Data["required"] != "set" {
		t.Fatalf("candidate machine = %#v, want omitted default repo and complete data", machine)
	}
	selection.Data["unknown"] = Override{Value: "x", Set: true}
	if _, err := prepared.BuildCandidate(selection); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("BuildCandidate(unknown data) error = %v", err)
	}
}

func TestInitInputs_BuildCandidatePreservesConfiguredRepoWhenOverrideOmitted(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	configPath := filepath.Join(root, "machine", "config.toml")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(repo) error = %v", err)
	}
	writeManifest(t, repo, ">=1.0.0", "")
	writeFile(t, configPath, []byte("profile = \"mac\"\nrepo = \""+repo+"\"\n"), 0o600)
	resolver := NewResolver(lookup(map[string]string{paths.ConfigEnvironment: configPath}), fixedHome(home))
	prepared, err := resolver.PrepareInit(Overrides{Home: Override{Value: home, Set: true}}, "v1.0.0")
	if err != nil {
		t.Fatalf("PrepareInit() error = %v", err)
	}
	candidate, err := prepared.BuildCandidate(InitSelection{})
	if err != nil {
		t.Fatalf("BuildCandidate() error = %v", err)
	}
	configured, ok := candidate.Machine().Repo, false
	if configured != nil {
		ok = *configured == repo
	}
	if !ok {
		t.Fatalf("candidate repo = %#v, want preserved %q", configured, repo)
	}
}

func TestRecoverySession_NestedMutationFailureKeepsExplicitOwnership(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, "{")

	outer, err := BeginRecovery(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginRecovery() error = %v", err)
	}
	nested, err := outer.BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("RecoverySession.BeginMutation() error = %v", err)
	}
	_, err = nested.Load("v1.0.0")
	if !errors.Is(err, state.ErrCorrupt) {
		t.Fatalf("MutationSession.Load() error = %v, want ErrCorrupt", err)
	}
	assertLockBusy(t, fixture)
	closeMutationSession(t, nested)
	assertLockBusy(t, fixture)
	closeRecoverySession(t, outer)
	assertLockAvailable(t, fixture)
}

func TestRecoverySession_ClosingOuterKeepsNestedOwnership(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	outer, err := BeginRecovery(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginRecovery() error = %v", err)
	}
	nested, err := outer.BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("RecoverySession.BeginMutation() error = %v", err)
	}

	closeRecoverySession(t, outer)
	assertLockBusy(t, fixture)
	if _, err := nested.Load("v1.0.0"); err != nil {
		t.Fatalf("MutationSession.Load() after outer Close error = %v", err)
	}
	closeMutationSession(t, nested)
	assertLockAvailable(t, fixture)
}

func TestRecoverySession_NestedPreflightFailureDoesNotReuseOwnership(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	operations := defaultLoadingOperations()
	events := wrapLoadingEvents(&operations)
	outer, err := beginRecovery(fixture.overrides, operations)
	if err != nil {
		t.Fatalf("beginRecovery() error = %v", err)
	}
	t.Cleanup(func() { closeRecoverySession(t, outer) })
	*events = nil
	writeFile(t, fixture.config, []byte("unknown = true\n"), 0o600)

	nested, err := outer.BeginMutation(fixture.overrides)
	if err == nil {
		t.Fatal("RecoverySession.BeginMutation() error = nil")
	}
	if nested != nil {
		t.Fatal("preflight failure returned a nested session")
	}
	if want := []string{"preflight"}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
	assertLockBusy(t, fixture)
}

func TestClosedRoleSessionsRejectFurtherUse(t *testing.T) {
	t.Run("init", func(t *testing.T) {
		fixture := newLoadingFixture(t, false)
		session, err := BeginInit(fixture.overrides)
		if err != nil {
			t.Fatalf("BeginInit() error = %v", err)
		}
		closeInitSession(t, session)
		if _, err := session.Load("v1.0.0"); !errors.Is(err, ErrSessionClosed) {
			t.Fatalf("InitSession.Load() error = %v, want ErrSessionClosed", err)
		}
		if _, err := session.BeginMutation(fixture.overrides); !errors.Is(err, ErrSessionClosed) {
			t.Fatalf("InitSession.BeginMutation() error = %v, want ErrSessionClosed", err)
		}
	})

	t.Run("recovery", func(t *testing.T) {
		fixture := newLoadingFixture(t, false)
		session, err := BeginRecovery(fixture.overrides)
		if err != nil {
			t.Fatalf("BeginRecovery() error = %v", err)
		}
		closeRecoverySession(t, session)
		if _, err := session.Context(); !errors.Is(err, ErrSessionClosed) {
			t.Fatalf("RecoverySession.Context() error = %v, want ErrSessionClosed", err)
		}
		if _, err := session.BeginMutation(fixture.overrides); !errors.Is(err, ErrSessionClosed) {
			t.Fatalf("RecoverySession.BeginMutation() error = %v, want ErrSessionClosed", err)
		}
	})
}

func wrapInitEvents(operations *loadingOperations) *[]string {
	events := make([]string, 0, 8)
	preflightInit := operations.preflightInit
	operations.preflightInit = func(overrides Overrides) (InitContext, error) {
		events = append(events, "init-preflight")
		return preflightInit(overrides)
	}
	wrapRepositoryEvents(operations, &events)
	return &events
}

func wrapRecoveryEvents(operations *loadingOperations) *[]string {
	events := make([]string, 0, 2)
	preflightRepository := operations.preflightRepository
	operations.preflightRepository = func(overrides Overrides) (ControlContext, error) {
		events = append(events, "repository-preflight")
		return preflightRepository(overrides)
	}
	acquire := operations.acquire
	operations.acquire = func(root, path string) (*lock.Ownership, error) {
		events = append(events, "acquire")
		return acquire(root, path)
	}
	return &events
}

func wrapRepositoryEvents(operations *loadingOperations, events *[]string) {
	acquire := operations.acquire
	operations.acquire = func(root, path string) (*lock.Ownership, error) {
		*events = append(*events, "acquire")
		return acquire(root, path)
	}
	readRequirement := operations.readRequirement
	operations.readRequirement = func(repo string) (manifest.Requirement, error) {
		*events = append(*events, "requires")
		return readRequirement(repo)
	}
	satisfies := operations.satisfies
	operations.satisfies = func(version string, requirement manifest.Requirement) (bool, bool, error) {
		*events = append(*events, "satisfies")
		return satisfies(version, requirement)
	}
	loadManifest := operations.loadManifest
	operations.loadManifest = func(repo string) (manifest.Repository, error) {
		*events = append(*events, "manifest")
		return loadManifest(repo)
	}
}

func closeInitSession(t *testing.T, session *InitSession) {
	t.Helper()
	if session == nil {
		return
	}
	if err := session.Close(); err != nil && !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("InitSession.Close() error = %v", err)
	}
}

func closeRecoverySession(t *testing.T, session *RecoverySession) {
	t.Helper()
	if session == nil {
		return
	}
	if err := session.Close(); err != nil && !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("RecoverySession.Close() error = %v", err)
	}
}
