package runtime

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/mianm12/dotfiles/internal/lock"
	"github.com/mianm12/dotfiles/internal/manifest"
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
