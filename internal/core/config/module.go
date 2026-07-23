package config

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
)

type matchDocument struct {
	OS     []string `toml:"os"`
	Distro []string `toml:"distro"`
	Arch   []string `toml:"arch"`
}

type linkDocument struct {
	ID     string `toml:"id"`
	Source string `toml:"source"`
	Target string `toml:"target"`
}

type localDocument struct {
	ID      string `toml:"id"`
	Example string `toml:"example"`
	Target  string `toml:"target"`
}

type variantDocument struct {
	Root   *string         `toml:"root"`
	Match  *matchDocument  `toml:"match"`
	Links  []linkDocument  `toml:"links"`
	Locals []localDocument `toml:"locals"`
}

type moduleDocument struct {
	Match    *matchDocument             `toml:"match"`
	Links    []linkDocument             `toml:"links"`
	Locals   []localDocument            `toml:"locals"`
	Variants map[string]variantDocument `toml:"variants"`
}

type placementSet struct {
	links  []linkDocument
	locals []localDocument
}

type selectedModule struct {
	variant    string
	root       string
	placements placementSet
}

func loadModule(id string, file moduleFile, platform Platform) (Module, bool, error) {
	var document moduleDocument
	if err := decodeStrict(file.manifest, &document); err != nil {
		return Module{}, false, fmt.Errorf(
			"%w: module %q: %w",
			ErrInvalidConfiguration,
			id,
			err,
		)
	}

	selected, applicable, err := selectModule(id, file.root, document, platform)
	if err != nil {
		return Module{}, false, err
	}
	if !applicable {
		return Module{}, false, nil
	}
	links, locals, err := materializePlacements(
		id,
		file.root,
		selected.root,
		selected.placements,
	)
	if err != nil {
		return Module{}, false, err
	}
	return Module{
		ID:      id,
		Variant: selected.variant,
		Root:    selected.root,
		Links:   links,
		Locals:  locals,
	}, true, nil
}

func selectModule(
	id, moduleRoot string,
	document moduleDocument,
	platform Platform,
) (selectedModule, bool, error) {
	portableDeclared := document.Match != nil || document.Links != nil || document.Locals != nil
	variantsDeclared := document.Variants != nil
	if portableDeclared && variantsDeclared {
		return selectedModule{}, false, fmt.Errorf(
			"%w: module %q mixes portable placements with variants",
			ErrInvalidConfiguration,
			id,
		)
	}

	if !variantsDeclared {
		if err := validateMatch(id, "portable", document.Match); err != nil {
			return selectedModule{}, false, err
		}
		placements, err := validatePlacements(id, "portable", document.Links, document.Locals)
		if err != nil {
			return selectedModule{}, false, err
		}
		if !matches(document.Match, platform) {
			return selectedModule{}, false, nil
		}
		return selectedModule{root: moduleRoot, placements: placements}, true, nil
	}

	var selected []selectedModule
	for _, name := range sortedKeys(document.Variants) {
		if err := validateID("variant", name); err != nil {
			return selectedModule{}, false, fmt.Errorf("module %q: %w", id, err)
		}
		variant := document.Variants[name]
		root, err := variantRoot(id, name, moduleRoot, variant.Root)
		if err != nil {
			return selectedModule{}, false, err
		}
		if err := validateMatch(id, "variant "+name, variant.Match); err != nil {
			return selectedModule{}, false, err
		}
		placements, err := validatePlacements(
			id,
			"variant "+name,
			variant.Links,
			variant.Locals,
		)
		if err != nil {
			return selectedModule{}, false, err
		}
		if matches(variant.Match, platform) {
			selected = append(selected, selectedModule{
				variant:    name,
				root:       root,
				placements: placements,
			})
		}
	}
	if len(selected) == 0 {
		return selectedModule{}, false, nil
	}
	if len(selected) > 1 {
		names := make([]string, len(selected))
		for index := range selected {
			names[index] = selected[index].variant
		}
		return selectedModule{}, false, fmt.Errorf(
			"%w: module %q has multiple matching variants: %s",
			ErrInvalidConfiguration,
			id,
			strings.Join(names, ", "),
		)
	}
	return selected[0], true, nil
}

func validateMatch(module, location string, match *matchDocument) error {
	if match == nil {
		return nil
	}
	for _, operatingSystem := range match.OS {
		if operatingSystem != "macos" && operatingSystem != "linux" {
			return fmt.Errorf(
				"%w: module %q %s has unsupported os token %q",
				ErrInvalidConfiguration,
				module,
				location,
				operatingSystem,
			)
		}
	}
	linuxOnly := len(match.OS) > 0
	for _, operatingSystem := range match.OS {
		linuxOnly = linuxOnly && operatingSystem == "linux"
	}
	if match.Distro != nil && !linuxOnly {
		return fmt.Errorf(
			"%w: module %q %s may declare distro only with os = [\"linux\"]",
			ErrInvalidConfiguration,
			module,
			location,
		)
	}
	if err := validateLowerTokens("distro", match.Distro); err != nil {
		return fmt.Errorf("module %q %s: %w", module, location, err)
	}
	if err := validateLowerTokens("arch", match.Arch); err != nil {
		return fmt.Errorf("module %q %s: %w", module, location, err)
	}
	return nil
}

func matches(match *matchDocument, platform Platform) bool {
	if match == nil {
		return true
	}
	return matchesField(match.OS, platform.OS) &&
		matchesField(match.Distro, platform.Distro) &&
		matchesField(match.Arch, platform.Arch)
}

func matchesField(allowed []string, actual string) bool {
	return allowed == nil || slices.Contains(allowed, actual)
}

func variantRoot(module, variant, moduleRoot string, declared *string) (string, error) {
	if declared == nil || *declared == "" {
		return "", fmt.Errorf(
			"%w: module %q variant %q root is required",
			ErrInvalidConfiguration,
			module,
			variant,
		)
	}
	if *declared == "." {
		return moduleRoot, nil
	}
	normalized, err := cleanRelative("variant root", *declared)
	if err != nil {
		return "", fmt.Errorf("module %q variant %q: %w", module, variant, err)
	}
	return filepath.Join(moduleRoot, filepath.FromSlash(normalized)), nil
}

func validatePlacements(
	module, location string,
	links []linkDocument,
	locals []localDocument,
) (placementSet, error) {
	ids := make(map[string]struct{}, len(links)+len(locals))
	for index := range links {
		link := &links[index]
		if err := validatePlacementID(module, location, "link", link.ID, ids); err != nil {
			return placementSet{}, err
		}
		source, err := cleanRelative("link source", link.Source)
		if err != nil {
			return placementSet{}, fmt.Errorf(
				"module %q %s link %q: %w",
				module,
				location,
				link.ID,
				err,
			)
		}
		if err := corepaths.ValidateTargetExpression(link.Target); err != nil {
			return placementSet{}, fmt.Errorf(
				"%w: module %q %s link %q target: %w",
				ErrInvalidConfiguration,
				module,
				location,
				link.ID,
				err,
			)
		}
		link.Source = source
	}
	for index := range locals {
		local := &locals[index]
		if err := validatePlacementID(module, location, "local", local.ID, ids); err != nil {
			return placementSet{}, err
		}
		example, err := cleanRelative("local example", local.Example)
		if err != nil {
			return placementSet{}, fmt.Errorf(
				"module %q %s local %q: %w",
				module,
				location,
				local.ID,
				err,
			)
		}
		if err := corepaths.ValidateTargetExpression(local.Target); err != nil {
			return placementSet{}, fmt.Errorf(
				"%w: module %q %s local %q target: %w",
				ErrInvalidConfiguration,
				module,
				location,
				local.ID,
				err,
			)
		}
		local.Example = example
	}
	return placementSet{
		links:  append([]linkDocument(nil), links...),
		locals: append([]localDocument(nil), locals...),
	}, nil
}

func validatePlacementID(
	module, location, kind, id string,
	ids map[string]struct{},
) error {
	if err := validateID("placement", id); err != nil {
		return fmt.Errorf("module %q %s %s: %w", module, location, kind, err)
	}
	if _, exists := ids[id]; exists {
		return fmt.Errorf(
			"%w: module %q %s duplicates placement ID %q",
			ErrInvalidConfiguration,
			module,
			location,
			id,
		)
	}
	ids[id] = struct{}{}
	return nil
}

func cleanRelative(kind, value string) (string, error) {
	if value == "" || strings.ContainsRune(value, '\x00') || path.IsAbs(value) {
		return "", fmt.Errorf(
			"%w: %s %q must be a non-empty relative path",
			ErrInvalidConfiguration,
			kind,
			value,
		)
	}
	cleaned := path.Clean(value)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf(
			"%w: %s %q escapes its module root",
			ErrInvalidConfiguration,
			kind,
			value,
		)
	}
	return cleaned, nil
}

func materializePlacements(
	module, moduleRoot, root string,
	placements placementSet,
) ([]Link, []Local, error) {
	resolvedModuleRoot, err := filepath.EvalSymlinks(moduleRoot)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"%w: module %q resolve module root %q: %w",
			ErrInvalidConfiguration,
			module,
			moduleRoot,
			err,
		)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"%w: module %q resolve selected root %q: %w",
			ErrInvalidConfiguration,
			module,
			root,
			err,
		)
	}
	if !pathWithin(resolvedModuleRoot, resolvedRoot) {
		return nil, nil, fmt.Errorf(
			"%w: module %q selected root %q escapes module root",
			ErrInvalidConfiguration,
			module,
			root,
		)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"%w: module %q inspect selected root %q: %w",
			ErrInvalidConfiguration,
			module,
			root,
			err,
		)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf(
			"%w: module %q selected root %q is not a directory",
			ErrInvalidConfiguration,
			module,
			root,
		)
	}

	links := make([]Link, len(placements.links))
	for index, declared := range placements.links {
		sourcePath := filepath.Join(root, filepath.FromSlash(declared.Source))
		mode, err := validateLinkSource(module, declared.ID, resolvedRoot, sourcePath)
		if err != nil {
			return nil, nil, err
		}
		links[index] = Link{
			ID:         declared.ID,
			Source:     declared.Source,
			SourcePath: sourcePath,
			Target:     declared.Target,
			SourceMode: mode,
		}
	}

	locals := make([]Local, len(placements.locals))
	for index, declared := range placements.locals {
		examplePath := filepath.Join(root, filepath.FromSlash(declared.Example))
		if err := validateLocalExample(module, declared.ID, resolvedRoot, examplePath); err != nil {
			return nil, nil, err
		}
		locals[index] = Local{
			ID:          declared.ID,
			Example:     declared.Example,
			ExamplePath: examplePath,
			Target:      declared.Target,
		}
	}
	return links, locals, nil
}

func validateLinkSource(module, placement, root, source string) (fs.FileMode, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return 0, fmt.Errorf(
			"%w: module %q link %q inspect source %q: %w",
			ErrInvalidConfiguration,
			module,
			placement,
			source,
			err,
		)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return 0, fmt.Errorf(
			"%w: module %q link %q source %q is a symlink",
			ErrInvalidConfiguration,
			module,
			placement,
			source,
		)
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return 0, fmt.Errorf(
			"%w: module %q link %q source %q is special",
			ErrInvalidConfiguration,
			module,
			placement,
			source,
		)
	}
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		return 0, fmt.Errorf(
			"%w: module %q link %q resolve source %q: %w",
			ErrInvalidConfiguration,
			module,
			placement,
			source,
			err,
		)
	}
	if !pathWithin(root, resolvedSource) {
		return 0, fmt.Errorf(
			"%w: module %q link %q source %q escapes selected root",
			ErrInvalidConfiguration,
			module,
			placement,
			source,
		)
	}
	return info.Mode(), nil
}

func validateLocalExample(module, placement, root, example string) error {
	info, err := os.Lstat(example)
	if err != nil {
		return fmt.Errorf(
			"%w: module %q local %q inspect example %q: %w",
			ErrInvalidConfiguration,
			module,
			placement,
			example,
			err,
		)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf(
			"%w: module %q local %q example %q is not a regular file",
			ErrInvalidConfiguration,
			module,
			placement,
			example,
		)
	}
	resolvedExample, err := filepath.EvalSymlinks(example)
	if err != nil {
		return fmt.Errorf(
			"%w: module %q local %q resolve example %q: %w",
			ErrInvalidConfiguration,
			module,
			placement,
			example,
			err,
		)
	}
	if !pathWithin(root, resolvedExample) {
		return fmt.Errorf(
			"%w: module %q local %q example %q escapes selected root",
			ErrInvalidConfiguration,
			module,
			placement,
			example,
		)
	}
	return nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
