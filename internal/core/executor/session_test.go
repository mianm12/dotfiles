package executor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/lock"
)

func TestSessionKeepsControlValuesFromOpen(t *testing.T) {
	fixture := newFixture(t)
	controls := sessionTestControls(fixture)
	original := controls
	session, err := OpenSession(fixture.home, controls)
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	controls = corepaths.Controls{
		Repository: filepath.Join(fixture.root, "other-repository"),
		Config:     filepath.Join(fixture.root, "other-config", "machine.toml"),
		State:      filepath.Join(fixture.root, "other-state", "state.json"),
		Lock:       filepath.Join(fixture.root, "other-state", "mutation.lock"),
	}
	machine := sessionTestMachine(original.Repository)
	changed, err := session.PublishSelection(machine)
	if err != nil || !changed {
		t.Fatalf("PublishSelection() = (%t, %v), want changed", changed, err)
	}
	result, err := session.Converge(nil, nil)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if !result.StateChanged {
		t.Fatalf("Converge() = %#v, want initial state commit", result)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	for _, path := range []string{original.Config, original.State, original.Lock} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("os.Lstat(original control %q) error = %v", path, err)
		}
	}
	for _, path := range []string{controls.Config, controls.State, controls.Lock} {
		assertSessionPathMissing(t, path)
	}
}

func TestSessionRejectsSecondOpenUntilClose(t *testing.T) {
	fixture := newFixture(t)
	controls := sessionTestControls(fixture)
	first, err := OpenSession(fixture.home, controls)
	if err != nil {
		t.Fatalf("OpenSession(first) error = %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	second, err := OpenSession(fixture.home, controls)
	if second != nil || !errors.Is(err, lock.ErrBusy) {
		t.Fatalf("OpenSession(second) = (%#v, %v), want nil lock.ErrBusy", second, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	reopened, err := OpenSession(fixture.home, controls)
	if err != nil {
		t.Fatalf("OpenSession(after close) error = %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close(reopened) error = %v", err)
	}
}

func TestClosedSessionRejectsPublishAndConverge(t *testing.T) {
	fixture := newFixture(t)
	session, err := OpenSession(fixture.home, sessionTestControls(fixture))
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	copied := *session
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if changed, err := copied.PublishSelection(sessionTestMachine(fixture.repository)); err == nil || changed {
		t.Fatalf("PublishSelection(after close) = (%t, %v), want unchanged error", changed, err)
	}
	if result, err := copied.Converge(nil, nil); err == nil {
		t.Fatalf("Converge(after close) = (%#v, nil), want error", result)
	}
	var nilSession *Session
	if changed, err := nilSession.PublishSelection(sessionTestMachine(fixture.repository)); err == nil || changed {
		t.Fatalf("PublishSelection(nil) = (%t, %v), want unchanged error", changed, err)
	}
	if result, err := nilSession.Converge(nil, nil); err == nil {
		t.Fatalf("Converge(nil) = (%#v, nil), want error", result)
	}
	assertSessionPathMissing(t, fixture.config)
	assertSessionPathMissing(t, fixture.state)
}

func TestSessionRejectsSelectionRepositoryMismatchWithoutConfigWrite(t *testing.T) {
	fixture := newFixture(t)
	session, err := OpenSession(fixture.home, sessionTestControls(fixture))
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	machine := sessionTestMachine(filepath.Join(fixture.root, "other-repository"))
	if changed, err := session.PublishSelection(machine); err == nil || changed {
		t.Fatalf("PublishSelection(repository mismatch) = (%t, %v), want unchanged error", changed, err)
	}
	assertSessionPathMissing(t, fixture.config)
	assertSessionPathMissing(t, filepath.Dir(fixture.config))
}

func TestSessionSelectionSemanticNoOpPreservesFileIdentityAndMetadata(t *testing.T) {
	fixture := newFixture(t)
	machine := sessionTestMachine(fixture.repository)
	if err := os.MkdirAll(filepath.Dir(fixture.config), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(config root) error = %v", err)
	}
	nonCanonical := fmt.Sprintf(
		"profiles = [\"base\", \"work\"]\n"+
			"repository = %q\n"+
			"version = 1\n"+
			"extra_modules = [\"tmux\"]\n",
		fixture.repository,
	)
	if err := os.WriteFile(fixture.config, []byte(nonCanonical), 0o600); err != nil {
		t.Fatalf("os.WriteFile(non-canonical config) error = %v", err)
	}
	session, err := OpenSession(fixture.home, sessionTestControls(fixture))
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	before, err := os.Stat(fixture.config)
	if err != nil {
		t.Fatalf("os.Stat(config before no-op) error = %v", err)
	}
	if got := before.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %04o, want 0600", got)
	}

	equivalent := config.Machine{
		Version:      machine.Version,
		Repository:   filepath.Clean(machine.Repository),
		Profiles:     append([]string(nil), machine.Profiles...),
		ExtraModules: append([]string(nil), machine.ExtraModules...),
	}
	changed, err := session.PublishSelection(equivalent)
	if err != nil || changed {
		t.Fatalf("PublishSelection(equivalent) = (%t, %v), want no-op", changed, err)
	}
	after, err := os.Stat(fixture.config)
	if err != nil {
		t.Fatalf("os.Stat(config after no-op) error = %v", err)
	}
	if !os.SameFile(before, after) {
		t.Error("semantic no-op replaced the machine config inode")
	}
	if before.Mode() != after.Mode() {
		t.Errorf("semantic no-op changed mode from %v to %v", before.Mode(), after.Mode())
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Errorf(
			"semantic no-op changed mtime from %v to %v",
			before.ModTime(),
			after.ModTime(),
		)
	}
	if data, err := os.ReadFile(fixture.config); err != nil || string(data) != nonCanonical {
		t.Errorf("semantic no-op config = (%q, %v), want original bytes", data, err)
	}
}

func TestSessionConvergeReloadsStateWrittenAfterOpen(t *testing.T) {
	fixture := newFixture(t)
	session, err := OpenSession(fixture.home, sessionTestControls(fixture))
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	destination := fixture.writeRepositoryFile(t, "modules/old/config", "old")
	target := filepath.Join(fixture.home, ".old")
	if err := os.Symlink(destination, target); err != nil {
		t.Fatalf("os.Symlink(stale target) error = %v", err)
	}
	fixture.writeLinkState(t, target, destination)

	result, err := session.Converge(nil, nil)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if !result.TargetsChanged || !result.StateChanged {
		t.Fatalf("Converge() = %#v, want stale target and state cleanup", result)
	}
	assertSessionPathMissing(t, target)
	loaded, err := state.Load(fixture.state, fixture.home)
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}
	if len(loaded.Snapshot.Modules) != 0 {
		t.Fatalf("state modules = %#v, want empty", loaded.Snapshot.Modules)
	}
}

func TestSessionConvergeRejectsStateCorruptedAfterOpen(t *testing.T) {
	fixture := newFixture(t)
	session, err := OpenSession(fixture.home, sessionTestControls(fixture))
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if err := os.WriteFile(fixture.state, []byte("{"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(corrupt state) error = %v", err)
	}

	result, err := session.Converge(nil, nil)
	if err == nil {
		t.Fatalf("Converge() = (%#v, nil), want state decode failure", result)
	}
	if result.TargetsChanged || result.StateChanged {
		t.Fatalf("Converge() result = %#v, want zero mutation", result)
	}
}

func TestSessionCloseFailureKeepsOwnershipForRetry(t *testing.T) {
	fixture := newFixture(t)
	controls := sessionTestControls(fixture)
	session, err := OpenSession(fixture.home, controls)
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	originalRelease := session.state.release
	attempts := 0
	session.state.release = func() error {
		attempts++
		if attempts == 1 {
			return fmt.Errorf("%w: synthetic release failure", lock.ErrIO)
		}
		return originalRelease()
	}

	if err := session.Close(); !errors.Is(err, lock.ErrIO) {
		t.Fatalf("Close(first) error = %v, want lock.ErrIO", err)
	}
	if changed, err := session.PublishSelection(sessionTestMachine(fixture.repository)); changed ||
		!errors.Is(err, ErrSessionClosing) {
		t.Fatalf("PublishSelection(after failed close) = (%t, %v), want ErrSessionClosing", changed, err)
	}
	if result, err := session.Converge(nil, nil); !errors.Is(err, ErrSessionClosing) {
		t.Fatalf("Converge(after failed close) = (%#v, %v), want ErrSessionClosing", result, err)
	}
	if contender, err := OpenSession(fixture.home, controls); contender != nil ||
		!errors.Is(err, lock.ErrBusy) {
		t.Fatalf("OpenSession(while close retry pending) = (%#v, %v), want busy", contender, err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close(retry) error = %v", err)
	}
	reopened, err := OpenSession(fixture.home, controls)
	if err != nil {
		t.Fatalf("OpenSession(after close retry) error = %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close(reopened) error = %v", err)
	}
}

func sessionTestControls(fixture fixture) corepaths.Controls {
	return corepaths.Controls{
		Repository: fixture.repository,
		Config:     fixture.config,
		State:      fixture.state,
		Lock:       fixture.lock,
	}
}

func sessionTestMachine(repository string) config.Machine {
	return config.Machine{
		Version:      1,
		Repository:   repository,
		Profiles:     []string{"base", "work"},
		ExtraModules: []string{"tmux"},
	}
}

func assertSessionPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("os.Lstat(%q) error = %v, want missing", path, err)
	}
}
