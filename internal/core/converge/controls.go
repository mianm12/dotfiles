package converge

import (
	"fmt"
	"io/fs"
	"path/filepath"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/storage"
)

type controlEntry struct {
	name      string
	path      string
	want      fs.FileMode
	directory bool
}

func planControlModes(paths corepaths.Controls) ([]planned, error) {
	entries := []controlEntry{
		{name: "config-root", path: filepath.Dir(paths.Config), want: storage.PrivateDirectoryMode, directory: true},
		{name: "config", path: paths.Config, want: storage.PrivateFileMode},
		{name: "state-root", path: filepath.Dir(paths.State), want: storage.PrivateDirectoryMode, directory: true},
		{name: "state", path: paths.State, want: storage.PrivateFileMode},
		{name: "lock", path: paths.Lock, want: storage.PrivateFileMode},
	}
	lines := make([]planned, 0, len(entries))
	for _, entry := range entries {
		mode, exists, err := inspectControlEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("inspect %s %q: %w", entry.name, entry.path, err)
		}
		if !exists || storage.PrivateModeMatches(mode, entry.want) {
			continue
		}
		lines = append(lines, planned{
			Line: Line{
				Op:      OpChmod,
				Control: entry.name,
				Path:    entry.path,
				Mode:    fmt.Sprintf("%04o", entry.want),
			},
			mode: entry.want,
		})
	}
	return lines, nil
}

func inspectControlEntry(entry controlEntry) (fs.FileMode, bool, error) {
	if entry.directory {
		return storage.InspectRoot(entry.path)
	}
	return storage.InspectPrivateFile(entry.path)
}
