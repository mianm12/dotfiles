package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

	if err := os.Chmod(root, PrivateDirectoryMode|fs.ModeSticky); err != nil {
		t.Fatalf("os.Chmod(%q) sticky error = %v", root, err)
	}
	if err := EnsureRoot(root); err != nil {
		t.Fatalf("EnsureRoot(%q) after sticky mode error = %v", root, err)
	}
	assertMode(t, root, PrivateDirectoryMode)
}

func TestChmodPrivateIfNeededSkipsExactModeAndCorrectsMismatch(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	for _, test := range []struct {
		name    string
		current fs.FileMode
		want    fs.FileMode
		match   bool
	}{
		{
			name:    "exact directory",
			current: fs.ModeDir | PrivateDirectoryMode,
			want:    PrivateDirectoryMode,
			match:   true,
		},
		{
			name:    "exact file",
			current: PrivateFileMode,
			want:    PrivateFileMode,
			match:   true,
		},
		{name: "broad", current: 0o755, want: PrivateDirectoryMode},
		{name: "narrow", current: 0o600, want: PrivateDirectoryMode},
		{
			name:    "setuid",
			current: PrivateDirectoryMode | fs.ModeSetuid,
			want:    PrivateDirectoryMode,
		},
		{
			name:    "setgid",
			current: PrivateDirectoryMode | fs.ModeSetgid,
			want:    PrivateDirectoryMode,
		},
		{
			name:    "sticky",
			current: PrivateDirectoryMode | fs.ModeSticky,
			want:    PrivateDirectoryMode,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := chmodPrivateIfNeeded(missing, test.current, test.want)
			if test.match && err != nil {
				t.Fatalf("chmodPrivateIfNeeded(exact mode) error = %v", err)
			}
			if !test.match && err == nil {
				t.Fatal("chmodPrivateIfNeeded(mismatched mode) error = nil, want chmod attempt")
			}
		})
	}
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

func TestEnsureRoot_RejectsFinalSymlinkWithoutChangingTarget(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "external")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", target, err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatalf("os.Chmod(%q) error = %v", target, err)
	}
	root := filepath.Join(base, "state")
	if err := os.Symlink(target, root); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", target, root, err)
	}

	err := EnsureRoot(root)
	if err == nil {
		t.Fatal("EnsureRoot() error = nil, want final symlink error")
	}
	if !strings.Contains(err.Error(), root) || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("EnsureRoot() error = %q, want path and symbolic-link detail", err)
	}
	info, inspectErr := os.Lstat(root)
	if inspectErr != nil {
		t.Fatalf("os.Lstat(%q) error = %v", root, inspectErr)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("mode(%q) = %v, want symlink preserved", root, info.Mode())
	}
	assertMode(t, target, 0o755)
}

func TestEnsureRoot_RejectsDanglingFinalSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "missing")
	root := filepath.Join(base, "state")
	if err := os.Symlink(target, root); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", target, root, err)
	}

	err := EnsureRoot(root)
	if err == nil {
		t.Fatal("EnsureRoot() error = nil, want dangling final symlink error")
	}
	if !strings.Contains(err.Error(), root) || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("EnsureRoot() error = %q, want path and symbolic-link detail", err)
	}
	info, inspectErr := os.Lstat(root)
	if inspectErr != nil {
		t.Fatalf("os.Lstat(%q) error = %v", root, inspectErr)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("mode(%q) = %v, want symlink preserved", root, info.Mode())
	}
	if _, inspectErr := os.Lstat(target); !errors.Is(inspectErr, fs.ErrNotExist) {
		t.Fatalf("os.Lstat(%q) error = %v, want target to remain missing", target, inspectErr)
	}
}

func TestEnsureRoot_AllowsParentSymlink(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "external")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", parent, err)
	}
	parentLink := filepath.Join(base, "state")
	if err := os.Symlink(parent, parentLink); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", parent, parentLink, err)
	}
	root := filepath.Join(parentLink, "dot")

	if err := EnsureRoot(root); err != nil {
		t.Fatalf("EnsureRoot(%q) error = %v", root, err)
	}
	assertMode(t, filepath.Join(parent, "dot"), PrivateDirectoryMode)
	info, err := os.Lstat(parentLink)
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", parentLink, err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("mode(%q) = %v, want parent symlink preserved", parentLink, info.Mode())
	}
}

func TestValidateRoot_IsReadOnly(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "state")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", root, err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("os.Chmod(%q) error = %v", root, err)
	}

	if err := ValidateRoot(root); err != nil {
		t.Fatalf("ValidateRoot(%q) error = %v", root, err)
	}
	assertMode(t, root, 0o755)

	missing := filepath.Join(base, "missing")
	if err := ValidateRoot(missing); err != nil {
		t.Fatalf("ValidateRoot(%q) error = %v", missing, err)
	}
	if _, err := os.Lstat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("os.Lstat(%q) error = %v, want missing path preserved", missing, err)
	}
}

func TestValidatePrivateFile_IsReadOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lock")
	if err := os.WriteFile(path, []byte("lock"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("os.Chmod(%q) error = %v", path, err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", path, err)
	}

	if err := ValidatePrivateFile(path); err != nil {
		t.Fatalf("ValidatePrivateFile(%q) error = %v", path, err)
	}
	after, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("os.Lstat(%q) after validation error = %v", path, err)
	}
	if after.Mode().Perm() != 0o644 || !os.SameFile(before, after) {
		t.Fatalf("ValidatePrivateFile() changed regular file: before=%v after=%v", before, after)
	}

	missing := filepath.Join(root, "missing")
	if err := ValidatePrivateFile(missing); err != nil {
		t.Fatalf("ValidatePrivateFile(%q) error = %v", missing, err)
	}
	if _, err := os.Lstat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("os.Lstat(%q) error = %v, want missing path preserved", missing, err)
	}

	abnormal := filepath.Join(root, "directory")
	if err := os.Mkdir(abnormal, 0o755); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", abnormal, err)
	}
	if err := ValidatePrivateFile(abnormal); err == nil {
		t.Fatal("ValidatePrivateFile(directory) error = nil")
	}
	assertMode(t, abnormal, 0o755)
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

func TestPublishPrivateFileCreatesAndReplacesPrivately(t *testing.T) {
	root := filepath.Join(t.TempDir(), "control")
	path := filepath.Join(root, "state.json")

	changed, err := PublishPrivateFile(path, []byte("first"))
	if err != nil || !changed {
		t.Fatalf("PublishPrivateFile(first) = (%t, %v), want changed", changed, err)
	}
	assertMode(t, root, PrivateDirectoryMode)
	assertMode(t, path, PrivateFileMode)
	if data, err := os.ReadFile(path); err != nil || string(data) != "first" {
		t.Fatalf("published data = (%q, %v), want first", data, err)
	}

	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("os.Chmod(root) error = %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("os.Chmod(path) error = %v", err)
	}
	changed, err = PublishPrivateFile(path, []byte("second"))
	if err != nil || !changed {
		t.Fatalf("PublishPrivateFile(second) = (%t, %v), want changed", changed, err)
	}
	assertMode(t, root, PrivateDirectoryMode)
	assertMode(t, path, PrivateFileMode)
	if data, err := os.ReadFile(path); err != nil || string(data) != "second" {
		t.Fatalf("published data = (%q, %v), want second", data, err)
	}
}

func TestPublishPrivateFileIdenticalContentIsStrictNoOp(t *testing.T) {
	root := filepath.Join(t.TempDir(), "control")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("os.Mkdir(root) error = %v", err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("os.Chmod(root) error = %v", err)
	}
	path := filepath.Join(root, "state.json")
	if err := os.WriteFile(path, []byte("same"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(path) error = %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("os.Chmod(path) error = %v", err)
	}
	beforeRoot, err := os.Stat(root)
	if err != nil {
		t.Fatalf("os.Stat(root before) error = %v", err)
	}
	beforeFile, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(path before) error = %v", err)
	}

	changed, err := publishPrivateFile(
		path,
		[]byte("same"),
		func(string) (pendingPrivateFile, error) {
			t.Fatal("identical content created a temporary file")
			return nil, nil
		},
	)
	if err != nil || changed {
		t.Fatalf("publishPrivateFile(identical) = (%t, %v), want no-op", changed, err)
	}

	afterRoot, err := os.Stat(root)
	if err != nil {
		t.Fatalf("os.Stat(root after) error = %v", err)
	}
	afterFile, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(path after) error = %v", err)
	}
	if !os.SameFile(beforeRoot, afterRoot) ||
		beforeRoot.Mode() != afterRoot.Mode() ||
		!beforeRoot.ModTime().Equal(afterRoot.ModTime()) {
		t.Fatalf("identical publish changed root metadata: before=%v after=%v", beforeRoot, afterRoot)
	}
	if !os.SameFile(beforeFile, afterFile) ||
		beforeFile.Mode() != afterFile.Mode() ||
		!beforeFile.ModTime().Equal(afterFile.ModTime()) {
		t.Fatalf("identical publish changed file metadata: before=%v after=%v", beforeFile, afterFile)
	}
}

func TestPublishPrivateFileRejectsFinalSymlinkWithoutChangingTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "external")
	if err := os.WriteFile(target, []byte("external"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(target) error = %v", err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatalf("os.Chmod(target) error = %v", err)
	}
	path := filepath.Join(root, "state.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("os.Symlink(target, path) error = %v", err)
	}

	changed, err := PublishPrivateFile(path, []byte("replacement"))
	if err == nil || changed {
		t.Fatalf("PublishPrivateFile(symlink) = (%t, %v), want unchanged error", changed, err)
	}
	info, inspectErr := os.Lstat(path)
	if inspectErr != nil {
		t.Fatalf("os.Lstat(path) error = %v", inspectErr)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("mode(path) = %v, want symlink", info.Mode())
	}
	if data, readErr := os.ReadFile(target); readErr != nil || string(data) != "external" {
		t.Fatalf("target data = (%q, %v), want external", data, readErr)
	}
	assertMode(t, target, 0o644)
}

func TestPublishPrivateFileFailureCleansUpAndPreservesExistingFile(t *testing.T) {
	newPendingErr := errors.New("injected pending creation failure")
	chmodErr := errors.New("injected chmod failure")
	writeErr := errors.New("injected write failure")
	publishErr := errors.New("injected atomic publish failure")
	cleanupErr := errors.New("injected cleanup failure")
	tests := []struct {
		name        string
		factoryErr  error
		pending     failingPendingPrivateFile
		wantPrimary error
		wantCleanup bool
	}{
		{
			name:        "create",
			factoryErr:  newPendingErr,
			wantPrimary: newPendingErr,
		},
		{
			name:        "chmod",
			pending:     failingPendingPrivateFile{chmodErr: chmodErr},
			wantPrimary: chmodErr,
			wantCleanup: true,
		},
		{
			name:        "write",
			pending:     failingPendingPrivateFile{writeErr: writeErr},
			wantPrimary: writeErr,
			wantCleanup: true,
		},
		{
			name:        "publish",
			pending:     failingPendingPrivateFile{publishErr: publishErr},
			wantPrimary: publishErr,
			wantCleanup: true,
		},
		{
			name: "cleanup error is joined",
			pending: failingPendingPrivateFile{
				writeErr:   writeErr,
				cleanupErr: cleanupErr,
			},
			wantPrimary: writeErr,
			wantCleanup: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "control")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatalf("os.Mkdir(root) error = %v", err)
			}
			path := filepath.Join(root, "state.json")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatalf("os.WriteFile(path) error = %v", err)
			}
			pending := test.pending

			changed, err := publishPrivateFile(
				path,
				[]byte("replacement"),
				func(string) (pendingPrivateFile, error) {
					if test.factoryErr != nil {
						return nil, test.factoryErr
					}
					return &pending, nil
				},
			)
			if changed || !errors.Is(err, test.wantPrimary) {
				t.Fatalf(
					"publishPrivateFile() = (%t, %v), want unchanged error containing %v",
					changed,
					err,
					test.wantPrimary,
				)
			}
			if pending.cleanupCalled != test.wantCleanup {
				t.Fatalf(
					"Cleanup called = %t, want %t",
					pending.cleanupCalled,
					test.wantCleanup,
				)
			}
			if test.pending.cleanupErr != nil && !errors.Is(err, cleanupErr) {
				t.Fatalf("publishPrivateFile() error = %v, want joined cleanup error", err)
			}
			if data, readErr := os.ReadFile(path); readErr != nil || string(data) != "original" {
				t.Fatalf("existing data = (%q, %v), want original", data, readErr)
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
	if changed, err := PublishPrivateFile("relative-state", []byte("state")); err == nil || changed {
		t.Fatalf("PublishPrivateFile(relative) = (%t, %v), want unchanged error", changed, err)
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
	if got := info.Mode() & privateModeMask; got != want {
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

type failingPendingPrivateFile struct {
	chmodErr      error
	writeErr      error
	publishErr    error
	cleanupErr    error
	cleanupCalled bool
}

func (file *failingPendingPrivateFile) Chmod(fs.FileMode) error {
	return file.chmodErr
}

func (file *failingPendingPrivateFile) Write(data []byte) (int, error) {
	if file.writeErr != nil {
		return 0, file.writeErr
	}
	return len(data), nil
}

func (file *failingPendingPrivateFile) CloseAtomicallyReplace() error {
	return file.publishErr
}

func (file *failingPendingPrivateFile) Cleanup() error {
	file.cleanupCalled = true
	return file.cleanupErr
}
