package manifest

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// HookDescriptor 是 M1 hook planner 所需的静态、只读 manifest 事实。脚本 bytes、mode 与指纹
// 属于 hook planner 的 plan-time observation，不由 manifest 接缝读取。
type HookDescriptor struct {
	Module         string
	ModulePath     string
	Script         string
	ScriptPath     string
	TargetRoot     string
	TargetRootPath string
}

// ScopedProfile 是完整 profile 已通过全局路径校验后形成的请求 scope。entries 已在 scope 内
// fail-fast 读取；hooks 只包含相同 effective module scope。
type ScopedProfile struct {
	name    string
	goos    string
	home    string
	full    bool
	modules []string
	entries []DesiredEntry
	hooks   []HookDescriptor
}

// Name 返回 effective profile 名。
func (profile ValidatedProfile) Name() string { return profile.name }

// GOOS 返回 effective profile 的目标 GOOS。
func (profile ValidatedProfile) GOOS() string { return profile.goos }

// Home 返回完整路径校验绑定的 clean effective HOME。
func (profile ValidatedProfile) Home() string { return profile.home }

// Modules 返回当前 GOOS 上按字节序排列的 effective module 名。
func (profile ValidatedProfile) Modules() []string {
	return sortedResolvedModuleNames(profile.modules)
}

// RenderScope 只能从已通过完整 profile 路径校验的 receiver 选择 module scope。requested 为空
// 表示全量；非空请求去重排序，任一 module 不在当前 effective profile 时整体失败。
func (profile ValidatedProfile) RenderScope(
	requested []string,
	context RuntimeContext,
) (ScopedProfile, error) {
	if profile.name == "" || !isSupportedGOOS(profile.goos) {
		return ScopedProfile{}, fmt.Errorf("validated profile is invalid")
	}
	runtimeProfile := ResolvedProfile{name: profile.name, goos: profile.goos}
	home, err := runtimeProfile.validateRuntimeContext(context)
	if err != nil {
		return ScopedProfile{}, err
	}
	if home != profile.home {
		return ScopedProfile{}, fmt.Errorf(
			"runtime HOME %q does not match validated HOME %q",
			home,
			profile.home,
		)
	}
	effective := profile.Modules()
	selected, full, err := selectEffectiveModules(effective, requested)
	if err != nil {
		return ScopedProfile{}, fmt.Errorf("profile %q scope: %w", profile.name, err)
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, module := range selected {
		selectedSet[module] = struct{}{}
	}
	entries := make([]DesiredEntry, 0, len(profile.entries))
	for _, entry := range profile.entries {
		if _, ok := selectedSet[entry.Module]; ok {
			entries = append(entries, entry)
		}
	}
	loaded, err := loadScaffolds(entries)
	if err != nil {
		return ScopedProfile{}, err
	}
	return ScopedProfile{
		name:    profile.name,
		goos:    profile.goos,
		home:    profile.home,
		full:    full,
		modules: selected,
		entries: loaded,
		hooks:   scopedHookDescriptors(profile.modules, selectedSet, home),
	}, nil
}

// Name 返回 effective profile 名。
func (profile ScopedProfile) Name() string { return profile.name }

// GOOS 返回 effective profile 的目标 GOOS。
func (profile ScopedProfile) GOOS() string { return profile.goos }

// Home 返回完整路径校验和 scope 读取共用的 clean effective HOME。
func (profile ScopedProfile) Home() string { return profile.home }

// Full 报告调用方是否选择完整 effective profile，而非显式 module scope。
func (profile ScopedProfile) Full() bool { return profile.full }

// Modules 返回按字节序排列、去重后的 effective module scope。
func (profile ScopedProfile) Modules() []string {
	return append([]string(nil), profile.modules...)
}

// Entries 返回 scope 内已经读取 scaffold 字面内容的 desired 副本。
func (profile ScopedProfile) Entries() []DesiredEntry {
	return cloneDesiredEntries(profile.entries)
}

// Hooks 返回 scope 内稳定排序的 M1 hook descriptor 副本。
func (profile ScopedProfile) Hooks() []HookDescriptor {
	return append([]HookDescriptor(nil), profile.hooks...)
}

func selectEffectiveModules(effective, requested []string) ([]string, bool, error) {
	if len(requested) == 0 {
		return append([]string(nil), effective...), true, nil
	}
	effectiveSet := make(map[string]struct{}, len(effective))
	for _, module := range effective {
		effectiveSet[module] = struct{}{}
	}
	selectedSet := make(map[string]struct{}, len(requested))
	for _, module := range requested {
		if _, ok := effectiveSet[module]; !ok {
			return nil, false, fmt.Errorf("module %q is not in the effective profile", module)
		}
		selectedSet[module] = struct{}{}
	}
	selected := make([]string, 0, len(selectedSet))
	for module := range selectedSet {
		selected = append(selected, module)
	}
	slices.Sort(selected)
	return selected, false, nil
}

func sortedResolvedModuleNames(modules []ResolvedModule) []string {
	names := make([]string, len(modules))
	for index, module := range modules {
		names[index] = module.Name
	}
	slices.Sort(names)
	return names
}

func scopedHookDescriptors(
	modules []ResolvedModule,
	selected map[string]struct{},
	home string,
) []HookDescriptor {
	ordered := cloneResolvedModules(modules)
	slices.SortFunc(ordered, func(left, right ResolvedModule) int {
		return strings.Compare(left.Name, right.Name)
	})
	hooks := make([]HookDescriptor, 0)
	for _, module := range ordered {
		if _, ok := selected[module.Name]; !ok {
			continue
		}
		for _, script := range module.RunOnce {
			hooks = append(hooks, HookDescriptor{
				Module:         module.Name,
				ModulePath:     filepath.Clean(module.SourceDir),
				Script:         script,
				ScriptPath:     filepath.Join(filepath.Clean(module.SourceDir), filepath.FromSlash(script)),
				TargetRoot:     module.TargetRoot,
				TargetRootPath: expandDesiredTarget(home, module.TargetRoot),
			})
		}
	}
	return hooks
}

func cloneResolvedModules(modules []ResolvedModule) []ResolvedModule {
	cloned := append([]ResolvedModule(nil), modules...)
	for index := range cloned {
		cloned[index].Ignore = append([]string(nil), cloned[index].Ignore...)
		cloned[index].FileRules = append([]ResolvedFileRule(nil), cloned[index].FileRules...)
		cloned[index].RunOnce = append([]string(nil), cloned[index].RunOnce...)
	}
	return cloned
}
