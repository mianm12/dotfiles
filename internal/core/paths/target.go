// Package paths resolves target locations for the replacement core.
//
// It intentionally models only the path rules from the design baseline: lexical
// normalization, existing ancestor symlinks, target-set uniqueness, and control
// path boundaries. It does not infer filesystem name aliases or inode identity.
package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrInvalidPath reports a target or control path outside the supported syntax.
	ErrInvalidPath = errors.New("invalid path")
	// ErrPathBlocked reports an ancestor that cannot be resolved as a directory.
	ErrPathBlocked = errors.New("path is blocked")
)

// Target is one target expression after HOME expansion and ancestor resolution.
// Lexical is the cleaned logical-HOME path. Resolved follows existing ancestor
// symlinks but never follows the target leaf itself.
type Target struct {
	lexical  string
	resolved string
}

// Lexical returns the cleaned path under the logical HOME.
func (target Target) Lexical() string {
	return target.lexical
}

// Resolved returns the target path after resolving existing ancestor symlinks.
func (target Target) Resolved() string {
	return target.resolved
}

// ResolveTarget expands a ~/ target against an absolute logical HOME.
func ResolveTarget(home, expression string) (Target, error) {
	cleanHome, err := cleanAbsolute("HOME", home)
	if err != nil {
		return Target{}, err
	}
	lexical, err := expandTarget(cleanHome, expression)
	if err != nil {
		return Target{}, err
	}

	resolved, err := resolveEntry(lexical)
	if err != nil {
		return Target{}, fmt.Errorf("resolve target %q parent: %w", expression, err)
	}
	info, err := os.Stat(filepath.Dir(resolved))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Target{}, fmt.Errorf(
			"inspect target %q parent %q: %w",
			expression,
			filepath.Dir(resolved),
			err,
		)
	}
	if err == nil && !info.IsDir() {
		return Target{}, fmt.Errorf(
			"%w: target %q parent %q is not a directory",
			ErrPathBlocked,
			expression,
			filepath.Dir(resolved),
		)
	}

	return Target{
		lexical:  lexical,
		resolved: resolved,
	}, nil
}

// ValidateTargetExpression validates the target syntax without consulting the
// filesystem or requiring a concrete HOME.
func ValidateTargetExpression(expression string) error {
	_, err := targetRelative(expression)
	return err
}

func expandTarget(home, expression string) (string, error) {
	relative, err := targetRelative(expression)
	if err != nil {
		return "", err
	}

	lexical := filepath.Clean(filepath.Join(home, relative))
	if !strictDescendant(home, lexical) {
		return "", fmt.Errorf("%w: target %q escapes HOME %q", ErrInvalidPath, expression, home)
	}
	return lexical, nil
}

func targetRelative(expression string) (string, error) {
	if !strings.HasPrefix(expression, "~/") {
		return "", fmt.Errorf("%w: target %q must start with ~/", ErrInvalidPath, expression)
	}
	relative := strings.TrimPrefix(expression, "~/")
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%w: target %q must name a path below HOME", ErrInvalidPath, expression)
	}
	if strings.ContainsRune(relative, '\x00') {
		return "", fmt.Errorf("%w: target %q contains NUL", ErrInvalidPath, expression)
	}
	if strings.ContainsAny(relative, "$*?[`") {
		return "", fmt.Errorf(
			"%w: target %q contains unsupported expansion syntax",
			ErrInvalidPath,
			expression,
		)
	}

	cleaned := filepath.Clean(relative)
	if cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: target %q escapes HOME", ErrInvalidPath, expression)
	}
	return cleaned, nil
}

func cleanAbsolute(label, path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %s must be a non-empty absolute path", ErrInvalidPath, label)
	}
	if strings.ContainsRune(path, '\x00') {
		return "", fmt.Errorf("%w: %s contains NUL", ErrInvalidPath, label)
	}
	return filepath.Clean(path), nil
}

func resolveEntry(path string) (string, error) {
	parent, err := resolvePath(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

// resolvePath follows every existing symlink in path. If a suffix is missing,
// it resolves the closest existing directory and appends the missing names
// literally. No directory scan or name canonicalization is performed.
func resolvePath(path string) (string, error) {
	cleanPath, err := cleanAbsolute("path", path)
	if err != nil {
		return "", err
	}

	current := cleanPath
	missing := make([]string, 0)
	for {
		info, inspectErr := os.Lstat(current)
		if inspectErr == nil {
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", fmt.Errorf("%w: resolve existing path %q: %w", ErrPathBlocked, current, resolveErr)
			}
			if len(missing) > 0 {
				resolvedInfo, statErr := os.Stat(resolved)
				if statErr != nil {
					return "", fmt.Errorf("inspect resolved ancestor %q: %w", resolved, statErr)
				}
				if !resolvedInfo.IsDir() {
					return "", fmt.Errorf("%w: ancestor %q is not a directory", ErrPathBlocked, current)
				}
			} else if info.Mode()&fs.ModeSymlink != 0 {
				if _, statErr := os.Stat(resolved); statErr != nil {
					return "", fmt.Errorf("%w: inspect symlink destination %q: %w", ErrPathBlocked, resolved, statErr)
				}
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(inspectErr, fs.ErrNotExist) {
			return "", fmt.Errorf("%w: inspect ancestor %q: %w", ErrPathBlocked, current, inspectErr)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("%w: no existing ancestor for %q", ErrPathBlocked, cleanPath)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func strictDescendant(parent, candidate string) bool {
	relative, err := filepath.Rel(parent, candidate)
	return err == nil &&
		relative != "." &&
		relative != ".." &&
		!filepath.IsAbs(relative) &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func sameOrDescendant(parent, candidate string) bool {
	return parent == candidate || strictDescendant(parent, candidate)
}
