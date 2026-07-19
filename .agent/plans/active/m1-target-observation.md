# feat/target-observation：建立纯计划输入与 target 观测

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

本 Milestone 为 M1 的纯 planner 建立可信输入边界。完成后，后续 decision/prune/hook planner
可以消费完整 effective profile 的结构 desired、请求 scope 内已渲染 desired、target 的显式只读
快照，以及按文件系统 identity 对齐的历史 state；单个历史 alias 会匹配当前 desired 而不会同时
成为 orphan。所有入口保持只读，不获取 lock、不写 target/state，也不预建 executor。

## Scope / Non-goals

范围内：

- 建立最小、稳定、自包含的 planner desired/observed/state/action/Precond/state-effect 值模型。
- 为 `manifest.ResolvedProfile` 增加完整结构校验后才可执行的 module scope 校验、scope render、
  effective module 与 M1 hook descriptor 窄接缝。
- 只读观测 missing、raw symlink、regular file bytes/hash/mode、directory 与 special object。
- 在完整 desired 与严格 state 间按 target identity 对齐；单历史 alias 匹配 desired，多历史 key 同
  identity fail closed；managed/rendered 仍由既有 M1 边界提前拒绝。

明确不做：

- L/S/P 决策、prune、hook 指纹与动作编排；这些属于后续 Milestone。
- executor、文件 mutation、state builder/commit、lock、backup、force 或 Precond 执行。
- filesystem abstraction、通用 planner 依赖、文本 diff、managed/rendered 生命周期或 M2/M3。

## Contract and Context

- `docs/02-architecture.md` §4–§6：planner 顺序是完整 desired、scope render、observation、decision；
  plan 必须自包含并携带观测前提与 state 处置。
- `docs/03-manifest-spec.md` §2–§6：部分 scope 不能绕过完整 profile 路径校验，hook 引用不进入
  desired 文件集合。
- `docs/05-apply-engine.md` §1–§5：观测必须保留 L/S/P 与 identity/alias 决策所需证据；多个 state
  key 同 identity fail closed，单历史 alias 与 desired 合并。
- `docs/06-templates.md` §3/§6：scaffold 只依赖显式上下文，在 plan 阶段 scope 内 fail-fast 渲染。
- `docs/08-testing.md` §3：纯规则覆盖 alias、scope、对象类型与零写入，M1 遇 rendered fail closed。
- `docs/09-roadmap.md` §1 M1/§3：只交付 link/scaffold planner 输入，不引 managed 或 executor。

当前 `internal/runtime.LoadReadOnly` 已提供 strict manifest/state 的零锁输入；
`manifest.ResolvedProfile.ValidatePathBoundaries` 返回完整、未渲染结构 desired；`state.Snapshot` 提供
严格只读 getters；`paths` 提供文件系统 target identity。缺口是 scope render/hook/module 接缝、
leaf 对象快照，以及 desired/state 的 identity 对齐结果。

## Progress

- [x] 2026-07-19：确认分配 worktree、Git top-level 均为
  `/private/tmp/dot-cp3-target-observation-019f795e`，branch 为 `feat/target-observation`，
  base 为 clean `bd6f4fcc05a6`。
- [ ] 提交本 active ExecPlan 起点。
- [ ] 测试先行建立 planner model 与 leaf target observation。
- [ ] 测试先行补齐 manifest scope/module/hook 窄接缝。
- [ ] 测试先行实现 desired/state identity 对齐与 alias/fail-closed。
- [ ] 完成窄测、重复测试、race/完整门禁、diff check，并保持计划 active 等待独立 review。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，使 branch 的范围、基线、验证和安全边界可独立恢复。

验收：commit diff 只包含本 active plan。

Commit 边界：

    docs(plan): 建立 target observation 执行计划

### Milestone 2：建立纯 planner model 与 leaf observation

先以缺失 API 测试固定封闭对象类型、raw symlink 不跟随、regular bytes/sha256/mode、directory、
special、missing 与 IO 错误；随后实现只读 observation。planner model 只表达后续组件共享的值与
状态处置，不执行动作，也不抽象 filesystem。

验收：所有对象分支有可观察证据；调用前后隔离根的路径、bytes、mode 与 symlink text 不变。

Commit 边界：

    feat(planner): 建立 target 观测模型

### Milestone 3：补齐完整 profile 后的 scope 接缝

先用测试证明完整 profile collision 必须在 scope 前失败、未请求 scaffold 渲染错误不阻塞请求
scope、未知/非 effective module 被拒绝、hook-only module 可合法选择，且 hook descriptor 保留
模块字节序与声明顺序；再以最小 manifest API 实现。

验收：调用方只能从已通过完整 path validation 的值执行 scope/render；Content 只在请求 scope
填充，完整结构 desired 保持可用于全局校验与 prune。

Commit 边界：

    feat(manifest): 提供 planner scope 输入

### Milestone 4：按 identity 对齐 desired 与 state

先覆盖精确 key、单历史 alias、orphan、多 state alias fail closed、阻断/IO 与 rendered 拒绝；再
把完整 desired、strict state 与显式 observation 合并成稳定排序的 planner targets/orphans。

验收：单 alias 只形成一个 matched target 且不在 orphan；多 state key 同 identity 整体失败；
入口不写文件或 state。

Commit 边界：

    feat(planner): 对齐 desired 与历史 state

## Validation and Acceptance

在本 worktree 根运行：

    go test ./internal/planner ./internal/manifest
    go test ./internal/planner ./internal/manifest ./internal/paths ./internal/state -count=20
    go test -race ./internal/planner ./internal/manifest ./internal/paths ./internal/state
    git diff bd6f4fcc05a6...HEAD --check
    make check

成功判据：全部命令退出 0；完整 branch diff 只含本计划、planner/manifest 实现及对应测试；没有
lock/state/target mutation。Linux 与精确 HEAD 的远端 macOS/Linux CI 未运行时明确标记待验收。

## Safety, Authorization, and Recovery

用户已明确授权在本 branch/worktree 创建 active plan、修改、stage 和 commit 当前 Milestone。
所有 fixture 使用 `t.TempDir()`；不读取真实 HOME、config、state、backup、`modules/` 或私人数据。
本 Milestone 不调用 mutation runtime、Store 或 lock。测试/实现失败可从 clean semantic commit
重试，不删除、覆盖或恢复任务外内容。计划保持 active，由主线程安排独立 review 和 lifecycle
closure。

## Interfaces and Dependencies

不新增依赖。planner model 是后续 decision/prune/hook/apply planner 的内部共享契约；只表达
desired、observation、历史 state、action、Precond 与 state effect，不规定 executor。manifest
只暴露已验证 profile 的最小只读 planner 输入，不暴露可绕过完整 profile validation 的原始私有
结构。

## Surprises & Discoveries

暂无。

## Decision Log

- Decision: target observation 与 decision engine 串行，先由本分支提交共享 model 与只读事实。
  Rationale: 当前没有稳定 plan model；并行会迫使两个 branch 同时定义 observation/action 契约。
  Date: 2026-07-19

## Outcomes and Handoff

尚未完成。计划保持 active，等待实现、完整门禁与主线程安排的独立复核。
