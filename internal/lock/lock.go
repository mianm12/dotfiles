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
	// ErrOwnership 表示嵌套 guard 不属于活动 owner，或路径与 owner 不一致。
	ErrOwnership = errors.New("invalid lock ownership")
)

// Ownership 表示一次 mutation 周期持有的排他锁所有权。
// 调用方必须显式传递它来复用嵌套流程，零值无效。
type Ownership struct {
	mu           sync.Mutex
	backend      backend
	root         string
	path         string
	references   int
	rootReleased bool
}

// Guard 表示从 Ownership 复用的一份嵌套所有权引用。
type Guard struct {
	owner    *Ownership
	released bool
}

// Acquire 建立 state root 与 lock 文件，并尝试立即取得进程间排他锁。
// state root 与 lock 路径必须是绝对路径，且 lock 必须直接位于 state root 内。
func Acquire(root, path string) (*Ownership, error) {
	cleanRoot, cleanPath, err := cleanPair(root, path)
	if err != nil {
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
	return &Ownership{
		backend:    fileLock,
		root:       cleanRoot,
		path:       cleanPath,
		references: 1,
	}, nil
}

// Reuse 为同一 root/path 的嵌套流程创建 guard，不再次获取 OS lock。
func (owner *Ownership) Reuse(root, path string) (*Guard, error) {
	if owner == nil {
		return nil, ErrOwnership
	}
	cleanRoot, cleanPath, err := cleanPair(root, path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOwnership, err)
	}

	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.rootReleased || owner.references < 1 || owner.backend == nil {
		return nil, ErrOwnership
	}
	if cleanRoot != owner.root || cleanPath != owner.path {
		return nil, fmt.Errorf("%w: owner is bound to %q and %q", ErrOwnership, owner.root, owner.path)
	}
	owner.references++
	return &Guard{owner: owner}, nil
}

// Release 释放外层 owner 的引用；仍有嵌套 guard 时不会提前解除 OS lock。
func (owner *Ownership) Release() error {
	if owner == nil {
		return ErrOwnership
	}
	return owner.release(&owner.rootReleased)
}

// Release 释放一份嵌套引用；只有最后一份所有权释放时才解除 OS lock。
func (guard *Guard) Release() error {
	if guard == nil || guard.owner == nil {
		return ErrOwnership
	}
	return guard.owner.release(&guard.released)
}

func (owner *Ownership) release(released *bool) error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if *released || owner.references < 1 || owner.backend == nil {
		return ErrOwnership
	}
	if owner.references > 1 {
		*released = true
		owner.references--
		return nil
	}
	if err := owner.backend.Unlock(); err != nil {
		return fmt.Errorf("%w: release process lock %q: %w", ErrIO, owner.path, err)
	}
	*released = true
	owner.references = 0
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
