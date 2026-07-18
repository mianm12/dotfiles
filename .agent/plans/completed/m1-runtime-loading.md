# feat/runtime-loading：组合可信 runtime 加载顺序

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和
`Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成后，后续 CLI 可以从 `internal/runtime` 选择语义明确的完整 mutation、嵌套 mutation、
read-only/dry-run、init 与恢复入口，而不再自行拼接 preflight、锁、requires、strict manifest
和 state。完整 mutation 在可信控制面前置后持有一次锁，再按固定顺序加载仓库与 state；
任何锁后失败释放本层 lease 且保留可分类的原始原因。read-only/control-only recovery 不创建
lock；init/recovery mutation 仍持锁，但全部恢复入口都跳过 state，因此不会被损坏、过新或 M1
不支持的 rendered state 阻塞。

## Scope / Non-goals

范围内：

- 在 `internal/runtime` 组合现有 preflight、`internal/lock`、manifest 两阶段加载与
  `internal/state`，为 full mutation/read-only、显式嵌套 ownership、init 和恢复流程提供窄入口。
- full mutation 在严格 preflight/control 成功后 acquire 或 reuse 同一 ownership，再执行
  requires 预读与版本检查、strict manifest、strict manifest 实际 requirement 复核、state
  加载和运行时 target 路径校验。
- state target 的永久词法控制面重叠归类为 corrupt；当前拓扑不可达、祖先关系或控制面别名
  归类为运行时路径不安全，不把历史记录改写成永久 corrupt。
- init 允许 config missing，但已有配置仍严格；锁后加载 requires/strict manifest，不读 state。
- recovery mutation 使用 repo-only preflight：config missing 可由 flag/env/default repo 继续，已有
  config 仍严格；控制面校验后持锁但跳过 manifest/state。control-only recovery 不取锁；update
  后进入完整 apply 时显式复用 `*lock.Ownership`。
- 以真实隔离文件系统和窄 operations seam 证明顺序、错误短路、失败释放、dev 兼容结果、
  state 三态、嵌套复用、恢复例外及只读零写入。

明确不做：

- 不新增或接入公开 Cobra 命令，不实现 planner、executor、state transition、config commit、Git、
  update 或 self-update 行为。
- 不实现 M2/M3，不改变 state/manifest 持久格式，不让 recovery 例外跳过控制面校验。
- 不新增第三方依赖，不建立通用 DI、policy flag、日志框架或兼容 fallback。
- 不读取或修改真实 `modules/`、machine config、state、backup、HOME 或私人数据。

## Contract and Context

- `docs/02-architecture.md` §2–§6：控制面校验必须早于 lock；mutation 全周期持锁，嵌套流程复用
  ownership；只读零锁；恢复前置不消费 state；full state 消费必须校验 target/control 边界。
- `docs/03-manifest-spec.md` §8：requires 先于 strict manifest；dev 只跳过版本比较；init 配置阶段
  需要两阶段 manifest，而 self-update/git 不读 manifest。
- `docs/04-cli-spec.md` §2–§3、§4.1、§4.7–§4.9：错误需要保持可分类；init missing 配置合法且
  配置提交不依赖 state；update pull、git 与 self-update 是明确恢复路径。
- `docs/05-apply-engine.md` §2、§4–§6：state missing 合法，corrupt/too-new/rendered fail closed；
  当前拓扑不安全不等于 corrupt；完整 mutation 的 lock→requires→manifest/state 顺序固定。
- `docs/06-templates.md` §3：runtime 只携带 preflight 的稳定 machine data，不在渲染期读取环境。
- `docs/08-testing.md` §1–§3：加载/恢复、锁与零写入边界必须以完全隔离的真实文件系统验证。
- `docs/09-roadmap.md` §1 M1、§3：本切片只交付可信加载基础，不预建计划或执行能力。

基线为 clean `main@af990e80f1fe`，分支 `feat/runtime-loading` 从同一提交创建。现有
`internal/runtime` 已提供普通、init 与 repository-only preflight；`internal/lock` 提供
`Ownership`/`Guard`；`internal/manifest` 提供 `ReadRequirement`、`Satisfies` 与 strict
`Load`；`internal/state` 提供 strict `Load`、`ValidateTargetIdentities` 和错误分类。缺口是这些
组件尚无统一顺序、lease 生命周期、恢复例外和 state/control 跨组件校验。

## Progress

- [x] 2026-07-19：确认分配 worktree、Git 顶层、branch 和 base 分别为
  `/private/tmp/dot-cp2-runtime-loading.uN4cR6`、`feat/runtime-loading` 与 `af990e80f1fe`，
  工作区 clean；读取规则、相关规范、代码和 CP2 前序 completed ExecPlans，未发现阻塞。
- [x] 2026-07-19：以 `6bf7f9f` 单独提交本 active ExecPlan。
- [x] 2026-07-19：先以缺失 API 的编译失败测试固定 full mutation/read-only/nested 顺序、失败
  释放、strict requirement 二次检查、dev 结果与 state 路径分类；完成最小组合层和共享词法
  target/control gate。runtime/paths 20 次重复与 race 通过。
- [x] 2026-07-19：先以缺失 API 的编译失败测试固定 init/recovery 入口；完成 init 的锁后
  manifest/no-state、repo-only recovery mutation、control-only recovery 和 update→nested
  full load。corrupt/too-new/rendered 恢复、config missing/invalid 与外层锁生命周期均通过，
  runtime 20 次重复、race 和窄 lint 通过。
- [x] 2026-07-19：runtime/paths/manifest/state/lock 20 次重复与 race 通过；Darwin/Linux amd64
  runtime 测试二进制交叉编译通过；完整 base diff check 与
  `make check BINARY=/private/tmp/dot-cp2-runtime-loading-check` 通过。worker 实现完成，计划保持
  active 等待独立复核。
- [x] 2026-07-19：首轮独立 review 确认 1 项有效 P2：read-only/control-only recovery 只断言
  lock 或 state 局部不变，不能支撑完整隔离树零写入结论。新增不跟随 symlink 的整树快照，
  记录路径、类型、mode、普通文件 bytes 与 symlink 文本；missing/loaded/corrupt/too-new/
  rendered read-only 及 control-only recovery 前后相等。修复提交 `4c2dc55` 后 runtime 20 次
  重复与 race、base diff check 及
  `make check BINARY=/private/tmp/dot-cp2-runtime-loading-review-fix-check` 通过，等待完整复审。
- [x] 2026-07-19：第二轮独立 reviewer 对完整 branch 复审 GO，无 P0–P3 finding；主 agent
  再次运行相关 package 20 次重复测试、完整 base diff check 和
  `make check BINARY=/private/tmp/dot-cp2-runtime-loading-final-check`，全部通过。迁移本计划至
  completed，等待 coordinator fast-forward 合入 main。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定范围、加载 DAG、错误分类、隔离和验证边界，不修改生产代码或测试。

Concrete steps：

    git diff --check
    git add .agent/plans/active/m1-runtime-loading.md
    git diff --cached --check && git diff --cached
    git commit -m 'docs(runtime): 新建 runtime loading ExecPlan'

Commit 边界：

    docs(runtime): 新建 runtime loading ExecPlan

### Milestone 2：固定 full loader、lease 与 state 消费边界

先在 `internal/runtime` 和必要的 `internal/paths` 测试中暴露缺失入口。full mutation 必须先
Preflight，再 acquire；read-only 使用相同 requires→strict manifest→state 数据语义但永不调用
lock；nested mutation 只能用显式 owner 复用同一路径。strict manifest 后重新检查其实际
requirement，dev 结果向调用方暴露。加载成功返回 manifest、state/status 和 compatibility；
锁后任一失败释放本层 lease，`errors.Is` 仍匹配 requires、manifest、state、path 或 lock cause。

state 校验分三层：state codec 的永久 schema/词法 HOME 校验；由 `internal/paths` 提供的窄词法
target/control boundary 将永久控制面重叠包装为 `state.ErrCorrupt`；现有 identity 与完整
`ValidatePathBoundaries` 将重复 identity 归 corrupt，而当前祖先阻断、alias、target overlap 或
control alias 包装为 `state.ErrPathValidation`。不删除、不改写 state。

Concrete steps：

    go test ./internal/runtime ./internal/paths
    go test -count=20 ./internal/runtime ./internal/paths
    go test -race ./internal/runtime ./internal/paths

验收：preflight 失败零 lock/写入；busy 在 requires 前短路；requires 失败跳过 strict
manifest/state；manifest 或 strict requirement 失败跳过 state；missing/loaded/corrupt/too-new/
rendered 分类正确；只读在全新 HOME 与损坏 state 下均零锁；nested guard 不提前释放外层 owner。

Commit 边界：

    feat(runtime): 组合完整运行时加载

### Milestone 3：交付 init 与恢复显式入口

测试先证明 init config-missing/strict-existing 差异、init 在锁后加载 manifest 且不读 state、
recovery mutation 在 repo-only 控制面校验后持锁但跳过 state/manifest，以及 control-only recovery
完全只读。config missing 可由 repo override/default 继续，已有 config 仍严格。用
corrupt/too-new/rendered state 证明这些入口不会被旧 state 阻塞；update 后嵌套 full load 仍复用
外层 owner，并在进入 state 依赖阶段恢复 fail closed。

Concrete steps：

    go test ./internal/runtime
    go test -count=20 ./internal/runtime
    go test -race ./internal/runtime

验收：init missing 可辨且 corrupt state 不阻止配置前置；已有非法 config 仍在 lock 前失败；
recovery mutation 在 config missing 时可继续、持锁且不读 state；control-only recovery 不创建
lock；嵌套 full load 的 lease 释放不解开外层锁。

Commit 边界：

    feat(runtime): 建立 init 与恢复加载入口

### Milestone 4：验证与 worker 交接

检查完整实质 diff、依赖无变化和 worktree 状态，运行重复/race、双平台编译、diff check 与
`make check`。更新本 active 计划的实际证据、风险和 handoff；不迁移 completed，等待
coordinator 安排未参与实现的独立 reviewer。

Concrete steps：

    go test -count=20 ./internal/runtime ./internal/paths ./internal/manifest ./internal/state ./internal/lock
    go test -race ./internal/runtime ./internal/paths ./internal/manifest ./internal/state ./internal/lock
    GOOS=darwin GOARCH=amd64 go test -c -o /private/tmp/dot-cp2-runtime-darwin.test ./internal/runtime
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-cp2-runtime-linux.test ./internal/runtime
    git diff af990e80f1fe...HEAD --check
    make check BINARY=/private/tmp/dot-cp2-runtime-loading-check

Commit 边界：

    docs(runtime): 记录 runtime loading 验证

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| mutation 顺序与锁后失败释放 | operations 顺序测试 + 真实锁重取 | 本机通过 |
| read-only/dry-run 零锁零写入 | 完整临时树快照与 lock 不存在断言 | 本机通过；review 修复后重验 |
| strict manifest 实际 requirement 复核与 dev notice 数据 | compatibility 测试 | 本机通过 |
| state missing/loaded/fail-closed 与路径错误分类 | 真实 state fixture 测试 | 本机通过 |
| init/recovery 不被损坏 state 阻塞 | 恢复入口真实文件系统测试 | 本机通过 |
| ownership nested reuse 不提前释放 | 外层/内层/竞争者生命周期测试 | 本机通过 |
| 当前平台完整门禁 | `make check` | 本机通过 |
| macOS/Linux 可编译 | 双平台测试二进制交叉编译 | 通过；Linux 未运行 |
| 远端 macOS/Linux CI | 精确 branch HEAD | 待验收 |

成功判据是所有命令退出 0，完整 diff 只含本计划、runtime 实现/测试和必要的 path boundary
扩展/测试，worktree clean。交叉编译只证明编译；远端 CI 未运行时准确标为待验收。

## Safety, Authorization, and Recovery

当前任务明确授权在分配 worktree 的本分支创建、修改、stage、commit 范围内文件和运行门禁；
不授权操作 main、其他 worktree、merge、push、rebase、amend、reset 或真实私人数据。全部测试
使用 `t.TempDir()` 下绝对 HOME/config/repo/state/lock，`LookupEnv` 只返回合成覆盖，不读取真实
`DOT_CONFIG`/`DOT_REPO`；构建产物和 cache 指向 `/private/tmp`。

每个 milestone 独立提交。失败后用新 fix commit 修正，不改写历史。真实 lock 测试始终显式
release；测试失败时临时根由测试框架清理。若无法在不改变恢复语义、ownership 或永久/当前
路径错误分类的前提下组合现有 API，更新计划并停止，不以吞错或近似语义继续。

## Interfaces and Dependencies

`internal/runtime` 将暴露语义明确的 full mutation、nested mutation、read-only、init、
recovery mutation 与 control-only recovery 入口。成功的 mutation 类入口返回调用方必须释放的
窄 lease；lease 暴露当前 `*lock.Ownership` 供嵌套流程复用，但内层只释放自己的 guard。一个
仅供 package 测试的 concrete operations seam 包装真实函数，用于捕获顺序和失败短路，不形成
通用 DI。

`internal/paths` 仅增加共享的词法 target/control 检查，以便 `internal/runtime` 区分永久 state
语义错误与当前 identity/topology 错误；完整文件系统关系仍由现有 `ValidatePathBoundaries`
决定。不新增依赖。

## Surprises & Discoveries

- Observation: strict `manifest.Load` 返回其实际 `Requirement`，但只检查 pre-read requirement
  不能证明 strict snapshot 的声明仍满足当前 binary。
  Evidence: `internal/manifest.Repository.Requirement` 与两阶段 API 独立存在。
  Impact: strict Load 后再次调用 `Satisfies`，结果以 strict requirement 为准。

- Observation: state 的永久词法控制面重叠与后来形成的控制面 alias 都会触发完整 path boundary，
  但规范要求前者 corrupt、后者只属于当前拓扑不安全。
  Evidence: `docs/05-apply-engine.md` §2 与 `paths.ValidatePathBoundaries` 的统一
  `ErrTargetControlOverlap`。
  Impact: 在同一 paths 职责中增加只读词法 target/control gate，再执行 identity/topology gate。

- Observation: “只有 init 保留 config-missing 状态”限制的是向调用方暴露提交前提，并不表示
  repo-only recovery 必须在 config 文件缺失时失败。
  Evidence: `PreflightRepository` 的已接受契约与 `docs/04-cli-spec.md` §4.8–§4.9 都只要求已有
  config 严格，并允许从 flag/env/default 解析 repo/control。
  Impact: recovery mutation 与 control-only recovery 都使用 repository-only preflight；前者在
  控制面校验后持锁，后者保持零锁。

- Observation: 首轮 worker 验证只检查 read-only 的 lock 不存在，以及 control-only recovery
  的 state bytes/lock，没有实际捕获完整临时树。
  Evidence: reviewer 对 `af990e80f1fe...63248a2` 的 P2 finding；原测试断言范围小于 ExecPlan
  记录的“临时树快照”。
  Impact: 用 `filepath.WalkDir` + `os.Lstat` 对 `t.TempDir()` fixture 做完整前后快照；symlink 只
  读取链接文本、不跟随外部路径。零写入证据现在与计划表述一致。

## Decision Log

- Decision: 用多个显式入口表达 full、nested、read-only、init 与恢复语义，不暴露组合 policy
  flags。
  Rationale: lock、manifest/state 消费和 config-missing 是安全契约，布尔组合会允许无意义或危险
  状态。
  Date: 2026-07-19

- Decision: 用统一 lease 封装 acquired owner 或 reused guard，并在 post-lock 失败路径集中释放。
  Rationale: 调用方需要在完整 mutation 周期持锁和传递 owner，同时每层只能释放自己的引用；
  集中 cleanup 可以用错误链保留原始 cause。
  Date: 2026-07-19

- Decision: dot git/update pull 的恢复 mutation 使用 repository-only preflight，不要求 profile/data。
  Rationale: 恢复前置只消费 repo/control；config missing 可使用更高优先级或默认 repo，已有 config
  仍完整严格校验。进入 apply 时 nested full loader 再要求普通完整 preflight。
  Date: 2026-07-19

## Outcomes and Handoff

Milestone 实现、复核修复与本地最终门禁均已完成。分支 base 为 `af990e80f1fe`，语义 commits
为计划起点 `6bf7f9f`、完整 full/read-only/nested loader `7f7f7c9`、init/recovery 入口
`7929613`、worker 验证证据 `63248a2`、只读零写入回归 `4c2dc55` 与复核证据 `9dbd9bd`。
实现没有新增依赖、CLI 或持久格式；所有文件系统测试只使用隔离临时根。

full loader 固定 preflight→lock/reuse→requires→strict manifest→strict requirement 复核→state→
路径边界的顺序；read-only 共享相同加载语义但不取锁。state 永久词法控制面重叠保持
`ErrCorrupt`，当前 topology/alias 错误保持 `ErrPathValidation`。init 在锁后加载 manifest 但跳过
state；repo-only recovery mutation 允许 config missing、已有 config strict，并持锁跳过
manifest/state；control-only recovery 零锁。nested 失败只释放自己的 guard，外层 ownership
继续覆盖 update 周期。

首轮独立 review 的 1 项有效 P2 已由 `4c2dc55` 以整树快照测试修复：read-only 的 missing、
loaded 与全部 fail-closed state 分支，以及 control-only recovery，均比较调用前后路径、类型、
mode、普通文件 bytes 与 symlink 文本，并保留 lock 不存在断言。第二轮对完整 branch 复审 GO，
无 P0–P3 finding；未改生产逻辑。

主 agent 在最终 HEAD 上再次运行相关 package 20 次重复测试、完整 base diff check 和
`make check`，全部通过；此前 worker 的 race 与 Darwin/Linux amd64 交叉编译也通过。Linux 未
实际运行，精确 branch HEAD 的远端 macOS/Linux CI 未运行，因此结论为“本地验收通过、远端待
验收”。
