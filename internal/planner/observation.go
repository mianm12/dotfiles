package planner

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/paths"
)

// ObserveTarget 只读分类绝对 target leaf。它使用 Lstat 区分 leaf symlink，并只保存原始链接
// 文本，绝不跟随 symlink leaf；regular 只返回 kind/mode，不读取内容。
func ObserveTarget(path string) (Observation, error) {
	return observeTarget(path, false)
}

// ObserveTargetWithDigest 在 ObserveTarget 的基础上为 regular leaf 流式计算 sha256 digest。
// 非 regular leaf 与 ObserveTarget 行为相同。
func ObserveTargetWithDigest(path string) (Observation, error) {
	return observeTarget(path, true)
}

func observeTarget(path string, regularDigest bool) (Observation, error) {
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
		observed := Observation{Kind: ObjectRegular, Mode: mode}
		if !regularDigest {
			return observed, nil
		}
		file, err := os.Open(cleanPath)
		if err != nil {
			return Observation{}, fmt.Errorf("open target file %q for digest: %w", cleanPath, err)
		}
		digest := sha256.New()
		if _, err := io.Copy(digest, file); err != nil {
			_ = file.Close()
			return Observation{}, fmt.Errorf("hash target file %q: %w", cleanPath, err)
		}
		if err := file.Close(); err != nil {
			return Observation{}, fmt.Errorf("close target file %q after digest: %w", cleanPath, err)
		}
		observed.Hash = fmt.Sprintf("sha256:%x", digest.Sum(nil))
		return observed, nil
	case mode.IsDir():
		return Observation{Kind: ObjectDirectory, Mode: mode}, nil
	default:
		return Observation{Kind: ObjectSpecial, Mode: mode}, nil
	}
}
