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
- [ ] 测试先固定 exact-input planning 和 CP4 预检，再实现 planner 接缝与范围门禁。
- [ ] 测试先固定锁内顺序执行、部分成功、Store 失败恢复和幂等，再连接内部 runner。
- [ ] 完成窄测、重复/race、CLI 拒绝回归、diff check 与 `make check`，保持计划 active 等待复核。

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

## Decision Log

- Decision: state transition 接收 state-native upsert，而不让 state package 导入 planner effect。
  Rationale: planner 已依赖 state；反向导入会形成 cycle，并把执行协议泄漏到持久格式层。
  Date: 2026-07-20

- Decision: conflict、skip、deferred prune 和 hook skip 是非执行计划事实；runner 只执行当前 CP4
  允许的 file mutation/adopt，且在执行前完整拒绝未交付 executable action。
  Rationale: 规范允许 conflict 与其他文件动作并存，但禁止把范围外 mutation 静默跳过。
  Date: 2026-07-20

## Outcomes and Handoff

实施中；完成实现、验证与自检后补充 commits、证据、风险和未验证项，并保持计划 active 交给
未参与实现的 reviewer。
