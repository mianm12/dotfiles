# feat/init-config：建立可安全提交的 init 机器配置内核

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

本 Goal 交付 `dot init` 的非交互配置内核，而不是 CLI。完成后，后续交互层可以在不取锁、
不读 state、零写入的阶段取得 strict config 与 manifest 快照，纯函数地选择 profile/data/repo
并形成完整候选；真正提交时由持锁的 init session 重新严格加载上下文，以初次配置对象的
kind、bytes 和参与决策 mode 为 Precond 原子发布 0600 配置。测试可直接观察旧配置保留、
显式空值、repo override 持久化、等价候选不重写、Precond 失配不覆盖，以及配置提交成功前
不能进入嵌套 mutation。

## Scope / Non-goals

范围内：

- 为 manifest `[data]` 暴露不可变的 M1 声明快照，区分 prompt/default 是否出现。
- 为 machine config 建立包含 profile、可选 repo、data、原始 bytes、对象 kind 和 mode 的完整快照，
  并保留本次 effective repo 的 flag/environment/config/default 来源。
- 增加 lock-free、read-only 的 init prepare；strict config/manifest 失败时不 fallback，也不读取 state。
- 用纯模型合并、选择、完整校验并确定性编码 candidate，保留未声明旧 data，但只把当前 manifest
  声明传给选择逻辑；显式空值与省略分离。
- 增加 0600 atomic publisher；提交前复核准备时 Precond，等价候选不重写，所有失败保持旧配置。
- 将一次 config commit capability 绑定到 `InitSession.Load`；持锁重跑 strict preflight，成功配置
  commit 前拒绝 `BeginMutation`，config 阶段不读取或依赖 state。

明确不做：

- 不增加 Cobra `init` 命令、TTY/提示、`--set` 解析、apply 接线或 CLI 输出。
- 不修改 `internal/apply`、现有 CLI I/O、state 格式、manifest 格式或公开规范。
- 不实现 M2 `from_env`、重复 `--set` 冲突、bootstrap/update/self-update 或 hook executor。
- 不新增依赖，不为现有 config symlink/non-regular 行为发明新的接受或拒绝政策。
- 不读取或修改真实 `modules/`、机器配置、state、backup、`.env` 或 HOME。

## Contract and Context

- `docs/03-manifest-spec.md` §2：M1 `[data]` 只允许 prompt/default 字符串；空字符串是有效显式值，
  默认顺序以合法旧配置优先，未声明旧 data 不成为当前 manifest 输入。
- `docs/04-cli-spec.md` §4.1：init 严格读取和合并完整旧配置；显式 repo override 必须持久化；新配置
  0600 原子替换，初次 kind/bytes/参与决策 mode 是 commit-time Precond，失配不得 apply。
- `docs/08-testing.md` §3：init prepare 必须零写入；未知/损坏旧配置不 fallback；配置提交不依赖
  损坏 state；Precond 失配不覆盖。
- `docs/09-roadmap.md` §3：本切片只交付 M1 init 配置内核，不提前接入后续 CLI/hook 流程。

基线为 `feat/init-config@1df57addac93c48bc1497f1be15aa182a3730ce6`。当前
`internal/config.Load` 严格解码 `Machine` 但丢失原始对象证据且没有 encoder/publisher；
`internal/manifest.Repository` 只暴露 data key；`internal/runtime.PreflightInit` 的 machine snapshot
丢失 repo 与 repo 来源；`InitSession.Load` 只加载 manifest，且只要 Load 成功就允许嵌套 mutation。
实现必须沿现有 resolver/loading/session capability 分层闭合这些缺口，不建立并行真相源。

## Progress

- [x] 2026-07-22：确认隔离 worktree、`feat/init-config`、clean 基线和任务授权；读完计划规范、
  仓库指南、init/manifest/testing/roadmap 契约及相关实现测试。
- [x] 2026-07-22：创建 active ExecPlan，冻结本 Goal 与三个串行 milestone；下一步提交计划 checkpoint。
- [x] 2026-07-22：Milestone 1 完成 immutable manifest declarations、完整 machine/config
  Precondition snapshot、repo provenance 和 lock/state-free `PrepareInit`；
  `go test -race ./internal/manifest ./internal/config ./internal/paths ./internal/runtime` 通过。
- [x] 2026-07-22：Milestone 2 完成纯 `InitInputs.BuildCandidate`、完整 machine 校验、
  deterministic TOML candidate、0600 atomic publisher、等价 no-op 与 kind/bytes/mode Precond；
  `go test -race ./internal/config ./internal/runtime` 通过，全仓非 race tests 通过。
- [ ] Milestone 3：InitSession 单次 config commit capability、锁内 refresh 和嵌套 mutation gate。
- [ ] 完成窄范围 race、全仓 `make check`、任务 diff/untracked 检查，并更新 review-ready handoff；
  按父任务边界保留计划在 `active/`，不执行 completed 迁移或 closure commit。

## Milestones

### Milestone 1：只读准备保留完整、不可变的决策输入

先用测试证明 `[data]` accessor 保留 prompt/default presence 且返回副本；machine snapshot 同时保留
profile、可选 repo、全部 data、原始对象证据；repo resolution 能区分 flag、`DOT_REPO`、config
和 default 来源。随后让 runtime 的 lock-free prepare 严格加载 config 与 manifest，但不 acquire
lock、不读取 state、不创建 config 临时文件。已有未知/损坏配置必须原样报错；缺失配置合法。

预计修改 `internal/manifest/repository.go`、`internal/config/config.go`、`internal/paths/paths.go`、
`internal/runtime/preflight.go`、`internal/runtime/loading.go` 及同包测试。保持现有公开入口可用；新增
返回值采用 copy-on-read，不把 schema 内部 map/pointer 暴露给调用方。

Concrete steps：

    在 repo root 运行：go test -race ./internal/manifest ./internal/config ./internal/paths ./internal/runtime
    预期：新增 presence/provenance/prepare 零写入测试与既有 strict load/runtime 测试全部通过。

验收：

- old undeclared data 只存在于 machine snapshot，不出现在 manifest declarations。
- explicit repo flag/environment provenance 可识别，effective repo 仍是已校验绝对路径。
- prepare missing/existing config 均加载 manifest；unknown/corrupt config 无 fallback。
- prepare 不创建 lock/state/config temp，不读取 corrupt state。

Commit 边界：

    feat(init): 建立只读配置准备快照

### Milestone 2：严格候选与 Precond 原子发布闭合配置边界

先增加纯选择模型测试，覆盖 profile/data 默认选择、显式空值、未提供字段保留、未知声明拒绝、
完整 candidate 验证、repo override 持久化与确定性编码；再增加 publisher fault/Precond 测试，覆盖
0600、atomic cleanup、kind/bytes/mode 变化、等价候选 no-op 和所有失败旧配置不变。候选由准备快照、
manifest 声明和明确 selection 形成；publisher 只能消费已校验候选及其初次 Precond，不允许调用方
拼装缺字段 TOML 绕过验证。

预计主要修改 `internal/config`，并按必要最小接口消费 manifest/runtime 的只读模型。原子发布复用
仓库已有 state publisher 的安全顺序，但 config 有独立的 Precond 与 0600 语义；临时文件只能位于
synthetic config 父目录，成功/失败都不得遗留。

Concrete steps：

    在 repo root 运行：go test -race ./internal/config ./internal/runtime
    预期：candidate 与 publisher 正反路径全部通过，fault/Precond 失败后旧字节和对象仍不变。

验收：

- omitted profile/repo/data 保留合法旧值；显式空 data 覆盖旧值，初次缺 profile/必填 data 拒绝。
- 未声明旧 data 原样保留，但 candidate 的默认选择只消费当前声明。
- 显式 `--repo` 或 `DOT_REPO` 持久化为同一 absolute repo；无 override 时保留旧 repo。
- candidate 必须整体合法，encoding 稳定；新文件最终 mode 为 0600。
- 等价 bytes+mode 可识别且不 rewrite；Precond 任一 kind/bytes/mode 变化或 IO fault 都不覆盖旧配置。

Commit 边界：

    feat(config): 原子发布严格 init 候选

### Milestone 3：session 只在配置提交后授予 mutation 权限

先修改 lifecycle/recovery 测试，证明 `InitSession.Load` 返回绑定当前 session 的单次 config commit
capability；Load 在持锁后重跑 strict config/context/manifest，配置成功提交前 `BeginMutation` 必须
返回 session-order 错误，等价 no-op commit 也算成功。错误 candidate、Precond 失配、publisher
失败或别的 session capability 都不能打开 gate。配置 commit 成功后，嵌套 mutation 沿现有 ownership
重新 strict preflight；损坏 state 只在后续 mutation Load 阶段 fail closed，不回滚已提交 config。

预计修改 `internal/runtime/session.go`、`internal/runtime/loading.go`、operation seams 与 runtime tests。
保持 recovery 和普通 mutation capability 语义不变，不接入 CLI/apply。

Concrete steps：

    在 repo root 运行：go test -race ./internal/runtime ./internal/config
    预期：session order、single-use、locked refresh、state-independent config commit 与既有 recovery tests 通过。

验收：

- Load 前、Load 后但 config commit 前、commit 失败后都不能 BeginMutation。
- Load 在锁内重新 strict load；准备后配置发生变化时 commit 不覆盖。
- 同一 capability 只成功 commit 一次，不能跨 session 使用；等价候选 no-op 后可以 BeginMutation。
- corrupt state 不阻止 config commit；选择 apply 时才由既有 mutation Load 拒绝。

Commit 边界：

    feat(runtime): 绑定 init 配置提交 capability

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| manifest/config snapshot 完整且不可变 | manifest/config 单元测试 | 待验证 |
| prepare lock-free、state-free、零写入 | runtime operation events + synthetic filesystem | 待验证 |
| merge/selection 区分 omitted 与 explicit empty | config table tests | 待验证 |
| repo provenance 与 override 持久化正确 | paths/runtime/config tests | 待验证 |
| candidate 整体严格有效且 encoding 稳定 | config candidate tests | 待验证 |
| 0600 atomic publish、cleanup、Precond/no-op 正确 | config publisher/fault tests | 待验证 |
| config commit capability 与 nested mutation gate 正确 | runtime lifecycle/recovery tests | 待验证 |
| Go、依赖、lint、race、build、manifest 门禁 | isolated-cache `make check` | 待验证 |

最终从 repo root 运行：

    git diff 1df57addac93c48bc1497f1be15aa182a3730ce6...HEAD --check
    GOCACHE=/private/tmp/dot-m1-cp7-init-config-gocache GOLANGCI_LINT_CACHE=/private/tmp/dot-m1-cp7-init-config-lint make check BINARY=/private/tmp/dot-m1-cp7-init-config/dot

成功判据是所有命令退出 0、任务 diff 只含本计划范围、worktree clean。远端 CI、真实 Linux 与真实
私人配置不属于本 Goal 的本地证据，不得声称已运行。

## Safety, Authorization, and Recovery

父任务已明确授权在隔离 worktree `/private/tmp/dot-m1-cp7-init-config-019f8857` 和分支
`feat/init-config` 内创建计划、修改范围内代码与测试、stage 并按语义 checkpoint commit；同时
明确禁止 fetch/pull/push/rebase/cherry-pick/squash/amend/reset/force、触碰 main/其他 worktree，
以及本分支的 completed 迁移和 closure commit。该授权只适用于当前任务，不由计划延续。

所有 mutation 测试使用 `t.TempDir()` 下的绝对 synthetic HOME/repo/config/state/backup，并显式
绑定 `DOT_CONFIG`/`DOT_REPO`；不得读取或写入真实路径。publisher fault 使用 session-local seam 或
包内确定性测试，不留全局 hook。每个 milestone 独立测试、stage、commit；失败保留上一个已验证
checkpoint，用后续修复 commit 继续，不重写历史。若实现需要为现有 config symlink/non-regular
对象新增公开接受/拒绝规则，或需要扩大到 CLI/apply/state 格式，立即更新 Progress 并请求裁决。

## Interfaces and Dependencies

不新增 Go module 依赖或持久化版本。必要跨 package 数据流为：manifest 暴露 immutable M1 data
declarations；paths/runtime 暴露 effective repo provenance；config 从 immutable preparation + explicit
selection 形成 sealed candidate 并通过 Precond publisher 提交；runtime 的 loaded init capability
协调一次 config commit 和后续 nested mutation gate。具体类型名可随测试驱动调整，但不得出现
另一套 config 解码、校验或 repo precedence 真相源。

## Surprises & Discoveries

- 现有 `config.Load` 已明确拒绝 dangling symlink，但对其他可打开对象沿用 `os.Open` 行为；规范只要求
  kind 参与 Precond，没有定义新的对象类型政策。因此本 Goal 不新增类型拒绝规则，只记录并复核
  初次实际对象证据。
- `InitContext` 原先只保留 profile/data，虽然 resolver 已用旧 repo 决定 effective path，却会丢失
  “旧 repo 字段缺失”和“repo override 来源”两项合并事实；Milestone 1 将它们随 strict snapshot
  一次保留，避免 candidate 阶段重新读取环境或配置。

## Decision Log

- 2026-07-22：将 lock-free prepare 与 locked session Load 分开。前者支持后续交互零写入并密封初次
  Precond；后者在 ownership 内重跑 strict load，避免把交互前上下文当作锁内事实。
- 2026-07-22：等价候选的 no-op publish 视为成功 config commit，因为配置已经满足完整 candidate，
  但不得发生 rename/mode mutation；这允许幂等 init 后进入可选 apply。
- 2026-07-22：repo 持久化完全由 preparation 中的 source 决定：flag/`DOT_REPO` 写入已解析 absolute
  repo，config source 保留旧原值，内置 default 且无旧字段时继续省略；candidate builder 不重新读环境。
- 2026-07-22：计划保持 `active/` 并只更新到 review-ready handoff；completed 迁移与 closure commit
  明确由父任务后续协调，不在本 worktree 授权内。

## Outcomes and Handoff

尚未实施。最终 handoff 将记录 milestone commits、窄范围 race 与 `make check` 证据、任何未验证
平台，以及独立复核所需的完整 Scope / Non-goals；本分支不自行迁移计划到 `completed/`。
