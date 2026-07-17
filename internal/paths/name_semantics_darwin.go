//go:build darwin

package paths

import (
	"fmt"
	"os"
	"syscall"
)

const pathconfCaseSensitive = 11

func missingNameKey(parent, name string) (string, error) {
	folded, ascii := asciiFold(name)
	if !ascii {
		return "", fmt.Errorf("%w: Unicode rules for missing names are not exposed by the filesystem", ErrIdentityUnavailable)
	}

	directory, err := os.Open(parent)
	if err != nil {
		return "", fmt.Errorf("open parent directory %q: %w", parent, err)
	}
	defer func() {
		_ = directory.Close()
	}()

	caseSensitive, err := syscall.Fpathconf(int(directory.Fd()), pathconfCaseSensitive)
	if err != nil {
		return "", fmt.Errorf("query case sensitivity for %q: %w", parent, err)
	}
	switch caseSensitive {
	case 1:
		return name, nil
	case 0:
		return folded, nil
	default:
		return "", fmt.Errorf("%w: filesystem returned unknown case sensitivity %d for %q", ErrIdentityUnavailable, caseSensitive, parent)
	}
}
