package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type machineDocument struct {
	Version      *int     `toml:"version"`
	Repository   *string  `toml:"repository"`
	Profiles     []string `toml:"profiles"`
	ExtraModules []string `toml:"extra_modules"`
}

// LoadMachine strictly reads a machine config. A missing path is a valid
// uninitialized state; an existing unreadable entry is an error.
func LoadMachine(path string) (Machine, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Machine{}, false, nil
		}
		return Machine{}, false, fmt.Errorf("inspect machine config %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return Machine{}, false, fmt.Errorf(
			"%w: machine config %q must be a direct regular file",
			ErrInvalidConfiguration,
			path,
		)
	}

	var document machineDocument
	if err := decodeStrict(path, &document); err != nil {
		return Machine{}, false, fmt.Errorf("%w: machine config: %w", ErrInvalidConfiguration, err)
	}
	if document.Version == nil || *document.Version != 1 {
		return Machine{}, false, fmt.Errorf(
			"%w: machine config version must be 1",
			ErrInvalidConfiguration,
		)
	}
	if document.Repository == nil || *document.Repository == "" ||
		strings.ContainsRune(*document.Repository, '\x00') ||
		!utf8.ValidString(*document.Repository) ||
		!filepath.IsAbs(*document.Repository) {
		return Machine{}, false, fmt.Errorf(
			"%w: machine repository must be a non-empty absolute path without NUL and with valid UTF-8",
			ErrInvalidConfiguration,
		)
	}
	if err := validateUniqueIDs("profile", document.Profiles); err != nil {
		return Machine{}, false, err
	}
	if err := validateIDs("extra module", document.ExtraModules); err != nil {
		return Machine{}, false, err
	}

	return Machine{
		Version:      1,
		Repository:   filepath.Clean(*document.Repository),
		Profiles:     append([]string(nil), document.Profiles...),
		ExtraModules: append([]string(nil), document.ExtraModules...),
	}, true, nil
}
