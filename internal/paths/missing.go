package paths

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// IsMissing 报告 operationErr 中的 ErrNotExist 是否确实源于路径缺失。
// 路径本身或祖先中存在悬空 symlink，或无法确认缺失时返回 false。
func IsMissing(path string, operationErr error) bool {
	if !errors.Is(operationErr, fs.ErrNotExist) {
		return false
	}

	current := filepath.Clean(path)
	atRequestedPath := true
	// 向上寻找最近的既有对象；只有可到达目录下缺少后续分量才算正常缺失。
	for {
		info, err := os.Lstat(current)
		if err == nil {
			// 原操作之后路径已经出现时仍报告原错误，避免把一次读取竞争误报为正常缺失。
			if atRequestedPath {
				return false
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				info, err = os.Stat(current)
				if err != nil {
					return false
				}
			}
			return info.IsDir()
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false
		}

		parent := filepath.Dir(current)
		if parent == current {
			return true
		}
		current = parent
		atRequestedPath = false
	}
}
