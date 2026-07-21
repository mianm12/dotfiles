// Package backup 持久保存 force replace 前的普通文件与 symlink 快照。
package backup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mianm12/dotfiles/internal/storage"
)

const randomSuffixBytes = 8

// Batch 是一次 apply 使用的唯一私有备份目录。
type Batch struct {
	path string
}

// NewBatch 建立 backup root 和不会覆盖既有内容的唯一 batch。
func NewBatch(root string) (*Batch, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("backup root must be a non-empty absolute path")
	}
	cleanRoot := filepath.Clean(root)
	existingAncestor, err := nearestExistingDirectory(cleanRoot)
	if err != nil {
		return nil, err
	}
	if err := storage.EnsureRoot(cleanRoot); err != nil {
		return nil, fmt.Errorf("prepare backup root: %w", err)
	}

	for {
		name, err := batchName()
		if err != nil {
			return nil, err
		}
		path := filepath.Join(cleanRoot, name)
		if err := os.Mkdir(path, storage.PrivateDirectoryMode); err != nil {
			if os.IsExist(err) {
				continue
			}
			return nil, fmt.Errorf("create backup batch %q: %w", path, err)
		}
		if err := os.Chmod(path, storage.PrivateDirectoryMode); err != nil {
			return nil, fmt.Errorf("set backup batch permissions %q: %w", path, err)
		}
		if err := syncDirectoryChain(path, existingAncestor); err != nil {
			if removeErr := os.Remove(path); removeErr != nil {
				return nil, errors.Join(
					fmt.Errorf("persist backup batch %q: %w", path, err),
					fmt.Errorf("remove incomplete backup batch %q: %w", path, removeErr),
				)
			}
			return nil, fmt.Errorf("persist backup batch %q: %w", path, err)
		}
		return &Batch{path: path}, nil
	}
}

func nearestExistingDirectory(path string) (string, error) {
	for candidate := path; ; candidate = filepath.Dir(candidate) {
		info, err := os.Stat(candidate)
		switch {
		case err == nil:
			if !info.IsDir() {
				return "", fmt.Errorf("backup ancestor %q is not a directory", candidate)
			}
			return candidate, nil
		case !errors.Is(err, os.ErrNotExist):
			return "", fmt.Errorf("inspect backup ancestor %q: %w", candidate, err)
		}
		if filepath.Dir(candidate) == candidate {
			return "", fmt.Errorf("backup root %q has no existing directory ancestor", path)
		}
	}
}

// Path 返回本 batch 的绝对路径，供调用方报告精确备份位置。
func (batch *Batch) Path() string {
	if batch == nil {
		return ""
	}
	return batch.path
}

func batchName() (string, error) {
	random := make([]byte, randomSuffixBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate backup batch suffix: %w", err)
	}
	return time.Now().UTC().Format(time.RFC3339Nano) + "-" + hex.EncodeToString(random), nil
}
