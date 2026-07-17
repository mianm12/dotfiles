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
	path    string
	ignored bool
}

func enumerateModuleSources(module ResolvedModule) ([]moduleSource, error) {
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

	objects := make(map[string]fs.FileInfo)
	sources := make([]moduleSource, 0)
	if err := walkModuleTree(module, patterns, hookPaths, objects, &sources); err != nil {
		return nil, err
	}
	if err := validateSourceReferences(module, objects); err != nil {
		return nil, err
	}
	slices.SortFunc(sources, func(left, right moduleSource) int {
		return strings.Compare(left.path, right.path)
	})
	return sources, nil
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

	var walk func(directory, relative string) error
	walk = func(directory, relative string) error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return fmt.Errorf("module %q read source directory %q: %w", module.Name, directory, err)
		}
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name())
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
