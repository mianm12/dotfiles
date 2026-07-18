package state

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/paths"
)

// LoadStatus 区分只读加载是否取得有效 v1 state，或确认文件尚不存在。
type LoadStatus uint8

const (
	// StatusInvalid 是错误返回时的零值；调用方不得消费对应 Snapshot。
	StatusInvalid LoadStatus = iota
	// StatusMissing 表示 state 文件确认缺失，是合法的全新状态。
	StatusMissing
	// StatusLoaded 表示返回的 Snapshot 已完整通过 v1 持久格式校验。
	StatusLoaded
)

// Load 从绝对路径只读加载 state。确认缺失返回 StatusMissing 且 error 为 nil；
// dangling symlink、权限及其他读取错误不得伪装成缺失或持久格式损坏。
func Load(path string) (Snapshot, LoadStatus, error) {
	if path == "" || !filepath.IsAbs(path) {
		return Snapshot{}, StatusInvalid, fmt.Errorf("state path %q must be a non-empty absolute path", path)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if paths.IsMissing(path, err) {
			return Snapshot{}, StatusMissing, nil
		}
		return Snapshot{}, StatusInvalid, fmt.Errorf("read state %q: %w", path, err)
	}
	snapshot, err := Decode(data)
	if err != nil {
		return Snapshot{}, StatusInvalid, fmt.Errorf("load state %q: %w", path, err)
	}
	return snapshot, StatusLoaded, nil
}
