package runtime

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mianm12/dotfiles/internal/lock"
	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/state"
)

const validEmptyState = `{"version":1,"entries":{},"run_once":{}}`

type loadingFixture struct {
	root      string
	home      string
	repo      string
	config    string
	overrides Overrides
	paths     paths.ControlPlanePaths
}

func TestLoadMutation_OrdersTrustedStagesAndReleasesFailure(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, validEmptyState)

	operations := defaultLoadingOperations()
	events := wrapLoadingEvents(&operations)
	result, lease, err := loadMutation(fixture.overrides, "v1.0.0", operations)
	if err != nil {
		t.Fatalf("loadMutation() error = %v", err)
	}
	t.Cleanup(func() { releaseLease(t, lease) })
	if result.State().Status() != state.StatusLoaded {
		t.Fatalf("State().Status() = %v, want StatusLoaded", result.State().Status())
	}
	want := []string{
		"preflight", "acquire", "requires", "satisfies", "manifest", "satisfies",
		"state", "lexical-boundaries", "state-identities", "path-boundaries",
	}
	if !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
}

func TestLoadMutation_PreflightFailureDoesNotCreateLock(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeFile(t, fixture.config, []byte("unknown = true\n"), 0o600)

	_, lease, err := LoadMutation(fixture.overrides, "v1.0.0")
	if err == nil {
		t.Fatal("LoadMutation() error = nil")
	}
	if lease != nil {
		t.Fatal("LoadMutation() returned a lease after preflight failure")
	}
	assertMissing(t, fixture.paths.StateRoot())
}

func TestLoadMutation_BusyStopsBeforeRepositoryAndState(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	owner, err := lock.Acquire(fixture.paths.StateRoot(), fixture.paths.StateLock())
	if err != nil {
		t.Fatalf("lock.Acquire() error = %v", err)
	}
	t.Cleanup(func() { releaseOwnership(t, owner) })

	operations := defaultLoadingOperations()
	events := wrapLoadingEvents(&operations)
	_, lease, err := loadMutation(fixture.overrides, "v1.0.0", operations)
	if !errors.Is(err, lock.ErrBusy) {
		t.Fatalf("loadMutation() error = %v, want ErrBusy", err)
	}
	if lease != nil {
		t.Fatal("busy mutation returned a lease")
	}
	if want := []string{"preflight", "acquire"}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
}

func TestLoadMutation_RepositoryFailuresShortCircuitAndRelease(t *testing.T) {
	t.Run("requires", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		writeManifest(t, fixture.repo, ">=9.0.0", "")

		_, lease, err := LoadMutation(fixture.overrides, "v1.0.0")
		if !errors.Is(err, ErrRequiresUnsatisfied) {
			t.Fatalf("LoadMutation() error = %v, want ErrRequiresUnsatisfied", err)
		}
		if lease != nil {
			t.Fatal("requires failure returned a lease")
		}
		assertLockAvailable(t, fixture)
	})

	t.Run("strict manifest", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		writeManifest(t, fixture.repo, ">=1.0.0", "unknown = true\n")
		writeState(t, fixture, "{")

		_, lease, err := LoadMutation(fixture.overrides, "v1.0.0")
		if err == nil || errors.Is(err, state.ErrCorrupt) {
			t.Fatalf("LoadMutation() error = %v, want strict manifest error before state", err)
		}
		if lease != nil {
			t.Fatal("manifest failure returned a lease")
		}
		assertLockAvailable(t, fixture)
	})

	t.Run("strict requirement snapshot", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		operations := defaultLoadingOperations()
		readRequirement := operations.readRequirement
		operations.readRequirement = func(repo string) (manifest.Requirement, error) {
			requirement, err := readRequirement(repo)
			if err == nil {
				writeManifest(t, repo, ">=9.0.0", "")
			}
			return requirement, err
		}

		_, lease, err := loadMutation(fixture.overrides, "v1.0.0", operations)
		if !errors.Is(err, ErrRequiresUnsatisfied) {
			t.Fatalf("loadMutation() error = %v, want strict ErrRequiresUnsatisfied", err)
		}
		if lease != nil {
			t.Fatal("strict requirement failure returned a lease")
		}
		assertLockAvailable(t, fixture)
	})
}

func TestLoadMutation_StateFailureReleasesAndPreservesCause(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, "{")

	_, lease, err := LoadMutation(fixture.overrides, "v1.0.0")
	if !errors.Is(err, state.ErrCorrupt) {
		t.Fatalf("LoadMutation() error = %v, want ErrCorrupt", err)
	}
	if lease != nil {
		t.Fatal("state failure returned a lease")
	}
	assertLockAvailable(t, fixture)
}

func TestLoadReadOnly_StateStatusesAreClassifiedWithoutLock(t *testing.T) {
	tests := []struct {
		name       string
		state      string
		writeState bool
		wantStatus state.LoadStatus
		wantErr    error
	}{
		{name: "missing", wantStatus: state.StatusMissing},
		{name: "loaded", state: validEmptyState, writeState: true, wantStatus: state.StatusLoaded},
		{name: "corrupt", state: "{", writeState: true, wantErr: state.ErrCorrupt},
		{name: "too new", state: `{"version":2,"entries":{},"run_once":{}}`, writeState: true, wantErr: state.ErrTooNew},
		{
			name:       "rendered",
			state:      `{"version":1,"entries":{"~/.config/app/config":{"module":"app","kind":"rendered","source":"modules/app/.config/app/config.tmpl","hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","applied_at":"2026-07-19T00:00:00Z"}},"run_once":{}}`,
			writeState: true,
			wantErr:    state.ErrUnsupportedRendered,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLoadingFixture(t, true)
			if test.writeState {
				writeState(t, fixture, test.state)
			}
			before := snapshotFixtureTree(t, fixture.root)
			result, err := LoadReadOnly(fixture.overrides, "v1.0.0")
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("LoadReadOnly() error = %v, want %v", err, test.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("LoadReadOnly() error = %v", err)
				}
				if result.State().Status() != test.wantStatus {
					t.Fatalf("State().Status() = %v, want %v", result.State().Status(), test.wantStatus)
				}
			}
			after := snapshotFixtureTree(t, fixture.root)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("LoadReadOnly() changed fixture tree\nbefore: %#v\nafter:  %#v", before, after)
			}
			assertMissing(t, fixture.paths.StateLock())
		})
	}
}

func TestLoadReadOnly_DevelopmentBuildReturnsCompatibility(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeManifest(t, fixture.repo, ">=999.0.0", "")

	result, err := LoadReadOnly(fixture.overrides, "dev")
	if err != nil {
		t.Fatalf("LoadReadOnly() error = %v", err)
	}
	if !result.Compatibility().DevelopmentBuild() {
		t.Fatal("DevelopmentBuild = false, want true")
	}
	if got := result.Compatibility().Requirement().String(); got != ">=999.0.0" {
		t.Fatalf("Requirement = %q", got)
	}
	assertMissing(t, fixture.paths.StateRoot())
}

func TestLoadReadOnly_StatePathClassification(t *testing.T) {
	t.Run("lexical control overlap is corrupt", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		writeState(t, fixture, stateWithSymlinkEntry(
			"~/.local/state/dot/state.json",
			"modules/app/state.json",
		))

		_, err := LoadReadOnly(fixture.overrides, "v1.0.0")
		if !errors.Is(err, state.ErrCorrupt) || !errors.Is(err, paths.ErrTargetControlOverlap) {
			t.Fatalf("LoadReadOnly() error = %v, want corrupt target/control overlap", err)
		}
		if errors.Is(err, state.ErrPathValidation) {
			t.Fatalf("lexical overlap misclassified as runtime path error: %v", err)
		}
	})

	t.Run("filesystem control alias is runtime unsafe", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		writeFile(t, filepath.Join(fixture.repo, "file"), []byte("fixture"), 0o600)
		alias := filepath.Join(fixture.home, "managed")
		if err := os.Symlink(fixture.repo, alias); err != nil {
			t.Fatalf("os.Symlink() error = %v", err)
		}
		writeState(t, fixture, stateWithSymlinkEntry("~/managed/file", "modules/app/file"))

		_, err := LoadReadOnly(fixture.overrides, "v1.0.0")
		if !errors.Is(err, state.ErrPathValidation) || !errors.Is(err, paths.ErrTargetControlOverlap) {
			t.Fatalf("LoadReadOnly() error = %v, want runtime target/control overlap", err)
		}
		if errors.Is(err, state.ErrCorrupt) {
			t.Fatalf("filesystem alias misclassified as corrupt: %v", err)
		}
	})
}

func TestLoadNestedMutation_ReusesOwnershipWithoutEarlyRelease(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	owner, err := lock.Acquire(fixture.paths.StateRoot(), fixture.paths.StateLock())
	if err != nil {
		t.Fatalf("lock.Acquire() error = %v", err)
	}
	ownerReleased := false
	t.Cleanup(func() {
		if !ownerReleased {
			releaseOwnership(t, owner)
		}
	})

	_, nestedLease, err := LoadNestedMutation(fixture.overrides, "v1.0.0", owner)
	if err != nil {
		t.Fatalf("LoadNestedMutation() error = %v", err)
	}
	if err := nestedLease.Release(); err != nil {
		t.Fatalf("nested Lease.Release() error = %v", err)
	}

	contender, err := lock.Acquire(fixture.paths.StateRoot(), fixture.paths.StateLock())
	if !errors.Is(err, lock.ErrBusy) {
		if err == nil {
			releaseOwnership(t, contender)
		}
		t.Fatalf("contender after nested release error = %v, want ErrBusy", err)
	}
	releaseOwnership(t, owner)
	ownerReleased = true
	contender, err = lock.Acquire(fixture.paths.StateRoot(), fixture.paths.StateLock())
	if err != nil {
		t.Fatalf("contender after outer release error = %v", err)
	}
	releaseOwnership(t, contender)
}

func TestLoadNestedMutation_PreflightFailureDoesNotReuseOwnership(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	owner, err := lock.Acquire(fixture.paths.StateRoot(), fixture.paths.StateLock())
	if err != nil {
		t.Fatalf("lock.Acquire() error = %v", err)
	}
	t.Cleanup(func() { releaseOwnership(t, owner) })
	writeFile(t, fixture.config, []byte("unknown = true\n"), 0o600)

	operations := defaultLoadingOperations()
	events := wrapLoadingEvents(&operations)
	_, lease, err := loadNestedMutation(fixture.overrides, "v1.0.0", owner, operations)
	if err == nil {
		t.Fatal("loadNestedMutation() error = nil")
	}
	if lease != nil {
		t.Fatal("preflight failure returned a nested lease")
	}
	if want := []string{"preflight"}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
}

func wrapLoadingEvents(operations *loadingOperations) *[]string {
	events := make([]string, 0, 10)
	preflight := operations.preflight
	operations.preflight = func(overrides Overrides) (RunContext, error) {
		events = append(events, "preflight")
		return preflight(overrides)
	}
	acquire := operations.acquire
	operations.acquire = func(root, path string) (*lock.Ownership, error) {
		events = append(events, "acquire")
		return acquire(root, path)
	}
	reuse := operations.reuse
	operations.reuse = func(owner *lock.Ownership, root, path string) (*lock.Guard, error) {
		events = append(events, "reuse")
		return reuse(owner, root, path)
	}
	readRequirement := operations.readRequirement
	operations.readRequirement = func(repo string) (manifest.Requirement, error) {
		events = append(events, "requires")
		return readRequirement(repo)
	}
	satisfies := operations.satisfies
	operations.satisfies = func(version string, requirement manifest.Requirement) (bool, bool, error) {
		events = append(events, "satisfies")
		return satisfies(version, requirement)
	}
	loadManifest := operations.loadManifest
	operations.loadManifest = func(repo string) (manifest.Repository, error) {
		events = append(events, "manifest")
		return loadManifest(repo)
	}
	loadState := operations.loadState
	operations.loadState = func(path string) (state.Loaded, error) {
		events = append(events, "state")
		return loadState(path)
	}
	lexical := operations.validateLexicalBoundaries
	operations.validateLexicalBoundaries = func(control paths.ControlPlanePaths, targets []paths.LabeledTarget) error {
		events = append(events, "lexical-boundaries")
		return lexical(control, targets)
	}
	identities := operations.validateStateIdentities
	operations.validateStateIdentities = func(snapshot state.Snapshot, home string) error {
		events = append(events, "state-identities")
		return identities(snapshot, home)
	}
	boundaries := operations.validatePathBoundaries
	operations.validatePathBoundaries = func(control paths.ControlPlanePaths, targets []paths.LabeledTarget) error {
		events = append(events, "path-boundaries")
		return boundaries(control, targets)
	}
	return &events
}

func newLoadingFixture(t *testing.T, configExists bool) loadingFixture {
	t.Helper()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	config := filepath.Join(root, "machine", "config.toml")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(repo) error = %v", err)
	}
	writeManifest(t, repo, ">=1.0.0", "")
	if configExists {
		writeFile(t, config, []byte("profile = \"mac\"\n\n[data]\nemail = \"test@example.com\"\n"), 0o600)
	}
	t.Setenv(paths.ConfigEnvironment, config)
	controlPaths, err := paths.ResolveControlPlanePaths(home, repo, config)
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}
	return loadingFixture{
		root:   root,
		home:   home,
		repo:   repo,
		config: config,
		overrides: Overrides{
			Home:       Override{Value: home, Set: true},
			Repository: Override{Value: repo, Set: true},
		},
		paths: controlPaths,
	}
}

type fixtureTreeEntry struct {
	Mode fs.FileMode
	Data []byte
	Link string
}

func snapshotFixtureTree(t *testing.T, root string) map[string]fixtureTreeEntry {
	t.Helper()

	snapshot := make(map[string]fixtureTreeEntry)
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		entry := fixtureTreeEntry{Mode: info.Mode()}
		switch {
		case info.Mode().IsRegular():
			entry.Data, err = os.ReadFile(path)
		case info.Mode()&fs.ModeSymlink != 0:
			// 只记录 symlink 文本，绝不跟随到隔离临时根之外。
			entry.Link, err = os.Readlink(path)
		}
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(relative)] = entry
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot fixture tree %q: %v", root, err)
	}
	return snapshot
}

func writeManifest(t *testing.T, repo, requirement, extra string) {
	t.Helper()
	content := fmt.Sprintf("requires = %q\n%s\n[profiles]\nmac = []\n", requirement, extra)
	writeFile(t, filepath.Join(repo, "dot.toml"), []byte(content), 0o600)
}

func writeState(t *testing.T, fixture loadingFixture, content string) {
	t.Helper()
	writeFile(t, fixture.paths.StateFile(), []byte(content), 0o600)
}

func writeFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}

func stateWithSymlinkEntry(target, source string) string {
	return fmt.Sprintf(
		`{"version":1,"entries":{%q:{"module":"app","kind":"symlink","source":%q,"link_dest":"/tmp/source","applied_at":"2026-07-19T00:00:00Z"}},"run_once":{}}`,
		target,
		source,
	)
}

func assertLockAvailable(t *testing.T, fixture loadingFixture) {
	t.Helper()
	owner, err := lock.Acquire(fixture.paths.StateRoot(), fixture.paths.StateLock())
	if err != nil {
		t.Fatalf("lock remains held: %v", err)
	}
	releaseOwnership(t, owner)
}

func releaseLease(t *testing.T, lease *Lease) {
	t.Helper()
	if lease == nil || lease.Ownership() == nil {
		return
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("Lease.Release() error = %v", err)
	}
}

func releaseOwnership(t *testing.T, owner *lock.Ownership) {
	t.Helper()
	if err := owner.Release(); err != nil {
		t.Fatalf("Ownership.Release() error = %v", err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	_, err := os.Lstat(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.Lstat(%q) error = %v, want not exist", path, err)
	}
}

func TestLoadingFixtureUsesSyntheticConfigEnvironment(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	value, ok := os.LookupEnv(paths.ConfigEnvironment)
	if !ok || value != fixture.config {
		t.Fatalf("DOT_CONFIG lookup = %q, %v", value, ok)
	}
}
