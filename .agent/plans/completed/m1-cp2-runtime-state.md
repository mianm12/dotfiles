# chore/m1-cp2-orchestration：编排可信运行时与 state 基础

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。本次用户明确选择“一条 Checkpoint Goal 编排多个 branch”，因此本计划只
记录跨 Milestone 的 DAG、基线、调度、验收证据和最终结果；每个 Milestone 另有独立 branch、
active ExecPlan、语义 commits 与 review 单元。

## Purpose / Big Picture

完成后，M1 会拥有可信运行时与 state 基础：一次运行先以统一、严格、零写入的 preflight
解析 HOME、机器配置、repo、profile、data 和控制面；mutation 才取得可复用的进程锁；state
v1 对结构、重复 JSON member、语义、版本和 M1 不支持的 rendered 记录 fail closed；state
提交保持 0700/0600 权限、完整旧/新文件和原子失败保留；最终 loader 用测试固定 mutation、
只读与恢复路径的加载顺序。Checkpoint 验收从本计划记录的基线审查整个 diff，而不是只拼接
逐分支结论。

## Scope / Non-goals

范围内：

- 编排 `feat/runtime-preflight`、`feat/state-v1`、`feat/process-lock`、`feat/state-store` 和
  `feat/runtime-loading` 五个独立 Milestone。
- 固定依赖、Wave、预定集成顺序、freshness gate、逐分支 review/closure 和 main 门禁。
- 汇总配置优先级、严格解码、config missing、state 分类、重复 member、rendered fail-closed、
  0700/0600、原子失败、锁竞争/复用、只读零写入和恢复路径不消费 state 的验收证据。
- 有实质 acceptance finding 时，按共享根因使用 `fix/m1-runtime-state-acceptance`；无有效问题
  不创建空修复分支。

明确不做：

- 不在 coordinator branch 实现产品代码、测试、依赖或公开命令；这些属于 Milestone branch。
- 不实现 planner/executor、ownership 决策、apply/add/init/self-update 的完整命令、M2 managed/
  rendered 生命周期、state rebuild、update、dot git 或完整 doctor。
- 不改变规范迁就实现，不引入 afero、JSON Schema、DI、日志框架或完整 semver。
- 不 fetch、pull、push、rebase、cherry-pick、squash、amend、reset、force、PR、tag 或 Release。

## Contract and Context

- `docs/02-architecture.md` §2–§6：preflight 在 lock 或其他写入前严格解析控制面；mutation、
  只读与恢复路径有不同 state/lock 消费边界；state 原子替换；组件返回语义错误。
- `docs/03-manifest-spec.md` §8：requires 宽松预读先于严格 manifest；恢复命令的 manifest
  例外不能被 runtime loader 误伤。
- `docs/04-cli-spec.md` §2–§3：全局 flag、路径与退出码映射；只读命令无锁。
- `docs/05-apply-engine.md` §2、§4–§6：state v1、重复 member、missing/corrupt/too-new、M1
  rendered fail-closed、锁、加载/恢复边界与原子提交性质。
- `docs/06-templates.md` §3：profile/data 必须来自显式、严格机器配置，不在渲染期 fallback。
- `docs/08-testing.md`：零写入、state、锁、加载/恢复和双平台回归证据。
- `docs/09-roadmap.md` §1 M1、§3：本 Checkpoint 只交付 runtime/state 基础，不预建 M2/M3。

Checkpoint 基线 `checkpoint_base` 为本地 `main@5b0990667b0aa29373d16e3fb126357d480b7184`。
Plan Gate 时 `HEAD == main == origin/main`；仅配置 `origin`，没有 `upstream` remote 或
`upstream/main` 引用，且未 fetch/pull。`5b09906` 已包含 CP1 acceptance 收口提交
`facac3b`；`.agent/plans/completed/m1-doctor-manifest.md` 与
`.agent/plans/completed/m1-static-config-acceptance.md` 提供 CP1 证据。Plan Gate 时 main
clean、只有 main worktree，所有 CP2 目标 branch 均不存在。

现有实现已提供 `internal/paths` 的 effective HOME、config/repo 解析和控制面边界，
`internal/config.Load` 的严格机器配置，`internal/manifest` 的 requires/严格加载，以及
manifest-only doctor；仓库尚无 state model/store、进程锁或 runtime loader。`version` 新机
可用是 `docs/04-cli-spec.md` §4.10 的既有行为；“只为 init 保留 config-missing 状态”约束
runtime 的普通 profile/data 消费策略，不得破坏 `version` 的 `requires=unavailable` 恢复行为，
也不得让 manifest-only 开始读取机器配置。

## Progress

- [x] 2026-07-19：确认 CP1 已合入；main clean，`main == origin/main == 5b09906`，无
  upstream/main，候选 branches 不存在，未 fetch/pull。
- [x] 2026-07-19：读取仓库规则、ExecPlan 生命周期、指定规范、当前实现/测试与 completed
  plans；基线 `make check BINARY=/private/tmp/dot-cp2-plan-gate` 在 Darwin/arm64 通过。
- [x] 2026-07-19：三名未参与实现的只读 subagent 分别完成规范缺口、DAG/共享契约和测试/
  依赖/跨平台审查；主 agent 核对后未发现 Plan Gate 停止条件。
- [x] 2026-07-19：从 checkpoint_base 创建 coordinator branch/worktree，并建立本计划。
- [x] 2026-07-19：以 `05b9c43` 提交 coordinator ExecPlan 起点并启动 Wave 1。
- [x] 2026-07-19：`feat/runtime-preflight` 完成测试先行实现、独立 review（GO、无 P0–P3）、
  review 后最终门禁与计划 closure；main 以 `7b43272` fast-forward-only 集成，合入后 runtime/CLI
  窄测与 `make check BINARY=/private/tmp/dot-cp2-main-after-preflight` 通过，worker worktree clean
  后无 force 移除。
- [x] 2026-07-19：Wave 2 从共同基线 `7b43272` 并行实施；state-v1 经三轮完整 review 修复 strict
  schema、identity corrupt 分类、Unicode/RFC3339 与 too-new precedence 后以 `0a874f1` 先合入。
- [x] 2026-07-19：process-lock 修复已发布 inode 错误清理竞态后 review GO；按 freshness gate 以
  `0b09dca` 合入 current main，完整复审 GO，再以 `2029afe` 合入。Wave 2 合入后窄测与
  `make check BINARY=/private/tmp/dot-cp2-main-after-wave2` 通过，两个 worktree clean 后无 force 移除。
- [x] 2026-07-19：state-store 采用标准库同目录 temp + file sync + rename，修复有损 UTF-8 编码
  与 short-write 测试缺口后完整复审 GO；以 `af990e8` 合入，合入后窄测与
  `make check BINARY=/private/tmp/dot-cp2-main-after-state-store` 通过，worktree clean 后移除。
- [x] 2026-07-19：runtime-loading 固定 full/read-only/init/recovery 加载顺序与嵌套 lease；首轮
  review 的只读零写入证据 P2 以完整隔离树快照修复，第二轮完整复审 GO。以 `0a461f3` 合入，
  合入后窄测与 `make check BINARY=/private/tmp/dot-cp2-runtime-loading-main-check` 通过，worktree
  clean 后无 force 移除。
- [x] 2026-07-19：三名未参与任何 Milestone 实现的只读 reviewer 从 checkpoint_base 分别复核
  规范/跨组件流、数据保护边界、Go/测试/依赖/双平台，均 GO 且无 P0–P3 finding；未创建空的
  acceptance-fix branch。最终 checkpoint diff check 与
  `make check BINARY=/private/tmp/dot-cp2-acceptance` 通过。
- [x] 2026-07-19：以明确 merge commit 将最终 `main@0a461f3` 非重写合入 coordinator，更新
  Outcomes and Handoff 并迁移本计划至 completed；本纯计划收口 commit 后可将 coordinator
  fast-forward-only 合入 main。

## Milestone DAG and Scheduling

保守 DAG：

```text
runtime-preflight
  ├── state-v1 ────────┐
  └── process-lock ─────┴──> state-store ──> runtime-loading
```

调度与固定集成顺序：

1. Wave 1：`feat/runtime-preflight`。
2. Wave 2：`feat/state-v1` 与 `feat/process-lock` 可从同一 wave_base 并行；固定先集成
   state-v1，再在 process-lock branch 非重写合入 current main，复测并完整复审后集成。
3. Wave 3：`feat/state-store` 从 state-v1 与 process-lock 都已合入且 main 门禁通过后的
   current main 创建。
4. Wave 4：`feat/runtime-loading` 从 state-store 合入且 main 门禁通过后的 current main 创建。

Wave 2 只有在以下边界保持时才并行：state-v1 只使用标准库并修改 state codec/model/测试；
process-lock 独占锁 package、共享 state storage 权限基础、go.mod/go.sum 与锁测试。任一节点需要
修改 `internal/paths`、preflight types、CLI、Makefile/CI、同一输出映射，或两边同时修改依赖
文件时，立即撤销并行并按 state-v1 → process-lock 顺序执行。state-store 同时消费 state model
和锁分支形成的 state root/权限基础，因此有两条入边，不得提前创建。

每个 worker 在分配 worktree 后首先确认 `pwd` 与 `git rev-parse --show-toplevel` 均精确等于
分配路径；创建并先提交自己的 active ExecPlan。实现必须先增加能暴露缺口的测试，按行为形成
多个语义 commits，运行窄测、完整 diff check、跨平台交叉编译和 `make check`，保持 worktree
clean。未参与实现的 reviewer 复用停止写入的 worker worktree；有效 finding 用新 fix commit，
最多三轮完整 review。计划 closure 只在 review 和最终门禁后迁移并提交。

## Milestone Contracts

### `feat/runtime-preflight`

统一 effective HOME、config、严格 machine config、repo/profile/data 优先级与完整控制面；普通
profile/data 消费者不接受 missing config，init policy 可保留 missing；manifest-only/version
例外保持既有行为。preflight 不读 state、不创建 state root/lock、不加载 manifest。

### `feat/state-v1`

建立只读 state v1 model/codec，token 级拒绝任意对象层级重复 member，严格 schema 与词法语义，
区分 missing、corrupt、too-new、unsupported rendered。运行时祖先拓扑不安全不永久归类为
corrupt；不写 state、不取锁、不实现 ownership。

### `feat/process-lock`

在已验证 lock 路径上提供非阻塞跨进程排他锁、busy/IO 错误分类和显式 ownership 复用；state
root/lock 精确 0700/0600。只读路径不调用该能力，不接 Cobra，不实现 PID/daemon 协议。

### `feat/state-store`

在可信 state model 上提供 0700/0600、同目录临时文件与完整旧/新原子发布；准备或发布失败
保留旧 state。依赖只承担通用原子机制，不接管 state schema、权限或失败契约。

### `feat/runtime-loading`

组合 preflight、控制面、lock、requires、严格 manifest 和 state，固定 mutation、只读 plan 和
不消费 state 的恢复前置顺序；不提前实现公开 apply/update/git/rebuild 命令或 planner。

## Validation and Acceptance

| 必须成立的性质 | 主要证据 | 状态 |
|---|---|---|
| 配置优先级、严格解码与 config-missing policy | preflight 表驱动与零写入测试 | 本地通过 |
| state 分类与任意层级重复 member | state codec/语义测试 | 本地通过 |
| M1 rendered fail-closed | state 与 runtime loader 测试 | 本地通过 |
| state root/file/lock 0700/0600 | state-store/process-lock 文件系统测试 | 本地通过 |
| 原子失败保留旧 state | store 故障注入与并发 reader 测试 | 本地通过 |
| 锁竞争、释放、崩溃恢复与嵌套复用 | 真实 helper 子进程测试 | 本地通过 |
| 只读路径零写入零锁 | runtime tree snapshot 与占锁测试 | 本地通过 |
| 恢复前置不被损坏 state 阻塞 | runtime policy 顺序/访问测试 | 本地通过 |
| 完整 Checkpoint 本地门禁 | checkpoint diff check + make check | 通过 |
| 精确最终 HEAD 远端 macOS/Linux CI | GitHub Actions | 待验收：本 Goal 不 push |

每个 Milestone 至少运行其 ExecPlan 规定的窄测、适用 `-count=20` 时序重复、darwin/linux
交叉编译、`git diff <effective-base>...HEAD --check` 和
`make check BINARY=/private/tmp/<unique>/dot`。最终 Acceptance 至少运行：

    git diff 5b0990667b0aa29373d16e3fb126357d480b7184...main --check
    make check BINARY=/private/tmp/dot-cp2-acceptance

本地平台是 Darwin/arm64。交叉编译不能替代 Linux runtime；精确最终 HEAD 未触发远端 CI 时，
结论必须写“本地验收通过、远端待验收”。

## Safety, Authorization, and Recovery

用户当前 Goal 已明确授权本 Checkpoint 的 coordinator/Milestone/integration-fix/
acceptance-fix branches、`/private/tmp` worktrees、范围内修改、stage、commit、计划迁移、
freshness merge 和本地 fast-forward-only main 集成。该证据只适用于本次 Goal，不由计划延续。

测试只使用 `t.TempDir()` 或 `/private/tmp` 的合成 HOME/repo/config/state/backup，不读取或写入
真实 modules、machine config、state、backup、`.env` 或主力 HOME。mutation 类手工验证必须
同时重定向 HOME、repo、config、state 和 backup。失败保留最近成功 commit；不 amend、rebase、
cherry-pick、squash、reset、force 或删除 branch。主 agent 只对本 Goal 创建且 clean、已合入的
worktree 使用不带 `--force` 的移除。语义 merge conflict、DAG 外 main 提交或三轮后 blocking
finding 触发停止并在本计划记录。

## Interfaces and Dependencies

Milestone 接口按依赖方向形成：preflight 提供可信运行上下文；state-v1 提供只读模型和错误分类；
process-lock 提供 owner/guard 及 state root/lock 权限基础；state-store 消费 state model 与共享
权限基础；runtime-loading 只消费这些稳定 contract，不反向泄漏 CLI/Cobra。

依赖候选只在相应 Milestone 独立调查并提交：`gofrs/flock` 只承担跨平台 advisory lock 原语；
`google/renameio/v2` 只承担同文件系统原子发布与同步机制。采用前必须核对官方仓库、稳定版本、
维护状态、采用规模、license、Go directive、传递依赖和替换成本，并用窄 adapter 限定边界。
`encoding/json` 加项目校验用于 state；go-cmp 仅在现有测试断言确实不足时引入。

## Surprises & Discoveries

- Observation: 仅配置 `origin`，不存在 `upstream/main`。
  Evidence: Plan Gate 的 `git remote -v` 与 `git show-ref`。
  Impact: coordinator 记录 upstream 缺失；不把它误报为漂移，也不自行添加 remote。

- Observation: state-store 与 process-lock 都消费 state root 0700/文件 0600 的创建不变量。
  Evidence: 三路 Plan Gate 审查对 `docs/02` §2 与 `docs/05` §4 的独立核对。
  Impact: DAG 增加 `process-lock → state-store`；共享机制由前者或独立基础文件形成，禁止复制。

- Observation: 本机默认 Go 是 1.26.5，而项目 directive 是 1.25.0。
  Evidence: 测试/跨平台审查的工具链核对和 `go.mod`。
  Impact: 依赖必须兼容 Go 1.25；本地成功不得冒充最低版本或 Linux runtime 证据。

- Observation: Wave 2 的独立复核暴露了两类会破坏持久边界的标准库细节。
  Evidence: `encoding/json` struct 字段名大小写不敏感且会替换非法 Unicode；已发布 lock inode
  在 post-create failure 后按路径删除会形成第二锁域。
  Impact: state-v1 在 struct decode 前固定原始文本、duplicate、version 与 exact schema 顺序；
  storage 保留已发布 inode 并用 contender `SameFile` 回归锁定互斥不变量。

- Observation: `gofrs/flock v0.13.0` 的生产 module graph 仅增加 direct flock 与 indirect
  `golang.org/x/sys v0.37.0`。
  Evidence: 官方版本/许可/Go directive 调查、`go mod graph`、tidy 与双平台交叉编译。
  Impact: 依赖只封装在排他非阻塞 adapter 后，ownership、权限与错误语义仍由项目承担。

- Observation: state-store 不需要为原子提交引入 `renameio/v2`。
  Evidence: v2.0.2 的 Go 1.25/Apache-2.0/零传递依赖均可接受，但其高层 API 合并
  sync/close/rename；标准库同目录 temp 能逐阶段注入并证明旧值保留。
  Impact: 保持 go.mod/go.sum 不变；项目以 strict Decode + Snapshot 无损 round-trip、0600、
  file sync、close、rename 和 cleanup 错误链表达完整 state 提交边界。

- Observation: 仅断言 lock 不存在不足以证明只读 runtime 零写入。
  Evidence: runtime-loading 首轮 reviewer 指出 requires/manifest/state/path 或 control recovery
  若误写其他文件，原测试仍会通过。
  Impact: 在隔离 fixture 上比较完整路径集合、类型、mode、普通文件 bytes 与 symlink 文本；
  read-only 的 missing/loaded/fail-closed 以及 control-only recovery 均锁定整树不变量。

## Decision Log

- Decision: 采用保守 DAG，并允许 Wave 2 有条件并行，预定 state-v1 先集成。
  Rationale: 两者不共享持久化语义或文件范围；依赖与 storage/CLI 接缝由 process-lock 独占。
  Date: 2026-07-19

- Decision: state-store 同时依赖 state-v1 与 process-lock。
  Rationale: 它消费 state model，又必须复用 state root/权限不变量，不能复制两套真相源。
  Date: 2026-07-19

- Decision: 保持 version 与 manifest-only 的既有恢复/诊断例外。
  Rationale: config-missing policy 约束普通 runtime 消费者，不得违反 CLI §4.10 或 CP1 的严格
  manifest-only 隔离。
  Date: 2026-07-19

- Decision: Acceptance 不创建 `fix/m1-runtime-state-acceptance`。
  Rationale: 三名全新只读 reviewer 对完整 checkpoint_base...main 均未发现 P0–P3 actionable
  finding；创建空分支不提供证据或修复价值。
  Date: 2026-07-19

## Outcomes and Handoff

Checkpoint 2 已完成本地交付。五个 Milestone 按既定 DAG 与集成顺序完成独立 branch、ExecPlan、
测试先行实现、语义 commits、review → fix → review、freshness、closure 和 main 集成；所有
worker worktree 均 clean 后无 force 移除。

三名未参与任何 Milestone 实现的 reviewer 从 `checkpoint_base=5b0990667b0a` 对完整
`...main@0a461f3` 分别复核规范/跨组件数据流、ownership/Precond/恢复/零写入/数据保护，以及
Go 架构/测试/依赖/Makefile/CI/双平台，均 GO 且没有 P0–P3 finding。主 agent 随后运行
`git diff 5b0990667b0a...main --check` 与
`make check BINARY=/private/tmp/dot-cp2-acceptance`，全部通过；因此没有创建 acceptance fix
branch。

最终 main 已以 `chore(integration): 同步 CP2 最终基线` merge commit 同步进 coordinator；本
计划迁移与提交只包含 coordinator ExecPlan 生命周期收口。该提交之后 coordinator 可
fast-forward-only 合入本地 main。精确最终 HEAD 的远端 macOS/Linux CI 未运行，Linux 仅有
交叉编译而非实机执行，因此交付结论为“本地验收通过、远端待验收”。
