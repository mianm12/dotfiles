package backup

import (
	"os"
	"path/filepath"
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
