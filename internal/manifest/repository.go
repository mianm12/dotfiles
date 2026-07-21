package manifest

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mianm12/dotfiles/internal/paths"
)

var manifestNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var tomlBareKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidModuleName 报告 name 是否满足 manifest module 单路径段语法。
func ValidModuleName(name string) bool { return manifestNamePattern.MatchString(name) }

// Repository 表示已经严格加载、但尚未按 profile 和 OS 解析的仓库 manifest。
type Repository struct {
	manifest         rootSpec
	modules          map[string]loadedModule
	moduleNames      []string
	expandedProfiles map[string][]string
	profileNames     []string
	unassigned       []string
}

type loadedModule struct {
	root     string
	manifest moduleSpec
}

// Load 严格读取根 manifest 与所有模块 manifest；缺少 modules 目录表示仓库没有模块。
// repo 的解析不属于 Load 职责，调用方应传入路径层已经解析的绝对路径。
// Load 只读取仓库，不创建、删除或修改任何文件。
func Load(repo string) (Repository, error) {
	info, err := os.Stat(repo)
	if err != nil {
		if paths.IsMissing(repo, err) {
			return Repository{}, ErrRepositoryUnavailable
		}
		return Repository{}, fmt.Errorf("inspect repository %q: %w", repo, err)
	}
	if !info.IsDir() {
		return Repository{}, fmt.Errorf("repository path %q is not a directory", repo)
	}

	rootManifest, err := decodeRootManifest(filepath.Join(repo, filename))
	if err != nil {
		return Repository{}, err
	}
	modules, moduleNames, err := loadModules(filepath.Join(repo, "modules"))
	if err != nil {
		return Repository{}, err
	}
	expandedProfiles, profileNames, unassigned, err := expandProfiles(
		rootManifest.declaredProfiles,
		modules,
		moduleNames,
	)
	if err != nil {
		return Repository{}, fmt.Errorf("manifest %q: %w", filepath.Join(repo, filename), err)
	}

	return Repository{
		manifest:         rootManifest,
		modules:          modules,
		moduleNames:      moduleNames,
		expandedProfiles: expandedProfiles,
		profileNames:     profileNames,
		unassigned:       unassigned,
	}, nil
}

// Requirement 返回根 manifest 声明的最低 CLI 版本约束。
func (r Repository) Requirement() Requirement {
	return r.manifest.requirement
}

// ModuleNames 返回按字节序排列的全部已发现模块名。
func (r Repository) ModuleNames() []string {
	return append([]string(nil), r.moduleNames...)
}

// ProfileNames 返回按字节序排列的全部 profile 名。
func (r Repository) ProfileNames() []string {
	return append([]string(nil), r.profileNames...)
}

// ProfileLineWithModule 返回可直接放入 [profiles] 的确切声明行。
// 它保留严格解码后的直接成员顺序和 @profile 引用，只在缺少时追加 module。
func (r Repository) ProfileLineWithModule(profile, module string) (string, error) {
	members, exists := r.manifest.declaredProfiles[profile]
	if !exists {
		return "", fmt.Errorf("unknown profile %q", profile)
	}
	if !ValidModuleName(module) {
		return "", fmt.Errorf("invalid module name %q", module)
	}
	result := append([]string(nil), members...)
	if !slices.Contains(result, module) {
		result = append(result, module)
	}
	quoted := make([]string, len(result))
	for index, member := range result {
		quoted[index] = strconv.Quote(member)
	}
	return fmt.Sprintf("%s = [%s]", formatTOMLKey(profile), strings.Join(quoted, ", ")), nil
}

// ModuleActivation 描述一个已发现 module 相对 profile 与当前 GOOS 的只读激活事实。
// TargetLines 仅在 effective target 是 OS table 时保存可直接复用的既有 TOML 成员行。
type ModuleActivation struct {
	InProfile    bool
	ManifestPath string
	OSLine       string
	TargetReady  bool
	TargetLines  []string
}

// ModuleActivationGuidance 返回 module-local 手工激活需要的稳定事实，不修改 manifest。
func (r Repository) ModuleActivationGuidance(profile, module, goos string) (ModuleActivation, error) {
	profileModules, exists := r.expandedProfiles[profile]
	if !exists {
		return ModuleActivation{}, fmt.Errorf("unknown profile %q", profile)
	}
	loaded, exists := r.modules[module]
	if !exists {
		return ModuleActivation{}, fmt.Errorf("unknown module %q", module)
	}
	if !isSupportedGOOS(goos) {
		return ModuleActivation{}, fmt.Errorf("unsupported GOOS %q: want %s or %s", goos, goosDarwin, goosLinux)
	}
	operatingSystems := r.moduleOperatingSystems(loaded)
	if !slices.Contains(operatingSystems, goos) {
		operatingSystems = append(operatingSystems, goos)
	}
	quoted := make([]string, len(operatingSystems))
	for index, value := range operatingSystems {
		quoted[index] = strconv.Quote(value)
	}
	target := r.moduleTarget(loaded)
	_, targetReady := target.forOS(goos)
	var targetLines []string
	if len(target.byOS) > 0 {
		targetLines = make([]string, 0, len(target.byOS))
		for _, targetGOOS := range sortedKeys(target.byOS) {
			targetLines = append(targetLines, fmt.Sprintf("%s = %s", targetGOOS, strconv.Quote(target.byOS[targetGOOS])))
		}
	}
	return ModuleActivation{
		InProfile:    slices.Contains(profileModules, module),
		ManifestPath: path.Join("modules", module, filename),
		OSLine:       "os = [" + strings.Join(quoted, ", ") + "]",
		TargetReady:  targetReady,
		TargetLines:  targetLines,
	}, nil
}

func formatTOMLKey(key string) string {
	if tomlBareKeyPattern.MatchString(key) {
		return key
	}
	return strconv.Quote(key)
}

// DataKeys 返回根 manifest 声明的用户 data key，结果按字节序排列。
func (r Repository) DataKeys() []string {
	return sortedKeys(r.manifest.data)
}

// ValidateTemplates 静态检查仓库中每个有效 scaffold 的语法、函数与变量引用。
// 检查不受 profile 或 OS 过滤影响，因此也覆盖 unassigned 与当前 OS 不活跃的模块；
// 它不需要运行 data，不渲染模板，也不读取 target。
func (r Repository) ValidateTemplates() error {
	entries := make([]DesiredEntry, 0)
	for _, name := range r.moduleNames {
		loaded := r.modules[name]
		// doctor 不经 Resolve，以免 profile 或 OS 过滤漏掉模板；这里只构造 source
		// 定级需要的字段，target 与运行上下文不参与静态检查。
		module := ResolvedModule{
			Name:      name,
			SourceDir: loaded.root,
			Ignore:    mergeIgnore(r.manifest.ignore, loaded.manifest.ignore),
			FileRules: materializeFileRules(loaded.manifest.files),
			RunOnce:   append([]string(nil), loaded.manifest.runOnce...),
		}
		moduleEntries, err := enumerateModuleScaffolds(module)
		if err != nil {
			return err
		}
		entries = append(entries, moduleEntries...)
	}
	return validateScaffolds(entries, r.DataKeys())
}

// ValidateModuleRules 在指定 GOOS 上检查全部模块的 effective 局部规则，不形成跨模块 target
// 集合。它覆盖未被当前或任何 profile 选择的模块；全局 target 不变量仍由各 profile 的
// ValidatePathBoundaries 单独负责。
func (r Repository) ValidateModuleRules(goos string) error {
	if !isSupportedGOOS(goos) {
		return fmt.Errorf("unsupported GOOS %q: want %s or %s", goos, goosDarwin, goosLinux)
	}
	for _, name := range r.moduleNames {
		if _, _, err := r.resolveModule(name, r.modules[name], goos); err != nil {
			return err
		}
	}
	return nil
}

// UnassignedModules 返回未被任何 profile 引用的模块名，结果按字节序排列。
func (r Repository) UnassignedModules() []string {
	return append([]string(nil), r.unassigned...)
}

func loadModules(modulesRoot string) (map[string]loadedModule, []string, error) {
	entries, err := os.ReadDir(modulesRoot)
	if err != nil {
		if paths.IsMissing(modulesRoot, err) {
			return map[string]loadedModule{}, nil, nil
		}
		return nil, nil, fmt.Errorf("read modules directory %q: %w", modulesRoot, err)
	}

	// module 只来自 modules/ 的直接子目录；普通文件和 symlink 条目都不是 module。
	modules := make(map[string]loadedModule)
	moduleNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !manifestNamePattern.MatchString(name) {
			return nil, nil, fmt.Errorf("modules directory %q contains invalid module name %q", modulesRoot, name)
		}
		moduleRoot := filepath.Join(modulesRoot, name)
		moduleManifest, err := loadOptionalModuleManifest(filepath.Join(moduleRoot, filename))
		if err != nil {
			return nil, nil, err
		}
		modules[name] = loadedModule{root: moduleRoot, manifest: moduleManifest}
		moduleNames = append(moduleNames, name)
	}
	slices.Sort(moduleNames)
	return modules, moduleNames, nil
}

func loadOptionalModuleManifest(path string) (moduleSpec, error) {
	manifest, err := decodeModuleManifest(path)
	if err == nil {
		return manifest, nil
	}
	// 只有确认该路径真正缺失才使用空 manifest；dangling symlink 与读取错误必须继续上报。
	if paths.IsMissing(path, err) {
		return moduleSpec{}, nil
	}
	return moduleSpec{}, err
}
