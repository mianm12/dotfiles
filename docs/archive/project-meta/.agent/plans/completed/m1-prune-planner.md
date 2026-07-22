# feat/prune-planner：纯函数规划安全 orphan 清理

> [!WARNING]
> 历史工程记录，非当前规范或工作流程。

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

本 Milestone 为 M1 planner 增加纯 prune 切片。完成后，调用方可以把完整 desired/state
observation、file decision 与全量或部分 module scope 交给 planner，得到确定排序的 P1/P2/P3
清理计划、整模块确认组和 conflict 下显式 deferred 结果；计划携带 target observation、plan-time
resolution 及成功/失败 state effect，但不读取或修改文件系统、不确认、不执行。

## Scope / Non-goals

范围内：

- 先以独立最小前置修复让 `OrphanTarget` 保留 observation 阶段的
  `paths.TargetResolution`，并覆盖值复制与 alias-orphan 回归。
- 复用冻结的 `Owned` 实现 P1 scaffold state-only、P2 owned symlink target+state、P3
  non-owned target-preserving state-only + warning reason。
- 全量 scope 包含全部 orphan；部分 scope 只包含 `entry.module` 在请求集内的 orphan。
- 整模块 orphan 组使用完整 effective desired 文件集合判定，并按 module/target 稳定列出。
- `--no-prune` 返回空 prune 计划；任一 file conflict 使全部候选显式 deferred 且 preserve state。
- 每个结果携带 observation、plan-time resolution Precondition 与成功/失败 state effect。

明确不做：

- target/state mutation、confirmation UI、executor、lock、backup、输出渲染或 apply 总编排。
- 修改冻结的 decision ownership contract、manifest 接缝或其他共享 planner model；获授权的
  `OrphanTarget.Resolution` 直接前置除外。
- filesystem abstraction、重新观测 target、复制 `Owned`、临时 adapter、managed/rendered 或 M2/M3。

## Contract and Context

- `docs/04-cli-spec.md` §4.2：全量/部分 prune scope、整模块确认、`--no-prune` 与 conflict
  deferred 公开行为。
- `docs/05-apply-engine.md` §3.1/§3.3：自动破坏性动作只依赖 `owned()`；P1/P2/P3 与全局收敛
  门控是规范决策表。
- `docs/02-architecture.md` §4/§6：计划必须自包含 observation、Precond 和 state disposition，
  deferred 保留旧 state。
- `docs/08-testing.md` §3：M1 必须覆盖 P1–P3、scope、整模块组、conflict 延迟与 alias 不误删。
- `docs/09-roadmap.md` §1 M1/§3：先固定纯计划，再由后续里程碑实现 mutation。

当前 base `f181f94175d3` 已包含 target observation 与 L/S decision。`planner.Owned` 是唯一 ownership
谓词；`ObservedProfile.Targets()` 是完整 effective desired，`Orphans()` 已排除单历史 alias。
初始并行 Wave 设计发现 `OrphanTarget` 丢失 observation 阶段 resolution，无法在零 IO 下构造
prune Precondition；主线程撤销并行并授权本分支串行完成唯一最小前置。

## Progress

- [x] 2026-07-19：确认 worktree/top-level 均为
  `/private/tmp/dot-cp3-prune-planner-019f795e`，branch 为 `feat/prune-planner`，base 为 clean
  `f181f94175d3`。
- [x] 2026-07-19：Plan Gate 识别冻结 `OrphanTarget` 缺失 resolution，未修改文件即停止；主线程
  撤销并行，调整为 decision → prune 串行，并明确授权最小直接前置。
- [x] 2026-07-19：提交本 active ExecPlan 起点（`c38c271`）。
- [x] 2026-07-19：测试先行补齐 orphan plan-time resolution，并形成独立 fix commit
  （`23e8b6c`）。
- [x] 2026-07-19：测试先行实现 P1/P2/P3 自包含 prune actions（`36ce18c`）。
- [x] 2026-07-19：测试先行实现 full/partial scope、完整 desired 整模块组、no-prune 与
  conflict deferred（`ea1b82f`）。
- [x] 2026-07-19：窄测、20 次重复、race、完整 diff check 与 `make check` 均通过。
- [x] 2026-07-19：未参与实现的 reviewer 对完整 branch 复核 GO，无 P0–P3 finding；主线程
  复跑窄测、base diff check 与 `make check BINARY=/private/tmp/dot-cp3-prune-planner-final`
  全通过，完成 lifecycle closure。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，记录串行 DAG 调整、直接前置边界和验证标准。

Commit 边界：

    docs(plan): 建立 prune planner 执行计划

### Milestone 2：保留 orphan observation resolution

先以回归证明 orphan 的 resolution 与 plan-time target identity 相同，且 `ObservedProfile.Orphans()`
值复制不泄漏内部状态；再只给 `OrphanTarget` 增加 `paths.TargetResolution` 并从
`ObserveProfileTargets` 已有 `resolvedHistory.resolution` 直接传入。不得修改其他共享模型。

验收：单 alias 已匹配 desired、不成为 orphan；真实 orphan 带非零且与其 target plan-time
resolution 等价的值；全程不重新解析或写盘。

Commit 边界：

    fix(planner): 保留 orphan 计划时身份

### Milestone 3：实现 P1/P2/P3 自包含动作

先用表驱动测试固定 scaffold、owned symlink、non-owned symlink 的 target/state 处置、warning
reason、Precondition 和失败 preserve；随后新增 prune 专属 result/action/reason 类型并复用
`Owned`。不得修改 frozen decision/model contract。

验收：只有 P2 标记删除 target；P1/P3 只删 state，P3 带稳定 warning reason；所有动作失败与
deferred 都 preserve state。

Commit 边界：

    feat(planner): 实现 P1 P2 P3 prune 计划

### Milestone 4：实现 scope、确认组与收敛门控

先覆盖 full/partial/no-prune、部分 scope 仍以完整 desired 判断整模块组、稳定排序和任一 file
conflict 全部 deferred；再完成纯组合入口。

验收：部分 scope 不包含未请求 module orphan；一个 conflict 不遗漏任何候选而将其全转 deferred；
确认组按完整 desired 判断并稳定列出 target 是否会被删除。

Commit 边界：

    feat(planner): 组合 prune scope 与收敛门控

## Validation and Acceptance

在本 worktree 根运行：

    go test ./internal/planner
    go test ./internal/planner ./internal/paths ./internal/state -count=20
    go test -race ./internal/planner ./internal/paths ./internal/state
    git diff f181f94175d3...HEAD --check
    make check BINARY=/private/tmp/dot-prune-planner-check/dot

成功判据：命令全部退出 0；完整 branch diff 只含 active plan、获授权的 orphan resolution 前置、
prune 实现与测试；无文件系统 IO/mutation、无新依赖。远端 CI 与 Linux 实机未运行时明确待验收。

## Safety, Authorization, and Recovery

用户已授权在本 worker branch/worktree 创建计划、修改、stage、commit 和验证当前切片；
`OrphanTarget.Resolution` 是唯一获授权的冻结 shared model 变更。所有 fixture 使用 `t.TempDir()`，
不读取真实 HOME/config/state/backup/modules 或私人数据。prune 实现必须是纯函数，测试只构造内存
model 或隔离 observation fixture，不调用 mutation runtime、Store、lock 或 executor。失败使用新
fix commit，不 amend/rebase；计划保持 active，由主线程安排 review/freshness/closure。

## Interfaces and Dependencies

不新增依赖。prune 专属 API 可以定义 result/action/reason/confirmation group，但必须消费既有
`ObservedProfile`、`Action` 和 `Owned`，并直接复用共享 `Precondition`/`StateEffect` 值；不得把
prune 语义复制进 adapter 或修改 manifest/decision contract。

## Surprises & Discoveries

- Observation: 冻结 observation 首版仅在 desired target 上保留 plan-time resolution，orphan
  返回值丢弃了已经解析的同类事实。
  Evidence: `ObservedTarget.Resolution` 存在，`OrphanTarget` 原无字段；
  `resolvedHistory.resolution` 在形成 orphan 时未传出。
  Impact: 并行 Wave 撤销；本分支在 prune 前用独立最小 commit 修复直接前置，不在 prune 层
  重新解析或增加 adapter。
- Observation: `--no-prune` 必须在本组合器的 scope 与 orphan 分类前短路，才能真正表示
  “不消费 prune 候选”；runtime load 的 strict state/profile 校验仍由上游按规范 fail closed。
  Evidence: `TestPlanPrune_NoPruneReturnsEmptyWithoutConsumingCandidates` 使用 M1 不支持的
  historical kind，入口仍返回零计划且无错误。
  Impact: `PlanPrune` 的首个分支只检查 `Enabled`，不读取 profile、不检查 scope，也不形成确认组；
  这不构成绕过 runtime load 校验的公开入口。
- Observation: deferred 不能覆盖 P2 的基础分类，否则 presentation 无法在整模块确认摘要中说明
  干净重跑后哪些 target 会被删除。
  Evidence: conflict 测试同时断言 `DeletesTarget() == false` 与 `WouldDeleteTarget() == true`。
  Impact: `Mode/Reason` 保留 P1/P2/P3，`Deferred` 单独决定本次可执行性和 state preserve。

## Decision Log

- Decision: 将 prune 从与 hook 并行的 Wave 改为 decision 后串行节点。
  Rationale: prune 消费的 observation contract 缺失 plan-time orphan resolution；串行修复共享
  前置后再实现，避免复制逻辑或临时兼容层。
  Date: 2026-07-19
- Decision: whole-module 组只从 scope 内 actions 汇总，但“module 是否仍有 desired”始终查询
  `ObservedProfile.Targets()` 的完整集合。
  Rationale: 这同时满足部分 apply 只缩小 prune 动作范围，以及不得用部分动作子集误判模块整体
  消失的规范要求。
  Date: 2026-07-19
- Decision: prune action 使用 state 的规范展示 key 作为 `Target`，实际提交前提使用 observation
  阶段解析得到的绝对 `TargetPath`、`TargetResolution` 与 leaf `Observation`。
  Rationale: 展示身份与文件系统身份分离；未来 executor 无需重新决策或在计划外补事实。
  Date: 2026-07-19

## Outcomes and Handoff

Milestone 已完成本地收口。

交付 commits：

    c38c271 docs(plan): 建立 prune planner 执行计划
    23e8b6c fix(planner): 保留 orphan 计划时身份
    36ce18c feat(planner): 实现 P1 P2 P3 prune 计划
    ea1b82f feat(planner): 组合 prune scope 与收敛门控

验证证据（2026-07-19，均退出 0）：

    go test ./internal/planner
    go test ./internal/planner ./internal/paths ./internal/state -count=20
    go test -race ./internal/planner ./internal/paths ./internal/state
    git diff f181f94175d3d58d907665d6996604e1eeb59cab...HEAD --check
    make check BINARY=/private/tmp/dot-prune-planner-check/dot

完整 branch diff 只含本计划、获授权的 orphan resolution 直接前置、prune 纯计划实现与测试；无新
依赖、无 runtime/CLI/executor/mutation 代码。测试 fixture 的路径解析只发生在 `t.TempDir()`；
生产 prune 组合没有文件系统 IO。远端 CI 与 Linux 实机尚未运行，待 Checkpoint 后续验收。
独立 reviewer 对完整 branch 复核 GO，无 P0–P3 finding；主线程在 review 后再次运行窄测、
base diff check 与完整 `make check`，全部通过。因此结论为“本地验收通过、远端待验收”，
当前满足 review-ready 与 lifecycle closure 条件，可由 coordinator fast-forward-only 集成本地 main。
