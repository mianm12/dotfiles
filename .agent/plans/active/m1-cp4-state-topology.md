# fix/m1-apply-core-acceptance：前置验证 file-stage state 拓扑

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

修复 CP4 收尾复核发现的 candidate state 拓扑提交过晚问题。完成后，planner 会在任何
executor 调用前证明：本轮可能成功 upsert 的 file action 与 file stage 期间仍会保留的历史
orphan 不会解析到同一 target，也不互为祖先。因此任意部分成功前缀都能形成可提交、下一次仍
可严格加载的 state；runtime 的 `CommitState` 校验继续作为最终 freshness/TOCTOU 门禁。

同时补齐 runner 对未知 `StateEffect`、failure effect 无 error 两个协议分支的显式回归，并校正
`ApplyPlan` 注释，使内部契约的唯一入口和复制边界没有歧义。

## Scope / Non-goals

范围内：

- 在 `validateApplyPlan` 的组合 gate 中校验所有 `StateUpsert` file action 与全部 observed orphan
  的 canonical `TargetResolution` 关系。
- 覆盖新 target 为历史 orphan 的祖先、历史 orphan 为新 target 的祖先，以及 no-prune、deferred
  prune、partial scope 下的零 mutation 拒绝。
- 保留 same-key metadata refresh、`PreviousKey` alias migration、kind migration 和无关 orphan
  的合法路径。
- 补齐 `validateFileResult` 已实现但缺少独立证据的协议组合；修正 `ApplyPlan` getter/构造注释。

明确不做：

- 不改变 state v1、ownership、L/S 决策表、prune 顺序、原子持久化边界、公开 CLI/输出/退出码。
- 不在 file stage 前自动删除 orphan，不放宽 state validation，不把 late commit error 当作正常恢复。
- 不引入 candidate-state preview API、filesystem abstraction、第三方依赖或 M2/M3 能力。
- 不为恶意仓库、恶意 hook 或主动并发篡改扩展威胁模型。

## Contract and Context

- `docs/02-architecture.md` §4–§6：pipeline validate 失败必须在 executor 前整体拒绝；部分成功结果
  必须可提交，executor 只消费完整可信 plan。
- `docs/05-apply-engine.md` §2/§3.3/§5–§7/§10：历史 orphan 可因 no-prune、scope 或 deferred prune
  暂存；state 语义失败 fail closed；成功 file effect 必须原子落账并支持重跑恢复。
- `docs/08-testing.md`：覆盖 Precond、零写入、部分成功、恢复与第二次运行零 mutation。
- `docs/09-roadmap.md`：CP4 只交付 link/scaffold 安全执行内核，active prune execute 留待后续。

基线为 clean `main@9ba69649109d76338166ad4000a8af1e2b97f633`，`origin/main@e9e8bac`，
本地 main ahead 39；未 fetch/pull，`upstream/main` 不存在。既有
`fix/m1-apply-core-acceptance` 精确指向基线且由 CP4 历史 commits 证明归属，本次复用该 branch，
worktree 为 `/private/tmp/dot-m1-cp4-state-topology`。

当前完整 desired 已在 manifest/path boundary 层证明 pairwise 拓扑安全，strict-loaded baseline
state 也已证明内部安全；缺失的唯一 cross-product 是“本轮会 upsert 的 target × observed orphan”。
orphan 即使已有 active prune 计划，也要到 file stage 之后才可能删除，且 prune 还可能失败或转为
deferred，所以前置证明必须保守地纳入全部 orphan。

## Progress

- [x] 2026-07-20：确认 main、branch、origin/main、upstream 与 worktree 状态；branch 等于 clean
  current main，没有 DAG 外提交或任务外改动。
- [x] 2026-07-20：复核规范、当前 planner/runtime/state 数据流、相关 tests 与 completed ExecPlans；
  确认问题是实现组合校验缺口，不是产品设计冲突。
- [x] 2026-07-20：以失败回归固定 upsert × orphan 拓扑缺口；实现 planner 前置 gate，并覆盖
  link L1、scaffold S3、state-only S1b、no-prune、deferred 与 partial scope。
- [ ] 2026-07-20：补齐 runner 结果协议回归并校正 planner 注释。
- [ ] 2026-07-20：完成窄测、race、diff check、`make check`、独立完整复核与 plan closure。
- [ ] 2026-07-20：freshness gate 后 fast-forward-only 合入 main，并重新验收整个 CP4。

## Milestones

### Milestone 1：证明所有 file-stage candidate state 前缀拓扑安全

先以 planner 组合测试和真实 `apply.Run` 隔离 fixture 复现：baseline strict state 仅含仍可加载的
descendant orphan，新 L1 在其祖先创建 symlink；旧实现会先修改 target，再在 `CommitState` 拒绝
candidate state，留下无法通过下一次 strict load 的组合。测试必须先在旧实现失败，并断言修复后
planner 在 executor 前返回 `ErrTargetOverlap`，target/state bytes 均不变，重复运行仍停在同一
plan gate。

实现一个私有、纯内存的组合 validator，只遍历 `OnSuccess.Kind == StateUpsert` 的 file action，并
使用 action `Precondition.TargetResolution` 与全部 orphan `Resolution` 的 `Equal` / `IsAncestorOf`
关系。`StatePreserve` 的 skip/conflict 不建立新账，不应误拒；matched state 已从 orphan 集合移除，
same-key refresh、alias `PreviousKey` 和 kind migration 继续合法。

Concrete steps：

    go test ./internal/planner -run 'TestValidateFileUpsertStateTopology'
    go test ./internal/apply -run 'TestRun_RejectsUnsafeCandidateStateTopologyBeforeExecutor'

Commit 边界：

    fix(planner): 前置验证 file state 拓扑

### Milestone 2：闭合执行协议证据与注释契约

把现有 result-protocol table 的二值 `successEffect` 改为 test-private 三态选择，新增 unknown effect
和 failure effect without error；只验证当前生产行为，不新增 production helper。校正 `ApplyPlan`
零值注释，明确 `PlanApply` 与 `PlanLoadedApply` 都能形成 Valid plan；getter 注释只声称实际复制的
slice 与 `Desired.Content`。

Concrete steps：

    go test ./internal/apply -run 'TestRun_RejectsExecutionResultsThatContradictActionClass'
    go test ./internal/planner

Commit 边界：

    test(apply): 闭合 file result 协议回归
    docs(planner): 校正 apply plan 注释

### Milestone 3：完整复核、freshness 与 Checkpoint 重验收

由未参与实现的 reviewer 从 `9ba6964...HEAD` 完整检查规范、数据保护与 Go/双平台；有效 finding
以新 fix commit 修复并复审，不重写历史。通过后迁移本计划并以纯计划 commit 收口，freshness
仍等于 effective base 才 fast-forward-only 合入 main。随后三路重新审查
`e9e8bac...main` 的整个 CP4，并运行完整本地门禁；若产生 checkpoint-level finding，继续复用
acceptance-fix branch 按同一流程修复。

Concrete steps：

    go test ./internal/planner ./internal/apply ./internal/executor ./internal/runtime ./internal/state
    go test -race ./internal/planner ./internal/apply ./internal/executor ./internal/runtime ./internal/state
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-m1-cp4-state-topology-linux.test ./internal/apply
    git diff 9ba69649109d76338166ad4000a8af1e2b97f633...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp4-state-topology-check/dot

验收：全部命令退出 0、worktree clean、无 unresolved blocking finding；精确 HEAD 的远端
macOS/Linux CI 未运行时只报告“本地验收通过、远端待验收”。

## Validation and Acceptance

| 必须成立的性质 | 主要证据 | 状态 |
|---|---|---|
| upsert 与任一保留 orphan 不同 identity、互不为祖先 | planner relation matrix | 窄测通过 |
| no-prune/deferred/partial 均在 executor 前拒绝危险组合 | planner + apply integration | 窄测通过 |
| 拒绝时 target/state 零 mutation，重复运行可诊断 | real isolated `apply.Run` regression | 窄测通过 |
| preserve action 不被误判；refresh/migration 仍合法 | planner control regressions | 待验证 |
| unknown/failure-without-error 协议分支 fail closed | runner seam table | 待验证 |
| CP4 既有 L/S、Precond、恢复与幂等不回退 | 五包窄测、race、完整 `make check` | 待验证 |

## Safety, Authorization, and Recovery

当前用户明确要求按 review 建议修复；上层 CP4 Goal 已授权复用 acceptance-fix branch、
`/private/tmp` clean worktree、范围内 stage/commit、plan lifecycle、freshness merge 和本地
fast-forward-only main 集成。测试只使用 `t.TempDir()` 与合成 HOME/repo/config/state，不读取或
修改真实 `modules/`、机器配置、state、backup、`.env` 或主力 HOME。

失败时保留最近成功 commit，以新 fix commit 继续；不 amend、rebase、cherry-pick、squash、reset、
force、fetch、pull、push 或删除 branch。若必须改变规范、ownership、state 格式、不可逆边界或
扩大 CP4 范围，立即更新本计划并停止请求裁决。

## Interfaces and Dependencies

不新增依赖或公开 API。planner 保持 canonical action/profile 组合不变量的唯一归属；runtime
继续按计划执行并在 `CommitState` 做最终 state/path freshness 校验；state 继续拥有 v1 语义和原子
Store。新增 helper 为 `internal/planner` 私有纯函数，不重新解析 filesystem，不复制 ownership 或
prune 决策。

## Surprises & Discoveries

- Observation: `CommitState` 会在 target 已越过提交点后才对 candidate state 重新做路径语义校验。
  Evidence: `apply.Run` 的 execute→TransitionEntries→CommitState 顺序与 runtime strict validator。
  Impact: late gate 仍应保留，但不能替代 planner 的零 mutation 组合证明。

- Observation: active prune 也不能从 file-stage candidate 中预先扣除 orphan。
  Evidence: pipeline 明确 file execute 在 prune 前，且任一 file error/Precond mismatch 会把 prune
  转为 deferred。
  Impact: validator 对全部 observed orphan 一视同仁，不依赖 `NoPrune` 或 prune action mode。

- Observation: 旧实现的真实 `Run` 回归先成功创建 `~/parent` symlink，随后才以
  `state.ErrPathValidation` 拒绝 `~/parent/child` candidate；`TargetCommits == 1` 且旧 state 未落账。
  Evidence: test-first 执行
  `go test ./internal/apply -run TestRun_RejectsUnsafeCandidateStateTopologyBeforeExecutor` 的失败输出。
  Impact: 该问题会把下一次 strict load 也阻断，证明不能只依赖 `CommitState` late gate。

- Observation: 既有测试曾把 P1/P3 state-only prune 视为可绕过 ancestor topology 的安全例外。
  Evidence: 完整 planner 回归中，orphan parent + child upsert 的 P1/P3/no-prune 三项在新增 gate 后
  按旧预期失败；同一 fixture 的 child conflict 因 `StatePreserve` 不产生 upsert，仍保持合法。
  Impact: 测试已按 file-stage 部分成功不变量收紧；active target-delete 的既有诊断仍优先于 candidate
  state 诊断。

## Decision Log

- Decision: 在 canonical combined `ApplyPlan` 校验中建立 upsert × orphan gate，而不是在 runner
  预演 state 或放宽 `CommitState`。
  Rationale: planner 已同时拥有完整 observation 与 canonical file actions；这里是最早且唯一能在
  零 mutation 下证明所有部分成功前缀安全的位置。
  Date: 2026-07-20

- Decision: 只比较 `StateUpsert` action 与 orphan，且所有 orphan 都按 file stage 保留处理。
  Rationale: preserve action 不改变 entry 集；matched historical state 不是 orphan；prune 尚未执行且
  可能失败，不能作为 file commit 安全性的前提。
  Date: 2026-07-20

- Decision: 先完成 canonical prune 与 active target-delete topology 校验，再执行 file-upsert state
  topology gate。
  Rationale: 两者都在 executor 前 fail closed；该顺序保留更具体的破坏性 prune 诊断，同时不把
  active state-only prune 当作 file-stage candidate 安全性的前提。
  Date: 2026-07-20

## Outcomes and Handoff

进行中。完成时记录 commits、review rounds、本地验证、未验证平台证据与 main 集成结果。
