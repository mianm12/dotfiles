# chore/m1-cp4-orchestration：交付 link/scaffold 安全执行内核

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。本次用户明确选择“一条 Checkpoint Goal 编排多个 branch”，这是对默认
一 Goal/一 branch 组织方式的有意例外；本计划只记录跨 Milestone 的 DAG、调度、基线、发现、
验收证据和最终结果，每个 Milestone 仍有独立 branch、active ExecPlan、语义 commits 与 review
单元。

## Purpose / Big Picture

完成后，M1 的纯 apply plan 可以在内部锁定的 mutation session 中安全执行 link 与 scaffold
文件动作：每个提交都重新验证 source、target、祖先拓扑和 control-plane Precond，创建不覆盖
并发新对象，替换只呈现完整旧/新对象；成功动作形成准确 state，即使后续动作失败也原子落账，
崩溃后由 L2/S1b 收养并在下一次运行达到零 mutation。Checkpoint 仍不公开真实 `dot apply`。

## Scope / Non-goals

范围内：

- 编排 `feat/apply-link`、`feat/apply-scaffold`、`feat/apply-runtime` 三个严格串行 Milestone。
- 实现 create-link、relink、link adopt、scaffold 创建/补录、symlink↔scaffold migration、共享
  Precond 复核、安全祖先创建、no-clobber、完整旧/新对象与 hard-link 隔离。
- 连接 locked load、同一输入形成 plan、executor、部分成功 state 变换和单次原子 persistence，
  覆盖 L2/S1b 崩溃恢复与成功收敛后的第二次零 mutation。

明确不做：

- 不公开真实 `dot apply`，不改变现有 CLI 对非 dry-run apply 的硬拒绝。
- 不实现 backup、force execute、prune execute、hooks execute、add、init、managed/rendered 或 M2/M3。
- 不改变 state v1、ownership、公开输出/退出码或 docs 契约迁就实现；不新增兼容 adapter 或静默
  跳过不支持的可执行动作。
- 不执行 fetch、pull、push、rebase、cherry-pick、squash、amend、reset、force、PR、tag 或 Release。

## Contract and Context

- `docs/02-architecture.md` §4–§6：锁覆盖完整 mutation，executor 只消费自包含 plan，成功动作
  才更新 state，部分成功仍须落账。
- `docs/04-cli-spec.md` §2–§4.4：真实 apply 的公开行为尚不在本 Checkpoint；dry-run 继续无锁零写入。
- `docs/05-apply-engine.md` §1–§7/§10：L/S、M1 kind migration、Precond、原子发布、恢复与幂等。
- `docs/06-templates.md`：scaffold payload 在 plan 阶段已完整渲染，产物创建后归用户。
- `docs/08-testing.md`、`docs/09-roadmap.md`：M1 link/scaffold 执行与 state 恢复门槛，managed 留待 M2。

`checkpoint_base` 是本地 `main@e9e8bac6e5c1406e0db8aeb6e9eca6194aeeddb2`。Plan Gate 时
`HEAD == main == origin/main`、ahead/behind 为 0，`upstream/main` 不存在；未 fetch/pull。
main clean、仅有 main worktree，全部 CP4 目标 branch 均不存在。CP3 coordinator、Acceptance
fix 和随后 planner contract 强化均已 completed 且是当前 main 祖先；基线
`make check BINARY=/private/tmp/dot-m1-cp4-plan-gate` 在 Darwin/arm64 通过，精确远端 CI 未运行。

现有 `internal/planner` 已唯一表达 L/S decision、`FileAction`、`Precondition` 与 `StateEffect`；
`internal/runtime` 已提供 `BeginMutation → Load → CommitState → Close`，`internal/state` 已提供严格
加载和原子 Store，`internal/paths` 已提供 target resolution/control boundary。缺口是安全 file
executor、可验证 state transition，以及基于同一 locked `LoadedInputs` 的内部编排入口。

## Progress

- [x] 2026-07-20：确认 CP3 已合入；main clean，`main == origin/main == e9e8bac`，无
  `upstream/main`，未 fetch/pull；目标 branches 不存在。
- [x] 2026-07-20：读取仓库规则、ExecPlan 生命周期、指定规范、当前实现/测试与相关 completed
  plans；基线完整门禁通过。
- [x] 2026-07-20：三名只读 subagent 完成规范缺口、DAG/共享契约及测试/依赖/双平台审计；
  主 agent 核对后无停止条件，确定严格串行 DAG。
- [x] 2026-07-20：从 checkpoint base 创建 coordinator branch/worktree 并建立本计划。
- [x] 2026-07-20：以 `4dfe964` 提交 coordinator ExecPlan 起点，启动 `feat/apply-link`。
- [x] 2026-07-20：`feat/apply-link` 完成 L1/L2、L3、source 模块边界与最终 no-clobber；
  独立完整复核 GO、无 P0–P3。closure 后 main 以 `f4522b0` fast-forward-only 集成，合入后
  窄测与 `make check BINARY=/private/tmp/dot-m1-cp4-main-after-link` 通过。
- [x] 2026-07-20：`feat/apply-scaffold` 完成 S1a–S3 当前子集、state-only 补录、双向 kind
  migration、no-clobber 完整发布与 hard-link 隔离；独立完整复核 GO、无 P0–P3。closure 后
  main 以 `061b783` fast-forward-only 集成，合入后窄测与
  `make check BINARY=/private/tmp/dot-m1-cp4-main-after-scaffold` 通过。
- [x] 2026-07-20：`feat/apply-runtime` 完成 state transition、locked exact-input plan、范围 gate、
  部分成功 persistence、Store 失败恢复与稳定零 mutation；独立完整复核 GO、无 P0–P3。
  closure 后 main 以 `0a6233d` fast-forward-only 集成，合入后窄测、CLI 拒绝回归与
  `make check BINARY=/private/tmp/dot-m1-cp4-main-after-runtime` 通过。
- [x] 2026-07-20：三路完整 Checkpoint Acceptance 首轮完成；规范/数据流 reviewer GO，数据保护
  与架构 reviewer 确认两个有效 P2：真实 mutation fixture 未隔离 `DOT_CONFIG`/`DOT_REPO` 与
  HOME/XDG；既有 Store 恢复测试在 runtime path validation 先失败，未证明越过验证后的 Store
  阶段。main 保持 `0a6233d` clean，已创建 `fix/m1-apply-core-acceptance` 修复共享验收证据根因。
- [x] 2026-07-20：`fix/m1-apply-core-acceptance` 以 `84cff56` 隔离 HOME/XDG/DOT 控制面环境，
  以 `eb5753d` 在真实 state Store 完成 write/sync/close 后的 publish 点注入失败；独立完整复审
  GO、无 P0–P3。main fast-forward-only 集成后窄测与
  `make check BINARY=/private/tmp/dot-m1-cp4-main-after-acceptance-fix` 通过。
- [x] 2026-07-20：三名未参与实现的 reviewer 从 `checkpoint_base...main@eb5753d` 重新审查完整
  Checkpoint；规范/公开行为/数据流、数据保护/恢复、架构/测试/双平台三路均 GO、无 P0–P3。
- [x] 2026-07-20：最终 `git diff e9e8bac...main --check`、相关窄测、CLI 硬拒绝回归与
  `make check BINARY=/private/tmp/dot-m1-cp4-acceptance-final` 通过；main clean。
- [x] 2026-07-20：以 `f60a3be` 将最终 main 非重写合入 coordinator；更新 Outcomes/Handoff，
  迁移本计划并以纯计划 commit 收口，随后 fast-forward-only 合入 main。
- [x] 2026-07-20：首次收口后的连续审查又发现 evidence contract 与 state topology 的实现/证据
  缺口；两组独立 fix 已按各自 ExecPlan 合入 current main `34f8609`。
- [x] 2026-07-20：Checkpoint 整体重验收确认本计划仍把 `eb5753d` 记为最终结果，构成有效 P1
  lifecycle finding；coordinator branch 已仅以本 Goal 提交 fast-forward 到 current main，现按
  `.agent/plans/README.md` 重新开启本计划。
- [x] 2026-07-20：`m1-cp4-evidence-contract@9ba6964` 闭合最小充分 Precondition、execution class、
  capability preflight、result/effect 协议与 Store 故障证据；三轮完整 review 后 GO。
- [x] 2026-07-20：`m1-cp4-state-topology@34f8609` 分离 immutable persisted baseline 与 mutable
  candidate success-prefix，前置保护部分成功和 target mutation→state publish 恢复窗口；三轮
  完整 review 后 GO。
- [x] 2026-07-20：最新整体 Acceptance 发现完整 target-set 漏掉单 target self traversal；
  `m1-cp4-target-self-traversal@f3e772e` 在共享 validator 闭合 full/partial/S1b，并保留 state
  equal-identity corrupt 分类；三轮完整 review 后 GO。
- [x] 2026-07-20：三名未参与实现的 reviewer 从 `checkpoint_base...main@f3e772e` 重新审查完整
  Checkpoint，规范/公开数据流、数据保护/恢复、Go/测试/双平台三路全部 GO，无 P0–P3。
- [x] 2026-07-20：以 `693b758` 非重写合入最终 main；checkpoint diff check 与
  `make check BINARY=/private/tmp/dot-m1-cp4-coordinator-final/dot` 通过，更新 Outcomes/Handoff，
  迁移本计划并以纯计划 commit 收口，随后交由 main FF-only 集成。

## Milestone DAG and Scheduling

```text
apply-link → apply-scaffold → apply-runtime
```

三个节点共享 executor、Precond、安全发布与 state effect contract，runtime 还消费前两者定义的
提交点和结果，因此不并行。coordinator 与 apply-link 从 checkpoint base 创建；scaffold 只在
link closure 已 ff 合入 main 且门禁通过后从当时 main 创建；runtime 同理从 scaffold 合入后的
main 创建。每个 Wave 只有一个 in-progress 节点。若 main 出现 DAG 外提交、共享契约要求发生
语义变化或 freshness 发生语义冲突，立即停止并重新规划。

每个 worker 先确认 `pwd` 与 Git 顶层均等于分配的 `/private/tmp` worktree，创建并先提交独立
active ExecPlan；测试先行，按可解释行为形成多个 commits，运行窄测、完整 diff check、
`make check` 并保持 clean。未参与实现的 reviewer 复用停止写入的 worker worktree；有效 finding
使用新 fix commit，最多三轮完整复核。

## Milestone Contracts

### `feat/apply-link`

建立共享 file executor 的提交前复核和安全发布内核，实现 L1 create-link、L3 owned relink 与
L2 state-only adopt。缺失祖先只按安全目录语义创建；提交前重新验证 target resolution、leaf
snapshot、control boundary 和 regular source；L1 绝不覆盖并发新对象，L3 失败时保持完整旧链，
成功后只见完整新链。动作返回成功/失败结果但不自行持锁或持久化完整 state。

### `feat/apply-scaffold`

复用 link 的 Precond/发布内核实现 S1a–S3 的当前非 force 子集、S1b state-only 补录和
symlink↔scaffold migration。新 scaffold 在同目录准备完整 bytes/mode 后排他发布；owned
symlink→scaffold 原子替换为独立普通文件，非 owned/缺失只释放所有权。不得原地修改既有普通
文件或共享 inode，不实现 S2 force 重建。

### `feat/apply-runtime`

建立受校验的 state transition，保留未涉及 entries/run_once，原子处理 `PreviousKey` 与成功
upsert；新增上层内部编排，从同一 mutation session 的 `LoadedInputs` 形成 plan、预先拒绝 CP4
范围外可执行 backup/prune/hook 分支、顺序执行 file actions、积累成功 state effects，并在后续
动作失败时仍一次原子提交先前成功结果。state Store 失败不回滚已发布 target，重跑通过 L2/S1b
收养，成功收敛并落账后的下一次执行不产生 target mutation、adopt 或 state Store。

## Validation and Acceptance

| 必须成立的性质 | 主要证据 | 状态 |
|---|---|---|
| L1–L3、source/target/ancestor Precond 与 no-clobber | executor filesystem tests | 通过 |
| S1a–S3 当前子集与 symlink↔scaffold migration | executor table/filesystem tests | 通过 |
| 完整旧/新对象与 hard-link 隔离 | failure injection + `os.SameFile` tests | 通过 |
| locked exact-input planning 与范围外动作零 mutation 拒绝 | internal apply integration tests | 通过 |
| 部分成功 state、原子 persistence、Store 失败恢复 | runtime/state integration tests | 通过 |
| L2/S1b 恢复与第二次运行零 mutation | end-to-end isolated tests | 通过 |
| file/state success-prefix、旧 state 可重载与 self traversal | planner/paths/runtime/apply regressions | 通过 |
| 完整 Checkpoint 本地门禁 | checkpoint diff check + make check | 通过 |
| 精确最终 HEAD 远端 macOS/Linux CI | GitHub Actions | 待验收：本 Goal 不 push |

每个 Milestone 运行其窄测、`git diff <effective-base>...HEAD --check` 与唯一 BINARY 路径的
`make check`。最终至少运行：

    git diff e9e8bac6e5c1406e0db8aeb6e9eca6194aeeddb2...main --check
    make check BINARY=/private/tmp/dot-m1-cp4-coordinator-final/dot

本地平台是 Darwin/arm64；远端 macOS/Linux CI 未实际运行时结论必须是“本地验收通过、远端待
验收”，不得以交叉编译代替实机证据。

## Safety, Authorization, and Recovery

当前用户 Goal 已明确授权本 Checkpoint 的 coordinator/Milestone/integration-fix/acceptance-fix
branches、`/private/tmp` worktrees、范围内修改、stage、commit、计划迁移、freshness merge 和
本地 fast-forward-only main 集成；该证据只适用于本次 Goal，不由计划延续。

测试只使用 `t.TempDir()` 或 `/private/tmp` 的合成 HOME/repo/config/state/backup，不读取或写入
真实 modules、machine config、state、backup、`.env` 或主力 HOME。mutation 手工验证必须同时
重定向 HOME、repo、config、state 与 backup。失败保留最近成功 commit；不 amend、rebase、
cherry-pick、squash、reset、force 或删除 branch。只对本 Goal 创建且 clean、已合入的 worktree
使用不带 `--force` 的移除。

## Interfaces and Dependencies

不新增依赖。共享 contract 是 planner 的 immutable action/Precond/state effect、paths 的 target
resolution/control boundary、executor 的逐动作结果和 state 的受校验 transition。`planner` 已依赖
`runtime`，因此 locked 编排应位于新的上层内部 package，避免 import cycle；runtime 不重新解释
plan，executor 不重新解释 manifest。若实现证明必须引入平台专用 syscall 或新依赖且出现维护/
兼容性取舍，按停止条件请求裁决。

## Surprises & Discoveries

- Observation: planner 已经形成 future executor 的 canonical action contract，并在组合层重算
  decision/prune 防止畸形计划。
  Evidence: `internal/planner/model.go`、`decision.go`、`apply_plan.go` 和 completed
  `m1-planner-contract`。
  Impact: executor 只消费 plan；不得复制 L/S decision 或放宽 validation。

- Observation: `state.Snapshot` 只有严格 getter，尚无 apply transition/builder；`PlanApply` 又固定
  自行调用无锁 `LoadReadOnly`。
  Evidence: `internal/state/model.go`、`internal/planner/apply_plan.go`、
  `internal/runtime/session.go`。
  Impact: runtime Milestone 必须分别增加单一 state 变换真相源和 locked exact-input planning 接缝，
  不能重新加载或让 link/scaffold 各自构造 state。

- Observation: 普通 `Rename` 可以覆盖并发出现对象，不满足 missing-only 发布。
  Evidence: 标准库语义与 Plan Gate 跨平台审计。
  Impact: missing-only link 使用 `os.Symlink` 的排他创建；scaffold 在同目录准备完整 temp 后以可移植
  no-clobber 发布，替换仅用于 owned 且最终 Precond 成立的分支。

- Observation: L3 可能在 target rename 已成功后因临时目录 cleanup 失败返回非空 error。
  Evidence: `feat/apply-link` 的提交点回归与独立复核。
  Impact: runtime 必须先消费返回的 `OnSuccess`/`TargetMutated=true` 并提交 state，再报告 cleanup
  error；不得用 `err != nil` 丢弃已提交结果。

- Observation: 首轮 Checkpoint Acceptance 发现真实 mutation fixture 仍可能继承开发机控制面环境，
  且原 Store 故障构造在 `CommitState` 的 path validation 阶段提前失败。
  Evidence: 两名独立 reviewer 对 `internal/apply/run_test.go` 和 runtime pre-store 校验链的审查。
  Impact: acceptance fix 必须显式隔离全部 HOME/XDG/DOT 控制面环境，并以能证明进入 Store 阶段的
  接缝或场景固定恢复链和错误原因；不得只把断言改宽。

- Observation: 仅把 state root 替换为普通文件会被 runtime 的 control/path validation 提前拒绝，
  不能作为 Store-stage failure 的验收证据。
  Evidence: Acceptance fix 的 test-first 编译失败、`BeginMutationWithStateStore`、
  `StoreWithPublisher` 与 publish sentinel 错误链断言。
  Impact: 精确故障测试使用 session-local operations 副本，并仍经过 encode、路径验证、temporary
  write/chmod/sync/close；生产 `BeginMutation`/`Store` 默认路径不变，也不引入全局 fault hook。

- Observation: 完整 profile 的结构校验不需要读取非 scope regular payload，而 executor contract
  必须在首个 mutation 前统一验证 action class、完整 state upsert 与 result/effect/error 组合。
  Evidence: evidence-contract fix 的 unreadable out-of-scope fixture、malformed link upsert 与
  state-only success+error 回归。
  Impact: Precondition 只携带决策实际依赖的证据；executor capability 和 runner protocol 各有单一
  validator，不再用过度 observation 或分支特例表达安全性。

- Observation: candidate state 的逻辑 `PreviousKey` 删除不等于磁盘旧 state 已发布；file actions
  全部完成前发生 Store failure 时，恢复性必须始终以 immutable persisted baseline 为准。
  Evidence: orphan、matched alias、partial/preserve 与 L3 publish-failure 回归。
  Impact: planner 分别验证每个 candidate success prefix 和每个 target mutation 对旧 state traversal
  的影响；runtime 的 late validation 只保留为 freshness/TOCTOU 门禁。

- Observation: target path 可在 `..` 折返前穿过自身 leaf；同一集合还可能同时存在更具体的 equal
  pair conflict。
  Evidence: `bridge -> real`、`detour -> bridge/..` 的 full/partial/S1b 与双 state-key fixtures。
  Impact: 共享 target-set 固定为 resolve→pair→unary，既整体拒绝 self traversal，又保留 state
  equal-identity 的 `ErrCorrupt`/`ErrTargetIdentityConflict` 分类。

## Decision Log

- Decision: 保持默认串行 DAG，不并行 link/scaffold。
  Rationale: 两者修改同一安全提交内核，runtime 又依赖二者的结果与 state effect；复制 adapter
  制造并行会产生多处真相源。
  Date: 2026-07-20

- Decision: CP4 内部 runtime 对 backup-replace、active prune 与 run-hook 在首次 target mutation
  前 fail closed；真实 CLI apply 继续拒绝。
  Rationale: 这些能力属于后续 Checkpoint，本切片不能静默跳过或预建公开行为。
  Date: 2026-07-20

- Decision: 不新增第三方依赖，优先标准库同目录发布原语与现有路径能力。
  Rationale: 当前机制足够，避免把 ownership、Precond、恢复或数据保护契约交给外部库。
  Date: 2026-07-20

- Decision: 接受首轮 Acceptance 的两个 P2，并在共享 acceptance-fix branch 修复测试环境与故障
  阶段证据；不改变产品实现契约。
  Rationale: 两项都影响安全性质的可证明性，但不要求修改公开行为、state 格式、ownership 或
  mutation 边界；独立复审确认新增接缝只服务确定性测试且生产路径不变。
  Date: 2026-07-20

- Decision: 首次 closure 后的新缺陷分别使用独立 fix ExecPlan 和语义 commits，coordinator 因最终
  结论失效而显式 reopen；不改写或把后续发现追加成旧 Milestone 的隐式补丁。
  Rationale: 三组问题分别属于 execution evidence、state transition topology 与共享 target-set，
  但都在 CP4 既有规范和授权内；独立生命周期保留可审计根因、review 轮次和验证证据。
  Date: 2026-07-20

- Decision: target-set 先报告 pair conflict，再报告无 pair 时的 unary self traversal。
  Rationale: pair error 携带双方 provenance 与 equal/ancestor 的更具体事实；该顺序让 state consumer
  保留永久 identity corruption 分类，同时所有 desired consumer 仍在 scope/executor 前 fail closed。
  Date: 2026-07-20

## Outcomes and Handoff

Checkpoint 4 已完成本地交付。原始三个 Milestone 按严格 DAG 独立计划、测试先行、语义提交、
完整复审、closure 后 FF-only 合入：`feat/apply-link@f4522b0`、
`feat/apply-scaffold@061b783`、`feat/apply-runtime@0a6233d`。首轮 Acceptance 的 fixture 隔离与
Store-stage 证据由 `fix/m1-apply-core-acceptance@eb5753d` 修复。

后续连续审查没有停在局部补丁：`m1-cp4-evidence-contract@9ba6964` 统一最小充分 Precondition、
executor capability 和 result/effect 协议；`m1-cp4-state-topology@34f8609` 建立完整 historical
baseline、candidate success-prefix 与 publish-window 恢复证明；
`m1-cp4-target-self-traversal@f3e772e` 把 unary topology 提升到共享完整 target-set，并明确
resolve→pair→unary 的 error contract。每组有效 finding 都以新的 test/fix commits 修复，并在最多
三轮完整 branch review 内收敛，无 unresolved finding。

最终实现提供内部 link/scaffold executor、严格 Precond/no-clobber/完整旧新对象、hard-link 隔离、
validated state transition，以及锁内 exact-input plan→execute→部分成功单次原子 persistence。
Store/崩溃窗口保留可 strict-load 的旧 state，重跑由 L2/S1b 收养并稳定达到零 executor/adopt/Store；
完整 effective profile 的 pair/unary topology 不能被 partial scope 或 state-only action 绕过。真实
非 dry-run `dot apply` 仍在 runtime 前硬拒绝；state v1、ownership、公开输出/退出码和 M2/M3 范围
均未改变，也未新增依赖。

三名未参与实现的 reviewer 对 `e9e8bac...main@f3e772e` 整体复核全部 GO、无 P0–P3。coordinator
以 `693b758` 同步该 main 后，最终 checkpoint diff check 与
`make check BINARY=/private/tmp/dot-m1-cp4-coordinator-final/dot` 在 Darwin/arm64 通过，Linux/amd64
apply test binary 与 CLI 仅完成交叉编译。main、coordinator 和已移除前的 worker worktree 均 clean；
所有 worker worktree 均无 force 移除。精确最终 HEAD 未 push，远端 macOS/Linux CI 未实际运行，
因此 handoff 结论是：**本地验收通过、远端待验收**。
