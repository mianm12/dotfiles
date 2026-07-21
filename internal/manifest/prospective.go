package manifest

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mianm12/dotfiles/internal/paths"
)

// ProspectiveSource 是 add 尚未发布、但预检需要按已存在普通文件解释的精确 source。
// Content 与 Mode 来自输入快照；调用方仍须在执行提交时维持该快照前提。
type ProspectiveSource struct {
	Module  string
	Source  string
	Content []byte
	Mode    fs.FileMode
}

type prospectiveSourceData struct {
	content []byte
	mode    fs.FileMode
}

type prospectiveSources map[string]map[string]prospectiveSourceData

// ProspectiveCandidate 是一个 target 在 effective module 下可能发布的 source 位置。
type ProspectiveCandidate struct {
	Module string
	Source string
}

// ProspectiveCandidates 使用 effective target root、显式 [files] 与后缀规则反向枚举候选。
// 返回值按 module/source 字节序稳定排序；它不读取或修改 source/target。
func (p ResolvedProfile) ProspectiveCandidates(
	home string,
	targetPath string,
	kind FileKind,
) ([]ProspectiveCandidate, error) {
	cleanHome, err := cleanEffectiveHome(home)
	if err != nil {
		return nil, err
	}
	if targetPath == "" || !filepath.IsAbs(targetPath) {
		return nil, fmt.Errorf("prospective target path must be a non-empty absolute path")
	}
	cleanTarget := filepath.Clean(targetPath)
	if kind != FileKindLink && kind != FileKindScaffold {
		return nil, fmt.Errorf("unsupported prospective kind %q", kind)
	}

	result := make([]ProspectiveCandidate, 0)
	seen := make(map[ProspectiveCandidate]struct{})
	for _, module := range p.modules {
		rootPath := expandDesiredTarget(cleanHome, module.TargetRoot)
		relative, ok := descendantRelativePath(rootPath, cleanTarget)
		if !ok {
			continue
		}
		defaultSource := filepath.ToSlash(relative)
		if kind == FileKindScaffold {
			defaultSource += ".template"
		}
		if candidateMatches(module, defaultSource, cleanTarget, cleanHome, kind) {
			candidate := ProspectiveCandidate{Module: module.Name, Source: defaultSource}
			seen[candidate] = struct{}{}
		}
		for _, rule := range module.FileRules {
			if rule.Kind != kind {
				continue
			}
			target, targetErr := desiredTarget(module, rule.Source, rule.TargetOverride)
			if targetErr != nil {
				return nil, targetErr
			}
			if expandDesiredTarget(cleanHome, target) != cleanTarget {
				continue
			}
			candidate := ProspectiveCandidate{Module: module.Name, Source: rule.Source}
			seen[candidate] = struct{}{}
		}
	}
	for candidate := range seen {
		result = append(result, candidate)
	}
	slices.SortFunc(result, func(left, right ProspectiveCandidate) int {
		if order := strings.Compare(left.Module, right.Module); order != 0 {
			return order
		}
		return strings.Compare(left.Source, right.Source)
	})
	return result, nil
}

func candidateMatches(module ResolvedModule, source, targetPath, home string, kind FileKind) bool {
	rules, err := indexFileRules(module)
	if err != nil {
		return false
	}
	rule, explicit := rules[source]
	effectiveKind, _, override, err := classifyDesiredSource(module.Name, source, rule, explicit)
	if err != nil || effectiveKind != kind {
		return false
	}
	target, err := desiredTarget(module, source, override)
	return err == nil && expandDesiredTarget(home, target) == targetPath
}

func descendantRelativePath(root, target string) (string, bool) {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relative, true
}

// ValidateProspectivePathBoundaries 只把 candidates 中的精确 source 视为虚拟普通文件，
// 再执行与正常 profile 相同的严格枚举和完整路径边界校验。真实仓库不会被修改。
func (p ResolvedProfile) ValidateProspectivePathBoundaries(
	controlPaths paths.ControlPlanePaths,
	candidates []ProspectiveSource,
) (ValidatedProfile, error) {
	prospective, err := p.prepareProspectiveSources(candidates)
	if err != nil {
		return ValidatedProfile{}, err
	}
	home, err := cleanEffectiveHome(controlPaths.EffectiveHome())
	if err != nil {
		return ValidatedProfile{}, err
	}
	entries, targets, err := p.targetStructure(home, prospective)
	if err != nil {
		return ValidatedProfile{}, err
	}
	if err := validateProspectiveCoverage(entries, prospective); err != nil {
		return ValidatedProfile{}, err
	}
	if _, err := paths.ValidatePathBoundaries(controlPaths, targets); err != nil {
		return ValidatedProfile{}, fmt.Errorf("resolved profile %q prospective path boundaries: %w", p.name, err)
	}
	return ValidatedProfile{
		name:     p.name,
		goos:     p.goos,
		home:     home,
		dataKeys: append([]string(nil), p.dataKeys...),
		modules:  cloneResolvedModules(p.modules),
		entries:  entries,
	}, nil
}

func (p ResolvedProfile) prepareProspectiveSources(candidates []ProspectiveSource) (prospectiveSources, error) {
	effective := make(map[string]struct{}, len(p.modules))
	for _, module := range p.modules {
		effective[module.Name] = struct{}{}
	}
	result := make(prospectiveSources)
	for _, candidate := range candidates {
		if _, ok := effective[candidate.Module]; !ok {
			return nil, fmt.Errorf("prospective source module %q is not effective in profile %q", candidate.Module, p.name)
		}
		normalized, err := normalizeModulePath(candidate.Source)
		if err != nil || normalized != candidate.Source {
			return nil, fmt.Errorf("module %q prospective source %q is not canonical", candidate.Module, candidate.Source)
		}
		if candidate.Mode.Type() != 0 || candidate.Mode&^(fs.ModePerm) != 0 {
			return nil, fmt.Errorf(
				"module %q prospective source %q mode %s is not a regular-file mode",
				candidate.Module,
				candidate.Source,
				candidate.Mode,
			)
		}
		moduleSources := result[candidate.Module]
		if moduleSources == nil {
			moduleSources = make(map[string]prospectiveSourceData)
			result[candidate.Module] = moduleSources
		}
		if _, exists := moduleSources[candidate.Source]; exists {
			return nil, fmt.Errorf("duplicate prospective source %q for module %q", candidate.Source, candidate.Module)
		}
		moduleSources[candidate.Source] = prospectiveSourceData{
			content: append([]byte(nil), candidate.Content...),
			mode:    candidate.Mode,
		}
	}
	return result, nil
}

func validateProspectiveCoverage(entries []DesiredEntry, prospective prospectiveSources) error {
	covered := make(map[string]map[string]int, len(prospective))
	for _, entry := range entries {
		if !entry.prospective {
			continue
		}
		if covered[entry.Module] == nil {
			covered[entry.Module] = make(map[string]int)
		}
		covered[entry.Module][entry.Source]++
	}
	for module, sources := range prospective {
		for source := range sources {
			if covered[module][source] != 1 {
				return fmt.Errorf(
					"module %q prospective source %q produced %d desired entries; want exactly one",
					module,
					source,
					covered[module][source],
				)
			}
		}
	}
	return nil
}

type prospectiveFileInfo struct {
	name string
	mode fs.FileMode
	size int64
}

func (info prospectiveFileInfo) Name() string       { return info.name }
func (info prospectiveFileInfo) Size() int64        { return info.size }
func (info prospectiveFileInfo) Mode() fs.FileMode  { return info.mode }
func (info prospectiveFileInfo) ModTime() time.Time { return time.Time{} }
func (info prospectiveFileInfo) IsDir() bool        { return false }
func (info prospectiveFileInfo) Sys() any           { return nil }

var _ fs.FileInfo = prospectiveFileInfo{}
