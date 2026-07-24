package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

const (
	rootManifestName   = "dot.toml"
	moduleManifestName = "module.toml"
)

type rootDocument struct {
	Version  *int                 `toml:"version"`
	Profiles *map[string][]string `toml:"profiles"`
}

type moduleFile struct {
	root     string
	manifest string
}

// Repository is a strictly loaded root manifest plus lazily discovered module
// manifest paths. Module contents are not decoded until Resolve selects them.
type Repository struct {
	valid        bool
	root         string
	profiles     map[string][]string
	modules      map[string]moduleFile
	moduleErrors map[string]error
	ids          []string
}

// OpenRepository loads dot.toml and discovers only recognized module entries.
func OpenRepository(root string) (Repository, error) {
	if root == "" || !filepath.IsAbs(root) {
		return Repository{}, fmt.Errorf(
			"%w: repository must be a non-empty absolute path",
			ErrInvalidConfiguration,
		)
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return Repository{}, fmt.Errorf("inspect repository %q: %w", root, err)
	}
	if !info.IsDir() {
		return Repository{}, fmt.Errorf("%w: repository %q is not a directory", ErrInvalidConfiguration, root)
	}

	var document rootDocument
	manifestPath := filepath.Join(root, rootManifestName)
	if err := decodeStrict(manifestPath, &document); err != nil {
		return Repository{}, fmt.Errorf("%w: root manifest: %w", ErrInvalidConfiguration, err)
	}
	profiles, err := validateRootDocument(manifestPath, document)
	if err != nil {
		return Repository{}, err
	}
	modules, moduleErrors, ids, err := discoverModules(filepath.Join(root, "modules"))
	if err != nil {
		return Repository{}, err
	}
	return Repository{
		valid:        true,
		root:         root,
		profiles:     profiles,
		modules:      modules,
		moduleErrors: moduleErrors,
		ids:          ids,
	}, nil
}

// Root returns the absolute repository root.
func (repository Repository) Root() string {
	return repository.root
}

// ModuleIDs returns the recognized module IDs in byte order.
func (repository Repository) ModuleIDs() []string {
	return append([]string(nil), repository.ids...)
}

// HasModule reports whether discovery recognized a module.
func (repository Repository) HasModule(id string) bool {
	_, exists := repository.modules[id]
	return repository.valid && exists
}

// ProfileModules returns the sorted module union selected by profiles without
// decoding module manifests.
func (repository Repository) ProfileModules(profiles []string) ([]string, error) {
	if !repository.valid {
		return nil, fmt.Errorf("%w: repository is invalid", ErrInvalidConfiguration)
	}
	if err := validateIDs("profile", profiles); err != nil {
		return nil, err
	}

	selected := make(map[string]bool)
	for _, profile := range profiles {
		members, exists := repository.profiles[profile]
		if !exists {
			return nil, fmt.Errorf(
				"%w: unknown profile %q",
				ErrInvalidConfiguration,
				profile,
			)
		}
		for _, module := range members {
			exists, err := repository.inspectModule(module)
			if err != nil {
				return nil, err
			}
			if !exists {
				return nil, fmt.Errorf(
					"%w: active profile %q references missing module %q",
					ErrInvalidConfiguration,
					profile,
					module,
				)
			}
			selected[module] = true
		}
	}
	return sortedKeys(selected), nil
}

// InspectModule strictly decodes one recognized module. Missing modules are
// reported without error so remove and status can distinguish inactive known
// modules from state-only cleanup.
func (repository Repository) InspectModule(
	id string,
	platform Platform,
) (module Module, exists, applicable bool, err error) {
	if !repository.valid {
		return Module{}, false, false, fmt.Errorf(
			"%w: repository is invalid",
			ErrInvalidConfiguration,
		)
	}
	if err := validateID("module", id); err != nil {
		return Module{}, false, false, err
	}
	exists, err = repository.inspectModule(id)
	if err != nil || !exists {
		return Module{}, exists, false, err
	}
	module, applicable, err = loadModule(id, repository.modules[id], platform)
	return module, true, applicable, err
}

func (repository Repository) inspectModule(id string) (bool, error) {
	if repository.HasModule(id) {
		return true, nil
	}
	if err, exists := repository.moduleErrors[id]; exists {
		return false, fmt.Errorf(
			"%w: inspect module %q: %w",
			ErrInvalidConfiguration,
			id,
			err,
		)
	}
	return false, nil
}

func validateRootDocument(path string, document rootDocument) (map[string][]string, error) {
	if document.Version == nil || *document.Version != 1 {
		return nil, fmt.Errorf(
			"%w: root manifest %q version must be 1",
			ErrInvalidConfiguration,
			path,
		)
	}
	if document.Profiles == nil {
		return nil, fmt.Errorf(
			"%w: root manifest %q must declare [profiles]",
			ErrInvalidConfiguration,
			path,
		)
	}
	profiles := make(map[string][]string, len(*document.Profiles))
	for name, members := range *document.Profiles {
		if err := validateID("profile", name); err != nil {
			return nil, err
		}
		if err := validateUniqueIDs("module", members); err != nil {
			return nil, fmt.Errorf("profile %q: %w", name, err)
		}
		profiles[name] = append([]string(nil), members...)
	}
	return profiles, nil
}

func discoverModules(root string) (map[string]moduleFile, map[string]error, []string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]moduleFile{}, map[string]error{}, nil, nil
		}
		return nil, nil, nil, fmt.Errorf("read modules directory %q: %w", root, err)
	}

	modules := make(map[string]moduleFile)
	moduleErrors := make(map[string]error)
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		id := entry.Name()
		if !entry.IsDir() || !idPattern.MatchString(id) {
			continue
		}
		moduleRoot := filepath.Join(root, id)
		manifest := filepath.Join(moduleRoot, moduleManifestName)
		if _, err := os.Lstat(manifest); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			moduleErrors[id] = fmt.Errorf("inspect module manifest %q: %w", manifest, err)
			continue
		}
		modules[id] = moduleFile{root: moduleRoot, manifest: manifest}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return modules, moduleErrors, ids, nil
}
