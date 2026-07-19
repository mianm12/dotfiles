package state

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/storage"
)

type storeFile interface {
	Name() string
	Chmod(fs.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type storeOperations struct {
	createTemp func(string, string) (storeFile, error)
	rename     func(string, string) error
	remove     func(string) error
}

// Store 把有效 Snapshot 原子发布到 state path。
// 调用方必须已经完成 control-plane preflight 并持有 mutation lock。
func Store(root, path string, snapshot Snapshot) error {
	return store(root, path, snapshot, defaultStoreOperations())
}

func store(root, path string, snapshot Snapshot, operations storeOperations) error {
	cleanRoot, cleanPath, err := cleanStorePair(root, path)
	if err != nil {
		return err
	}
	data, err := Encode(snapshot)
	if err != nil {
		return fmt.Errorf("prepare state store: %w", err)
	}
	if err := storage.EnsureRoot(cleanRoot); err != nil {
		return fmt.Errorf("prepare state store root: %w", err)
	}
	if err := validateStateDestination(cleanPath); err != nil {
		return err
	}

	file, err := operations.createTemp(cleanRoot, "."+filepath.Base(cleanPath)+"-")
	if err != nil {
		return fmt.Errorf("create state temporary file for %q: %w", cleanPath, err)
	}
	fail := func(primary error, closed bool) error {
		return cleanupStoreFailure(primary, file, closed, operations.remove)
	}
	if err := file.Chmod(storage.PrivateFileMode); err != nil {
		return fail(fmt.Errorf("set state temporary file permissions: %w", err), false)
	}
	written, err := file.Write(data)
	if err != nil {
		return fail(fmt.Errorf("write state temporary file: %w", err), false)
	}
	if written != len(data) {
		return fail(fmt.Errorf("write state temporary file: %w", io.ErrShortWrite), false)
	}
	if err := file.Sync(); err != nil {
		return fail(fmt.Errorf("sync state temporary file: %w", err), false)
	}
	if err := file.Close(); err != nil {
		return fail(fmt.Errorf("close state temporary file: %w", err), true)
	}
	if err := operations.rename(file.Name(), cleanPath); err != nil {
		return fail(fmt.Errorf("publish state %q: %w", cleanPath, err), true)
	}
	return nil
}

func defaultStoreOperations() storeOperations {
	return storeOperations{
		createTemp: func(dir, pattern string) (storeFile, error) {
			return os.CreateTemp(dir, pattern)
		},
		rename: os.Rename,
		remove: os.Remove,
	}
}

func cleanStorePair(root, path string) (string, string, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", "", fmt.Errorf("state root must be a non-empty absolute path")
	}
	if path == "" || !filepath.IsAbs(path) {
		return "", "", fmt.Errorf("state path must be a non-empty absolute path")
	}
	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)
	if filepath.Dir(cleanPath) != cleanRoot {
		return "", "", fmt.Errorf("state path %q must be directly inside root %q", cleanPath, cleanRoot)
	}
	return cleanRoot, cleanPath, nil
}

func validateStateDestination(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect state destination %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("state destination %q is not a regular file", path)
	}
	return nil
}

func cleanupStoreFailure(primary error, file storeFile, closed bool, remove func(string) error) error {
	errorsToReport := []error{primary}
	if !closed {
		if err := file.Close(); err != nil {
			errorsToReport = append(errorsToReport, fmt.Errorf("close failed state temporary file: %w", err))
		}
	}
	if err := remove(file.Name()); err != nil {
		errorsToReport = append(errorsToReport, fmt.Errorf("remove failed state temporary file: %w", err))
	}
	return errors.Join(errorsToReport...)
}
