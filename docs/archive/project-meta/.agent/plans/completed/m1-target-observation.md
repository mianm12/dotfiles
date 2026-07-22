# feat/target-observation：建立纯计划输入与 target 观测

> [!WARNING]
> 历史工程记录，非当前规范或工作流程。

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
- [x] 2026-07-19：以 `f5b3c52` 单独提交本 active ExecPlan 起点。
- [x] 2026-07-19：先以缺失 API 测试固定 raw symlink、regular bytes/hash/mode、missing、
  directory、special、unsafe missing 与零写入；以 `67be650` 提交共享 planner model 与只读
  leaf observation。
- [x] 2026-07-19：先以缺失 API 测试固定完整 validation 后的 scope-only render、effective
  module 校验、hook-only module 与 hook 稳定顺序；以 `c131733` 提交 manifest 窄接缝。
- [x] 2026-07-19：先以缺失 API 测试固定单历史 alias join、orphan、多 state alias fail-closed、
  missing state/target 与 unsupported desired 零写入；以 `d179b5f` 提交 identity 对齐。
- [x] 2026-07-19：完整门禁先后发现测试直接导入 indirect `x/sys` 与 exported const 注释缺失；
  分别以 `feb0900`、`5c4cb72` 修复，未修改依赖文件或产品行为。
- [x] 2026-07-19：相关四包 20 次重复与 race、完整 `make check`、base diff check 通过；
  Linux/amd64 planner 与 manifest test binary 交叉编译通过。
- [x] 2026-07-19：Wave 1 独立 review 提出有效 P1：完整路径校验的 HOME 未绑定到
  `ValidatedProfile`，`RenderScope` 可用另一个 HOME 混合 target/template/hook 域。先增加
  HOME A validation 后 HOME B render 整体拒绝与整树零写入回归，再以 `584473c` 保存 clean
  HOME capability 并精确校验；相关四包 20 次、race、diff check 与完整 `make check` 通过。
- [x] 2026-07-19：第二轮对完整 branch 独立复核 GO，无 P0–P3 finding；主线程复跑窄测、
  base diff check 与 `make check BINARY=/private/tmp/dot-cp3-target-observation-final` 全通过，
  完成 lifecycle closure。

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

- Observation: 测试直接导入已有 indirect `golang.org/x/sys/unix` 会让 `go mod tidy -diff`
  要求把 `x/sys` 提升为 direct dependency。
  Evidence: 首次 `make check` 只在 tidy-check 产生该 module graph diff；改用标准库
  `syscall.Mkfifo` 后 tidy-check 通过。
  Impact: 保持本 Milestone 零新增依赖，FIFO 测试仍在 Darwin/Linux 使用真实特殊对象。

- Observation: 现有 `ValidatedProfile` 只保存结构 desired，无法在不重新消费未经验证
  `ResolvedProfile` 的情况下取得 effective module/hook 元数据或 scope render。
  Evidence: `internal/manifest/desired.go` 原 `ValidatedProfile` 只有 `entries`；测试先行时
  `RenderScope`、`Modules` 与 `HookDescriptor` 均不存在。
  Impact: 将同一次完整路径校验的 profile/GOOS/data/modules 快照收进 opaque validated value，
  scope API 只能从该 capability 调用，未开放绕过全局 validation 的入口。

- Observation: 首版 validated capability 保存了 profile/GOOS/data/modules，却遗漏同一次完整
  路径校验所使用的 effective HOME。
  Evidence: review 回归在 HOME A 调用 `ValidatePathBoundaries` 后以 HOME B 调用 `RenderScope`；
  旧实现返回成功，entry `TargetPath` 仍指向 A、模板 `.Home` 和 hook `TargetRootPath` 却指向 B。
  Impact: `ValidatedProfile` 现在保存 clean HOME 值；`RenderScope` 在任何 render/hook 派生前要求
  clean runtime HOME 精确相等，值副本不共享可变 HOME 状态。

## Decision Log

- Decision: target observation 与 decision engine 串行，先由本分支提交共享 model 与只读事实。
  Rationale: 当前没有稳定 plan model；并行会迫使两个 branch 同时定义 observation/action 契约。
  Date: 2026-07-19

- Decision: 显式 module scope 只接受当前 GOOS 的 effective modules；空请求表示完整 effective
  profile，重复请求按集合去重并按字节序稳定化。
  Rationale: OS 过滤后的 module 才具有本次 target root、desired 与 hook 语义；对非 effective
  module 静默产生空计划会掩盖调用错误。
  Date: 2026-07-19

- Decision: leaf observation 直接使用标准库 `Lstat`、`Readlink` 与 `ReadFile`，identity join
  复用 `paths.TargetResolution`，不引 filesystem abstraction。
  Rationale: 当前安全模型不对抗主动并发篡改；plan 保存明确快照，未来 executor 仍按 Precond
  复核。新增通用 IO 层不会增加当前切片的可证性质。
  Date: 2026-07-19

- Decision: HOME 与 profile、GOOS、data/modules 一样属于 `ValidatedProfile` capability 的绑定值。
  Rationale: target path validation、模板 `.Home` 与 hook target root 必须来自同一安全域；允许
  render 阶段替换 HOME 会使全局 validation 证明失效。
  Date: 2026-07-19

## Outcomes and Handoff

Milestone 已完成本地收口。branch base 为
`bd6f4fcc05a6`；语义 commits 为计划起点 `f5b3c52`、planner model/leaf observation
`67be650`、manifest scope/hook 接缝 `c131733`、desired/state identity join `d179b5f`，以及
门禁修复 `feb0900`、`5c4cb72` 和 review P1 修复 `584473c`。

实现新增 `internal/planner` 的纯值 model、leaf observation 与完整 desired/state identity join。
raw symlink 不跟随，regular 保存 bytes、`sha256:` digest 与 mode，directory/special/missing
显式区分；单历史 alias 与 current desired 合并且不成为 orphan，多历史 key 同 identity 同时
匹配 `state.ErrCorrupt` 与 `state.ErrTargetIdentityConflict`。manifest validated capability 现在
保存同一次完整 profile 校验的 metadata 与 clean HOME，并只在其后允许同 HOME 的 effective
module scope render；hook
descriptor 保留 module 字节序、声明顺序、cwd/script/target root 路径事实。没有 executor、lock、
state mutation、filesystem abstraction、managed/rendered 生命周期或新增依赖。

本机 Darwin/arm64 上相关四包 20 次重复与 race、完整
`make check BINARY=/private/tmp/dot-target-observation-home-fix-check/dot`、
`git diff bd6f4fcc05a6...HEAD --check` 均通过；Linux/amd64 planner 与 manifest 测试二进制交叉
编译通过但未实机运行。首轮独立 review 的 P1 已由 `584473c` 修复；第二轮对完整 branch
复审 GO，无 P0–P3 finding。主线程在 review 后再次运行窄测、base diff check 与完整
`make check`，全部通过。精确 branch HEAD 的远端 macOS/Linux CI 未运行，因此结论为“本地
验收通过、远端待验收”。当前满足 review-ready 与 lifecycle closure 条件，可由 coordinator
fast-forward-only 集成本地 main。
