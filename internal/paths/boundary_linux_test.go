//go:build linux

package paths

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePathBoundaries_LinuxMissingNamesFailClosed(t *testing.T) {
	t.Run("desired target", func(t *testing.T) {
		controlPaths := writeValidControlPlaneFixture(t)
		target := LabeledTarget{
			Label: "missing desired target",
			Path:  filepath.Join(filepath.Dir(controlPaths.Repository()), "missing-target"),
		}

		validated, err := ValidatePathBoundaries(controlPaths, []LabeledTarget{target})
		if !errors.Is(err, ErrIdentityUnavailable) || !strings.Contains(err.Error(), target.Label) {
			t.Fatalf("ValidatePathBoundaries() error = %v, want labeled ErrIdentityUnavailable", err)
		}
		if validated.control.paths != (ControlPlanePaths{}) || validated.targets.targets != nil {
			t.Fatalf("ValidatePathBoundaries() = %#v, want zero result", validated)
		}
	})

	t.Run("control member", func(t *testing.T) {
		controlPaths := writeValidControlPlaneFixture(t)
		if err := os.Remove(controlPaths.Config()); err != nil {
			t.Fatalf("os.Remove(%q) error = %v", controlPaths.Config(), err)
		}

		validated, err := ValidatePathBoundaries(controlPaths, nil)
		if !errors.Is(err, ErrIdentityUnavailable) || !strings.Contains(err.Error(), controlPaths.Config()) {
			t.Fatalf("ValidatePathBoundaries() error = %v, want control ErrIdentityUnavailable", err)
		}
		if validated.control.paths != (ControlPlanePaths{}) || validated.targets.targets != nil {
			t.Fatalf("ValidatePathBoundaries() = %#v, want zero result", validated)
		}
	})
}
