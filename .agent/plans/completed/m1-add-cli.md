# feat/add-cli：开放安全 add 命令

本 ExecPlan 是 living document。实施期间持续更新 `Progress`、`Surprises & Discoveries`、
`Decision Log` 和 `Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成后，M1 用户可以通过公开 `dot add` 把 HOME 中普通文件安全收编为 link 或 scaffold。
命令支持 `-m`、互斥的 `--template`/`--scaffold` 与 `--dry-run`；mutation 只调用已经封存的
`internal/add.Run`，dry-run 只用 `runtime.LoadReadOnly` 与 `add.Preflight`。输出稳定展示运行
context、逐项动作和手工 Git 提示，所有无效内部 plan/result 都 fail closed。

## Scope / Non-goals

范围内：

- 注册 `add [-m <module>] [--template|--scaffold] [--dry-run] <path>...`，校验至少一个路径和
  模式互斥。
- M1 `--template` 在 runtime IO、lock、source、target、state 前硬错误。
- dry-run 走严格只读加载与 sealed preflight，无锁且对全新 HOME 零写入。
- CLI 仅投影 sealed add plan/result/error，稳定输出 context、link/scaffold 动作和成功后的手工
  `git add`/`git commit` 提示；退出码遵循 `1 > 3 > 2 > 0`。
- module 推断零/多候选映射为 3，运行错误映射为 1；zero/invalid result + nil error 映射为 1。
- 全隔离 CLI 测试证明 link/scaffold、部分成功、hard-link、dry-run 与 add 后 apply 收敛；同步
  README 当前实现事实。

明确不做：

- 不复制 manifest、Git ignore、Precond、ownership、state 或恢复逻辑；不改变下层持久化契约。
- 不实现 M2 managed/`--template`，不创建 modules、不修改 manifest、不执行 Git stage/commit。
- 不新增依赖，不读取或修改真实 modules、机器配置、state、backup、`.env` 或主力 HOME。

## Contract and Context

- `docs/02-architecture.md` §2–§6：mutation 持锁、dry-run 无锁、CLI/计划/执行职责分离。
- `docs/03-manifest-spec.md` §2–§6/§8：add 仍经 strict manifest 与完整 profile 校验，CLI 不写 manifest。
- `docs/04-cli-spec.md` §2–§4.5/§5：公开 flags、输出、退出码、source-first 与 Git 提示。
- `docs/05-apply-engine.md` §1–§7/§9–§10：反向映射、提交点、恢复与 add 后 apply 幂等。
- `docs/06-templates.md`：M1 scaffold 与 `*.local`，managed/template 留给 M2。
- `docs/08-testing.md`：全隔离、dry-run 零写入、提交点/恢复与公开输出证据。
- `docs/09-roadmap.md` §1/§3：本切片只开放 M1 link/scaffold add。

有效 base 为 clean `main@9206981002dd4c0e0420b5e3c1fde5132aca1737`，branch `feat/add-cli`。
前三个 Milestone 已分别封存 prospective/Git 全批预检、link publication/target commit 和 scaffold
state commit。`internal/add.Preflight` 返回 sealed `BatchPlan`；`internal/add.Run` 返回 sealed
`Result` 并在协议违规时无效。`internal/runtime.LoadReadOnly` 是 dry-run 唯一加载入口。

## Progress

- [x] 2026-07-22：确认分配 worktree、branch、有效 base 和 clean 状态；读取执行规则、CP6 规范、
  coordinator 与前三个 completed plans，以及现有 CLI/add/runtime/apply 实现和测试。
- [x] 2026-07-22：以 `5a275b7` 提交本 active ExecPlan 起点。
- [x] 2026-07-22：以 `c6024d8` 测试先行注册公开 add、dry-run/mutation wiring、稳定投影与退出码；
  全隔离覆盖 link/scaffold、hard-link、部分成功、template/歧义/invalid result 和 apply 收敛。
- [x] 2026-07-22：以 `95623d8` 同步 README 当前 M1 add 事实。
- [x] 2026-07-22：自审以 `8f1206e`、`0f462c2`、`b2c8303`、`b7a6cfe` 分别补齐 compatibility
  notice/成功结果协议、空 module、sealed GOOS 与 state-committed fail-closed，并补 module 指引和
  公开 flag 回归。
- [x] 2026-07-22：相关包普通、5 次重复、全仓 race/lint、完整 base diff check、独立
  cache/BINARY `make check` 与 Darwin/Linux amd64 CLI test binary 交叉编译通过。
- [x] 2026-07-22：Round 1 reviewer 报告两个有效 finding：sealed `Result.Valid` 未按 kind 绑定
  succeeded/counter 事实，且 link target 已提交但 state Store 失败时 CLI 缺少明确恢复提示。
  `dd6bdc4` 与 `ca98d74` 分别测试先行修复；真实 partial/Store failure/post-commit error 语义保持。
- [x] 2026-07-22：Round 1 fix 后相关包普通、add/CLI 5 次重复与 add/CLI/apply race、完整 diff
  check、Darwin/Linux amd64 CLI test binary 交叉编译及独立 cache/BINARY `make check` 重新通过。
- [x] 2026-07-22：原 reviewer 对有效 base `9206981...HEAD` 完成 Round 2 全分支复审，结论 GO，
  无 P0–P3 finding；主 agent 确认 main 仍为有效 base，最终相关测试、完整 diff check 与隔离
  `make check` freshness gate 通过。
- [x] 2026-07-22：完成 ExecPlan 终态记录并迁移到 `completed/`；实现、测试与 README 不再变更。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定 CLI 与下层安全契约分界。

    docs(add): 新建 add CLI ExecPlan

### Milestone 2：固定公开参数与结果投影

先在 `internal/cli` 增加 runner seam 测试，证明命令注册、至少一个 path、`-m`、互斥模式、M1
template 早拒绝、sealed plan/result 投影和退出码优先级。投影只读取 accessors，不重做下层判断；
invalid/zero result + nil error 返回 `add.ErrExecutionProtocol`。动作输出按 plan 顺序稳定，context
取 sealed plan；只有已越过各自提交点的成功项进入 Git 提示。

    feat(cli): 注册安全 add 命令

### Milestone 3：连接只读与 mutation 路径并证明 E2E

dry-run 调 `runtime.LoadReadOnly` 后调用 `add.Preflight`，不取锁；mutation 调 `add.Run`。所有 runtime
override 都来自统一 global options。全隔离 fixture 创建 synthetic HOME/repo/config/state/backup 与
Git repository，直接覆盖 link/scaffold、module inference/显式 module、部分成功、hard-link、
dry-run 零写入及 add→apply→apply 收敛；下层已有 ignore/source/故障点覆盖不在 CLI 重复实现。

实际 E2E 与 wiring 测试随 `c6024d8` 对应实现提交，未拆成脱离实现的测试 commit。

### Milestone 4：同步当前实现说明

README 将 `add` 列入已实现命令，准确说明 M1 link/scaffold、dry-run、手工 Git 提示和 template
未支持；不改变规范。

    docs(readme): 记录 M1 add 能力

## Validation and Acceptance

运行 `go test ./internal/cli ./internal/add ./internal/apply`、相关重复/race、定向 lint，
`git diff 9206981002dd4c0e0420b5e3c1fde5132aca1737...HEAD --check`，使用 `/private/tmp` 独立
cache/BINARY 的 `make check`，以及 Darwin/Linux amd64 CLI test binary 交叉编译。所有 mutation
测试使用 `t.TempDir()` 的合成 HOME/repo/config/state/backup，显式隔离 DOT/HOME/XDG/Git，并检查
真实 HOME sentinel 不变。真实 Linux 与远端 macOS/Linux CI 未运行时明确标为待验收。

当前证据：`go test ./internal/cli ./internal/add ./internal/apply ./internal/runtime ./internal/state`
通过；CLI/add 5 次重复通过；最终隔离 `make check` 完成 `go mod tidy -diff`、format diff、0 lint
issue、`go test -race ./...`、build 和 manifest check；完整 base diff check 与 Darwin/Linux amd64
CLI test binary 交叉编译通过。前三个 Milestone 已直接覆盖 manifest/Git ignore/exclude、Git
unavailable、source variants、等价续跑及 link/scaffold 故障接缝；本分支不复制这些机制，只以 CLI
E2E 证明公开 wiring、输出和退出码。真实 Linux 主机与远端 CI 待验收。

Round 1 fix 后再次证明：counter 缺失的全 succeeded/state-committed sealed result 无效；真实 link
partial、Store failure、post-target cleanup error 与 scaffold Store failure 仍合法。恢复提示投影只
消费 valid result 的 `TargetCommits`/`StateCommitted`，同时保留成功 action、Git hint 与原始运行
error。随后 5 次重复、相关 race、完整 diff check、双目标编译和隔离 `make check` 全部通过。

## Safety, Authorization, and Recovery

用户已授权本 branch/worktree 的 active plan、范围内修改、stage、commit 与验证。失败使用新 fix
commit，不 amend/rebase/reset/cherry-pick/squash；不切换或合并 main/其他 worktree。若 CLI 必须
复制下层安全语义、改变公开/ownership/state 契约，或无法证明 dry-run/模板零写入，则更新计划并停止。

## Interfaces and Dependencies

不新增依赖。CLI seam 只替换 `add.Run`、只读加载/预检以覆盖协议投影；production 默认分别绑定
`add.Run`、`runtime.LoadReadOnly` 与 `add.Preflight`。context/action/result 均从 sealed plan/result
accessor 获得，CLI 不获得构造可信 plan/result 的能力。

## Surprises & Discoveries

- Observation: sealed `BatchPlan` 原先不携带 development compatibility 与实际 GOOS，CLI 若从
  build/environment 独立推断会形成第二个 context 真相源。
  Evidence: 首版投影只能用 `env.goos`，且无法输出 development notice。
  Impact: plan 在成功 strict preflight 时封存 GOOS 与 compatibility，只读/mutation 投影统一消费
  accessor；零值/伪造 plan 仍无效。

- Observation: `Result.Valid` 合法允许“已越过 link target 提交点但 state Store 失败”，供 non-nil
  error 的恢复投影；因此 validity 本身不能证明 nil-error 的整体成功。
  Evidence: link Store failure 需要保留 source/link、返回 valid result 与 error。
  Impact: CLI 在 `runErr == nil` 时额外要求全部 outcome succeeded 且 `StateCommitted`，否则按
  `ErrExecutionProtocol` fail closed。

- Observation: 公开 `-m` 的不存在/不在 profile 错误原先只有诊断，没有规范要求的手工修复步骤；
  显式 `-m=` 还会退化为 inference。
  Evidence: `validateExplicitModule` 与 Cobra string flag 的默认空值。
  Impact: 保持下层 module 校验为单一语义源并返回当前 profile 指引；CLI 在 runtime 前拒绝显式空值。

- Observation: 不需要新增 CLI 专用故障 seam 即可直接证明多输入部分成功。
  Evidence: 隔离 E2E 让排序靠后的 target parent 在预检可读、执行不可写，首项 link/state 提交而
  次项保持原文件。
  Impact: CLI 测试消费真实 `add.Run`，没有复制或绕过 publication/Precond/state 逻辑。

- Observation: `Result.Valid` 原先只校验 counter 上界，不能证明每个 succeeded item 都有 source
  publication，或每个 succeeded link 都对应一次 target commit。
  Evidence: Round 1 reviewer 可从真实 sealed success result 删除 publication/target counter，结果仍
  被 CLI 当作可信；scaffold 也可能伪报 target commit。
  Impact: validity 按 item kind 统计 succeeded；publication 覆盖全部 succeeded，link succeeded 数
  精确等于 `TargetCommits`，scaffold target commit 为零。失败项已发布 source 仍允许 counter 更大。

- Observation: link Store failure 的 valid result 已精确证明 target commit，但普通 runtime error
  文本本身不能安全指导 ownership/recovery。
  Evidence: Round 1 P2；`TargetCommits>0 && !StateCommitted` 是 sealed result 中唯一需要的事实。
  Impact: CLI 仅由这两个 accessor 生成明确 `rerun dot apply` warning，保留 action、Git hint 和随后
  的原始 error；scaffold/预提交错误不会误触发。

## Decision Log

- Decision: dry-run 与 mutation 分别消费 `Preflight` sealed plan 和 `Run` sealed result，不为二者
  建立新的共享可伪造 DTO。
  Rationale: 下层已经以 validity seal 固定完整预检和执行事实；CLI 应保持纯投影。
  Date: 2026-07-22

- Decision: CLI E2E 只直接覆盖公开 wiring/output/exit 与代表性的真实安全流；ignore、Git
  unavailable、source variants 和提交点故障继续复用前三个 Milestone 的下层测试。
  Rationale: 这些机制的唯一实现位于 manifest/add/runtime；在 CLI 重建同类 fault seam 会产生重复
  语义，真实 link/scaffold/partial/hard-link/apply convergence 已证明 production wiring。
  Date: 2026-07-22

## Outcomes and Handoff

实现与 Round 1 fixes 已完成并通过 Round 2 完整独立复核。当前 branch 提供公开 add flags、M1 template
早拒绝、只读 dry-run、sealed mutation result 投影、确定 context/action/Git 提示与 1/3/2/0 映射；
README 已同步。自审 fix 关闭 compatibility/GOOS 第二真相源、nil-error 未完整提交、空 module 与
缺失手工指引。无新依赖、持久化格式或 ownership 变化。

本机已通过相关包普通/重复、全仓 race/lint、完整 diff check、隔离 `make check` 和 Darwin/Linux
amd64 test binary 交叉编译；Round 1 两项有效 finding 修复后上述关键门禁已完整重跑。真实 Linux
主机和远端 macOS/Linux CI 未运行，远端待验收。Round 2 reviewer 结论 GO，无 P0–P3 finding；
主 agent 确认 main 仍等于有效 base `9206981`，最终相关测试、完整 diff check 与隔离
`make check` freshness gate 通过，无 unresolved blocking finding。本计划现迁移至 `completed/`；
handoff 为主 agent 创建纯计划收口 commit、确认 worktree clean，并按 CP6 DAG 执行本地 main
fast-forward integration。本 Milestone 无剩余实现工作。
