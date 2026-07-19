# feat/apply-runtime：连接 M1 apply 安全执行链

本 ExecPlan 是 living document。实施期间持续更新 `Progress`、`Surprises & Discoveries`、
`Decision Log` 和 `Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成后，内部调用方可以在一个 mutation lock 周期内严格加载 manifest/state，用同一个
`LoadedMutation.Inputs()` 形成 canonical `ApplyPlan`，预先拒绝 CP4 尚未交付的可执行动作，
再按计划顺序执行 link/scaffold。每个已越过提交点的成功 action 都进入唯一 state transition，
即使后续 action、cleanup 或 state Store 失败也不回滚 target；重跑通过 L2/S1b 自动补录，成功
收敛后的再次运行既不触碰 target，也不重复 adopt/Store。真实 `dot apply` 仍硬拒绝。

## Scope / Non-goals

范围内：

- 建立 state v1 entry transition 单一真相源：missing/loaded 基线、保留未涉及 entries/run_once、
  原子 upsert/PreviousKey、AppliedAt 与无变化判定。
- 为 planner 提供消费既有 `runtime.LoadedInputs` 的 canonical exact-input 入口；MutationSession
  加载后不得重新无锁调用 `PlanApply`。
- 在任何 file mutation 前整体拒绝 `FileBackupReplace`、`ScaffoldRebuild`、active prune 与
  `HookRun`；conflict/deferred/skip 保留为非执行计划事实，不静默冒充成功动作。
- 内部 apply runner 持锁覆盖 load→plan→precheck→顺序 file execute→一次 state commit→close；
  正确消费 `FileResult` 的成功/失败 effect 和 post-commit cleanup error。
- 用完全隔离的合成 HOME/repo/config/state 固定部分成功、原子 persistence、Store 失败恢复、
  L2/S1b 补录、第二/第三次运行幂等与 CLI apply 继续拒绝。

明确不做：

- 不连接 Cobra 或公开真实 `dot apply`，不改变公开输出/退出码。
- 不实现 backup/force/prune/hooks/add/init/managed/rendered，也不改变 state v1、ownership 或
  planner 决策表。
- 不吞错、不在 Store 失败后回滚 target、不重新加载 state 猜测恢复结果。

## Contract and Context

- `docs/02-architecture.md` §4–§6：mutation 锁覆盖 load/plan/execute/persist，file actions 顺序
  执行，已成功 effect 必须一次原子提交；state Store 失败不得回滚 target。
- `docs/04-cli-spec.md` §2–§4.4：conflict 不阻塞其他文件动作；CP4 不公开 apply，也不得借内部
  runner 执行未交付的 force/prune/hooks。
- `docs/05-apply-engine.md` §1–§7/§10：失败 action 保留旧账，部分成功必须记账；提交后 state
  失败重跑走 L2/S1b，成功收敛后重跑零 mutation/adopt。
- `docs/06-templates.md`：scaffold state 永不提供 target ownership，恢复只能 state-only 补录。
- `docs/08-testing.md`：真实路径语义、完整旧/新 state、Store 故障和完整隔离是验收重点。
- `docs/09-roadmap.md`：本节点只交付内部 link/scaffold 执行链，不提前交付公开 apply 或 M2/M3。

基线是 clean `main@061b783ee5553ced594f3004ccaed0854551f2ed` 创建的
`feat/apply-runtime`。前置 link/scaffold 已提供 `executor.ExecuteFile` 与明确 `FileResult` 提交点
契约；runtime 已提供持锁 `BeginMutation→Load→CommitState→Close`，planner 只有会调用
`LoadReadOnly` 的公开 `PlanApply`。真实缺口是 state 没有安全 transition builder，planner 没有
exact-input 公开接缝，也没有将三者连接的内部 orchestration package。

## Progress

- [x] 2026-07-20：确认分配 worktree、Git 顶层和 branch 均正确，HEAD 为 `061b783` 且 clean；
  读取规范、completed plans 与 runtime/planner/state/executor 当前实现。
- [x] 2026-07-20：以 `140c931` 提交本 active ExecPlan 起点。
- [x] 2026-07-20：测试先固定 state transition；随后实现 missing/loaded、未涉及 entry/run_once
  保留、PreviousKey 同提交迁移、AppliedAt、无变化判定与歧义 update fail closed。
- [x] 2026-07-20：测试先证明磁盘 state 在 strict load 后变化不会影响 exact-input plan，并固定
  backup-replace/ScaffoldRebuild/active prune/HookRun 整体拒绝；随后实现 `PlanLoadedApply` 与
  `internal/apply` 范围门禁。
- [x] 2026-07-20：测试先固定锁内顺序执行、scope gate 零 executor、部分成功、post-commit
  cleanup、Store/Close 错误合并和真实 Store 失败恢复；随后连接 `internal/apply.Run`，验证
  L2/S1b adopt 与第三次零 target mutation/adopt/Store。
- [x] 2026-07-20：state/planner/apply/executor/runtime 窄测、apply/state 20 次重复、相关包 race、
  CLI 真实 apply 拒绝、Linux/amd64 test binary、基线 diff check 与完整 `make check` 通过；
  更新 handoff，计划保持 active 等待独立复核。
- [x] 2026-07-20：未参与实现的 reviewer 对 `061b783...fcefa15` 完整复核 GO，无 P0–P3；
  主 agent 复核完整 diff，并重跑窄测、CLI 拒绝回归、diff check 与最终 `make check` 通过。

## Milestones

### Milestone 1：建立 state entry transition 单一真相源

先在 `internal/state` 增加测试，覆盖 missing 与 loaded 基线、未涉及 entry、全部 run_once、
多个成功 upsert、PreviousKey 同提交迁移、AppliedAt、重复/冲突 update fail closed 和输入快照不被
修改。实现只接收 state-native entry updates，不导入 planner；返回完整有效 Snapshot 和 changed
标志，无 updates 时不制造 Store 需求。

Commit 边界：

    feat(state): 建立 apply entry transition

### Milestone 2：建立锁内 exact-input planner 接缝与 CP4 预检

先证明 loaded state 文件在加载后变化也不会被 planner 重读，再暴露消费既有
`LoadedInputs` 的 canonical 入口；`PlanApply` 仍复用同一组合核心。新增内部 apply package 的纯
预检，完整扫描 file/prune/hook plan，在任何 executor 调用前拒绝 backup-replace、
ScaffoldRebuild、active prune、HookRun 和未知 executable 形态；skip/conflict/deferred 保持
非执行事实。

Commit 边界：

    feat(planner): 支持 exact-input apply 规划

### Milestone 3：连接持锁 runner、部分成功 persistence 与恢复

先用合成文件系统和可控 operation seam 固定：锁覆盖完整周期；file action 串行；成功 effect
按 action 顺序转换；post-commit cleanup error 仍先记账；普通 error/Precond 后停止后续动作并
一次提交此前成功；CommitState/Close 错误合并且 target 不回滚。真实隔离集成测试覆盖 missing
state 首跑、Store 失败后的 L2/S1b adopt 恢复、第三次以及成功收敛后的第二次零 target
mutation/adopt/Store。

Commit 边界：

    feat(apply): 连接锁内执行与部分成功记账

## Validation and Acceptance

最终从分配 worktree root 运行：

    go test ./internal/state ./internal/planner ./internal/apply ./internal/executor ./internal/runtime
    go test ./internal/apply ./internal/state -count=20
    go test -race ./internal/state ./internal/planner ./internal/apply ./internal/executor ./internal/runtime
    go test ./internal/cli -run 'Apply.*Unsupported|Unsupported.*Apply'
    git diff 061b783ee5553ced594f3004ccaed0854551f2ed...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp4-runtime-check

所有命令必须退出 0，完整 diff 只包含本计划、state transition、planner exact-input 接缝和内部
apply runner/tests，worktree clean。当前原生平台为 Darwin/arm64；远端 macOS/Linux CI 未运行时
只报告“本地验收通过、远端待验收”。

## Safety, Authorization, and Recovery

测试只使用 `t.TempDir()` 下绝对 HOME/repo/config/state/binary/target，并显式覆盖 runtime
路径；不读取或修改真实 `modules/`、machine config、state、backup、`.env` 或主力 HOME。
失败保留最近成功 commit，以新 commit 修复，不 amend、rebase、cherry-pick、squash、reset、
force，也不操作 main、coordinator、其他 worktree 或 branch。若 exact-input 必须重读、无法可靠
提交部分成功、需要改变持久格式/公开行为或只能吞错，立即停止请求裁决。

## Interfaces and Dependencies

不新增依赖。`internal/state` 提供不依赖 planner 的 entry transition；planner 继续单向依赖
runtime/state，新增 exact-input 入口；新 `internal/apply` 位于组合层，单向依赖 runtime、planner、
executor、state，避免 runtime↔planner import cycle。operation seam 只覆盖 orchestration 边界，
生产执行仍使用既有 MutationSession 和 executor。

## Surprises & Discoveries

- Observation: `internal/planner` 已依赖 `internal/runtime`，因此 runner 不能直接放入 runtime
  package 而再导入 planner。
  Evidence: `planner.PlanApply` 当前调用 `runtime.LoadReadOnly`。
  Impact: orchestration 放入独立 `internal/apply`；runtime 仍只负责严格加载、锁和 state commit，
  planner/runtime 依赖方向不反转。

- Observation: state Store 失败可以在完整隔离集成测试中通过 rename 已持锁 state root、在旧路径
  放置普通文件稳定触发，不需要修改生产 state Store 或依赖权限位行为。
  Evidence: 两个 file action 均越过 target 提交点后，`CommitState` 因 state root 不是目录失败；
  还原 root 后重跑形成 link/scaffold 两个 state-only adopt，第三次无 executor 与 Store。
  Impact: 恢复测试覆盖真实 MutationSession、lock、planner、executor、state.Store 全链，而不是
  mock 成功路径；所有操作只发生在 `t.TempDir()`。

## Decision Log

- Decision: state transition 接收 state-native upsert，而不让 state package 导入 planner effect。
  Rationale: planner 已依赖 state；反向导入会形成 cycle，并把执行协议泄漏到持久格式层。
  Date: 2026-07-20

- Decision: conflict、skip、deferred prune 和 hook skip 是非执行计划事实；runner 只执行当前 CP4
  允许的 file mutation/adopt，且在执行前完整拒绝未交付 executable action。
  Rationale: 规范允许 conflict 与其他文件动作并存，但禁止把范围外 mutation 静默跳过。
  Date: 2026-07-20

## Outcomes and Handoff

Milestone 已达到 review-ready。branch 基线为 `061b783ee555`，未参与实现的 reviewer 完整复核
GO，无 P0–P3 finding；主 agent 复核完整 diff 与提交边界后重跑最终门禁。已形成以下 commits：

    140c931 docs(apply): 建立 apply runtime 执行计划
    3b558a9 feat(state): 建立 apply entry transition
    464c4f4 feat(planner): 支持 exact-input apply 规划
    a7bf63f feat(apply): 连接锁内执行与部分成功记账
    b08f52b fix(state): 保留 transition 校验错误链
    d1bf9f6 test(apply): 覆盖真实 Precond 部分成功

`state.TransitionEntries` 现在从 missing/loaded 严格基线形成完整 Snapshot，保留未涉及 entries 和
全部 run_once，按一个 transition 处理多个 upsert 与 `PreviousKey`，验证 `AppliedAt`，并以
changed 标志避免无变化 Store。`planner.PlanLoadedApply` 只消费调用方已有
`LoadedMutation.Inputs()`；测试在 strict load 后改写磁盘 state，exact-input plan 仍采用旧输入，
而普通 `PlanApply` 才看到重新加载结果。

`internal/apply.Run` 在一个 MutationSession 中完成 load→exact-input plan→完整 scope gate→顺序
file execute→一次 `CommitState`→Close。scope gate 在 executor 前拒绝 backup-replace、
ScaffoldRebuild、active prune、HookRun 与未知 executable verb；skip/conflict/deferred/hook skip
仅作为非执行计划事实。runner 只应用 executor 实际返回的成功 upsert：普通 error/Precond 保留
失败项旧账并提交此前成功；post-commit cleanup error 同样先记当前成功；Store 失败不回滚 target，
Close error 与主要错误合并。

真实隔离证据覆盖：两个 target 均成功后故意让 state Store 路径变成普通文件，Store 失败而
link/scaffold 保留；还原路径后重跑得到两个 state-only L2/S1b adopt；成功提交后的下一次运行为
零 executor、零 adopt、零 target mutation、零 Store。另一个真实 Precond 用例在首项 scaffold
成功后让第二项 target 出现并失败，最终 state 包含首项新账、第二项旧账与原 run_once。执行被
阻塞期间第二个 `lock.Acquire` 返回 `ErrBusy`，Run Close 后可再次取得锁。

本地证据：

    go test ./internal/state ./internal/planner ./internal/apply ./internal/executor ./internal/runtime
    go test ./internal/apply ./internal/state -count=20
    go test -race ./internal/state ./internal/planner ./internal/apply ./internal/executor ./internal/runtime
    go test ./internal/cli -run TestApply_RejectsMutationAndAdoptBeforeRuntime
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-m1-cp4-runtime-linux-amd64.test ./internal/apply
    git diff 061b783ee5553ced594f3004ccaed0854551f2ed...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp4-runtime-final-gate

全部退出 0；`make check` 包含 tidy/fmt check、lint 0 issues、全仓 race、build 与隔离
doctor-manifest。没有新增依赖、state v1/ownership/公开输出/CLI wiring 改动，也没有执行
backup/force/prune/hooks/add/init/managed。当前原生平台为 Darwin/arm64；Linux 只完成交叉编译，
远端 macOS/Linux CI 未运行：本地验收通过、远端待验收。

独立 reviewer 已确认两个组合层取舍：runner 遇到第一个 executor error 后停止尚未开始的后续
file action，但仍一次提交已成功 effects；conflict 不作为 executor error，其他 file action 继续
执行。`FileResult` 语义优先于 `err != nil`，因此 link L3/scaffold S3 的 cleanup error 不会丢账。
