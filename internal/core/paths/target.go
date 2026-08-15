// Package paths owns lexical HOME targets and control-path prefixes.
package paths

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ErrInvalidPath reports a target or control path outside the supported syntax.
var ErrInvalidPath = errors.New("invalid path")

// Target is one canonical HOME-relative lexical placement identity.
type Target struct {
	relative string
}

// Relative returns the canonical HOME-relative identity without a ~/ prefix.
func (target Target) Relative() string {
	return target.relative
}

// Absolute derives the filesystem path used for observation and mutation.
func (target Target) Absolute(home string) (string, error) {
	cleanHome, err := cleanAbsolute("HOME", home)
	if err != nil {
		return "", err
	}
	if _, err := ResolveStoredTarget(target.relative); err != nil {
		return "", err
	}
	absolute := filepath.Join(cleanHome, target.relative)
	if !strictDescendant(cleanHome, absolute) {
		return "", fmt.Errorf(
			"%w: target %q escapes HOME %q",
			ErrInvalidPath,
			target.relative,
			home,
		)
	}
	return absolute, nil
}

// ResolveTarget normalizes one ~/ declaration into its HOME-relative identity.
func ResolveTarget(home, expression string) (Target, error) {
	cleanHome, err := cleanAbsolute("HOME", home)
	if err != nil {
		return Target{}, err
	}
	relative, err := targetRelative(expression)
	if err != nil {
		return Target{}, err
	}
	target := Target{relative: relative}
	if _, err := target.Absolute(cleanHome); err != nil {
		return Target{}, err
	}
	return target, nil
}

// ResolveStoredTarget validates the canonical identity persisted in state.
func ResolveStoredTarget(relative string) (Target, error) {
	if relative == "" || filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, "~/") ||
		strings.ContainsRune(relative, '\x00') || !utf8.ValidString(relative) {
		return Target{}, fmt.Errorf(
			"%w: stored target %q must be a non-empty HOME-relative UTF-8 path",
			ErrInvalidPath,
			relative,
		)
	}
	cleaned := filepath.Clean(relative)
	if cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) ||
		cleaned != relative {
		return Target{}, fmt.Errorf(
			"%w: stored target %q must be canonical and below HOME",
			ErrInvalidPath,
			relative,
		)
	}
	return Target{relative: relative}, nil
}

// ValidateTargetExpression validates the target syntax without consulting the
// filesystem or requiring a concrete HOME.
func ValidateTargetExpression(expression string) error {
	_, err := targetRelative(expression)
	return err
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
	if !utf8.ValidString(relative) {
		return "", fmt.Errorf("%w: target %q contains invalid UTF-8", ErrInvalidPath, expression)
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
	if !utf8.ValidString(path) {
		return "", fmt.Errorf("%w: %s contains invalid UTF-8", ErrInvalidPath, label)
	}
	return filepath.Clean(path), nil
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

// TargetsConflict reports lexical equality or nesting.
func TargetsConflict(left, right Target) bool {
	return left.relative == right.relative ||
		strictDescendant(left.relative, right.relative) ||
		strictDescendant(right.relative, left.relative)
}
