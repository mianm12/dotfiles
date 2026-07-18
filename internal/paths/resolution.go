package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
)

// TargetResolution 保存 target leaf 身份及路径解析实际经过的祖先拓扑。
// 零值无效；文件系统发生 mutation 后必须重新解析，不能继续比较旧值。
type TargetResolution struct {
	identity  TargetIdentity
	ancestors []TargetIdentity
}

// Identity 返回 resolution 的 leaf target 身份；零值 resolution 返回无效身份。
func (resolution TargetResolution) Identity() TargetIdentity {
	return resolution.identity
}

// Equal 报告两个有效 resolution 是否表示同一 leaf target。
func (resolution TargetResolution) Equal(other TargetResolution) bool {
	return resolution.identity.Equal(other.identity)
}

// IsAncestorOf 报告 resolution 的 leaf 是否是 other 展示路径经过的严格祖先。
func (resolution TargetResolution) IsAncestorOf(other TargetResolution) bool {
	if resolution.Equal(other) {
		return false
	}
	for _, ancestor := range other.ancestors {
		if resolution.identity.Equal(ancestor) {
			return true
		}
	}
	return false
}

type nameKeyRequest struct {
	parent string
	name   string
}

type nameKeyResult struct {
	key string
	err error
}

// targetResolver 的缓存只服务一次公开解析调用，不跨文件系统快照复用。
type targetResolver struct {
	directories map[string][]os.DirEntry
	nameKeys    map[nameKeyRequest]nameKeyResult
}

func newTargetResolver() *targetResolver {
	return &targetResolver{
		directories: make(map[string][]os.DirEntry),
		nameKeys:    make(map[nameKeyRequest]nameKeyResult),
	}
}

// ResolveTarget 只读解析非根绝对展示路径的 leaf 身份与祖先拓扑。
func ResolveTarget(path string) (TargetResolution, error) {
	return newTargetResolver().resolve(path)
}

func (resolver *targetResolver) resolve(path string) (TargetResolution, error) {
	cleanPath, err := cleanTargetPath(path)
	if err != nil {
		return TargetResolution{}, err
	}
	return resolver.resolveCleanTarget(cleanPath)
}

type resolvedTargetParent struct {
	existingPath string
	root         string
	components   []string
	ancestors    []TargetIdentity
	hasMissing   bool
}

func (resolver *targetResolver) resolveTargetParent(path string) (resolvedTargetParent, error) {
	// 向上收集 missing tail；最近的现存对象负责锚定 canonical parent 和名称语义。
	current := filepath.Clean(path)
	missing := make([]string, 0)
	for {
		_, err := os.Lstat(current)
		if err == nil {
			canonical, ancestors, resolveErr := resolver.resolveExistingDirectory(current)
			if resolveErr != nil {
				return resolvedTargetParent{}, fmt.Errorf("resolve target ancestor %q: %w", current, resolveErr)
			}

			slices.Reverse(missing)
			root, components := splitAbsolutePath(canonical)
			for _, missingComponent := range missing {
				key, keyErr := resolver.missingNameKey(canonical, missingComponent)
				if keyErr != nil {
					return resolvedTargetParent{}, fmt.Errorf(
						"resolve missing target parent component %q below %q: %w",
						missingComponent,
						canonical,
						keyErr,
					)
				}
				components = append(components, key)
				ancestors = appendUniqueIdentity(ancestors, newTargetIdentity(root, components))
			}
			return resolvedTargetParent{
				existingPath: canonical,
				root:         root,
				components:   components,
				ancestors:    ancestors,
				hasMissing:   len(missing) > 0,
			}, nil
		}

		if errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP) {
			return resolvedTargetParent{}, fmt.Errorf("%w: target ancestor %q is blocked", ErrPathBlocked, current)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return resolvedTargetParent{}, fmt.Errorf("inspect target ancestor %q: %w", current, err)
		}
		if !IsMissing(current, err) {
			return resolvedTargetParent{}, fmt.Errorf("%w: target ancestor %q is not safely missing", ErrPathBlocked, current)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return resolvedTargetParent{}, fmt.Errorf("%w: no reachable directory for target parent %q", ErrPathBlocked, path)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// 与 filepath.EvalSymlinks 保持同一上限，避免递归 link 无限展开。
const maxSymlinkTraversals = 255

type resolvedExistingPath struct {
	canonical  string
	resolution TargetResolution
	info       fs.FileInfo
}

// resolveExistingDirectory 逐组件解析既有目录，并把目录自身加入调用方 target 的祖先拓扑。
func (resolver *targetResolver) resolveExistingDirectory(path string) (string, []TargetIdentity, error) {
	resolved, err := resolver.resolveExistingPath(path)
	if err != nil {
		return "", nil, err
	}
	if !resolved.info.IsDir() {
		return "", nil, fmt.Errorf("%w: target ancestor %q is not a directory", ErrPathBlocked, path)
	}
	ancestors := appendUniqueIdentity(resolved.resolution.ancestors, resolved.resolution.identity)
	return resolved.canonical, ancestors, nil
}

// resolveExistingPath 跟随完整展示路径（包含 leaf symlink），返回最终消费位置与完整遍历轨迹。
// walker 负责记录 trace；只有内核 Stat 能权威判定整条路径实际可达。
func (resolver *targetResolver) resolveExistingPath(path string) (resolvedExistingPath, error) {
	resolvedInfo, err := os.Stat(path)
	if err != nil {
		if isTraversalBlocker(err) {
			return resolvedExistingPath{}, fmt.Errorf("%w: path %q cannot resolve to an existing object", ErrPathBlocked, path)
		}
		return resolvedExistingPath{}, fmt.Errorf("inspect existing path %q: %w", path, err)
	}

	root, pending := splitTraversalPath(path)
	currentRoot := root
	currentComponents := make([]string, 0, len(pending))
	ancestors := make([]TargetIdentity, 0, len(pending)+1)
	rootIdentity := newTargetIdentity(root, nil)
	if len(pending) > 0 {
		ancestors = append(ancestors, rootIdentity)
	}
	symlinks := 0

	for len(pending) > 0 {
		component := pending[0]
		pending = pending[1:]
		switch component {
		case ".":
			continue
		case "..":
			if len(currentComponents) > 0 {
				currentComponents = currentComponents[:len(currentComponents)-1]
			}
			continue
		}

		parent := joinIdentityPath(currentRoot, currentComponents)
		actual, info, err := resolver.resolveTraversedEntry(parent, component)
		if err != nil {
			if isTraversalBlocker(err) {
				return resolvedExistingPath{}, fmt.Errorf("%w: path component %q is not traversable", ErrPathBlocked, filepath.Join(parent, component))
			}
			return resolvedExistingPath{}, fmt.Errorf("inspect path component %q: %w", filepath.Join(parent, component), err)
		}

		entryComponents := append(slices.Clone(currentComponents), actual)
		entryIdentity := newTargetIdentity(currentRoot, entryComponents)
		entryPath := filepath.Join(parent, actual)
		if info.Mode()&fs.ModeSymlink != 0 {
			ancestors = appendUniqueIdentity(ancestors, entryIdentity)
			symlinks++
			if symlinks > maxSymlinkTraversals {
				return resolvedExistingPath{}, fmt.Errorf("%w: too many symlinks while resolving path %q", ErrPathBlocked, path)
			}
			link, readErr := os.Readlink(entryPath)
			if readErr != nil {
				return resolvedExistingPath{}, fmt.Errorf("read path symlink %q: %w", entryPath, readErr)
			}
			linkRoot, linkComponents := splitTraversalPath(link)
			if filepath.IsAbs(link) {
				currentRoot = linkRoot
				currentComponents = nil
				ancestors = appendUniqueIdentity(ancestors, newTargetIdentity(linkRoot, nil))
			}
			pending = append(linkComponents, pending...)
			continue
		}
		if len(pending) == 0 {
			return resolvedExistingPath{
				canonical:  joinIdentityPath(currentRoot, entryComponents),
				resolution: TargetResolution{identity: entryIdentity, ancestors: ancestors},
				info:       resolvedInfo,
			}, nil
		}
		if !info.IsDir() {
			return resolvedExistingPath{}, fmt.Errorf("%w: path component %q is not a directory", ErrPathBlocked, entryPath)
		}
		ancestors = appendUniqueIdentity(ancestors, entryIdentity)
		currentComponents = entryComponents
	}

	return resolvedExistingPath{
		canonical:  joinIdentityPath(currentRoot, currentComponents),
		resolution: TargetResolution{identity: newTargetIdentity(currentRoot, currentComponents), ancestors: ancestors},
		info:       resolvedInfo,
	}, nil
}

func isTraversalBlocker(err error) bool {
	return errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, syscall.ENOTDIR) ||
		errors.Is(err, syscall.ELOOP)
}

func (resolver *targetResolver) resolveTraversedEntry(parent, requested string) (string, fs.FileInfo, error) {
	entries, err := resolver.readDir(parent)
	if err != nil {
		return "", nil, fmt.Errorf("read ancestor directory %q: %w", parent, err)
	}
	requestedPath := filepath.Join(parent, requested)
	actual, err := resolver.resolveExistingEntryName(requestedPath, parent, requested, entries)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Lstat(filepath.Join(parent, actual))
	if err != nil {
		return "", nil, err
	}
	return actual, info, nil
}

func (resolver *targetResolver) readDir(path string) ([]os.DirEntry, error) {
	cleanPath := filepath.Clean(path)
	if entries, ok := resolver.directories[cleanPath]; ok {
		return entries, nil
	}
	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return nil, err
	}
	resolver.directories[cleanPath] = entries
	return entries, nil
}

func (resolver *targetResolver) missingNameKey(parent, name string) (string, error) {
	request := nameKeyRequest{parent: filepath.Clean(parent), name: name}
	if result, ok := resolver.nameKeys[request]; ok {
		return result.key, result.err
	}
	key, err := missingNameKey(request.parent, request.name)
	resolver.nameKeys[request] = nameKeyResult{key: key, err: err}
	return key, err
}

func newTargetIdentity(root string, components []string) TargetIdentity {
	return TargetIdentity{root: root, components: slices.Clone(components), valid: true}
}

func appendUniqueIdentity(identities []TargetIdentity, candidate TargetIdentity) []TargetIdentity {
	for _, identity := range identities {
		if identity.Equal(candidate) {
			return identities
		}
	}
	return append(identities, candidate)
}

func splitTraversalPath(path string) (string, []string) {
	volume := filepath.VolumeName(path)
	rest := path[len(volume):]
	root := ""
	if len(rest) > 0 && os.IsPathSeparator(rest[0]) {
		root = volume + string(filepath.Separator)
	}

	// 不能先 filepath.Clean：link target 中 X/.. 仍实际要求 X 可遍历。
	components := make([]string, 0)
	for start := 0; start < len(rest); {
		for start < len(rest) && os.IsPathSeparator(rest[start]) {
			start++
		}
		end := start
		for end < len(rest) && !os.IsPathSeparator(rest[end]) {
			end++
		}
		if start < end {
			components = append(components, rest[start:end])
		}
		start = end
	}
	return root, components
}

func joinIdentityPath(root string, components []string) string {
	path := root
	for _, component := range components {
		path = filepath.Join(path, component)
	}
	return path
}
