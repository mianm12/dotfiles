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

func TestValidatePathBoundaries_ValidAndReadOnly(t *testing.T) {
	controlPaths := writeValidControlPlaneFixture(t)
	root := filepath.Dir(controlPaths.Repository())
	targets := []LabeledTarget{
		{Label: "module app source config", Path: filepath.Join(root, "targets", "config")},
		{Label: "module shell source rc", Path: filepath.Join(root, "targets", "shell", "rc")},
	}
	for _, target := range targets {
		if err := os.MkdirAll(filepath.Dir(target.Path), 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(target.Path), err)
		}
		writeFixtureFile(t, target.Path)
	}
	before := snapshotFixtureTree(t, root)

	validated, err := ValidatePathBoundaries(controlPaths, targets)
	if err != nil {
		t.Fatalf("ValidatePathBoundaries() error = %v", err)
	}
	if len(validated.targets.targets) != len(targets) || validated.control.Paths() != controlPaths {
		t.Fatalf("ValidatePathBoundaries() = %#v, want complete validated result", validated)
	}
	if after := snapshotFixtureTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("ValidatePathBoundaries() changed fixture: before=%v after=%v", before, after)
	}
}

func TestValidatePathBoundaries_RejectsControlPlaneBeforeTargets(t *testing.T) {
	controlPaths := writeValidControlPlaneFixture(t)
	controlPaths.members[controlMemberConfig].path = controlPaths.Repository()
	target := LabeledTarget{Label: "invalid target should not resolve", Path: "relative"}

	validated, err := ValidatePathBoundaries(controlPaths, []LabeledTarget{target})
	if !errors.Is(err, ErrControlPlaneOverlap) {
		t.Fatalf("ValidatePathBoundaries() error = %v, want ErrControlPlaneOverlap", err)
	}
	if validated.control.paths != (ControlPlanePaths{}) || validated.targets.targets != nil {
		t.Fatalf("ValidatePathBoundaries() = %#v, want zero result", validated)
	}
	if strings.Contains(err.Error(), target.Label) {
		t.Fatalf("control-plane failure unexpectedly resolved target: %v", err)
	}
}

func TestValidatePathBoundaries_RejectsTargetSetBeforeCrossProduct(t *testing.T) {
	controlPaths := writeValidControlPlaneFixture(t)
	targetPath := filepath.Join(controlPaths.Repository(), "also-inside-repository")
	writeFixtureFile(t, targetPath)
	targets := []LabeledTarget{
		{Label: "first desired", Path: targetPath},
		{Label: "second desired", Path: targetPath},
	}

	validated, err := ValidatePathBoundaries(controlPaths, targets)
	if !errors.Is(err, ErrTargetOverlap) || errors.Is(err, ErrTargetControlOverlap) {
		t.Fatalf("ValidatePathBoundaries() error = %v, want target-set conflict before cross-product", err)
	}
	if validated.control.paths != (ControlPlanePaths{}) || validated.targets.targets != nil {
		t.Fatalf("ValidatePathBoundaries() = %#v, want zero result", validated)
	}
}

func TestValidatePathBoundaries_DesiredControlOverlapMatrix(t *testing.T) {
	roles := []controlMemberRole{
		controlMemberRepository,
		controlMemberConfig,
		controlMemberStateRoot,
		controlMemberStateFile,
		controlMemberStateLock,
		controlMemberInstalledBinary,
	}

	for _, role := range roles {
		t.Run(role.String(), func(t *testing.T) {
			controlPaths := writeValidControlPlaneFixture(t)
			targetPath := controlPaths.members[role].path
			if role > controlMemberStateRoot && role <= controlMemberStateLock {
				external := filepath.Join(
					filepath.Dir(controlPaths.Repository()),
					"external-"+filepath.Base(targetPath),
				)
				writeFixtureFile(t, external)
				if err := os.Remove(targetPath); err != nil {
					t.Fatalf("os.Remove(%q) error = %v", targetPath, err)
				}
				if err := os.Symlink(external, targetPath); err != nil {
					t.Fatalf("os.Symlink(%q, %q) error = %v", external, targetPath, err)
				}
				targetPath = external
			}

			assertTargetControlOverlap(t, controlPaths, LabeledTarget{
				Label: "desired for " + role.String(),
				Path:  targetPath,
			}, role)
		})
	}
}

func TestValidatePathBoundaries_RejectsDesiredInsideAndAboveControl(t *testing.T) {
	t.Run("desired inside repository", func(t *testing.T) {
		controlPaths := writeValidControlPlaneFixture(t)
		targetPath := filepath.Join(controlPaths.Repository(), "nested", "target")
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(targetPath), err)
		}
		writeFixtureFile(t, targetPath)
		target := LabeledTarget{
			Label: "desired inside repository",
			Path:  targetPath,
		}
		assertTargetControlOverlap(t, controlPaths, target, controlMemberRepository)
	})

	t.Run("desired is repository ancestor", func(t *testing.T) {
		controlPaths := writeValidControlPlaneFixture(t)
		target := LabeledTarget{
			Label: "desired above repository",
			Path:  filepath.Dir(controlPaths.Repository()),
		}
		assertTargetControlOverlap(t, controlPaths, target, controlMemberRepository)
	})

	t.Run("repository leaf symlink consumption", func(t *testing.T) {
		controlPaths := writeValidControlPlaneFixture(t)
		realRepository := filepath.Join(filepath.Dir(controlPaths.Repository()), "real-repository")
		if err := os.Mkdir(realRepository, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", realRepository, err)
		}
		if err := os.Remove(controlPaths.Repository()); err != nil {
			t.Fatalf("os.Remove(%q) error = %v", controlPaths.Repository(), err)
		}
		if err := os.Symlink(realRepository, controlPaths.Repository()); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", realRepository, controlPaths.Repository(), err)
		}
		targetPath := filepath.Join(realRepository, "target")
		writeFixtureFile(t, targetPath)
		target := LabeledTarget{
			Label: "desired in real repository",
			Path:  targetPath,
		}
		assertTargetControlOverlap(t, controlPaths, target, controlMemberRepository)
	})

	t.Run("desired symlink is control ancestor", func(t *testing.T) {
		controlPaths := writeValidControlPlaneFixture(t)
		root := filepath.Dir(controlPaths.Repository())
		realDirectory := filepath.Join(root, "real-control-parent")
		if err := os.Mkdir(realDirectory, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", realDirectory, err)
		}
		alias := filepath.Join(root, "control-parent-alias")
		if err := os.Symlink(realDirectory, alias); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", realDirectory, alias, err)
		}
		config := filepath.Join(alias, "config.toml")
		writeFixtureFile(t, filepath.Join(realDirectory, "config.toml"))
		controlPaths.members[controlMemberConfig].path = config

		assertTargetControlOverlap(t, controlPaths, LabeledTarget{
			Label: "desired ancestor symlink",
			Path:  alias,
		}, controlMemberConfig)
	})
}

func TestValidatePathBoundaries_NameAliasesMatchFilesystem(t *testing.T) {
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
			controlPaths := writeValidControlPlaneFixture(t)
			parent := filepath.Dir(controlPaths.Config())
			actual := filepath.Join(parent, test.actual)
			alias := filepath.Join(parent, test.alias)
			if err := os.Rename(controlPaths.Config(), actual); err != nil {
				t.Fatalf("os.Rename(%q, %q) error = %v", controlPaths.Config(), actual, err)
			}
			controlPaths.members[controlMemberConfig].path = actual
			_, lookupErr := os.Lstat(alias)
			validated, err := ValidatePathBoundaries(controlPaths, []LabeledTarget{{
				Label: test.name + " alias target",
				Path:  alias,
			}})

			switch {
			case lookupErr == nil:
				if !errors.Is(err, ErrTargetControlOverlap) {
					t.Fatalf("filesystem accepts alias but validator error = %v, want target/control overlap", err)
				}
			case errors.Is(lookupErr, fs.ErrNotExist):
				if err == nil || errors.Is(err, ErrIdentityUnavailable) {
					return
				}
				if errors.Is(err, ErrTargetControlOverlap) {
					t.Fatalf("filesystem distinguishes names but validator reports overlap: %v", err)
				}
				t.Fatalf("ValidatePathBoundaries() = (%#v, %v), want success or ErrIdentityUnavailable", validated, err)
			default:
				t.Fatalf("observe filesystem alias %q: %v", alias, lookupErr)
			}
		})
	}
}

func assertTargetControlOverlap(
	t *testing.T,
	controlPaths ControlPlanePaths,
	target LabeledTarget,
	wantRole controlMemberRole,
) {
	t.Helper()

	validated, err := ValidatePathBoundaries(controlPaths, []LabeledTarget{target})
	if !errors.Is(err, ErrTargetControlOverlap) {
		t.Fatalf("ValidatePathBoundaries() error = %v, want ErrTargetControlOverlap", err)
	}
	if validated.control.paths != (ControlPlanePaths{}) || validated.targets.targets != nil {
		t.Fatalf("ValidatePathBoundaries() = %#v, want zero result", validated)
	}
	member := controlPaths.members[wantRole]
	for _, want := range []string{
		target.Label,
		target.Path,
		member.family.String(),
		member.role.String(),
		member.path,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidatePathBoundaries() error = %q, want %q", err, want)
		}
	}
}
