// Package storage 维护 state 家族目录和文件的共同权限边界。
package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	// PrivateDirectoryMode 是 state 等私有目录的规范权限。
	PrivateDirectoryMode fs.FileMode = 0o700
	// PrivateFileMode 是 state、lock 等私有控制文件的规范权限。
	PrivateFileMode fs.FileMode = 0o600
)

type privateFile interface {
	Chmod(fs.FileMode) error
	Close() error
}

type privateFileOpener func(string, int, fs.FileMode) (privateFile, error)

// ValidateRoot 只读校验私有控制根目录。目录尚不存在时也通过，由 mutation
// 在需要写入时调用 EnsureRoot 建立。
func ValidateRoot(path string) error {
	cleanPath, err := cleanAbsolute(path)
	if err != nil {
		return fmt.Errorf("private root: %w", err)
	}
	_, err = inspectRoot(cleanPath)
	return err
}

// ValidatePrivateFile read-only validates an existing private file entry.
// A missing path is valid because mutation may create it later.
func ValidatePrivateFile(path string) error {
	cleanPath, err := cleanAbsolute(path)
	if err != nil {
		return fmt.Errorf("private file: %w", err)
	}
	info, err := os.Lstat(cleanPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect private file %q: %w", cleanPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("private file %q is not a regular file", cleanPath)
	}
	return nil
}

// EnsureRoot 建立私有控制根目录，并把现存目录权限收敛为 0700。
// 最终对象必须是真实目录；更高层的 ancestor symlink 仍然合法。
func EnsureRoot(path string) error {
	cleanPath, err := cleanAbsolute(path)
	if err != nil {
		return fmt.Errorf("private root: %w", err)
	}

	exists, err := inspectRoot(cleanPath)
	if err != nil {
		return err
	}
	if !exists {
		if err := os.MkdirAll(cleanPath, PrivateDirectoryMode); err != nil {
			return fmt.Errorf("create private root %q: %w", cleanPath, err)
		}
		if _, err := inspectRoot(cleanPath); err != nil {
			return err
		}
	}
	if err := os.Chmod(cleanPath, PrivateDirectoryMode); err != nil {
		return fmt.Errorf("set private root permissions %q: %w", cleanPath, err)
	}
	return nil
}

func inspectRoot(cleanPath string) (bool, error) {
	info, err := os.Lstat(cleanPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect private root %q: %w", cleanPath, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return false, fmt.Errorf(
			"private root %q is a symbolic link; expected a directory",
			cleanPath,
		)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("private root %q is not a directory", cleanPath)
	}
	return true, nil
}

// EnsurePrivateFile 建立私有普通文件，并把现存普通文件权限收敛为 0600。
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
			if err := os.Chmod(cleanPath, PrivateFileMode); err != nil {
				return fmt.Errorf("set private file permissions %q: %w", cleanPath, err)
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

func cleanAbsolute(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be a non-empty absolute path")
	}
	return filepath.Clean(path), nil
}
