package runtime

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/mianm12/dotfiles/internal/config"
	"github.com/mianm12/dotfiles/internal/lock"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/state"
)

var (
	// ErrSessionClosed 表示调用方试图消费已经关闭或无效的 runtime session。
	ErrSessionClosed = errors.New("runtime session is closed")
	// ErrSessionOrder 表示调用方没有按 role session 规定的阶段顺序消费 capability。
	ErrSessionOrder = errors.New("runtime session operation is out of order")
	// ErrNestedMutationActive 表示同一外层 ownership 已有一个未成功关闭的 child mutation。
	ErrNestedMutationActive = errors.New("nested mutation session is already active")
)

type leaseReleaser interface {
	Release() error
}

// sessionLease 在 runtime session 完整操作期间串行保护活动锁引用。
// Close 失败时 closed 保持 false，调用方可以使用同一 session 重试。
type sessionLease struct {
	mu       sync.Mutex
	owner    *lock.Ownership
	releaser leaseReleaser
	closed   bool

	childActive bool
	onClose     func()
}

func newSessionLease(owner *lock.Ownership, releaser leaseReleaser) *sessionLease {
	return &sessionLease{owner: owner, releaser: releaser}
}

func (lease *sessionLease) lockActive() (unlock func(), err error) {
	if lease == nil {
		return nil, ErrSessionClosed
	}
	lease.mu.Lock()
	if lease.closed || lease.releaser == nil {
		lease.mu.Unlock()
		return nil, ErrSessionClosed
	}
	return lease.mu.Unlock, nil
}

func (lease *sessionLease) close() error {
	if lease == nil {
		return ErrSessionClosed
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed || lease.releaser == nil {
		return ErrSessionClosed
	}
	if err := lease.releaser.Release(); err != nil {
		return fmt.Errorf("close runtime session: %w", err)
	}
	lease.closed = true
	if lease.onClose != nil {
		onClose := lease.onClose
		lease.onClose = nil
		onClose()
	}
	return nil
}

func (lease *sessionLease) childClosed() {
	lease.mu.Lock()
	lease.childActive = false
	lease.mu.Unlock()
}

// MutationSession 持有普通 mutation 完整周期的锁和可信运行上下文。
// 值副本共享同一 core，因此不能分叉加载阶段或 state 提交额度。
type MutationSession struct {
	core *mutationSessionCore
}

type mutationSessionCore struct {
	lease      *sessionLease
	context    RunContext
	operations loadingOperations

	loaded         *loadedMutationCapability
	stateCommitted bool
}

// LoadedMutation 是成功完成 requires、strict manifest、state 与路径校验后获得的提交 capability。
// 它只由 MutationSession.Load 创建；值副本共享同一 capability，零值或加载失败没有提交权限。
type LoadedMutation struct {
	capability *loadedMutationCapability
}

type loadedMutationCapability struct {
	session *mutationSessionCore
	inputs  LoadedInputs
}

// Inputs 返回本次成功加载的不可变输入。
func (mutation *LoadedMutation) Inputs() LoadedInputs {
	if mutation == nil || mutation.capability == nil {
		return LoadedInputs{}
	}
	return mutation.capability.inputs
}

// BeginMutation 在严格 preflight 后取得 mutation 锁，但不读取 requires、manifest 或 state。
func BeginMutation(overrides Overrides) (*MutationSession, error) {
	return systemResolver().BeginMutation(overrides)
}

// BeginMutationWithStateStore 建立与 BeginMutation 相同的 production mutation session，只把
// 最终 state Store 依赖显式替换为调用方提供的实现。它供 internal orchestration 的确定性
// Store-stage 故障测试使用；依赖只绑定本 session，不写全局状态。
func BeginMutationWithStateStore(
	overrides Overrides,
	store func(root, path string, snapshot state.Snapshot) error,
) (*MutationSession, error) {
	if store == nil {
		return nil, fmt.Errorf("state store dependency is required")
	}
	operations := loadingOperationsWithResolver(systemResolver())
	operations.storeState = store
	return beginMutation(overrides, operations)
}

// BeginMutation 使用 resolver 的明确系统来源建立 mutation session。
func (resolver Resolver) BeginMutation(overrides Overrides) (*MutationSession, error) {
	return beginMutation(overrides, loadingOperationsWithResolver(resolver))
}

func beginMutation(overrides Overrides, operations loadingOperations) (*MutationSession, error) {
	context, err := operations.preflight(overrides)
	if err != nil {
		return nil, err
	}
	controlPaths := context.Control().Paths()
	owner, err := operations.acquire(controlPaths.StateRoot(), controlPaths.StateLock())
	if err != nil {
		return nil, err
	}
	return newMutationSession(
		newSessionLease(owner, owner),
		context,
		operations,
	), nil
}

func newMutationSession(
	lease *sessionLease,
	context RunContext,
	operations loadingOperations,
) *MutationSession {
	return &MutationSession{core: &mutationSessionCore{
		lease:      lease,
		context:    context,
		operations: operations,
	}}
}

// Load 在 session 已持锁的前提下按 requires、strict manifest、state 顺序加载可信输入。
// 失败不会自动关闭 session；调用方仍负责 Close 并处理其错误。
func (session *MutationSession) Load(cliVersion string) (*LoadedMutation, error) {
	if session == nil || session.core == nil {
		return nil, ErrSessionClosed
	}
	core := session.core
	unlock, err := core.lease.lockActive()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if core.loaded != nil {
		return nil, fmt.Errorf("%w: mutation inputs already loaded", ErrSessionOrder)
	}
	inputs, err := loadFull(core.context, cliVersion, core.operations)
	if err != nil {
		return nil, err
	}
	capability := &loadedMutationCapability{session: core, inputs: inputs}
	core.loaded = capability
	return &LoadedMutation{capability: capability}, nil
}

// CommitState 在授予 capability 的活动 session 下校验并原子发布 Snapshot。
// 发布失败可以重试；发布成功后同一 mutation 不得再次提交 state。
func (mutation *LoadedMutation) CommitState(snapshot state.Snapshot) error {
	if mutation == nil || mutation.capability == nil || mutation.capability.session == nil {
		return fmt.Errorf("%w: mutation inputs were not loaded", ErrSessionOrder)
	}
	capability := mutation.capability
	core := capability.session
	unlock, err := core.lease.lockActive()
	if err != nil {
		return err
	}
	defer unlock()
	if core.loaded != capability {
		return fmt.Errorf("%w: state commit capability does not belong to this mutation", ErrSessionOrder)
	}
	if core.stateCommitted {
		return fmt.Errorf("%w: mutation state already committed", ErrSessionOrder)
	}
	if err := validateLoadedState(core.context, snapshot, core.operations); err != nil {
		return err
	}
	controlPaths := core.context.Control().Paths()
	if err := core.operations.storeState(controlPaths.StateRoot(), controlPaths.StateFile(), snapshot); err != nil {
		return fmt.Errorf("commit runtime state: %w", err)
	}
	core.stateCommitted = true
	return nil
}

// Close 释放本 session 的锁引用。失败时可以对同一 session 重试。
func (session *MutationSession) Close() error {
	if session == nil || session.core == nil {
		return ErrSessionClosed
	}
	return session.core.lease.close()
}

// InitSession 持有 init 配置阶段的锁和允许 config missing 的可信上下文。
// 值副本共享同一 core，因此不能重复完成 init 加载阶段。
type InitSession struct {
	core *initSessionCore
}

type initSessionCore struct {
	lease      *sessionLease
	context    InitContext
	overrides  Overrides
	operations loadingOperations
	loaded     *loadedInitCapability

	configCommitted bool
}

// LoadedInit 是持锁 strict refresh 与 manifest load 后获得的一次 config commit capability。
type LoadedInit struct {
	capability *loadedInitCapability
}

type loadedInitCapability struct {
	session *initSessionCore
	inputs  InitInputs
}

// Inputs 返回本次持锁 strict refresh 后的 immutable init inputs。
func (loaded *LoadedInit) Inputs() InitInputs {
	if loaded == nil || loaded.capability == nil {
		return InitInputs{}
	}
	return loaded.capability.inputs
}

// BeginInit 在 init preflight 后取得锁，但不读取 manifest 或 state。
func BeginInit(overrides Overrides) (*InitSession, error) {
	return systemResolver().BeginInit(overrides)
}

// BeginInit 使用 resolver 的明确系统来源建立 init session。
func (resolver Resolver) BeginInit(overrides Overrides) (*InitSession, error) {
	return beginInit(overrides, loadingOperationsWithResolver(resolver))
}

func beginInit(overrides Overrides, operations loadingOperations) (*InitSession, error) {
	context, err := operations.preflightInit(overrides)
	if err != nil {
		return nil, err
	}
	controlPaths := context.Control().Paths()
	owner, err := operations.acquire(controlPaths.StateRoot(), controlPaths.StateLock())
	if err != nil {
		return nil, err
	}
	return newInitSession(
		newSessionLease(owner, owner),
		context,
		overrides,
		operations,
	), nil
}

func newInitSession(
	lease *sessionLease,
	context InitContext,
	overrides Overrides,
	operations loadingOperations,
) *InitSession {
	return &InitSession{core: &initSessionCore{
		lease:      lease,
		context:    context,
		overrides:  overrides,
		operations: operations,
	}}
}

// Load 在 init session 已持锁时加载 requires 与 strict manifest，但不读取 state。
func (session *InitSession) Load(cliVersion string) (*LoadedInit, error) {
	if session == nil || session.core == nil {
		return nil, ErrSessionClosed
	}
	core := session.core
	unlock, err := core.lease.lockActive()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if core.loaded != nil {
		return nil, fmt.Errorf("%w: init inputs already loaded", ErrSessionOrder)
	}
	refreshed, err := core.operations.preflightInit(core.overrides)
	if err != nil {
		return nil, err
	}
	if refreshed.Control().Paths() != core.context.Control().Paths() {
		return nil, fmt.Errorf("%w: init control context changed after lock acquisition", config.ErrPreconditionChanged)
	}
	compatibility, repository, err := loadRepository(
		refreshed.Control().RepositoryPath(),
		cliVersion,
		core.operations,
	)
	if err != nil {
		return nil, err
	}
	inputs := InitInputs{
		context:       refreshed,
		compatibility: compatibility,
		repository:    repository,
	}
	capability := &loadedInitCapability{session: core, inputs: inputs}
	core.context = refreshed
	core.loaded = capability
	return &LoadedInit{capability: capability}, nil
}

// CommitConfig 校验 candidate 属于本次 preparation 和持锁 strict inputs 后原子发布。
// 发布失败可重试；成功或等价 no-op 后同一 capability 不得再次提交。
func (loaded *LoadedInit) CommitConfig(candidate InitCandidate) (bool, error) {
	if loaded == nil || loaded.capability == nil || loaded.capability.session == nil {
		return false, fmt.Errorf("%w: init inputs were not loaded", ErrSessionOrder)
	}
	capability := loaded.capability
	core := capability.session
	unlock, err := core.lease.lockActive()
	if err != nil {
		return false, err
	}
	defer unlock()
	if core.loaded != capability {
		return false, fmt.Errorf("%w: config commit capability does not belong to this init", ErrSessionOrder)
	}
	if core.configCommitted {
		return false, fmt.Errorf("%w: init config already committed", ErrSessionOrder)
	}
	if err := validateInitCandidate(capability.inputs, candidate); err != nil {
		return false, err
	}
	changed, err := core.operations.publishConfig(candidate.configPath, candidate.config)
	if err != nil {
		return false, fmt.Errorf("commit init config: %w", err)
	}
	core.configCommitted = true
	return changed, nil
}

func validateInitCandidate(inputs InitInputs, candidate InitCandidate) error {
	if !candidate.valid {
		return fmt.Errorf("init config candidate is invalid")
	}
	context := inputs.Context()
	control := context.Control()
	if candidate.configPath != control.ConfigPath() || candidate.repositoryPath != control.RepositoryPath() {
		return fmt.Errorf("init config candidate belongs to a different control context")
	}
	machine := candidate.Machine()
	if !slices.Contains(inputs.Manifest().ProfileNames(), machine.Profile) {
		return fmt.Errorf("unknown init profile %q after locked refresh", machine.Profile)
	}
	for _, declaration := range inputs.Manifest().DataDeclarations() {
		if _, ok := machine.Data[declaration.Key()]; !ok {
			return fmt.Errorf("init data %q is missing after locked refresh", declaration.Key())
		}
	}
	switch context.RepositorySource() {
	case paths.RepositorySourceFlag, paths.RepositorySourceEnvironment:
		if machine.Repo == nil || *machine.Repo != control.RepositoryPath() {
			return fmt.Errorf("init config candidate does not persist effective repo override")
		}
	case paths.RepositorySourceConfig:
		existing, ok := context.ExistingMachine()
		if !ok {
			return fmt.Errorf("init config source claims missing machine config")
		}
		repo, ok := existing.Repo()
		if !ok || machine.Repo == nil || *machine.Repo != repo {
			return fmt.Errorf("init config candidate changed omitted repo selection")
		}
	case paths.RepositorySourceDefault:
		if machine.Repo != nil {
			return fmt.Errorf("init config candidate added repo without an explicit source")
		}
	default:
		return fmt.Errorf("init repository source is invalid")
	}
	return nil
}

// BeginMutation 在 init 配置成功提交后，以同一 ownership 和更新后的严格 preflight
// 建立可选 apply 的 child mutation。必须先成功 Load init manifest。
func (session *InitSession) BeginMutation(overrides Overrides) (*MutationSession, error) {
	if session == nil || session.core == nil {
		return nil, ErrSessionClosed
	}
	core := session.core
	unlock, err := core.lease.lockActive()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !core.configCommitted {
		return nil, fmt.Errorf("%w: init config must commit before nested mutation", ErrSessionOrder)
	}
	return beginNestedMutationLocked(overrides, core.lease, core.operations)
}

// Close 释放 init session；失败时可以重试。
func (session *InitSession) Close() error {
	if session == nil || session.core == nil {
		return ErrSessionClosed
	}
	return session.core.lease.close()
}

// RecoverySession 持有 dot git/update pull 等恢复 mutation 的 repo/control 上下文和锁。
type RecoverySession struct {
	lease      *sessionLease
	context    ControlContext
	operations loadingOperations
}

// BeginRecovery 在 repository-only preflight 后取得锁，不读取 requires、manifest 或 state。
func BeginRecovery(overrides Overrides) (*RecoverySession, error) {
	return systemResolver().BeginRecovery(overrides)
}

// BeginRecovery 使用 resolver 的明确系统来源建立 recovery session。
func (resolver Resolver) BeginRecovery(overrides Overrides) (*RecoverySession, error) {
	return beginRecovery(overrides, loadingOperationsWithResolver(resolver))
}

func beginRecovery(overrides Overrides, operations loadingOperations) (*RecoverySession, error) {
	context, err := operations.preflightRepository(overrides)
	if err != nil {
		return nil, err
	}
	controlPaths := context.Paths()
	owner, err := operations.acquire(controlPaths.StateRoot(), controlPaths.StateLock())
	if err != nil {
		return nil, err
	}
	return &RecoverySession{
		lease:      newSessionLease(owner, owner),
		context:    context,
		operations: operations,
	}, nil
}

// Context 返回恢复流程使用的可信 repo/control 上下文。
func (session *RecoverySession) Context() (ControlContext, error) {
	if session == nil {
		return ControlContext{}, ErrSessionClosed
	}
	unlock, err := session.lease.lockActive()
	if err != nil {
		return ControlContext{}, err
	}
	defer unlock()
	return session.context, nil
}

// BeginMutation 在 recovery 所有权下重新执行完整 preflight 并建立嵌套 mutation session。
// 它适用于 update pull 后消费可能已经变化的 manifest、配置与 state。
func (session *RecoverySession) BeginMutation(overrides Overrides) (*MutationSession, error) {
	if session == nil {
		return nil, ErrSessionClosed
	}
	unlock, err := session.lease.lockActive()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return beginNestedMutationLocked(overrides, session.lease, session.operations)
}

// Close 释放 recovery session；已有 nested session 时只释放外层引用。
func (session *RecoverySession) Close() error {
	if session == nil {
		return ErrSessionClosed
	}
	return session.lease.close()
}

func beginNestedMutationLocked(
	overrides Overrides,
	parent *sessionLease,
	operations loadingOperations,
) (*MutationSession, error) {
	if parent.childActive {
		return nil, ErrNestedMutationActive
	}
	context, err := operations.preflight(overrides)
	if err != nil {
		return nil, err
	}
	controlPaths := context.Control().Paths()
	guard, err := operations.reuse(
		parent.owner,
		controlPaths.StateRoot(),
		controlPaths.StateLock(),
	)
	if err != nil {
		return nil, err
	}
	childLease := newSessionLease(parent.owner, guard)
	childLease.onClose = parent.childClosed
	parent.childActive = true
	return newMutationSession(childLease, context, operations), nil
}
