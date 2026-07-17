package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
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
	resolvedParent, missingParents, err := resolveTargetParent(parent)
	if err != nil {
		return TargetIdentity{}, err
	}

	root, components := splitAbsolutePath(resolvedParent)
	for _, missingParent := range missingParents {
		key, keyErr := missingNameKey(resolvedParent, missingParent)
		if keyErr != nil {
			return TargetIdentity{}, fmt.Errorf(
				"resolve missing target parent component %q below %q: %w",
				missingParent,
				resolvedParent,
				keyErr,
			)
		}
		components = append(components, key)
	}

	leafName := ""
	if len(missingParents) == 0 {
		leafName, err = resolveLeafName(cleanPath, resolvedParent, leaf)
	} else {
		leafName, err = missingNameKey(resolvedParent, leaf)
		if err != nil {
			err = fmt.Errorf("resolve missing target name %q: %w", cleanPath, err)
		}
	}
	if err != nil {
		return TargetIdentity{}, err
	}
	components = append(components, leafName)
	return TargetIdentity{root: root, components: components, valid: true}, nil
}

func resolveTargetParent(path string) (string, []string, error) {
	current := filepath.Clean(path)
	missing := make([]string, 0)
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolvedInfo, statErr := os.Stat(current)
			if statErr != nil {
				if errors.Is(statErr, fs.ErrNotExist) || errors.Is(statErr, syscall.ELOOP) {
					return "", nil, fmt.Errorf("%w: target ancestor %q cannot resolve to a directory", ErrPathBlocked, current)
				}
				return "", nil, fmt.Errorf("inspect target ancestor %q: %w", current, statErr)
			}
			if !resolvedInfo.IsDir() {
				return "", nil, fmt.Errorf("%w: target ancestor %q is not a directory", ErrPathBlocked, current)
			}
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				if errors.Is(resolveErr, fs.ErrNotExist) || errors.Is(resolveErr, syscall.ELOOP) {
					return "", nil, fmt.Errorf("%w: target ancestor %q cannot resolve to a directory", ErrPathBlocked, current)
				}
				return "", nil, fmt.Errorf("resolve target ancestor %q: %w", current, resolveErr)
			}
			slices.Reverse(missing)
			return resolved, missing, nil
		}

		if errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP) {
			return "", nil, fmt.Errorf("%w: target ancestor %q is blocked", ErrPathBlocked, current)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", nil, fmt.Errorf("inspect target ancestor %q: %w", current, err)
		}
		if !IsMissing(current, err) {
			return "", nil, fmt.Errorf("%w: target ancestor %q is not safely missing", ErrPathBlocked, current)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", nil, fmt.Errorf("%w: no reachable directory for target parent %q", ErrPathBlocked, path)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
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
