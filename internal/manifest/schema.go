package manifest

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/ghstlnx/dotfiles/internal/datakey"
	"github.com/pelletier/go-toml/v2"
)

var modePattern = regexp.MustCompile(`^0[0-7]{3}$`)

type optional[T any] struct {
	value T
	set   bool
}

type rawRootManifest struct {
	Requires *string                 `toml:"requires"`
	Defaults *rawDefaults            `toml:"defaults"`
	Ignore   *rawIgnore              `toml:"ignore"`
	Profiles *map[string][]string    `toml:"profiles"`
	Data     map[string]rawDataEntry `toml:"data"`
}

type rawDefaults struct {
	OS     *[]string `toml:"os"`
	Target any       `toml:"target"`
}

type rawIgnore struct {
	Patterns []string `toml:"patterns"`
}

type rawDataEntry struct {
	Prompt  *string `toml:"prompt"`
	Default *string `toml:"default"`
	FromEnv *string `toml:"from_env"`
}

type rawModuleManifest struct {
	OS     *[]string          `toml:"os"`
	Target any                `toml:"target"`
	Ignore *rawIgnore         `toml:"ignore"`
	Files  map[string]rawFile `toml:"files"`
	Hooks  *rawHooks          `toml:"hooks"`
}

type rawFile struct {
	Mode   *string `toml:"mode"`
	Kind   *string `toml:"kind"`
	Target *string `toml:"target"`
}

type rawHooks struct {
	RunOnce []any `toml:"run_once"`
}

type rootSpec struct {
	requirement Requirement
	defaults    defaultsSpec
	ignore      []string
	profiles    map[string][]string
	data        map[string]dataSpec
}

type defaultsSpec struct {
	os     optional[[]string]
	target optional[targetSpec]
}

type dataSpec struct {
	prompt       *string
	defaultValue *string
}

type moduleSpec struct {
	os      optional[[]string]
	target  optional[targetSpec]
	ignore  []string
	files   map[string]fileSpec
	runOnce []string
}

type targetSpec struct {
	common *string
	byOS   map[string]string
}

// FileKind 表示 manifest 声明或文件名推导出的 M1 文件行为。
type FileKind string

const (
	// FileKindLink 表示 target 应链接到仓库 source。
	FileKindLink FileKind = "link"
	// FileKindScaffold 表示只在首次缺失时生成普通文件。
	FileKindScaffold FileKind = "scaffold"
)

type fileSpec struct {
	kind   FileKind
	mode   *string
	target *string
}

func decodeRootManifest(path string) (rootSpec, error) {
	raw, err := decodeManifestFile[rawRootManifest](path)
	if err != nil {
		return rootSpec{}, err
	}
	if raw.Requires == nil {
		return rootSpec{}, fmt.Errorf("manifest %q: required top-level requires is missing", path)
	}
	requirement, err := ParseRequirement(*raw.Requires)
	if err != nil {
		return rootSpec{}, fmt.Errorf("manifest %q: %w", path, err)
	}
	if raw.Profiles == nil || len(*raw.Profiles) == 0 {
		return rootSpec{}, fmt.Errorf("manifest %q: profiles must declare at least one profile", path)
	}

	defaults, err := parseDefaults(path, raw.Defaults)
	if err != nil {
		return rootSpec{}, err
	}
	data := make(map[string]dataSpec, len(raw.Data))
	for key, entry := range raw.Data {
		if !datakey.Valid(key) {
			return rootSpec{}, fmt.Errorf("manifest %q: invalid data key %q", path, key)
		}
		if entry.FromEnv != nil {
			return rootSpec{}, fmt.Errorf("manifest %q: data.%s.from_env requires M2", path, key)
		}
		data[key] = dataSpec{prompt: entry.Prompt, defaultValue: entry.Default}
	}

	return rootSpec{
		requirement: requirement,
		defaults:    defaults,
		ignore:      cloneStrings(raw.Ignore),
		profiles:    cloneProfiles(*raw.Profiles),
		data:        data,
	}, nil
}

func decodeModuleManifest(path string) (moduleSpec, error) {
	raw, err := decodeManifestFile[rawModuleManifest](path)
	if err != nil {
		return moduleSpec{}, err
	}

	osValues, err := parseOS(path, "os", raw.OS)
	if err != nil {
		return moduleSpec{}, err
	}
	target, err := parseTarget(path, "target", raw.Target)
	if err != nil {
		return moduleSpec{}, err
	}
	files := make(map[string]fileSpec, len(raw.Files))
	for source, entry := range raw.Files {
		file, err := parseFile(path, source, entry)
		if err != nil {
			return moduleSpec{}, err
		}
		files[source] = file
	}
	runOnce, err := parseRunOnce(path, raw.Hooks)
	if err != nil {
		return moduleSpec{}, err
	}

	return moduleSpec{
		os:      osValues,
		target:  target,
		ignore:  cloneStrings(raw.Ignore),
		files:   files,
		runOnce: runOnce,
	}, nil
}

func decodeManifestFile[T any](path string) (T, error) {
	var zero T
	file, err := os.Open(path)
	if err != nil {
		return zero, fmt.Errorf("open manifest %q: %w", path, err)
	}
	var document T
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&document)
	closeErr := file.Close()
	if decodeErr != nil {
		return zero, fmt.Errorf("decode manifest %q: %w", path, decodeErr)
	}
	if closeErr != nil {
		return zero, fmt.Errorf("close manifest %q after reading: %w", path, closeErr)
	}
	return document, nil
}

func parseDefaults(path string, raw *rawDefaults) (defaultsSpec, error) {
	if raw == nil {
		return defaultsSpec{}, nil
	}
	osValues, err := parseOS(path, "defaults.os", raw.OS)
	if err != nil {
		return defaultsSpec{}, err
	}
	target, err := parseTarget(path, "defaults.target", raw.Target)
	if err != nil {
		return defaultsSpec{}, err
	}
	return defaultsSpec{os: osValues, target: target}, nil
}

func parseOS(path, field string, raw *[]string) (optional[[]string], error) {
	if raw == nil {
		return optional[[]string]{}, nil
	}
	seen := make(map[string]struct{}, len(*raw))
	values := make([]string, 0, len(*raw))
	for _, value := range *raw {
		if value != "darwin" && value != "linux" {
			return optional[[]string]{}, fmt.Errorf("manifest %q: %s contains unsupported OS %q", path, field, value)
		}
		if _, exists := seen[value]; exists {
			return optional[[]string]{}, fmt.Errorf("manifest %q: %s contains duplicate OS %q", path, field, value)
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return optional[[]string]{value: values, set: true}, nil
}

func parseTarget(path, field string, raw any) (optional[targetSpec], error) {
	if raw == nil {
		return optional[targetSpec]{}, nil
	}
	switch value := raw.(type) {
	case string:
		if err := validateTargetPath(value); err != nil {
			return optional[targetSpec]{}, fmt.Errorf("manifest %q: %s: %w", path, field, err)
		}
		return optional[targetSpec]{value: targetSpec{common: &value}, set: true}, nil
	case map[string]any:
		if len(value) == 0 {
			return optional[targetSpec]{}, fmt.Errorf("manifest %q: %s table must contain darwin or linux", path, field)
		}
		byOS := make(map[string]string, len(value))
		for goos, rawPath := range value {
			if goos != "darwin" && goos != "linux" {
				return optional[targetSpec]{}, fmt.Errorf("manifest %q: %s contains unsupported OS %q", path, field, goos)
			}
			targetPath, ok := rawPath.(string)
			if !ok {
				return optional[targetSpec]{}, fmt.Errorf("manifest %q: %s.%s must be a string", path, field, goos)
			}
			if err := validateTargetPath(targetPath); err != nil {
				return optional[targetSpec]{}, fmt.Errorf("manifest %q: %s.%s: %w", path, field, goos, err)
			}
			byOS[goos] = targetPath
		}
		return optional[targetSpec]{value: targetSpec{byOS: byOS}, set: true}, nil
	default:
		return optional[targetSpec]{}, fmt.Errorf("manifest %q: %s must be a string or OS table", path, field)
	}
}

func validateTargetPath(value string) error {
	if value == "~" {
		return nil
	}
	if !strings.HasPrefix(value, "~/") || strings.HasSuffix(value, "/") || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("target %q must be ~ or a canonical ~/ path", value)
	}
	for _, part := range strings.Split(strings.TrimPrefix(value, "~/"), "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("target %q must be ~ or a canonical ~/ path", value)
		}
	}
	return nil
}

func parseFile(path, source string, raw rawFile) (fileSpec, error) {
	kind := FileKindLink
	if strings.HasSuffix(source, ".tmpl") {
		kind = "managed"
	} else if strings.HasSuffix(source, ".template") {
		kind = FileKindScaffold
	}
	if raw.Kind != nil {
		switch *raw.Kind {
		case string(FileKindLink):
			kind = FileKindLink
		case string(FileKindScaffold):
			kind = FileKindScaffold
		case "managed":
			return fileSpec{}, fmt.Errorf("manifest %q: files.%s kind managed requires M2", path, source)
		default:
			return fileSpec{}, fmt.Errorf("manifest %q: files.%s has invalid kind %q", path, source, *raw.Kind)
		}
	}
	if kind == "managed" {
		return fileSpec{}, fmt.Errorf("manifest %q: files.%s resolves to managed, which requires M2", path, source)
	}
	if raw.Mode != nil {
		if !modePattern.MatchString(*raw.Mode) {
			return fileSpec{}, fmt.Errorf("manifest %q: files.%s has invalid mode %q", path, source, *raw.Mode)
		}
		if kind == FileKindLink {
			return fileSpec{}, fmt.Errorf("manifest %q: files.%s mode is not allowed for link", path, source)
		}
	}
	if raw.Target != nil {
		if err := validateTargetPath(*raw.Target); err != nil {
			return fileSpec{}, fmt.Errorf("manifest %q: files.%s.target: %w", path, source, err)
		}
	}
	return fileSpec{kind: kind, mode: raw.Mode, target: raw.Target}, nil
}

func parseRunOnce(path string, raw *rawHooks) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	runOnce := make([]string, 0, len(raw.RunOnce))
	for index, entry := range raw.RunOnce {
		script, ok := entry.(string)
		if !ok {
			if _, table := entry.(map[string]any); table {
				return nil, fmt.Errorf("manifest %q: hooks.run_once[%d] inline table requires M2", path, index)
			}
			return nil, fmt.Errorf("manifest %q: hooks.run_once[%d] must be a string", path, index)
		}
		if script == "" {
			return nil, fmt.Errorf("manifest %q: hooks.run_once[%d] must not be empty", path, index)
		}
		runOnce = append(runOnce, script)
	}
	return runOnce, nil
}

func cloneStrings(raw *rawIgnore) []string {
	if raw == nil {
		return nil
	}
	return append([]string(nil), raw.Patterns...)
}

func cloneProfiles(raw map[string][]string) map[string][]string {
	profiles := make(map[string][]string, len(raw))
	for name, members := range raw {
		profiles[name] = append([]string(nil), members...)
	}
	return profiles
}
