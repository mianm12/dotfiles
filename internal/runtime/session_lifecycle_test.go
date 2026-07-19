package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/state"
)

func TestMutationSession_LoadFailureDoesNotGrantStateCommit(t *testing.T) {
	candidate := mustDecodeState(t, validEmptyState)
	rendered := `{"version":1,"entries":{"~/.config/app/config":{"module":"app","kind":"rendered","source":"modules/app/.config/app/config.tmpl","hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","applied_at":"2026-07-19T00:00:00Z"}},"run_once":{}}`
	tests := []struct {
		name  string
		setup func(loadingFixture)
		match func(error) bool
	}{
		{
			name: "requires unsatisfied",
			setup: func(fixture loadingFixture) {
				writeManifest(t, fixture.repo, ">=9.0.0", "")
			},
			match: func(err error) bool { return errors.Is(err, ErrRequiresUnsatisfied) },
		},
		{
			name: "strict manifest invalid",
			setup: func(fixture loadingFixture) {
				writeManifest(t, fixture.repo, ">=1.0.0", "unknown = true\n")
			},
			match: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "strict mode")
			},
		},
		{
			name: "state corrupt",
			setup: func(fixture loadingFixture) {
				writeState(t, fixture, "{")
			},
			match: func(err error) bool { return errors.Is(err, state.ErrCorrupt) },
		},
		{
			name: "state too new",
			setup: func(fixture loadingFixture) {
				writeState(t, fixture, `{"version":2,"entries":{},"run_once":{}}`)
			},
			match: func(err error) bool { return errors.Is(err, state.ErrTooNew) },
		},
		{
			name: "state rendered unsupported",
			setup: func(fixture loadingFixture) {
				writeState(t, fixture, rendered)
			},
			match: func(err error) bool { return errors.Is(err, state.ErrUnsupportedRendered) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLoadingFixture(t, true)
			writeState(t, fixture, stateWithSymlinkEntry("~/old", "modules/app/file"))
			test.setup(fixture)
			before, err := os.ReadFile(fixture.paths.StateFile())
			if err != nil {
				t.Fatalf("os.ReadFile(state) error = %v", err)
			}

			session, err := BeginMutation(fixture.overrides)
			if err != nil {
				t.Fatalf("BeginMutation() error = %v", err)
			}
			t.Cleanup(func() { closeMutationSession(t, session) })
			mutation, err := session.Load("v1.0.0")
			if mutation != nil || !test.match(err) {
				t.Fatalf("MutationSession.Load() = (%#v, %v), want matched failure", mutation, err)
			}
			if err := mutation.CommitState(candidate); !errors.Is(err, ErrSessionOrder) {
				t.Fatalf("unavailable LoadedMutation.CommitState() error = %v, want ErrSessionOrder", err)
			}
			after, err := os.ReadFile(fixture.paths.StateFile())
			if err != nil {
				t.Fatalf("os.ReadFile(state after failure) error = %v", err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatal("failed mutation load changed old state bytes")
			}
		})
	}
}

func TestLoadedMutation_CommitsOnceAfterSuccessfulLoad(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, validEmptyState)
	session, err := BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginMutation() error = %v", err)
	}
	t.Cleanup(func() { closeMutationSession(t, session) })

	mutation, err := session.Load("v1.0.0")
	if err != nil {
		t.Fatalf("MutationSession.Load() error = %v", err)
	}
	if mutation.Inputs().State().Status() != state.StatusLoaded {
		t.Fatalf("LoadedMutation state status = %v, want StatusLoaded", mutation.Inputs().State().Status())
	}
	if second, err := session.Load("v1.0.0"); second != nil || !errors.Is(err, ErrSessionOrder) {
		t.Fatalf("second MutationSession.Load() = (%#v, %v), want ErrSessionOrder", second, err)
	}

	candidate := mustDecodeState(t, stateWithSymlinkEntry("~/new", "modules/app/file"))
	if err := mutation.CommitState(candidate); err != nil {
		t.Fatalf("LoadedMutation.CommitState() error = %v", err)
	}
	before, err := os.ReadFile(fixture.paths.StateFile())
	if err != nil {
		t.Fatalf("os.ReadFile(committed state) error = %v", err)
	}
	if err := mutation.CommitState(candidate); !errors.Is(err, ErrSessionOrder) {
		t.Fatalf("second LoadedMutation.CommitState() error = %v, want ErrSessionOrder", err)
	}
	after, err := os.ReadFile(fixture.paths.StateFile())
	if err != nil {
		t.Fatalf("os.ReadFile(state after second commit) error = %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("second state commit changed bytes")
	}

	closeMutationSession(t, session)
	if err := mutation.CommitState(candidate); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("CommitState() after Close error = %v, want ErrSessionClosed", err)
	}
}

func TestMutationSession_CopiesShareLoadAndCommitState(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, validEmptyState)
	session, err := BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginMutation() error = %v", err)
	}
	t.Cleanup(func() { closeMutationSession(t, session) })
	copiedSession := *session

	mutation, err := session.Load("v1.0.0")
	if err != nil {
		t.Fatalf("MutationSession.Load() error = %v", err)
	}
	if second, err := copiedSession.Load("v1.0.0"); second != nil || !errors.Is(err, ErrSessionOrder) {
		t.Fatalf("copied MutationSession.Load() = (%#v, %v), want ErrSessionOrder", second, err)
	}

	copiedMutation := *mutation
	first := mustDecodeState(t, stateWithSymlinkEntry("~/first", "modules/app/file"))
	if err := copiedMutation.CommitState(first); err != nil {
		t.Fatalf("copied LoadedMutation.CommitState() error = %v", err)
	}
	committed, err := os.ReadFile(fixture.paths.StateFile())
	if err != nil {
		t.Fatalf("os.ReadFile(committed state) error = %v", err)
	}

	second := mustDecodeState(t, stateWithSymlinkEntry("~/second", "modules/app/file"))
	if err := mutation.CommitState(second); !errors.Is(err, ErrSessionOrder) {
		t.Fatalf("original LoadedMutation.CommitState() after copied commit error = %v, want ErrSessionOrder", err)
	}
	after, err := os.ReadFile(fixture.paths.StateFile())
	if err != nil {
		t.Fatalf("os.ReadFile(state after rejected commit) error = %v", err)
	}
	if !reflect.DeepEqual(after, committed) {
		t.Fatal("rejected commit through capability alias changed first committed state")
	}
}

func TestMutationSession_CopiesLoadConcurrentlyOnce(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, validEmptyState)
	session, err := BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginMutation() error = %v", err)
	}
	t.Cleanup(func() { closeMutationSession(t, session) })
	copied := *session

	type result struct {
		mutation *LoadedMutation
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, handle := range []*MutationSession{session, &copied} {
		go func(candidate *MutationSession) {
			<-start
			mutation, err := candidate.Load("v1.0.0")
			results <- result{mutation: mutation, err: err}
		}(handle)
	}
	close(start)

	loaded := 0
	orderErrors := 0
	for range 2 {
		got := <-results
		switch {
		case got.err == nil && got.mutation != nil:
			loaded++
		case got.mutation == nil && errors.Is(got.err, ErrSessionOrder):
			orderErrors++
		default:
			t.Fatalf("concurrent copied Load() = (%#v, %v), want capability or ErrSessionOrder", got.mutation, got.err)
		}
	}
	if loaded != 1 || orderErrors != 1 {
		t.Fatalf("concurrent copied Load() results = %d capabilities, %d order errors; want 1 and 1", loaded, orderErrors)
	}
}

func TestLoadedMutation_CopiesCommitConcurrentlyOnce(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, validEmptyState)
	operations := defaultLoadingOperations()
	store := operations.storeState
	storeCalls := 0
	operations.storeState = func(root, path string, snapshot state.Snapshot) error {
		storeCalls++
		return store(root, path, snapshot)
	}
	session, err := beginMutation(fixture.overrides, operations)
	if err != nil {
		t.Fatalf("beginMutation() error = %v", err)
	}
	t.Cleanup(func() { closeMutationSession(t, session) })
	mutation, err := session.Load("v1.0.0")
	if err != nil {
		t.Fatalf("MutationSession.Load() error = %v", err)
	}
	left := *mutation
	right := *mutation
	candidate := mustDecodeState(t, stateWithSymlinkEntry("~/new", "modules/app/file"))

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, capability := range []*LoadedMutation{&left, &right} {
		go func(copy *LoadedMutation) {
			<-start
			results <- copy.CommitState(candidate)
		}(capability)
	}
	close(start)

	committed := 0
	orderErrors := 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			committed++
		case errors.Is(err, ErrSessionOrder):
			orderErrors++
		default:
			t.Fatalf("concurrent copied CommitState() error = %v, want nil or ErrSessionOrder", err)
		}
	}
	if committed != 1 || orderErrors != 1 || storeCalls != 1 {
		t.Fatalf("concurrent copied commits = %d success, %d order errors, %d stores; want 1, 1, 1", committed, orderErrors, storeCalls)
	}
}

func TestMutationSession_LoadFailureCanBeRetriedBeforeCapability(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, validEmptyState)
	loadErr := errors.New("state load failed")
	operations := defaultLoadingOperations()
	loadState := operations.loadState
	loadCalls := 0
	operations.loadState = func(path string) (state.Loaded, error) {
		loadCalls++
		if loadCalls == 1 {
			return state.Loaded{}, loadErr
		}
		return loadState(path)
	}
	session, err := beginMutation(fixture.overrides, operations)
	if err != nil {
		t.Fatalf("beginMutation() error = %v", err)
	}
	t.Cleanup(func() { closeMutationSession(t, session) })

	if mutation, err := session.Load("v1.0.0"); mutation != nil || !errors.Is(err, loadErr) {
		t.Fatalf("first MutationSession.Load() = (%#v, %v), want load failure without capability", mutation, err)
	}
	mutation, err := session.Load("v1.0.0")
	if err != nil || mutation == nil {
		t.Fatalf("second MutationSession.Load() = (%#v, %v), want capability", mutation, err)
	}
}

func TestLoadedMutation_ValidatesCandidateBeforeStore(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, validEmptyState)
	session, err := BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginMutation() error = %v", err)
	}
	t.Cleanup(func() { closeMutationSession(t, session) })
	mutation, err := session.Load("v1.0.0")
	if err != nil {
		t.Fatalf("MutationSession.Load() error = %v", err)
	}

	realDirectory := filepath.Join(fixture.home, "real")
	writeFile(t, filepath.Join(realDirectory, "file"), []byte("fixture"), 0o600)
	if err := os.Symlink(realDirectory, filepath.Join(fixture.home, "alias")); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	conflicting := mustDecodeState(t, stateWithSymlinkEntries("~/real/file", "~/alias/file"))
	before, err := os.ReadFile(fixture.paths.StateFile())
	if err != nil {
		t.Fatalf("os.ReadFile(state) error = %v", err)
	}
	if err := mutation.CommitState(conflicting); !errors.Is(err, state.ErrCorrupt) ||
		!errors.Is(err, state.ErrTargetIdentityConflict) || !errors.Is(err, paths.ErrTargetOverlap) {
		t.Fatalf("CommitState(conflicting) error = %v, want corrupt target identity conflict", err)
	}
	after, err := os.ReadFile(fixture.paths.StateFile())
	if err != nil {
		t.Fatalf("os.ReadFile(state after rejection) error = %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("rejected candidate changed old state bytes")
	}
	if err := mutation.CommitState(mustDecodeState(t, validEmptyState)); err != nil {
		t.Fatalf("CommitState(valid retry) error = %v", err)
	}
}

func TestLoadedMutation_StoreFailureCanBeRetried(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, validEmptyState)
	storeErr := errors.New("store failed")
	operations := defaultLoadingOperations()
	store := operations.storeState
	storeCalls := 0
	operations.storeState = func(root, path string, snapshot state.Snapshot) error {
		storeCalls++
		if storeCalls == 1 {
			return storeErr
		}
		return store(root, path, snapshot)
	}
	session, err := beginMutation(fixture.overrides, operations)
	if err != nil {
		t.Fatalf("beginMutation() error = %v", err)
	}
	t.Cleanup(func() { closeMutationSession(t, session) })
	mutation, err := session.Load("v1.0.0")
	if err != nil {
		t.Fatalf("MutationSession.Load() error = %v", err)
	}
	candidate := mustDecodeState(t, stateWithSymlinkEntry("~/new", "modules/app/file"))
	if err := mutation.CommitState(candidate); !errors.Is(err, storeErr) {
		t.Fatalf("CommitState() error = %v, want store failure", err)
	}
	copied := *mutation
	if err := copied.CommitState(candidate); err != nil {
		t.Fatalf("copied CommitState() retry error = %v", err)
	}
	if storeCalls != 2 {
		t.Fatalf("storeState calls = %d, want 2", storeCalls)
	}
}

func TestInitSession_CopiesShareLoadPhase(t *testing.T) {
	fixture := newLoadingFixture(t, false)
	session, err := BeginInit(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginInit() error = %v", err)
	}
	t.Cleanup(func() { closeInitSession(t, session) })
	copied := *session

	if _, err := session.Load("v1.0.0"); err != nil {
		t.Fatalf("InitSession.Load() error = %v", err)
	}
	if _, err := copied.Load("v1.0.0"); !errors.Is(err, ErrSessionOrder) {
		t.Fatalf("copied InitSession.Load() error = %v, want ErrSessionOrder", err)
	}
}

func TestInitSession_NestedMutationUsesUpdatedConfigAndSameOwnership(t *testing.T) {
	fixture := newLoadingFixture(t, false)
	outer, err := BeginInit(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginInit() error = %v", err)
	}
	t.Cleanup(func() { closeInitSession(t, outer) })
	if nested, err := outer.BeginMutation(fixture.overrides); nested != nil || !errors.Is(err, ErrSessionOrder) {
		t.Fatalf("BeginMutation() before init Load = (%#v, %v), want ErrSessionOrder", nested, err)
	}
	inputs, err := outer.Load("v1.0.0")
	if err != nil {
		t.Fatalf("InitSession.Load() error = %v", err)
	}
	if _, err := outer.Load("v1.0.0"); !errors.Is(err, ErrSessionOrder) {
		t.Fatalf("second InitSession.Load() error = %v, want ErrSessionOrder", err)
	}
	if !inputs.Context().ConfigMissing() {
		t.Fatal("ConfigMissing() = false, want true")
	}
	writeFile(t, fixture.config, []byte("profile = \"mac\"\n"), 0o600)
	writeState(t, fixture, "{")

	nested, err := outer.BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("InitSession.BeginMutation() error = %v", err)
	}
	closeInitSession(t, outer)
	assertLockBusy(t, fixture)
	if mutation, err := nested.Load("v1.0.0"); mutation != nil || !errors.Is(err, state.ErrCorrupt) {
		t.Fatalf("nested MutationSession.Load() = (%#v, %v), want ErrCorrupt", mutation, err)
	}
	configBytes, err := os.ReadFile(fixture.config)
	if err != nil || string(configBytes) != "profile = \"mac\"\n" {
		t.Fatalf("updated config = (%q, %v), want preserved", configBytes, err)
	}
	closeMutationSession(t, nested)
	assertLockAvailable(t, fixture)
}

func TestNestedMutationGateAllowsOnlyOneActiveChild(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	outer, err := BeginRecovery(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginRecovery() error = %v", err)
	}
	t.Cleanup(func() { closeRecoverySession(t, outer) })
	first, err := outer.BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("first BeginMutation() error = %v", err)
	}
	if second, err := outer.BeginMutation(fixture.overrides); second != nil || !errors.Is(err, ErrNestedMutationActive) {
		t.Fatalf("second BeginMutation() = (%#v, %v), want ErrNestedMutationActive", second, err)
	}
	closeMutationSession(t, first)
	second, err := outer.BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginMutation() after child Close error = %v", err)
	}
	closeMutationSession(t, second)
}

func TestNestedMutationGateSerializesConcurrentBegins(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	outer, err := BeginRecovery(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginRecovery() error = %v", err)
	}
	t.Cleanup(func() { closeRecoverySession(t, outer) })

	type result struct {
		session *MutationSession
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			session, err := outer.BeginMutation(fixture.overrides)
			results <- result{session: session, err: err}
		}()
	}
	ready.Wait()
	close(start)

	children := make([]*MutationSession, 0, 1)
	t.Cleanup(func() {
		for _, child := range children {
			closeMutationSession(t, child)
		}
	})
	activeErrors := 0
	for range 2 {
		got := <-results
		switch {
		case got.err == nil && got.session != nil:
			children = append(children, got.session)
		case got.session == nil && errors.Is(got.err, ErrNestedMutationActive):
			activeErrors++
		default:
			t.Fatalf("concurrent BeginMutation() = (%#v, %v), want one child or ErrNestedMutationActive", got.session, got.err)
		}
	}
	if len(children) != 1 || activeErrors != 1 {
		t.Fatalf("concurrent BeginMutation results = %d children, %d active errors; want 1 and 1", len(children), activeErrors)
	}
	closeMutationSession(t, children[0])
	children[0] = nil
}

func TestNestedMutationGateCloseFailureKeepsChildActive(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	releaseErr := errors.New("release child failed")
	outer, err := BeginRecovery(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginRecovery() error = %v", err)
	}
	var child *MutationSession
	var failing *retryableLeaseReleaser
	t.Cleanup(func() {
		if failing != nil {
			failing.err = nil
		}
		closeMutationSession(t, child)
		closeRecoverySession(t, outer)
	})
	child, err = outer.BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginMutation() error = %v", err)
	}
	failing = &retryableLeaseReleaser{inner: child.core.lease.releaser, err: releaseErr}
	child.core.lease.releaser = failing
	if err := child.Close(); !errors.Is(err, releaseErr) {
		t.Fatalf("child Close() error = %v, want release failure", err)
	}
	if second, err := outer.BeginMutation(fixture.overrides); second != nil || !errors.Is(err, ErrNestedMutationActive) {
		t.Fatalf("BeginMutation() after failed Close = (%#v, %v), want active child", second, err)
	}
	failing.err = nil
	if err := child.Close(); err != nil {
		t.Fatalf("child Close() retry error = %v", err)
	}
	child = nil
	second, err := outer.BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginMutation() after successful Close error = %v", err)
	}
	closeMutationSession(t, second)
}

func TestInitContext_ZeroValueDoesNotClaimConfigMissing(t *testing.T) {
	var context InitContext
	if context.ConfigMissing() {
		t.Fatal("zero InitContext claims confirmed config missing")
	}
	if _, ok := context.ExistingMachine(); ok {
		t.Fatal("zero InitContext returned an existing machine")
	}
	if _, ok := context.ProfileOverride(); ok {
		t.Fatal("zero InitContext returned a profile override")
	}
}

type retryableLeaseReleaser struct {
	inner leaseReleaser
	err   error
}

func (releaser *retryableLeaseReleaser) Release() error {
	if releaser.err != nil {
		return releaser.err
	}
	return releaser.inner.Release()
}

func mustDecodeState(t *testing.T, raw string) state.Snapshot {
	t.Helper()
	snapshot, err := state.Decode([]byte(raw))
	if err != nil {
		t.Fatalf("state.Decode() error = %v", err)
	}
	return snapshot
}
