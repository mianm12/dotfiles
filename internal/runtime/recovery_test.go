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

func TestLoadInitMutation_ConfigMissingLoadsManifestAfterLockAndSkipsState(t *testing.T) {
	fixture := newLoadingFixture(t, false)
	writeState(t, fixture, "{")

	operations := defaultLoadingOperations()
	events := wrapInitEvents(&operations)
	result, lease, err := loadInitMutation(fixture.overrides, "v1.0.0", operations)
	if err != nil {
		t.Fatalf("loadInitMutation() error = %v", err)
	}
	t.Cleanup(func() { releaseLease(t, lease) })
	if !result.Context().ConfigMissing() {
		t.Fatal("ConfigMissing = false, want true")
	}
	if got := result.Compatibility().Requirement().String(); got != ">=1.0.0" {
		t.Fatalf("Requirement = %q", got)
	}
	want := []string{"init-preflight", "acquire", "requires", "satisfies", "manifest", "satisfies"}
	if !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
}

func TestLoadInitMutation_ExistingInvalidConfigFailsBeforeLock(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeFile(t, fixture.config, []byte("unknown = true\n"), 0o600)

	_, lease, err := LoadInitMutation(fixture.overrides, "v1.0.0")
	if err == nil {
		t.Fatal("LoadInitMutation() error = nil")
	}
	if lease != nil {
		t.Fatal("invalid config returned a lease")
	}
	assertMissing(t, fixture.paths.StateRoot())
}

func TestLoadInitMutation_ManifestFailureReleasesLockAndSkipsState(t *testing.T) {
	fixture := newLoadingFixture(t, false)
	writeManifest(t, fixture.repo, ">=1.0.0", "unknown = true\n")
	writeState(t, fixture, "{")

	_, lease, err := LoadInitMutation(fixture.overrides, "v1.0.0")
	if err == nil || errors.Is(err, state.ErrCorrupt) {
		t.Fatalf("LoadInitMutation() error = %v, want manifest error before state", err)
	}
	if lease != nil {
		t.Fatal("manifest failure returned a lease")
	}
	assertLockAvailable(t, fixture)
}

func TestLoadRecoveryMutation_SkipsManifestAndStateButHoldsLock(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeManifest(t, fixture.repo, "invalid", "unknown = true\n")
	writeState(t, fixture, "{")

	operations := defaultLoadingOperations()
	events := wrapRecoveryEvents(&operations)
	context, lease, err := loadRecoveryMutation(fixture.overrides, operations)
	if err != nil {
		t.Fatalf("loadRecoveryMutation() error = %v", err)
	}
	t.Cleanup(func() { releaseLease(t, lease) })
	if context.RepositoryPath() != fixture.repo {
		t.Fatalf("RepositoryPath() = %q, want %q", context.RepositoryPath(), fixture.repo)
	}
	if want := []string{"repository-preflight", "acquire"}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}

	contender, err := lock.Acquire(fixture.paths.StateRoot(), fixture.paths.StateLock())
	if !errors.Is(err, lock.ErrBusy) {
		if err == nil {
			releaseOwnership(t, contender)
		}
		t.Fatalf("contender error = %v, want ErrBusy", err)
	}
}

func TestLoadRecoveryMutation_StateFailClosedVariantsDoNotBlockRecovery(t *testing.T) {
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

			_, lease, err := LoadRecoveryMutation(fixture.overrides)
			if err != nil {
				t.Fatalf("LoadRecoveryMutation() error = %v", err)
			}
			releaseLease(t, lease)
		})
	}
}

func TestLoadRecoveryMutation_AllowsMissingConfig(t *testing.T) {
	fixture := newLoadingFixture(t, false)

	context, lease, err := LoadRecoveryMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("LoadRecoveryMutation() error = %v", err)
	}
	t.Cleanup(func() { releaseLease(t, lease) })
	if context.RepositoryPath() != fixture.repo {
		t.Fatalf("RepositoryPath() = %q, want %q", context.RepositoryPath(), fixture.repo)
	}
}

func TestLoadRecoveryMutation_ExistingInvalidConfigFailsBeforeLock(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeFile(t, fixture.config, []byte("unknown = true\n"), 0o600)

	_, lease, err := LoadRecoveryMutation(fixture.overrides)
	if err == nil {
		t.Fatal("LoadRecoveryMutation() error = nil")
	}
	if lease != nil {
		t.Fatal("invalid config returned a recovery lease")
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

func TestRecoveryThenNestedMutation_CorruptStateReleasesOnlyNestedLease(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, "{")

	_, outerLease, err := LoadRecoveryMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("LoadRecoveryMutation() error = %v", err)
	}
	outerReleased := false
	t.Cleanup(func() {
		if !outerReleased {
			releaseLease(t, outerLease)
		}
	})

	_, nestedLease, err := LoadNestedMutation(fixture.overrides, "v1.0.0", outerLease.Ownership())
	if !errors.Is(err, state.ErrCorrupt) {
		t.Fatalf("LoadNestedMutation() error = %v, want ErrCorrupt", err)
	}
	if nestedLease != nil {
		t.Fatal("failed nested mutation returned a lease")
	}
	contender, err := lock.Acquire(fixture.paths.StateRoot(), fixture.paths.StateLock())
	if !errors.Is(err, lock.ErrBusy) {
		if err == nil {
			releaseOwnership(t, contender)
		}
		t.Fatalf("contender after nested failure error = %v, want ErrBusy", err)
	}
	if err := outerLease.Release(); err != nil {
		t.Fatalf("outer Lease.Release() error = %v", err)
	}
	outerReleased = true
	assertLockAvailable(t, fixture)
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
