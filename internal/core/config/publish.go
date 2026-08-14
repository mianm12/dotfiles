package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

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
	if err := validateUniqueIDs("profile", machine.Profiles); err != nil {
		return err
	}
	if err := validateIDs("extra module", machine.ExtraModules); err != nil {
		return err
	}
	return nil
}
