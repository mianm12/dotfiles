// Package lock owns the process lock used by core mutation entrypoints.
package lock

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/storage"
)

var (
	// ErrBusy reports that another mutation currently owns the advisory lock.
	ErrBusy = errors.New("another dot process is running")
	// ErrIO reports a failure while accessing the advisory lock.
	ErrIO = errors.New("process lock I/O failure")
)

// Acquire prepares and acquires the non-blocking exclusive lock. The returned
// release function is owned by the caller and must be invoked exactly once.
func Acquire(root, path string) (func() error, error) {
	cleanRoot, cleanPath, err := cleanPair(root, path)
	if err != nil {
		return nil, err
	}
	if err := validateEntries(cleanRoot, cleanPath); err != nil {
		return nil, err
	}
	if err := storage.EnsureRoot(cleanRoot); err != nil {
		return nil, fmt.Errorf("%w: prepare process lock root: %w", ErrIO, err)
	}
	if err := storage.EnsurePrivateFile(cleanPath); err != nil {
		return nil, fmt.Errorf("%w: prepare process lock file: %w", ErrIO, err)
	}

	fileLock := newBackend(cleanPath)
	locked, err := fileLock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("%w: acquire process lock %q: %w", ErrIO, cleanPath, err)
	}
	if !locked {
		return nil, fmt.Errorf("%w: %q", ErrBusy, cleanPath)
	}
	return releaseBackend(fileLock, cleanPath), nil
}

func releaseBackend(fileLock backend, path string) func() error {
	return func() error {
		if err := fileLock.Unlock(); err != nil {
			return fmt.Errorf("%w: release process lock %q: %w", ErrIO, path, err)
		}
		return nil
	}
}

// Validate performs the read-only validation used before lock acquisition.
func Validate(root, path string) error {
	cleanRoot, cleanPath, err := cleanPair(root, path)
	if err != nil {
		return err
	}
	return validateEntries(cleanRoot, cleanPath)
}

func validateEntries(root, path string) error {
	if err := storage.ValidateRoot(root); err != nil {
		return fmt.Errorf("%w: validate process lock root: %w", ErrIO, err)
	}
	if err := storage.ValidatePrivateFile(path); err != nil {
		return fmt.Errorf("%w: validate process lock file: %w", ErrIO, err)
	}
	return nil
}

func cleanPair(root, path string) (string, string, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", "", fmt.Errorf("process lock root must be a non-empty absolute path")
	}
	if path == "" || !filepath.IsAbs(path) {
		return "", "", fmt.Errorf("process lock path must be a non-empty absolute path")
	}
	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)
	if filepath.Dir(cleanPath) != cleanRoot {
		return "", "", fmt.Errorf(
			"process lock path %q must be directly inside root %q",
			cleanPath,
			cleanRoot,
		)
	}
	return cleanRoot, cleanPath, nil
}
