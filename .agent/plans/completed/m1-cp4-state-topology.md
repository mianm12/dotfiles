# fix/m1-apply-core-acceptance：前置验证 file-stage state 拓扑

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

修复 CP4 收尾复核发现的 candidate state 拓扑提交过晚问题。完成后，planner 会在任何
executor 调用前证明：本轮可能成功 upsert 的 file action 与 file stage 期间仍会保留的历史
state key 不会解析到同一 target，也不互为祖先。validator 按执行顺序模拟每个成功 upsert 及其
`PreviousKey` 删除，因此任意部分成功前缀都能形成可提交、下一次仍可严格加载的 state；runtime
的 `CommitState` 校验继续作为最终 freshness/TOCTOU 门禁。

此外，每个 target-mutation action 都会相对磁盘上尚未发布变化的原始 baseline state 做恢复门禁：
任何旧 key 的解析轨迹都不得穿过将被改成非目录对象的 target leaf，即使旧/新 key 的最终 leaf
identity 相等。这样 Store 失败或提交前崩溃后，旧 state 仍能 strict load 并进入 L2/S1b 恢复。

同时补齐 runner 对未知 `StateEffect`、failure effect 无 error 两个协议分支的显式回归，并校正
`ApplyPlan` 注释，使内部契约的唯一入口和复制边界没有歧义。

## Scope / Non-goals

范围内：

- 在 `validateApplyPlan` 的组合 gate 中保留 matched historical key 自身的
  `TargetResolution`，从完整 baseline entries 按 action 顺序验证每个成功 state 前缀。
- 对每个 target mutation 额外验证原始 persisted baseline 的 traversal trace；state-only migration
  不因该恢复门禁误拒，target mutation 的自遍历路径 fail closed。
- 覆盖新 target 为历史 orphan 的祖先、历史 orphan 为新 target 的祖先，以及 no-prune、deferred
  prune、partial scope、matched alias preserve 与 late `PreviousKey` migration 下的零 mutation 拒绝。
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
state 也已证明内部安全。file stage 缺失的是 transition 证明：baseline 包含全部 orphan，也包含按
identity 匹配 desired、但会因 conflict/skip/partial scope 或 later action 尚未成功而暂时保留的历史
key；每个成功 upsert 还可能用 `PreviousKey` 原子迁移 alias key。prune 位于 file stage 之后且可能
失败或转为 deferred，不能预先从任一成功前缀扣除历史 entry。

runner 在全部 file actions 后才一次 Store，所以 candidate prefix 的逻辑 `PreviousKey` 删除不等于
磁盘旧 key 已消失。target mutation 与 state publish 之间的 crash/Store-failure 窗口必须始终以
原始 baseline 为准；若旧 key 的 traversal trace 经过 mutation target，后者改成 link/scaffold 的
非目录产物后会让旧 state 无法加载。

## Progress

- [x] 2026-07-20：确认 main、branch、origin/main、upstream 与 worktree 状态；branch 等于 clean
  current main，没有 DAG 外提交或任务外改动。
- [x] 2026-07-20：复核规范、当前 planner/runtime/state 数据流、相关 tests 与 completed ExecPlans；
  确认问题是实现组合校验缺口，不是产品设计冲突。
- [x] 2026-07-20：以失败回归固定 upsert × orphan 拓扑缺口；实现 planner 前置 gate，并覆盖
  link L1、scaffold S3、state-only S1b、no-prune、deferred 与 partial scope。
- [x] 2026-07-20：补齐 runner unknown effect / failure-without-error 协议回归，并校正 planner
  Valid 构造入口与 `Desired.Content` 复制注释。
- [x] 2026-07-20：完成五包窄测、race、重复回归、Linux/amd64 交叉编译、branch diff check 与
  `make check`（首轮 review 前 HEAD `80446e2`）。
- [x] 2026-07-20：独立完整复核 round 1 中两路 GO；数据保护 reviewer 发现并证明有效 P1：
  orphan-only gate 漏掉 matched alias historical key。旧实现再次在真实 Run 越过 target commit 后
  才由 `CommitState` 拒绝；已接受 finding 并实现完整 baseline-prefix 模型。
- [x] 2026-07-20：对 P1 fix 重跑窄测、race、重复回归、Linux 交叉编译、diff check 与 `make check`。
- [x] 2026-07-20：独立完整复核 round 2 中两路 GO；规范主线 reviewer 发现有效 P1：逻辑上先删
  `PreviousKey` 漏掉 target mutation→state publish 的崩溃窗口。真实 L3 + Store publish failure
  证明旧实现会改链并留下随后无法 strict load 的旧 alias key。
- [x] 2026-07-20：新增 `TargetResolution.Traverses`，分离 immutable persisted-baseline 恢复门禁与
  mutable candidate-prefix 拓扑门禁；state-only/safe alias 正例和 self-traversal 反例窄测通过。
- [x] 2026-07-20：对 round 2 P1 fix 重跑 paths+五包窄测、race、重复回归、Linux/amd64
  交叉编译、diff check 与 `make check`。
- [x] 2026-07-20：最终 round 3 完整复审由三位未参与实现的 reviewer 分别检查规范/数据流、
  数据保护/恢复和 Go/测试/双平台，全部给出 GO，未发现 P0–P3 actionable finding。
- [x] 2026-07-20：确认 current main 仍精确等于 effective base `9ba6964`，没有 DAG 外提交或
  任务外改动；完成独立复核、freshness gate 与 plan closure。
- [x] 2026-07-20：将 clean、可 fast-forward 的 acceptance-fix branch 交还 coordinator；本计划
  收口后由主 agent 在 main checkout 执行 FF-only 集成并重新验收整个 CP4。

## Milestones

### Milestone 1：证明所有 file-stage candidate state 前缀拓扑安全

先以 planner 组合测试和真实 `apply.Run` 隔离 fixture 复现：baseline strict state 仅含仍可加载的
descendant orphan，新 L1 在其祖先创建 symlink；旧实现会先修改 target，再在 `CommitState` 拒绝
candidate state，留下无法通过下一次 strict load 的组合。测试必须先在旧实现失败，并断言修复后
planner 在 executor 前返回 `ErrTargetOverlap`，target/state bytes 均不变，重复运行仍停在同一
plan gate。

实现一个私有、纯内存的组合 validator：先从完整 observation 重建所有 baseline state key 的
plan-time resolution，其中 matched entry 必须保留历史 key 自身的 ancestor 轨迹；再按 file action
顺序只模拟 `StateUpsert`，先删除同一步的 `PreviousKey`/被替换 key，再把新 resolution 与当前全部
retained entries 做 `Equal` / 双向 `IsAncestorOf` 比较。每次成功后更新模型，故 action 失败前的每个
可提交前缀都已验证；plan-only preserve 不改集合，candidate 层的 early migration 可以解除 alias
冲突，later migration 不能倒推修复不安全前缀。

candidate 模型之外保留 immutable baseline resolution map。每个 `FileTargetMutation` 在应用逻辑
state effect 前，检查其 target leaf 是否被任何 persisted baseline key 的完整 traversal trace 经过；
该检查不能用 strict `IsAncestorOf`，因为 historical alias 与 desired key 的 leaf identity 可以相等，
同时历史解析曾在中途经过该 leaf。`TargetResolution.Traverses` 只暴露这个既有 trace 事实；
state-only action 不执行该门禁，普通不穿 target leaf 的 alias migration 继续允许。

Concrete steps：

    go test ./internal/planner -run 'TestValidateFileStateTopology'
    go test ./internal/apply -run 'TestRun_Rejects.*(CandidateStateTopology|PersistedState)BeforeExecutor'

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
| 每个 upsert prefix 与当时 retained state 不同 identity、互不为祖先 | planner relation/prefix matrix | 窄测通过 |
| no-prune/deferred/partial 均在 executor 前拒绝危险组合 | planner + apply integration | 窄测通过 |
| 拒绝时 target/state 零 mutation，重复运行可诊断 | real isolated `apply.Run` regression | 窄测通过 |
| matched alias preserve/partial/late migration 不漏检 | planner prefix + real Run regressions | 窄测通过 |
| preserve action 不被误判；early migration/refresh 仍合法 | planner control regressions | 窄测通过 |
| target mutation 后旧 persisted state 仍可 strict load | traversal matrix + L3 Store-failure regression | 窄测通过 |
| unknown/failure-without-error 协议分支 fail closed | runner seam table | 通过 |
| CP4 既有 L/S、Precond、恢复与幂等不回退 | paths+五包窄测、race、完整 `make check` | 通过 |

## Safety, Authorization, and Recovery

当前用户明确要求按 review 建议修复；上层 CP4 Goal 已授权复用 acceptance-fix branch、
`/private/tmp` clean worktree、范围内 stage/commit、plan lifecycle、freshness merge 和本地
fast-forward-only main 集成。测试只使用 `t.TempDir()` 与合成 HOME/repo/config/state，不读取或
修改真实 `modules/`、机器配置、state、backup、`.env` 或主力 HOME。

失败时保留最近成功 commit，以新 fix commit 继续；不 amend、rebase、cherry-pick、squash、reset、
force、fetch、pull、push 或删除 branch。若必须改变规范、ownership、state 格式、不可逆边界或
扩大 CP4 范围，立即更新本计划并停止请求裁决。

## Interfaces and Dependencies

不新增依赖或仓库外 API。planner 保持 canonical action/profile 组合不变量的唯一归属；runtime
继续按计划执行并在 `CommitState` 做最终 state/path freshness 校验；state 继续拥有 v1 语义和原子
Store。`internal/paths.TargetResolution.Traverses` 暴露 resolver 已保存的 ancestor trace，不重新
解析 filesystem；planner 私有 helper 分别验证 persisted-baseline 恢复与 candidate prefix，不复制
ownership、codec、Store 或 prune 决策。

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

- Observation: identity join 会把 matched historical entry 移出 orphan，但 desired resolution 只
  保存 desired 展示路径的 ancestor 轨迹；alias historical key 可能具有不同轨迹，并在对应 action
  preserve、scope 外或 later migration 尚未成功时继续留在 candidate。
  Evidence: round 1 reviewer 构造 `~/alias -> ~/real`、state `~/alias/child`、desired
  `~/alias` + `~/real/child`；真实 test-first Run 先创建无关 `~/00`，最终才在 state Store 前以
  `ErrPathValidation` 拒绝 alias parent/child candidate。
  Impact: `ObservedTarget` 必须保留 matched historical resolution；validator 必须从完整 baseline
  逐步模拟成功 upsert/`PreviousKey`，不能把 orphan 当成全部 retained state。

- Observation: runner 在全部 file actions 结束后才一次发布 state；逻辑 candidate 已删除
  `PreviousKey` 时，磁盘仍保留原始 key。historical alias 的 leaf 可以与 mutation target 相等，同时
  traversal trace 曾经过该 target；strict `IsAncestorOf` 会因 equal leaf 有意返回 false。
  Evidence: round 2 reviewer 的 `bridge -> real/`、`detour -> bridge/..` fixture；state key
  `~/detour/bridge` 与 desired `~/bridge` identity 相等，但 L3 relink 后注入 Store publish failure，
  第二次 strict load 被非目录 `bridge` 阻断。
  Impact: candidate prefix 与 crash recovery 是两个不变量；每个 target mutation 必须对 immutable
  persisted baseline 使用允许 equal leaf 的 traversal predicate，state-only migration 无此风险。

## Decision Log

- Decision: 最初在 canonical combined `ApplyPlan` 校验中建立 upsert × orphan gate，而不是在
  runner 预演 state 或放宽 `CommitState`；该集合选择已被 round 1 P1 supersede。
  Rationale: planner 已同时拥有完整 observation 与 canonical file actions；这里是最早且唯一能在
  零 mutation 下证明所有部分成功前缀安全的位置。
  Date: 2026-07-20

- Decision: 最初只比较 `StateUpsert` action 与 orphan；round 1 证明“matched historical state 不是
  orphan”不等于“它已从 candidate 删除”，因此该决定已 supersede。
  Rationale: 全部 orphan 仍必须保留，但它们只是 baseline retained entries 的子集。
  Date: 2026-07-20

- Decision: 保留 matched historical key 自身的 `HistoricalResolution`，并在 planner 内按执行顺序
  验证完整 baseline state 的每个全成功前缀。
  Rationale: historical alias 与 desired 可 leaf identity 相同但 ancestor 轨迹不同；逐步应用
  `PreviousKey` 才与 `state.TransitionEntries` 和 runner 停在首个错误的语义一致，同时无需引入
  candidate Snapshot API、重新解析 filesystem 或复制 state 持久化规则。
  Date: 2026-07-20

- Decision: 以 `TargetResolution.Traverses` 表达“解析一个 key 是否曾经过另一 target leaf”，并让
  target mutation 对原始 baseline 做独立恢复门禁。
  Rationale: `IsAncestorOf` 的 strict/equal 语义对 target-set 正确，不能为 crash recovery 放宽；新增
  predicate 复用同一 resolver trace，既覆盖 equal-leaf detour，也避免把 state-only alias migration
  当成物理破坏。
  Date: 2026-07-20

- Decision: 先完成 canonical prune 与 active target-delete topology 校验，再执行 file-upsert state
  topology gate。
  Rationale: 两者都在 executor 前 fail closed；该顺序保留更具体的破坏性 prune 诊断，同时不把
  active state-only prune 当作 file-stage candidate 安全性的前提。
  Date: 2026-07-20

## Outcomes and Handoff

已完成。实质 commits：

- `a16a1bd`：在 canonical combined plan 中前置验证 file upsert × orphan 拓扑，并加入 planner/
  real runner 回归。
- `9e96b95`：补齐 unknown effect 与 failure-without-error 执行协议证据。
- `801b2ad`：校正 `ApplyPlan` Valid 构造入口和 getter 复制边界注释。
- `55db7b9`：保留 matched historical resolution，并按真实 action 顺序验证完整 candidate state
  成功前缀。
- `e511ac0`：分离 immutable persisted baseline 与 mutable candidate prefix，前置保护 target
  mutation 到原子 state publish 之间的恢复窗口。

本地 `internal/paths` 与 planner/apply/executor/runtime/state 窄测、同集合 race、关键回归
`-count=10`、Linux/amd64 交叉编译、`git diff 9ba6964...e511ac0 --check` 与精确 HEAD 的
`make check BINARY=/private/tmp/dot-m1-cp4-state-topology-r3-final/dot` 均通过。round 1 的
orphan-only 模型和 round 2 的逻辑 candidate-only 恢复模型各产生一项有效 P1，均以独立 fix
commit 修复；round 3 三路完整复审全部 GO，没有 unresolved blocking finding。

worktree 在收口时 clean，branch 与 current main 的 merge-base 仍为 effective base `9ba6964`，可由
主 agent fast-forward-only 合入。整个 `checkpoint_base...main` 的三路 Acceptance 与最终 main
门禁属于 coordinator 后续步骤，尚未在本计划内声称完成。精确 HEAD 的远端 macOS/Linux CI 未
运行；本地 Linux 证据仅为交叉编译。
