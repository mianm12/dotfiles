package runtime

import (
	"errors"
	"fmt"
	"sync"

	"github.com/mianm12/dotfiles/internal/lock"
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
type MutationSession struct {
	lease      *sessionLease
	context    RunContext
	operations loadingOperations

	loaded         *LoadedMutation
	stateCommitted bool
}

// LoadedMutation 是成功完成 requires、strict manifest、state 与路径校验后获得的提交 capability。
// 它只由 MutationSession.Load 创建；零值或加载失败不会获得 state 提交权限。
type LoadedMutation struct {
	session *MutationSession
	inputs  LoadedInputs
}

// Inputs 返回本次成功加载的不可变输入。
func (mutation *LoadedMutation) Inputs() LoadedInputs {
	return mutation.inputs
}

// BeginMutation 在严格 preflight 后取得 mutation 锁，但不读取 requires、manifest 或 state。
func BeginMutation(overrides Overrides) (*MutationSession, error) {
	return systemResolver().BeginMutation(overrides)
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
	return &MutationSession{
		lease:      newSessionLease(owner, owner),
		context:    context,
		operations: operations,
	}, nil
}

// Load 在 session 已持锁的前提下按 requires、strict manifest、state 顺序加载可信输入。
// 失败不会自动关闭 session；调用方仍负责 Close 并处理其错误。
func (session *MutationSession) Load(cliVersion string) (*LoadedMutation, error) {
	if session == nil {
		return nil, ErrSessionClosed
	}
	unlock, err := session.lease.lockActive()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if session.loaded != nil {
		return nil, fmt.Errorf("%w: mutation inputs already loaded", ErrSessionOrder)
	}
	inputs, err := loadFull(session.context, cliVersion, session.operations)
	if err != nil {
		return nil, err
	}
	mutation := &LoadedMutation{session: session, inputs: inputs}
	session.loaded = mutation
	return mutation, nil
}

// CommitState 在授予 capability 的活动 session 下校验并原子发布 Snapshot。
// 发布失败可以重试；发布成功后同一 mutation 不得再次提交 state。
func (mutation *LoadedMutation) CommitState(snapshot state.Snapshot) error {
	if mutation == nil || mutation.session == nil {
		return fmt.Errorf("%w: mutation inputs were not loaded", ErrSessionOrder)
	}
	session := mutation.session
	unlock, err := session.lease.lockActive()
	if err != nil {
		return err
	}
	defer unlock()
	if session.loaded != mutation {
		return fmt.Errorf("%w: state commit capability does not belong to this mutation", ErrSessionOrder)
	}
	if session.stateCommitted {
		return fmt.Errorf("%w: mutation state already committed", ErrSessionOrder)
	}
	if err := validateLoadedState(session.context, snapshot, session.operations); err != nil {
		return err
	}
	controlPaths := session.context.Control().Paths()
	if err := session.operations.storeState(controlPaths.StateRoot(), controlPaths.StateFile(), snapshot); err != nil {
		return fmt.Errorf("commit runtime state: %w", err)
	}
	session.stateCommitted = true
	return nil
}

// Close 释放本 session 的锁引用。失败时可以对同一 session 重试。
func (session *MutationSession) Close() error {
	if session == nil {
		return ErrSessionClosed
	}
	return session.lease.close()
}

// InitSession 持有 init 配置阶段的锁和允许 config missing 的可信上下文。
type InitSession struct {
	lease      *sessionLease
	context    InitContext
	operations loadingOperations
	loaded     bool
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
	return &InitSession{
		lease:      newSessionLease(owner, owner),
		context:    context,
		operations: operations,
	}, nil
}

// Load 在 init session 已持锁时加载 requires 与 strict manifest，但不读取 state。
func (session *InitSession) Load(cliVersion string) (InitInputs, error) {
	if session == nil {
		return InitInputs{}, ErrSessionClosed
	}
	unlock, err := session.lease.lockActive()
	if err != nil {
		return InitInputs{}, err
	}
	defer unlock()
	if session.loaded {
		return InitInputs{}, fmt.Errorf("%w: init inputs already loaded", ErrSessionOrder)
	}
	compatibility, repository, err := loadRepository(
		session.context.Control().RepositoryPath(),
		cliVersion,
		session.operations,
	)
	if err != nil {
		return InitInputs{}, err
	}
	session.loaded = true
	return InitInputs{
		context:       session.context,
		compatibility: compatibility,
		repository:    repository,
	}, nil
}

// BeginMutation 在 init 配置成功提交后，以同一 ownership 和更新后的严格 preflight
// 建立可选 apply 的 child mutation。必须先成功 Load init manifest。
func (session *InitSession) BeginMutation(overrides Overrides) (*MutationSession, error) {
	if session == nil {
		return nil, ErrSessionClosed
	}
	unlock, err := session.lease.lockActive()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !session.loaded {
		return nil, fmt.Errorf("%w: init inputs must load before nested mutation", ErrSessionOrder)
	}
	return beginNestedMutationLocked(overrides, session.lease, session.operations)
}

// Close 释放 init session；失败时可以重试。
func (session *InitSession) Close() error {
	if session == nil {
		return ErrSessionClosed
	}
	return session.lease.close()
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
	return &MutationSession{
		lease:      childLease,
		context:    context,
		operations: operations,
	}, nil
}
