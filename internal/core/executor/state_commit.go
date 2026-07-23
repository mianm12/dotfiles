package executor

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/google/renameio/v2"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/storage"
)

func commitState(path string, snapshot state.Snapshot) (err error) {
	data, err := state.Marshal(snapshot)
	if err != nil {
		return err
	}
	parent := filepath.Dir(filepath.Clean(path))
	if err := storage.EnsureRoot(parent); err != nil {
		return err
	}

	pending, err := renameio.NewPendingFile(
		filepath.Clean(path),
		renameio.WithStaticPermissions(storage.PrivateFileMode),
	)
	if err != nil {
		return fmt.Errorf("create state temporary file: %w", err)
	}
	defer func() {
		if cleanupErr := pending.Cleanup(); cleanupErr != nil {
			err = errors.Join(
				err,
				fmt.Errorf("clean up state temporary file: %w", cleanupErr),
			)
		}
	}()
	if _, err := pending.Write(data); err != nil {
		return fmt.Errorf("write state temporary file: %w", err)
	}
	if err := pending.CloseAtomicallyReplace(); err != nil {
		return fmt.Errorf("publish state %q: %w", path, err)
	}
	return nil
}
