package paths

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestIsMissing(t *testing.T) {
	t.Run("missing path", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing")
		if !IsMissing(path, fs.ErrNotExist) {
			t.Errorf("IsMissing(%q) = false, want true", path)
		}
	})

	t.Run("unrelated error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing")
		if IsMissing(path, fs.ErrPermission) {
			t.Errorf("IsMissing(%q) = true, want false", path)
		}
	})

	t.Run("path appeared after operation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "existing")
		if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", path, err)
		}
		if IsMissing(path, fs.ErrNotExist) {
			t.Errorf("IsMissing(%q) = true, want false", path)
		}
	})

	t.Run("dangling requested symlink", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "link")
		if err := os.Symlink(filepath.Join(root, "missing"), path); err != nil {
			t.Fatalf("os.Symlink(%q) error = %v", path, err)
		}
		if IsMissing(path, fs.ErrNotExist) {
			t.Errorf("IsMissing(%q) = true, want false", path)
		}
	})

	t.Run("dangling ancestor symlink", func(t *testing.T) {
		root := t.TempDir()
		ancestor := filepath.Join(root, "link")
		if err := os.Symlink(filepath.Join(root, "missing"), ancestor); err != nil {
			t.Fatalf("os.Symlink(%q) error = %v", ancestor, err)
		}
		path := filepath.Join(ancestor, "child")
		if IsMissing(path, fs.ErrNotExist) {
			t.Errorf("IsMissing(%q) = true, want false", path)
		}
	})

	t.Run("valid symlink ancestor with missing child", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", target, err)
		}
		ancestor := filepath.Join(root, "link")
		if err := os.Symlink(target, ancestor); err != nil {
			t.Fatalf("os.Symlink(%q) error = %v", ancestor, err)
		}
		path := filepath.Join(ancestor, "missing")
		if !IsMissing(path, fs.ErrNotExist) {
			t.Errorf("IsMissing(%q) = false, want true", path)
		}
	})
}
