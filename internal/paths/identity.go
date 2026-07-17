package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var (
	// ErrPathBlocked 表示 target 的现存祖先不能作为目录使用。
	ErrPathBlocked = errors.New("target path is blocked")
	// ErrIdentityUnavailable 表示当前文件系统语义无法在只读条件下可靠建立 target 身份。
	ErrIdentityUnavailable = errors.New("target identity is unavailable")
)

// TargetIdentity 是当前文件系统拓扑中一个 target 目录项位置的不透明身份。
// 零值无效；身份只用于当前进程内比较，不是持久化格式。
type TargetIdentity struct {
	root       string
	components []string
	valid      bool
}

// Equal 报告两个有效身份是否表示同一 target 目录项位置。
func (id TargetIdentity) Equal(other TargetIdentity) bool {
	return id.valid && other.valid && id.root == other.root && slices.Equal(id.components, other.components)
}

// IsAncestorOf 报告 id 是否是 other 的严格祖先。
func (id TargetIdentity) IsAncestorOf(other TargetIdentity) bool {
	if !id.valid || !other.valid || id.root != other.root || len(id.components) >= len(other.components) {
		return false
	}
	return slices.Equal(id.components, other.components[:len(id.components)])
}

// ResolveTargetIdentity 只读解析绝对展示路径在当前文件系统中的 target 身份。
func ResolveTargetIdentity(path string) (TargetIdentity, error) {
	if path == "" || !filepath.IsAbs(path) {
		return TargetIdentity{}, fmt.Errorf("target path %q must be a non-empty absolute path", path)
	}

	cleanPath := filepath.Clean(path)
	parent := filepath.Dir(cleanPath)
	leaf := filepath.Base(cleanPath)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return TargetIdentity{}, fmt.Errorf("resolve target parent %q: %w", parent, err)
	}
	parentInfo, err := os.Stat(resolvedParent)
	if err != nil {
		return TargetIdentity{}, fmt.Errorf("inspect target parent %q: %w", resolvedParent, err)
	}
	if !parentInfo.IsDir() {
		return TargetIdentity{}, fmt.Errorf("%w: target parent %q is not a directory", ErrPathBlocked, resolvedParent)
	}

	leafName, err := resolveLeafName(cleanPath, resolvedParent, leaf)
	if err != nil {
		return TargetIdentity{}, err
	}
	root, components := splitAbsolutePath(filepath.Join(resolvedParent, leafName))
	return TargetIdentity{root: root, components: components, valid: true}, nil
}

func resolveLeafName(path, resolvedParent, leaf string) (string, error) {
	entries, err := os.ReadDir(resolvedParent)
	if err != nil {
		return "", fmt.Errorf("read target parent %q: %w", resolvedParent, err)
	}
	for _, entry := range entries {
		if entry.Name() == leaf {
			return leaf, nil
		}
	}

	_, err = os.Lstat(path)
	if err == nil {
		return "", fmt.Errorf("%w: target %q uses an unclassified filesystem alias", ErrIdentityUnavailable, path)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("inspect target %q: %w", path, err)
	}
	if !IsMissing(path, err) {
		return "", fmt.Errorf("%w: target %q is not a safely reachable missing path", ErrPathBlocked, path)
	}

	key, err := missingNameKey(resolvedParent, leaf)
	if err != nil {
		return "", fmt.Errorf("resolve missing target name %q: %w", path, err)
	}
	return key, nil
}

func splitAbsolutePath(path string) (string, []string) {
	cleanPath := filepath.Clean(path)
	volume := filepath.VolumeName(cleanPath)
	rest := strings.TrimPrefix(cleanPath[len(volume):], string(filepath.Separator))
	root := volume + string(filepath.Separator)
	if rest == "" {
		return root, nil
	}
	return root, strings.Split(rest, string(filepath.Separator))
}

func asciiFold(name string) (string, bool) {
	bytes := []byte(name)
	for i, value := range bytes {
		if value >= 0x80 {
			return "", false
		}
		if value >= 'A' && value <= 'Z' {
			bytes[i] = value + ('a' - 'A')
		}
	}
	return string(bytes), true
}
