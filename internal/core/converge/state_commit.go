package converge

import (
	"fmt"

	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/storage"
)

func commitState(path string, snapshot state.Snapshot) (bool, error) {
	data, err := state.Marshal(snapshot)
	if err != nil {
		return false, err
	}
	changed, err := storage.PublishPrivateFile(path, data)
	if err != nil {
		return changed, fmt.Errorf("publish state %q: %w", path, err)
	}
	return changed, nil
}
