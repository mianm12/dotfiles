//go:build linux && (amd64 || arm64)

package paths

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidatePathBoundaries_LinuxMissingControlPlaneIsReadOnly(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repository")
	for _, directory := range []string{home, repository} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", directory, err)
		}
	}
	controlPaths, err := ResolveControlPlanePaths(
		home,
		repository,
		filepath.Join(home, ".config", "dot", "config.toml"),
	)
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}
	before := snapshotFixtureTree(t, root)

	validated, err := ValidatePathBoundaries(controlPaths, nil)
	if err != nil {
		t.Fatalf("ValidatePathBoundaries() error = %v, want nil", err)
	}
	if validated.control.paths != controlPaths || len(validated.targets.targets) != 0 {
		t.Fatalf("ValidatePathBoundaries() = %#v, want validated empty target set", validated)
	}
	if after := snapshotFixtureTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("ValidatePathBoundaries() changed fixture tree:\nbefore=%v\nafter=%v", before, after)
	}
	for _, path := range []string{
		controlPaths.Config(),
		controlPaths.StateRoot(),
		controlPaths.StateFile(),
		controlPaths.StateLock(),
		controlPaths.BackupRoot(),
		controlPaths.InstalledBinary(),
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("os.Lstat(%q) error = %v, want missing path", path, err)
		}
	}
}

func TestValidateTargetSet_LinuxNestedMissingNamesUseByteIdentity(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name  string
		left  string
		right string
	}{
		{name: "case", left: "Config", right: "config"},
		{name: "Unicode normalization", left: "\u00e9", right: "e\u0301"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets := []LabeledTarget{
				{Label: "left", Path: filepath.Join(root, "missing", tt.left)},
				{Label: "right", Path: filepath.Join(root, "missing", tt.right)},
			}
			if _, err := ValidateTargetSet(targets); err != nil {
				t.Fatalf("ValidateTargetSet() error = %v, want byte-distinct targets", err)
			}
		})
	}

	parent := filepath.Join(root, "missing-parent")
	child := filepath.Join(parent, "nested", "child")
	_, err := ValidateTargetSet([]LabeledTarget{
		{Label: "parent", Path: parent},
		{Label: "child", Path: child},
	})
	if !errors.Is(err, ErrTargetOverlap) || !strings.Contains(err.Error(), "left-ancestor") {
		t.Fatalf("ValidateTargetSet() error = %v, want nested missing ancestor conflict", err)
	}
}
