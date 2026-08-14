// Package storage 维护私有控制目录和文件发布的共同权限边界。
package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/google/renameio/v2"
)

const (
	// PrivateDirectoryMode 是 state 等私有目录的规范权限。
	PrivateDirectoryMode fs.FileMode = 0o700
	// PrivateFileMode 是 state、lock 等私有控制文件的规范权限。
	PrivateFileMode fs.FileMode = 0o600
	privateModeMask fs.FileMode = fs.ModePerm |
		fs.ModeSetuid |
		fs.ModeSetgid |
		fs.ModeSticky
)

type privateFile interface {
	Chmod(fs.FileMode) error
	Close() error
}

type privateFileOpener func(string, int, fs.FileMode) (privateFile, error)

type pendingPrivateFile interface {
	Write([]byte) (int, error)
	Chmod(fs.FileMode) error
	CloseAtomicallyReplace() error
	Cleanup() error
}

type pendingPrivateFileFactory func(string) (pendingPrivateFile, error)

// ValidateRoot 只读校验私有控制根目录。目录尚不存在时也通过，由 mutation
// 在需要写入时调用 EnsureRoot 建立。
func ValidateRoot(path string) error {
	_, _, err := InspectRoot(path)
	return err
}

// InspectRoot returns the mode and existence of a private directory entry.
func InspectRoot(path string) (fs.FileMode, bool, error) {
	cleanPath, err := cleanAbsolute(path)
	if err != nil {
		return 0, false, fmt.Errorf("private root: %w", err)
	}
	return inspectRoot(cleanPath)
}

// ValidatePrivateFile read-only validates an existing private file entry.
// A missing path is valid because mutation may create it later.
func ValidatePrivateFile(path string) error {
	_, _, err := InspectPrivateFile(path)
	return err
}

// InspectPrivateFile returns the mode and existence of a regular control file.
func InspectPrivateFile(path string) (fs.FileMode, bool, error) {
	cleanPath, err := cleanAbsolute(path)
	if err != nil {
		return 0, false, fmt.Errorf("private file: %w", err)
	}
	info, err := os.Lstat(cleanPath)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("inspect private file %q: %w", cleanPath, err)
	}
	if !info.Mode().IsRegular() {
		return 0, false, fmt.Errorf("private file %q is not a regular file", cleanPath)
	}
	return info.Mode(), true, nil
}

// EnsureRoot 建立缺失的私有控制根目录；现存目录的权限由显式 chmod 行收敛。
// 最终对象必须是真实目录；更高层的 ancestor symlink 仍然合法。
func EnsureRoot(path string) error {
	cleanPath, err := cleanAbsolute(path)
	if err != nil {
		return fmt.Errorf("private root: %w", err)
	}

	_, exists, err := inspectRoot(cleanPath)
	if err != nil {
		return err
	}
	if !exists {
		if err := os.MkdirAll(cleanPath, PrivateDirectoryMode); err != nil {
			return fmt.Errorf("create private root %q: %w", cleanPath, err)
		}
		_, _, err = inspectRoot(cleanPath)
		if err != nil {
			return err
		}
		if err := os.Chmod(cleanPath, PrivateDirectoryMode); err != nil {
			return fmt.Errorf("set new private root permissions %q: %w", cleanPath, err)
		}
	}
	return nil
}

func inspectRoot(cleanPath string) (fs.FileMode, bool, error) {
	info, err := os.Lstat(cleanPath)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("inspect private root %q: %w", cleanPath, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return 0, false, fmt.Errorf(
			"private root %q is a symbolic link; expected a directory",
			cleanPath,
		)
	}
	if !info.IsDir() {
		return 0, false, fmt.Errorf("private root %q is not a directory", cleanPath)
	}
	return info.Mode(), true, nil
}

// EnsurePrivateFile 建立缺失的私有普通文件；现存文件的权限由显式 chmod 行收敛。
// 调用方必须先建立并校验父目录。
func EnsurePrivateFile(path string) error {
	cleanPath, err := cleanAbsolute(path)
	if err != nil {
		return fmt.Errorf("private file: %w", err)
	}
	return ensurePrivateFile(cleanPath, func(name string, flag int, perm fs.FileMode) (privateFile, error) {
		return os.OpenFile(name, flag, perm)
	})
}

func ensurePrivateFile(cleanPath string, openFile privateFileOpener) error {
	for {
		info, inspectErr := os.Lstat(cleanPath)
		switch {
		case inspectErr == nil:
			if !info.Mode().IsRegular() {
				return fmt.Errorf("private file %q is not a regular file", cleanPath)
			}
			return nil
		case !errors.Is(inspectErr, fs.ErrNotExist):
			return fmt.Errorf("inspect private file %q: %w", cleanPath, inspectErr)
		}

		file, createErr := openFile(cleanPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, PrivateFileMode)
		if errors.Is(createErr, fs.ErrExist) {
			continue
		}
		if createErr != nil {
			return fmt.Errorf("create private file %q: %w", cleanPath, createErr)
		}
		if err := file.Chmod(PrivateFileMode); err != nil {
			_ = file.Close()
			return fmt.Errorf("set new private file permissions %q: %w", cleanPath, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close new private file %q: %w", cleanPath, err)
		}
		return nil
	}
}

// PrivateModeMatches reports whether all permission and special mode bits match.
func PrivateModeMatches(current, want fs.FileMode) bool {
	return current&privateModeMask == want
}

// SetPrivateMode rechecks the final entry type and applies one planned mode repair.
// It reports whether this call actually changed the entry.
func SetPrivateMode(path string, want fs.FileMode) (bool, error) {
	cleanPath, err := cleanAbsolute(path)
	if err != nil {
		return false, err
	}
	switch want {
	case PrivateDirectoryMode:
		current, exists, err := InspectRoot(cleanPath)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, fmt.Errorf("private root %q disappeared before chmod", cleanPath)
		}
		if PrivateModeMatches(current, want) {
			return false, nil
		}
	case PrivateFileMode:
		current, exists, err := InspectPrivateFile(cleanPath)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, fmt.Errorf("private file %q disappeared before chmod", cleanPath)
		}
		if PrivateModeMatches(current, want) {
			return false, nil
		}
	default:
		return false, fmt.Errorf("unsupported private mode %04o", want)
	}
	if err := os.Chmod(cleanPath, want); err != nil {
		return false, fmt.Errorf("set private mode %04o on %q: %w", want, cleanPath, err)
	}
	return true, nil
}

// PublishPrivateFile atomically publishes changed bytes through a private
// regular file. Identical content is a strict no-op.
func PublishPrivateFile(path string, data []byte) (changed bool, err error) {
	cleanPath, err := cleanAbsolute(path)
	if err != nil {
		return false, fmt.Errorf("publish private file: %w", err)
	}
	return publishPrivateFile(cleanPath, data, func(path string) (pendingPrivateFile, error) {
		return renameio.NewPendingFile(path)
	})
}

func publishPrivateFile(
	cleanPath string,
	data []byte,
	newPending pendingPrivateFileFactory,
) (changed bool, err error) {
	parent := filepath.Dir(cleanPath)
	if err := ValidateRoot(parent); err != nil {
		return false, err
	}
	if err := ValidatePrivateFile(cleanPath); err != nil {
		return false, err
	}

	current, readErr := os.ReadFile(cleanPath)
	switch {
	case readErr == nil && bytes.Equal(current, data):
		return false, nil
	case readErr != nil && !errors.Is(readErr, fs.ErrNotExist):
		return false, fmt.Errorf("read existing private file %q: %w", cleanPath, readErr)
	}

	if err := EnsureRoot(parent); err != nil {
		return false, err
	}
	pending, err := newPending(cleanPath)
	if err != nil {
		return false, fmt.Errorf("create temporary private file for %q: %w", cleanPath, err)
	}
	defer func() {
		if cleanupErr := pending.Cleanup(); cleanupErr != nil {
			err = errors.Join(
				err,
				fmt.Errorf(
					"clean up temporary private file for %q: %w",
					cleanPath,
					cleanupErr,
				),
			)
		}
	}()
	if err := pending.Chmod(PrivateFileMode); err != nil {
		return false, fmt.Errorf(
			"set temporary private file permissions for %q: %w",
			cleanPath,
			err,
		)
	}
	if _, err := pending.Write(data); err != nil {
		return false, fmt.Errorf("write temporary private file for %q: %w", cleanPath, err)
	}
	if err := pending.CloseAtomicallyReplace(); err != nil {
		return false, fmt.Errorf("publish private file %q: %w", cleanPath, err)
	}
	return true, nil
}

func cleanAbsolute(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be a non-empty absolute path")
	}
	return filepath.Clean(path), nil
}
