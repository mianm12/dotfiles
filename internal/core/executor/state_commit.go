package executor

import (
	"fmt"

	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/storage"
)

func commitState(path string, snapshot state.Snapshot) error {
	data, err := state.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = storage.PublishPrivateFile(path, data)
	if err != nil {
		return fmt.Errorf("publish state %q: %w", path, err)
	}
	return nil
}
