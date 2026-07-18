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
	// PrivateDirectoryMode 是 state 与 backup 等私有目录的规范权限。
	PrivateDirectoryMode fs.FileMode = 0o700
	// PrivateFileMode 是 state、lock 等私有控制文件的规范权限。
	PrivateFileMode fs.FileMode = 0o600
)

// EnsureRoot 建立私有 state 家族根目录，并把现存目录权限收敛为 0700。
func EnsureRoot(path string) error {
	cleanPath, err := cleanAbsolute(path)
	if err != nil {
		return fmt.Errorf("state root: %w", err)
	}
	if err := os.MkdirAll(cleanPath, PrivateDirectoryMode); err != nil {
		return fmt.Errorf("create state root %q: %w", cleanPath, err)
	}
	info, err := os.Stat(cleanPath)
	if err != nil {
		return fmt.Errorf("inspect state root %q: %w", cleanPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("state root %q is not a directory", cleanPath)
	}
	if err := os.Chmod(cleanPath, PrivateDirectoryMode); err != nil {
		return fmt.Errorf("set state root permissions %q: %w", cleanPath, err)
	}
	return nil
}

// EnsurePrivateFile 建立私有普通文件，并把现存普通文件权限收敛为 0600。
// 调用方必须先建立并校验父目录。
func EnsurePrivateFile(path string) error {
	cleanPath, err := cleanAbsolute(path)
	if err != nil {
		return fmt.Errorf("private file: %w", err)
	}

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

		file, createErr := os.OpenFile(cleanPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, PrivateFileMode)
		if errors.Is(createErr, fs.ErrExist) {
			continue
		}
		if createErr != nil {
			return fmt.Errorf("create private file %q: %w", cleanPath, createErr)
		}
		if err := file.Chmod(PrivateFileMode); err != nil {
			_ = file.Close()
			_ = os.Remove(cleanPath)
			return fmt.Errorf("set new private file permissions %q: %w", cleanPath, err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(cleanPath)
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
