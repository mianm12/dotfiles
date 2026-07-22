package runtime

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mianm12/dotfiles/internal/config"
	"github.com/mianm12/dotfiles/internal/lock"
	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/state"
)

// ErrRequiresUnsatisfied 表示当前发布构建低于 strict manifest 声明的最低版本。
var ErrRequiresUnsatisfied = errors.New("CLI does not satisfy manifest requires")

// Compatibility 是 strict manifest 实际 requirement 的兼容性结果。
type Compatibility struct {
	requirement      manifest.Requirement
	developmentBuild bool
}

// Requirement 返回 strict manifest 实际声明的版本要求。
func (compatibility Compatibility) Requirement() manifest.Requirement {
	return compatibility.requirement
}

// DevelopmentBuild 报告当前构建是否按 dev 规则跳过版本大小比较。
func (compatibility Compatibility) DevelopmentBuild() bool { return compatibility.developmentBuild }

// LoadedInputs 保存完整 runtime 加载后的可信只读输入。
type LoadedInputs struct {
	context       RunContext
	compatibility Compatibility
	repository    manifest.Repository
	state         state.Loaded
}

// Context 返回本次运行的严格 preflight 结果。
func (inputs LoadedInputs) Context() RunContext { return inputs.context }

// Compatibility 返回 strict manifest 的兼容性结果。
func (inputs LoadedInputs) Compatibility() Compatibility { return inputs.compatibility }

// Manifest 返回严格加载的仓库 manifest。
func (inputs LoadedInputs) Manifest() manifest.Repository { return inputs.repository }

// State 返回缺失或严格加载的 state 联合结果。
func (inputs LoadedInputs) State() state.Loaded { return inputs.state }

// InitInputs 保存 init 配置阶段所需的 preflight 与 strict manifest，不包含 state。
type InitInputs struct {
	context       InitContext
	compatibility Compatibility
	repository    manifest.Repository
}

// InitSelection 保存 interaction/非交互层明确选择的 profile 与 data。
// Override.Set 区分省略和显式空字符串。
type InitSelection struct {
	Profile Override
	Data    map[string]Override
}

// InitCandidate 是由一份 immutable preparation inputs 形成并绑定 control context 的候选。
type InitCandidate struct {
	valid          bool
	config         config.Candidate
	configPath     string
	repositoryPath string
}

// Machine 返回候选 machine config 的独立副本。
func (candidate InitCandidate) Machine() config.Machine { return candidate.config.Machine() }

// Bytes 返回候选的确定性 TOML 字节副本。
func (candidate InitCandidate) Bytes() []byte { return candidate.config.Bytes() }

// Context 返回 init 的严格 preflight 结果。
func (inputs InitInputs) Context() InitContext { return inputs.context }

// Compatibility 返回 strict manifest 的兼容性结果。
func (inputs InitInputs) Compatibility() Compatibility { return inputs.compatibility }

// Manifest 返回严格加载的仓库 manifest。
func (inputs InitInputs) Manifest() manifest.Repository { return inputs.repository }

// BuildCandidate 从 immutable preparation inputs 纯函数地合并完整 machine config candidate。
func (inputs InitInputs) BuildCandidate(selection InitSelection) (InitCandidate, error) {
	context := inputs.context
	snapshot := context.ConfigSnapshot()
	existing, exists := context.ExistingMachine()

	profile := ""
	switch {
	case selection.Profile.Set:
		profile = selection.Profile.Value
	default:
		if override, ok := context.ProfileOverride(); ok {
			profile = override
		} else if exists {
			profile = existing.Profile()
		}
	}
	if profile == "" {
		return InitCandidate{}, fmt.Errorf("init profile is required")
	}
	if !slices.Contains(inputs.repository.ProfileNames(), profile) {
		return InitCandidate{}, fmt.Errorf("unknown init profile %q", profile)
	}

	declarations := inputs.repository.DataDeclarations()
	declared := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		declared[declaration.Key()] = struct{}{}
	}
	selectedKeys := make([]string, 0, len(selection.Data))
	for key := range selection.Data {
		selectedKeys = append(selectedKeys, key)
	}
	sort.Strings(selectedKeys)
	for _, key := range selectedKeys {
		if _, ok := declared[key]; !ok {
			return InitCandidate{}, fmt.Errorf("unknown init data key %q", key)
		}
	}

	data := map[string]string{}
	if exists {
		data = existing.Data()
	}
	for _, declaration := range declarations {
		key := declaration.Key()
		if selected, ok := selection.Data[key]; ok && selected.Set {
			data[key] = selected.Value
			continue
		}
		if _, ok := data[key]; ok {
			continue
		}
		if defaultValue, ok := declaration.Default(); ok {
			data[key] = defaultValue
			continue
		}
		return InitCandidate{}, fmt.Errorf("init data %q is required", key)
	}

	machine := config.Machine{Profile: profile, Data: data}
	if exists {
		if repo, ok := existing.Repo(); ok {
			machine.Repo = &repo
		}
	}
	switch context.RepositorySource() {
	case paths.RepositorySourceFlag, paths.RepositorySourceEnvironment:
		repository := context.Control().RepositoryPath()
		machine.Repo = &repository
	}
	candidate, err := config.NewCandidate(snapshot, machine)
	if err != nil {
		return InitCandidate{}, err
	}
	control := context.Control()
	return InitCandidate{
		valid:          true,
		config:         candidate,
		configPath:     control.ConfigPath(),
		repositoryPath: control.RepositoryPath(),
	}, nil
}

// LoadReadOnly 加载与完整 mutation 相同的只读输入，但从不获取或创建 lock。
func LoadReadOnly(overrides Overrides, cliVersion string) (LoadedInputs, error) {
	return systemResolver().LoadReadOnly(overrides, cliVersion)
}

// LoadReadOnly 使用 resolver 的明确系统来源加载完整只读输入。
func (resolver Resolver) LoadReadOnly(overrides Overrides, cliVersion string) (LoadedInputs, error) {
	operations := loadingOperationsWithResolver(resolver)
	context, err := operations.preflight(overrides)
	if err != nil {
		return LoadedInputs{}, err
	}
	return loadFull(context, cliVersion, operations)
}

// PrepareInit 在不获取 lock、不读取 state 的前提下严格加载 init config 与 manifest。
func PrepareInit(overrides Overrides, cliVersion string) (InitInputs, error) {
	return systemResolver().PrepareInit(overrides, cliVersion)
}

// PrepareInit 使用 resolver 的明确系统来源执行只读 init preparation。
func (resolver Resolver) PrepareInit(overrides Overrides, cliVersion string) (InitInputs, error) {
	operations := loadingOperationsWithResolver(resolver)
	context, err := operations.preflightInit(overrides)
	if err != nil {
		return InitInputs{}, err
	}
	compatibility, repository, err := loadRepository(context.Control().RepositoryPath(), cliVersion, operations)
	if err != nil {
		return InitInputs{}, err
	}
	return InitInputs{context: context, compatibility: compatibility, repository: repository}, nil
}

// LoadControlRecovery 为 self-update 等 control-only 恢复流程建立只读控制面上下文。
// config missing 合法；已有 config 仍严格校验。本入口不获取锁，也不读取 manifest/state。
func LoadControlRecovery(overrides Overrides) (ControlContext, error) {
	return systemResolver().LoadControlRecovery(overrides)
}

// LoadControlRecovery 使用 resolver 的明确系统来源建立只读控制面上下文。
func (resolver Resolver) LoadControlRecovery(overrides Overrides) (ControlContext, error) {
	operations := loadingOperationsWithResolver(resolver)
	return operations.preflightRepository(overrides)
}

func loadFull(context RunContext, cliVersion string, operations loadingOperations) (LoadedInputs, error) {
	control := context.Control()
	compatibility, repository, err := loadRepository(control.RepositoryPath(), cliVersion, operations)
	if err != nil {
		return LoadedInputs{}, err
	}
	loadedState, err := operations.loadState(control.Paths().StateFile())
	if err != nil {
		return LoadedInputs{}, err
	}
	if snapshot, ok := loadedState.Snapshot(); ok {
		if err := validateLoadedState(context, snapshot, operations); err != nil {
			return LoadedInputs{}, err
		}
	}
	return LoadedInputs{
		context:       context,
		compatibility: compatibility,
		repository:    repository,
		state:         loadedState,
	}, nil
}

func loadRepository(
	repositoryPath string,
	cliVersion string,
	operations loadingOperations,
) (Compatibility, manifest.Repository, error) {
	preRead, err := operations.readRequirement(repositoryPath)
	if err != nil {
		return Compatibility{}, manifest.Repository{}, err
	}
	if _, err := checkRequirement(cliVersion, preRead, operations); err != nil {
		return Compatibility{}, manifest.Repository{}, err
	}
	repository, err := operations.loadManifest(repositoryPath)
	if err != nil {
		return Compatibility{}, manifest.Repository{}, err
	}
	strictRequirement := repository.Requirement()
	developmentBuild, err := checkRequirement(cliVersion, strictRequirement, operations)
	if err != nil {
		return Compatibility{}, manifest.Repository{}, err
	}
	return Compatibility{
		requirement:      strictRequirement,
		developmentBuild: developmentBuild,
	}, repository, nil
}

func checkRequirement(
	cliVersion string,
	requirement manifest.Requirement,
	operations loadingOperations,
) (bool, error) {
	satisfied, developmentBuild, err := operations.satisfies(cliVersion, requirement)
	if err != nil {
		return false, err
	}
	if !satisfied {
		return developmentBuild, fmt.Errorf(
			"%w: build %q does not satisfy %s",
			ErrRequiresUnsatisfied,
			cliVersion,
			requirement.String(),
		)
	}
	return developmentBuild, nil
}

func validateLoadedState(context RunContext, snapshot state.Snapshot, operations loadingOperations) error {
	control := context.Control()
	targets := stateTargets(control.Home(), snapshot)
	if err := operations.validateLexicalBoundaries(control.Paths(), targets); err != nil {
		return fmt.Errorf("%w: validate state target lexical boundaries: %w", state.ErrCorrupt, err)
	}
	if err := operations.validatePathBoundaries(control.Paths(), targets); err != nil {
		var conflict *paths.TargetConflictError
		if errors.As(err, &conflict) && conflict.Relation().Has(paths.TargetRelationEqual) {
			return fmt.Errorf(
				"%w: %w: validate equal state target identities: %w",
				state.ErrCorrupt,
				state.ErrTargetIdentityConflict,
				err,
			)
		}
		return fmt.Errorf("%w: validate state target runtime boundaries: %w", state.ErrPathValidation, err)
	}
	return nil
}

func stateTargets(home string, snapshot state.Snapshot) []paths.LabeledTarget {
	targets := make([]paths.LabeledTarget, 0, len(snapshot.EntryKeys()))
	for _, key := range snapshot.EntryKeys() {
		relative := strings.TrimPrefix(key, "~/")
		targets = append(targets, paths.LabeledTarget{
			Label: "state target " + key,
			Path:  filepath.Join(home, filepath.FromSlash(relative)),
		})
	}
	return targets
}

type loadingOperations struct {
	preflight                 func(Overrides) (RunContext, error)
	preflightInit             func(Overrides) (InitContext, error)
	preflightRepository       func(Overrides) (ControlContext, error)
	acquire                   func(string, string) (*lock.Ownership, error)
	reuse                     func(*lock.Ownership, string, string) (*lock.Guard, error)
	readRequirement           func(string) (manifest.Requirement, error)
	satisfies                 func(string, manifest.Requirement) (bool, bool, error)
	loadManifest              func(string) (manifest.Repository, error)
	loadState                 func(string) (state.Loaded, error)
	storeState                func(string, string, state.Snapshot) error
	publishConfig             func(string, config.Candidate) (bool, error)
	validateLexicalBoundaries func(paths.ControlPlanePaths, []paths.LabeledTarget) error
	validatePathBoundaries    func(paths.ControlPlanePaths, []paths.LabeledTarget) error
}

func defaultLoadingOperations() loadingOperations {
	return loadingOperations{
		preflight:                 Preflight,
		preflightInit:             PreflightInit,
		preflightRepository:       PreflightRepository,
		acquire:                   lock.Acquire,
		reuse:                     func(owner *lock.Ownership, root, path string) (*lock.Guard, error) { return owner.Reuse(root, path) },
		readRequirement:           manifest.ReadRequirement,
		satisfies:                 manifest.Satisfies,
		loadManifest:              manifest.Load,
		loadState:                 state.Load,
		storeState:                state.Store,
		publishConfig:             config.Publish,
		validateLexicalBoundaries: paths.ValidateLexicalTargetControlBoundaries,
		validatePathBoundaries: func(control paths.ControlPlanePaths, targets []paths.LabeledTarget) error {
			_, err := paths.ValidatePathBoundaries(control, targets)
			return err
		},
	}
}

func loadingOperationsWithResolver(resolver Resolver) loadingOperations {
	operations := defaultLoadingOperations()
	operations.preflight = resolver.Preflight
	operations.preflightInit = resolver.PreflightInit
	operations.preflightRepository = resolver.PreflightRepository
	return operations
}
