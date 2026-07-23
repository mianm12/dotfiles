package paths

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateControlPlane_Valid(t *testing.T) {
	paths := writeValidControlPlaneFixture(t)

	validated, err := ValidateControlPlane(paths)
	if err != nil {
		t.Fatalf("ValidateControlPlane() error = %v", err)
	}
	if validated.Paths() != paths {
		t.Fatalf("Paths() = %#v, want %#v", validated.Paths(), paths)
	}
}

func TestValidateControlPlane_AllFamilyPairsRejectEquality(t *testing.T) {
	roles := []controlMemberRole{
		controlMemberRepository,
		controlMemberConfig,
		controlMemberStateRoot,
		controlMemberInstalledBinary,
	}

	for leftIndex, leftRole := range roles {
		for _, rightRole := range roles[leftIndex+1:] {
			name := leftRole.String() + " and " + rightRole.String()
			t.Run(name, func(t *testing.T) {
				paths := writeValidControlPlaneFixture(t)
				paths.members[rightRole].path = paths.members[leftRole].path

				assertControlPlaneOverlap(t, paths, leftRole, rightRole, "equal")
			})
		}
	}
}

func TestValidateControlPlane_RejectsAncestorsAndAliases(t *testing.T) {
	t.Run("repository contains config", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		config := filepath.Join(paths.Repository(), "nested-config.toml")
		writeFixtureFile(t, config)
		paths.members[controlMemberConfig].path = config

		assertControlPlaneOverlap(
			t,
			paths,
			controlMemberRepository,
			controlMemberConfig,
			"left-ancestor",
		)
	})

	t.Run("config contains repository", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		paths.members[controlMemberConfig].path = filepath.Dir(paths.Repository())

		assertControlPlaneOverlap(
			t,
			paths,
			controlMemberRepository,
			controlMemberConfig,
			"right-ancestor",
		)
	})

	t.Run("repository symlink contains config", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		realRepository := filepath.Join(filepath.Dir(paths.Repository()), "real-repository")
		if err := os.Mkdir(realRepository, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", realRepository, err)
		}
		config := filepath.Join(realRepository, "config.toml")
		writeFixtureFile(t, config)
		if err := os.Remove(paths.Repository()); err != nil {
			t.Fatalf("os.Remove(%q) error = %v", paths.Repository(), err)
		}
		if err := os.Symlink(realRepository, paths.Repository()); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", realRepository, paths.Repository(), err)
		}
		paths.members[controlMemberConfig].path = config

		assertControlPlaneOverlap(
			t,
			paths,
			controlMemberRepository,
			controlMemberConfig,
			"left-ancestor",
		)
	})

	t.Run("config symlink consumes installed binary", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		if err := os.Remove(paths.Config()); err != nil {
			t.Fatalf("os.Remove(%q) error = %v", paths.Config(), err)
		}
		if err := os.Symlink(paths.InstalledBinary(), paths.Config()); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", paths.InstalledBinary(), paths.Config(), err)
		}

		assertControlPlaneOverlap(
			t,
			paths,
			controlMemberConfig,
			controlMemberInstalledBinary,
			"equal",
		)
	})

	t.Run("filesystem root repository", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		paths.members[controlMemberRepository].path = string(filepath.Separator)

		assertControlPlaneOverlap(
			t,
			paths,
			controlMemberRepository,
			controlMemberConfig,
			"left-ancestor",
		)
	})

	t.Run("installed binary is inside repository", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		binary := filepath.Join(paths.Repository(), "bin", "dot")
		if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(binary), err)
		}
		writeFixtureFile(t, binary)
		paths.members[controlMemberInstalledBinary].path = binary

		assertControlPlaneOverlap(
			t,
			paths,
			controlMemberRepository,
			controlMemberInstalledBinary,
			"left-ancestor",
		)
	})

	t.Run("repository ancestor symlink enters state", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		nestedRepository := filepath.Join(paths.StateRoot(), "nested-repository")
		if err := os.Mkdir(nestedRepository, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", nestedRepository, err)
		}
		stateAlias := filepath.Join(filepath.Dir(paths.Repository()), "state-alias")
		if err := os.Symlink(paths.StateRoot(), stateAlias); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", paths.StateRoot(), stateAlias, err)
		}
		paths.members[controlMemberRepository].path = filepath.Join(stateAlias, "nested-repository")

		assertControlPlaneOverlap(
			t,
			paths,
			controlMemberRepository,
			controlMemberStateRoot,
			"right-ancestor",
		)
	})
}

func TestValidateControlPlane_NameAliasesMatchFilesystem(t *testing.T) {
	tests := []struct {
		name   string
		actual string
		alias  string
	}{
		{name: "case", actual: "ControlCase", alias: "controlcase"},
		{name: "Unicode", actual: "caf\u00e9", alias: "cafe\u0301"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := writeValidControlPlaneFixture(t)
			parent := filepath.Dir(paths.Config())
			actual := filepath.Join(parent, test.actual)
			alias := filepath.Join(parent, test.alias)
			if err := os.Rename(paths.Config(), actual); err != nil {
				t.Fatalf("os.Rename(%q, %q) error = %v", paths.Config(), actual, err)
			}
			paths.members[controlMemberConfig].path = actual
			paths.members[controlMemberInstalledBinary].path = alias

			_, lookupErr := os.Lstat(alias)
			_, err := ValidateControlPlane(paths)
			switch {
			case lookupErr == nil:
				if !errors.Is(err, ErrControlPlaneOverlap) {
					t.Fatalf("filesystem accepts alias but ValidateControlPlane() error = %v, want overlap", err)
				}
				for _, want := range []string{actual, alias, "equal"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("ValidateControlPlane() error = %q, want %q", err, want)
					}
				}
			case errors.Is(lookupErr, fs.ErrNotExist):
				if err == nil || errors.Is(err, ErrIdentityUnavailable) {
					return
				}
				if errors.Is(err, ErrControlPlaneOverlap) {
					t.Fatalf("filesystem distinguishes names but validator reports overlap: %v", err)
				}
				t.Fatalf("ValidateControlPlane() error = %v, want success or ErrIdentityUnavailable", err)
			default:
				t.Fatalf("observe filesystem alias %q: %v", alias, lookupErr)
			}
		})
	}
}

func TestValidateControlPlane_StatePlannedHierarchy(t *testing.T) {
	t.Run("ordinary children", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		if _, err := ValidateControlPlane(paths); err != nil {
			t.Fatalf("ValidateControlPlane() error = %v", err)
		}
	})

	t.Run("state root symlink", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		realState := filepath.Join(filepath.Dir(filepath.Dir(paths.StateRoot())), "real-state")
		if err := os.Rename(paths.StateRoot(), realState); err != nil {
			t.Fatalf("os.Rename(%q, %q) error = %v", paths.StateRoot(), realState, err)
		}
		if err := os.Symlink(realState, paths.StateRoot()); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", realState, paths.StateRoot(), err)
		}

		if _, err := ValidateControlPlane(paths); err != nil {
			t.Fatalf("ValidateControlPlane() error = %v", err)
		}
	})

	t.Run("state file symlink outside family", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		external := filepath.Join(filepath.Dir(paths.Repository()), "external-state.json")
		writeFixtureFile(t, external)
		if err := os.Remove(paths.StateFile()); err != nil {
			t.Fatalf("os.Remove(%q) error = %v", paths.StateFile(), err)
		}
		if err := os.Symlink(external, paths.StateFile()); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", external, paths.StateFile(), err)
		}

		if _, err := ValidateControlPlane(paths); err != nil {
			t.Fatalf("ValidateControlPlane() error = %v", err)
		}
	})

	t.Run("hard-linked state siblings", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		if err := os.Remove(paths.StateLock()); err != nil {
			t.Fatalf("os.Remove(%q) error = %v", paths.StateLock(), err)
		}
		if err := os.Link(paths.StateFile(), paths.StateLock()); err != nil {
			t.Fatalf("os.Link(%q, %q) error = %v", paths.StateFile(), paths.StateLock(), err)
		}

		if _, err := ValidateControlPlane(paths); err != nil {
			t.Fatalf("ValidateControlPlane() error = %v", err)
		}
	})
}

func TestValidateControlPlane_RejectsUnexpectedStateOverlap(t *testing.T) {
	t.Run("state siblings consume same path", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		if err := os.Remove(paths.StateFile()); err != nil {
			t.Fatalf("os.Remove(%q) error = %v", paths.StateFile(), err)
		}
		if err := os.Symlink(filepath.Base(paths.StateLock()), paths.StateFile()); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", filepath.Base(paths.StateLock()), paths.StateFile(), err)
		}

		assertControlPlaneOverlap(
			t,
			paths,
			controlMemberStateFile,
			controlMemberStateLock,
			"equal",
		)
	})

	t.Run("state child consumes state root", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		if err := os.Remove(paths.StateFile()); err != nil {
			t.Fatalf("os.Remove(%q) error = %v", paths.StateFile(), err)
		}
		if err := os.Symlink(".", paths.StateFile()); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", ".", paths.StateFile(), err)
		}

		assertControlPlaneOverlap(
			t,
			paths,
			controlMemberStateRoot,
			controlMemberStateFile,
			"equal",
		)
	})
}

func TestValidateControlPlane_FailsClosedOnIdentityError(t *testing.T) {
	t.Run("dangling control symlink", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		if err := os.Remove(paths.Config()); err != nil {
			t.Fatalf("os.Remove(%q) error = %v", paths.Config(), err)
		}
		if err := os.Symlink("missing", paths.Config()); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", "missing", paths.Config(), err)
		}

		_, err := ValidateControlPlane(paths)
		if !errors.Is(err, ErrPathBlocked) {
			t.Fatalf("ValidateControlPlane() error = %v, want ErrPathBlocked", err)
		}
	})

	t.Run("zero member table", func(t *testing.T) {
		_, err := ValidateControlPlane(ControlPlanePaths{})
		if err == nil {
			t.Fatal("ValidateControlPlane() error = nil, want failure")
		}
	})

	t.Run("permission error", func(t *testing.T) {
		paths := writeValidControlPlaneFixture(t)
		restricted := filepath.Join(filepath.Dir(paths.Repository()), "restricted")
		if err := os.Mkdir(restricted, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", restricted, err)
		}
		config := filepath.Join(restricted, "config.toml")
		writeFixtureFile(t, config)
		if err := os.Chmod(restricted, 0o400); err != nil {
			t.Fatalf("os.Chmod(%q) error = %v", restricted, err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(restricted, 0o700); err != nil {
				t.Errorf("restore os.Chmod(%q) error = %v", restricted, err)
			}
		})
		if _, err := os.Lstat(config); !errors.Is(err, fs.ErrPermission) {
			t.Skipf("fixture config lookup error = %v, want permission error", err)
		}
		paths.members[controlMemberConfig].path = config

		_, err := ValidateControlPlane(paths)
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("ValidateControlPlane() error = %v, want permission cause", err)
		}
	})
}

func TestValidateControlPlane_IsReadOnly(t *testing.T) {
	paths := writeValidControlPlaneFixture(t)
	root := filepath.Dir(paths.Repository())
	before := snapshotFixtureTree(t, root)

	if _, err := ValidateControlPlane(paths); err != nil {
		t.Fatalf("ValidateControlPlane() error = %v", err)
	}
	after := snapshotFixtureTree(t, root)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("ValidateControlPlane() changed fixture tree:\nbefore=%v\nafter=%v", before, after)
	}
}

func writeValidControlPlaneFixture(t *testing.T) ControlPlanePaths {
	t.Helper()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repository")
	config := filepath.Join(root, "config.toml")
	paths, err := ResolveControlPlanePaths(home, repository, config)
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}

	for _, directory := range []string{
		paths.Repository(),
		paths.StateRoot(),
		filepath.Dir(paths.InstalledBinary()),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	for _, file := range []string{
		paths.Config(),
		paths.StateFile(),
		paths.StateLock(),
		paths.InstalledBinary(),
	} {
		writeFixtureFile(t, file)
	}
	return paths
}

func writeFixtureFile(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, []byte("fixture\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}

func assertControlPlaneOverlap(
	t *testing.T,
	paths ControlPlanePaths,
	leftRole, rightRole controlMemberRole,
	wantRelations ...string,
) {
	t.Helper()

	_, err := ValidateControlPlane(paths)
	if !errors.Is(err, ErrControlPlaneOverlap) {
		t.Fatalf("ValidateControlPlane() error = %v, want ErrControlPlaneOverlap", err)
	}
	for _, want := range []string{leftRole.String(), rightRole.String()} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateControlPlane() error = %q, want role %q", err, want)
		}
	}
	for _, want := range []string{
		paths.members[leftRole].path,
		paths.members[rightRole].path,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateControlPlane() error = %q, want path %q", err, want)
		}
	}
	for _, want := range []string{
		paths.members[leftRole].family.String(),
		paths.members[rightRole].family.String(),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateControlPlane() error = %q, want family %q", err, want)
		}
	}
	for _, want := range wantRelations {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateControlPlane() error = %q, want relation %q", err, want)
		}
	}
}

func snapshotFixtureTree(t *testing.T, root string) []string {
	t.Helper()

	entries := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		line := relative + " " + entry.Type().String()
		if entry.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			line += " -> " + target
		}
		entries = append(entries, line)
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.WalkDir(%q) error = %v", root, err)
	}
	return entries
}
