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
	Requirement      manifest.Requirement
	DevelopmentBuild bool
}

// LoadResult 保存完整 runtime 加载后的可信只读输入。
type LoadResult struct {
	Context       Context
	Compatibility Compatibility
	Manifest      manifest.Repository
	State         state.Snapshot
	StateStatus   state.LoadStatus
}

// InitLoadResult 保存 init 配置阶段所需的 preflight 与 strict manifest，不包含 state。
type InitLoadResult struct {
	Context       InitContext
	Compatibility Compatibility
	Manifest      manifest.Repository
}

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
func LoadMutation(options Options, cliVersion string) (LoadResult, *Lease, error) {
	return loadMutation(options, cliVersion, defaultLoadingOperations())
}

// LoadNestedMutation 在可信 preflight 后复用显式 ownership，再加载完整 manifest 与 state。
func LoadNestedMutation(
	options Options,
	cliVersion string,
	owner *lock.Ownership,
) (LoadResult, *Lease, error) {
	return loadNestedMutation(options, cliVersion, owner, defaultLoadingOperations())
}

// LoadReadOnly 加载与完整 mutation 相同的只读输入，但从不获取或创建 lock。
func LoadReadOnly(options Options, cliVersion string) (LoadResult, error) {
	operations := defaultLoadingOperations()
	context, err := operations.preflight(options)
	if err != nil {
		return LoadResult{}, err
	}
	return loadFull(context, cliVersion, operations)
}

// LoadInitMutation 允许 config missing，在控制面校验后持锁加载 strict manifest，但不读 state。
func LoadInitMutation(options Options, cliVersion string) (InitLoadResult, *Lease, error) {
	return loadInitMutation(options, cliVersion, defaultLoadingOperations())
}

// LoadRecoveryMutation 为 dot git 与 update pull 等恢复 mutation 建立 repo/control 上下文并持锁。
// 它有意不读取 requires、manifest 或 state；后续进入 apply 时应调用 LoadNestedMutation。
func LoadRecoveryMutation(options Options) (ControlContext, *Lease, error) {
	return loadRecoveryMutation(options, defaultLoadingOperations())
}

// LoadControlRecovery 为 self-update 等 control-only 恢复流程建立只读控制面上下文。
// config missing 合法；已有 config 仍严格校验。本入口不获取锁，也不读取 manifest/state。
func LoadControlRecovery(options Options) (ControlContext, error) {
	return defaultLoadingOperations().preflightRepository(options)
}

func loadMutation(
	options Options,
	cliVersion string,
	operations loadingOperations,
) (LoadResult, *Lease, error) {
	context, err := operations.preflight(options)
	if err != nil {
		return LoadResult{}, nil, err
	}
	owner, err := operations.acquire(context.ControlPaths.StateRoot(), context.ControlPaths.StateLock())
	if err != nil {
		return LoadResult{}, nil, err
	}
	lease := newLease(owner, owner)
	result, err := loadFull(context, cliVersion, operations)
	if err != nil {
		return LoadResult{}, nil, releaseAfterFailure(err, lease)
	}
	return result, lease, nil
}

func loadNestedMutation(
	options Options,
	cliVersion string,
	owner *lock.Ownership,
	operations loadingOperations,
) (LoadResult, *Lease, error) {
	context, err := operations.preflight(options)
	if err != nil {
		return LoadResult{}, nil, err
	}
	guard, err := operations.reuse(owner, context.ControlPaths.StateRoot(), context.ControlPaths.StateLock())
	if err != nil {
		return LoadResult{}, nil, err
	}
	lease := newLease(owner, guard)
	result, err := loadFull(context, cliVersion, operations)
	if err != nil {
		return LoadResult{}, nil, releaseAfterFailure(err, lease)
	}
	return result, lease, nil
}

func loadInitMutation(
	options Options,
	cliVersion string,
	operations loadingOperations,
) (InitLoadResult, *Lease, error) {
	context, err := operations.preflightInit(options)
	if err != nil {
		return InitLoadResult{}, nil, err
	}
	owner, err := operations.acquire(context.ControlPaths.StateRoot(), context.ControlPaths.StateLock())
	if err != nil {
		return InitLoadResult{}, nil, err
	}
	lease := newLease(owner, owner)
	compatibility, repository, err := loadRepository(context.Repository, cliVersion, operations)
	if err != nil {
		return InitLoadResult{}, nil, releaseAfterFailure(err, lease)
	}
	return InitLoadResult{
		Context:       context,
		Compatibility: compatibility,
		Manifest:      repository,
	}, lease, nil
}

func loadRecoveryMutation(
	options Options,
	operations loadingOperations,
) (ControlContext, *Lease, error) {
	context, err := operations.preflightRepository(options)
	if err != nil {
		return ControlContext{}, nil, err
	}
	owner, err := operations.acquire(context.ControlPaths.StateRoot(), context.ControlPaths.StateLock())
	if err != nil {
		return ControlContext{}, nil, err
	}
	return context, newLease(owner, owner), nil
}

func loadFull(context Context, cliVersion string, operations loadingOperations) (LoadResult, error) {
	compatibility, repository, err := loadRepository(context.Repository, cliVersion, operations)
	if err != nil {
		return LoadResult{}, err
	}
	snapshot, status, err := operations.loadState(context.ControlPaths.StateFile())
	if err != nil {
		return LoadResult{}, err
	}
	if status == state.StatusLoaded {
		if err := validateLoadedState(context, snapshot, operations); err != nil {
			return LoadResult{}, err
		}
	}
	return LoadResult{
		Context:       context,
		Compatibility: compatibility,
		Manifest:      repository,
		State:         snapshot,
		StateStatus:   status,
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
	if _, _, err := checkRequirement(cliVersion, preRead, operations); err != nil {
		return Compatibility{}, manifest.Repository{}, err
	}
	repository, err := operations.loadManifest(repositoryPath)
	if err != nil {
		return Compatibility{}, manifest.Repository{}, err
	}
	strictRequirement := repository.Requirement()
	_, developmentBuild, err := checkRequirement(cliVersion, strictRequirement, operations)
	if err != nil {
		return Compatibility{}, manifest.Repository{}, err
	}
	return Compatibility{
		Requirement:      strictRequirement,
		DevelopmentBuild: developmentBuild,
	}, repository, nil
}

func checkRequirement(
	cliVersion string,
	requirement manifest.Requirement,
	operations loadingOperations,
) (bool, bool, error) {
	satisfied, developmentBuild, err := operations.satisfies(cliVersion, requirement)
	if err != nil {
		return false, false, err
	}
	if !satisfied {
		return false, developmentBuild, fmt.Errorf(
			"%w: build %q does not satisfy %s",
			ErrRequiresUnsatisfied,
			cliVersion,
			requirement.String(),
		)
	}
	return true, developmentBuild, nil
}

func validateLoadedState(context Context, snapshot state.Snapshot, operations loadingOperations) error {
	targets := stateTargets(context.Home, snapshot)
	if err := operations.validateLexicalBoundaries(context.ControlPaths, targets); err != nil {
		return fmt.Errorf("%w: validate state target lexical boundaries: %w", state.ErrCorrupt, err)
	}
	if err := operations.validateStateIdentities(snapshot, context.Home); err != nil {
		return err
	}
	if err := operations.validatePathBoundaries(context.ControlPaths, targets); err != nil {
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
	preflight                 func(Options) (Context, error)
	preflightInit             func(Options) (InitContext, error)
	preflightRepository       func(Options) (ControlContext, error)
	acquire                   func(string, string) (*lock.Ownership, error)
	reuse                     func(*lock.Ownership, string, string) (*lock.Guard, error)
	readRequirement           func(string) (manifest.Requirement, error)
	satisfies                 func(string, manifest.Requirement) (bool, bool, error)
	loadManifest              func(string) (manifest.Repository, error)
	loadState                 func(string) (state.Snapshot, state.LoadStatus, error)
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
