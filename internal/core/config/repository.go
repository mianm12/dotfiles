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
	valid    bool
	root     string
	profiles map[string][]string
	modules  map[string]moduleFile
	ids      []string
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
	modules, ids, err := discoverModules(filepath.Join(root, "modules"))
	if err != nil {
		return Repository{}, err
	}
	return Repository{
		valid:    true,
		root:     root,
		profiles: profiles,
		modules:  modules,
		ids:      ids,
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

func discoverModules(root string) (map[string]moduleFile, []string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]moduleFile{}, nil, nil
		}
		return nil, nil, fmt.Errorf("read modules directory %q: %w", root, err)
	}

	modules := make(map[string]moduleFile)
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
			return nil, nil, fmt.Errorf("inspect module manifest %q: %w", manifest, err)
		}
		modules[id] = moduleFile{root: moduleRoot, manifest: manifest}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return modules, ids, nil
}
