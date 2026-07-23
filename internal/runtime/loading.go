package runtime

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mianm12/dotfiles/internal/config"
	"github.com/mianm12/dotfiles/internal/lock"
	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/state"
)

// LoadedInputs 保存完整 runtime 加载后的可信只读输入。
type LoadedInputs struct {
	context    RunContext
	repository manifest.Repository
	state      state.Loaded
}

// Context 返回本次运行的严格 preflight 结果。
func (inputs LoadedInputs) Context() RunContext { return inputs.context }

// Manifest 返回严格加载的仓库 manifest。
func (inputs LoadedInputs) Manifest() manifest.Repository { return inputs.repository }

// State 返回缺失或严格加载的 state 联合结果。
func (inputs LoadedInputs) State() state.Loaded { return inputs.state }

// InitInputs 保存 init 配置阶段所需的 preflight 与 strict manifest，不包含 state。
type InitInputs struct {
	context    InitContext
	repository manifest.Repository
}

// InitSelection 保存 interaction/非交互层明确选择的 profile。
// Override.Set 区分省略和显式空字符串。
type InitSelection struct {
	Profile Override
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

// Manifest 返回严格加载的仓库 manifest。
func (inputs InitInputs) Manifest() manifest.Repository { return inputs.repository }

// BuildCandidate 从 immutable preparation inputs 纯函数地合并完整 machine config candidate。
func (inputs InitInputs) BuildCandidate(selection InitSelection) (InitCandidate, error) {
	context := inputs.context
	snapshot := context.ConfigSnapshot()
	existing, exists := context.ExistingMachine()

	profile := ""
	if override, ok := context.ProfileOverride(); ok {
		profile = override
	} else if selection.Profile.Set {
		profile = selection.Profile.Value
	} else if exists {
		profile = existing.Profile()
	}
	if profile == "" {
		return InitCandidate{}, fmt.Errorf("init profile is required")
	}
	if !slices.Contains(inputs.repository.ProfileNames(), profile) {
		return InitCandidate{}, fmt.Errorf("unknown init profile %q", profile)
	}

	machine := config.Machine{Profile: profile}
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
func LoadReadOnly(overrides Overrides) (LoadedInputs, error) {
	return systemResolver().LoadReadOnly(overrides)
}

// LoadReadOnly 使用 resolver 的明确系统来源加载完整只读输入。
func (resolver Resolver) LoadReadOnly(overrides Overrides) (LoadedInputs, error) {
	operations := loadingOperationsWithResolver(resolver)
	context, err := operations.preflight(overrides)
	if err != nil {
		return LoadedInputs{}, err
	}
	return loadFull(context, operations)
}

// PrepareInit 在不获取 lock、不读取 state 的前提下严格加载 init config 与 manifest。
func PrepareInit(overrides Overrides) (InitInputs, error) {
	return systemResolver().PrepareInit(overrides)
}

// PrepareInit 使用 resolver 的明确系统来源执行只读 init preparation。
func (resolver Resolver) PrepareInit(overrides Overrides) (InitInputs, error) {
	operations := loadingOperationsWithResolver(resolver)
	context, err := operations.preflightInit(overrides)
	if err != nil {
		return InitInputs{}, err
	}
	repository, err := loadRepository(context.Control().RepositoryPath(), operations)
	if err != nil {
		return InitInputs{}, err
	}
	return InitInputs{context: context, repository: repository}, nil
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

func loadFull(context RunContext, operations loadingOperations) (LoadedInputs, error) {
	control := context.Control()
	repository, err := loadRepository(control.RepositoryPath(), operations)
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
		context:    context,
		repository: repository,
		state:      loadedState,
	}, nil
}

func loadRepository(
	repositoryPath string,
	operations loadingOperations,
) (manifest.Repository, error) {
	repository, err := operations.loadManifest(repositoryPath)
	if err != nil {
		return manifest.Repository{}, err
	}
	return repository, nil
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
	loadManifest              func(string) (manifest.Repository, error)
	loadState                 func(string) (state.Loaded, error)
	storeState                func(string, string, state.Snapshot) error
	publishConfig             func(string, config.Candidate) (config.PublishResult, error)
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
