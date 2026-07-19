package state

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mianm12/dotfiles/internal/storage"
)

func TestStore_CreatesAndAtomicallyReplacesPrivateState(t *testing.T) {
	t.Run("first state", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "state", "dot")
		path := filepath.Join(root, "state.json")
		snapshot := storeTestSnapshot(t, "first")

		if err := Store(root, path, snapshot); err != nil {
			t.Fatalf("Store() error = %v", err)
		}
		assertStoredSnapshot(t, root, path, snapshot)
	})

	t.Run("replace old state and permissions", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "state")
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatalf("os.Mkdir() error = %v", err)
		}
		path := filepath.Join(root, "state.json")
		if err := os.WriteFile(path, []byte("old state bytes"), 0o644); err != nil {
			t.Fatalf("os.WriteFile(old state) error = %v", err)
		}
		snapshot := storeTestSnapshot(t, "second")

		if err := Store(root, path, snapshot); err != nil {
			t.Fatalf("Store() error = %v", err)
		}
		assertStoredSnapshot(t, root, path, snapshot)
	})
}

func TestStore_PublishesOnlyAtRenameBoundary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	oldState := []byte("complete old state")
	if err := os.WriteFile(path, oldState, 0o600); err != nil {
		t.Fatalf("os.WriteFile(old state) error = %v", err)
	}
	snapshot := storeTestSnapshot(t, "new")
	wantNew, err := Encode(snapshot)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	beforeRename := make(chan struct{})
	continueRename := make(chan struct{})
	operations := defaultStoreOperations()
	rename := operations.rename
	operations.rename = func(oldPath, newPath string) error {
		close(beforeRename)
		<-continueRename
		return rename(oldPath, newPath)
	}
	result := make(chan error, 1)
	go func() {
		result <- store(root, path, snapshot, operations)
	}()

	select {
	case <-beforeRename:
	case err := <-result:
		t.Fatalf("store() returned before rename boundary: %v", err)
	case <-time.After(5 * time.Second):
		close(continueRename)
		t.Fatal("store() did not reach rename boundary")
	}
	if got := readFile(t, path); !reflect.DeepEqual(got, oldState) {
		t.Fatalf("state before rename = %q, want complete old state %q", got, oldState)
	}
	close(continueRename)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("store() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("store() did not finish after rename was released")
	}
	if got := readFile(t, path); !reflect.DeepEqual(got, wantNew) {
		t.Fatalf("state after rename = %q, want complete new state %q", got, wantNew)
	}
}

func TestStore_PrePublishFailuresPreserveOldState(t *testing.T) {
	tests := []struct {
		name              string
		fileFailure       storeFileFailure
		configure         func(*storeOperations, error)
		wantCause         error
		wantCleanupCause  bool
		wantTemporaryFile bool
	}{
		{
			name: "create",
			configure: func(operations *storeOperations, injected error) {
				operations.createTemp = func(string, string) (storeFile, error) {
					return nil, injected
				}
			},
		},
		{name: "permissions", fileFailure: storeFileFailChmod},
		{name: "write", fileFailure: storeFileFailWrite},
		{name: "partial write", fileFailure: storeFileFailShortWrite, wantCause: io.ErrShortWrite},
		{name: "sync", fileFailure: storeFileFailSync},
		{name: "close", fileFailure: storeFileFailClose},
		{
			name: "rename",
			configure: func(operations *storeOperations, injected error) {
				operations.rename = func(string, string) error { return injected }
			},
		},
		{
			name: "cleanup",
			configure: func(operations *storeOperations, injected error) {
				operations.rename = func(string, string) error { return injected }
				operations.remove = func(string) error { return errInjectedCleanup }
			},
			wantCleanupCause:  true,
			wantTemporaryFile: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "state.json")
			oldState := []byte("old state must survive")
			const oldMode fs.FileMode = 0o640
			if err := os.WriteFile(path, oldState, oldMode); err != nil {
				t.Fatalf("os.WriteFile(old state) error = %v", err)
			}
			if err := os.Chmod(path, oldMode); err != nil {
				t.Fatalf("os.Chmod(old state) error = %v", err)
			}
			injected := errors.New("injected " + test.name + " failure")
			operations := defaultStoreOperations()
			if test.fileFailure != storeFileFailNone {
				createTemp := operations.createTemp
				operations.createTemp = func(dir, pattern string) (storeFile, error) {
					file, err := createTemp(dir, pattern)
					if err != nil {
						return nil, err
					}
					return &failingStoreFile{storeFile: file, failure: test.fileFailure, err: injected}, nil
				}
			}
			if test.configure != nil {
				test.configure(&operations, injected)
			}

			err := store(root, path, storeTestSnapshot(t, "new"), operations)
			wantCause := test.wantCause
			if wantCause == nil {
				wantCause = injected
			}
			if !errors.Is(err, wantCause) {
				t.Fatalf("store() error = %v, want cause %v", err, wantCause)
			}
			if test.wantCleanupCause && !errors.Is(err, errInjectedCleanup) {
				t.Fatalf("store() error = %v, want cleanup cause", err)
			}
			if got := readFile(t, path); !reflect.DeepEqual(got, oldState) {
				t.Fatalf("state after %s failure = %q, want old %q", test.name, got, oldState)
			}
			assertMode(t, path, oldMode)
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("os.ReadDir() error = %v", err)
			}
			wantEntries := 1
			if test.wantTemporaryFile {
				wantEntries = 2
			}
			if len(entries) != wantEntries {
				t.Fatalf("entries after %s failure = %v, want %d", test.name, entries, wantEntries)
			}
		})
	}
}

func TestStore_RejectsInvalidInputsBeforePublishing(t *testing.T) {
	t.Run("invalid paths are zero write", func(t *testing.T) {
		base := t.TempDir()
		t.Chdir(base)
		cases := []struct {
			root string
			path string
		}{
			{root: "relative", path: "relative/state.json"},
			{root: filepath.Join(base, "state"), path: filepath.Join(base, "outside.json")},
		}
		for _, test := range cases {
			if err := Store(test.root, test.path, storeTestSnapshot(t, "valid")); err == nil {
				t.Fatalf("Store(%q, %q) error = nil, want path error", test.root, test.path)
			}
		}
		if entries, err := os.ReadDir(base); err != nil || len(entries) != 0 {
			t.Fatalf("invalid path Store changed base: entries=%v err=%v", entries, err)
		}
	})

	t.Run("invalid Snapshot is zero write", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, "state")
		if err := Store(root, filepath.Join(root, "state.json"), Snapshot{}); err == nil {
			t.Fatal("Store(zero Snapshot) error = nil, want error")
		}
		if entries, err := os.ReadDir(base); err != nil || len(entries) != 0 {
			t.Fatalf("invalid Snapshot Store changed base: entries=%v err=%v", entries, err)
		}
	})

	t.Run("abnormal destination is preserved", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, "state")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("os.Mkdir() error = %v", err)
		}
		external := filepath.Join(base, "external")
		if err := os.WriteFile(external, []byte("external data"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(external) error = %v", err)
		}
		path := filepath.Join(root, "state.json")
		if err := os.Symlink(external, path); err != nil {
			t.Fatalf("os.Symlink() error = %v", err)
		}

		if err := Store(root, path, storeTestSnapshot(t, "valid")); err == nil {
			t.Fatal("Store(symlink destination) error = nil, want error")
		}
		if got := readFile(t, external); string(got) != "external data" {
			t.Fatalf("external bytes = %q, want preserved", got)
		}
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("state path after rejection = (%v, %v), want symlink", info, err)
		}
	})

	t.Run("directory destination is preserved", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "state")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("os.Mkdir(root) error = %v", err)
		}
		path := filepath.Join(root, "state.json")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("os.Mkdir(state path) error = %v", err)
		}

		if err := Store(root, path, storeTestSnapshot(t, "valid")); err == nil {
			t.Fatal("Store(directory destination) error = nil, want error")
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("state path after rejection = (%v, %v), want directory", info, err)
		}
	})
}

var errInjectedCleanup = errors.New("injected cleanup failure")

type storeFileFailure uint8

const (
	storeFileFailNone storeFileFailure = iota
	storeFileFailChmod
	storeFileFailWrite
	storeFileFailShortWrite
	storeFileFailSync
	storeFileFailClose
)

type failingStoreFile struct {
	storeFile
	failure storeFileFailure
	err     error
}

func (file *failingStoreFile) Chmod(mode fs.FileMode) error {
	if file.failure == storeFileFailChmod {
		return file.err
	}
	return file.storeFile.Chmod(mode)
}

func (file *failingStoreFile) Write(data []byte) (int, error) {
	if file.failure == storeFileFailWrite {
		return 0, file.err
	}
	if file.failure == storeFileFailShortWrite {
		return len(data) - 1, nil
	}
	return file.storeFile.Write(data)
}

func (file *failingStoreFile) Sync() error {
	if file.failure == storeFileFailSync {
		return file.err
	}
	return file.storeFile.Sync()
}

func (file *failingStoreFile) Close() error {
	err := file.storeFile.Close()
	if file.failure == storeFileFailClose {
		return file.err
	}
	return err
}

func storeTestSnapshot(t *testing.T, module string) Snapshot {
	t.Helper()
	document := testDocument()
	document["entries"] = map[string]any{
		"~/.config/" + module + "/file": map[string]any{
			"module":     module,
			"kind":       "scaffold",
			"source":     "modules/" + module + "/file.template",
			"applied_at": "2026-07-14T10:00:00Z",
		},
	}
	snapshot, err := Decode(marshalDocument(t, document))
	if err != nil {
		t.Fatalf("Decode(store fixture) error = %v", err)
	}
	return snapshot
}

func assertStoredSnapshot(t *testing.T, root, path string, snapshot Snapshot) {
	t.Helper()
	assertMode(t, root, storage.PrivateDirectoryMode)
	assertMode(t, path, storage.PrivateFileMode)
	want, err := Encode(snapshot)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if got := readFile(t, path); !reflect.DeepEqual(got, want) {
		t.Fatalf("stored state = %q, want %q", got, want)
	}
	loaded, err := Load(path)
	snapshot, ok := loaded.Snapshot()
	if err != nil || loaded.Status() != StatusLoaded || !ok || snapshot.Version() != 1 {
		t.Fatalf("Load(stored state) = (%#v, %v), want loaded v1", loaded, err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", path, err)
	}
	return data
}

func assertMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(%q) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%q) = %04o, want %04o", path, got, want)
	}
}
