package manifest

import (
	"fmt"
	"slices"
	"strings"
)

const defaultScaffoldMode = "0644"

// ResolvedProfile 表示一个 profile 在指定 GOOS 上的有效 manifest 配置。
type ResolvedProfile struct {
	Name              string
	Modules           []ResolvedModule
	UnassignedModules []string
}

// ResolvedModule 表示已经应用 defaults、ignore 合并和 OS 过滤的模块配置。
type ResolvedModule struct {
	Name       string
	SourceDir  string
	TargetRoot string
	Ignore     []string
	Files      []ResolvedFile
	RunOnce    []string
}

// ResolvedFile 表示模块 manifest 中一条稳定排序的显式文件声明。
type ResolvedFile struct {
	Source         string
	Kind           FileKind
	Mode           string
	TargetOverride string
}

// Resolve 返回 profile 在 goos 上的有效模块；goos 只接受 darwin 或 linux。
// 返回值不展开 HOME，也不枚举模块文件树。
func (r Repository) Resolve(profile, goos string) (ResolvedProfile, error) {
	if goos != "darwin" && goos != "linux" {
		return ResolvedProfile{}, fmt.Errorf("unsupported GOOS %q: want darwin or linux", goos)
	}
	moduleNames, exists := r.profiles[profile]
	if !exists {
		return ResolvedProfile{}, fmt.Errorf("unknown profile %q", profile)
	}

	modules := make([]ResolvedModule, 0, len(moduleNames))
	for _, name := range moduleNames {
		loaded := r.modules[name]
		resolved, active, err := r.resolveModule(name, loaded, goos)
		if err != nil {
			return ResolvedProfile{}, err
		}
		if active {
			modules = append(modules, resolved)
		}
	}
	return ResolvedProfile{
		Name:              profile,
		Modules:           modules,
		UnassignedModules: append([]string(nil), r.unassigned...),
	}, nil
}

func (r Repository) resolveModule(name string, loaded loadedModule, goos string) (ResolvedModule, bool, error) {
	operatingSystems := []string{"darwin", "linux"}
	if r.manifest.defaults.os.set {
		operatingSystems = r.manifest.defaults.os.value
	}
	if loaded.manifest.os.set {
		operatingSystems = loaded.manifest.os.value
	}
	if !slices.Contains(operatingSystems, goos) {
		return ResolvedModule{}, false, nil
	}

	target := targetSpec{common: stringPointer("~")}
	if r.manifest.defaults.target.set {
		target = r.manifest.defaults.target.value
	}
	if loaded.manifest.target.set {
		target = loaded.manifest.target.value
	}
	targetRoot, exists := target.forOS(goos)
	if !exists {
		return ResolvedModule{}, false, fmt.Errorf("module %q is active on %s but target table has no %s entry", name, goos, goos)
	}

	files, err := resolveFiles(name, loaded.manifest.files, targetRoot)
	if err != nil {
		return ResolvedModule{}, false, err
	}

	return ResolvedModule{
		Name:       name,
		SourceDir:  loaded.root,
		TargetRoot: targetRoot,
		Ignore:     mergeIgnore(r.manifest.ignore, loaded.manifest.ignore),
		Files:      files,
		RunOnce:    append([]string(nil), loaded.manifest.runOnce...),
	}, true, nil
}

func (t targetSpec) forOS(goos string) (string, bool) {
	if t.common != nil {
		return *t.common, true
	}
	value, exists := t.byOS[goos]
	return value, exists
}

func resolveFiles(module string, files map[string]fileSpec, targetRoot string) ([]ResolvedFile, error) {
	sources := make([]string, 0, len(files))
	for source := range files {
		sources = append(sources, source)
	}
	slices.Sort(sources)

	resolved := make([]ResolvedFile, 0, len(sources))
	for _, source := range sources {
		file := files[source]
		mode := ""
		if file.kind == FileKindScaffold {
			mode = defaultScaffoldMode
		}
		if file.mode != nil {
			mode = *file.mode
		}
		target := ""
		if file.target != nil {
			target = *file.target
			if !isTargetDescendant(targetRoot, target) {
				return nil, fmt.Errorf(
					"module %q file %q target %q must be a true descendant of target root %q",
					module,
					source,
					target,
					targetRoot,
				)
			}
		}
		resolved = append(resolved, ResolvedFile{
			Source:         source,
			Kind:           file.kind,
			Mode:           mode,
			TargetOverride: target,
		})
	}
	return resolved, nil
}

func isTargetDescendant(root, target string) bool {
	if root == "~" {
		return strings.HasPrefix(target, "~/")
	}
	return strings.HasPrefix(target, root+"/")
}

func mergeIgnore(global, module []string) []string {
	merged := make([]string, 0, len(global)+len(module))
	seen := make(map[string]struct{}, len(global)+len(module))
	for _, patterns := range [][]string{global, module} {
		for _, pattern := range patterns {
			if _, exists := seen[pattern]; exists {
				continue
			}
			seen[pattern] = struct{}{}
			merged = append(merged, pattern)
		}
	}
	return merged
}

func stringPointer(value string) *string {
	return &value
}
