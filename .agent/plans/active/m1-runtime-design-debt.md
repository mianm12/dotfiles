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
- 修复 `lock.Ownership`、`lock.Guard`、`MutationSession` 与 `InitSession` 按值复制时分叉
  释放、加载或提交状态的问题；同一逻辑 handle 的全部副本必须共享唯一生命周期状态。
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
- [x] 2026-07-19：先以 paths typed-error 测试确认 `TargetRelation`/`TargetConflictError` 缺失，
  再以 runtime 顺序与错误链测试确认旧实现仍执行两次 identity 解析且没有保留
  `ErrTargetOverlap` provenance；随后以 `e8669bf` 让 `ValidatePathBoundaries` 的一次 resolver
  snapshot 同时提供 control/target/equal/ancestor 事实，runtime 将 equal state keys 映射为
  corrupt，其余当前拓扑错误保持 `ErrPathValidation`。paths/state/runtime 20 次重复、race、
  关联 package 测试及完整 `make check BINARY=/private/tmp/dot-runtime-design-path-check` 通过。
- [x] 2026-07-19：核对 golangci-lint 官方 immutable latest release v2.12.2 与官方 Action v9
  的 exact patch 输入支持；本机安装版本同为 v2.12.2。以 `4681e8f` 将 CI 从 `latest` 固定为
  `v2.12.2`，同步 README/CONTRIBUTING；完整
  `make check BINARY=/private/tmp/dot-runtime-design-ci-check` 通过，未新增或修改 Go 依赖。
- [x] 2026-07-19：第一轮三路独立复核发现三项有效问题：symlink traversal 可同时形成两个
  target 祖先方向；`MutationSession.CommitState` 可在 full load 失败前被调用；同一 recovery
  ownership 可建立多个活动 child，且 init 配置提交后的可选 apply 没有同所有权入口。以
  `869f399` 恢复复合 relation bitset；20 次 paths/runtime 重复、race 和完整门禁通过。
- [x] 2026-07-19：先补充 requires/strict manifest/corrupt/too-new/rendered 失败后旧 state 不变、
  并发 child、init 同所有权、提交前 candidate 校验和失败重试测试，再以 `56c7d59` 让成功
  `Load` 返回 `LoadedMutation` 提交 capability，并把 init/recovery child 限制为一个活动写者。
  runtime/lock/state/paths 20 次重复、race 及完整
  `make check BINARY=/private/tmp/dot-runtime-design-session-review-fix-check` 通过。
- [x] 2026-07-19：三位未参与实现的只读 reviewer 对 `bf530d2ed4ce...6e87339` 完整分支
  第二轮复审均给出 GO，无 P0–P3 finding；分别确认规范/公开数据流、capability/ownership/锁序、
  paths relation/测试/CI/双平台风险。上一轮三项 finding 全部关闭，没有 unresolved blocker。
- [x] 2026-07-19：最终运行相关四包 20 次重复与 race、全仓 `make check`、`go mod tidy -diff`、
  `git diff bf530d2ed4ce...HEAD --check`，并完成当前 Darwin arm64 原生构建测试和 Darwin/Linux
  amd64 交叉编译，均退出 0；本地验收通过，远端 macOS/Linux CI 待验收。
- [x] 2026-07-19：合并前最终审查以隔离 overlay 测试稳定复现两项遗漏：复制
  `MutationSession` 可取得两份加载能力并以第二次提交覆盖第一次 state；复制 `Guard` 可消费
  两个引用并在 root owner 仍活动时解除 OS lock。依赖复核确认 `gofrs/flock` 继续只承担底层
  文件锁，`renameio/v2` 与其他并发原语均不能表达项目 ownership/session 状态机；不新增依赖。
- [ ] 2026-07-19：已获用户授权重新开启本计划；先提交纯计划 reopen，再依次修复 lock handle
  与 runtime session 的复制安全，完成独立复核和最终门禁后重新收口。

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

### Milestone 7：让 lock ownership handle 的副本共享一个逻辑引用

先增加按值复制回归，证明 root `Ownership` 或 nested `Guard` 的副本不能重复消费同一引用、
提前解除 OS lock 或在底层锁已释放后继续签发 guard。随后把 backend、路径、引用计数与 mutex
集中在私有 owner state，每个 root/guard 逻辑引用通过共享 release token 记录释放状态；所有副本
只是同一 token 的别名。最后一个引用的 `Unlock` 失败时 token 保持活动，原 handle 或任一副本
仍可重试。

Concrete steps：

    go test ./internal/lock
    go test -count=50 ./internal/lock
    go test -race ./internal/lock

验收：复制 root 或 guard 后只有第一次成功 `Release` 消费引用；另一个活跃逻辑引用存在时竞争
进程持续得到 `ErrBusy`；最后一次 Unlock 失败可重试；不同路径、outer-first 与异常退出行为不变。

Commit 边界：

    fix(lock): 共享 ownership 引用与释放状态

### Milestone 8：让 runtime session 的副本共享阶段与提交能力

先增加 mutation/init session 的 copy-before-load 与并发副本回归，证明同一逻辑 mutation 只能成功
加载一次、只能发布一次 state，且第二次提交失败后第一次发布的字节保持不变。随后让 role-specific
session 只持有私有共享 core；lease、可信 context、operations、加载能力与提交状态在同一 core
中串行变化。`LoadedMutation` 的副本共享同一不可伪造 capability，失败 Store 仍允许同一能力
重试；不建立通用状态机或 interface hierarchy。

Concrete steps：

    go test ./internal/runtime ./internal/lock ./internal/state
    go test -count=50 ./internal/runtime ./internal/lock ./internal/state
    go test -race ./internal/runtime ./internal/lock ./internal/state

验收：mutation/init session 副本不能分叉阶段；并发副本只有一个 Load 成功；一个 capability 的
全部副本共享一次成功提交额度；失败提交可重试；recovery、nested child、Close 重试与只读零写入
行为不变。

Commit 边界：

    fix(runtime): 共享 session 阶段与提交能力

### Milestone 9：重新独立复核并收口

由未参与实现的只读 reviewer 审查 `bf530d2ed4ce...HEAD` 完整实质 diff，特别核对 handle 副本、
引用计数、Unlock/Store 失败重试、nested gate、锁序、零写入及 Go API。有效 finding 使用新 fix
commit 处理并复审完整分支；通过后运行完整 diff、依赖、race、双平台构建和 `make check` 门禁，
更新 living sections 并以纯计划 commit 把本计划重新迁移至 `completed/`。

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| production preflight 零值安全、上下文不可伪造 | runtime/CLI tests | 已通过 |
| 锁后失败不丢失会话且 release 可重试 | runtime/lock failure tests | 已通过 |
| nested/recovery/init/read-only 边界不回归 | runtime filesystem/order tests | 已通过 |
| full load 失败不授予提交能力，旧 state 保持不变 | runtime capability/fail-closed tests | 已通过 |
| state 提交只使用 session 可信 control path 且提交前复核 candidate | runtime/state tests | 已通过 |
| 同一外层 ownership 仅有一个活动 child writer | sequential/concurrent/close-retry tests | 已通过 |
| ownership/guard 值副本不能重复释放同一引用 | copy/concurrent/cross-process lock tests | 待修复重验 |
| mutation/init session 值副本共享阶段和提交额度 | copy/concurrent/state-preservation tests | 待修复重验 |
| state target 使用单一 identity snapshot 且分类正确 | paths/runtime tests | 已通过 |
| CI lint 版本可复现且文档一致 | workflow/diff review | 已通过 |
| 三路独立完整复审 | spec / ownership-quality / test-platform reviewers | GO，无 P0–P3 |
| 当前平台完整门禁 | `make check` | 已通过 |
| Darwin/Linux 构建 | Darwin arm64 原生；Darwin/Linux amd64 交叉编译 | 已通过 |
| 远端 macOS/Linux CI | 精确 branch HEAD | 待验收（未运行） |

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
锁引用的具体 mutation/recovery session。mutation session 的成功 full load 返回
`LoadedMutation`，只有该 capability 能提交经复核的 state；外层 init/recovery session 只允许一个
活动 child。lock handle 和有可变阶段的 runtime session 使用私有共享 state，使 Go 值复制只产生
同一逻辑 handle 的别名；所有 load/close/commit 都在共享互斥区内验证阶段与活动状态，但不建立
通用状态机或 interface hierarchy。paths 以 typed error 暴露 target relation 事实；runtime/state
决定事实对应 corrupt 还是当前路径不可安全消费。

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

- Observation: 两个输入的 target relation 不是互斥枚举；不同展示路径经 symlink traversal
  可以让 left、right 的 leaf 分别出现在对方遍历拓扑中，从而同时形成两个严格祖先事实。
  Evidence: `right/left` 与 `left/right` 在 `left -> root`、`right -> root` 拓扑中稳定产生
  `left-ancestor|right-ancestor`；第一轮 reviewer 用该反例否定了原先的互斥假设。
  Impact: 对外 `TargetRelation` 保留内部 bitset 的全部已知事实并提供 `Has`；runtime 只查询
  是否包含 equal，诊断字符串稳定输出所有关系，不再因 enum 转换丢失 provenance。

- Observation: session 活动与“已完成 full load”是不同安全阶段；仅验证 session 未关闭，无法
  阻止 requires、strict manifest 或 state fail-closed 失败后的直接 state 覆盖。
  Evidence: 第一轮 reviewer 指出旧测试明确允许 `BeginMutation` 后不调用 `Load` 即
  `CommitState`，会抹掉 corrupt/too-new/rendered 恢复证据。
  Impact: 成功 `Load` 才返回不可伪造的 `LoadedMutation`；提交入口移动到该 capability，candidate
  仍在 store 前复用同一 state/path 校验，成功 load/commit 只允许一次，失败保持可重试。

- Observation: lock ownership 的引用计数只证明 OS lock 不提前释放，不自动保证同一进程内只有
  一个 state 写者；多个 reused guard 可从同一旧 snapshot 独立提交并产生 lost update。
  Evidence: 第一轮 reviewer 追踪 `Ownership.Reuse` 与 `RecoverySession.BeginMutation`，确认原实现
  可创建多个并行 child；init 也缺少配置提交后复用现有 ownership 的 apply 入口。
  Impact: init/recovery 共用 nested gate，同一外层 session 只允许一个活动 child；成功 Close
  才清 gate，Close 失败保留活动状态。init child 重新 strict preflight，且外层先 Close 时仍由
  child 引用保持 OS lock。

- Observation: golangci-lint Action 接受 exact patch（如 `v2.12.2`）和 minor（如 `v2.12`）；
  minor 会在运行时寻找最新 patch，因此仍会漂移。
  Evidence: 官方 Action README 的 `version` 选项与安装流程说明；官方 v2.12.2 release 页面将
  该版本标记为 Latest、Immutable release，本机 `golangci-lint version` 也报告 v2.12.2。
  Impact: workflow 固定 exact patch，并要求本地使用同一基线；不使用 `latest` 或 minor。

- Observation: 指针接收者和共享 mutex 不会自动让外层 struct 的值复制安全；当前 lock/runtime
  handle 把部分生命周期状态放在共享对象、另一部分直接放在可复制外层值，导致串行执行仍可
  分叉引用或提交额度，race detector 无法发现。
  Evidence: copy-before-load 可完成两次 `CommitState` 并覆盖第一次 state；复制 nested `Guard`
  后两次 `Release` 可在 root owner 未释放时让竞争者成功取得 OS lock。
  Impact: lock logical reference 与 runtime phase/capability 都改为私有共享 state；回归必须直接按值
  复制并覆盖顺序与并发两种消费方式，不能只依赖 `noCopy`、注释或 vet。

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

- Decision: 把 state 发布权限绑定到成功 full load 返回的 `LoadedMutation`，而不是暴露在仅完成
  preflight/acquire 的 `MutationSession` 上。
  Rationale: 类型边界直接表达 requires、strict manifest、state 与路径校验均已通过；nil/失败
  load 无 capability，candidate 发布前再次验证永久语义和当前路径，避免调用顺序靠注释维持。
  Date: 2026-07-19

- Decision: init/recovery 外层 session 对 nested mutation 实施单活动 child gate，但继续复用
  `lock.Ownership` 的引用计数，不把单写者策略下沉到 lock package。
  Rationale: lock package 的职责是持有同一 OS 排他锁，允许多 guard 是有效通用机制；lost update
  来自 runtime state 生命周期，应由 runtime 在 child 成功 Close 前拒绝第二个写者。
  Date: 2026-07-19

- Decision: state package 只保留持久文档解码/编码和语义错误类别，不再自行解析 target identity；
  runtime 使用一次完整 `paths.ValidatePathBoundaries` 的 typed conflict 完成分类。
  Rationale: control、target set 和 cross-product 必须共享同一 resolver snapshot；按职责迁移测试
  可消除第二个文件系统真相源，同时保留 corrupt/path-validation sentinel 与双方 provenance。
  Date: 2026-07-19

- Decision: 本 Goal 只固定 golangci-lint binary 的 exact patch，保留仓库既有 Action major tag
  组织方式。
  Rationale: 消除 lint 规则在普通代码提交中无预警漂移是当前可复现性问题；把所有 GitHub
  Actions 改为 commit SHA 属独立的供应链策略变更，超出本 Goal 与项目威胁边界。
  Date: 2026-07-19

- Decision: 当前分支合并前重新开启同一 ExecPlan，并先修 lock handle、后修 runtime session。
  Rationale: 两项 finding 都来自本 Goal 建立的可信生命周期边界；lock 是 runtime 的下游机制，
  先固定共享 logical reference 能让 session 修复基于已证明的释放语义。两项均不改变公开行为、
  state 格式或规范，延期到 planner/executor 只会扩大错误契约的调用面。
  Date: 2026-07-19

- Decision: 不新增或替换 `gofrs/flock`、不引入 `renameio/v2`、semaphore、引用计数库或通用状态机。
  Rationale: 外部依赖适合 OS 锁或单文件原子发布，不能承担 root/guard 引用、可重试 Unlock、
  mutation phase 与单次 state capability；私有共享 core/token 更小且能直接表达项目不变量。
  Date: 2026-07-19

## Outcomes and Handoff

本 Goal 完成了 CP2 后主线审查确认的结构性修复，并保持公开 CLI 与 state v1 持久格式不变：

- `de3af79` 把用户覆盖与系统来源分离，control/run/init/state 输入改为明确的不可变值；
  `44c0223` 以 role-specific session 持有完整 mutation 锁生命周期。
- `e8669bf` 让 runtime/state 共用一次 paths identity snapshot 和结构化冲突 provenance；
  `869f399` 修复复审发现的 mutual symlink ancestor 事实丢失。
- `4681e8f` 固定 golangci-lint v2.12.2，并同步 README/CONTRIBUTING；未修改 Go module 依赖。
- `56c7d59` 将 state 发布绑定到成功 full load 的 `LoadedMutation` capability，提交前复核 candidate；
  init/recovery 在同一 ownership 下只允许一个活动 child，Close 失败不提前开放第二写者。

所有有效 finding 均以新 commit 修复并经过完整复审，没有 amend、rebase、squash 或历史重写。
测试覆盖 failed-load 五类 fail-closed、旧 state 字节保留、candidate/Store 重试、并发 nested Begin、
child Close 失败、outer-first、init 更新后同 ownership、mutual ancestor 与只读零写入。完整 diff、
依赖整洁、race、lint、构建和 manifest 门禁均通过；分支与工作区在收口前 clean。

明确留给后续里程碑：planner/executor 的 exact desired、action Precond、state transition builder，
以及真实 `dot init` 配置原子提交的 CLI 编排。它们共享尚未交付的动作计划契约，在本 Goal 预建
会扩大范围并产生二次抽象；当前 runtime 已提供后续实现所需的安全 session/capability 边界。

上述结果是首次收口证据；合并前最终审查随后复现 handle 值复制会分叉引用和提交状态，本计划
已重新开启。当前状态为等待 Milestone 7–9 实施、独立复核和门禁；远端 macOS/Linux CI、Linux
实机运行和其他 macOS 文件系统格式仍待验收，不得据此声称当前分支可合并或所有平台已通过。
