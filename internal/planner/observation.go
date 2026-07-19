package planner

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/paths"
)

// ObserveTarget 只读观测绝对 target leaf。它使用 Lstat 区分 leaf symlink，并只保存原始链接
// 文本，绝不跟随 leaf 到目标对象。
func ObserveTarget(path string) (Observation, error) {
	if path == "" || !filepath.IsAbs(path) {
		return Observation{}, fmt.Errorf("target path %q must be a non-empty absolute path", path)
	}
	cleanPath := filepath.Clean(path)
	info, err := os.Lstat(cleanPath)
	if err != nil {
		if paths.IsMissing(cleanPath, err) {
			return Observation{Kind: ObjectMissing}, nil
		}
		return Observation{}, fmt.Errorf("inspect target %q: %w", cleanPath, err)
	}

	mode := info.Mode()
	switch {
	case mode&fs.ModeSymlink != 0:
		destination, err := os.Readlink(cleanPath)
		if err != nil {
			return Observation{}, fmt.Errorf("read target symlink %q: %w", cleanPath, err)
		}
		return Observation{Kind: ObjectSymlink, Mode: mode, LinkDest: destination}, nil
	case mode.IsRegular():
		content, err := os.ReadFile(cleanPath)
		if err != nil {
			return Observation{}, fmt.Errorf("read target file %q: %w", cleanPath, err)
		}
		digest := sha256.Sum256(content)
		return Observation{
			Kind:    ObjectRegular,
			Mode:    mode,
			Content: content,
			Hash:    fmt.Sprintf("sha256:%x", digest),
		}, nil
	case mode.IsDir():
		return Observation{Kind: ObjectDirectory, Mode: mode}, nil
	default:
		return Observation{Kind: ObjectSpecial, Mode: mode}, nil
	}
}
