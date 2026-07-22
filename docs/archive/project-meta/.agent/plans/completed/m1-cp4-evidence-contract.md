# fix/m1-apply-core-acceptance：收紧 CP4 观测证据与执行职责

> [!WARNING]
> 历史工程记录，非当前规范或工作流程。

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

修复 CP4 review 发现的过度观测和执行职责漂移。完成后，link/scaffold planner 只读取当前
决策与最终前提实际需要的 leaf 证据；partial plan 不因未请求 target 的普通文件内容不可读而
失败，S1 scaffold 的 state-only adopt 也不因无关内容变化被拒绝。同时 runner 会在任何 target
mutation 前通过 executor 的唯一能力检查完成全计划 preflight，`skip/conflict` 始终只是计划
事实，执行结果计数使用无歧义语义。

本计划不改变产品决策表、ownership、state v1、公开 CLI 输出或退出码，只修正内部事实模型和
执行契约，使代码更准确地落实既有规范。

## Scope / Non-goals

范围内：

- 分离完整 profile 的 identity/topology/basic leaf classification 与 scope 内 regular payload/digest
  evidence。
- 让 regular digest 按实际动作需求读取，不在通用 Observation 中保存或复制 raw target bytes。
- 让 Precondition 表达 missing、present、精确 symlink、非 owned symlink 与精确 regular 等参与
  决策的 leaf 谓词；identity、ancestor、control-plane 和 source 前提保持原有强度。
- 统一 file action 的 plan-only、state-only、target-mutation 分类；executor 提供单一、纯只读的
  当前能力检查，runner 在执行前检查全部 file actions。
- 删除 scaffold executor 的 skip 死分支，并澄清 runner 结果计数语义。

明确不做：

- 不实现 backup、force execution、prune execution、hooks、真实 `dot apply`、managed/rendered、
  add/init 或 M2/M3 能力。
- 不改变 target identity、所有权谓词、决策表、state 持久化格式或原子提交边界。
- 不删除 CP4 acceptance 已建立的 session-local Store publish 故障注入接缝。
- 不拆分 `StateEffect.Entry`/`HistoricalState`；该非阻塞模型债务留给后续共享 contract 节点。
- 不引入第三方依赖、通用 filesystem interface、全局 fault hook 或静默 fallback。

## Contract and Context

- `docs/02-architecture.md` §4–§6：partial 只缩小动作范围，但完整 profile 仍做全局 identity/path
  validation；计划与执行通过自包含动作通信。
- `docs/05-apply-engine.md` §3.2/§5–§7：S1a/S1b 只依赖 target 是否存在；L1–L6、迁移和 prune
  ownership 使用各自必要证据；提交时只要求参与决策的证据仍成立。
- `docs/08-testing.md` §3：必须覆盖决策表、partial 全局校验、Precond、失败安全和第二次运行零
  mutation。
- `docs/09-roadmap.md` §3：CP4 完成 link/scaffold 安全提交内核，backup/prune 在后续加入，因此
  当前修复不能预先执行这些能力。

基线为 `fix/m1-apply-core-acceptance@25eacdda94a475183a9c264eedf2041b9a958f6e`，该提交以明确
integration merge 同步 clean `main@3b02453abba3040951a4b260d7e7f50de6fac75e`。当前
`ObserveProfileTargets` 在 scope 选择前对完整 desired/orphan 调用 `ObserveTarget`；后者对所有
regular leaf 无条件 `ReadFile` 并把 bytes/hash 放入共享 `Observation`。executor 随后又以完整
结构体相等复核 Precondition。当前 L/S 决策不消费 regular raw bytes，因而这些读取和相等要求
不是安全证明的一部分，反而让不相关 IO 和内容变化阻塞合法计划。

`internal/apply.validateExecutionScope` 与 executor 的 action validator 各自维护一套当前能力规则；
runtime 已明确不把 skip/conflict 传给 executor，但 scaffold executor 仍保留旧 skip 分支。

## Progress

- [x] 2026-07-20：确认 main clean，existing acceptance-fix branch 可证明属于 CP4，并以
  `25eacdd` 非重写同步当前 main；worktree 与 Git top-level 均为分配的绝对路径。
- [x] 2026-07-20：以失败回归证明 complete-profile/partial、non-force L6、force digest、S1b 和
  ownership-release 缺口；完成按需 digest、语义 LeafCondition 与 executor Precondition 修复。
- [x] 2026-07-20：统一 execution class 与 executor capability preflight，删除 skip 执行死分支，
  并将 Result 命名为 attempts、target commits、adoption effects 和 state publish 四个边界。
- [x] 2026-07-20：三轮独立完整复核闭合全部 P2/P3 finding；窄测、重复回归、race、完整 diff
  check、`make check` 和 Linux/amd64 交叉构建通过，计划已具备迁移和收口条件。

## Milestones

### Milestone 1：按动作获取 leaf 证据并复核语义前提

先补充真实文件系统回归：partial 未请求 regular target 内容不可读不阻塞请求模块；scope 内
S1b unreadable regular 仍形成 state-only adopt；非 force L6 unreadable regular 形成 conflict；
S1b 计划后 regular 内容变化但仍存在时允许 adopt，变为 missing 时拒绝；非 owned symlink 在
release ownership 前恢复为 owned 时仍拒绝。随后将完整 identity join/basic leaf classification
与 scope regular digest acquisition 分层，移除通用 Observation raw bytes，并让 executor 按
action 所需 leaf predicate 复核。

完整 profile 的 target identity、ancestor、collision 和 control-plane validation 继续在 scope 前
执行；regular digest 只为明确需要精确 regular 快照的动作读取。当前 CP4 没有可执行 regular
替换，因此不得用缺失 digest 近似允许未来 backup。

Concrete steps：

    在 repo root 运行：go test ./internal/planner ./internal/executor -run 'TestPlanApply_ReadsRegularDigestOnlyWhenRequired|Test.*(Precondition|Partial)'
    预期：新增测试在旧实现失败，完成实现后退出 0。

验收：

- partial scope 仍拒绝完整 profile 的 identity/path 冲突，但不读取未请求 leaf 的 regular payload。
- S1b 只要求相同 target identity 下 leaf 仍存在；L1/S3 仍要求 missing；owned migration 仍要求精确
  raw symlink 证据。
- 任何需要 digest 的动作缺少完整证据时 fail closed；没有吞错或未知证据 fallback。

Commit 边界：

    fix(planner): 按动作收集并复核 target 证据

### Milestone 2：统一可执行动作职责和结果语义

为 canonical file action 提供封闭 execution class。runner 以此跳过 plan-only facts，并在任何
executor 调用前使用 executor 的同一 validator 检查所有可执行动作；`ExecuteFile` 复用该检查。
删除 scaffold skip 执行与相应直接 executor 断言，把 skip 行为留在 planner/apply 层。结果字段
按真实边界命名：尝试调用、target commit point、adopt effect 与 state publish 不再混用。

Concrete steps：

    在 repo root 运行：go test ./internal/apply ./internal/executor ./internal/planner
    预期：unsupported 后项在首项 mutation 前整体拒绝；link/scaffold executor 对 plan-only action 一致拒绝。

验收：

- 当前 file 能力支持矩阵只有 executor 一处真相源，runner 不复制 verb/reason 组合。
- skip/conflict 永不进入 executor；adopt 仍复核 Precondition 并只产生 state effect。
- Result 字段从名称即可区分 executor attempts、target commits、adopt effects 与 state publish。

Commit 边界：

    refactor(apply): 统一 file action 执行契约

### Milestone 3：完整复核与收口

从 `25eacdd...HEAD` 检查全部实质 diff，重点复核 scope、identity、ownership、Precondition、零
mutation、部分成功 state 和 macOS/Linux 文件系统语义。有效 finding 使用新 fix commit，不重写
历史；完整门禁和独立复核通过后更新本计划并迁移到 completed。

Concrete steps：

    go test ./internal/planner ./internal/executor ./internal/apply ./internal/runtime ./internal/state
    go test -race ./internal/planner ./internal/executor ./internal/apply ./internal/runtime ./internal/state
    git diff 25eacdda94a475183a9c264eedf2041b9a958f6e...HEAD --check
    make check

验收：全部命令退出 0，worktree clean；远端 macOS/Linux CI 未实际运行时明确标为待验收。

Commit 边界：

    docs(apply): 收口 CP4 证据契约修复计划

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| 完整 identity/path validation 不因 partial 削弱 | planner complete-profile collision/ancestor regressions | 通过 |
| 非 scope regular payload 不被读取 | `TestPlanApply_ReadsRegularDigestOnlyWhenRequired` | 通过 |
| S1b/L6 只依赖规范要求的 leaf 证据 | planner + executor filesystem regressions | 通过 |
| owned migration 和 target mutation Precond 不降级 | executor symlink/identity/ancestor regressions | 通过 |
| unsupported/malformed plan 在首个 mutation 前拒绝 | apply preflight + incomplete upsert regressions | 通过 |
| execution class、commit point、effect 和 error 组合闭合 | apply result-protocol + post-commit regressions | 通过 |
| skip/conflict 只有一个职责归属 | planner/apply/executor contract tests | 通过 |
| 既有 L/S、恢复和幂等不回退 | 五包窄测、全仓 race、完整 `make check` | 通过 |

## Safety, Authorization, and Recovery

当前用户明确要求修复 review 建议；上层 CP4 Goal 已授权使用
`fix/m1-apply-core-acceptance`、在 `/private/tmp` 建立 clean worktree、创建语义 commits、执行
freshness merge 并在门禁/复核后 fast-forward-only 集成本地 main。所有测试使用 `t.TempDir()`
和合成 HOME/repo/config/state；不读取或修改真实 `modules/`、真实 HOME、机器配置、state、backup、
`.env` 或私人数据。

失败时保留最近成功 commit，用新 fix commit 继续；不 amend、rebase、cherry-pick、squash、reset、
force、fetch、pull、push 或删除 branch。若发现必须改变规范、ownership、持久化格式或接受风险，
立即更新 Progress 并停止请求裁决。

## Interfaces and Dependencies

不新增依赖。planner 继续拥有 canonical action、leaf evidence 与 Precondition 语义；executor 拥有
当前 file action 执行能力和无 IO validator；apply runner 只负责编排 plan-only/state-only/target
mutation、部分成功 effects 与一次 state publish。state v1 与现有 Store/runtime 接口不变。

## Surprises & Discoveries

- 完整 profile 的 regular raw bytes 从未参与 identity/path validation 或非 force L/S 决策；安全边界
  需要保留 basic leaf classification，但 payload digest 可以严格按 scope 与 force replacement 请求。
- 第一轮独立复核发现 link state upsert validator 比 scaffold 少校验 entry key/module/source，导致
  malformed 后项可能绕过全计划 preflight；修复后两类 action 复用 `validateFileUpsert`。
- execution class 不能只用于跳过 plan-only action；runner 还必须交叉验证 target commit、state
  effect 与 error。尤其 state-only 没有 post-commit 边界，`OnSuccess + error` 必须拒绝；target
  mutation 已提交后的 cleanup error 则必须保留成功 effect 并提交部分成功 state。
- 初始 ExecPlan 的 `-run` 表达式未命中 unreadable regular 顶层测试；已单独修正并以 verbose
  定向执行确认四个子场景实际运行。

## Decision Log

- Decision: 完整 profile validation 与 leaf payload observation 分离，而不是把完整 observation
  简单裁剪成 partial profile。
  Rationale: partial 不能绕过 identity/ancestor/collision 安全证明，但这些证明不需要读取非 scope
  regular 内容。
  Date: 2026-07-20

- Decision: Precondition 表达 action validity predicate，不再默认比较最大 Observation。
  Rationale: §6 要求参与决策的证据保持成立；S1b 在内容变化后仍然是合法 adopt，而 owned link
  migration 和 regular replacement 仍需要更强证据。
  Date: 2026-07-20

- Decision: 保留 CP4 的 session-local Store publish 故障接缝，暂不拆分 StateEffect draft。
  Rationale: 前者提供真实 Store-stage 恢复证据且不影响 production 默认路径；后者没有当前行为
  故障，混入本修复会扩大 review 面。
  Date: 2026-07-20

- Decision: link/scaffold 的 state upsert 共有形态由 executor 的一个纯 validator 表达。
  Rationale: runner 在任何 mutation 前必须证明 effect 可被当前 state transition 消费；让两类 action
  复用 key/module/kind/source/link destination 校验可消除已发生的 contract drift。
  Date: 2026-07-20

- Decision: runner 只接受与 execution class、TargetMutated、state effect 和 execute error 一致的
  FileResult。
  Rationale: target mutation 的已提交 success 可以携带 cleanup error，state-only success 不存在同类
  提交点；矛盾结果不形成 state update，已报告的物理提交由 L2/S1b 等既定收养路径恢复。
  Date: 2026-07-20

## Outcomes and Handoff

完成。交付 commits：

- `33ebbc3`：按 scope/action 获取 regular digest，以 LeafCondition 表达最小而充分的 Precondition。
- `7791cd5`：统一 execution class、executor capability preflight，并移除 skip executor 死分支。
- `503e8d6`：澄清 runner Result 的 attempts/commit/effect/publish 边界。
- `4eab0e0`：闭合 link/scaffold upsert 与 FileResult 组合协议，补充零 executor/零 state 回归。
- `000314b`：修正 ExecPlan 窄测匹配。
- `dc1a035`：拒绝 state-only success 携带 error，同时保留 target post-commit error 的部分成功记账。

最终本地证据：

    go test ./internal/planner ./internal/executor ./internal/apply ./internal/runtime ./internal/state
    go test -race ./internal/planner ./internal/executor ./internal/apply ./internal/runtime ./internal/state
    go test -count=10 ./internal/apply -run 'TestRun_(PersistsPostCommitCleanupResultBeforeReturningError|RejectsExecutionResultsThatContradictActionClass)'
    go test -count=10 ./internal/executor -run 'TestValidateFileAction_RejectsIncompleteLinkUpsert'
    make check BINARY=/private/tmp/dot-cp4-contract-check/dot
    GOOS=linux GOARCH=amd64 go build ./...
    git diff 25eacdda94a475183a9c264eedf2041b9a958f6e...HEAD --check

上述命令均退出 0；`make check` 包含全仓 lint、format/tidy check、race、build 和隔离 manifest
检查。三位未参与实现的 reviewer 在第三轮完整 branch review 均给出 GO，无 unresolved P0–P3
finding。没有新增依赖、公开 CLI/state v1/ownership 变化或 M2/M3 能力。精确最终 HEAD 的远端
macOS/Linux CI 未实际运行：本地验收通过、远端待验收。
