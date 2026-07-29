package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
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

func TestResolveAbsoluteTargetRequiresStrictHomeDescendant(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	inside := filepath.Join(home, ".config", "tool")
	target, err := ResolveAbsoluteTarget(home, inside)
	if err != nil {
		t.Fatalf("ResolveAbsoluteTarget(inside) error = %v", err)
	}
	if got := target.Lexical(); got != inside {
		t.Fatalf("ResolveAbsoluteTarget(inside).Lexical() = %q, want %q", got, inside)
	}

	tests := []struct {
		name   string
		home   string
		target string
	}{
		{name: "HOME itself", home: home, target: home},
		{name: "HOME parent", home: home, target: root},
		{name: "sibling with HOME prefix", home: home, target: filepath.Join(root, "home-other")},
		{name: "relative HOME", home: "home", target: inside},
		{name: "relative target", home: home, target: ".config/tool"},
		{name: "target containing NUL", home: home, target: inside + "\x00"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveAbsoluteTarget(test.home, test.target)
			if !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("ResolveAbsoluteTarget() error = %v, want ErrInvalidPath", err)
			}
			if class, ok := ClassifyResolutionError(err); ok {
				t.Fatalf(
					"ClassifyResolutionError() = (%v, true), want unclassified invalid input",
					class,
				)
			}
		})
	}
}

func TestResolveAbsoluteTargetPreservesAncestorAndLeafSemantics(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	outside := filepath.Join(root, "outside")
	destination := filepath.Join(root, "destination")
	for _, directory := range []string{home, outside, destination} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(home, "alias")); err != nil {
		t.Fatalf("os.Symlink(ancestor) error = %v", err)
	}
	if err := os.Symlink(destination, filepath.Join(outside, "leaf")); err != nil {
		t.Fatalf("os.Symlink(leaf) error = %v", err)
	}
	resolvedOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(outside) error = %v", err)
	}

	absolute := filepath.Join(home, "alias", "leaf")
	target, err := ResolveAbsoluteTarget(home, absolute)
	if err != nil {
		t.Fatalf("ResolveAbsoluteTarget() error = %v", err)
	}
	if got := target.Lexical(); got != absolute {
		t.Fatalf("Lexical() = %q, want %q", got, absolute)
	}
	if got, want := target.Resolved(), filepath.Join(resolvedOutside, "leaf"); got != want {
		t.Fatalf("Resolved() = %q, want target entry %q", got, want)
	}
	if target.Resolved() == destination {
		t.Fatal("ResolveAbsoluteTarget() followed the target leaf symlink")
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
			_, err := ResolveTarget(home, expression)
			if !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("ResolveTarget(%q) error = %v, want ErrInvalidPath", expression, err)
			}
			if class, ok := ClassifyResolutionError(err); ok {
				t.Fatalf(
					"ClassifyResolutionError(%q) = (%v, true), want unclassified invalid input",
					expression,
					class,
				)
			}
		})
	}
}

func TestValidateTargetExpression_DoesNotConsultFilesystem(t *testing.T) {
	for _, expression := range []string{"~/config", "~/.config/app/config", "~/missing/../config"} {
		if err := ValidateTargetExpression(expression); err != nil {
			t.Fatalf("ValidateTargetExpression(%q) error = %v", expression, err)
		}
	}
	for _, expression := range []string{"", "~", "~/", "~/../../outside", "~/$HOME/config"} {
		if err := ValidateTargetExpression(expression); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf(
				"ValidateTargetExpression(%q) error = %v, want ErrInvalidPath",
				expression,
				err,
			)
		}
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
	if err := os.Symlink("file", filepath.Join(home, "file-alias")); err != nil {
		t.Fatalf("os.Symlink(file alias) error = %v", err)
	}
	if err := os.Symlink("loop", filepath.Join(home, "loop")); err != nil {
		t.Fatalf("os.Symlink(loop) error = %v", err)
	}

	for _, expression := range []string{
		"~/file/child",
		"~/file-alias/child",
		"~/dangling/child",
		"~/loop/child",
	} {
		t.Run(expression, func(t *testing.T) {
			_, err := ResolveTarget(home, expression)
			if !errors.Is(err, ErrPathBlocked) {
				t.Fatalf("ResolveTarget(%q) error = %v, want ErrPathBlocked", expression, err)
			}
			wrapped := fmt.Errorf("outer resolution context: %w", err)
			class, ok := ClassifyResolutionError(wrapped)
			if !ok || class != ResolutionObstructed {
				t.Fatalf(
					"ClassifyResolutionError(%q) = (%v, %t), want (%v, true)",
					expression,
					class,
					ok,
					ResolutionObstructed,
				)
			}
		})
	}
}

func TestResolveTarget_AllowsMissingSuffix(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}

	target, err := ResolveTarget(home, "~/missing/child/config")
	if err != nil {
		t.Fatalf("ResolveTarget(missing suffix) error = %v", err)
	}
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error = %v", err)
	}
	if got, want := target.Resolved(), filepath.Join(resolvedHome, "missing", "child", "config"); got != want {
		t.Fatalf("Resolved() = %q, want %q", got, want)
	}
}

func TestResolveTarget_ClassifiesUnavailableFilesystemObservation(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	blocked := filepath.Join(home, "blocked")
	if err := os.MkdirAll(blocked, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(blocked) error = %v", err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Skipf("os.Chmod(blocked) error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0o700); err != nil {
			t.Errorf("restore blocked directory mode: %v", err)
		}
	})

	_, err := ResolveTarget(home, "~/blocked/child/config")
	if err == nil {
		t.Skip("filesystem did not enforce directory search permission")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("ResolveTarget(permission denied) error = %v, want fs.ErrPermission", err)
	}
	wrapped := fmt.Errorf("outer resolution context: %w", err)
	class, ok := ClassifyResolutionError(wrapped)
	if !ok || class != ResolutionUnavailable {
		t.Fatalf(
			"ClassifyResolutionError(permission denied) = (%v, %t), want (%v, true)",
			class,
			ok,
			ResolutionUnavailable,
		)
	}
}

func TestFilesystemResolutionClass(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		class ResolutionClass
	}{
		{name: "missing", err: fs.ErrNotExist, class: ResolutionObstructed},
		{name: "not a directory", err: syscall.ENOTDIR, class: ResolutionObstructed},
		{name: "symlink loop", err: syscall.ELOOP, class: ResolutionObstructed},
		{name: "permission", err: fs.ErrPermission, class: ResolutionUnavailable},
		{name: "other I/O", err: errors.New("synthetic I/O error"), class: ResolutionUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := filesystemResolutionClass(test.err); got != test.class {
				t.Fatalf(
					"filesystemResolutionClass(%v) = %v, want %v",
					test.err,
					got,
					test.class,
				)
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

func TestValidateScopedIgnoresUnselectedOnlyConflictsAndBoundaries(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	controls := Controls{
		Repository: filepath.Join(root, "repo"),
		Config:     filepath.Join(home, ".control", "machine.toml"),
		State:      filepath.Join(root, "state-control", "state.json"),
		Lock:       filepath.Join(root, "state-control", "lock"),
	}
	placements := []Placement{
		{Label: "selected", Target: "~/.config/selected"},
		{Label: "outside-first", Target: "~/.config/shared"},
		{Label: "outside-second", Target: "~/.config/shared/child"},
		{Label: "outside-control", Target: "~/.control/machine.toml"},
	}

	resolved, err := ValidateScoped(
		home,
		controls,
		placements,
		[]string{"selected"},
	)
	if err != nil {
		t.Fatalf("ValidateScoped(unrelated conflicts) error = %v", err)
	}
	if len(resolved) != len(placements) {
		t.Fatalf("ValidateScoped() returned %d placements, want %d", len(resolved), len(placements))
	}

	placements[1].Target = "~/.config/selected/child"
	resolved, err = ValidateScoped(
		home,
		controls,
		placements,
		[]string{"selected"},
	)
	if !errors.Is(err, ErrTargetConflict) {
		t.Fatalf("ValidateScoped(selected conflict) = (%#v, %v), want target conflict", resolved, err)
	}
}

func controlsOutsideFixture(root string) Controls {
	return Controls{
		Repository: filepath.Join(root, "repository"),
		Config:     filepath.Join(root, "config-control", "config.toml"),
		State:      filepath.Join(root, "state-control", "state.json"),
		Lock:       filepath.Join(root, "state-control", "lock"),
	}
}
