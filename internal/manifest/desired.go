package manifest

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// DesiredEntry 是 render、target 身份校验和 planner 消费的只读结构性期望。
// Source 是规范化模块相对路径，Target 是规范化 ~/ 展示路径；对应的 Path 字段是绝对路径。
type DesiredEntry struct {
	Module     string
	Source     string
	SourcePath string
	Target     string
	TargetPath string
	Kind       FileKind
	Mode       fs.FileMode
}

// Enumerate 把 effective profile 转换为确定排序的结构性 desired entries。
// 它只读取 source 树，不读取或修改 target，也不执行模板解析、文件系统身份或控制面校验。
func (p ResolvedProfile) Enumerate(home string) ([]DesiredEntry, error) {
	if home == "" || !filepath.IsAbs(home) {
		return nil, fmt.Errorf("effective HOME must be a non-empty absolute path")
	}
	home = filepath.Clean(home)

	modules := append([]ResolvedModule(nil), p.Modules...)
	slices.SortFunc(modules, func(left, right ResolvedModule) int {
		if order := strings.Compare(left.Name, right.Name); order != 0 {
			return order
		}
		return strings.Compare(left.SourceDir, right.SourceDir)
	})

	entries := make([]DesiredEntry, 0)
	for _, module := range modules {
		moduleEntries, err := enumerateModuleDesired(module, home)
		if err != nil {
			return nil, err
		}
		entries = append(entries, moduleEntries...)
	}
	slices.SortFunc(entries, func(left, right DesiredEntry) int {
		if order := strings.Compare(left.Module, right.Module); order != 0 {
			return order
		}
		return strings.Compare(left.Source, right.Source)
	})
	return entries, nil
}

func enumerateModuleDesired(module ResolvedModule, home string) ([]DesiredEntry, error) {
	if !manifestNamePattern.MatchString(module.Name) {
		return nil, fmt.Errorf("invalid resolved module name %q", module.Name)
	}
	if module.SourceDir == "" || !filepath.IsAbs(module.SourceDir) {
		return nil, fmt.Errorf("module %q source directory must be a non-empty absolute path", module.Name)
	}
	if err := validateTargetPath(module.TargetRoot); err != nil {
		return nil, fmt.Errorf("module %q target root: %w", module.Name, err)
	}

	rules, err := indexFileRules(module)
	if err != nil {
		return nil, err
	}
	sources, err := enumerateModuleSources(module)
	if err != nil {
		return nil, err
	}

	usedRules := make(map[string]struct{}, len(rules))
	entries := make([]DesiredEntry, 0, len(sources))
	for _, source := range sources {
		rule, explicit := rules[source.path]
		if !explicit && source.ignored {
			continue
		}
		if explicit {
			usedRules[source.path] = struct{}{}
		}

		kind, modeText, targetOverride, err := classifyDesiredSource(module.Name, source.path, rule, explicit)
		if err != nil {
			return nil, err
		}
		mode, err := parseDesiredMode(module.Name, source.path, kind, modeText)
		if err != nil {
			return nil, err
		}
		target, err := desiredTarget(module, source.path, targetOverride)
		if err != nil {
			return nil, err
		}

		entries = append(entries, DesiredEntry{
			Module:     module.Name,
			Source:     source.path,
			SourcePath: filepath.Join(filepath.Clean(module.SourceDir), filepath.FromSlash(source.path)),
			Target:     target,
			TargetPath: expandDesiredTarget(home, target),
			Kind:       kind,
			Mode:       mode,
		})
	}

	for _, source := range sortedKeys(rules) {
		if _, used := usedRules[source]; !used {
			return nil, fmt.Errorf("module %q file rule source %q is excluded by a built-in ignore", module.Name, source)
		}
	}
	return entries, nil
}

func indexFileRules(module ResolvedModule) (map[string]ResolvedFileRule, error) {
	rules := make(map[string]ResolvedFileRule, len(module.FileRules))
	for _, rule := range module.FileRules {
		normalized, err := normalizeModulePath(rule.Source)
		if err != nil || normalized != rule.Source {
			return nil, fmt.Errorf("module %q has non-canonical file rule source %q", module.Name, rule.Source)
		}
		if _, exists := rules[rule.Source]; exists {
			return nil, fmt.Errorf("module %q has duplicate file rule source %q", module.Name, rule.Source)
		}
		rules[rule.Source] = rule
	}
	return rules, nil
}

func classifyDesiredSource(
	module, source string,
	rule ResolvedFileRule,
	explicit bool,
) (FileKind, string, string, error) {
	if explicit {
		switch rule.Kind {
		case FileKindLink, FileKindScaffold:
			return rule.Kind, rule.Mode, rule.TargetOverride, nil
		default:
			return "", "", "", fmt.Errorf("module %q source %q has unsupported kind %q", module, source, rule.Kind)
		}
	}
	if strings.HasSuffix(source, ".tmpl") {
		return "", "", "", fmt.Errorf("module %q source %q resolves to managed, which requires M2", module, source)
	}
	if strings.HasSuffix(source, ".template") {
		return FileKindScaffold, defaultScaffoldMode, "", nil
	}
	return FileKindLink, "", "", nil
}

func parseDesiredMode(module, source string, kind FileKind, raw string) (fs.FileMode, error) {
	switch kind {
	case FileKindLink:
		if raw != "" {
			return 0, fmt.Errorf("module %q link source %q must not declare mode %q", module, source, raw)
		}
		return 0, nil
	case FileKindScaffold:
		if raw == "" {
			raw = defaultScaffoldMode
		}
		if !modePattern.MatchString(raw) {
			return 0, fmt.Errorf("module %q scaffold source %q has invalid mode %q", module, source, raw)
		}
		value, err := strconv.ParseUint(raw, 8, 12)
		if err != nil {
			return 0, fmt.Errorf("module %q scaffold source %q parse mode %q: %w", module, source, raw, err)
		}
		return fs.FileMode(value), nil
	default:
		return 0, fmt.Errorf("module %q source %q has unsupported kind %q", module, source, kind)
	}
}

func desiredTarget(module ResolvedModule, source, override string) (string, error) {
	derived, err := stripTemplateSuffix(source)
	if err != nil {
		return "", fmt.Errorf("module %q source %q: %w", module.Name, source, err)
	}

	target := override
	if target == "" {
		target = module.TargetRoot + "/" + derived
		if module.TargetRoot == "~" {
			target = "~/" + derived
		}
	}
	if err := validateEntryTargetPath(target); err != nil {
		return "", fmt.Errorf("module %q source %q target: %w", module.Name, source, err)
	}
	if !isLexicalTargetDescendant(module.TargetRoot, target) {
		return "", fmt.Errorf(
			"module %q source %q target %q must be a true descendant of target root %q",
			module.Name,
			source,
			target,
			module.TargetRoot,
		)
	}
	return target, nil
}

func stripTemplateSuffix(source string) (string, error) {
	result := source
	switch {
	case strings.HasSuffix(result, ".template"):
		result = strings.TrimSuffix(result, ".template")
	case strings.HasSuffix(result, ".tmpl"):
		result = strings.TrimSuffix(result, ".tmpl")
	}
	if result == "" || strings.HasSuffix(result, "/") {
		return "", fmt.Errorf("template suffix removal leaves an empty target basename")
	}
	return result, nil
}

func expandDesiredTarget(home, target string) string {
	if target == "~" {
		return home
	}
	return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(target, "~/")))
}
