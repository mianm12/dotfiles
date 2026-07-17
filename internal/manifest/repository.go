package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/ghstlnx/dotfiles/internal/paths"
)

var manifestNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Repository 表示已经严格加载、但尚未按 profile 和 OS 解析的仓库 manifest。
type Repository struct {
	manifest    rootSpec
	modules     map[string]loadedModule
	moduleNames []string
}

type loadedModule struct {
	root     string
	manifest moduleSpec
}

// Load 严格读取根 manifest 与所有模块 manifest；缺少 modules 目录表示仓库没有模块。
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

	return Repository{
		manifest:    rootManifest,
		modules:     modules,
		moduleNames: moduleNames,
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

func loadModules(modulesRoot string) (map[string]loadedModule, []string, error) {
	entries, err := os.ReadDir(modulesRoot)
	if err != nil {
		if paths.IsMissing(modulesRoot, err) {
			return map[string]loadedModule{}, nil, nil
		}
		return nil, nil, fmt.Errorf("read modules directory %q: %w", modulesRoot, err)
	}

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
	if paths.IsMissing(path, err) {
		return moduleSpec{}, nil
	}
	return moduleSpec{}, err
}
