package manifest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoad_DiscoversModulesInStableOrder(t *testing.T) {
	repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = []
`)
	writeModule(t, repo, "zsh", `os = ["darwin"]`)
	writeModule(t, repo, "git", "")

	got, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if got.Requirement().String() != ">=0.3.0" {
		t.Errorf("Load().Requirement() = %q, want %q", got.Requirement(), ">=0.3.0")
	}
	wantModules := []string{"git", "zsh"}
	if !reflect.DeepEqual(got.ModuleNames(), wantModules) {
		t.Errorf("Load().ModuleNames() = %v, want %v", got.ModuleNames(), wantModules)
	}
	modules := got.ModuleNames()
	modules[0] = "changed"
	if !reflect.DeepEqual(got.ModuleNames(), wantModules) {
		t.Errorf("mutating ModuleNames result changed repository: got %v, want %v", got.ModuleNames(), wantModules)
	}
}

func TestRepositoryDataKeys_ReturnsStableCopy(t *testing.T) {
	repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = []
[data.machine]
[data.email]
`)
	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	want := []string{"email", "machine"}
	first := loaded.DataKeys()
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("DataKeys() = %v, want %v", first, want)
	}
	first[0] = "changed"
	if got := loaded.DataKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("mutating DataKeys result changed repository: got %v, want %v", got, want)
	}
}

func TestRepositoryProfileLineWithModule_PreservesDirectDeclaration(t *testing.T) {
	repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
shared = ["core"]
all = ["alpha", "@shared"]
`)
	for _, module := range []string{"alpha", "beta", "core"} {
		writeModule(t, repo, module, "")
	}
	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	line, err := loaded.ProfileLineWithModule("all", "beta")
	if err != nil {
		t.Fatalf("ProfileLineWithModule() error = %v, want nil", err)
	}
	if want := `all = ["alpha", "@shared", "beta"]`; line != want {
		t.Fatalf("ProfileLineWithModule() = %q, want %q", line, want)
	}
	if again, err := loaded.ProfileLineWithModule("all", "beta"); err != nil || again != line {
		t.Fatalf("repeated ProfileLineWithModule() = %q, %v; want %q", again, err, line)
	}

	if _, err := loaded.ProfileLineWithModule("missing", "beta"); err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Fatalf("unknown profile error = %v", err)
	}
	if _, err := loaded.ProfileLineWithModule("all", "../beta"); err == nil || !strings.Contains(err.Error(), "invalid module") {
		t.Fatalf("invalid module error = %v", err)
	}
}

func TestRepositoryValidateModuleRules_CoversUnassignedModules(t *testing.T) {
	repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = []")
	writeModule(t, repo, "unassigned", `target = { darwin = "~/.config/app" }`)
	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if err := loaded.ValidateModuleRules("darwin"); err != nil {
		t.Fatalf("ValidateModuleRules(darwin) error = %v, want nil", err)
	}
	if err := loaded.ValidateModuleRules("linux"); err == nil || !strings.Contains(err.Error(), "no linux entry") {
		t.Fatalf("ValidateModuleRules(linux) error = %v, want missing current GOOS target", err)
	}
	if err := loaded.ValidateModuleRules("freebsd"); err == nil || !strings.Contains(err.Error(), "unsupported GOOS") {
		t.Fatalf("ValidateModuleRules(freebsd) error = %v, want unsupported GOOS", err)
	}
}

func TestLoad_MissingModulesDirectoryMeansNoModules(t *testing.T) {
	repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = []")

	got, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if len(got.ModuleNames()) != 0 {
		t.Errorf("Load().ModuleNames() = %v, want empty", got.ModuleNames())
	}
}

func TestLoad_RejectsInvalidModulesDirectoryPath(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{
			name: "regular file",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
					t.Fatalf("os.WriteFile(%q) error = %v", path, err)
				}
			},
		},
		{
			name: "dangling symlink",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Symlink(filepath.Join(filepath.Dir(path), "missing-modules"), path); err != nil {
					t.Fatalf("os.Symlink(%q) error = %v", path, err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = []")
			modulesRoot := filepath.Join(repo, "modules")
			tt.setup(t, modulesRoot)

			_, err := Load(repo)
			if err == nil || !strings.Contains(err.Error(), modulesRoot) {
				t.Fatalf("Load() error = %v, want hard error containing %q", err, modulesRoot)
			}
		})
	}
}

func TestLoad_RejectsUnavailableOrInvalidRepository(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := Load(missing); !errors.Is(err, ErrRepositoryUnavailable) {
		t.Fatalf("Load(%q) error = %v, want ErrRepositoryUnavailable", missing, err)
	}

	file := filepath.Join(t.TempDir(), "repo")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", file, err)
	}
	if _, err := Load(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Load(%q) error = %v, want not a directory", file, err)
	}
}

func TestLoad_RejectsInvalidModuleName(t *testing.T) {
	repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = []")
	writeModule(t, repo, "_invalid", "")

	_, err := Load(repo)
	if err == nil || !strings.Contains(err.Error(), "invalid module name") {
		t.Fatalf("Load() error = %v, want invalid module name", err)
	}
}

func TestLoad_RejectsInvalidModuleManifest(t *testing.T) {
	repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = []")
	writeModule(t, repo, "zsh", "unknown = true")

	_, err := Load(repo)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(repo, "modules", "zsh", filename)) {
		t.Fatalf("Load() error = %v, want module manifest path", err)
	}
}

func TestLoad_RejectsDanglingModuleManifestSymlink(t *testing.T) {
	repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = []")
	moduleRoot := filepath.Join(repo, "modules", "zsh")
	if err := os.MkdirAll(moduleRoot, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", moduleRoot, err)
	}
	manifestPath := filepath.Join(moduleRoot, filename)
	if err := os.Symlink(filepath.Join(moduleRoot, "missing.toml"), manifestPath); err != nil {
		t.Fatalf("os.Symlink(%q) error = %v", manifestPath, err)
	}

	_, err := Load(repo)
	if err == nil || !strings.Contains(err.Error(), manifestPath) {
		t.Fatalf("Load() error = %v, want dangling manifest error containing %q", err, manifestPath)
	}
}

func TestLoad_DoesNotWriteOnSuccessOrFailure(t *testing.T) {
	tests := []struct {
		name      string
		root      string
		module    string
		wantError bool
	}{
		{name: "success", root: "requires = \">=0.3.0\"\n[profiles]\nbase = []", module: `os = ["darwin"]`},
		{name: "failure", root: "requires = \">=0.3.0\"\n[profiles]\nbase = []", module: "unknown = true", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := writeRepositoryManifest(t, tt.root)
			writeModule(t, repo, "zsh", tt.module)
			before := snapshotTree(t, repo)

			_, err := Load(repo)
			if (err != nil) != tt.wantError {
				t.Fatalf("Load() error = %v, wantError %v", err, tt.wantError)
			}
			after := snapshotTree(t, repo)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("Load() changed repository\nbefore: %v\nafter:  %v", before, after)
			}
		})
	}
}

func TestLoad_ReadOnlyRepository(t *testing.T) {
	repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = []")
	writeModule(t, repo, "zsh", "")
	setTreeWritable(t, repo, false)
	t.Cleanup(func() { setTreeWritable(t, repo, true) })

	if _, err := Load(repo); err != nil {
		t.Fatalf("Load() error = %v, want nil for read-only repository", err)
	}
}

type treeEntry struct {
	mode fs.FileMode
	data string
}

func snapshotTree(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	snapshot := make(map[string]treeEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		data := ""
		switch {
		case info.Mode().IsRegular():
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			data = string(content)
		case info.Mode()&fs.ModeSymlink != 0:
			data, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot[relative] = treeEntry{mode: info.Mode(), data: data}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %q: %v", root, err)
	}
	return snapshot
}

func setTreeWritable(t *testing.T, root string, writable bool) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		mode := fs.FileMode(0o444)
		if entry.IsDir() {
			mode = 0o555
		}
		if writable {
			mode |= 0o200
			if entry.IsDir() {
				mode |= 0o100
			}
		}
		return os.Chmod(path, mode)
	})
	if err != nil {
		t.Fatalf("set writable=%v for %q: %v", writable, root, err)
	}
}

func writeRepositoryManifest(t *testing.T, content string) string {
	t.Helper()
	repo := t.TempDir()
	path := filepath.Join(repo, filename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return repo
}

func writeModule(t *testing.T, repo, name, content string) {
	t.Helper()
	root := filepath.Join(repo, "modules", name)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", root, err)
	}
	if content == "" {
		return
	}
	path := filepath.Join(root, filename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
