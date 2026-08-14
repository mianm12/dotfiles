package paths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTargetKeepsOneRelativeIdentity(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	target, err := ResolveTarget(home, "~/.config/app/../tool/config")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
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

func TestResolveTargetDoesNotResolveAncestorSymlink(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "alias")); err != nil {
		t.Fatal(err)
	}
	target, err := ResolveTarget(home, "~/alias/config")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
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

func TestResolveTargetRejectsUnsupportedExpressions(t *testing.T) {
	home := t.TempDir()
	for _, expression := range []string{"", "~", ".config/app", "~/../outside", "~/$HOME/app", "~/app/*"} {
		t.Run(expression, func(t *testing.T) {
			if _, err := ResolveTarget(home, expression); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("ResolveTarget(%q) error = %v, want ErrInvalidPath", expression, err)
			}
		})
	}
}

func TestTargetsConflictIsRelativeAndLexicalOnly(t *testing.T) {
	home := t.TempDir()
	parent, _ := ResolveTarget(home, "~/.config/app")
	child, _ := ResolveTarget(home, "~/.config/app/child")
	other, _ := ResolveTarget(home, "~/.other")
	if !TargetsConflict(parent, child) || !TargetsConflict(child, parent) {
		t.Fatal("parent and child must conflict")
	}
	if TargetsConflict(parent, other) {
		t.Fatal("disjoint targets must not conflict")
	}
	if !TargetsEqual(parent, parent) || TargetStrictlyContains(other, parent) {
		t.Fatal("target relations are inconsistent")
	}
}
