package manifest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// moduleSource 是通过 Lstat 确认的普通 source 文件。ignored 只表示命中用户 ignore；
// [files] 是否覆盖该结果由 desired 定级阶段决定。
type moduleSource struct {
	path               string
	ignored            bool
	prospectiveContent []byte
	prospective        bool
}

func enumerateModuleSources(module ResolvedModule) ([]moduleSource, error) {
	return enumerateModuleSourcesProspective(module, nil)
}

func enumerateModuleSourcesProspective(
	module ResolvedModule,
	prospective map[string]prospectiveSourceData,
) ([]moduleSource, error) {
	patterns := make([]ignorePattern, 0, len(module.Ignore))
	for _, raw := range module.Ignore {
		pattern, err := parseIgnorePattern(raw)
		if err != nil {
			return nil, fmt.Errorf("module %q ignore pattern %q: %w", module.Name, raw, err)
		}
		patterns = append(patterns, pattern)
	}

	hookPaths := make(map[string]struct{}, len(module.RunOnce))
	for _, script := range module.RunOnce {
		hookPaths[script] = struct{}{}
	}

	// 保留包括内置/用户 ignore 在内的全部已遍历对象；显式文件与 hook 引用仍需据此
	// 校验存在性、对象类型和文件系统身份，不能只看最终 desired sources。
	objects := make(map[string]fs.FileInfo)
	sources := make([]moduleSource, 0)
	if err := walkModuleTree(module, patterns, hookPaths, objects, &sources); err != nil {
		return nil, err
	}
	for _, source := range sortedKeys(prospective) {
		candidate := prospective[source]
		if _, exists := objects[source]; exists {
			return nil, fmt.Errorf("module %q prospective source %q already exists", module.Name, source)
		}
		if reason := builtInIgnoreReason(source, false, hookPaths); reason != "" {
			return nil, fmt.Errorf(
				"module %q prospective source %q is excluded by built-in ignore: %s",
				module.Name,
				source,
				reason,
			)
		}
		if err := validateProspectiveAncestors(module, source, objects); err != nil {
			return nil, err
		}
		objects[source] = prospectiveFileInfo{name: filepath.Base(source), mode: candidate.mode, size: int64(len(candidate.content))}
		sources = append(sources, moduleSource{
			path:               source,
			ignored:            matchesAnyIgnore(patterns, source, false),
			prospectiveContent: append([]byte(nil), candidate.content...),
			prospective:        true,
		})
	}
	if err := validateSourceReferences(module, objects); err != nil {
		return nil, err
	}
	slices.SortFunc(sources, func(left, right moduleSource) int {
		return strings.Compare(left.path, right.path)
	})
	return sources, nil
}

func validateProspectiveAncestors(module ResolvedModule, source string, objects map[string]fs.FileInfo) error {
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(source)))
	if parent == "." {
		return nil
	}
	parts := strings.Split(parent, "/")
	for index := range parts {
		ancestor := strings.Join(parts[:index+1], "/")
		info, exists := objects[ancestor]
		if !exists {
			continue
		}
		if !info.IsDir() {
			return fmt.Errorf(
				"module %q prospective source %q has non-directory ancestor %q",
				module.Name,
				source,
				ancestor,
			)
		}
	}
	return nil
}

func walkModuleTree(
	module ResolvedModule,
	patterns []ignorePattern,
	hookPaths map[string]struct{},
	objects map[string]fs.FileInfo,
	sources *[]moduleSource,
) error {
	rootInfo, err := os.Lstat(module.SourceDir)
	if err != nil {
		return fmt.Errorf("module %q inspect source root %q: %w", module.Name, module.SourceDir, err)
	}
	if rootInfo.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("module %q source root %q is a symlink", module.Name, module.SourceDir)
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("module %q source root %q is not a directory", module.Name, module.SourceDir)
	}

	// 不能按 ignore 对目录剪枝：[files] 可以覆盖用户 ignore，且任何层级出现 symlink
	// 或特殊文件都必须报错，即使该路径最终不会进入 desired。
	var walk func(directory, relative string) error
	walk = func(directory, relative string) error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return fmt.Errorf("module %q read source directory %q: %w", module.Name, directory, err)
		}
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name())
			// 显式 Lstat 每个目录项，既不依赖 DirEntry 缓存的类型信息，也绝不跟随 symlink。
			info, err := os.Lstat(path)
			if err != nil {
				return fmt.Errorf("module %q inspect source %q: %w", module.Name, path, err)
			}
			source := entry.Name()
			if relative != "" {
				source = relative + "/" + entry.Name()
			}
			objects[source] = info

			switch {
			case info.Mode()&fs.ModeSymlink != 0:
				return fmt.Errorf("module %q source %q is a symlink", module.Name, source)
			case info.IsDir():
				if err := walk(path, source); err != nil {
					return err
				}
			case info.Mode().IsRegular():
				if isBuiltInIgnored(source, false, hookPaths) {
					continue
				}
				*sources = append(*sources, moduleSource{
					path:    source,
					ignored: matchesAnyIgnore(patterns, source, false),
				})
			default:
				return fmt.Errorf("module %q source %q is a special file (%s)", module.Name, source, info.Mode().Type())
			}
		}
		return nil
	}

	return walk(module.SourceDir, "")
}

func validateSourceReferences(module ResolvedModule, objects map[string]fs.FileInfo) error {
	for _, rule := range module.FileRules {
		if err := validateSourceReference(module.Name, "files", rule.Source, objects); err != nil {
			return err
		}
	}
	for _, script := range module.RunOnce {
		if err := validateSourceReference(module.Name, "hook", script, objects); err != nil {
			return err
		}
	}
	// 路径规范化只能排除词法重复；所有引用已经确认是普通文件后，再用 SameFile
	// 捕获 hard link 等不同路径指向同一文件系统对象的情况。
	for index, script := range module.RunOnce {
		for previous := 0; previous < index; previous++ {
			if os.SameFile(objects[script], objects[module.RunOnce[previous]]) {
				return fmt.Errorf(
					"module %q hook source %q duplicates filesystem identity of %q",
					module.Name,
					script,
					module.RunOnce[previous],
				)
			}
		}
	}
	return nil
}

func validateSourceReference(module, kind, source string, objects map[string]fs.FileInfo) error {
	info, exists := objects[source]
	if !exists {
		return fmt.Errorf("module %q %s references missing source %q", module, kind, source)
	}
	mode := info.Mode()
	if mode.IsDir() {
		return fmt.Errorf("module %q %s references directory source %q", module, kind, source)
	}
	if !mode.IsRegular() {
		return fmt.Errorf("module %q %s references non-regular source %q", module, kind, source)
	}
	return nil
}

func isBuiltInIgnored(source string, isDir bool, hookPaths map[string]struct{}) bool {
	return builtInIgnoreReason(source, isDir, hookPaths) != ""
}

func matchesAnyIgnore(patterns []ignorePattern, source string, isDir bool) bool {
	for _, pattern := range patterns {
		if pattern.matches(source, isDir) {
			return true
		}
	}
	return false
}
