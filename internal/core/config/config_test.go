package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	coreconfig "github.com/mianm12/dotfiles/internal/core/config"
)

func TestLoadMachine_StrictValidation(t *testing.T) {
	root := t.TempDir()
	validPath := filepath.Join(root, "valid.toml")
	writeFile(t, validPath, `
version = 1
repository = "/absolute/repository"
profiles = ["base"]
extra_modules = ["tmux"]
`)
	machine, exists, err := coreconfig.LoadMachine(validPath)
	if err != nil || !exists {
		t.Fatalf("LoadMachine(valid) = (%#v, %t, %v)", machine, exists, err)
	}
	if machine.Repository != "/absolute/repository" ||
		!reflect.DeepEqual(machine.Profiles, []string{"base"}) ||
		!reflect.DeepEqual(machine.ExtraModules, []string{"tmux"}) {
		t.Fatalf("machine = %#v", machine)
	}

	missing, exists, err := coreconfig.LoadMachine(filepath.Join(root, "missing.toml"))
	if err != nil || exists || !reflect.DeepEqual(missing, coreconfig.Machine{}) {
		t.Fatalf("LoadMachine(missing) = (%#v, %t, %v)", missing, exists, err)
	}

	tests := []struct {
		name    string
		content string
	}{
		{name: "unknown field", content: "version = 1\nrepository = \"/repo\"\nunknown = true\n"},
		{name: "wrong version", content: "version = 2\nrepository = \"/repo\"\n"},
		{name: "relative repository", content: "version = 1\nrepository = \"repo\"\n"},
		{name: "invalid extra", content: "version = 1\nrepository = \"/repo\"\nextra_modules = [\"Bad\"]\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.name+".toml")
			writeFile(t, path, test.content)
			if _, _, err := coreconfig.LoadMachine(path); !errors.Is(
				err,
				coreconfig.ErrInvalidConfiguration,
			) {
				t.Fatalf("LoadMachine() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

func TestOpenRepository_RecognizesOnlyManifestDirectories(t *testing.T) {
	root := t.TempDir()
	repository := writeRepository(t, root, "version = 1\n[profiles]\n")
	writeModule(t, repository, "valid", "")
	if err := os.MkdirAll(filepath.Join(repository, "modules", "no-manifest"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(no-manifest) error = %v", err)
	}
	writeModule(t, repository, "Bad", "")
	writeFile(t, filepath.Join(repository, "modules", "plain-file"), "not a directory")
	external := filepath.Join(root, "external")
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(external) error = %v", err)
	}
	if err := os.Symlink(external, filepath.Join(repository, "modules", "linked")); err != nil {
		t.Fatalf("os.Symlink(linked) error = %v", err)
	}

	loaded, err := coreconfig.OpenRepository(repository)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	if got, want := loaded.ModuleIDs(), []string{"valid"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ModuleIDs() = %v, want %v", got, want)
	}
}

func TestOpenRepository_StrictRootManifest(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{name: "missing version", manifest: "[profiles]\n"},
		{name: "wrong version", manifest: "version = 2\n[profiles]\n"},
		{name: "missing profiles", manifest: "version = 1\n"},
		{name: "unknown field", manifest: "version = 1\nunknown = true\n[profiles]\n"},
		{name: "invalid profile", manifest: "version = 1\n[profiles]\nBad = []\n"},
		{name: "duplicate member", manifest: "version = 1\n[profiles]\nbase = [\"app\", \"app\"]\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := writeRepository(t, t.TempDir(), test.manifest)
			if _, err := coreconfig.OpenRepository(repository); !errors.Is(
				err,
				coreconfig.ErrInvalidConfiguration,
			) {
				t.Fatalf("OpenRepository() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

func TestResolve_ValidatesOnlySelectedProfileReferences(t *testing.T) {
	root := t.TempDir()
	repository := writeRepository(t, root, `
version = 1
[profiles]
active = ["good"]
inactive = ["deleted"]
`)
	writeModule(t, repository, "good", "")
	loaded, err := coreconfig.OpenRepository(repository)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	if _, err := loaded.Resolve(
		coreconfig.Scope{Profiles: []string{"active"}},
		coreconfig.Platform{OS: "linux"},
	); err != nil {
		t.Fatalf("Resolve(active profile) error = %v", err)
	}
	if _, err := loaded.Resolve(
		coreconfig.Scope{Profiles: []string{"inactive"}},
		coreconfig.Platform{OS: "linux"},
	); !errors.Is(err, coreconfig.ErrInvalidConfiguration) {
		t.Fatalf("Resolve(inactive profile when selected) error = %v", err)
	}
}

func TestResolve_TreatsMachineSelectionsAsSets(t *testing.T) {
	root := t.TempDir()
	repository := writeRepository(t, root, `
version = 1
[profiles]
base = ["app"]
`)
	writeModule(t, repository, "app", "")
	loaded, err := coreconfig.OpenRepository(repository)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	resolution, err := loaded.Resolve(
		coreconfig.Scope{
			Profiles:        []string{"base", "base"},
			ExtraModules:    []string{"app", "app"},
			RequiredModules: []string{"app", "app"},
		},
		coreconfig.Platform{OS: "linux"},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := moduleIDs(resolution.Modules); !reflect.DeepEqual(got, []string{"app"}) {
		t.Fatalf("resolved modules = %v, want [app]", got)
	}
}

func TestResolve_ValidatesModuleStructure(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{
			name: "portable and variants mixed",
			manifest: `
[match]
os = ["linux"]
[variants.linux]
root = "."
`,
		},
		{
			name: "duplicate placement across kinds",
			manifest: `
[[links]]
id = "same"
source = "config"
target = "~/.config/app/config"
[[locals]]
id = "same"
example = "config.example"
target = "~/.config/app/config.local"
`,
		},
		{
			name: "variant root escapes",
			manifest: `
[variants.linux]
root = "../outside"
`,
		},
		{
			name: "distro without exact linux constraint",
			manifest: `
[match]
os = ["macos", "linux"]
distro = ["ubuntu"]
`,
		},
		{
			name: "uppercase arch token",
			manifest: `
[match]
arch = ["ARM64"]
`,
		},
		{
			name: "multiple matching variants",
			manifest: `
[variants.first]
root = "."
[variants.second]
root = "."
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			repository := writeRepository(t, root, "version = 1\n[profiles]\n")
			writeModule(t, repository, "app", test.manifest)
			loaded, err := coreconfig.OpenRepository(repository)
			if err != nil {
				t.Fatalf("OpenRepository() error = %v", err)
			}
			if _, err := loaded.Resolve(
				coreconfig.Scope{RequiredModules: []string{"app"}},
				coreconfig.Platform{OS: "linux", Distro: "ubuntu", Arch: "aarch64"},
			); !errors.Is(err, coreconfig.ErrInvalidConfiguration) {
				t.Fatalf("Resolve() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

func TestResolve_DistroAllowsDuplicateLinuxMatchTokens(t *testing.T) {
	root := t.TempDir()
	repository := writeRepository(t, root, "version = 1\n[profiles]\nbase = [\"app\"]\n")
	writeModule(t, repository, "app", `
[match]
os = ["linux", "linux"]
distro = ["ubuntu"]
`)

	loaded, err := coreconfig.OpenRepository(repository)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	resolution, err := loaded.Resolve(
		coreconfig.Scope{Profiles: []string{"base"}},
		coreconfig.Platform{OS: "linux", Distro: "ubuntu"},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := moduleIDs(resolution.Modules); !reflect.DeepEqual(got, []string{"app"}) {
		t.Fatalf("resolved modules = %v, want [app]", got)
	}
}

func TestResolve_MaterializesFileAndDirectoryLinksAndLocal(t *testing.T) {
	root := t.TempDir()
	repository := writeRepository(t, root, `
version = 1
[profiles]
base = ["app"]
`)
	moduleRoot := writeModule(t, repository, "app", `
[[links]]
id = "file"
source = "config"
target = "~/.config/app/config"
[[links]]
id = "directory"
source = "tree"
target = "~/.config/app/tree"
[[links]]
id = "root"
source = "."
target = "~/.config/app/root"
[[locals]]
id = "local"
example = "config.local.example"
target = "~/.config/app/config.local"
`)
	writeFile(t, filepath.Join(moduleRoot, "config"), "config")
	if err := os.Mkdir(filepath.Join(moduleRoot, "tree"), 0o700); err != nil {
		t.Fatalf("os.Mkdir(tree) error = %v", err)
	}
	writeFile(t, filepath.Join(moduleRoot, "tree", "nested"), "nested")
	writeFile(t, filepath.Join(moduleRoot, "config.local.example"), "local")

	loaded, err := coreconfig.OpenRepository(repository)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	resolution, err := loaded.Resolve(
		coreconfig.Scope{Profiles: []string{"base"}},
		coreconfig.Platform{OS: "macos", Arch: "aarch64"},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolution.Modules) != 1 ||
		len(resolution.Modules[0].Links) != 3 ||
		len(resolution.Modules[0].Locals) != 1 {
		t.Fatalf("resolution = %#v", resolution)
	}
	if !resolution.Modules[0].Links[1].SourceMode.IsDir() {
		t.Fatalf("directory link mode = %v, want directory", resolution.Modules[0].Links[1].SourceMode)
	}
	if rootLink := resolution.Modules[0].Links[2]; rootLink.SourcePath != moduleRoot ||
		!rootLink.SourceMode.IsDir() {
		t.Fatalf("root directory link = %#v, want source path %q", rootLink, moduleRoot)
	}
}

func TestResolve_RejectsVariantRootSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	repository := writeRepository(t, root, `
version = 1
[profiles]
base = ["app"]
`)
	moduleRoot := writeModule(t, repository, "app", `
[variants.linux]
root = "external"
[variants.linux.match]
os = ["linux"]
`)
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatalf("os.Mkdir(outside) error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(moduleRoot, "external")); err != nil {
		t.Fatalf("os.Symlink(external root) error = %v", err)
	}

	loaded, err := coreconfig.OpenRepository(repository)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	if _, err := loaded.Resolve(
		coreconfig.Scope{Profiles: []string{"base"}},
		coreconfig.Platform{OS: "linux"},
	); !errors.Is(err, coreconfig.ErrInvalidConfiguration) {
		t.Fatalf("Resolve() error = %v, want ErrInvalidConfiguration", err)
	}
}
