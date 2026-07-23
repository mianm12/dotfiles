package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

type cliTreeEntry struct {
	mode fs.FileMode
	data string
	link string
}

func snapshotCLITree(t *testing.T, root string) map[string]cliTreeEntry {
	t.Helper()
	snapshot := make(map[string]cliTreeEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		data := ""
		link := ""
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			data = string(content)
		} else if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot[relative] = cliTreeEntry{mode: info.Mode(), data: data, link: link}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot tree %q: %v", root, err)
	}
	return snapshot
}

func makeDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", path, err)
	}
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	makeDirectory(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
