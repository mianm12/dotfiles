// Package lock 提供 dot mutation 使用的进程间非阻塞排他锁。
package lock

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/mianm12/dotfiles/internal/storage"
)

var (
	// ErrBusy 表示另一进程已经持有同一个 dot mutation 锁。
	ErrBusy = errors.New("another dot process is running")
	// ErrIO 表示准备、获取或释放进程锁时发生文件系统错误。
	ErrIO = errors.New("process lock I/O failure")
	// ErrOwnership 表示锁所有权无效或已经释放。
	ErrOwnership = errors.New("invalid lock ownership")
)

// Ownership 表示一次 mutation 周期持有的排他锁所有权。
// 值副本共享同一次所有权且只能成功释放一次；零值无效。
type Ownership struct {
	state *ownershipState
}

type ownershipState struct {
	mu       sync.Mutex
	backend  backend
	path     string
	released bool
}

// Acquire 建立 state root 与 lock 文件，并尝试立即取得进程间排他锁。
// state root 与 lock 路径必须是绝对路径，且 lock 必须直接位于 state root 内。
func Acquire(root, path string) (*Ownership, error) {
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
	return newOwnership(fileLock, cleanPath), nil
}

// Validate read-only validates the lock root and existing lock entry.
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

func newOwnership(fileLock backend, path string) *Ownership {
	return &Ownership{state: &ownershipState{
		backend: fileLock,
		path:    path,
	}}
}

// Release 释放锁；同一次所有权只能成功释放一次。
func (owner *Ownership) Release() error {
	if owner == nil || owner.state == nil {
		return ErrOwnership
	}
	state := owner.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.released || state.backend == nil {
		return ErrOwnership
	}
	if err := state.backend.Unlock(); err != nil {
		return fmt.Errorf("%w: release process lock %q: %w", ErrIO, state.path, err)
	}
	state.released = true
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
		return "", "", fmt.Errorf("process lock path %q must be directly inside root %q", cleanPath, cleanRoot)
	}
	return cleanRoot, cleanPath, nil
}
