package config_test

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	coreconfig "github.com/mianm12/dotfiles/internal/core/config"
)

func TestLoadMachineRequiresDirectRegularFile(t *testing.T) {
	const validMachine = `
version = 1
repository = "/absolute/repository"
profiles = []
extra_modules = []
`
	tests := []struct {
		name string
		path func(*testing.T, string) string
	}{
		{
			name: "symlink to regular file",
			path: func(t *testing.T, root string) string {
				target := filepath.Join(root, "actual.toml")
				writeFile(t, target, validMachine)
				path := filepath.Join(root, "machine.toml")
				if err := os.Symlink(target, path); err != nil {
					t.Fatalf("os.Symlink(machine) error = %v", err)
				}
				return path
			},
		},
		{
			name: "directory",
			path: func(t *testing.T, root string) string {
				path := filepath.Join(root, "machine.toml")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("os.Mkdir(machine) error = %v", err)
				}
				return path
			},
		},
		{
			name: "socket",
			path: func(t *testing.T, _ string) string {
				return unixSocket(t, temporarySocketPath(t))
			},
		},
		{
			name: "dangling symlink",
			path: func(t *testing.T, root string) string {
				path := filepath.Join(root, "machine.toml")
				if err := os.Symlink("missing.toml", path); err != nil {
					t.Fatalf("os.Symlink(dangling machine) error = %v", err)
				}
				return path
			},
		},
		{
			name: "device",
			path: func(*testing.T, string) string {
				return os.DevNull
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := test.path(t, root)
			before := snapshotTree(t, root)

			machine, exists, err := coreconfig.LoadMachine(path)

			if !errors.Is(err, coreconfig.ErrInvalidConfiguration) ||
				exists ||
				!reflect.DeepEqual(machine, coreconfig.Machine{}) {
				t.Fatalf(
					"LoadMachine() = (%#v, %t, %v), want direct-regular error",
					machine,
					exists,
					err,
				)
			}
			assertTreeUnchanged(t, root, before)
		})
	}
}

func TestManifestSymlinkToRegularFileIsAccepted(t *testing.T) {
	t.Run("root manifest", func(t *testing.T) {
		repository := filepath.Join(t.TempDir(), "repository")
		if err := os.MkdirAll(repository, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(repository) error = %v", err)
		}
		writeFile(t, filepath.Join(repository, "actual.toml"), "version = 1\n[profiles]\n")
		if err := os.Symlink("actual.toml", filepath.Join(repository, "dot.toml")); err != nil {
			t.Fatalf("os.Symlink(dot.toml) error = %v", err)
		}

		if _, err := coreconfig.OpenRepository(repository); err != nil {
			t.Fatalf("OpenRepository(symlink manifest) error = %v", err)
		}
	})

	t.Run("module manifest", func(t *testing.T) {
		repository := writeRepository(t, t.TempDir(), `
version = 1
[profiles]
base = ["app"]
`)
		moduleRoot := filepath.Join(repository, "modules", "app")
		writeFile(t, filepath.Join(moduleRoot, "actual.toml"), "")
		if err := os.Symlink("actual.toml", filepath.Join(moduleRoot, "module.toml")); err != nil {
			t.Fatalf("os.Symlink(module.toml) error = %v", err)
		}
		loaded, err := coreconfig.OpenRepository(repository)
		if err != nil {
			t.Fatalf("OpenRepository() error = %v", err)
		}

		module, exists, applicability, err := loaded.InspectModule(
			"app",
			testPlatform("linux", "", ""),
		)
		if err != nil {
			t.Fatalf("InspectModule(symlink manifest) error = %v", err)
		}
		if !exists || module.ID != "app" ||
			applicability.State != coreconfig.ApplicabilityApplicable {
			t.Fatalf(
				"InspectModule(symlink manifest) = (%#v, %t, %#v), want applicable app",
				module,
				exists,
				applicability,
			)
		}
	})
}

func TestOpenRepositoryRejectsUnsafeRootManifestEntries(t *testing.T) {
	for _, test := range unsafeManifestEntries() {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			repository := filepath.Join(root, "repository")
			if err := os.MkdirAll(repository, 0o700); err != nil {
				t.Fatalf("os.MkdirAll(repository) error = %v", err)
			}
			test.create(t, filepath.Join(repository, "dot.toml"))
			before := snapshotTree(t, root)

			_, err := coreconfig.OpenRepository(repository)

			if !errors.Is(err, coreconfig.ErrInvalidConfiguration) {
				t.Fatalf("OpenRepository() error = %v, want invalid configuration", err)
			}
			assertTreeUnchanged(t, root, before)
		})
	}
}

func TestInspectModuleDefersUnsafeInactiveManifestEntries(t *testing.T) {
	for _, test := range unsafeManifestEntries() {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			repository := writeRepository(t, root, `
version = 1
[profiles]
base = ["good"]
`)
			writeModule(t, repository, "good", "")
			badRoot := filepath.Join(repository, "modules", "bad")
			if err := os.MkdirAll(badRoot, 0o700); err != nil {
				t.Fatalf("os.MkdirAll(bad module) error = %v", err)
			}
			test.create(t, filepath.Join(badRoot, "module.toml"))
			loaded, err := coreconfig.OpenRepository(repository)
			if err != nil {
				t.Fatalf("OpenRepository() error = %v", err)
			}
			before := snapshotTree(t, root)

			profileModules, err := loaded.ProfileModules([]string{"base"})
			if err != nil {
				t.Fatalf("ProfileModules(active profile) error = %v", err)
			}
			if !reflect.DeepEqual(profileModules, []string{"good"}) {
				t.Fatalf("ProfileModules(active profile) = %v, want [good]", profileModules)
			}
			module, exists, applicability, err := loaded.InspectModule(
				"good",
				testPlatform("linux", "", ""),
			)
			if err != nil ||
				!exists ||
				module.ID != "good" ||
				applicability.State != coreconfig.ApplicabilityApplicable {
				t.Fatalf(
					"InspectModule(good) = (%#v, %t, %#v, %v), want applicable good",
					module,
					exists,
					applicability,
					err,
				)
			}
			if _, exists, _, err := loaded.InspectModule(
				"bad",
				testPlatform("linux", "", ""),
			); !exists || !errors.Is(err, coreconfig.ErrInvalidConfiguration) {
				t.Fatalf(
					"InspectModule(bad) = (exists=%t, err=%v), want selected failure",
					exists,
					err,
				)
			}
			assertTreeUnchanged(t, root, before)
		})
	}
}

func TestProfileModulesDefersInactiveDiscoveryError(t *testing.T) {
	root := t.TempDir()
	repository := writeRepository(t, root, `
version = 1

[profiles]
base = ["good"]
`)
	writeModule(t, repository, "good", "")
	badRoot := writeModule(t, repository, "bad", "")
	if err := os.Chmod(badRoot, 0); err != nil {
		t.Fatalf("os.Chmod(bad module) error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(badRoot, 0o700)
	})
	manifest := filepath.Join(badRoot, "module.toml")
	if _, err := os.Lstat(manifest); !errors.Is(err, fs.ErrPermission) {
		t.Skipf("filesystem does not enforce directory traversal permissions: %v", err)
	}

	loaded, err := coreconfig.OpenRepository(repository)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	profileModules, err := loaded.ProfileModules([]string{"base"})
	if err != nil {
		t.Fatalf("ProfileModules(active profile) error = %v", err)
	}
	if !reflect.DeepEqual(profileModules, []string{"good"}) {
		t.Fatalf("ProfileModules(active profile) = %v, want [good]", profileModules)
	}
	if _, exists, _, err := loaded.InspectModule(
		"bad",
		testPlatform("linux", "", ""),
	); exists ||
		!errors.Is(err, coreconfig.ErrInvalidConfiguration) ||
		!errors.Is(err, fs.ErrPermission) {
		t.Fatalf(
			"InspectModule(bad) = (exists=%t, err=%v), want discovery permission failure",
			exists,
			err,
		)
	}
}

type manifestEntryTest struct {
	name   string
	create func(*testing.T, string)
}

func unsafeManifestEntries() []manifestEntryTest {
	return []manifestEntryTest{
		{
			name: "directory",
			create: func(t *testing.T, path string) {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(manifest) error = %v", err)
				}
			},
		},
		{
			name: "socket",
			create: func(t *testing.T, path string) {
				socket := unixSocket(t, temporarySocketPath(t))
				if err := os.Symlink(socket, path); err != nil {
					t.Fatalf("os.Symlink(socket manifest) error = %v", err)
				}
			},
		},
		{
			name: "device",
			create: func(t *testing.T, path string) {
				if err := os.Symlink(os.DevNull, path); err != nil {
					t.Fatalf("os.Symlink(device manifest) error = %v", err)
				}
			},
		},
		{
			name: "dangling symlink",
			create: func(t *testing.T, path string) {
				if err := os.Symlink("missing.toml", path); err != nil {
					t.Fatalf("os.Symlink(dangling manifest) error = %v", err)
				}
			},
		},
		{
			name: "symlink loop",
			create: func(t *testing.T, path string) {
				if err := os.Symlink(filepath.Base(path), path); err != nil {
					t.Fatalf("os.Symlink(loop manifest) error = %v", err)
				}
			},
		},
	}
}

func unixSocket(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(socket parent) error = %v", err)
	}
	listener, err := net.Listen("unix", path)
	if errors.Is(err, os.ErrPermission) {
		t.Skipf("Unix socket creation is unavailable in this sandbox: %v", err)
	}
	if err != nil {
		t.Fatalf("net.Listen(unix, %q) error = %v", path, err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	return path
}

func temporarySocketPath(t *testing.T) string {
	t.Helper()
	file, err := os.CreateTemp("/tmp", "dot-cp4-socket-*")
	if err != nil {
		t.Fatalf("os.CreateTemp(socket path) error = %v", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatalf("close temporary socket path error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove temporary socket path error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	return path
}
