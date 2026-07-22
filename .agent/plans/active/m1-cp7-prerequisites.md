# codex/m1-cp7-prerequisites：闭合 CP7 前的执行协议与状态边界

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

本 Milestone 在 CP6 已交付的安全 `add` 与 CP7 的 M1 `run_once` executor 之间建立可信基线。
完成后，CP6 的测试不再受宿主 `DOT_*` / `GIT_*` 环境污染；真实 `apply` 只执行同一个有效
canonical `ApplyPlan` 中的动作；runner 返回给 CLI 的结果是密封且自洽的事实模型；state 包可在
保持 state v1 格式和单次 `CommitState` 的前提下，把 entry 与未来成功 hook 的 `run_once` 变化
合成一个候选 Snapshot。用户可通过 hostile-env 回归、injected protocol 测试和完整 `make check`
直接观察这些性质。

## Scope / Non-goals

范围内：

- 修复 `internal/add` 测试 fixture 的 HOME、XDG、DOT 与 Git 环境隔离，并补 hostile-env 回归。
- 移除 `internal/apply.executionPlan` 的 plan/actions 多真相源；真实 runner 只消费有效
  `planner.ApplyPlan`。
- 将 apply Result 改为私有字段、密封构造和只读 accessor；在 apply 层闭合 plan、逐项 outcome、
  物理提交、state effect、backup、确认与最终 state commit 的关系，CLI 只投影有效结果。
- 在不改变现有公开行为的前提下，把 apply orchestration 按 canonical preparation、file、prune、
  transition/result 职责在同 package 内拆分，保留部分成功与安全重跑语义。
- 扩展 state transition，使 entry upsert/delete 与 `run_once` upsert 通过同一严格 API 合成一个
  Snapshot；现有 add/apply 行为继续复用该单一真相源。
- 在规范中明确 CP7 hook 子进程 stdio 的透传边界，以及其输出不属于 dot 确定性摘要契约。

明确不做：

- 不执行 hook、不移除当前 `run_once` apply gate、不增加 hook outcome、subprocess、context 或
  shared hook observation；这些在 CP7 中与真实 executor 一起设计和验证。
- 不实现 M2 watch/table-form、并行 hook、跨模块依赖图、sandbox 或 exactly-once。
- 不实现 init/update/self-update/bootstrap/release，不提前修复它们的锁前 context refresh、config
  publisher 或交互模型。
- 不改变 state v1 持久格式、公开 CLI 输出/退出码、ownership、Precondition、backup、prune、
  manifest 或 accepted-risk 契约；不新增依赖、通用 transaction、WAL 或 package hierarchy。
- 不读取或修改真实 `modules/`、机器配置、state、backup、`.env` 或 HOME。

## Contract and Context

- `docs/01-overview.md` §4：保护对象是工具自身 bug 和日常事故，不扩展到恶意仓库或对抗性并发。
- `docs/02-architecture.md` §4–§6：canonical plan、执行职责、成功 effect 合并和单次 state 原子提交。
- `docs/04-cli-spec.md` §3–§5：退出优先级、apply 输出与确定性摘要。
- `docs/05-apply-engine.md` §6–§8：部分成功、Precond、hook at-least-once 与成功前缀落账。
- `docs/08-testing.md` §1–§4：mutation fixture 必须隔离 HOME/repo/config/state/backup 与 Git 配置。
- `docs/09-roadmap.md` §1/§3：本 Goal 只准备 M1 string-form run_once 的执行边界，不交付 CP7 hook。

当前基线是 clean `main@012820fb006a5c35b339a2a083b78335eb8c65d0`。CP6 本地 `make check`
在普通环境通过，但 `internal/add/preflight_test.go` 的 helper 继承 ambient `DOT_CONFIG` 与
`GIT_CONFIG_GLOBAL`；合成 hostile env 已证明其可读取或写入 fixture 外路径。生产 add 的
`gitEnvironment` 会过滤这些变量，因此缺口属于测试证据边界，不是已证实的生产越界。

`internal/apply/run.go` 当前由 `executionPlan` 同时保存 `value/files/prune/groups/hooks`，runner
执行独立 slices 却把 `value` 交给 CLI；`Result` 又暴露多个可矛盾字段，完整性校验散落在 CLI。
本 Goal 必须把 plan 与 result 两端都改成单一真相源，同时保持 production runner 当前已验证的
file、backup、prune、partial success、conflict 和 state Store 恢复行为。

`internal/state.TransitionEntries` 目前只处理 entries 并保留全部 `run_once`。state v1 已包含严格
run_once schema，runtime 只允许一次成功 `CommitState`；因此本 Goal 增加的是内存 transition
能力，不是持久化迁移。

## Progress

- [x] 2026-07-22：完成只读审查、三路独立复核、当前基线 `make check` 与 hostile-env 缺口定位。
- [x] 2026-07-22：创建 active ExecPlan，冻结本 Goal 的 Scope / Non-goals 和串行 milestone 顺序。
- [x] 2026-07-22：用户明确授权创建 `codex/m1-cp7-prerequisites`、按下述边界 stage/commit、
  完成计划 `active/` → `completed/` 迁移及 closure commit；不含 merge、push、rebase、历史重写
  或分支删除。runtime approval 后已创建分支。
- [ ] Milestone 1：关闭 CP6 add 测试环境隔离债。
- [ ] Milestone 2：收拢 canonical apply plan、sealed Result 与同包 phase 结构。
- [ ] Milestone 3：统一 entries/run_once state transition，并明确 hook stdio 规范边界。
- [ ] 完整门禁、独立复核、必要 fix、终态计划迁移与 review-ready handoff。

## Milestones

### Milestone 1：hostile env 下的 CP6 测试仍严格隔离

先增加能证明 ambient `DOT_CONFIG`、`DOT_REPO`、`GIT_CONFIG_GLOBAL`、`GIT_DIR`、`GIT_INDEX_FILE`
等变量不能改变 fixture 解析或 helper Git 写入位置的回归，再让 `newAddFixture` 显式绑定 synthetic
config/repo，并让 helper Git 使用与生产相同的过滤规则。测试只能在 `t.TempDir()` 内写入，并以
fixture 外 synthetic sentinel 未变化为验收；不得依赖当前 shell 恰好没有这些变量。

Concrete steps：

    在 repo root 运行：go test -race ./internal/add
    预期：普通与 hostile-env 子测试均通过，所有 Git/config 写入只存在于各自 t.TempDir。

验收：

- ambient DOT/GIT 变量不能让 fixture 读取或写入其临时根之外。
- Git local/global exclude 语义测试仍使用真实 Git 并继续通过。
- production `gitEnvironment` 行为不放宽。

Commit 边界：

    test(add): 隔离 fixture 的宿主配置环境

### Milestone 2：apply 执行协议只有一个可信 plan 和一个可信 result

先增加 invalid plan、actions/groups 分叉、nil-error 不完整结果、counter/effect/state commit 矛盾的
失败关闭测试，再让 runner dependency 只返回 `planner.ApplyPlan`。runner 必须先验证 plan，随后
从它取得 files/prune/groups/hooks；测试故障注入下沉到 phase dependency，而不是伪造与 plan
分叉的 actions。Result 采用私有状态与成功 seal，每个可执行 action 的 outcome 保存足以验证
尝试、物理提交、state effect 和 backup 的事实，聚合值由可信事实派生或受单一 validator 约束。
CLI 只通过 accessor 投影，不能重建 execution protocol。

同时将 `runWithOperations` 的 file、prune 和最终 state 阶段拆入同 package 的短函数/文件；拆分
必须保持 create-before-prune、conflict 门控 prune、部分成功仍提交 state、cleanup error 后收养、
Store failure 不回滚 target，以及确定性输出/退出码。

Concrete steps：

    在 repo root 运行：go test -race ./internal/apply ./internal/cli
    预期：全部既有恢复/幂等/CLI 测试与新增 protocol rejection 测试通过。

验收：

- runner 在任何 executor、backup、confirmation、prune 或 state mutation 前拒绝 invalid plan。
- 不存在 plan value 与实际执行 actions/groups/hooks 的并行事实源。
- zero/forged/incomplete Result 不能被 CLI 投影；当前 production 的所有部分成功事实仍可表达。
- CLI 不校验 executor/state 细节，只映射有效 result 到输出与退出码。

Commit 边界：

    refactor(apply): 密封计划执行与结果协议

### Milestone 3：一次 transition 合并 entry 与 run_once 效果

在 state package 内增加一个严格 transition 输入模型，原子应用 entry upsert/delete 和 run_once
upsert，复用现有 key、SHA-256 与 RFC3339 校验。缺失/loaded 基线、重复或冲突 update、空变化、
保留未涉及记录、返回值不共享 map 都必须测试。`TransitionEntries` 可保留为兼容薄 wrapper，现有
add/apply 改为或继续通过同一实现路径；本 Goal 不产生任何 run_once 写调用，也不改变 JSON。

同步 `docs/04-cli-spec.md` / `docs/05-apply-engine.md`，明确未来 hook 继承调用命令 stdin/stdout/
stderr，hook 自身输出属于外部透传 stream，可实时交错且不纳入 dot 的确定性摘要契约；dot 自身
context/action/verdict 顺序继续稳定。

Concrete steps：

    在 repo root 运行：go test -race ./internal/state ./internal/add ./internal/apply ./internal/cli
    预期：state transition、现有 mutation 和 CLI 行为全部通过，编码后的 state v1 结构不变。

验收：

- 一个 candidate Snapshot 能同时包含 entry 与 run_once 成功效果，但 runtime 仍只提交一次。
- invalid run_once key/hash/time、重复 key 与含糊输入 fail closed，零值 Snapshot 不泄漏。
- 规范不要求 buffer、截断、解析或重排任意 hook 输出，不改变现有命令摘要契约。

Commit 边界：

    refactor(state): 统一 apply 与 hook 状态转换

    docs(hooks): 明确子进程输出透传边界

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| CP6 add tests 不受 ambient DOT/GIT 环境影响 | hostile-env + real Git fixture 测试 | 待验证 |
| apply 只执行 valid canonical plan | apply seam/protocol 测试 | 待验证 |
| Result 完整、自验证且 CLI 只投影 | apply/CLI injected result 测试 | 待验证 |
| 既有 file/backup/prune/恢复/幂等不回归 | apply/add/CLI race tests | 待验证 |
| entries 与 run_once 可形成一个 strict Snapshot | state transition/codec/store 测试 | 待验证 |
| 公开行为与 state v1 不变，hook stdio 边界已明确 | docs diff + repository diff review | 待验证 |
| Go、依赖、构建、lint、race、manifest 全门禁 | `make check` | 待验证 |
| 未参与实现者完整复核 | read-only subagent review | 待验证 |

最终从 repo root 使用独立 `/private/tmp` cache/BINARY 运行：

    git diff <goal-base>...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp7-prerequisites/dot

远端 macOS/Linux CI、真实 Linux 和真实私人配置不属于本 Goal 的本地验收证据，不得声称已运行。

## Safety, Authorization, and Recovery

当前任务已授权本 Goal 范围内的代码、测试、规范和计划文件修改，以及创建
`codex/m1-cp7-prerequisites`、按本计划语义边界 stage/commit，并在门禁与独立复核通过后将同一
计划迁至 `completed/` 创建 closure commit；不含 merge、push、pull/fetch、rebase、amend、reset、
force、branch 删除、PR 或 release。授权来自 2026-07-22 用户消息“授权”，仅适用于本次 Goal。

所有测试 mutation 使用 `t.TempDir()` 或唯一 `/private/tmp` cache/BINARY；hostile env 的“外部”
对象仍位于另一个 synthetic 临时根，显式 sentinel 证明未改写。失败时保留当前语义 commit 之前的
已验证 checkpoint，不重写历史；新的问题用后续 fix commit 处理。真实私人数据路径仅核对 Git
状态，不展开内容。

## Interfaces and Dependencies

不新增依赖或持久化版本。跨 package 的必要契约只有：apply 从 planner 获得一个有效
`ApplyPlan`；state transition 接收严格的 entry/run_once 变化并返回一个有效 Snapshot；CLI 通过
apply Result accessor 读取已验证事实。具体 phase helper 与私有字段布局由实现反馈决定。

## Surprises & Discoveries

- Observation: CP6 fixture 使用生产代码能过滤的 Git 环境，但测试 helper 自身没有复用该边界。
  Evidence: `internal/add/preflight_test.go` 的 `runGit` 直接 append `os.Environ()`，hostile
  `GIT_CONFIG_GLOBAL` 合成验证会在 fixture 外创建文件。
  Impact: 先修测试证据边界，再把普通环境下的 `make check` 当成可靠验收。

- Observation: apply 的 fault-injection seam 允许用零 `ApplyPlan` 配独立 actions 测试 executor。
  Evidence: `internal/apply/run_test.go` 的 `runSeamFixture.operations(executionPlan)`。
  Impact: 测试结构本身固化了 production gate 的 split-brain，需要在重构协议时一起迁移。

## Decision Log

- Decision: 本 Goal 不实现 hook executor，但交付它依赖的 canonical plan/result 和统一 state
  transition。
  Rationale: 这些是现有安全协议债，可独立验证；subprocess observation、context 与 HookOutcome
  只有进入 CP7 真实执行流后才有足够反馈，提前实现会预建抽象。
  Date: 2026-07-22

- Decision: 保留现有 package 边界，只在 `internal/apply` 内按 phase 分文件。
  Rationale: 当前依赖图清晰无循环；问题是单文件与协议多真相源，不是 package 职责缺失。
  Date: 2026-07-22

- Decision: hook stdio 采用实时透传且排除在 dot 确定性摘要契约之外。
  Rationale: 支持 brew 等交互/长时任务，避免无界 buffer、截断和延迟；dot 自己的摘要仍保持稳定。
  Date: 2026-07-22

## Outcomes and Handoff

尚未实施。正式 Git 交付授权已获得，分支已创建；从 Milestone 1 串行执行。
