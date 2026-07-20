package backup

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestBatchSaveRegular_PreservesBytesModeAndPrivateParents(t *testing.T) {
	fixture := t.TempDir()
	batch := newTestBatch(t, filepath.Join(fixture, "backup"))
	source := filepath.Join(fixture, "target")
	content := []byte("private configuration\nwith two lines\n")
	writeModeFile(t, source, content, 0o640)

	wantHash := digest(content)
	path, err := batch.SaveRegular(source, "home/.config/app/config", wantHash, 0o640)
	if err != nil {
		t.Fatalf("SaveRegular() error = %v", err)
	}
	wantPath := filepath.Join(batch.Path(), "home", ".config", "app", "config")
	if path != wantPath {
		t.Fatalf("SaveRegular() path = %q, want %q", path, wantPath)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(content) {
		t.Fatalf("backup content = %q, %v; want %q", got, err, content)
	}
	assertPermissions(t, path, 0o640)
	for _, parent := range []string{
		filepath.Join(batch.Path(), "home"),
		filepath.Join(batch.Path(), "home", ".config"),
		filepath.Join(batch.Path(), "home", ".config", "app"),
	} {
		assertPermissions(t, parent, 0o700)
	}
	if got, err := os.ReadFile(source); err != nil || string(got) != string(content) {
		t.Fatalf("source after backup = %q, %v; want unchanged", got, err)
	}
}

func TestBatchSaveRegular_ValidatesPlanDigestAndMode(t *testing.T) {
	fixture := t.TempDir()
	content := []byte("planned bytes")
	source := filepath.Join(fixture, "target")
	writeModeFile(t, source, content, 0o600)

	tests := []struct {
		name string
		hash string
		mode os.FileMode
	}{
		{name: "digest mismatch", hash: digest([]byte("other")), mode: 0o600},
		{name: "mode mismatch", hash: digest(content), mode: 0o640},
		{name: "unsupported digest", hash: "sha1:abcd", mode: 0o600},
		{name: "non permission mode", hash: digest(content), mode: os.ModeDir | 0o600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batch := newTestBatch(t, filepath.Join(fixture, strings.ReplaceAll(tt.name, " ", "-")))
			path := filepath.Join(batch.Path(), "target")
			if _, err := batch.SaveRegular(source, "target", tt.hash, tt.mode); err == nil {
				t.Fatalf("SaveRegular() error = nil, want plan evidence rejection")
			}
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("os.Lstat(%q) error = %v, want no failed backup", path, err)
			}
		})
	}
}

func TestBatchSaveSymlink_PreservesRawLinkTextWithoutFollowing(t *testing.T) {
	fixture := t.TempDir()
	batch := newTestBatch(t, filepath.Join(fixture, "backup"))
	source := filepath.Join(fixture, "dangling-link")
	wantText := "../missing/raw-target"
	if err := os.Symlink(wantText, source); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}

	path, err := batch.SaveSymlink(source, "home/link", wantText)
	if err != nil {
		t.Fatalf("SaveSymlink() error = %v", err)
	}
	if got, err := os.Readlink(path); err != nil || got != wantText {
		t.Fatalf("os.Readlink(%q) = (%q, %v), want %q", path, got, err, wantText)
	}
	assertPermissions(t, filepath.Dir(path), 0o700)
}

func TestBatchSaveSymlink_RejectsChangedLinkText(t *testing.T) {
	fixture := t.TempDir()
	batch := newTestBatch(t, filepath.Join(fixture, "backup"))
	source := filepath.Join(fixture, "link")
	if err := os.Symlink("actual", source); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	if _, err := batch.SaveSymlink(source, "link", "planned"); err == nil {
		t.Fatalf("SaveSymlink() error = nil, want link text mismatch")
	}
	if _, err := os.Lstat(filepath.Join(batch.Path(), "link")); !os.IsNotExist(err) {
		t.Fatalf("failed backup exists: os.Lstat() error = %v", err)
	}
}

func TestBatchSave_RejectsDirectoryAndSpecialSource(t *testing.T) {
	fixture := t.TempDir()
	batch := newTestBatch(t, filepath.Join(fixture, "backup"))
	directory := filepath.Join(fixture, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}
	fifo := filepath.Join(fixture, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("syscall.Mkfifo() error = %v", err)
	}

	for _, source := range []string{directory, fifo} {
		name := filepath.Base(source)
		t.Run(name+" as regular", func(t *testing.T) {
			if _, err := batch.SaveRegular(source, name+"-regular", digest(nil), 0o600); err == nil {
				t.Fatalf("SaveRegular(%q) error = nil, want rejection", source)
			}
		})
		t.Run(name+" as symlink", func(t *testing.T) {
			if _, err := batch.SaveSymlink(source, name+"-symlink", "raw"); err == nil {
				t.Fatalf("SaveSymlink(%q) error = nil, want rejection", source)
			}
		})
	}
}

func TestBatchSave_RejectsUnsafeRelativePathAndNeverOverwrites(t *testing.T) {
	fixture := t.TempDir()
	batch := newTestBatch(t, filepath.Join(fixture, "backup"))
	source := filepath.Join(fixture, "target")
	content := []byte("new")
	writeModeFile(t, source, content, 0o600)

	for _, relative := range []string{"", ".", "../escape", "a/../target", filepath.Join(string(filepath.Separator), "absolute")} {
		if _, err := batch.SaveRegular(source, relative, digest(content), 0o600); err == nil {
			t.Errorf("SaveRegular(relative=%q) error = nil, want rejection", relative)
		}
	}

	existing := filepath.Join(batch.Path(), "existing")
	writeModeFile(t, existing, []byte("keep"), 0o600)
	if _, err := batch.SaveRegular(source, "existing", digest(content), 0o600); err == nil {
		t.Fatalf("SaveRegular(existing) error = nil, want no-overwrite rejection")
	}
	if got, err := os.ReadFile(existing); err != nil || string(got) != "keep" {
		t.Fatalf("existing backup = %q, %v; want unchanged", got, err)
	}
}

func newTestBatch(t *testing.T, root string) *Batch {
	t.Helper()
	batch, err := NewBatch(root)
	if err != nil {
		t.Fatalf("NewBatch(%q) error = %v", root, err)
	}
	return batch
}

func writeModeFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("os.Chmod(%q) error = %v", path, err)
	}
}

func digest(content []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(content))
}
