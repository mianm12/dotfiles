package planner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

type actualKind uint8

const (
	actualAbsent actualKind = iota
	actualSymlink
	actualRegular
	actualDirectory
	actualSpecial
)

type actual struct {
	kind            actualKind
	linkDestination string
}

func observeLocal(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect local target %q: %w", path, err)
	}
	return true, nil
}

func observeLink(path string) (actual, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return actual{kind: actualAbsent}, nil
	}
	if err != nil {
		return actual{}, fmt.Errorf("inspect target %q: %w", path, err)
	}

	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		destination, readErr := os.Readlink(path)
		if readErr != nil {
			return actual{}, fmt.Errorf("read target symlink %q: %w", path, readErr)
		}
		return actual{kind: actualSymlink, linkDestination: destination}, nil
	case info.Mode().IsRegular():
		return actual{kind: actualRegular}, nil
	case info.IsDir():
		return actual{kind: actualDirectory}, nil
	default:
		return actual{kind: actualSpecial}, nil
	}
}

func (kind actualKind) String() string {
	switch kind {
	case actualAbsent:
		return "absent"
	case actualSymlink:
		return "symlink"
	case actualRegular:
		return "regular file"
	case actualDirectory:
		return "directory"
	default:
		return "special file"
	}
}
