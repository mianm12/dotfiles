package paths

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestValidateLexicalTargetControlBoundaries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	config := filepath.Join(root, "machine", "config.toml")
	controlPaths, err := ResolveControlPlanePaths(home, repository, config)
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}

	t.Run("rejects lexical control descendant", func(t *testing.T) {
		t.Parallel()

		err := ValidateLexicalTargetControlBoundaries(controlPaths, []LabeledTarget{{
			Label: "state target ~/.local/state/dot/backup/item",
			Path:  filepath.Join(controlPaths.BackupRoot(), "item"),
		}})
		if !errors.Is(err, ErrTargetControlOverlap) {
			t.Fatalf("error = %v, want ErrTargetControlOverlap", err)
		}
	})

	t.Run("accepts component prefix and does not inspect aliases", func(t *testing.T) {
		t.Parallel()

		targets := []LabeledTarget{
			{Label: "component prefix", Path: repository + "-data"},
			{Label: "unrelated", Path: filepath.Join(home, "managed", "file")},
		}
		if err := ValidateLexicalTargetControlBoundaries(controlPaths, targets); err != nil {
			t.Fatalf("ValidateLexicalTargetControlBoundaries() error = %v", err)
		}
	})
}

func TestValidateLexicalTargetControlBoundaries_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	controlPaths, err := ResolveControlPlanePaths(
		filepath.Join(root, "home"),
		filepath.Join(root, "repo"),
		filepath.Join(root, "config.toml"),
	)
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}

	if err := ValidateLexicalTargetControlBoundaries(controlPaths, []LabeledTarget{{
		Label: "relative",
		Path:  "relative",
	}}); err == nil {
		t.Fatal("relative target accepted")
	}
}
