# feat/init-interactive：交付可交互且复用锁所有权的 dot init

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和
`Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成后，用户可以在新机或已有配置上运行 `dot init`，通过用户终端选择 profile、更新 manifest
声明的 data，并决定是否立即 apply；也可以用 `--profile`、可重复 `--set key=value` 和 `--yes`
完成无人值守初始化。交互前不持锁、不写配置/state/temp；配置提交后若选择 apply，命令在同一
init 锁所有权内复用现有 apply pipeline，因此不会自锁、重复规划或重复提交 state。测试可直接
观察无 TTY 零写入、repo 持久化、hook stdio、prune 确认、损坏 state 的提交边界和第二次运行幂等。

## Scope / Non-goals

范围内：

- 新增 `dot init` Cobra 命令，以及 `--set key=value`、`--yes` 的解析、校验和帮助文本。
- 在 lock-free `runtime.PrepareInit` 后，以标准库 `/dev/tty`、`bufio` 和可注入 I/O 完成 profile、
  data、立即 apply 决策；无终端且输入不完整时在任何 config/state/lock/temp 写入前失败。
- 明确区分省略与显式空 data，保守拒绝重复 `--set`、非法语法和未声明键；`--profile` 与 repo
  override 继续由既有 runtime/config 单一真相源校验并持久化。
- 配置提交后用同一 `InitSession` 创建 child `MutationSession`，给 apply 增加只消费既有 session
  的窄入口，复用现有 planner、executor、prune、hook 与单次 state commit。
- `--yes` 只授权立即 apply 与 whole-module prune 确认，不打开 force/adopt；配置提交成功后 state
  或 apply 失败时保留配置并准确失败；补齐 README 当前实现事实。

明确不做：

- 不实现 bootstrap/self-update/release、M2 `from_env`、managed/adopt、update/git 或新持久化格式。
- 不改变 manifest、state、ownership、prune、hook 或配置发布的规范语义，不引入新依赖。
- 不读取或修改真实 `modules/`、机器配置、state、backup、`.env` 或 HOME；不迁移本计划到
  `completed/`，独立复核和 lifecycle closure 由父任务协调。

## Contract and Context

- `docs/02-architecture.md` §2–§6：mutation 完整周期使用一次锁所有权；嵌套流程不能自锁；配置、
  state 和控制面路径严格校验，动作/state 变化按既有 pipeline 提交。
- `docs/03-manifest-spec.md` §2–§4：profile/data 来自严格顶层 manifest；data 显式空合法，已有值
  优先于 manifest default，缺少当前声明的必填值必须在 plan 前失败。
- `docs/04-cli-spec.md` §2–§4.2：init 的交互、持久化、Precond、无 TTY、`--yes` 与嵌套 apply 是公开
  契约；`--yes` 不放宽 force/adopt/owned/prune 收敛条件。
- `docs/05-apply-engine.md` §2/§4/§6/§8：损坏 state fail closed，但不阻止 init 配置提交；apply
  继续保持 file → prune → hook、Precond、at-least-once hook 与一次 state commit。
- `docs/06-templates.md` §3、`docs/07-bootstrap-and-release.md` §2：data 命名空间稳定；管道启动时
  init 从用户终端而非 stdin 交互。
- `docs/08-testing.md`、`docs/09-roadmap.md` §1/§3：以完整 synthetic HOME/repo/config/state/
  backup/XDG 证明 M1 init/apply 与失败安全，不把静态或交叉编译证据升级成真实平台运行。

基线为 `main@0844a846137b85fdba5925e5c5e72d777a548184`。已完成的
`.agent/plans/completed/m1-init-config.md` 提供 immutable `InitInputs`、pure `BuildCandidate`、0600
Precond publisher、`LoadedInit.CommitConfig` 和 commit 后 `InitSession.BeginMutation` gate；
`.agent/plans/completed/m1-hooks-run-once.md` 提供真实 apply 的 file/prune/hook 与单次 state commit。
当前 `internal/cli/cli.go` 尚未注册 init，`internal/cli/plan.go` 的 apply 投影和 prune 确认可复用；
`internal/apply.Run` 固定自行 `runtime.BeginMutation`，这是嵌套 init 会自锁的唯一必要 seam。

## Progress

- [x] 2026-07-22：确认隔离 worktree、`feat/init-interactive`、clean 基线与任务授权；读完计划规范、
  指定产品规范、completed init-config/hooks 计划及相关 CLI/runtime/config/manifest/apply 代码测试。
- [x] 2026-07-22：创建 active ExecPlan，冻结交互配置与同 ownership apply 两个串行 milestone；
  下一步先提交独立计划 checkpoint。
- [x] 2026-07-22：Milestone 1 完成未注册的 init 决策层，覆盖 `--set` presence/重复/未知键、TTY
  profile/data/apply、`--yes` 无歧义免 TTY、无 TTY 零写入和 real HOME sentinel；
  `go test -race ./internal/cli` 通过。
- [x] 2026-07-22：Milestone 2 新增 `RunWithMutationSession`，消费并关闭既有 session，普通 `Run`
  继续自行 begin；持锁集成测试证明无第二次取锁，nil/already-loaded 准确失败；
  `go test -race ./internal/apply ./internal/runtime` 与 package lint 通过。
- [x] 2026-07-22：Milestone 3 注册公开 `dot init`，完成 config-only/repo persistence、nested apply、
  hook stdio、corrupt state、prune/`--yes`、conflict 非 force 与第二次收敛测试，更新 README；
  `go test -race ./internal/cli ./internal/apply ./internal/runtime ./internal/config` 与 package lint 通过。
- [x] 2026-07-22：Darwin/Linux arm64 `go test -exec=true ./...` compile-only、base...HEAD diff check
  与 implementation HEAD `041b5f3` 的 isolated `make check` 全部通过；计划更新后将再跑最终 clean
  HEAD freshness gate。当前保持 active，等待未参与实现者完整复核与父任务协调 closure。
- [x] 2026-07-22：Round 1 独立复核给出唯一 P2：纯 exit 2/3 与 outer init close error 聚合后，root
  仍因 `errors.As(commandExitError)` 返回 2/3并隐藏 close failure。新增 init-local close precedence helper
  与 session-local seam；exit2/3 + close failure 现升级 exit1并打印 close error，close success 保留2/3，
  普通 error 与 close error 继续聚合。修复 commit `9de7a15` 的 targeted race、package lint、branch
  diff check 与 isolated exact-HEAD `make check` 全部通过；等待 Round 2 完整独立复核。

## Milestones

### Milestone 1：所有交互决策在锁和写入之前闭合

先用 CLI 级 synthetic fixture 固定 `--set` 解析、profile/data 默认、显式空、用户终端读取和立即
apply 拒绝路径，再新增只消费既有 `runtime.InitInputs` 的决策层。它只在 unresolved profile/data
或未由 `--yes` 明确的 apply 决策需要时打开读写 `/dev/tty`，形成 `InitSelection` 与 apply bool，
不注册公开命令、不取 lock、不写 config/state/temp。终端不可用、EOF、重复/非法 `--set`、未声明
键或必填值未解析均在调用方可能进入 mutation 前失败。

预计修改 `internal/cli/cli.go`、新增 init 专用 CLI 文件与测试；不复制候选合并规则。

Concrete steps：

    在 repo root 运行：go test -race ./internal/cli
    预期：init 解析、终端/无终端、零写入与既有 CLI 测试全部通过。

验收：

- 无 TTY 且缺少 profile/data/apply 任一决策时退出 1，synthetic config/state/lock/temp 全部缺失，
  real HOME sentinel 未变化。
- `--set key=` 保留显式空；重复 key、非法语法、未声明 key 在配置写入前拒绝。
- 用户终端而非 command stdin 提供选择，回车接受旧值/default；选择层不发生 mutation。

Commit 边界：

    feat(init): 建立零写入交互决策

### Milestone 2：apply runner 接受既有 mutation session

在 `internal/apply` 增加测试，证明新窄入口消费调用方提供的 `*runtime.MutationSession`、仍只执行
一次 strict Load/plan/execute/state commit 并负责关闭 child，不调用第二次 `BeginMutation`；普通
`apply.Run` 行为保持不变。该 seam 不注册 CLI，也不暴露 planner/state capability。

Concrete steps：

    在 repo root 运行：go test -race ./internal/apply ./internal/runtime
    预期：普通/既有 session apply 与 runtime ownership lifecycle 测试全部通过。

验收：

- caller-provided session 从 Load 到 Close 只消费一次；nil/已消费 session 准确失败。
- 新入口不 acquire 第二个 lock，普通 `apply.Run` 仍自行创建并关闭 session。

Commit 边界：

    feat(apply): 支持消费既有 mutation session

### Milestone 3：公开 init 复用同一锁所有权完成配置与 apply

注册 Cobra command；init 在 `runtime.PrepareInit` 与 Milestone 1 的选择阶段之后才调用
`BeginInit`、锁内 `Load` 与 `CommitConfig`。配置提交结果和 Close 错误原样参与最终失败，不把已提交
配置回滚。config committed 且选择 apply 后调用
`InitSession.BeginMutation`，把 child 交给同一 apply runner，并复用 CLI 的结果投影、backup 输出、
exit code 与 prune confirmation。`--yes` 只把 prune callback 固定为接受，Options 中 Force 保持
false；非 `--yes` 的 whole-module prune 继续走既有独立终端确认。最后用真实 synthetic hook 与
state 覆盖 config success + corrupt state、hook stdio、整模块 prune 接受/拒绝、无人值守、第二次
运行不产生文件 mutation 或重复 hook，并更新 README 当前事实。

预计修改 `internal/apply/run.go` 与测试、init/plan CLI 编排与集成测试、`README.md`。不新建 planner
或 state 提交路径，不更改 runtime session ownership 契约。

Concrete steps：

    在 repo root 运行：go test -race ./internal/apply ./internal/cli ./internal/runtime
    预期：普通/嵌套 apply、hook/prune/config-state 边界和 idempotence 测试全部通过。

    在 repo root 运行：GOOS=darwin go test ./internal/cli ./internal/apply -run '^$'
    在 repo root 运行：GOOS=linux go test ./internal/cli ./internal/apply -run '^$'
    预期：两个目标平台均 compile-only 退出 0；这不代表真实 Linux 运行。

验收：

- init 配置提交与 apply 从准备到 state commit 始终由同一 outer ownership 覆盖，不出现第二个锁、
  第二份 planner/state commit 或 self-lock。
- config 已提交后 corrupt state/apply/hook 失败保留配置并退出 1；成功 hook 使用 synthetic HOME/XDG
  与调用命令 stdio，real HOME sentinel 不变。
- `init --yes` 立即 apply 并确认 whole-module prune，但不能 force conflict/adopt；不带 `--yes` 的
  prune 仍要求既有确认且拒绝时退出 2。
- 相同输入第二次 init/apply 不重写等价配置、不产生 target mutation、不重复成功 run_once hook。

Commit 边界：

    feat(init): 复用锁所有权执行嵌套 apply

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| prepare/interaction 无锁、state-free、零写入 | CLI synthetic no-TTY/TTY tests | 已通过 |
| `--set` presence、重复与声明校验正确 | CLI parser/integration tests | 已通过 |
| 配置 0600、repo/profile/data 持久化且失败不回滚 | CLI/runtime/config tests | 已通过 |
| init/apply 使用同一 ownership 与一次 state commit | apply seam/runtime/CLI tests | 已通过 |
| hook/prune/`--yes`/corrupt state/第二次运行符合契约 | CLI filesystem integration tests | 已通过 |
| Round 1 close/unlock error 退出优先级与聚合 | CLI helper + real session close seam tests | 已通过 |
| Darwin/Linux compile-only | `CGO_ENABLED=0 GOOS=... GOARCH=arm64 go test -exec=true ./...` | 已通过 |
| Go、依赖、lint、race、build、manifest 门禁 | isolated `make check` | 已通过 |

最终从 repo root 运行：

    git diff 0844a846137b85fdba5925e5c5e72d777a548184...HEAD --check
    GOOS=darwin go test ./internal/cli ./internal/apply -run '^$'
    GOOS=linux go test ./internal/cli ./internal/apply -run '^$'
    GOCACHE=/private/tmp/dot-m1-cp7-init-interactive-gocache GOLANGCI_LINT_CACHE=/private/tmp/dot-m1-cp7-init-interactive-lint make check BINARY=/private/tmp/dot-m1-cp7-init-interactive-019f8857/dot

成功判据是所有命令退出 0、任务 diff 只含本计划范围、worktree clean。远端 CI、真实 Linux、真实
私人配置均未运行，不计入本地完成证据。

## Safety, Authorization, and Recovery

父任务已明确授权在隔离 worktree `/private/tmp/dot-m1-cp7-init-interactive-019f8857` 与分支
`feat/init-interactive` 内创建 active 计划、修改范围内代码/测试/README、stage 并创建小型语义
commits；明确禁止 fetch/pull/push/rebase/cherry-pick/squash/amend/reset/force、触碰 main/其他
worktree，以及 completed 迁移或 closure commit。该证据只适用于当前任务，不由计划延续。

所有 mutation 测试只使用 `t.TempDir()` 下绝对 synthetic HOME/repo/config/state/backup/XDG，显式
清除或重定向 `DOT_CONFIG`/`DOT_REPO`，并用 sentinel 断言真实 HOME 不变。终端测试用 session-local
注入，不打开真实 `/dev/tty`。每个 milestone 经窄测试和 diff check 后独立提交；失败保留上一个
checkpoint，用后续 fix commit 修正，不重写历史。配置已 committed 后的 state/apply 失败按规范保留
配置，重跑安全收敛。

## Interfaces and Dependencies

不新增 Go module 依赖或持久化版本。跨 package 数据流保持为：CLI 从 `runtime.InitInputs` 只读决定
`InitSelection`，既有 `BuildCandidate`/`LoadedInit.CommitConfig` 负责合并、校验与发布；commit 后
`InitSession.BeginMutation` 产生 child，`internal/apply` 的窄入口消费它并复用原有执行/结果协议。
标准库 `/dev/tty`、`bufio` 和 `io` 是唯一新增交互机制。apply seam 不暴露 planner/state 内部能力，
普通 `Run` 仍自行创建并消费 mutation session。

## Surprises & Discoveries

- Observation: 现有 apply CLI 已把计划/执行结果投影、backup 输出和 whole-module prune 终端确认集中
  在 `internal/cli/plan.go`，但 `internal/apply.Run` 的 `begin` 依赖固定在 runner 内。
  Evidence: `runMutationApply` 与 `runWithOperations/defaultRunOperations`。
  Impact: init 只需复用 CLI 投影并给 runner 增加既有 session 入口，不需要第二套 apply 编排。

- Observation: `runtime.InitInputs.BuildCandidate` 已保存显式空、旧 data、default、profile override 与 repo
  provenance；`InitSession` 已阻止 config commit 前创建 child，并将 child close 绑定 outer ownership。
  Evidence: `internal/runtime/loading.go` 与 `internal/runtime/session.go` 及 lifecycle tests。
  Impact: 交互层只产生明确 selection，不能重复实现合并或持久化规则。

- Observation: 把普通 runner 的 begin 阶段与共享的 session consumption 拆开后，既有 seam tests 仍能
  注入 begin，而 init 可以直接传 child；共享路径继续统一负责 Load、result sealing 与 Close。
  Evidence: `internal/apply/run.go` 的 `runWithOperations`/`runWithSession` 及持锁 integration test。
  Impact: 新入口无需暴露 planner/state capability，且通过再次 `BeginMutation` 后可用证明 child 已释放。

- Observation: config commit 与 apply 投影之间需要明确保留提交事实；先输出 deterministic config
  changed/unchanged 行，再进入 child apply，可让 corrupt state/conflict 的非零结果仍清楚显示配置已保留。
  Evidence: CLI corrupt-state 与 conflict integration tests 均先观察 config 行，再得到 exit 1/3。
  Impact: init 不伪装跨 config/state 的事务，也不需要更改 apply 的 Result/exit protocol。

- Observation: `errors.Join(commandExitError, closeError)` 会保留 command exit 的 error chain；root 的
  统一 `errors.As` 因而先返回 action/conflict code，既违反 1 > 3 > 2，也完全不打印 close error。
  Evidence: Round 1 reviewer finding 与 exit2/exit3 注入 close failure 的 CLI tests。
  Impact: precedence 必须在 init defer 局部消解，不能全局改变其他命令的 command exit 映射。

## Decision Log

- Decision: `--yes` 作为无人值守入口时不为已有/manifest default 的 data 打开终端；只有尚未明确的
  profile/required data 才交互。未带 `--yes` 时按声明询问未显式 `--set` 的 data，并询问立即 apply。
  Rationale: 既满足逐项交互与回车接受默认，也满足规范允许“全部值无歧义且 --yes”在无终端继续。
  Date: 2026-07-22

- Decision: 为让每个 checkpoint 都可独立解释且不注册半成品命令，把原两个 milestone 细分成
  “lock-free 决策层”“既有 session apply seam”“公开 init 集成”三个串行切片；Scope 与验收不变。
  Rationale: 决策层和 seam 均能用独立测试证明安全性质，公开命令只在完整配置/apply 语义闭合后注册。
  Date: 2026-07-22

- Decision: init 的 apply 使用现有 runner 的同一 Options/Result 协议，由新窄入口消费并关闭 child
  `MutationSession`；outer `InitSession` 仍由 CLI 最后关闭。
  Rationale: session 生命周期和错误聚合与普通 apply 一致，同时从类型与测试上排除第二次取锁。
  Date: 2026-07-22

- Decision: outer close 成功时原样返回 command exit；close 失败且结果动态类型精确等于 package 内
  sealed `commandExitError` value 时，用带原 code context 的普通 wrapped close error 替代该 sentinel；
  wrapped command exit 与其他 result error 继续与 close error join 并保留全部 cause。
  Rationale: 只修正 init 的 1 > 3 > 2 优先级，不吞普通 error cause，也不改变 root/其他命令行为。
  Date: 2026-07-22

## Outcomes and Handoff

实现已在 `feat/init-interactive` 形成以下 checkpoints：

    75b1ddb plan(init): 冻结交互初始化范围
    0b305ac feat(init): 建立零写入交互决策
    cdcb2c1 feat(apply): 支持消费既有 mutation session
    041b5f3 feat(init): 复用锁所有权完成初始化
    9de7a15 fix(init): 提升关闭失败退出优先级

结果覆盖 `dot init`、TTY/无 TTY、`--set` presence/重复、`--yes`、0600 config/repo persistence、同
ownership child apply、hook stdio、whole-module prune、corrupt state 后保留 config、conflict 非
force 和第二次收敛。targeted race tests、package lint、Darwin/Linux arm64 compile-only、完整 base
diff check 和 isolated `make check` 已通过；远端 CI、真实 Linux、真实私人配置均未运行。

Round 1 的唯一 P2 已在 `9de7a15` 处理并通过 exact-HEAD full gate；production 仍关闭真实
`InitSession`，测试 seam
先真实关闭再注入错误并重新取锁证明无泄漏。状态：active，等待 Round 2 未参与实现者对
base...HEAD 的完整只读复核。按父任务边界，本 worker 不迁移
`completed/`、不创建 closure commit，也不声称 review-ready；父任务负责处理复核 finding、freshness
gate 与最终集成协调。
