package runtime

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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

// Context 返回 init 的严格 preflight 结果。
func (inputs InitInputs) Context() InitContext { return inputs.context }

// Compatibility 返回 strict manifest 的兼容性结果。
func (inputs InitInputs) Compatibility() Compatibility { return inputs.compatibility }

// Manifest 返回严格加载的仓库 manifest。
func (inputs InitInputs) Manifest() manifest.Repository { return inputs.repository }

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
	if err := operations.validateStateIdentities(snapshot, control.Home()); err != nil {
		return err
	}
	if err := operations.validatePathBoundaries(control.Paths(), targets); err != nil {
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
	validateLexicalBoundaries func(paths.ControlPlanePaths, []paths.LabeledTarget) error
	validateStateIdentities   func(state.Snapshot, string) error
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
		validateLexicalBoundaries: paths.ValidateLexicalTargetControlBoundaries,
		validateStateIdentities:   state.ValidateTargetIdentities,
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
