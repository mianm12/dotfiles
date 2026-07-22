# chore/m1-cp7-orchestration：交付 M1 hooks 与 init

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

本计划是用户明确选择的“一条 Checkpoint Goal 编排多个 branch”的 coordinator 记录；每个
Milestone 另有独立 branch、active ExecPlan、semantic commits 与 review 单元。本文件只记录
DAG、调度、基线、跨 Milestone 发现、验收证据和最终结果，不重新定义产品契约。

## Purpose / Big Picture

完成后，M1 的 string-form `run_once` 会在真实 `apply` 中按规范串行执行并以 at-least-once
语义落账；`dot init` 会严格、幂等地收集 profile/data、以 0600 原子发布机器配置，并可在同一
锁所有权下立即执行完整 apply。用户可通过隔离 synthetic HOME 的端到端测试、完整本地门禁和
Checkpoint Acceptance 直接观察 hook 顺序/作用域/失败恢复，以及 init 的严格合并、Precond、
无终端零写入、嵌套 apply 与第二次运行幂等。

## Scope / Non-goals

范围内：

- `feat/hooks-run-once`：真实 hook observation/executor、apply hook phase、HookOutcome、file/prune/
  hook 单次 state commit，以及 apply CLI 的 stdin/stdout/stderr 和结果投影。
- `feat/init-config`：manifest data declaration 的不可变读取接口、机器配置完整 snapshot、严格
  merge/encode、0600 原子发布、提交时 Precond、repo override provenance 与绑定 InitSession 的
  config commit capability。
- `feat/init-interactive`：`dot init`、`--set`/`--yes`、用户终端交互、无终端判定、配置提交后
  同 ownership 的 nested apply，以及 README 当前实现状态。
- 每个 Milestone 的独立实现复核、freshness gate、FF-only main 集成，以及整个 Checkpoint 的
  三路独立 Acceptance。

明确不做：

- 不实现 M2 watch/table-form hooks、`from_env`、managed/rendered、TUI、并行 hooks 或 exactly-once。
- 不实现 bootstrap、self-update、update、git、release、真实机器迁移或主力 HOME 验证。
- 不新增 hook runner、shell parser 或 sandbox；hooks 使用标准库 `os/exec`。
- 无明确必要性不引入 `golang.org/x/term`；普通输入使用 `bufio` 与可注入 I/O。
- 不改变 state v1、ownership、prune、backup、Precond、accepted-risk 或其他规范契约。

## Contract and Context

- `docs/02-architecture.md` §2–§6：mutation 锁、机器配置 0600、init 严格更新、pipeline 顺序和
  file/run_once 单次 state transition。
- `docs/03-manifest-spec.md` §2–§4：M1 data/default、string-form run_once、严格 schema 与整键合并。
- `docs/04-cli-spec.md` §2–§4.2：统一退出码、`dot init`、无终端条件、repo/profile 持久化和
  nested apply 行为。
- `docs/05-apply-engine.md` §2/§4/§6/§8：state fail-closed、同 ownership、hook 顺序、cwd/env/
  stdio、执行分类、指纹、at-least-once、失败后缀停止与成功前缀落账。
- `docs/06-templates.md` §3：data 只从机器配置进入确定性渲染，不在渲染期读取环境或 manifest
  default。
- `docs/07-bootstrap-and-release.md` §2：bootstrap 交给 init 的 repo 必须持久化到下一进程。
- `docs/08-testing.md`：hook/init 必测矩阵与 synthetic HOME/repo/config/state/backup/XDG 隔离。
- `docs/09-roadmap.md` §1/§3：本 Checkpoint 只交付 M1 hooks/init 切片。

Checkpoint 基线是本地 clean `main@1df57addac93c48bc1497f1be15aa182a3730ce6`；它包含已完成
CP6 coordinator `012820fb006a5c35b339a2a083b78335eb8c65d0` 和 CP7 prerequisite。Plan Gate 时
`origin/main=e9e8bac6e5c1406e0db8aeb6e9eca6194aeeddb2`，本地 main 相对它 ahead 237；没有
`upstream` ref。本 Goal 不 fetch/pull，current local main 是唯一 checkpoint_base。

现有底座包括 canonical ApplyPlan/HookPlan、严格 state v1 `run_once`、统一
`state.Transition(Loaded, ChangeSet)`、InitSession 与 nested mutation ownership。真实缺口是：
apply 仍在 mutation 前拒绝全部 hooks，file/prune 后立即提交 state；config 只有严格 Load，缺少
publisher/Precond/provenance/data declaration；CLI 尚无 init，apply runner 尚不能消费已有 nested
mutation。

## DAG and Scheduling

```text
Wave 1 base: checkpoint_base

feat/hooks-run-once ──┐
                      ├──> feat/init-interactive
feat/init-config ─────┘
```

Wave 1 允许并行的冻结边界：hooks 节点独占 `internal/apply`、hook executor 和 apply CLI outcome；
init-config 只触及 manifest/config/runtime init capability，不接 Cobra、不接 apply。若 hooks 节点必须
修改 InitSession/config publisher，或 init-config 必须修改 apply/CLI，则立即取消并行并改串行。

Wave 1 预定合入顺序是 `feat/hooks-run-once` 后 `feat/init-config`。第二个分支在 freshness gate 中
只允许非重写合入由本 Checkpoint 已验收节点推进的 current main，再完整重跑测试与独立复核。
`feat/init-interactive` 只在两个前驱均 FF-only 合入 main 且 post-merge gate 通过后，从当时 main
创建。

同时最多四个 worktree：main integration checkout、coordinator，以及最多两个 Wave 1 worker。
reviewer 复用停止写入的 worker worktree。所有 worktree/branch 创建、main 集成和无 force 移除由
主 agent 串行执行。

## Progress

- [x] 2026-07-22：完整读取用户目标、执行规则、指定规范、当前代码/测试和 completed plans。
- [x] 2026-07-22：确认 main clean、CP6 与 CP7 prerequisite 已合入；目标 branches/worktrees 不存在；
  记录 checkpoint_base/main/origin/upstream，未 fetch/pull。
- [x] 2026-07-22：三路只读 Plan Gate 分别完成规范缺口、DAG/共享契约、测试/依赖/平台风险检查；
  无已确认规范冲突或前置阻塞。
- [x] 2026-07-22：baseline `make check BINARY=/private/tmp/dot-m1-cp7-plan-gate/dot` 在隔离
  Go/lint cache 下通过；首次未定向 cache 的尝试被 sandbox 拒绝，未运行到测试且未改仓库。
- [x] 2026-07-22：创建 coordinator branch/worktree 和本 active ExecPlan，冻结 DAG 与 Wave 边界。
- [x] 2026-07-22：以 `f8ea007` 提交 coordinator ExecPlan 起点；随后从 checkpoint_base 创建
  `feat/hooks-run-once` 与 `feat/init-config`，worker worktree 分别为
  `/private/tmp/dot-m1-cp7-hooks-019f8857` 和 `/private/tmp/dot-m1-cp7-init-config-019f8857`。
- [ ] Wave 1：`hooks-run-once` 经 Round 1 finding/fix 和 Round 2 完整 GO，已迁移计划并
  FF-only 集成 main@`0b6b979`，post-merge `make check` 通过；`init-config` 经三轮
  finding/fix/review 后 GO，已以 `4247347` 非重写合入 current main 并通过同步后
  `make check`，正在执行必需的 freshness 完整独立复核。
- [ ] Wave 2：从两前驱集成后的 main 交付 init-interactive。
- [ ] 对 checkpoint_base...main 执行三路独立 Acceptance，处理有效 findings。
- [ ] 更新 Outcomes/Handoff，迁移 coordinator plan，创建纯计划 closure 并 FF-only 合入 main。

## Milestone Coordination

### Wave 1A：`feat/hooks-run-once`

worker 从 checkpoint_base 创建独立 active ExecPlan，先以测试暴露 unsupported gate、缺少 hook
outcome 和 prune 后过早 state commit。实现必须从 canonical `plan.Hooks()` 消费，执行前重验
script 普通文件、bytes 与 direct/sh 分类；hook 位于 file/prune 后且不受收敛门控。失败停止未启动
后缀，成功前缀与 file/prune effects 经一个 ChangeSet 只 CommitState 一次。stdin/stdout/stderr
实时继承 CLI 注入流；测试完全隔离 HOME/XDG/repo/config/state/backup 并断言真实 HOME 不变。

预期 semantic boundaries：计划起点；hook executor/observation；apply phase/result/state transition；
CLI stdio/outcome；review fixes；plan closure。

### Wave 1B：`feat/init-config`

worker 从 checkpoint_base 创建独立 active ExecPlan。先补 manifest data declaration、完整已有配置
snapshot（含 optional repo）与 repo override provenance；再交付纯 candidate merge/validation 和
0600 原子 publisher。init 必须有不取锁的 read-only prepare，交互选择完成后才 BeginInit；锁内
重新 strict load，并以初始对象 kind/bytes/参与决策的 mode 作为 config commit Precond。绑定
InitSession 的 config capability 只允许一次成功发布，且 config commit 前不得进入 nested mutation。

预期 semantic boundaries：计划起点；manifest/config model；publisher/Precond；runtime init
capability/provenance；review fixes；plan closure。

### Wave 2：`feat/init-interactive`

从两前驱均验收合入后的 main 创建。新增 init runner/Cobra、重复 `--set` 的无歧义解析、profile/
data prompts 和“立即 apply?”。需要交互时从用户终端读取；如果无终端且 profile/data/apply 决策不
完整，必须在 config/state/lock/temp 写入前失败。配置成功后由 InitSession.BeginMutation 建立
唯一 child，并通过消费已有 mutation 的 apply runner 执行完整 hook-enabled apply；`--yes` 只传递
立即 apply 与整模块 prune 确认，不隐含 force/adopt。最终更新 README 当前实现状态。

预期 semantic boundaries：计划起点；init selection/runner；CLI interaction；nested apply；README/
端到端；review fixes；plan closure。

## Validation and Acceptance

| 必须成立的性质 | 证据 | 状态 |
|---|---|---|
| Plan Gate 基线可信 | clean main、三路只读复核、baseline make check | 已通过 |
| hook 顺序/scope/cwd/env/exec-sh/stdio | planner/executor/apply/CLI tests | 已通过（local） |
| hook at-least-once/失败后缀/成功前缀/单次 state commit | apply/state failure tests | 已通过（local） |
| init strict merge/repo persistence/0600/Precond | manifest/config/runtime tests | 已通过分支门禁，freshness review 中 |
| init 无终端零写入/nested ownership/第二次幂等 | CLI 真实进程 synthetic fixture | 待验证 |
| 每个 Milestone review/freshness/FF-only/post-merge | branch ExecPlans + main evidence | 待验证 |
| checkpoint_base...main 完整三路 Acceptance | independent reviewer reports | 待验证 |
| 完整本地门禁 | diff check + isolated-cache make check | 待验证 |
| 远端 macOS/Linux CI、真实 Linux/私人配置 | 外部证据 | 未运行，远端待验收 |

每个 worker 至少运行其窄 race tests、branch diff check 和隔离 cache 的 `make check`。最终从 main：

    git diff 1df57addac93c48bc1497f1be15aa182a3730ce6...main --check
    GOCACHE=/private/tmp/... GOLANGCI_LINT_CACHE=/private/tmp/... make check BINARY=/private/tmp/.../dot

必要的 Darwin/Linux `go test` 交叉编译只记为 compile-only，不外推为远端平台执行通过。

## Safety, Authorization, and Recovery

用户目标已明确授权本 Checkpoint 定义内的 branches、`/private/tmp` worktrees、代码/测试/必要
README/构建/依赖/ExecPlans 的 stage/commit、current main freshness merge、失败 merge abort、
FF-only local main 集成与 clean worktree 无 force 移除。未授权 fetch/pull/push/rebase/cherry-pick/
squash/amend/reset/force、branch 删除、PR/tag/release 或真实私人数据访问。

所有 mutation 测试必须使用唯一绝对 synthetic HOME/repo/config/state/backup；清除或重定向
DOT_CONFIG/DOT_REPO，hook HOME/XDG 也在同一临时根，并显式断言真实 HOME sentinel 未变化。
失败保留最近成功 semantic commit；有效问题使用新 fix commit。语义冲突、未知 main 提交、
Precond/ownership 无法证明、三轮 review 仍有 blocker 或缺少独立 reviewer 时更新本计划并停止。

## Interfaces and Dependencies

Wave 1 的共享冻结点是 checkpoint_base 的 canonical ApplyPlan、state ChangeSet 与 runtime ownership。
hooks 节点不得修改 InitSession/config contract；init-config 不得修改 apply/CLI。init-interactive 可以在
两前驱合入后增加消费已有 nested MutationSession 的窄 apply 接缝，但不得形成第二套 planner、
state commit 或锁所有权。

当前不新增依赖。`/dev/tty` + `bufio` 足以表达目标平台的普通用户终端输入；只有实际发现需要
IsTerminal/raw/password 能力时才调查 `golang.org/x/term` 的版本、license、Go directive、维护与
传递依赖，并按用户目标的依赖 commit 规则处理。

## Surprises & Discoveries

- Observation: `runtime.BeginInit` 当前在 strict manifest/交互前取得锁并会创建 state root/lock。
  Evidence: `internal/runtime/session.go` 的 BeginInit/Load 顺序；Plan Gate 测试复核。
  Impact: init 必须新增不取锁的 read-only prepare；不能用现有 BeginInit 直接包住提示流程，否则
  无终端缺输入时违反零写入。

- Observation: `InitContext.ExistingMachine` 丢弃 optional repo，路径 resolver 也不暴露 repo override
  来源。
  Evidence: `internal/runtime/preflight.go` 的 MachineContext/loadedContext 与 `paths.Repository`。
  Impact: init-config 必须先建立完整配置 snapshot 和显式 --repo/DOT_REPO provenance，才能安全
  保留或持久化 repo。

- Observation: apply 当前在 file/prune 后立即 TransitionEntries/CommitState，Result 不含 HookOutcome。
  Evidence: `internal/apply/execution.go`、`internal/apply/result.go` 和 completed CP7 prerequisite handoff。
  Impact: hooks 节点必须把 commit 上移到 hook phase 后，用一个 ChangeSet 合并所有成功 effect。

- Observation: hooks 首轮复核发现 runtime outcome 使用了 `run-hook (failed)` 和
  `run-hook (deferred)`，超出 CLI 规范的 verb 闭集。
  Evidence: Round 1 finding 与 `af40f7b` 修复；Round 2 复核确认所有 runtime hook 均使用
  `run-hook`，结果只由 reason 表达。
  Impact: 修复后无持久化或退出码契约变化；hooks milestone 已完整 GO 并集成。

- Observation: init-config 复核先后发现 missing-config 发布竞态、显式 profile override
  优先级错误，以及 hard-link 发布成功后 temp cleanup error 使磁盘事实与 session gate
  分裂。
  Evidence: `184e20c`、`ed18759`、`a7bd19d` 与 Round 3 完整 GO；publisher 现显式返回
  sealed `Changed/Committed`，runtime 在返回 post-commit cleanup error 前先同步 capability gate。
  Impact: missing path 使用 no-replace 发布，显式 override 在 pure/locked 两阶段保持优先；
  pre-publish failure 可重试，post-commit error 不再把已发布配置误判为未提交。

## Decision Log

- Decision: 采用 `hooks-run-once` 与纯 `init-config` 的条件并行 Wave，预定按 hooks 后 init-config
  合入，再串行 init-interactive。
  Rationale: 两节点不共享持久化契约或 production 文件；init-interactive 同时消费两者以及 nested
  apply，必须后置。不确定或越界时默认串行。
  Date: 2026-07-22

- Decision: 不引入 `x/term`，除非实现证明普通 `/dev/tty` 行输入不足。
  Rationale: 目标平台仅 macOS/Linux，不需要 raw/password/Windows；避免无必要依赖与 x/sys
  升级风险。
  Date: 2026-07-22

- Decision: init 的交互选择发生在任何 BeginInit/lock mutation 之前；锁内重新 strict load 并以
  read-only prepare 的 config observation 作为发布 Precond。
  Rationale: 同时满足无终端零写入、manifest 严格性、配置提交时 freshness 和嵌套 apply 的同一
  ownership。
  Date: 2026-07-22

## Outcomes and Handoff

尚未收口。当前已完成 Plan Gate、基线验证、DAG 与边界冻结；下一步是提交本 coordinator
ExecPlan 起点，然后从 checkpoint_base 创建两个 Wave 1 worker branches/worktrees。
