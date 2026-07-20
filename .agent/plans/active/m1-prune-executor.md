# feat/prune-executor：交付 prune 执行与 apply 阶段编排

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，内部 apply runner 能在 file 阶段收敛且 whole-module 清理获得确认时，按 planner
已形成的 canonical P1/P2/P3 动作执行 prune，并把成功 file upsert 与 prune delete 合并成一次
state Store。file 错误、Precondition 失配或确认拒绝不会误删 target；scope 内存在任何
run_once 动作时会在一切 mutation 前硬拒绝。

## Scope / Non-goals

范围内：

- 执行 P1 scaffold 摘除 state、P2 owned symlink 删除 target 和 state、P3 unowned 只摘 state。
- state transition 同时接受 upsert/delete，保留未涉及 entry 与全部 run_once。
- runner 按 files → confirmation → prune → single Store 编排，报告部分成功、deferred 和确认结果。
- file error/Precondition 失配、计划 conflict、确认拒绝使全部 prune deferred；partial scope 只门禁请求模块的 hook plan。
- 定义供后续 CLI 消费的确认 callback 与执行结果事实。

明确不做：

- 不接入公开 CLI，不实现 backup、force replacement、hook 执行、managed/rendered 或 M1 `--adopt`。
- 不改变 state v1 持久格式、planner 的 P1/P2/P3 ownership 规则或规范文档。
- 不引入第三方 filesystem/transaction/rollback 依赖。

## Contract and Context

- `docs/04-cli-spec.md` §2–§4.4：apply 顺序、whole-module 确认、partial scope 与退出语义。
- `docs/05-apply-engine.md` §1–§7/§10：P1/P2/P3、收敛门控、Precondition、单次 state 提交和恢复边界。
- `docs/08-testing.md`：mutation 测试使用隔离真实文件系统，并覆盖零写入与重复收敛。
- `docs/09-roadmap.md` M1：当前只交付 link/scaffold/prune；backup/force、公开 apply 和 hooks 分属后续节点。

基线 `f7da6a63d76103cabcaaf329a878018dbbb333f8` 已有 `planner.PlanPrune` 作为 P1/P2/P3
和 whole-module groups 的单一真相源，`executor.ExecuteFile` 复核 file Precondition，
`state.TransitionEntries` 只支持 upsert，`apply.Run` 只执行 files 并拒绝 active prune/hook run。
本分支只消费 canonical plan，不重算 ownership 或 scope。

## Progress

- [x] 2026-07-20：确认 worktree、branch、clean baseline，阅读规范、既有实现、测试与 completed ExecPlans。
- [ ] Milestone 1：以测试先行扩展 mixed state transition。
- [ ] Milestone 2：以测试先行实现 canonical prune executor。
- [ ] Milestone 3：以测试先行接入 runner 阶段、确认与 run_once 零写入门禁。
- [ ] 运行窄测、完整 diff check、隔离 cache `make check`，保持计划 active 等待独立复核。

## Milestones

### Milestone 1：mixed state transition

扩展 `internal/state/transition.go` 的 transition 输入，使一个候选 Snapshot 能同时原子应用 file
upsert 与 prune delete。重复 key、upsert/delete 冲突、删除不存在 key 等模糊输入必须失败；
未涉及 entries 和 run_once 保持不变。

Concrete steps：

    go test ./internal/state -run 'TestTransition'

验收：mixed transition 产生可编码 Snapshot；失败不产生候选 state；空 transition 不请求 Store。

Commit 边界：

    feat(state): 支持 apply 混合状态迁移

### Milestone 2：P1/P2/P3 prune executor

在 `internal/executor` 增加只消费 `planner.PruneAction` 的执行入口。P2 在 target commit 前复核
control-plane、resolution 和 leaf Precondition，只删除仍 owned 的 symlink；P1/P3 只形成 delete
effect 且绝不触碰 target。deferred、畸形、目录/特殊对象和 Precondition 失配 fail closed。

Concrete steps：

    go test ./internal/executor -run 'TestExecutePrune'

验收：真实文件系统证明 P1/P3 零 target mutation、P2 只删精确 owned link、失配保留 target/state。

Commit 边界：

    feat(executor): 执行 canonical prune 动作

### Milestone 3：runner 编排与确认门禁

扩展 `internal/apply`：全量预检 scoped hooks（run 与 skip 均拒绝），执行 file 后仅在完全收敛时
调用 whole-module 确认，再执行 active prune；确认不可得或拒绝、file error/Precondition 失配均将
全部 prune 留作 deferred。所有成功 effect 汇成一次 mixed state transition 和一次 Store。

Concrete steps：

    go test ./internal/apply -run 'TestRun'

验收：files→confirm→prune→single Store 顺序可观察；部分成功可持久化；确认拒绝/deferred 无 prune
mutation；scope 内 hook 在任何 file/prune/state mutation 前拒绝；重跑收敛。

Commit 边界：

    feat(apply): 编排确认与 prune 阶段

## Validation and Acceptance

在 `/private/tmp/dot-m1-cp5-prune` 运行：

    go test ./internal/state ./internal/executor ./internal/apply
    git diff f7da6a63d76103cabcaaf329a878018dbbb333f8...HEAD --check
    make check

成功要求：全部命令退出 0；真实 target/state fixture 位于 `t.TempDir`；不读取或写入真实 HOME、
modules、state、backup；完整 branch 无未解释 diff 或 untracked。远端 macOS/Linux CI 留待
Checkpoint integration 后验收。

## Surprises & Discoveries

暂无。

## Decision Log

- Decision: prune executor 只执行 planner canonical action，不接收原始 orphan/state 并重算 P1/P2/P3。
  Rationale: ownership、Precondition 与 deferred 已由 planner 统一表达，避免第二真相源。
- Decision: run_once 硬门禁在任何 file executor/confirmation/prune/state commit 前检查整个 scoped hook slice。
  Rationale: CP5 明确要求 run 与 skip 都不得被静默忽略，partial scope 已由 PlanLoadedApply 缩小。

## Outcomes and Handoff

尚未完成；保持 active，待实现、验证和独立复核。
