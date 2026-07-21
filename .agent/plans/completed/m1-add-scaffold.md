# feat/add-scaffold：安全发布 scaffold source 并建账

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和
`Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成后，内部 add runner 能消费已经通过完整 locked preflight 的 `ModeScaffold` batch：每项先用
既有 no-clobber publication 协议发布 `.template` source，始终保留原 target；只有 target 在
state 提交前仍满足执行前 bytes、九位 mode、identity、祖先拓扑与 control boundary，才把该项
加入 scaffold state 成功前缀。state 原子提交是 scaffold 的提交点；Store 失败保留 source，正常
apply 以 S1b 自动补录，后续删除 target 不会重建。

## Scope / Non-goals

范围内：

- publication 接受 sealed scaffold item，并保持 link 已验证的独立 inode、no-clobber、sync、
  等价续跑和保守 cleanup 协议。
- scaffold 单项执行只发布 source，不修改 target；state 候选形成前最终重验 target/source。
- runner 支持单一 `Request.Mode` 下的 link 或 scaffold batch，保持成功前缀、单次 state Store、
  invalid/矛盾结果 fail closed 和 M1 template 早拒绝。
- source/target/ancestor/control/state 故障、hard-link sibling、部分成功、S1b 恢复及 add→apply
  幂等证据。

明确不做：

- 不实现 managed/`--template`、混合 mode request、Cobra/CLI、README 或公开输出。
- 不修改 target、不创建 module、不修改 manifest、不执行 `git add`/`git commit`。
- 不改变 state v1、scaffold 非所有权语义、apply 决策表、publication cleanup contract 或引入依赖。
- 不读取或修改真实 modules、机器配置、state、backup、`.env` 或主力 HOME。

## Contract and Context

- `docs/02-architecture.md` §2/§4–§6：mutation 全周期持锁；成功动作才产生 state upsert，Store
  失败不回滚已发布对象。
- `docs/03-manifest-spec.md` §2–§6/§8：locked preflight 继续复用 prospective 严格 manifest、
  render 和完整 profile path boundary，不修改 manifest/module。
- `docs/04-cli-spec.md` §4.5：scaffold source 先发布、target 零修改、state 为提交点；提交前只
  清理仍可证明的 source，提交后不回滚。
- `docs/05-apply-engine.md` §1–§7/§9–§10：最终 Precond、S1b 收养、scaffold 非所有权生命周期与
  apply 幂等。
- `docs/06-templates.md`：scaffold 永不拥有 target，记录只表示一次性生成机会已满足。
- `docs/08-testing.md` §2–§3：隔离环境、提交点两侧、hard-link、恢复和不可删除回归。
- `docs/09-roadmap.md` §1/§3：本切片只交付 M1 scaffold add 内部 mutation。

有效 base 为 clean `main@cff70b6e220343130f447e482e9cc944629ffcad`，branch
`feat/add-scaffold`。前置 `feat/add-preflight` 已证明 prospective render bytes/desired mode 与
输入 snapshot 一致；`feat/add-link` 已固定 `publishSource`、最终 target/source Precond、sealed
per-item result、成功前缀 `state.EntryUpdate` 和一次 `CommitState`。当前 runner 只接受 link，且
把“成功 item”与“target mutation”合并为同一事实；本切片需要在同一协议内分开表达 scaffold
state effect 与 target 零 mutation。

## Progress

- [x] 2026-07-22：确认分配 worktree、branch、有效 base 与 clean 状态；完整阅读执行规则、CP6
  coordinator、completed preflight/link 计划、相关规范及 add/runtime/state/apply 实现。
- [x] 2026-07-22：以 `42ce69d` 提交本 active ExecPlan 起点。
- [x] 2026-07-22：以 `ddc10fc` 复用 publication、实现 scaffold 单项 target 零 mutation 与 runner
  state 提交点；覆盖 S1b、删除不重建、幂等、部分成功、Store failure、M1 template 零 lock 和
  executor protocol fail-closed。
- [x] 2026-07-22：以 `1f673cb` 补齐 scaffold publication create/write/file-sync/publish/dirsync/
  cleanup 故障，以及 Store 紧前最终 Precond 失败只提交更早前缀的回归；`3268ec8` 同步泛化内部
  runner 提交点术语。
- [x] 2026-07-22：add/apply/runtime/state 普通、add 5 次重复、add/apply race、定向 lint、完整
  base diff check、独立 cache/BINARY `make check` 和 Darwin/Linux amd64 add test binary 交叉编译
  通过。保持 active 等待独立复核。
- [x] 2026-07-22：Round 1 reviewer 报告一个有效 P2：scaffold `stateReady=true` 与 executor error
  的矛盾组合会被误提交。`4f8f9d6` 按 kind 收紧 protocol，scaffold 返回 invalid Result、零 Store，
  并保留无法信任的 source 供等价续跑；link post-target cleanup error 语义保持不变。修复后定向、
  add 5 次重复、add/apply race、完整 diff check 与独立 cache/BINARY `make check` 重新通过，等待
  Round 2 完整复审。
- [x] 2026-07-22：未参与实现的 Round 2 reviewer 复审完整 branch，结论 GO，无 P0–P3 finding；
  主 agent 确认 `main` 仍等于有效 base `cff70b6e`，最终窄测、完整 diff check 与隔离
  `make check` freshness gate 通过。
- [x] 2026-07-22：完成 ExecPlan 终态记录并迁移到 `completed/`；实现、测试与契约不再变更。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定 scope、提交点、恢复与验证边界。

Commit 边界：

    docs(add): 新建 add scaffold ExecPlan

### Milestone 2：发布 scaffold source 并保持 target 不变

先用 `internal/add` 真实文件系统与既有 publication seam 证明 `.template` source 走与 link 完全相同
的 create/write/chmod/sync/close/no-clobber/dirsync/cleanup 流程。增加 scaffold 单项 executor：发布
前取得 target 证据，发布后重新验证 source 与 target bytes/mode/type/identity/ancestor/control；
成功只返回可候选建账的 sealed fact，不对 target 做 rename、chmod、write 或 remove。最终 Precond
失败清理仍由本轮创建且未变化的 source；等价既有 source 不改写也不清理。

验收：target 与 hard-link sibling 的 bytes/mode/inode 全程不变；source 与 target inode 独立；
publication 全故障点、source/target/topology/control 变化和 cleanup ownership 均有测试。

Commit 边界：

    feat(add): 安全发布 scaffold source

### Milestone 3：提交 scaffold 成功前缀 state 并证明恢复

扩展 `internal/add/run.go` 的 sealed item result，使“item 已准备好提交 state”与“target 已 mutation”
分离；link 既有计数和行为保持不变。locked plan 必须全为 request 对应的单一 kind；scaffold 成功项
生成 `state.KindScaffold`、空 `LinkDest` 的 update，后项失败仍提交成功前缀。候选形成前再次执行
scaffold 最终 Precond；一旦进入成功 updates，就不再 cleanup source。Store 失败保留 source，并由
真实 apply S1b state-only adopt；成功 state 后删除 target，正常 apply 不重建，立即再跑保持幂等。

验收：多输入部分成功、Store failure、invalid/contradictory executor result、M1 template 零 lock、
等价 source 续跑，以及 add→apply→apply 的零 target mutation/adopt 均由隔离测试证明。

Commit 边界：

    feat(add): 提交 scaffold 成功前缀 state

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| scaffold source publication/cleanup 与 link 同一协议 | publication/scaffold seam 与真实 FS tests | 本地通过 |
| target bytes/mode/type/identity/topology/control 最终 Precond | shared target validator + scaffold executor/runner mutation tests | 本地通过 |
| target/hard-link sibling 零 mutation | inode/bytes/mode 回归 | 本地通过 |
| state 提交点、部分成功与 Store failure source 保留 | runner integration tests | 本地通过 |
| S1b 恢复、删除不重建与立即幂等 | add + apply recovery tests | 本地通过 |
| invalid result 与 M1 template fail closed | runner protocol tests | 本地通过 |
| Darwin/Linux | 本地测试、双目标交叉编译、远端 CI | Darwin 本地通过；双目标交叉编译通过；远端待验收 |

最终在 `/private/tmp/dot-m1-cp6-add-scaffold` 运行相关 add/apply/runtime/state package tests、5 次
重复、race、定向 lint、`git diff cff70b6e220343130f447e482e9cc944629ffcad...HEAD --check`、
唯一 `/private/tmp` cache/BINARY 的 `make check`，以及 Darwin/Linux test binary 交叉编译。成功
要求命令退出 0、完整 diff 仅含本计划与 scaffold mutation 实现/测试、worktree clean。真实 Linux
主机和远端 macOS/Linux CI 未运行时明确标记远端待验收。

## Safety, Authorization, and Recovery

用户已授权本 branch/worktree 的 active plan、范围内修改、stage、commit 和验证。测试只使用
`t.TempDir()` 的合成 HOME/repo/config/state/backup，显式隔离 DOT/HOME/XDG/Git；不运行涉及真实
数据的命令。失败使用新 fix commit，不 amend/rebase/reset/cherry-pick/squash；不切换或合并
main/其他 branch。

state candidate 前失败只能清理仍可证明为本轮创建且未变化的 source；一旦 item 进入成功 update
或 state Store 被调用，source 永不由本轮回滚。若既有 publication/result/state contract 无法在不
复制真相源或改变 state/ownership/公开行为下承载 scaffold，或无法证明 target 零 mutation与最终
Precond，则更新本计划并停止。

## Interfaces and Dependencies

不新增依赖。共享 contract 是 sealed preflight plan、source publication、per-item “source 已可用 /
state effect 可提交 / target 是否 mutation”事实、成功 `EntryUpdate` 和单次 runtime
`CommitState`。publication 不理解 ownership/state；scaffold executor 不重做 manifest/render；runner
不重做 path/Git 推断。`Result.TargetCommits` 继续只计 link target mutation，scaffold success 通过
outcome/state effect 表达，避免把 target 零写入伪装为 target commit。

## Surprises & Discoveries

- Observation: scaffold executor 完成 publication 后返回的 state-ready 事实仍早于 batch 的单次
  `CommitState`，只在 executor 内重验不足以满足“state 提交前最终 Precond”。
  Evidence: runner seam 可在 executor 返回后、Store 前替换 target identity。
  Impact: sealed per-item result 携带 publication 与 target evidence；runner 在形成 candidate 紧前按序
  重验，并仅提交仍有效的 scaffold 前缀。

- Observation: 用 outcome 状态反推要标记的 scaffold 前缀会把真实失败的后项误标为成功。
  Evidence: 两项 batch 中第一项 state-ready、第二项 executor/final Precond 失败的自审回归。
  Impact: runner 显式记录 prepared scaffold indices；Store 成功后只将实际进入 updates 的 indices
  标为 `OutcomeSucceeded`，Store 失败则都保持未越过提交点。

- Observation: link 与 scaffold 的 executor error 不能共享“state-ready 仍可信”判断。
  Evidence: Round 1 P2 构造 sealed scaffold result 同时返回 `stateReady=true` 与非 nil error；旧 runner
  仍追加 update 并在 Store 后投影成功。
  Impact: kind-specific protocol 只允许 link 在 target 已提交后携带 cleanup error；scaffold 的相同
  组合 fail closed，且 runner 不尝试清理无法信任其错误来源的 source。

## Decision Log

- Decision: 在共享 executor result 中分开 state-ready 与 target mutation，而不是把 scaffold 成功
  伪装成 target commit。
  Rationale: scaffold 的提交点是 state Store，规范同时要求 target 零 mutation；两个事实必须能被
  sealed result 与计数独立验证。
  Date: 2026-07-22

- Decision: scaffold 的第二次最终 Precond 位于 batch `TransitionEntries`/`CommitState` 紧前，并按
  稳定 plan 顺序截断为成功前缀。
  Rationale: 单次 state Store 与部分成功都必须保留；顺序截断能让更早的有效项建账，同时安全清理
  尚未越过提交点且仍可证明的后缀 source。
  Date: 2026-07-22

- Decision: scaffold executor 只有 `stateReady=true` 且 error 为 nil 才能进入 state candidate。
  Rationale: scaffold 的唯一提交点是 state Store；executor error 使 state-ready 事实自相矛盾，不能
  沿用 link 已越过 target 提交点后的 cleanup error 例外。无法证明 error 发生位置时保留 source，
  由等价 preflight 或 apply S1b 恢复。
  Date: 2026-07-22

## Outcomes and Handoff

Milestone 已完成并通过两轮独立复审。`ddc10fc` 复用既有 publication 与 sealed runner/state
协议，建立 scaffold source-first、target 零 mutation、Store 提交点、成功前缀与 S1b 恢复；
`1f673cb` 补齐 publication 故障和 Store 紧前最终 Precond 回归；`3268ec8` 修正泛化 runner 后的
术语。Round 1 的唯一有效 P2 由 `4f8f9d6` 测试先行修复：scaffold state-ready 与 executor error
的矛盾结果 fail closed、零 Store 且不清理无法信任的 source，同时保留 link post-target cleanup
error 语义。Round 2 reviewer 对完整 branch 给出 GO，无 P0–P3 finding。

主 agent 随后确认 `main` 仍等于有效 base `cff70b6e`，最终窄测、完整 diff check 与隔离
`make check` freshness gate 通过，无 unresolved blocking finding。完整 branch diff 仅包含本计划
与 `internal/add` 实现/测试；未新增依赖、CLI、README、持久化格式或 ownership 变化。

本机已通过相关包普通测试、add 5 次重复、add/apply race、定向 lint、完整 diff check、独立
`GOCACHE`/`GOLANGCI_LINT_CACHE`/BINARY 的 `make check`。Round 1 fix 后已重跑 add 5 次重复、
add/apply race、完整 diff check 与隔离
`make check`。Darwin/Linux amd64 add test binary 交叉编译通过。真实 Linux 主机和远端
macOS/Linux CI 未运行：本地 Darwin 验收与双目标交叉编译通过，远端待验收。本计划现迁移至
`completed/`；handoff 为由主 agent 确认 closure/worktree clean，并按 CP6 DAG 执行本地 `main`
fast-forward integration。本 Milestone 无剩余实现工作，满足正式分支流程的 review-ready 条件。
