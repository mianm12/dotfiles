package paths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTarget_ExpandsAndCleansBelowHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	target, err := ResolveTarget(home, "~/.config/app/../tool/config")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	wantLexical := filepath.Join(home, ".config", "tool", "config")
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error = %v", err)
	}
	wantResolved := filepath.Join(resolvedHome, ".config", "tool", "config")
	if target.Lexical() != wantLexical || target.Resolved() != wantResolved {
		t.Fatalf(
			"ResolveTarget() = (%q, %q), want (%q, %q)",
			target.Lexical(),
			target.Resolved(),
			wantLexical,
			wantResolved,
		)
	}
}

func TestResolveTarget_RejectsUnsupportedOrEscapingExpressions(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	for _, expression := range []string{
		"",
		"relative",
		"~",
		"~/",
		"~/../outside",
		"~/$HOME/config",
		"~/*.toml",
		"~/$(command)",
		"~/`command`",
	} {
		t.Run(expression, func(t *testing.T) {
			if _, err := ResolveTarget(home, expression); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("ResolveTarget(%q) error = %v, want ErrInvalidPath", expression, err)
			}
		})
	}
}

func TestResolveTarget_RejectsBlockedAndDanglingAncestors(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "file"), []byte("data"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(file) error = %v", err)
	}
	if err := os.Symlink("missing", filepath.Join(home, "dangling")); err != nil {
		t.Fatalf("os.Symlink(dangling) error = %v", err)
	}

	for _, expression := range []string{"~/file/child", "~/dangling/child"} {
		t.Run(expression, func(t *testing.T) {
			if _, err := ResolveTarget(home, expression); !errors.Is(err, ErrPathBlocked) {
				t.Fatalf("ResolveTarget(%q) error = %v, want ErrPathBlocked", expression, err)
			}
		})
	}
}

func TestResolveTarget_DoesNotFollowTargetLeafSymlink(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	destination := filepath.Join(root, "destination")
	for _, directory := range []string{home, destination} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	leaf := filepath.Join(home, "leaf")
	if err := os.Symlink(destination, leaf); err != nil {
		t.Fatalf("os.Symlink(leaf) error = %v", err)
	}
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error = %v", err)
	}

	target, err := ResolveTarget(home, "~/leaf")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if got, want := target.Resolved(), filepath.Join(resolvedHome, "leaf"); got != want {
		t.Fatalf("Resolved() = %q, want target entry %q", got, want)
	}
	if target.Resolved() == destination {
		t.Fatal("ResolveTarget() followed the target leaf symlink")
	}
}

func TestValidate_DoesNotInventCaseUnicodeOrHardLinkAliases(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	first := filepath.Join(home, "first")
	second := filepath.Join(home, "second")
	if err := os.WriteFile(first, []byte("same inode"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(first) error = %v", err)
	}
	if err := os.Link(first, second); err != nil {
		t.Fatalf("os.Link(second) error = %v", err)
	}

	placements := []Placement{
		{Label: "case-upper", Target: "~/Missing/Config"},
		{Label: "case-lower", Target: "~/missing/config"},
		{Label: "unicode-composed", Target: "~/missing/\u00e9"},
		{Label: "unicode-decomposed", Target: "~/missing/e\u0301"},
		{Label: "hard-link-first", Target: "~/first"},
		{Label: "hard-link-second", Target: "~/second"},
	}
	resolved, err := Validate(home, controlsOutsideFixture(root), placements)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(resolved) != len(placements) {
		t.Fatalf("Validate() returned %d placements, want %d", len(resolved), len(placements))
	}
}

func controlsOutsideFixture(root string) Controls {
	controlRoot := filepath.Join(root, "control")
	return Controls{
		Repository: filepath.Join(controlRoot, "repository"),
		Config:     filepath.Join(controlRoot, "config.toml"),
		State:      filepath.Join(controlRoot, "state.json"),
		Lock:       filepath.Join(controlRoot, "lock"),
	}
}
