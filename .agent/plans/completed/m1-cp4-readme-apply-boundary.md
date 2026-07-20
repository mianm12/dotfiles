# fix/m1-apply-core-acceptance：校正 README 的 apply 阶段边界

本 ExecPlan 是 living document。实施期间持续更新 `Progress`、`Surprises & Discoveries`、
`Decision Log` 和 `Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

修正 README 对当前实现阶段的失真描述。完成后，README 会明确区分“CP4 已交付内部
link/scaffold 安全执行内核”和“真实非 dry-run `dot apply` 尚未接入公开 CLI，仍在 runtime
读取前拒绝”，避免读者误以为 executor 尚不存在或 executor 一交付就自动公开 apply。

## Scope / Non-goals

范围内：

- 只更新 README 的当前实现状态说明，并保持 dry-run/status 的只读性质描述。
- 用现有 CLI 测试与代码位置核对非 dry-run apply 的拒绝边界。
- 完成独立 review、diff check 与完整门禁。

明确不做：

- 不连接或公开真实 `dot apply`，不改变 CLI 输出、退出码或 runtime 行为。
- 不修改规范、state 格式、ownership、Precondition、executor 或 planner contract。
- 不实现 backup、force、prune、hooks、managed/rendered 或其他 M2/M3 能力。

## Contract and Context

`docs/09-roadmap.md` 与 CP4 coordinator 规定，本 Checkpoint 只建立内部执行链路，不公开真实
apply。当前 `internal/executor` 和 `internal/apply` 已存在；CLI 对非 dry-run apply 的硬拒绝仍
发生在 runtime 读取之前。README 仍称“在 executor 交付前会明确拒绝”，把已交付的内部机制
和未交付的 CLI wiring 混为同一阶段，属于文档事实缺陷而非产品设计变更。

本 branch 是当前 Goal 已知归属的 `fix/m1-apply-core-acceptance`。它原 tip `f3e772e` 已合入
main；从 clean `main@8a0f8a1` 开始本轮前，按 freshness 规则以 `9d28523` 非重写同步 current
main。未 fetch/pull，不新增依赖。

## Progress

- [x] 2026-07-20：三路完整 CP4 Acceptance 完成；两路 GO，一路发现 README 阶段边界 P2，
  执行内核本身无新的 P0–P3 finding。
- [x] 2026-07-20：确认 acceptance branch 归属本 Goal 且已合入；创建专用 worktree，并以明确
  integration commit 同步 clean current main。
- [x] 2026-07-20：以 `9eac25a` 提交 README 边界修正；CLI/apply/executor 窄测、branch
  diff check 与隔离 cache/output 下的 `make check` 全部通过。
- [x] 2026-07-20：两名未参与修改的 reviewer 完成规范边界与 Go/测试完整复审，均 GO，
  无 P0–P3 finding；原 README P2 不再复现。
- [x] 2026-07-20：更新 Outcomes/Handoff，迁移计划并创建纯 plan-closure commit。

## Milestones

### Milestone 1：让 README 与当前实现一致

把“executor 交付前拒绝”改成两项可分别验证的事实：内部 link/scaffold 安全执行内核已交付；
非 dry-run apply 尚未接入公开 CLI，并在 runtime 读取前明确拒绝。不要把内部 API 描述成公开
承诺，也不要预告后续 Checkpoint 的具体 CLI 形态。

验证：

    go test ./internal/cli ./internal/apply ./internal/executor
    git diff main...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp4-acceptance-readme/dot

Commit：

    docs(readme): 校正 apply 执行阶段边界

## Validation and Acceptance

| 必须成立的性质 | 证据 | 状态 |
|---|---|---|
| README 不再声称 executor 尚未交付 | README 当前能力段落 | 通过 |
| 真实 apply 仍明确标为未公开且 runtime 前拒绝 | README + CLI 拒绝回归 | 通过 |
| 无产品行为、依赖或规范变化 | main...branch diff + `make check` | 通过 |
| 独立完整 review | 未参与实现的 reviewer | 两份 GO；无 P0–P3 finding |

## Safety, Authorization, and Recovery

本计划只修改受版本管理的 README 与自身生命周期文件，不读取或修改真实 modules、machine
config、state、backup、`.env` 或主力 HOME。用户当前 CP4 Goal 已授权必要 README、计划、
acceptance branch/worktree、commits、review 和 FF-only 本地 main 集成。不 amend、rebase、
cherry-pick、squash、reset 或 force。

## Interfaces and Dependencies

不新增依赖、接口或数据结构。README 只陈述由现有代码和测试已经证明的阶段边界。

## Surprises & Discoveries

- Observation: 三路 Acceptance 的产品实现审查没有发现新缺陷；唯一新 finding 是 README 把
  内部 executor 交付与公开 CLI wiring 混成一个条件。
  Impact: 最佳修复是校正当前事实，不创建代码 adapter、feature flag 或未来接口。
- Observation: 现有 `TestApply_RejectsMutationAndAdoptBeforeRuntime` 已用无效 config/manifest
  fixture 证明非 dry-run apply 在加载 runtime 输入前拒绝，并断言隔离树零 mutation。
  Impact: README 修复不需要新增仅验证文字的脆弱测试；复用行为测试并以独立 review 核对描述。
- Observation: 两名独立 reviewer 均确认新文案没有把内部 package 暗示为公开 API，也没有暗示
  CP4 Non-goals 已实现；完整实质 diff 只有 README 与本计划。
  Evidence: 两份 `main@8a0f8a1...866c702` 完整 branch review 均为 GO。

## Decision Log

- Decision: 在当前 CP4 收尾修复，而不延后到公开 apply 的后续 Checkpoint。
  Rationale: README 负责说明“已经实现什么”；保留失真描述会误导当前用户和后续维护者，修复
  收益明确且不改变任何产品契约。
  Date: 2026-07-20

## Outcomes and Handoff

本计划已完成，可以进入 freshness gate 与本地 main 集成：

- `9eac25a docs(readme): 校正 apply 执行阶段边界` 只修改 README 当前能力段落。
- `go test ./internal/cli ./internal/apply ./internal/executor`、`git diff main...HEAD --check`
  与 `make check BINARY=/private/tmp/dot-m1-cp4-acceptance-readme/dot` 均通过。
- 两名未参与修改的 reviewer 均给出 GO，无 P0–P3 finding；原 README P2 已关闭。
- 未改变 CLI 行为、规范、依赖、state 或内部接口。本地 Darwin/arm64 验收通过；远端
  macOS/Linux CI 未运行，仍待远端验收。
