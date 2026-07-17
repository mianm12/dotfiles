package manifest

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/ghstlnx/dotfiles/internal/datakey"
	"github.com/pelletier/go-toml/v2"
)

var (
	modePattern                 = regexp.MustCompile(`^0[0-7]{3}$`)
	environmentReferencePattern = regexp.MustCompile(`\$[A-Za-z_][A-Za-z0-9_]*|\$\{[A-Za-z_][A-Za-z0-9_]*\}`)
)

const (
	goosDarwin = "darwin"
	goosLinux  = "linux"
)

// optional 保留字段是否出现；显式空值也必须覆盖上一层，不能与缺失混为一谈。
type optional[T any] struct {
	value T
	set   bool
}

// raw 类型只描述允许的 TOML 键和基础类型。target 与 run_once 使用 any 表达规范规定的
// 联合形态，随后由 parseTarget 和 parseRunOnce 收口到封闭类型集合。
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
	requirement      Requirement
	defaults         defaultsSpec
	ignore           []string
	declaredProfiles map[string][]string
	data             map[string]dataSpec
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

// targetSpec 是已经校验的联合形态；有效值只会设置 common 或非空 byOS 之一。
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

	managedFileKindName = "managed"
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
	for _, key := range sortedKeys(raw.Data) {
		entry := raw.Data[key]
		if !datakey.Valid(key) {
			return rootSpec{}, fmt.Errorf("manifest %q: invalid data key %q", path, key)
		}
		if entry.FromEnv != nil {
			return rootSpec{}, fmt.Errorf("manifest %q: data.%s.from_env requires M2", path, key)
		}
		data[key] = dataSpec{prompt: entry.Prompt, defaultValue: entry.Default}
	}

	ignore, err := parseIgnore(path, "ignore.patterns", raw.Ignore)
	if err != nil {
		return rootSpec{}, err
	}

	return rootSpec{
		requirement:      requirement,
		defaults:         defaults,
		ignore:           ignore,
		declaredProfiles: cloneProfiles(*raw.Profiles),
		data:             data,
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
	for _, declaredSource := range sortedKeys(raw.Files) {
		source, err := normalizeModulePath(declaredSource)
		if err != nil {
			return moduleSpec{}, fmt.Errorf("manifest %q: files key %q: %w", path, declaredSource, err)
		}
		if _, exists := files[source]; exists {
			return moduleSpec{}, fmt.Errorf(
				"manifest %q: files key %q duplicates normalized source %q",
				path,
				declaredSource,
				source,
			)
		}
		file, err := parseFile(path, source, raw.Files[declaredSource])
		if err != nil {
			return moduleSpec{}, err
		}
		files[source] = file
	}
	ignore, err := parseIgnore(path, "ignore.patterns", raw.Ignore)
	if err != nil {
		return moduleSpec{}, err
	}
	runOnce, err := parseRunOnce(path, raw.Hooks)
	if err != nil {
		return moduleSpec{}, err
	}
	if err := validateExplicitFiles(path, files, runOnce); err != nil {
		return moduleSpec{}, err
	}

	return moduleSpec{
		os:      osValues,
		target:  target,
		ignore:  ignore,
		files:   files,
		runOnce: runOnce,
	}, nil
}

func decodeManifestFile[T any](path string) (T, error) {
	var zero T
	file, err := openManifest(path)
	if err != nil {
		return zero, err
	}
	var document T
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	// 不使用 defer，以便报告 Close 错误；Decode 与 Close 均失败时优先返回 Decode 错误。
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
		if !isSupportedGOOS(value) {
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
			return optional[targetSpec]{}, fmt.Errorf(
				"manifest %q: %s table must contain %s or %s",
				path,
				field,
				goosDarwin,
				goosLinux,
			)
		}
		byOS := make(map[string]string, len(value))
		for _, goos := range sortedKeys(value) {
			rawPath := value[goos]
			if !isSupportedGOOS(goos) {
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

func isSupportedGOOS(value string) bool {
	return value == goosDarwin || value == goosLinux
}

func validateTargetPath(value string) error {
	if value == "~" {
		return nil
	}
	if !strings.HasPrefix(value, "~/") || strings.HasSuffix(value, "/") ||
		strings.ContainsRune(value, '\x00') || environmentReferencePattern.MatchString(value) {
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
	kind, err := parseFileKind(path, source, raw.Kind)
	if err != nil {
		return fileSpec{}, err
	}
	if raw.Mode != nil {
		if !modePattern.MatchString(*raw.Mode) {
			return fileSpec{}, fmt.Errorf("manifest %q: files key %q: invalid mode %q", path, source, *raw.Mode)
		}
		if kind == FileKindLink {
			return fileSpec{}, fmt.Errorf("manifest %q: files key %q: mode is not allowed for link", path, source)
		}
	}
	if raw.Target != nil {
		if err := validateEntryTargetPath(*raw.Target); err != nil {
			return fileSpec{}, fmt.Errorf("manifest %q: files key %q: target: %w", path, source, err)
		}
	}
	return fileSpec{kind: kind, mode: raw.Mode, target: raw.Target}, nil
}

func parseFileKind(path, source string, declared *string) (FileKind, error) {
	// 后缀只提供缺省；显式 kind 可以把模板后缀覆盖为 M1 支持的 link 或 scaffold。
	kindName := string(FileKindLink)
	switch {
	case strings.HasSuffix(source, ".tmpl"):
		kindName = managedFileKindName
	case strings.HasSuffix(source, ".template"):
		kindName = string(FileKindScaffold)
	}
	if declared != nil {
		kindName = *declared
	}

	switch kindName {
	case string(FileKindLink):
		return FileKindLink, nil
	case string(FileKindScaffold):
		return FileKindScaffold, nil
	case managedFileKindName:
		if declared != nil {
			return "", fmt.Errorf("manifest %q: files key %q: kind managed requires M2", path, source)
		}
		return "", fmt.Errorf("manifest %q: files key %q: resolves to managed, which requires M2", path, source)
	default:
		return "", fmt.Errorf("manifest %q: files key %q: invalid kind %q", path, source, kindName)
	}
}

func parseRunOnce(path string, raw *rawHooks) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	runOnce := make([]string, 0, len(raw.RunOnce))
	seen := make(map[string]struct{}, len(raw.RunOnce))
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
		normalized, err := normalizeModulePath(script)
		if err != nil {
			return nil, fmt.Errorf("manifest %q: hooks.run_once[%d]: %w", path, index, err)
		}
		if _, exists := seen[normalized]; exists {
			return nil, fmt.Errorf("manifest %q: hooks.run_once[%d] duplicates script %q", path, index, normalized)
		}
		seen[normalized] = struct{}{}
		runOnce = append(runOnce, normalized)
	}
	return runOnce, nil
}

func parseIgnore(path, field string, raw *rawIgnore) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	patterns := append([]string(nil), raw.Patterns...)
	for index, pattern := range patterns {
		if err := validateIgnorePattern(pattern); err != nil {
			return nil, fmt.Errorf("manifest %q: %s[%d]: %w", path, field, index, err)
		}
	}
	return patterns, nil
}

func validateIgnorePattern(pattern string) error {
	if pattern == "" || strings.ContainsRune(pattern, '\x00') {
		return fmt.Errorf("ignore pattern %q must not be empty or contain NUL", pattern)
	}
	if strings.HasPrefix(pattern, "!") {
		return fmt.Errorf("ignore pattern %q uses unsupported negation", pattern)
	}
	if strings.ContainsAny(pattern, `?[]\`) {
		return fmt.Errorf("ignore pattern %q uses unsupported glob syntax", pattern)
	}

	trimmed := strings.TrimPrefix(pattern, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return fmt.Errorf("ignore pattern %q has no path component", pattern)
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("ignore pattern %q contains an invalid path segment", pattern)
		}
		if strings.Contains(segment, "**") && segment != "**" {
			return fmt.Errorf("ignore pattern %q requires ** to occupy a complete path segment", pattern)
		}
	}
	return nil
}

func normalizeModulePath(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, '\x00') || filepath.IsAbs(path) {
		return "", fmt.Errorf("path %q must be a non-empty relative path", path)
	}
	normalized := filepath.Clean(path)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q must stay within the module", path)
	}
	return filepath.ToSlash(normalized), nil
}

func validateEntryTargetPath(path string) error {
	if err := validateTargetPath(path); err != nil {
		return err
	}
	if path == "~" {
		return fmt.Errorf("entry target must be a true descendant of HOME")
	}
	return nil
}

func validateExplicitFiles(path string, files map[string]fileSpec, runOnce []string) error {
	// 用户 ignore 可由 [files] 覆盖，内置 ignore 与 hook 引用则始终不可覆盖。
	hookPaths := make(map[string]struct{}, len(runOnce))
	for _, script := range runOnce {
		hookPaths[script] = struct{}{}
	}
	for _, source := range sortedKeys(files) {
		if reason := builtInIgnoreReason(source, hookPaths); reason != "" {
			return fmt.Errorf("manifest %q: files key %q: cannot override built-in ignore for %s", path, source, reason)
		}
	}
	return nil
}

func builtInIgnoreReason(source string, hookPaths map[string]struct{}) string {
	if source == filename {
		return "root dot.toml"
	}
	segments := strings.Split(source, "/")
	if len(segments) > 1 && segments[0] == "hooks" {
		return "root hooks directory"
	}
	for _, segment := range segments {
		if segment == ".git" {
			return ".git path"
		}
		if strings.HasSuffix(segment, ".swp") {
			return "*.swp path"
		}
	}
	if _, exists := hookPaths[source]; exists {
		return "hook reference"
	}
	return ""
}

func cloneProfiles(raw map[string][]string) map[string][]string {
	profiles := make(map[string][]string, len(raw))
	for name, members := range raw {
		profiles[name] = append([]string(nil), members...)
	}
	return profiles
}

// sortedKeys 按字节序返回 map key，避免调用方的结果或首个错误受 TOML 键顺序影响。
func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
