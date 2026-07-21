package add

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/mianm12/dotfiles/internal/state"
)

type operations struct {
	git      gitRunner
	hostname func() (string, error)
	getwd    func() (string, error)
	environ  func() []string
}

func defaultOperations() operations {
	return operations{git: runSystemGit, hostname: os.Hostname, getwd: os.Getwd, environ: os.Environ}
}

// Preflight 只读建立完整 add batch plan，不获取 lock，也不写 source、target 或 state。
func Preflight(inputs dotruntime.LoadedInputs, request Request) (BatchPlan, error) {
	return preflight(inputs, request, defaultOperations())
}

func preflight(inputs dotruntime.LoadedInputs, request Request, operations operations) (BatchPlan, error) {
	if request.Mode == ModeTemplate {
		return BatchPlan{}, ErrTemplateUnsupported
	}
	kind, err := requestedKind(request.Mode)
	if err != nil {
		return BatchPlan{}, err
	}
	if len(request.Paths) == 0 {
		return BatchPlan{}, fmt.Errorf("add requires at least one path")
	}
	if operations.git == nil || operations.hostname == nil || operations.getwd == nil || operations.environ == nil {
		return BatchPlan{}, fmt.Errorf("add preflight operations are unavailable")
	}

	context := inputs.Context()
	control := context.Control().Paths()
	home := control.EffectiveHome()
	repositoryPath := control.Repository()
	resolved, err := inputs.Manifest().Resolve(context.Profile(), goruntime.GOOS)
	if err != nil {
		return BatchPlan{}, err
	}
	if err := validateExplicitModule(inputs.Manifest(), resolved, request.Module); err != nil {
		return BatchPlan{}, err
	}
	cwd, err := operations.getwd()
	if err != nil {
		return BatchPlan{}, fmt.Errorf("resolve current directory for add inputs: %w", err)
	}

	targets := make([]paths.LabeledTarget, 0, len(request.Paths))
	normalizedInputs := make([]normalizedInput, 0, len(request.Paths))
	for _, raw := range request.Paths {
		targetPath, err := normalizeInput(raw, home, cwd)
		if err != nil {
			return BatchPlan{}, err
		}
		if strings.HasSuffix(filepath.Base(targetPath), ".local") {
			return BatchPlan{}, fmt.Errorf("add input %q matches forbidden *.local path", targetPath)
		}
		target := displayTarget(home, targetPath)
		normalizedInputs = append(normalizedInputs, normalizedInput{target: target, targetPath: targetPath})
		targets = append(targets, paths.LabeledTarget{Label: "add input " + target, Path: targetPath})
	}
	// 先对全批次路径证明 control/identity/topology 边界，之后才读取输入内容或启动 Git。
	if _, err := paths.ValidatePathBoundaries(control, targets); err != nil {
		return BatchPlan{}, fmt.Errorf("validate add input boundaries: %w", err)
	}
	inputsToPlan := make([]inputSnapshot, 0, len(normalizedInputs))
	for _, input := range normalizedInputs {
		snapshot, err := snapshotInput(input.targetPath)
		if err != nil {
			return BatchPlan{}, err
		}
		if err := rejectExistingState(inputs.State(), home, input.targetPath, snapshot.identity); err != nil {
			return BatchPlan{}, err
		}
		inputsToPlan = append(inputsToPlan, inputSnapshot{
			target: input.target, targetPath: input.targetPath, snapshot: snapshot,
		})
	}

	provisional := make([]provisionalItem, 0, len(inputsToPlan))
	families := make([]sourceFamily, 0, len(inputsToPlan))
	for _, input := range inputsToPlan {
		candidate, err := selectCandidate(resolved, inputs.State(), home, input.targetPath, request.Module, kind)
		if err != nil {
			return BatchPlan{}, err
		}
		sourcePath := filepath.Join(repositoryPath, "modules", candidate.Module, filepath.FromSlash(candidate.Source))
		repositorySource := filepath.ToSlash(filepath.Join("modules", candidate.Module, filepath.FromSlash(candidate.Source)))
		provisional = append(provisional, provisionalItem{
			plan: ItemPlan{
				target: input.target, targetPath: input.targetPath, module: candidate.Module,
				source: candidate.Source, sourcePath: sourcePath, kind: kind,
				snapshot: input.snapshot,
			},
			repositorySource: repositorySource,
		})
		families = append(families, sourceFamily{input: input.targetPath, paths: sourceVariantPaths(sourcePath)})
	}
	if err := validateSourceFamilies(families); err != nil {
		return BatchPlan{}, err
	}

	items := make([]ItemPlan, 0, len(provisional))
	prospective := make([]manifest.ProspectiveSource, 0, len(provisional))
	gitEnv := gitEnvironment(operations.environ(), home)
	for _, candidate := range provisional {
		sourceExists, err := validateSourceVariants(candidate.plan.sourcePath, candidate.plan.snapshot)
		if err != nil {
			return BatchPlan{}, err
		}
		if err := gitTrackable(operations.git, repositoryPath, gitEnv, candidate.repositorySource); err != nil {
			return BatchPlan{}, err
		}
		if !sourceExists {
			prospective = append(prospective, manifest.ProspectiveSource{
				Module: candidate.plan.module, Source: candidate.plan.source,
				Content: candidate.plan.snapshot.content, Mode: candidate.plan.snapshot.mode,
			})
		}
		candidate.plan.sourceExists = sourceExists
		items = append(items, candidate.plan)
	}
	validated, err := resolved.ValidateProspectivePathBoundaries(control, prospective)
	if err != nil {
		return BatchPlan{}, err
	}
	hostname, err := operations.hostname()
	if err != nil {
		return BatchPlan{}, fmt.Errorf("resolve hostname for add rendering: %w", err)
	}
	scoped, err := validated.RenderScope(nil, manifest.RuntimeContext{
		OS: goruntime.GOOS, Arch: goruntime.GOARCH, Hostname: hostname,
		Profile: context.Profile(), Home: home, Data: context.Data(),
	})
	if err != nil {
		return BatchPlan{}, err
	}
	if err := validateDesiredItems(items, scoped.Entries()); err != nil {
		return BatchPlan{}, err
	}
	slices.SortFunc(items, func(left, right ItemPlan) int {
		return strings.Compare(left.targetPath, right.targetPath)
	})
	plan := sealBatchPlan(context.Profile(), home, repositoryPath, items)
	if !plan.Valid() {
		return BatchPlan{}, fmt.Errorf("add preflight produced an invalid sealed plan")
	}
	return plan, nil
}

type inputSnapshot struct {
	target     string
	targetPath string
	snapshot   Snapshot
}

type normalizedInput struct {
	target     string
	targetPath string
}

type provisionalItem struct {
	plan             ItemPlan
	repositorySource string
}

type sourceFamily struct {
	input string
	paths []string
}

func validateSourceFamilies(families []sourceFamily) error {
	targets := make([]paths.LabeledTarget, 0, len(families)*3)
	for _, family := range families {
		for _, variant := range family.paths {
			targets = append(targets, paths.LabeledTarget{
				Label: fmt.Sprintf("add input %q source variant", family.input),
				Path:  variant,
			})
		}
	}
	if _, err := paths.ValidateTargetSet(targets); err != nil {
		return fmt.Errorf("validate add batch source variant families: %w", err)
	}
	return nil
}

func requestedKind(mode Mode) (manifest.FileKind, error) {
	switch mode {
	case "", ModeLink:
		return manifest.FileKindLink, nil
	case ModeScaffold:
		return manifest.FileKindScaffold, nil
	default:
		return "", fmt.Errorf("unsupported add mode %q", mode)
	}
}

func validateExplicitModule(repository manifest.Repository, resolved manifest.ResolvedProfile, module string) error {
	if module == "" {
		return nil
	}
	if !manifest.ValidModuleName(module) {
		return fmt.Errorf("invalid add module name %q", module)
	}
	if !slices.Contains(repository.ModuleNames(), module) {
		return fmt.Errorf("add module %q does not exist", module)
	}
	if !slices.Contains(resolved.ModuleNames(), module) {
		return fmt.Errorf("add module %q is not in the effective profile", module)
	}
	return nil
}

func normalizeInput(raw, home, cwd string) (string, error) {
	var target string
	switch {
	case raw == "~":
		target = home
	case strings.HasPrefix(raw, "~/"):
		target = filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(raw, "~/")))
	case strings.HasPrefix(raw, "~"):
		return "", fmt.Errorf("unsupported add input path %q", raw)
	case filepath.IsAbs(raw):
		target = raw
	default:
		target = filepath.Join(cwd, raw)
	}
	target = filepath.Clean(target)
	if _, ok := descendantRelative(home, target); !ok {
		return "", fmt.Errorf("add input %q must be a true descendant of HOME %q", target, home)
	}
	return target, nil
}

func snapshotInput(target string) (Snapshot, error) {
	before, err := os.Lstat(target)
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect add input %q: %w", target, err)
	}
	if !before.Mode().IsRegular() {
		return Snapshot{}, fmt.Errorf("add input %q is not an ordinary file", target)
	}
	file, err := os.Open(target)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open add input %q: %w", target, err)
	}
	openedBefore, statBeforeErr := file.Stat()
	content, readErr := io.ReadAll(file)
	openedAfter, statAfterErr := file.Stat()
	closeErr := file.Close()
	if statBeforeErr != nil {
		return Snapshot{}, fmt.Errorf("inspect opened add input %q before read: %w", target, statBeforeErr)
	}
	if readErr != nil {
		return Snapshot{}, fmt.Errorf("read add input %q: %w", target, readErr)
	}
	if statAfterErr != nil {
		return Snapshot{}, fmt.Errorf("inspect opened add input %q after read: %w", target, statAfterErr)
	}
	if closeErr != nil {
		return Snapshot{}, fmt.Errorf("close add input %q: %w", target, closeErr)
	}
	after, err := os.Lstat(target)
	if err != nil {
		return Snapshot{}, fmt.Errorf("reinspect add input %q: %w", target, err)
	}
	if !openedBefore.Mode().IsRegular() || !openedAfter.Mode().IsRegular() || !after.Mode().IsRegular() ||
		!os.SameFile(before, openedBefore) || !os.SameFile(openedBefore, openedAfter) || !os.SameFile(openedAfter, after) ||
		before.Mode().Perm() != openedBefore.Mode().Perm() || openedBefore.Mode().Perm() != openedAfter.Mode().Perm() ||
		openedAfter.Mode().Perm() != after.Mode().Perm() || openedBefore.Size() != openedAfter.Size() ||
		!openedBefore.ModTime().Equal(openedAfter.ModTime()) {
		return Snapshot{}, fmt.Errorf("add input %q changed while taking snapshot", target)
	}
	identity, err := paths.ResolveTargetIdentity(target)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve add input %q identity: %w", target, err)
	}
	return Snapshot{content: content, mode: after.Mode().Perm(), identity: identity}, nil
}

func rejectExistingState(loaded state.Loaded, home, target string, identity paths.TargetIdentity) error {
	snapshot, ok := loaded.Snapshot()
	if !ok {
		return nil
	}
	for _, key := range snapshot.EntryKeys() {
		statePath := filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(key, "~/")))
		stateIdentity, err := paths.ResolveTargetIdentity(statePath)
		if err != nil {
			return fmt.Errorf("resolve state target %q for add: %w", key, err)
		}
		if identity.Equal(stateIdentity) {
			return fmt.Errorf("add input %q already has state entry %q", target, key)
		}
	}
	return nil
}

func selectCandidate(
	resolved manifest.ResolvedProfile,
	loaded state.Loaded,
	home, target, explicitModule string,
	kind manifest.FileKind,
) (manifest.ProspectiveCandidate, error) {
	candidates, err := resolved.ProspectiveCandidates(home, target, kind)
	if err != nil {
		return manifest.ProspectiveCandidate{}, err
	}
	if explicitModule != "" {
		selected := filterCandidates(candidates, explicitModule)
		if len(selected) != 1 {
			return manifest.ProspectiveCandidate{}, fmt.Errorf(
				"%w: module %q maps target %q to %d sources",
				ErrModuleAmbiguous,
				explicitModule,
				target,
				len(selected),
			)
		}
		return selected[0], nil
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	evidence := stateEvidenceModules(loaded, home, target, candidates)
	if len(evidence) == 1 {
		selected := filterCandidates(candidates, evidence[0])
		if len(selected) == 1 {
			return selected[0], nil
		}
	}
	return manifest.ProspectiveCandidate{}, fmt.Errorf(
		"%w: target %q has %d prospective sources; specify -m",
		ErrModuleAmbiguous,
		target,
		len(candidates),
	)
}

func filterCandidates(candidates []manifest.ProspectiveCandidate, module string) []manifest.ProspectiveCandidate {
	result := make([]manifest.ProspectiveCandidate, 0, 1)
	for _, candidate := range candidates {
		if candidate.Module == module {
			result = append(result, candidate)
		}
	}
	return result
}

func stateEvidenceModules(
	loaded state.Loaded,
	home, target string,
	candidates []manifest.ProspectiveCandidate,
) []string {
	snapshot, ok := loaded.Snapshot()
	if !ok {
		return nil
	}
	candidateModules := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidateModules[candidate.Module] = struct{}{}
	}
	targetDisplay := displayTarget(home, target)
	targetDirectory := path.Dir(targetDisplay)
	evidence := make(map[string]struct{})
	for _, key := range snapshot.EntryKeys() {
		entry, _ := snapshot.Entry(key)
		if _, candidate := candidateModules[entry.Module()]; !candidate {
			continue
		}
		stateDirectory := path.Dir(key)
		if stateDirectory == targetDirectory || strings.HasPrefix(targetDirectory, stateDirectory+"/") {
			evidence[entry.Module()] = struct{}{}
		}
	}
	modules := make([]string, 0, len(evidence))
	for module := range evidence {
		modules = append(modules, module)
	}
	slices.Sort(modules)
	return modules
}

func validateSourceVariants(sourcePath string, input Snapshot) (bool, error) {
	variants := sourceVariantPaths(sourcePath)
	expectedExists := false
	for _, variant := range variants {
		info, err := os.Lstat(variant)
		if err != nil {
			if paths.IsMissing(variant, err) {
				continue
			}
			return false, fmt.Errorf("inspect source variant %q: %w", variant, err)
		}
		if variant != sourcePath {
			return false, fmt.Errorf("source variant %q already exists", variant)
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("source variant %q is not an ordinary file", variant)
		}
		content, err := os.ReadFile(variant)
		if err != nil {
			return false, fmt.Errorf("read source variant %q: %w", variant, err)
		}
		if !bytes.Equal(content, input.content) || info.Mode().Perm() != input.mode {
			return false, fmt.Errorf("source variant %q is not equivalent to add input", variant)
		}
		expectedExists = true
	}
	return expectedExists, nil
}

func sourceVariantPaths(sourcePath string) []string {
	base := sourcePath
	if strings.HasSuffix(base, ".template") {
		base = strings.TrimSuffix(base, ".template")
	} else if strings.HasSuffix(base, ".tmpl") {
		base = strings.TrimSuffix(base, ".tmpl")
	}
	return []string{base, base + ".tmpl", base + ".template"}
}

func validateDesiredItems(items []ItemPlan, entries []manifest.DesiredEntry) error {
	for _, item := range items {
		matches := make([]manifest.DesiredEntry, 0, 1)
		for _, entry := range entries {
			if entry.Module == item.module && entry.Source == item.source {
				matches = append(matches, entry)
			}
		}
		if len(matches) != 1 {
			return fmt.Errorf("add candidate %s/%s produced %d desired entries; want one", item.module, item.source, len(matches))
		}
		entry := matches[0]
		if filepath.Clean(entry.TargetPath) != item.targetPath || entry.Kind != item.kind {
			return fmt.Errorf(
				"add candidate %s/%s resolves to target %q kind %q; want %q kind %q",
				item.module,
				item.source,
				entry.TargetPath,
				entry.Kind,
				item.targetPath,
				item.kind,
			)
		}
		if item.kind == manifest.FileKindScaffold &&
			(!bytes.Equal(entry.Content, item.snapshot.content) || entry.Mode != item.snapshot.mode) {
			return fmt.Errorf("add scaffold %q rendered bytes or mode do not match input snapshot", item.targetPath)
		}
	}
	return nil
}

func displayTarget(home, target string) string {
	relative, _ := descendantRelative(home, target)
	return "~/" + filepath.ToSlash(relative)
}

func descendantRelative(root, target string) (string, bool) {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relative, true
}
