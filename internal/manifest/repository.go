package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/mianm12/dotfiles/internal/paths"
)

var manifestNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

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
