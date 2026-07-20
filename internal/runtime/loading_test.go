package runtime

import (
	"errors"
	"fmt"
	"io/fs"
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

const validEmptyState = `{"version":1,"entries":{},"run_once":{}}`

type loadingFixture struct {
	root      string
	home      string
	repo      string
	config    string
	overrides Overrides
	paths     paths.ControlPlanePaths
}

func TestMutationSession_OrdersTrustedStages(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, validEmptyState)

	operations := defaultLoadingOperations()
	events := wrapLoadingEvents(&operations)
	session, err := beginMutation(fixture.overrides, operations)
	if err != nil {
		t.Fatalf("beginMutation() error = %v", err)
	}
	t.Cleanup(func() { closeMutationSession(t, session) })
	result, err := session.Load("v1.0.0")
	if err != nil {
		t.Fatalf("MutationSession.Load() error = %v", err)
	}
	if result.Inputs().State().Status() != state.StatusLoaded {
		t.Fatalf("State().Status() = %v, want StatusLoaded", result.Inputs().State().Status())
	}
	want := []string{
		"preflight", "acquire", "requires", "satisfies", "manifest", "satisfies",
		"state", "lexical-boundaries", "path-boundaries",
	}
	if !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
}

func TestBeginMutation_PreflightFailureDoesNotCreateLock(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeFile(t, fixture.config, []byte("unknown = true\n"), 0o600)

	session, err := BeginMutation(fixture.overrides)
	if err == nil {
		t.Fatal("BeginMutation() error = nil")
	}
	if session != nil {
		t.Fatal("BeginMutation() returned a session after preflight failure")
	}
	assertMissing(t, fixture.paths.StateRoot())
}

func TestBeginMutation_BusyStopsBeforeRepositoryAndState(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	owner, err := lock.Acquire(fixture.paths.StateRoot(), fixture.paths.StateLock())
	if err != nil {
		t.Fatalf("lock.Acquire() error = %v", err)
	}
	t.Cleanup(func() { releaseOwnership(t, owner) })

	operations := defaultLoadingOperations()
	events := wrapLoadingEvents(&operations)
	session, err := beginMutation(fixture.overrides, operations)
	if !errors.Is(err, lock.ErrBusy) {
		t.Fatalf("beginMutation() error = %v, want ErrBusy", err)
	}
	if session != nil {
		t.Fatal("busy mutation returned a session")
	}
	if want := []string{"preflight", "acquire"}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
}

func TestMutationSession_RepositoryFailuresShortCircuit(t *testing.T) {
	t.Run("requires", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		writeManifest(t, fixture.repo, ">=9.0.0", "")
		session, err := BeginMutation(fixture.overrides)
		if err != nil {
			t.Fatalf("BeginMutation() error = %v", err)
		}
		_, err = session.Load("v1.0.0")
		if !errors.Is(err, ErrRequiresUnsatisfied) {
			t.Fatalf("MutationSession.Load() error = %v, want ErrRequiresUnsatisfied", err)
		}
		closeMutationSession(t, session)
		assertLockAvailable(t, fixture)
	})

	t.Run("strict manifest", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		writeManifest(t, fixture.repo, ">=1.0.0", "unknown = true\n")
		writeState(t, fixture, "{")
		session, err := BeginMutation(fixture.overrides)
		if err != nil {
			t.Fatalf("BeginMutation() error = %v", err)
		}
		_, err = session.Load("v1.0.0")
		if err == nil || errors.Is(err, state.ErrCorrupt) {
			t.Fatalf("MutationSession.Load() error = %v, want strict manifest error before state", err)
		}
		closeMutationSession(t, session)
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
		session, err := beginMutation(fixture.overrides, operations)
		if err != nil {
			t.Fatalf("beginMutation() error = %v", err)
		}
		_, err = session.Load("v1.0.0")
		if !errors.Is(err, ErrRequiresUnsatisfied) {
			t.Fatalf("MutationSession.Load() error = %v, want strict ErrRequiresUnsatisfied", err)
		}
		closeMutationSession(t, session)
		assertLockAvailable(t, fixture)
	})
}

func TestMutationSession_LoadFailureKeepsCallerOwnedLock(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	writeState(t, fixture, "{")

	session, err := BeginMutation(fixture.overrides)
	if err != nil {
		t.Fatalf("BeginMutation() error = %v", err)
	}
	_, err = session.Load("v1.0.0")
	if !errors.Is(err, state.ErrCorrupt) {
		t.Fatalf("MutationSession.Load() error = %v, want ErrCorrupt", err)
	}
	assertLockBusy(t, fixture)
	closeMutationSession(t, session)
	assertLockAvailable(t, fixture)
}

func TestMutationSession_LoadAndCloseFailuresKeepRetryableSession(t *testing.T) {
	loadErr := errors.New("load failed")
	releaseErr := errors.New("release failed")
	releaser := &stubSessionReleaser{err: releaseErr}
	operations := defaultLoadingOperations()
	operations.readRequirement = func(string) (manifest.Requirement, error) {
		return manifest.Requirement{}, loadErr
	}
	session := newMutationSession(
		newSessionLease(&lock.Ownership{}, releaser),
		RunContext{},
		operations,
	)

	if _, err := session.Load("v1.0.0"); !errors.Is(err, loadErr) {
		t.Fatalf("MutationSession.Load() error = %v, want load failure", err)
	}
	if err := session.Close(); !errors.Is(err, releaseErr) {
		t.Fatalf("MutationSession.Close() error = %v, want release failure", err)
	}
	releaser.err = nil
	if err := session.Close(); err != nil {
		t.Fatalf("MutationSession.Close() retry error = %v", err)
	}
	if releaser.calls != 2 {
		t.Fatalf("Release() calls = %d, want 2", releaser.calls)
	}
	if err := session.Close(); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("MutationSession.Close() after success error = %v, want ErrSessionClosed", err)
	}
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

		_, err := loadReadOnlyWithoutMutation(t, fixture)
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

		_, err := loadReadOnlyWithoutMutation(t, fixture)
		if !errors.Is(err, state.ErrPathValidation) || !errors.Is(err, paths.ErrTargetControlOverlap) {
			t.Fatalf("LoadReadOnly() error = %v, want runtime target/control overlap", err)
		}
		if errors.Is(err, state.ErrCorrupt) {
			t.Fatalf("filesystem alias misclassified as corrupt: %v", err)
		}
	})

	t.Run("filesystem state alias is corrupt", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		realDirectory := filepath.Join(fixture.home, "real")
		writeFile(t, filepath.Join(realDirectory, "file"), []byte("fixture"), 0o600)
		if err := os.Symlink(realDirectory, filepath.Join(fixture.home, "alias")); err != nil {
			t.Fatalf("os.Symlink() error = %v", err)
		}
		writeState(t, fixture, stateWithSymlinkEntries("~/real/file", "~/alias/file"))

		_, err := loadReadOnlyWithoutMutation(t, fixture)
		if !errors.Is(err, state.ErrCorrupt) ||
			!errors.Is(err, state.ErrTargetIdentityConflict) ||
			!errors.Is(err, paths.ErrTargetOverlap) {
			t.Fatalf("LoadReadOnly() error = %v, want corrupt equal target identity", err)
		}
		if errors.Is(err, state.ErrPathValidation) {
			t.Fatalf("equal state identities misclassified as runtime path error: %v", err)
		}
		var conflict *paths.TargetConflictError
		if !errors.As(err, &conflict) || conflict.Relation() != paths.TargetRelationEqual {
			t.Fatalf("LoadReadOnly() conflict = %#v, want equal TargetConflictError", conflict)
		}
	})

	t.Run("equal state identity precedes self traversal", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		realDirectory := filepath.Join(fixture.home, "real")
		writeFile(t, filepath.Join(realDirectory, "fixture"), []byte("fixture"), 0o600)
		if err := os.Symlink("real", filepath.Join(fixture.home, "bridge")); err != nil {
			t.Fatalf("os.Symlink(bridge) error = %v", err)
		}
		if err := os.Symlink(
			filepath.FromSlash("bridge/.."),
			filepath.Join(fixture.home, "detour"),
		); err != nil {
			t.Fatalf("os.Symlink(detour) error = %v", err)
		}
		writeState(t, fixture, stateWithSymlinkEntries("~/bridge", "~/detour/bridge"))

		_, err := loadReadOnlyWithoutMutation(t, fixture)
		if !errors.Is(err, state.ErrCorrupt) ||
			!errors.Is(err, state.ErrTargetIdentityConflict) ||
			!errors.Is(err, paths.ErrTargetOverlap) {
			t.Fatalf("LoadReadOnly() error = %v, want corrupt equal target identity", err)
		}
		if errors.Is(err, state.ErrPathValidation) {
			t.Fatalf("equal state identities misclassified as self-traversal path error: %v", err)
		}
		var conflict *paths.TargetConflictError
		if !errors.As(err, &conflict) || conflict.Relation() != paths.TargetRelationEqual {
			t.Fatalf("LoadReadOnly() conflict = %#v, want equal TargetConflictError", conflict)
		}
	})

	t.Run("state ancestor relation is runtime unsafe", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		writeFile(t, filepath.Join(fixture.home, "parent", "child"), []byte("fixture"), 0o600)
		writeState(t, fixture, stateWithSymlinkEntries("~/parent", "~/parent/child"))

		_, err := loadReadOnlyWithoutMutation(t, fixture)
		if !errors.Is(err, state.ErrPathValidation) || !errors.Is(err, paths.ErrTargetOverlap) {
			t.Fatalf("LoadReadOnly() error = %v, want runtime ancestor conflict", err)
		}
		if errors.Is(err, state.ErrCorrupt) || errors.Is(err, state.ErrTargetIdentityConflict) {
			t.Fatalf("ancestor state targets misclassified as corrupt identity conflict: %v", err)
		}
	})

	t.Run("blocked target is runtime unsafe", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		writeFile(t, filepath.Join(fixture.home, "blocked"), []byte("user data"), 0o600)
		writeState(t, fixture, stateWithSymlinkEntry("~/blocked/child", "modules/app/file"))

		_, err := loadReadOnlyWithoutMutation(t, fixture)
		if !errors.Is(err, state.ErrPathValidation) || !errors.Is(err, paths.ErrPathBlocked) {
			t.Fatalf("LoadReadOnly() error = %v, want runtime blocked-path error", err)
		}
		if errors.Is(err, state.ErrCorrupt) {
			t.Fatalf("blocked target misclassified as corrupt: %v", err)
		}
	})

	t.Run("different hard-link entries remain valid", func(t *testing.T) {
		fixture := newLoadingFixture(t, true)
		first := filepath.Join(fixture.home, "first")
		second := filepath.Join(fixture.home, "second")
		writeFile(t, first, []byte("same inode"), 0o600)
		if err := os.Link(first, second); err != nil {
			t.Fatalf("os.Link() error = %v", err)
		}
		writeState(t, fixture, stateWithSymlinkEntries("~/first", "~/second"))

		if _, err := loadReadOnlyWithoutMutation(t, fixture); err != nil {
			t.Fatalf("LoadReadOnly() error = %v, want distinct target entries", err)
		}
	})
}

func loadReadOnlyWithoutMutation(t *testing.T, fixture loadingFixture) (LoadedInputs, error) {
	t.Helper()
	before := snapshotFixtureTree(t, fixture.root)
	result, err := LoadReadOnly(fixture.overrides, "v1.0.0")
	after := snapshotFixtureTree(t, fixture.root)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("LoadReadOnly() changed fixture tree\nbefore: %#v\nafter:  %#v", before, after)
	}
	assertMissing(t, fixture.paths.StateLock())
	return result, err
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

func stateWithSymlinkEntries(targets ...string) string {
	entries := make([]string, len(targets))
	for index, target := range targets {
		module := fmt.Sprintf("module%d", index)
		entries[index] = fmt.Sprintf(
			`%q:{"module":%q,"kind":"symlink","source":%q,"link_dest":%q,"applied_at":"2026-07-19T00:00:00Z"}`,
			target,
			module,
			"modules/"+module+"/file",
			"/repo/modules/"+module+"/file",
		)
	}
	return `{"version":1,"entries":{` + strings.Join(entries, ",") + `},"run_once":{}}`
}

func assertLockBusy(t *testing.T, fixture loadingFixture) {
	t.Helper()
	contender, err := lock.Acquire(fixture.paths.StateRoot(), fixture.paths.StateLock())
	if !errors.Is(err, lock.ErrBusy) {
		if err == nil {
			releaseOwnership(t, contender)
		}
		t.Fatalf("lock contender error = %v, want ErrBusy", err)
	}
}

func assertLockAvailable(t *testing.T, fixture loadingFixture) {
	t.Helper()
	owner, err := lock.Acquire(fixture.paths.StateRoot(), fixture.paths.StateLock())
	if err != nil {
		t.Fatalf("lock remains held: %v", err)
	}
	releaseOwnership(t, owner)
}

func closeMutationSession(t *testing.T, session *MutationSession) {
	t.Helper()
	if session == nil {
		return
	}
	if err := session.Close(); err != nil && !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("MutationSession.Close() error = %v", err)
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

type stubSessionReleaser struct {
	err   error
	calls int
}

func (releaser *stubSessionReleaser) Release() error {
	releaser.calls++
	return releaser.err
}

func TestLoadingFixtureUsesSyntheticConfigEnvironment(t *testing.T) {
	fixture := newLoadingFixture(t, true)
	value, ok := os.LookupEnv(paths.ConfigEnvironment)
	if !ok || value != fixture.config {
		t.Fatalf("DOT_CONFIG lookup = %q, %v", value, ok)
	}
}
