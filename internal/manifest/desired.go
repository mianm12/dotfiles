package manifest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	templateengine "github.com/ghstlnx/dotfiles/internal/template"
)

// DesiredEntry 是 render、target 身份校验和 planner 消费的结构性期望值。“只读”描述
// enumerate 的 IO 边界和下游消费约定；该类型仍是普通 Go 值，每次调用返回彼此独立的结果。
// Source 是规范化模块相对路径，Target 是规范化 ~/ 展示路径；对应的 Path 字段是绝对路径。
// Mode 与 Content 仅对 scaffold 有效；link 的 Mode 恒为零且 Content 为 nil。
type DesiredEntry struct {
	Module     string
	Source     string
	SourcePath string
	Target     string
	TargetPath string
	Kind       FileKind
	Mode       fs.FileMode
	Content    []byte
}

// RuntimeContext 是 desired 形成时唯一允许传给模板的显式运行输入。
type RuntimeContext struct {
	OS       string
	Arch     string
	Hostname string
	Profile  string
	Home     string
	Data     map[string]string
}

// Enumerate 把 effective profile 转换为确定排序且已完成 scaffold 渲染的 desired entries。
// 它只读取 source 树，不读取或修改 target，也不执行文件系统身份或控制面校验。任一模板
// parse、变量或渲染错误都返回 nil，planner 不会看到部分结果。
func (p ResolvedProfile) Enumerate(context RuntimeContext) ([]DesiredEntry, error) {
	renderContext, err := p.validateRuntimeContext(context)
	if err != nil {
		return nil, err
	}
	entries, err := p.enumerateStructure(renderContext.Home)
	if err != nil {
		return nil, err
	}
	return renderScaffolds(entries, p.dataKeys, renderContext)
}

// ValidateTemplates 为 doctor 等静态消费者检查 scaffold 的语法、函数与变量引用。
// 它不需要运行 data，也不渲染模板或读取 target。
func (p ResolvedProfile) ValidateTemplates() error {
	if !isSupportedGOOS(p.goos) {
		return fmt.Errorf("resolved profile has unsupported GOOS %q", p.goos)
	}
	entries, err := p.enumerateStructure(string(filepath.Separator))
	if err != nil {
		return err
	}
	return validateScaffolds(entries, p.dataKeys)
}

func (p ResolvedProfile) enumerateStructure(home string) ([]DesiredEntry, error) {
	// 不依赖 Resolve 的既有顺序，也不原地排序 receiver，保证其他合法构造方得到同样结果。
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

func (p ResolvedProfile) validateRuntimeContext(context RuntimeContext) (templateengine.Context, error) {
	if !isSupportedGOOS(p.goos) {
		return templateengine.Context{}, fmt.Errorf("resolved profile has unsupported GOOS %q", p.goos)
	}
	if context.OS != p.goos {
		return templateengine.Context{}, fmt.Errorf(
			"runtime OS %q does not match resolved profile OS %q",
			context.OS,
			p.goos,
		)
	}
	if context.Profile != p.Name {
		return templateengine.Context{}, fmt.Errorf(
			"runtime profile %q does not match resolved profile %q",
			context.Profile,
			p.Name,
		)
	}
	if context.Arch != "arm64" && context.Arch != "amd64" {
		return templateengine.Context{}, fmt.Errorf("runtime architecture %q is not supported", context.Arch)
	}
	if context.Home == "" || !filepath.IsAbs(context.Home) {
		return templateengine.Context{}, fmt.Errorf("effective HOME must be a non-empty absolute path")
	}
	return templateengine.Context{
		OS:       context.OS,
		Arch:     context.Arch,
		Hostname: context.Hostname,
		Profile:  context.Profile,
		Home:     filepath.Clean(context.Home),
		Data:     context.Data,
	}, nil
}

func renderScaffolds(
	entries []DesiredEntry,
	dataKeys []string,
	context templateengine.Context,
) ([]DesiredEntry, error) {
	rendered := append([]DesiredEntry(nil), entries...)
	for index := range rendered {
		entry := &rendered[index]
		if entry.Kind != FileKindScaffold {
			continue
		}
		parsed, err := loadScaffoldTemplate(*entry, dataKeys)
		if err != nil {
			return nil, err
		}
		content, err := parsed.Render(dataKeys, context)
		if err != nil {
			return nil, fmt.Errorf(
				"module %q scaffold source %q: %w",
				entry.Module,
				entry.Source,
				err,
			)
		}
		entry.Content = content
	}
	return rendered, nil
}

func validateScaffolds(entries []DesiredEntry, dataKeys []string) error {
	for _, entry := range entries {
		if entry.Kind != FileKindScaffold {
			continue
		}
		if _, err := loadScaffoldTemplate(entry, dataKeys); err != nil {
			return err
		}
	}
	return nil
}

func loadScaffoldTemplate(entry DesiredEntry, dataKeys []string) (*templateengine.Template, error) {
	source, err := os.ReadFile(entry.SourcePath)
	if err != nil {
		return nil, fmt.Errorf(
			"read scaffold template for module %q source %q: %w",
			entry.Module,
			entry.Source,
			err,
		)
	}
	parsed, err := templateengine.Parse(entry.Module+"/"+entry.Source, source)
	if err != nil {
		return nil, fmt.Errorf(
			"module %q scaffold source %q: %w",
			entry.Module,
			entry.Source,
			err,
		)
	}
	if err := parsed.ValidateVariables(dataKeys); err != nil {
		return nil, fmt.Errorf(
			"module %q scaffold source %q: %w",
			entry.Module,
			entry.Source,
			err,
		)
	}
	return parsed, nil
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
		// source 层已移除不可覆盖的内置 ignore；这里表达 [files] > 用户 ignore。
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

	// 正常 Resolve 结果中的规则都应命中普通 source；拒绝被外部修改后试图覆盖
	// 内置 ignore 的结构，避免把无效显式声明静默丢弃。
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

// classifyDesiredSource 只处理优先级的最后两层：[files] 显式声明和文件名后缀推断。
// 内置 ignore 已在 source 层移除，用户 ignore 已由调用方处理。
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
			if string(rule.Kind) == managedFileKindName {
				return "", "", "", fmt.Errorf("module %q source %q: %w", module, source, ErrManagedUnsupported)
			}
			return "", "", "", fmt.Errorf("module %q source %q has unsupported kind %q", module, source, rule.Kind)
		}
	}
	if strings.HasSuffix(source, ".tmpl") {
		return "", "", "", fmt.Errorf("module %q source %q resolves to managed: %w", module, source, ErrManagedUnsupported)
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
	target := override
	if target == "" {
		// [files].target 是完整 entry target；只有未声明时才从 source 去后缀派生。
		derived, err := stripTemplateSuffix(source)
		if err != nil {
			return "", fmt.Errorf("module %q source %q: %w", module.Name, source, err)
		}
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
