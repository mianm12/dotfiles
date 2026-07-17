package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestEnumerateModuleSources_AppliesIgnoreInStableOrder(t *testing.T) {
	repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = ["app"]
`)
	writeModule(t, repo, "app", `
[ignore]
patterns = ["README.md", "cache/"]
[hooks]
run_once = ["scripts/setup.sh"]
`)
	moduleRoot := filepath.Join(repo, "modules", "app")
	writeSourceFile(t, moduleRoot, "z", "z")
	writeSourceFile(t, moduleRoot, "a", "a")
	writeSourceFile(t, moduleRoot, "README.md", "ignored")
	writeSourceFile(t, moduleRoot, "docs/cache/data", "ignored descendant")
	writeSourceFile(t, moduleRoot, ".git/config", "built in")
	writeSourceFile(t, moduleRoot, "hooks/data", "built in")
	writeSourceFile(t, moduleRoot, "scripts/setup.sh", "hook")
	writeSourceFile(t, moduleRoot, "nested/file.swp", "built in")

	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	resolved, err := loaded.Resolve("base", "darwin")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}

	first, err := enumerateModuleSources(resolved.modules[0])
	if err != nil {
		t.Fatalf("enumerateModuleSources() error = %v, want nil", err)
	}
	second, err := enumerateModuleSources(resolved.modules[0])
	if err != nil {
		t.Fatalf("enumerateModuleSources() second error = %v, want nil", err)
	}
	want := []moduleSource{
		{path: "README.md", ignored: true},
		{path: "a"},
		{path: "docs/cache/data", ignored: true},
		{path: "z"},
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("enumerateModuleSources() = %#v, want %#v", first, want)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("repeated enumerateModuleSources() = %#v, want %#v", second, first)
	}
}

func TestEnumerateModuleSources_RejectsSymlinksEvenWhenIgnored(t *testing.T) {
	repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = ["app"]
`)
	writeModule(t, repo, "app", "[ignore]\npatterns = [\"ignored/\"]")
	moduleRoot := filepath.Join(repo, "modules", "app")
	ignoredDir := filepath.Join(moduleRoot, "ignored")
	if err := os.MkdirAll(ignoredDir, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", ignoredDir, err)
	}
	if err := os.Symlink(filepath.Join(moduleRoot, "missing"), filepath.Join(ignoredDir, "link")); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}

	module := resolveOnlyModule(t, repo)
	_, err := enumerateModuleSources(module)
	if err == nil || !strings.Contains(err.Error(), `source "ignored/link" is a symlink`) {
		t.Fatalf("enumerateModuleSources() error = %v, want ignored symlink error", err)
	}
}

func TestEnumerateModuleSources_RejectsSpecialFiles(t *testing.T) {
	repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = ["app"]
`)
	writeModule(t, repo, "app", "")
	fifo := filepath.Join(repo, "modules", "app", "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("syscall.Mkfifo(%q) error = %v", fifo, err)
	}

	module := resolveOnlyModule(t, repo)
	_, err := enumerateModuleSources(module)
	if err == nil || !strings.Contains(err.Error(), `source "pipe" is a special file`) {
		t.Fatalf("enumerateModuleSources() error = %v, want special-file error", err)
	}
}

func TestEnumerateModuleSources_ValidatesReferences(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		setup    func(t *testing.T, moduleRoot string)
		want     string
	}{
		{
			name:     "missing file rule",
			manifest: "[files.missing]",
			want:     `files references missing source "missing"`,
		},
		{
			name:     "file rule directory",
			manifest: "[files.data]",
			setup: func(t *testing.T, moduleRoot string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(moduleRoot, "data"), 0o700); err != nil {
					t.Fatalf("os.Mkdir() error = %v", err)
				}
			},
			want: `files references directory source "data"`,
		},
		{
			name:     "hook directory",
			manifest: "[hooks]\nrun_once = [\"scripts\"]",
			setup: func(t *testing.T, moduleRoot string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(moduleRoot, "scripts"), 0o700); err != nil {
					t.Fatalf("os.Mkdir() error = %v", err)
				}
			},
			want: `hook references directory source "scripts"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = [\"app\"]")
			writeModule(t, repo, "app", tt.manifest)
			moduleRoot := filepath.Join(repo, "modules", "app")
			if tt.setup != nil {
				tt.setup(t, moduleRoot)
			}

			module := resolveOnlyModule(t, repo)
			_, err := enumerateModuleSources(module)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("enumerateModuleSources() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestEnumerateModuleSources_RejectsHardLinkedHookScripts(t *testing.T) {
	repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = [\"app\"]")
	writeModule(t, repo, "app", "[hooks]\nrun_once = [\"hooks/first\", \"scripts/alias\"]")
	moduleRoot := filepath.Join(repo, "modules", "app")
	writeSourceFile(t, moduleRoot, "hooks/first", "script")
	alias := filepath.Join(moduleRoot, "scripts", "alias")
	if err := os.MkdirAll(filepath.Dir(alias), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(alias), err)
	}
	if err := os.Link(filepath.Join(moduleRoot, "hooks", "first"), alias); err != nil {
		t.Fatalf("os.Link(%q) error = %v", alias, err)
	}

	module := resolveOnlyModule(t, repo)
	_, err := enumerateModuleSources(module)
	if err == nil || !strings.Contains(err.Error(), `hook source "scripts/alias" duplicates filesystem identity of "hooks/first"`) {
		t.Fatalf("enumerateModuleSources() error = %v, want duplicate hook identity error", err)
	}
}

func TestEnumerateModuleSources_DoesNotWrite(t *testing.T) {
	repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = [\"app\"]")
	writeModule(t, repo, "app", "")
	writeSourceFile(t, filepath.Join(repo, "modules", "app"), "config", "value")
	module := resolveOnlyModule(t, repo)
	before := snapshotTree(t, repo)

	if _, err := enumerateModuleSources(module); err != nil {
		t.Fatalf("enumerateModuleSources() error = %v, want nil", err)
	}
	after := snapshotTree(t, repo)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("enumerateModuleSources() changed repository\nbefore: %v\nafter:  %v", before, after)
	}
}

func resolveOnlyModule(t *testing.T, repo string) ResolvedModule {
	t.Helper()
	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	resolved, err := loaded.Resolve("base", "darwin")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if len(resolved.modules) != 1 {
		t.Fatalf("Resolve() modules = %v, want one module", resolved.modules)
	}
	return resolved.modules[0]
}

func writeSourceFile(t *testing.T, moduleRoot, relative, content string) {
	t.Helper()
	path := filepath.Join(moduleRoot, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
