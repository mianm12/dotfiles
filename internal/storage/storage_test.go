package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureRoot_CreatesAndCorrectsPrivateDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state", "dot")

	if err := EnsureRoot(root); err != nil {
		t.Fatalf("EnsureRoot(%q) error = %v", root, err)
	}
	assertMode(t, root, PrivateDirectoryMode)

	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("os.Chmod(%q) error = %v", root, err)
	}
	if err := EnsureRoot(root); err != nil {
		t.Fatalf("EnsureRoot(%q) after broad mode error = %v", root, err)
	}
	assertMode(t, root, PrivateDirectoryMode)
}

func TestEnsureRoot_RejectsNonDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", root, err)
	}

	if err := EnsureRoot(root); err == nil {
		t.Fatal("EnsureRoot() error = nil, want non-directory error")
	}
}

func TestEnsurePrivateFile_CreatesAndCorrectsMode(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := EnsureRoot(root); err != nil {
		t.Fatalf("EnsureRoot(%q) error = %v", root, err)
	}
	path := filepath.Join(root, "lock")

	if err := EnsurePrivateFile(path); err != nil {
		t.Fatalf("EnsurePrivateFile(%q) error = %v", path, err)
	}
	assertMode(t, path, PrivateFileMode)

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("os.Chmod(%q) error = %v", path, err)
	}
	if err := EnsurePrivateFile(path); err != nil {
		t.Fatalf("EnsurePrivateFile(%q) after broad mode error = %v", path, err)
	}
	assertMode(t, path, PrivateFileMode)
}

func TestEnsurePrivateFile_RejectsAbnormalObjects(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "lock")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", path, err)
		}

		if err := EnsurePrivateFile(path); err == nil {
			t.Fatal("EnsurePrivateFile() error = nil, want directory error")
		}
	})

	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", target, err)
		}
		path := filepath.Join(root, "lock")
		if err := os.Symlink(target, path); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", target, path, err)
		}

		if err := EnsurePrivateFile(path); err == nil {
			t.Fatal("EnsurePrivateFile() error = nil, want symlink error")
		}
	})
}

func TestEnsurePrivateFile_PostCreateFailurePreservesPublishedInode(t *testing.T) {
	tests := []struct {
		name     string
		chmodErr error
		closeErr error
	}{
		{name: "chmod", chmodErr: errors.New("injected chmod failure")},
		{name: "close", closeErr: errors.New("injected close failure")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "lock")
			var contender *os.File
			t.Cleanup(func() {
				if contender != nil {
					_ = contender.Close()
				}
			})

			err := ensurePrivateFile(path, func(name string, flag int, perm fs.FileMode) (privateFile, error) {
				file, err := os.OpenFile(name, flag, perm)
				if err != nil {
					return nil, err
				}
				contender, err = os.Open(name)
				if err != nil {
					_ = file.Close()
					return nil, err
				}
				return &failingPrivateFile{File: file, chmodErr: test.chmodErr, closeErr: test.closeErr}, nil
			})
			if err == nil {
				t.Fatal("ensurePrivateFile() error = nil, want injected post-create error")
			}

			pathInfo, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatalf("os.Stat(%q) after post-create error = %v, want published inode preserved", path, statErr)
			}
			contenderInfo, statErr := contender.Stat()
			if statErr != nil {
				t.Fatalf("contender.Stat() error = %v", statErr)
			}
			if !os.SameFile(pathInfo, contenderInfo) {
				t.Fatal("lock path no longer names the inode already opened by a contender")
			}
		})
	}
}

func TestEnsurePaths_RejectRelativePathsWithoutWriting(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	if err := EnsureRoot("relative-state"); err == nil {
		t.Fatal("EnsureRoot() error = nil, want relative path error")
	}
	if err := EnsurePrivateFile("relative-lock"); err == nil {
		t.Fatal("EnsurePrivateFile() error = nil, want relative path error")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("os.ReadDir(%q) error = %v", root, err)
	}
	if len(entries) != 0 {
		t.Fatalf("relative path validation wrote entries: %v", entries)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(%q) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode(%q) = %04o, want %04o", path, got, want)
	}
}

type failingPrivateFile struct {
	*os.File
	chmodErr error
	closeErr error
}

func (file *failingPrivateFile) Chmod(mode fs.FileMode) error {
	if file.chmodErr != nil {
		return file.chmodErr
	}
	return file.File.Chmod(mode)
}

func (file *failingPrivateFile) Close() error {
	err := file.File.Close()
	if file.closeErr != nil {
		return file.closeErr
	}
	return err
}
