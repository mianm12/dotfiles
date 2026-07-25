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
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		f.Fatalf("filepath.EvalSymlinks(home) error = %v", err)
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
		assertStrictDescendant(t, home, target.Lexical())
		assertStrictDescendant(t, resolvedHome, target.Resolved())
	})
}

func assertStrictDescendant(t *testing.T, parent, child string) {
	t.Helper()
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q) error = %v", parent, child, err)
	}
	if relative == "." ||
		relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("path %q escaped synthetic HOME %q", child, parent)
	}
}
