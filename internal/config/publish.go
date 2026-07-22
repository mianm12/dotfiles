package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/storage"
)

// ErrPreconditionChanged 表示 config preparation 依据的 kind、bytes 或 mode 已失效。
var ErrPreconditionChanged = errors.New("machine config precondition changed")

// Publish 在最终 rename 前复核 candidate 绑定的 Precondition，并以 0600 原子发布。
// changed 为 false 表示当前 bytes 与 0600 mode 已等价，不发生临时文件或 rename。
func Publish(path string, candidate Candidate) (changed bool, err error) {
	return publish(path, candidate, publishOperations{rename: os.Rename})
}

type publishOperations struct {
	rename func(string, string) error
}

func publish(path string, candidate Candidate, operations publishOperations) (changed bool, err error) {
	if path == "" || !filepath.IsAbs(path) {
		return false, fmt.Errorf("machine config path must be a non-empty absolute path")
	}
	if !candidate.valid || !candidate.precondition.valid {
		return false, fmt.Errorf("machine config candidate is invalid")
	}
	cleanPath := filepath.Clean(path)
	current, err := currentPrecondition(cleanPath, candidate.precondition)
	if err != nil {
		return false, err
	}
	if !samePrecondition(current, candidate.precondition) {
		return false, ErrPreconditionChanged
	}
	if current.exists && current.mode == storage.PrivateFileMode && bytes.Equal(current.bytes, candidate.bytes) {
		return false, nil
	}

	directory := filepath.Dir(cleanPath)
	if err := os.MkdirAll(directory, storage.PrivateDirectoryMode); err != nil {
		return false, fmt.Errorf("prepare machine config directory %q: %w", directory, err)
	}
	file, err := os.CreateTemp(directory, "."+filepath.Base(cleanPath)+"-")
	if err != nil {
		return false, fmt.Errorf("create machine config temporary file for %q: %w", cleanPath, err)
	}
	closed := false
	fail := func(primary error) (bool, error) {
		errs := []error{primary}
		if !closed {
			if closeErr := file.Close(); closeErr != nil {
				errs = append(errs, fmt.Errorf("close failed machine config temporary file: %w", closeErr))
			}
			closed = true
		}
		if removeErr := os.Remove(file.Name()); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove failed machine config temporary file: %w", removeErr))
		}
		return false, errors.Join(errs...)
	}
	if err := file.Chmod(storage.PrivateFileMode); err != nil {
		return fail(fmt.Errorf("set machine config temporary file permissions: %w", err))
	}
	written, err := file.Write(candidate.bytes)
	if err != nil {
		return fail(fmt.Errorf("write machine config temporary file: %w", err))
	}
	if written != len(candidate.bytes) {
		return fail(fmt.Errorf("write machine config temporary file: %w", io.ErrShortWrite))
	}
	if err := file.Sync(); err != nil {
		return fail(fmt.Errorf("sync machine config temporary file: %w", err))
	}
	if err := file.Close(); err != nil {
		closed = true
		return fail(fmt.Errorf("close machine config temporary file: %w", err))
	}
	closed = true

	current, err = currentPrecondition(cleanPath, candidate.precondition)
	if err != nil {
		return fail(err)
	}
	if !samePrecondition(current, candidate.precondition) {
		return fail(ErrPreconditionChanged)
	}
	if operations.rename == nil {
		return fail(fmt.Errorf("machine config publisher is nil"))
	}
	if err := operations.rename(file.Name(), cleanPath); err != nil {
		return fail(fmt.Errorf("publish machine config %q: %w", cleanPath, err))
	}
	return true, nil
}

func currentPrecondition(path string, expected Precondition) (Precondition, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if paths.IsMissing(path, err) {
			return Precondition{valid: true}, nil
		}
		return Precondition{}, fmt.Errorf("inspect current machine config %q: %w", path, err)
	}
	if !expected.exists || info.Mode().Type() != expected.kind {
		return Precondition{valid: true, exists: true, kind: info.Mode().Type(), mode: info.Mode().Perm()}, nil
	}
	snapshot, err := LoadSnapshot(path)
	if err != nil {
		return Precondition{}, fmt.Errorf("%w: reread current machine config %q: %v", ErrPreconditionChanged, path, err)
	}
	return snapshot.precondition, nil
}

func samePrecondition(left, right Precondition) bool {
	return left.valid && right.valid &&
		left.exists == right.exists &&
		left.kind == right.kind &&
		left.mode == right.mode &&
		bytes.Equal(left.bytes, right.bytes)
}
