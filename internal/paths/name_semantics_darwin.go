//go:build darwin

package paths

import (
	"fmt"
	"os"
	"syscall"
)

// pathconfCaseSensitive 对应 Darwin <sys/unistd.h> 的 _PC_CASE_SENSITIVE。
const pathconfCaseSensitive = 11

func missingNameKey(parent, name string) (string, error) {
	return missingNameKeyWithQuery(parent, name, queryCaseSensitivity)
}

func queryCaseSensitivity(fd int) (int, error) {
	return syscall.Fpathconf(fd, pathconfCaseSensitive)
}

func missingNameKeyWithQuery(parent, name string, query func(int) (int, error)) (string, error) {
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

	caseSensitive, err := query(int(directory.Fd()))
	if err != nil {
		return "", fmt.Errorf("%w: query case sensitivity for %q: %w", ErrIdentityUnavailable, parent, err)
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
