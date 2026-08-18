package paths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseTargetKeepsOneRelativeIdentity(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	target, err := ParseTarget("~/.config/app/../tool/config")
	if err != nil {
		t.Fatalf("ParseTarget() error = %v", err)
	}
	if got, want := target.Relative(), filepath.Join(".config", "tool", "config"); got != want {
		t.Fatalf("Relative() = %q, want %q", got, want)
	}
	absolute, err := target.Absolute(home)
	if err != nil {
		t.Fatalf("Absolute() error = %v", err)
	}
	if want := filepath.Join(home, ".config", "tool", "config"); absolute != want {
		t.Fatalf("Absolute() = %q, want %q", absolute, want)
	}
}

func TestParseTargetDoesNotResolveAncestorSymlink(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "alias")); err != nil {
		t.Fatal(err)
	}
	target, err := ParseTarget("~/alias/config")
	if err != nil {
		t.Fatalf("ParseTarget() error = %v", err)
	}
	if got, want := target.Relative(), filepath.Join("alias", "config"); got != want {
		t.Fatalf("Relative() = %q, want lexical %q", got, want)
	}
	absolute, err := target.Absolute(home)
	if err != nil {
		t.Fatalf("Absolute() error = %v", err)
	}
	if want := filepath.Join(home, "alias", "config"); absolute != want {
		t.Fatalf("Absolute() = %q, want lexical %q", absolute, want)
	}
}

func TestResolveStoredTargetRequiresCanonicalRelativeIdentity(t *testing.T) {
	valid := filepath.Join(".config", "app")
	target, err := ResolveStoredTarget(valid)
	if err != nil || target.Relative() != valid {
		t.Fatalf("ResolveStoredTarget(valid) = (%q, %v)", target.Relative(), err)
	}
	for _, value := range []string{"", ".", "..", "../app", "~/app", "/tmp/app", "app/../other", "app/./file"} {
		t.Run(value, func(t *testing.T) {
			if _, err := ResolveStoredTarget(value); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("ResolveStoredTarget(%q) error = %v, want ErrInvalidPath", value, err)
			}
		})
	}
}

func TestParseTargetRejectsUnsupportedExpressions(t *testing.T) {
	for _, test := range []struct {
		name       string
		expression string
	}{
		{name: "empty"},
		{name: "bare tilde", expression: "~"},
		{name: "relative", expression: ".config/app"},
		{name: "escape", expression: "~/../outside"},
		{name: "expansion", expression: "~/$HOME/app"},
		{name: "glob", expression: "~/app/*"},
		{name: "nested declaration", expression: "~/~/app"},
		{name: "NUL", expression: "~/bad\x00name"},
		{name: "invalid UTF-8", expression: string([]byte{'~', '/', 0xff})},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseTarget(test.expression); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("ParseTarget(%q) error = %v, want ErrInvalidPath", test.expression, err)
			}
		})
	}
}

func TestTargetsConflictIsRelativeAndLexicalOnly(t *testing.T) {
	parent, _ := ParseTarget("~/.config/app")
	child, _ := ParseTarget("~/.config/app/child")
	other, _ := ParseTarget("~/.other")
	if !TargetsConflict(parent, child) || !TargetsConflict(child, parent) {
		t.Fatal("parent and child must conflict")
	}
	if TargetsConflict(parent, other) {
		t.Fatal("disjoint targets must not conflict")
	}
	if !TargetsConflict(parent, parent) {
		t.Fatal("equal targets must conflict")
	}
}

func TestZeroTargetCannotBecomeAbsolute(t *testing.T) {
	var target Target
	if _, err := target.Absolute(t.TempDir()); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Absolute() error = %v, want ErrInvalidPath", err)
	}
}
