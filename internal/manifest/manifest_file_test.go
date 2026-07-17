package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestOpenManifest_AllowsSymlinkToRegularFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.toml")
	if err := os.WriteFile(target, []byte("requires = \">=0.3.0\""), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", target, err)
	}
	path := filepath.Join(root, filename)
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("os.Symlink(%q) error = %v", path, err)
	}

	file, err := openManifest(path)
	if err != nil {
		t.Fatalf("openManifest() error = %v, want nil", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}

func TestOpenManifest_RejectsNonRegularFiles(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{
			name: "directory",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("os.Mkdir(%q) error = %v", path, err)
				}
			},
		},
		{
			name: "fifo",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Fatalf("syscall.Mkfifo(%q) error = %v", path, err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), filename)
			tt.setup(t, path)

			file, err := openManifest(path)
			if file != nil {
				_ = file.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "not a regular file") {
				t.Fatalf("openManifest() error = %v, want non-regular-file error", err)
			}
		})
	}
}
