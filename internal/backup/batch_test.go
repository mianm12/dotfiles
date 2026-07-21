package backup

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNewBatch_CreatesUniquePrivateHierarchy(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state", "backup")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", root, err)
	}

	first, err := NewBatch(root)
	if err != nil {
		t.Fatalf("NewBatch() first error = %v", err)
	}
	second, err := NewBatch(root)
	if err != nil {
		t.Fatalf("NewBatch() second error = %v", err)
	}
	if first.Path() == second.Path() {
		t.Fatalf("batch paths both = %q, want unique paths", first.Path())
	}
	if filepath.Dir(first.Path()) != root || filepath.Dir(second.Path()) != root {
		t.Fatalf("batch paths = (%q, %q), want direct children of %q", first.Path(), second.Path(), root)
	}

	assertPermissions(t, root, 0o700)
	assertPermissions(t, first.Path(), 0o700)
	assertPermissions(t, second.Path(), 0o700)
}

func TestNewBatch_RejectsInvalidRoot(t *testing.T) {
	for _, root := range []string{"", "relative/backup"} {
		t.Run(root, func(t *testing.T) {
			if _, err := NewBatch(root); err == nil {
				t.Fatalf("NewBatch(%q) error = nil, want rejection", root)
			}
		})
	}
}

func TestNewBatch_RejectsNonDirectoryRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "backup")
	if err := os.WriteFile(root, []byte("occupied"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", root, err)
	}
	if _, err := NewBatch(root); err == nil {
		t.Fatalf("NewBatch() error = nil, want non-directory rejection")
	}
}

func TestNewBatch_SyncsNewDirectoryEntryChainBeforeSuccess(t *testing.T) {
	fixture := t.TempDir()
	root := filepath.Join(fixture, "state", "dot", "backup")
	var synced []string
	replaceDirectorySyncOpener(t, func(path string) (directorySyncer, error) {
		synced = append(synced, path)
		return stubDirectorySyncer{}, nil
	})

	batch, err := NewBatch(root)
	if err != nil {
		t.Fatalf("NewBatch() error = %v", err)
	}
	want := []string{
		batch.Path(),
		root,
		filepath.Join(fixture, "state", "dot"),
		filepath.Join(fixture, "state"),
		fixture,
	}
	if !reflect.DeepEqual(synced, want) {
		t.Fatalf("synced directories = %#v, want leaf-to-existing chain %#v", synced, want)
	}
}

func TestNewBatch_SyncFailureNeverReportsSuccess(t *testing.T) {
	fixture := t.TempDir()
	root := filepath.Join(fixture, "state", "dot", "backup")
	wantFailurePath := filepath.Join(fixture, "state", "dot")
	replaceDirectorySyncOpener(t, func(path string) (directorySyncer, error) {
		stub := stubDirectorySyncer{}
		if path == wantFailurePath {
			stub.syncErr = errors.New("injected sync failure")
		}
		return stub, nil
	})

	batch, err := NewBatch(root)
	if err == nil || batch != nil {
		t.Fatalf("NewBatch() = (%#v, %v), want failure without usable batch", batch, err)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatalf("os.ReadDir(%q) error = %v", root, readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("backup root entries after failed sync = %v, want incomplete batch removed", entries)
	}
}

type stubDirectorySyncer struct {
	syncErr error
}

func (stub stubDirectorySyncer) Sync() error { return stub.syncErr }
func (stubDirectorySyncer) Close() error     { return nil }

func replaceDirectorySyncOpener(t *testing.T, replacement directorySyncOpener) {
	t.Helper()
	original := openDirectoryForSync
	openDirectoryForSync = replacement
	t.Cleanup(func() {
		openDirectoryForSync = original
	})
}

func assertPermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(%q) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions %q = %04o, want %04o", path, got, want)
	}
}
