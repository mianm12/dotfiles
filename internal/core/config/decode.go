package config

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/identifier"
	"github.com/pelletier/go-toml/v2"
)

// ValidateModuleID validates one module identifier without constructing a
// machine configuration or consulting the repository.
func ValidateModuleID(id string) error {
	return validateID("module", id)
}

func decodeStrict(path string, destination any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}
	decoder := toml.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode %q: %w", path, err)
	}
	return nil
}

func decodeStrictManifest(path string, destination any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect manifest %q: %w", path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		info, err = os.Stat(path)
		if err != nil {
			return fmt.Errorf("resolve manifest %q: %w", path, err)
		}
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf(
			"manifest %q must be a regular file or symlink to a regular file",
			path,
		)
	}
	return decodeStrict(path, destination)
}

func validateID(kind, value string) error {
	if !identifier.Valid(value) {
		return fmt.Errorf("%w: invalid %s ID %q", ErrInvalidConfiguration, kind, value)
	}
	return nil
}

func validateUniqueIDs(kind string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateID(kind, value); err != nil {
			return err
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%w: duplicate %s ID %q", ErrInvalidConfiguration, kind, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateLowerTokens(kind string, values []string) error {
	for _, value := range values {
		if value == "" || strings.ToLower(value) != value {
			return fmt.Errorf(
				"%w: %s token %q must be a non-empty lowercase string",
				ErrInvalidConfiguration,
				kind,
				value,
			)
		}
	}
	return nil
}
