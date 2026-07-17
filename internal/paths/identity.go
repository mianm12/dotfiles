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

// TargetIdentity 是当前文件系统拓扑中一个 target leaf 目录项位置的不透明身份。
// 零值无效；身份只用于相关拓扑未变化的一次只读快照内比较，不是持久化格式。
type TargetIdentity struct {
	root       string
	components []string
	valid      bool
}

// Equal 报告两个有效身份是否表示同一 target 目录项位置。
func (id TargetIdentity) Equal(other TargetIdentity) bool {
	return id.valid && other.valid && id.root == other.root && slices.Equal(id.components, other.components)
}

// ResolveTargetIdentity 只读解析非根绝对展示路径的 leaf target 身份。
func ResolveTargetIdentity(path string) (TargetIdentity, error) {
	resolution, err := newTargetResolver().resolve(path)
	if err != nil {
		return TargetIdentity{}, err
	}
	return resolution.identity, nil
}

func (resolver *targetResolver) resolveCleanTarget(cleanPath string) (TargetResolution, error) {
	parent := filepath.Dir(cleanPath)
	leaf := filepath.Base(cleanPath)
	resolvedParent, err := resolver.resolveTargetParent(parent)
	if err != nil {
		return TargetResolution{}, err
	}

	leafName := ""
	if !resolvedParent.hasMissing {
		leafName, err = resolver.resolveLeafName(cleanPath, resolvedParent.existingPath, leaf)
	} else {
		leafName, err = resolver.missingNameKey(resolvedParent.existingPath, leaf)
		if err != nil {
			err = fmt.Errorf("resolve missing target name %q: %w", cleanPath, err)
		}
	}
	if err != nil {
		return TargetResolution{}, err
	}
	components := append(slices.Clone(resolvedParent.components), leafName)
	identity := TargetIdentity{root: resolvedParent.root, components: components, valid: true}
	return TargetResolution{identity: identity, ancestors: resolvedParent.ancestors}, nil
}

func cleanTargetPath(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("target path %q must be a non-empty absolute path", path)
	}
	cleanPath := filepath.Clean(path)
	if filepath.Dir(cleanPath) == cleanPath {
		return "", fmt.Errorf("target path %q must not be a filesystem root", path)
	}
	return cleanPath, nil
}

func (resolver *targetResolver) resolveLeafName(path, resolvedParent, leaf string) (string, error) {
	entries, err := resolver.readDir(resolvedParent)
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
		name, resolveErr := resolver.resolveExistingEntryName(path, resolvedParent, leaf, entries)
		if resolveErr != nil {
			return "", fmt.Errorf("resolve existing target alias %q: %w", path, resolveErr)
		}
		return name, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("inspect target %q: %w", path, err)
	}
	if !IsMissing(path, err) {
		return "", fmt.Errorf("%w: target %q is not a safely reachable missing path", ErrPathBlocked, path)
	}

	key, err := resolver.missingNameKey(resolvedParent, leaf)
	if err != nil {
		return "", fmt.Errorf("resolve missing target name %q: %w", path, err)
	}
	return key, nil
}

func (resolver *targetResolver) resolveExistingEntryName(path, parent, requested string, entries []os.DirEntry) (string, error) {
	for _, entry := range entries {
		if entry.Name() == requested {
			return requested, nil
		}
	}

	requestedInfo, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect existing path %q: %w", path, err)
	}
	candidates := make([]string, 0, 1)
	for _, entry := range entries {
		candidatePath := filepath.Join(parent, entry.Name())
		candidateInfo, candidateErr := os.Lstat(candidatePath)
		if candidateErr != nil {
			return "", fmt.Errorf("inspect directory entry %q: %w", candidatePath, candidateErr)
		}
		if os.SameFile(requestedInfo, candidateInfo) {
			candidates = append(candidates, entry.Name())
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("%w: filesystem lookup for %q has no matching directory entry", ErrIdentityUnavailable, path)
	}

	requestedKey, err := resolver.missingNameKey(parent, requested)
	if err != nil {
		return "", fmt.Errorf("%w: multiple hard-link entries match alias %q: %w", ErrIdentityUnavailable, path, err)
	}
	matchingNames := make([]string, 0, 1)
	for _, candidate := range candidates {
		candidateKey, keyErr := resolver.missingNameKey(parent, candidate)
		if keyErr != nil {
			return "", fmt.Errorf("%w: cannot classify hard-link entry %q: %w", ErrIdentityUnavailable, candidate, keyErr)
		}
		if candidateKey == requestedKey {
			matchingNames = append(matchingNames, candidate)
		}
	}
	if len(matchingNames) != 1 {
		return "", fmt.Errorf("%w: alias %q matches %d hard-link directory entries", ErrIdentityUnavailable, path, len(matchingNames))
	}
	return matchingNames[0], nil
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
