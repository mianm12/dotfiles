package planner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/paths"
)

// ObserveTarget 只读分类绝对 target leaf。它使用 Lstat 区分 leaf symlink，并只保存原始链接
// 文本，绝不跟随 symlink leaf；regular 只返回 kind/mode，不读取内容。
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
		return Observation{Kind: ObjectRegular, Mode: mode}, nil
	case mode.IsDir():
		return Observation{Kind: ObjectDirectory, Mode: mode}, nil
	default:
		return Observation{Kind: ObjectSpecial, Mode: mode}, nil
	}
}
