package runtime

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

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

// Lease 表示当前 runtime 层持有的一份 mutation 锁引用。
// 调用方必须在完整 mutation 周期结束时 Release。
type Lease struct {
	mu       sync.Mutex
	owner    *lock.Ownership
	releaser leaseReleaser
	released bool
}

type leaseReleaser interface {
	Release() error
}

// Ownership 返回仍处于活动状态的外层 ownership，供嵌套 mutation 显式复用。
func (lease *Lease) Ownership() *lock.Ownership {
	if lease == nil {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.released {
		return nil
	}
	return lease.owner
}

// Release 只释放当前 runtime 层取得或复用的锁引用。
func (lease *Lease) Release() error {
	if lease == nil {
		return lock.ErrOwnership
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.released || lease.owner == nil || lease.releaser == nil {
		return lock.ErrOwnership
	}
	if err := lease.releaser.Release(); err != nil {
		return err
	}
	lease.released = true
	return nil
}

// LoadMutation 在可信 preflight 后获取锁，并加载完整 manifest 与 state。
func LoadMutation(options Overrides, cliVersion string) (LoadedInputs, *Lease, error) {
	return loadMutation(options, cliVersion, defaultLoadingOperations())
}

// LoadNestedMutation 在可信 preflight 后复用显式 ownership，再加载完整 manifest 与 state。
func LoadNestedMutation(
	options Overrides,
	cliVersion string,
	owner *lock.Ownership,
) (LoadedInputs, *Lease, error) {
	return loadNestedMutation(options, cliVersion, owner, defaultLoadingOperations())
}

// LoadReadOnly 加载与完整 mutation 相同的只读输入，但从不获取或创建 lock。
func LoadReadOnly(options Overrides, cliVersion string) (LoadedInputs, error) {
	operations := defaultLoadingOperations()
	context, err := operations.preflight(options)
	if err != nil {
		return LoadedInputs{}, err
	}
	return loadFull(context, cliVersion, operations)
}

// LoadInitMutation 允许 config missing，在控制面校验后持锁加载 strict manifest，但不读 state。
func LoadInitMutation(options Overrides, cliVersion string) (InitInputs, *Lease, error) {
	return loadInitMutation(options, cliVersion, defaultLoadingOperations())
}

// LoadRecoveryMutation 为 dot git 与 update pull 等恢复 mutation 建立 repo/control 上下文并持锁。
// 它有意不读取 requires、manifest 或 state；后续进入 apply 时应调用 LoadNestedMutation。
func LoadRecoveryMutation(options Overrides) (ControlContext, *Lease, error) {
	return loadRecoveryMutation(options, defaultLoadingOperations())
}

// LoadControlRecovery 为 self-update 等 control-only 恢复流程建立只读控制面上下文。
// config missing 合法；已有 config 仍严格校验。本入口不获取锁，也不读取 manifest/state。
func LoadControlRecovery(options Overrides) (ControlContext, error) {
	return defaultLoadingOperations().preflightRepository(options)
}

func loadMutation(
	options Overrides,
	cliVersion string,
	operations loadingOperations,
) (LoadedInputs, *Lease, error) {
	context, err := operations.preflight(options)
	if err != nil {
		return LoadedInputs{}, nil, err
	}
	controlPaths := context.Control().Paths()
	owner, err := operations.acquire(controlPaths.StateRoot(), controlPaths.StateLock())
	if err != nil {
		return LoadedInputs{}, nil, err
	}
	lease := newLease(owner, owner)
	result, err := loadFull(context, cliVersion, operations)
	if err != nil {
		return LoadedInputs{}, nil, releaseAfterFailure(err, lease)
	}
	return result, lease, nil
}

func loadNestedMutation(
	options Overrides,
	cliVersion string,
	owner *lock.Ownership,
	operations loadingOperations,
) (LoadedInputs, *Lease, error) {
	context, err := operations.preflight(options)
	if err != nil {
		return LoadedInputs{}, nil, err
	}
	controlPaths := context.Control().Paths()
	guard, err := operations.reuse(owner, controlPaths.StateRoot(), controlPaths.StateLock())
	if err != nil {
		return LoadedInputs{}, nil, err
	}
	lease := newLease(owner, guard)
	result, err := loadFull(context, cliVersion, operations)
	if err != nil {
		return LoadedInputs{}, nil, releaseAfterFailure(err, lease)
	}
	return result, lease, nil
}

func loadInitMutation(
	options Overrides,
	cliVersion string,
	operations loadingOperations,
) (InitInputs, *Lease, error) {
	context, err := operations.preflightInit(options)
	if err != nil {
		return InitInputs{}, nil, err
	}
	controlPaths := context.Control().Paths()
	owner, err := operations.acquire(controlPaths.StateRoot(), controlPaths.StateLock())
	if err != nil {
		return InitInputs{}, nil, err
	}
	lease := newLease(owner, owner)
	compatibility, repository, err := loadRepository(context.Control().RepositoryPath(), cliVersion, operations)
	if err != nil {
		return InitInputs{}, nil, releaseAfterFailure(err, lease)
	}
	return InitInputs{
		context:       context,
		compatibility: compatibility,
		repository:    repository,
	}, lease, nil
}

func loadRecoveryMutation(
	options Overrides,
	operations loadingOperations,
) (ControlContext, *Lease, error) {
	context, err := operations.preflightRepository(options)
	if err != nil {
		return ControlContext{}, nil, err
	}
	controlPaths := context.Paths()
	owner, err := operations.acquire(controlPaths.StateRoot(), controlPaths.StateLock())
	if err != nil {
		return ControlContext{}, nil, err
	}
	return context, newLease(owner, owner), nil
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

func newLease(owner *lock.Ownership, releaser leaseReleaser) *Lease {
	return &Lease{owner: owner, releaser: releaser}
}

func releaseAfterFailure(cause error, lease *Lease) error {
	if err := lease.Release(); err != nil {
		return errors.Join(cause, fmt.Errorf("release runtime lock after failure: %w", err))
	}
	return cause
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
		validateLexicalBoundaries: paths.ValidateLexicalTargetControlBoundaries,
		validateStateIdentities:   state.ValidateTargetIdentities,
		validatePathBoundaries: func(control paths.ControlPlanePaths, targets []paths.LabeledTarget) error {
			_, err := paths.ValidatePathBoundaries(control, targets)
			return err
		},
	}
}
