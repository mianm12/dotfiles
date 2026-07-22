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

// PublishResult 保存 publisher 是否实际改变配置，以及是否已经越过不可逆发布点。
// 等价 no-op 不 changed，但 committed；发布后的 cleanup error 仍保留 committed 事实。
type PublishResult struct {
	changed   bool
	committed bool
}

// Changed 报告配置对象或字节是否实际变化。
func (result PublishResult) Changed() bool { return result.changed }

// Committed 报告 candidate 是否已成为 config path 上的已提交事实。
func (result PublishResult) Committed() bool { return result.committed }

// Publish 在最终 rename 前复核 candidate 绑定的 Precondition，并以 0600 原子发布。
// 等价 candidate 返回 committed=true、changed=false，不发生临时文件或 rename。
func Publish(path string, candidate Candidate) (PublishResult, error) {
	return publish(path, candidate, defaultPublishOperations())
}

// PublishWithRemover 执行与 Publish 相同的完整发布流程，只替换 temporary remove 操作。
// 它供 internal orchestration 确定性验证发布后 cleanup failure，不修改全局依赖。
func PublishWithRemover(
	path string,
	candidate Candidate,
	remove func(string) error,
) (PublishResult, error) {
	if remove == nil {
		return PublishResult{}, fmt.Errorf("machine config temporary remover is required")
	}
	operations := defaultPublishOperations()
	operations.remove = remove
	return publish(path, candidate, operations)
}

type publishFile interface {
	Name() string
	Chmod(fs.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type publishOperations struct {
	mkdirAll   func(string, fs.FileMode) error
	createTemp func(string, string) (publishFile, error)
	rename     func(string, string) error
	link       func(string, string) error
	remove     func(string) error
}

func defaultPublishOperations() publishOperations {
	return publishOperations{
		mkdirAll: os.MkdirAll,
		createTemp: func(directory, pattern string) (publishFile, error) {
			return os.CreateTemp(directory, pattern)
		},
		rename: os.Rename,
		link:   os.Link,
		remove: os.Remove,
	}
}

func publish(path string, candidate Candidate, operations publishOperations) (PublishResult, error) {
	if path == "" || !filepath.IsAbs(path) {
		return PublishResult{}, fmt.Errorf("machine config path must be a non-empty absolute path")
	}
	if !candidate.valid || !candidate.precondition.valid {
		return PublishResult{}, fmt.Errorf("machine config candidate is invalid")
	}
	cleanPath := filepath.Clean(path)
	current, err := currentPrecondition(cleanPath, candidate.precondition)
	if err != nil {
		return PublishResult{}, err
	}
	if !samePrecondition(current, candidate.precondition) {
		return PublishResult{}, ErrPreconditionChanged
	}
	if current.exists && current.mode == storage.PrivateFileMode && bytes.Equal(current.bytes, candidate.bytes) {
		return PublishResult{committed: true}, nil
	}

	directory := filepath.Dir(cleanPath)
	if operations.mkdirAll == nil {
		return PublishResult{}, fmt.Errorf("machine config directory creator is nil")
	}
	if err := operations.mkdirAll(directory, storage.PrivateDirectoryMode); err != nil {
		return PublishResult{}, fmt.Errorf("prepare machine config directory %q: %w", directory, err)
	}
	if operations.createTemp == nil {
		return PublishResult{}, fmt.Errorf("machine config temporary creator is nil")
	}
	file, err := operations.createTemp(directory, "."+filepath.Base(cleanPath)+"-")
	if err != nil {
		return PublishResult{}, fmt.Errorf("create machine config temporary file for %q: %w", cleanPath, err)
	}
	closed := false
	fail := func(primary error) (PublishResult, error) {
		errs := []error{primary}
		if !closed {
			if closeErr := file.Close(); closeErr != nil {
				errs = append(errs, fmt.Errorf("close failed machine config temporary file: %w", closeErr))
			}
			closed = true
		}
		if operations.remove == nil {
			errs = append(errs, fmt.Errorf("machine config temporary remover is nil"))
		} else if removeErr := operations.remove(file.Name()); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove failed machine config temporary file: %w", removeErr))
		}
		return PublishResult{}, errors.Join(errs...)
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
	if candidate.precondition.exists {
		if operations.rename == nil {
			return fail(fmt.Errorf("machine config replacement publisher is nil"))
		}
		if err := operations.rename(file.Name(), cleanPath); err != nil {
			return fail(fmt.Errorf("replace machine config %q: %w", cleanPath, err))
		}
		return PublishResult{changed: true, committed: true}, nil
	}
	if operations.link == nil {
		return fail(fmt.Errorf("machine config no-replace publisher is nil"))
	}
	if err := operations.link(file.Name(), cleanPath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fail(fmt.Errorf("%w: machine config %q appeared before publish", ErrPreconditionChanged, cleanPath))
		}
		return fail(fmt.Errorf("publish new machine config %q: %w", cleanPath, err))
	}
	if operations.remove == nil {
		return PublishResult{changed: true, committed: true}, fmt.Errorf("machine config %q was published but temporary remover is nil", cleanPath)
	}
	if err := operations.remove(file.Name()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return PublishResult{changed: true, committed: true}, fmt.Errorf("machine config %q was published but remove temporary file %q failed: %w", cleanPath, file.Name(), err)
	}
	return PublishResult{changed: true, committed: true}, nil
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
		return Precondition{}, fmt.Errorf("%w: reread current machine config %q: %w", ErrPreconditionChanged, path, err)
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
