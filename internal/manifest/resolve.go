package manifest

import (
	"fmt"
	"slices"
	"strings"
)

const defaultScaffoldMode = "0644"

// ResolvedProfile 表示一个 profile 在指定 GOOS 上的有效 manifest 配置。
type ResolvedProfile struct {
	// Name 是被解析的 profile 名。
	Name string
	// Modules 是经过 OS 过滤、按模块名字节序排列的有效模块。
	Modules  []ResolvedModule
	goos     string
	dataKeys []string
}

// ResolvedModule 表示已经应用 defaults、ignore 合并和 OS 过滤的模块配置。
type ResolvedModule struct {
	// Name 是模块目录名。
	Name string
	// SourceDir 是模块文件树根目录。
	SourceDir string
	// TargetRoot 是尚未展开 HOME 的有效 target root。
	TargetRoot string
	// Ignore 按全局、模块顺序保存去重后的有效 ignore pattern。
	Ignore []string
	// FileRules 是按 source 排序的显式 [files] 规则，不是枚举后的完整文件集合。
	FileRules []ResolvedFileRule
	// RunOnce 按 manifest 声明顺序保存规范化的 hook script 路径。
	RunOnce []string
}

// ResolvedFileRule 表示模块 manifest 中一条已经应用 M1 缺省的显式 [files] 规则。
type ResolvedFileRule struct {
	// Source 是规范化的模块相对路径。
	Source string
	// Kind 是显式覆盖或文件名后缀推导出的有效文件行为。
	Kind FileKind
	// Mode 是 scaffold 的有效 mode；link 不管理 mode，因此返回空字符串。
	Mode string
	// TargetOverride 是显式完整 entry target；未声明时返回空字符串。
	TargetOverride string
}

// Resolve 返回 profile 在 goos 上的有效模块；goos 只接受 darwin 或 linux。
// 返回值不展开 HOME，也不枚举模块文件树。
func (r Repository) Resolve(profile, goos string) (ResolvedProfile, error) {
	if !isSupportedGOOS(goos) {
		return ResolvedProfile{}, fmt.Errorf("unsupported GOOS %q: want %s or %s", goos, goosDarwin, goosLinux)
	}
	moduleNames, exists := r.expandedProfiles[profile]
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
		Name:     profile,
		Modules:  modules,
		goos:     goos,
		dataKeys: sortedKeys(r.manifest.data),
	}, nil
}

func (r Repository) resolveModule(name string, loaded loadedModule, goos string) (ResolvedModule, bool, error) {
	// os 与 target 分别按“内建缺省 → 顶层 defaults → 模块”整键替换，不做逐项合并。
	operatingSystems := []string{goosDarwin, goosLinux}
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

	fileRules, err := resolveFileRules(name, loaded.manifest.files, targetRoot)
	if err != nil {
		return ResolvedModule{}, false, err
	}

	return ResolvedModule{
		Name:       name,
		SourceDir:  loaded.root,
		TargetRoot: targetRoot,
		Ignore:     mergeIgnore(r.manifest.ignore, loaded.manifest.ignore),
		FileRules:  fileRules,
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

func resolveFileRules(module string, rules map[string]fileSpec, targetRoot string) ([]ResolvedFileRule, error) {
	sources := make([]string, 0, len(rules))
	for source := range rules {
		sources = append(sources, source)
	}
	slices.Sort(sources)

	resolvedRules := make([]ResolvedFileRule, 0, len(sources))
	for _, source := range sources {
		rule := rules[source]
		mode := ""
		if rule.kind == FileKindScaffold {
			mode = defaultScaffoldMode
		}
		if rule.mode != nil {
			mode = *rule.mode
		}
		target := ""
		if rule.target != nil {
			target = *rule.target
			if !isLexicalTargetDescendant(targetRoot, target) {
				return nil, fmt.Errorf(
					"module %q file %q target %q must be a true descendant of target root %q",
					module,
					source,
					target,
					targetRoot,
				)
			}
		}
		resolvedRules = append(resolvedRules, ResolvedFileRule{
			Source:         source,
			Kind:           rule.kind,
			Mode:           mode,
			TargetOverride: target,
		})
	}
	return resolvedRules, nil
}

// isLexicalTargetDescendant 只接收 validateTargetPath 校验后的 target，比较其字面层级关系。
// 它不解析文件系统身份，不能用于 HOME 展开后的控制面或所有权边界。
func isLexicalTargetDescendant(root, target string) bool {
	if root == "~" {
		return strings.HasPrefix(target, "~/")
	}
	return strings.HasPrefix(target, root+"/")
}

func mergeIgnore(global, module []string) []string {
	// ignore 是唯一的并集合并项；按全局、模块声明顺序保留每个 pattern 的首次出现。
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
