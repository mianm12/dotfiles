package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	coreconfig "github.com/mianm12/dotfiles/internal/core/config"
)

func TestInspectModuleInvalidSourceOrExampleFailsReadOnly(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		setup    func(*testing.T, string)
	}{
		{
			name: "missing link source",
			manifest: `
[[links]]
id = "config"
source = "missing"
target = "~/.config/example/config"
`,
		},
		{
			name: "link source is symlink",
			manifest: `
[[links]]
id = "config"
source = "alias"
target = "~/.config/example/config"
`,
			setup: func(t *testing.T, moduleRoot string) {
				writeFile(t, filepath.Join(moduleRoot, "real"), "content")
				if err := os.Symlink("real", filepath.Join(moduleRoot, "alias")); err != nil {
					t.Fatalf("os.Symlink(alias) error = %v", err)
				}
			},
		},
		{
			name: "link source ancestor escapes selected root",
			manifest: `
[[links]]
id = "config"
source = "alias/config"
target = "~/.config/example/config"
`,
			setup: func(t *testing.T, moduleRoot string) {
				outside := filepath.Join(moduleRoot, "..", "..", "outside")
				writeFile(t, filepath.Join(outside, "config"), "content")
				if err := os.Symlink(outside, filepath.Join(moduleRoot, "alias")); err != nil {
					t.Fatalf("os.Symlink(alias ancestor) error = %v", err)
				}
			},
		},
		{
			name: "missing local example",
			manifest: `
[[locals]]
id = "local"
example = "missing.example"
target = "~/.config/example/config.local"
`,
		},
		{
			name: "local example is directory",
			manifest: `
[[locals]]
id = "local"
example = "example"
target = "~/.config/example/config.local"
`,
			setup: func(t *testing.T, moduleRoot string) {
				if err := os.Mkdir(filepath.Join(moduleRoot, "example"), 0o700); err != nil {
					t.Fatalf("os.Mkdir(example) error = %v", err)
				}
			},
		},
		{
			name: "local example is symlink",
			manifest: `
[[locals]]
id = "local"
example = "alias.example"
target = "~/.config/example/config.local"
`,
			setup: func(t *testing.T, moduleRoot string) {
				writeFile(t, filepath.Join(moduleRoot, "real.example"), "content")
				if err := os.Symlink(
					"real.example",
					filepath.Join(moduleRoot, "alias.example"),
				); err != nil {
					t.Fatalf("os.Symlink(alias example) error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			repository := writeRepository(t, root, `
version = 1

[profiles]
base = ["app"]
`)
			moduleRoot := writeModule(t, repository, "app", test.manifest)
			if test.setup != nil {
				test.setup(t, moduleRoot)
			}
			loaded, err := coreconfig.OpenRepository(repository)
			if err != nil {
				t.Fatalf("OpenRepository() error = %v", err)
			}
			before := snapshotTree(t, root)

			_, exists, _, err := loaded.InspectModule(
				"app",
				testPlatform("linux", "ubuntu", "x86_64"),
			)
			if !exists || !errors.Is(err, coreconfig.ErrInvalidConfiguration) {
				t.Fatalf(
					"InspectModule() = (exists=%t, err=%v), want selected invalid configuration",
					exists,
					err,
				)
			}
			assertTreeUnchanged(t, root, before)
		})
	}
}

func writeRepository(t *testing.T, root, manifest string) string {
	t.Helper()
	repository := filepath.Join(root, "repository")
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(repository) error = %v", err)
	}
	writeFile(t, filepath.Join(repository, "dot.toml"), manifest)
	return repository
}

func writeModule(t *testing.T, repository, id, manifest string) string {
	t.Helper()
	root := filepath.Join(repository, "modules", id)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(module %q) error = %v", id, err)
	}
	writeFile(t, filepath.Join(root, "module.toml"), manifest)
	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}

type treeEntry struct {
	mode fs.FileMode
	link string
	data string
}

func snapshotTree(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	snapshot := make(map[string]treeEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		record := treeEntry{mode: info.Mode()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			record.link, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var content []byte
			content, err = os.ReadFile(path)
			record.data = string(content)
		}
		if err != nil {
			return err
		}
		snapshot[relative] = record
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot tree %q: %v", root, err)
	}
	return snapshot
}

func assertTreeUnchanged(t *testing.T, root string, before map[string]treeEntry) {
	t.Helper()
	after := snapshotTree(t, root)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("config inspection mutated fixture\nbefore=%v\nafter=%v", before, after)
	}
}
