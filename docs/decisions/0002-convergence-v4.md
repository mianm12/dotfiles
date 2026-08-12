# 0002: Convergence V4 分离语义计划、执行与 ownership

状态：Accepted
日期：2026-08-13

## 背景

Convergence V3 消除了 executor 增量修补 state 等旧问题，但又要求一个 `Plan` 同时拥有每个
placement 的 Transition、跨 placement 的全局 Action 顺序、内部 execution schedule 和
FinalState。锁前完整 planning、锁内重新 planning 与 machine fingerprint 也把“blocked 时零 lock
bookkeeping”和“只执行锁内最新事实”两套目标叠在了一起。

这些表示各自有合理用途，却重复回答同一问题，并迫使测试保护索引、schedule 和复制规则。复杂
stale-link traversal 还把保守的 ownership 清理扩展成图调度问题。继续调整这些内部表示不能消除
根因，必须先收窄产品契约。

## 决策

- `Plan` 只包含一串有序语义 Actions、结构化 Issues 和 planner 一次计算的 link-only
  `NextState`。Actions 唯一拥有公开顺序，NextState 唯一拥有成功后的 ownership；不再保留
  Transition、Action 索引或独立 execution schedule。
- Action 表示用户可理解的语义变化，不是逐 syscall trace。创建 parent 属于 create Action 的内部
  执行步骤；无变化的 keep 不是 Action。Adopt、repair-state 和 forget 仍作为纯 state 语义变化
  公开展示。
- State 切换为 version 4，只保存 link ownership。Local 从 example 首次创建后完全由用户拥有，
  不再保存 provenance。V1–V3 不兼容读取、不自动迁移；用户在程序外归档或移除旧 state。
- Mutation 使用 lock-first：纯参数与安全取锁所需的路径/entry 校验完成后创建并获取 advisory
  lock，锁内只做一次权威加载、解析、观察和 planning。后续 blocker 可以留下私有 state root/lock
  bookkeeping，但不得修改 machine config、state、target、placement parent 或 local temporary
  file。
- 完整但不可执行的 Apply 是带完整 Report 的 blocked outcome，不是运行时 error。配置无法加载、
  lock/I/O、执行、commit 与 release 失败才是 error；CLI 只投影 core 提供的 typed recovery。
- Planner 不再调度复杂 stale-link 图。只有孤立且 ownership 完整的 stale link 可以自动 prune；
  equality、alias、ancestor、descendant 或 traversal 关系采用 warning + forget 并保留 actual。Active
  managed-link traversal、namespace-changing dependency 与跨 module ownership transfer 继续
  fail closed，要求显式两阶段迁移。
- 保留公开命令、参数和退出码；status/dry-run 仍是无锁 best-effort snapshot。允许 facts、actions、
  issues 与结果行的文本字段调整，不承诺 V3 逐行文案兼容。
- 保留 lexical/resolved identity、control topology、严格 state 解码、fail-closed platform、
  no-clobber publication、删除前复核、changed-target 复核、原子 state commit、create/update 完成后
  才 prune、partial 后完整重跑和重复 apply no-op。

目标内部模型固定为：

```go
type Plan struct {
    Actions   []Action
    Issues    []Issue
    nextState state.Snapshot
}

type Issue struct {
    Severity    IssueSeverity
    Code        IssueCode
    ModuleID    string
    PlacementID string
    Target      string
    Reason      string
    Recovery    Recovery
}

type ApplyStatus uint8 // applied | blocked

type ApplyResult struct {
    Status         ApplyStatus
    Report         Report
    TargetsChanged bool
    StateChanged   bool
}
```

State 模型固定为 `Snapshot{Home string, Links map[Key]LinkRecord}`，其中 `LinkRecord` 只含 target、
resolved target 与 raw link destination。只有 active desired set 完整确定且 Plan 可执行时，私有
`nextState` 才是可提交载荷；blocked Plan 不承诺或暴露可提交 state。`Action` 中用于 placement 分类的
`Kind state.Kind` 字段删除，Decision 完整表达行为。运行时 failure 使用携带 stage、partial、recovery
和可选 Action 的 typed error；不为此建立通用 workflow 或 precondition framework。纯 reconcile
继续位于 `internal/core/converge`，不新增 package 或依赖。

## 后果

- 语义 Action、ownership state、诊断/恢复和 syscall 执行各有唯一 owner，不再通过索引或复制层
  保持多份表示同步。
- Blocked Apply 不再承诺“连 lock 都没有痕迹”；它承诺的是除协调 bookkeeping 外零业务 mutation。
- 保守 forget 会放弃复杂 stale link 的自动清理能力，但不会删除不确定数据，也不会为了低频拓扑
  维持 DAG 调度器。
- State v4 是首次发布前的破坏性切换。回滚旧二进制时必须同时恢复归档的 v3 state；V4 本身不增加
  migration、reset、backup 或兼容引擎。
- State、planner、mutation/CLI 与架构门禁已经完成原子切换，并通过行为、安全边界与依赖政策验收；
  本 ADR 因而为 Accepted。产品规则仍由 `docs/spec/**` 的对应 owner 定义。
