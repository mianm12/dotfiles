package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/mianm12/dotfiles/internal/storage"
	"github.com/pelletier/go-toml/v2"
)

type encodedMachine struct {
	Version      int      `toml:"version"`
	Repository   string   `toml:"repository"`
	Profiles     []string `toml:"profiles"`
	ExtraModules []string `toml:"extra_modules"`
}

// MarshalMachine validates and deterministically encodes a machine selection.
func MarshalMachine(machine Machine) ([]byte, error) {
	if err := validateMachine(machine); err != nil {
		return nil, err
	}
	data, err := toml.Marshal(encodedMachine{
		Version:      1,
		Repository:   filepath.Clean(machine.Repository),
		Profiles:     append([]string(nil), machine.Profiles...),
		ExtraModules: append([]string(nil), machine.ExtraModules...),
	})
	if err != nil {
		return nil, fmt.Errorf("encode machine config: %w", err)
	}
	return data, nil
}

// PublishMachine atomically writes a changed machine selection with private
// permissions. Identical content is a no-op.
func PublishMachine(path string, machine Machine) (changed bool, err error) {
	if path == "" || !filepath.IsAbs(path) {
		return false, fmt.Errorf(
			"%w: machine config path must be a non-empty absolute path",
			ErrInvalidConfiguration,
		)
	}
	path = filepath.Clean(path)
	data, err := MarshalMachine(machine)
	if err != nil {
		return false, err
	}
	changed, err = storage.PublishPrivateFile(path, data)
	if err != nil {
		return changed, fmt.Errorf("publish machine config %q: %w", path, err)
	}
	return changed, nil
}

func validateMachine(machine Machine) error {
	if machine.Version != 1 {
		return fmt.Errorf(
			"%w: machine config version must be 1",
			ErrInvalidConfiguration,
		)
	}
	if machine.Repository == "" ||
		!filepath.IsAbs(machine.Repository) ||
		strings.ContainsRune(machine.Repository, '\x00') ||
		!utf8.ValidString(machine.Repository) ||
		filepath.Clean(machine.Repository) != machine.Repository {
		return fmt.Errorf(
			"%w: machine repository must be a normalized absolute path without NUL and with valid UTF-8",
			ErrInvalidConfiguration,
		)
	}
	if err := validateIDs("profile", machine.Profiles); err != nil {
		return err
	}
	if err := validateIDs("extra module", machine.ExtraModules); err != nil {
		return err
	}
	return nil
}
