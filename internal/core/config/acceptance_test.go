package config_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"

	coreconfig "github.com/mianm12/dotfiles/internal/core/config"
)

func TestAcceptance03_ProfileVariantSkipsButExplicitModuleFails(t *testing.T) {
	root := t.TempDir()
	repository := writeRepository(t, root, `
version = 1

[profiles]
base = ["variant"]
`)
	writeModule(t, repository, "variant", `
[variants.linux]
root = "."

[variants.linux.match]
os = ["linux"]

[[variants.linux.links]]
id = "config"
source = "config"
target = "~/.config/example/config"
`)
	writeFile(t, filepath.Join(repository, "modules", "variant", "config"), "portable bytes")

	machinePath := filepath.Join(root, "machine.toml")
	writeFile(t, machinePath, fmt.Sprintf(`
version = 1
repository = %s
profiles = ["base"]
extra_modules = []
`, strconv.Quote(repository)))
	machine, exists, err := coreconfig.LoadMachine(machinePath)
	if err != nil || !exists {
		t.Fatalf("LoadMachine() = (%#v, %t, %v), want existing config", machine, exists, err)
	}

	loaded, err := coreconfig.OpenRepository(repository)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	before := snapshotTree(t, root)
	resolution, err := loaded.Resolve(machine.Scope(), coreconfig.Platform{OS: "macos", Arch: "aarch64"})
	if err != nil {
		t.Fatalf("Resolve(profile scope) error = %v", err)
	}
	if len(resolution.Modules) != 0 ||
		!reflect.DeepEqual(resolution.NotApplicable, []string{"variant"}) {
		t.Fatalf("profile resolution = %#v, want variant skipped", resolution)
	}

	explicit, err := loaded.Resolve(
		machine.Scope("variant"),
		coreconfig.Platform{OS: "macos", Arch: "aarch64"},
	)
	if !errors.Is(err, coreconfig.ErrNotApplicable) {
		t.Fatalf("Resolve(explicit variant) = (%#v, %v), want ErrNotApplicable", explicit, err)
	}
	if len(machine.ExtraModules) != 0 {
		t.Fatalf("explicit failure changed in-memory extras: %#v", machine.ExtraModules)
	}
	assertTreeUnchanged(t, root, before)
}

func TestAcceptance17_ScopedResolutionIgnoresDamagedOutOfScopeModule(t *testing.T) {
	root := t.TempDir()
	repository := writeRepository(t, root, `
version = 1

[profiles]
base = ["good"]
`)
	writeModule(t, repository, "good", `
[[links]]
id = "config"
source = "config"
target = "~/.config/good/config"
`)
	writeFile(t, filepath.Join(repository, "modules", "good", "config"), "good")
	writeModule(t, repository, "bad", "unknown = true\n")

	loaded, err := coreconfig.OpenRepository(repository)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	before := snapshotTree(t, root)
	resolution, err := loaded.Resolve(
		coreconfig.Scope{Profiles: []string{"base"}},
		coreconfig.Platform{OS: "linux", Distro: "ubuntu", Arch: "x86_64"},
	)
	if err != nil {
		t.Fatalf("Resolve(good scope) error = %v", err)
	}
	if got := moduleIDs(resolution.Modules); !reflect.DeepEqual(got, []string{"good"}) {
		t.Fatalf("resolved modules = %v, want [good]", got)
	}

	damaged, err := loaded.Resolve(
		coreconfig.Scope{
			Profiles:        []string{"base"},
			RequiredModules: []string{"bad"},
		},
		coreconfig.Platform{OS: "linux", Distro: "ubuntu", Arch: "x86_64"},
	)
	if !errors.Is(err, coreconfig.ErrInvalidConfiguration) {
		t.Fatalf("Resolve(bad scope) = (%#v, %v), want ErrInvalidConfiguration", damaged, err)
	}
	assertTreeUnchanged(t, root, before)
}

func TestAcceptance18_InvalidSourceOrExampleFailsReadOnly(t *testing.T) {
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
				if err := os.Symlink("real.example", filepath.Join(moduleRoot, "alias.example")); err != nil {
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
			resolution, err := loaded.Resolve(
				coreconfig.Scope{Profiles: []string{"base"}},
				coreconfig.Platform{OS: "linux", Distro: "ubuntu", Arch: "x86_64"},
			)
			if !errors.Is(err, coreconfig.ErrInvalidConfiguration) {
				t.Fatalf("Resolve() = (%#v, %v), want ErrInvalidConfiguration", resolution, err)
			}
			assertTreeUnchanged(t, root, before)
		})
	}
}

func TestAcceptance19_UnknownPlatformSkipsGatedVariantAndRejectsInvalidOS(t *testing.T) {
	root := t.TempDir()
	repository := writeRepository(t, root, `
version = 1

[profiles]
base = ["portable", "gated"]
`)
	writeModule(t, repository, "portable", `
[[links]]
id = "config"
source = "config"
target = "~/.config/portable/config"
`)
	writeFile(t, filepath.Join(repository, "modules", "portable", "config"), "portable")
	writeModule(t, repository, "gated", `
[variants.ubuntu]
root = "."

[variants.ubuntu.match]
os = ["linux"]
distro = ["ubuntu"]

[[variants.ubuntu.links]]
id = "config"
source = "config"
target = "~/.config/gated/config"
`)
	writeFile(t, filepath.Join(repository, "modules", "gated", "config"), "gated")
	writeModule(t, repository, "invalid-os", `
[match]
os = ["freebsd"]
`)

	loaded, err := coreconfig.OpenRepository(repository)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	before := snapshotTree(t, root)
	for _, platform := range []coreconfig.Platform{
		{OS: "linux", Distro: "gentoo", Arch: "riscv64"},
		{OS: "unknown", Distro: "unknown", Arch: "unknown"},
	} {
		resolution, resolveErr := loaded.Resolve(
			coreconfig.Scope{Profiles: []string{"base"}},
			platform,
		)
		if resolveErr != nil {
			t.Fatalf("Resolve(%#v) error = %v", platform, resolveErr)
		}
		if got := moduleIDs(resolution.Modules); !reflect.DeepEqual(got, []string{"portable"}) {
			t.Fatalf("Resolve(%#v) modules = %v, want [portable]", platform, got)
		}
		if !reflect.DeepEqual(resolution.NotApplicable, []string{"gated"}) {
			t.Fatalf(
				"Resolve(%#v) not-applicable = %v, want [gated]",
				platform,
				resolution.NotApplicable,
			)
		}
	}

	invalid, err := loaded.Resolve(
		coreconfig.Scope{RequiredModules: []string{"invalid-os"}},
		coreconfig.Platform{OS: "linux", Distro: "ubuntu", Arch: "x86_64"},
	)
	if !errors.Is(err, coreconfig.ErrInvalidConfiguration) {
		t.Fatalf("Resolve(invalid os) = (%#v, %v), want ErrInvalidConfiguration", invalid, err)
	}
	assertTreeUnchanged(t, root, before)
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

func moduleIDs(modules []coreconfig.Module) []string {
	ids := make([]string, len(modules))
	for index := range modules {
		ids[index] = modules[index].ID
	}
	slices.Sort(ids)
	return ids
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
		t.Fatalf("config resolution mutated fixture\nbefore=%v\nafter=%v", before, after)
	}
}
