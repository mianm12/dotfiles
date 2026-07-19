# fix/m1-runtime-design-debt：收拢可信 runtime 生命周期与路径证明

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，mutation 调用方会在任何锁后加载步骤发生前先取得一份明确拥有的运行会话；加载、
嵌套恢复和 state 提交都在该会话下完成，释放失败不会丢失可重试句柄。runtime preflight 的
用户覆盖与系统来源分离，返回的控制面、机器数据、init 配置状态和 state 加载结果不能表达
互相矛盾的组合。state target 的永久语义与当前拓扑错误由同一次文件系统 identity snapshot
分类，不再先逐项解析后重复执行全局校验。

本 Goal 是 CP2 合入后的内部可靠性修复，不改变公开 CLI、state v1 持久格式、ownership、
恢复例外或 mutation 顺序契约。CI 同时改为安装明确版本的 golangci-lint；与上述改动相邻、
能够在不扩大契约范围下消除的低收益整洁度债务随对应语义 commit 处理。

## Scope / Non-goals

范围内：

- 修复锁后加载失败且释放失败时 runtime 丢失 lease 的问题，以明确 session 生命周期取代
  “加载结果、裸 lease、error”三元返回和公开 `*lock.Ownership` 传递。
- 把路径/profile 覆盖与环境/HOME 来源分离；让 production 默认来源内聚，测试继续使用窄的
  concrete seam；返回上下文使用单一控制面路径真相和不可变数据访问。
- 分离 init 配置存在性与普通 profile context；把 state missing/loaded 封装为不能与无效
  Snapshot 错配的只读值。
- 让 mutation session 从可信控制面派生 state root/path，并提供生产编排使用的 state 提交
  入口；`internal/state` 继续只负责编码和原子发布，不自行获取锁。
- 为 paths target 冲突提供结构化 relation/provenance，并让 runtime 用一次完整
  `ValidatePathBoundaries` 结果区分相同 state identity 与当前 topology 错误。
- 移除本次重构直接暴露的无意义返回值、重复字段或死接缝；固定 CI lint 工具版本并同步
  README/CONTRIBUTING 的长期事实。

明确不做：

- 不实现 planner、executor、action Precond、state transition/builder、公开 mutation 命令或
  M2/M3 能力。
- 不在本 Goal 重构 `ValidatedProfile`/`DesiredEntry`。它们必须与后续 planner 的 exact desired
  和 scope 模型一起设计，不能形成提前抽象。
- 不修改 state v1 JSON、错误作用域、锁文件格式、控制面成员、路径 identity 算法或规范。
- 不为整洁度单独拆分 package、引入通用 DI/interface、JSON Schema、日志框架、第三方依赖，
  也不因标准库 `runtime` 同名而进行无行为收益的目录重命名。
- 不读取或修改真实 `modules/`、machine config、state、backup、HOME 或私人数据。

## Contract and Context

- `docs/02-architecture.md` §2–§6：preflight 必须早于 lock；mutation 完整周期持有同一所有权，
  嵌套流程复用所有权；只读零锁；state/desired/control 必须复用同一 identity 与边界定义。
- `docs/04-cli-spec.md` §2–§3、§4.7–§4.10：init、恢复和只读入口的 manifest/state/lock 消费范围
  不同，损坏 state 不能阻断不依赖它的恢复步骤。
- `docs/05-apply-engine.md` §2、§4–§6：state missing 合法；损坏/过新/rendered fail closed；
  state 原子提交必须位于 mutation lock 周期内，当前拓扑错误不能改写为永久 corrupt。
- `docs/08-testing.md` §1–§3：失败路径、只读零写入、锁竞争/嵌套与旧 state 保留需要隔离验证。
- `docs/09-roadmap.md` §1、§3：当前仍只交付 M1 运行基础，不预建 planner/executor。

基线是 clean `main@bf530d2ed4ce`，CP2 coordinator 与所有 Milestone ExecPlans 已迁移至
`completed/`。现有 `internal/runtime.LoadMutation` 在取得锁后立即执行 `loadFull`；失败时先
调用 `Lease.Release`，即使释放失败也返回 nil lease。`Options` 同时暴露覆盖值和可为 nil 的
系统函数；`ControlContext` 重复保存 HOME/config/repo、ControlPlanePaths 与一次性 identity
结果；`InitContext` 嵌入可能不完整的普通 Context；`LoadResult` 用 Snapshot/LoadStatus 两个
字段表达同一联合状态。state 加载路径先由 `state.ValidateTargetIdentities` 为各 target 分别
创建 resolver，再由 `paths.ValidatePathBoundaries` 重新解析完整集合。

## Progress

- [x] 2026-07-19：确认 main、origin/main、所有本地 branch/worktree 和 completed CP2 plans；
  main 为 `bf530d2ed4ce`、相对 origin/main ahead 45、工作树 clean，未 fetch/pull。
- [x] 2026-07-19：创建 `fix/m1-runtime-design-debt`，读取仓库规则、相关规范、当前实现/测试及
  completed runtime/preflight/path/state plans，未发现规范冲突或任务外改动。
- [x] 2026-07-19：以 `918b6fd` 提交 active ExecPlan 起点。
- [x] 2026-07-19：先增加 production preflight 无 callback 回归，确认原实现 nil dereference；
  随后以 `de3af79` 分离 `Overrides` 与 concrete `Resolver`，把 control/run/init/compatibility/
  loaded state 改为不可变值对象，并移除 requirement 检查的无意义 bool。runtime/CLI/state
  20 次重复与完整 `make check BINARY=/private/tmp/dot-runtime-design-value-model-check` 通过。
- [x] 2026-07-19：以 `44c0223` 用 `MutationSession`、`InitSession`、`RecoverySession` 取代
  load/lease/error 三元返回和裸 `Ownership` 传递；session 先取得锁再显式加载，加载失败保留
  调用方拥有的可重试句柄，`Close` 失败不终结 session，嵌套 session 以引用保持 OS 锁，state
  提交从可信 control path 派生。runtime/lock/state 20 次重复、race、全仓测试及完整
  `make check BINARY=/private/tmp/dot-runtime-design-session-check` 通过。
- [ ] 完成单次路径证明和 CI/整洁度 milestones。
- [ ] 运行完整门禁，完成独立复核、必要 fix commit、复审与计划收口。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定范围、基线、实施顺序、非目标和验证边界，不修改生产代码、测试或配置。

Concrete steps：

    git diff --check
    git add .agent/plans/active/m1-runtime-design-debt.md
    git diff --cached --check
    git commit -m 'docs(runtime): 新建 runtime 设计债务修复计划'

Commit 边界：

    docs(runtime): 新建 runtime 设计债务修复计划

### Milestone 2：建立零值安全且不可变的 runtime 输入

先补充测试证明 production preflight 不要求调用方注入系统函数、显式空覆盖仍被区分并拒绝，
控制面路径只有一个真相源，machine data 的调用方修改不影响后续读取，init missing 不返回伪造
的普通 profile context，state missing 不能被当作有效 Snapshot 消费。随后重构 Options/context/
load result，使 CLI 和测试消费语义明确的方法或不透明值。测试 seam 保持 concrete、package
private，不形成通用 DI。

Concrete steps：

    go test ./internal/runtime ./internal/cli ./internal/state
    go test -count=20 ./internal/runtime ./internal/cli ./internal/state

验收：默认 production 来源不 panic；所有 preflight 仍先严格校验 config/control 且零写入；
init missing、普通完整 context 与 state missing/loaded 无非法组合；version 公开输出不变。

Commit 边界：

    refactor(runtime): 收拢可信运行输入值模型

### Milestone 3：用 session 表达 mutation 锁所有权

测试先注入锁后加载错误与首次 release IO 错误，证明调用方仍持有可重试 session；覆盖正常
mutation、recovery、update 后 nested full load、init 与全部失败短路。实现让 acquire/reuse 与
后续 load 分离：成功取得锁后先返回 role-specific session，再由 session 加载对应输入或开始
嵌套 full mutation。session 不暴露裸 Ownership，Close 失败不把自身标记为已释放；mutation
session 从自身 control context 派生 state 路径，并在 active 时提交 Snapshot。

Concrete steps：

    go test ./internal/runtime ./internal/lock ./internal/state
    go test -count=20 ./internal/runtime ./internal/lock ./internal/state
    go test -race ./internal/runtime ./internal/lock ./internal/state

验收：任何 post-lock 加载失败都不会隐藏仍活动的锁引用；释放失败可以重试；nested 只释放
自身 guard；recovery/init 不消费 state；只读入口仍无 lock/state 写入；inactive session 拒绝
加载、嵌套和 state 提交。

Commit 边界：

    fix(runtime): 以会话持有 mutation 锁生命周期

### Milestone 4：统一 state target 的 identity snapshot

先增加 paths/runtime 测试，要求 target equal/left-ancestor/right-ancestor 冲突可通过 typed error
取得双方 provenance 和 relation，同时保持 `errors.Is(ErrTargetOverlap)`。随后让 runtime 只调用
一次完整 path boundary；equal identity 的不同 state keys 映射为 corrupt，ancestor、blocked、
IO 和 control alias 保持 path validation。永久词法 control overlap 仍在 identity 前归 corrupt。

Concrete steps：

    go test ./internal/paths ./internal/state ./internal/runtime
    go test -count=20 ./internal/paths ./internal/state ./internal/runtime
    go test -race ./internal/paths ./internal/state ./internal/runtime

验收：一次 validation 使用一个 resolver snapshot；错误保留现有 sentinel、provenance 和作用域；
不再由 state package 单独读取文件系统 identity；路径校验保持只读和 fail closed。

Commit 边界：

    refactor(paths): 结构化 target 冲突并统一 state 校验

### Milestone 5：固定工具链并完成相邻整洁度清理

把 CI 的 golangci-lint 从浮动 latest 改为经过官方资料核对、兼容当前 Go/.golangci.yml 的明确
2.x 版本，同步 README 与 CONTRIBUTING。只处理前述重构直接触及的多余返回值、死字段、重复
wrapper 或含义模糊局部命名；不新建共享 util package，不重写 state codec 或 manifest desired。

Concrete steps：

    go mod tidy -diff
    git diff bf530d2ed4ce...HEAD --check
    make check BINARY=/private/tmp/dot-runtime-design-debt-check

验收：CI 安装版本可复现且文档一致；无新增依赖；完整门禁退出 0；整洁度改动只与本 Goal 已
修改的数据流相邻并形成独立 commit。

Commit 边界：

    ci: 固定 golangci-lint 工具版本

### Milestone 6：独立复核与收口

由未参与实现的只读 reviewer 审查 `bf530d2ed4ce...HEAD` 完整实质 diff，重点检查 session
所有权、失败恢复、只读零写入、init/recovery 例外、state 错误分类、单 snapshot 路径证明、
Go API 清晰度与 macOS/Linux 影响。主 agent 验证意见；有效问题用新的 fix commit 处理并复审，
不 amend 或重写历史。通过后重跑完整门禁，更新 living sections，将本计划迁移至 completed，
以纯计划收口 commit 结束。

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| production preflight 零值安全、上下文不可伪造 | runtime/CLI tests | 已通过 |
| 锁后失败不丢失会话且 release 可重试 | runtime/lock failure tests | 已通过 |
| nested/recovery/init/read-only 边界不回归 | runtime filesystem/order tests | 已通过 |
| state 提交只使用 session 可信 control path | runtime/state tests | 已通过 |
| state target 使用单一 identity snapshot 且分类正确 | paths/runtime tests | 待验证 |
| CI lint 版本可复现且文档一致 | workflow/diff review | 待验证 |
| 当前平台完整门禁 | `make check` | 待验证 |
| 远端 macOS/Linux CI | 精确 branch HEAD | 待验收 |

最终从 repo root 运行窄测、重复、race、Darwin/Linux 交叉编译、
`git diff bf530d2ed4ce...HEAD --check` 与 `make check`。交叉编译只证明编译；远端 CI 未运行时
必须记录“本地验收通过、远端待验收”。

## Safety, Authorization, and Recovery

用户当前任务明确授权修改、stage、commit 本次修复及相邻整洁度债务，并要求拆分语义 commits；
这覆盖短生命周期分支、active/completed plan 生命周期和必要 fix commits，不授权 push、PR、
rebase、amend、reset、force、真实私人数据或范围外重构。全部文件系统测试只使用 `t.TempDir()`
下合成 HOME/config/repo/state/lock；构建产物写入已忽略目录或 `/private/tmp`。

每个 milestone 形成独立 commit。失败后从最近成功提交继续，以新 commit 修复；不改写历史。
若 session API 无法在不改变恢复语义或锁边界下收拢，或 typed path error 需要改变永久/当前错误
分类，则更新计划并停止，不以兼容 adapter、吞错或近似语义继续。

## Interfaces and Dependencies

runtime 对外提供零写入 control/run/init preflight、无锁 read-only load，以及持有 acquired/reused
锁引用的具体 mutation/recovery session。session 的 load/close/state commit 必须验证活动状态，
但不建立通用状态机或 interface hierarchy。paths 以 typed error 暴露 target relation 事实；
runtime/state 决定事实对应 corrupt 还是当前路径不可安全消费。

本 Goal 不新增 Go module 依赖。CI 只把既有 lint 工具安装方式固定为明确版本；确定版本前核对
官方 release、Go 兼容性和 action 支持。

## Surprises & Discoveries

- Observation: CLI 自身已有可替换 environment，用于不读取测试进程的真实 HOME/环境；仅保留
  package-private sources 会迫使 CLI 测试 mock 整个 preflight 或改读真实环境。
  Evidence: `internal/cli.environment` 同时服务 version 与 manifest-only doctor，version 测试以
  合成 lookup/HOME 验证真实 runtime 解析。
  Impact: runtime 暴露只有 lookup/HOME 两个函数的 concrete `Resolver`，用户覆盖仍只存在于
  `Overrides`；nil 来源显式报错，不建立通用 DI/interface。

- Observation: `lock.Ownership` 的引用计数已经支持外层先释放而嵌套 guard 继续持有 OS 锁，
  runtime 无需复制第二套 ownership 状态机。
  Evidence: runtime 测试覆盖 outer recovery 先 `Close`、nested session 随后继续 `Load`，竞争
  进程直到 nested `Close` 前始终得到 `ErrBusy`。
  Impact: role-specific session 只封装可用范围和关闭重试；真正的锁引用计数仍由 `internal/lock`
  单一实现负责。

## Decision Log

- Decision: 本次作为 CP2 完成后的新 fix Goal，不 reopen 或修改历史 completed ExecPlans。
  Rationale: CP2 已合入 main 并完成生命周期；当前问题来自后续主线审查，应由新的可审阅历史
  表达。
  Date: 2026-07-19

- Decision: 先收拢 runtime 值模型，再修改 session，最后统一 path conflict。
  Rationale: session 消费可信 context；path 改动与 runtime state loader 共享文件，串行可避免
  adapter 和 freshness 冲突。
  Date: 2026-07-19

- Decision: planner desired 与 state transition 留给后续相应 Milestone。
  Rationale: exact desired、action Precond、执行结果和 state effect 是一个共享契约；本 Goal
  预建 builder 或 variant 会扩大范围并很可能二次重写。
  Date: 2026-07-19

- Decision: 成功 acquire/reuse 只返回 session，不在构造函数中继续加载任何 post-lock 输入；
  load/state commit/close 在 session 私有互斥区内串行。
  Rationale: 调用方在所有锁后失败路径都保有同一个可重试资源句柄，同时 `Close` 不会与正在
  使用上下文的 I/O 交错；无需暴露裸 owner 或建立可伪造的公开状态枚举。
  Date: 2026-07-19

## Outcomes and Handoff

尚未完成。
