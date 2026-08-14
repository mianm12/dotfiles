package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzTargetExpression(f *testing.F) {
	home := filepath.Join(f.TempDir(), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		f.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	for _, seed := range []string{
		"~/.config/app/config",
		"~/nested/../config",
		"~/../outside",
		"~/$HOME/config",
		"",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, expression string) {
		if err := ValidateTargetExpression(expression); err != nil {
			return
		}
		target, err := ResolveTarget(home, expression)
		if err != nil {
			return
		}
		absolute, absoluteErr := target.Absolute(home)
		if absoluteErr != nil {
			t.Fatalf("Absolute(%q) error = %v", target.Relative(), absoluteErr)
		}
		relative, relErr := filepath.Rel(home, absolute)
		if relErr != nil {
			t.Fatalf("filepath.Rel(%q, %q) error = %v", home, absolute, relErr)
		}
		if relative != target.Relative() ||
			relative == "." ||
			relative == ".." ||
			strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Fatalf("path %q escaped synthetic HOME %q", absolute, home)
		}
	})
}
