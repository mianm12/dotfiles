package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Load reads state from an absolute path. A genuinely missing path returns a
// valid empty state and a warning; other read failures remain errors.
func Load(path, home string) (Loaded, error) {
	empty, err := New(home)
	if err != nil {
		return Loaded{}, err
	}
	if path == "" || !filepath.IsAbs(path) {
		return Loaded{}, invalidf(
			"state path %q must be a non-empty absolute path",
			path,
		)
	}
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if statePathMissing(path, err) {
			return Loaded{
				Snapshot: empty,
				Missing:  true,
				Warning:  MissingWarning,
			}, nil
		}
		return Loaded{}, fmt.Errorf("read state %q: %w", path, err)
	}
	snapshot, err := Decode(data, empty.Home)
	if err != nil {
		return Loaded{}, fmt.Errorf("load state %q: %w", path, err)
	}
	return Loaded{Snapshot: snapshot}, nil
}

func statePathMissing(path string, readErr error) bool {
	if !errors.Is(readErr, fs.ErrNotExist) {
		return false
	}

	current := filepath.Clean(path)
	atRequestedPath := true
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if atRequestedPath {
				return false
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				info, err = os.Stat(current)
				if err != nil {
					return false
				}
			}
			return info.IsDir()
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return true
		}
		current = parent
		atRequestedPath = false
	}
}
