package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/google/renameio/v2"
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
	current, readErr := os.ReadFile(path)
	switch {
	case readErr == nil && bytes.Equal(current, data):
		return false, nil
	case readErr != nil && !errors.Is(readErr, fs.ErrNotExist):
		return false, fmt.Errorf("read existing machine config %q: %w", path, readErr)
	}

	if err := storage.EnsureRoot(filepath.Dir(path)); err != nil {
		return false, err
	}
	pending, err := renameio.NewPendingFile(
		path,
		renameio.WithStaticPermissions(storage.PrivateFileMode),
	)
	if err != nil {
		return false, fmt.Errorf("create machine config temporary file: %w", err)
	}
	defer func() {
		if cleanupErr := pending.Cleanup(); cleanupErr != nil {
			err = errors.Join(
				err,
				fmt.Errorf("clean up machine config temporary file: %w", cleanupErr),
			)
		}
	}()
	if _, err := pending.Write(data); err != nil {
		return false, fmt.Errorf("write machine config temporary file: %w", err)
	}
	if err := pending.CloseAtomicallyReplace(); err != nil {
		return false, fmt.Errorf("publish machine config %q: %w", path, err)
	}
	return true, nil
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
		filepath.Clean(machine.Repository) != machine.Repository {
		return fmt.Errorf(
			"%w: machine repository must be a normalized absolute path",
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
