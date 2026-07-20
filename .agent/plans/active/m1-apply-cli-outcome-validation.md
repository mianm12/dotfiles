# fix/m1-apply-cli-outcome-validation：拒绝提升静态 deferred prune

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和
`Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成后，apply CLI 会在生成任何 stdout 前验证 planner 与 runner 的逐项结果单调性：planner
已经标记 `Deferred=true` 的 prune 只能保持 `ActionDeferred`，runner 若把它报告为 succeeded、
conflict 或 failed，命令必须 fail closed 并最终退出 1。合法的运行期非 deferred prune 结果、
具体 conflict target、退出优先级以及既有 apply/dry-run 行为保持不变。

## Scope / Non-goals

范围内：

- 在 `internal/cli` 的 apply outcome validator 建立静态 prune plan 到 runtime outcome 的一致性门禁。
- 用表驱动回归覆盖 deferred+succeeded 原始缺口、其余非法提升以及合法 deferred。
- 证明矛盾结果在 stdout 产生前失败，错误走 stderr，最终退出码为 1。

明确不做：

- 不改变公开规范、state、ownership、planner 或 runner 的正常生成逻辑。
- 不改变非 deferred prune 的 succeeded/conflict/deferred/failed 语义、输出映射或退出优先级。
- 不扩大 apply 功能，不引入依赖，不修改真实私人数据。

## Contract and Context

- `docs/04-cli-spec.md` §3/§4.1/§5：运行错误优先退出 1，deferred prune 如实展示，verdict 前应完成必要输出验证。
- `docs/05-apply-engine.md` §3/§4/§7：planner 的 deferred prune 不执行 mutation；逐动作结果必须与计划处置一致。
- `docs/08-testing.md`：真实文件系统和 mutation 测试必须隔离；本修复使用既有合成 fixture。
- `docs/09-roadmap.md` M1：本分支只修正 CP5 apply CLI 的跨组件结果验证。

基线是 `feat/apply-cli@bf021c9947458da7d8667e3d6247efb36eedb53f`。当前
`internal/cli/plan.go:validateApplyOutcomes` 验证 outcome identity、coverage、状态枚举和聚合摘要，
却没有将 `result.Plan.Prune().Actions()[index].Deferred` 与对应 runtime status 关联。因此矛盾的
`Deferred=true` + 非 deferred runtime status 可通过验证并被 `projectApplyPlanWithOutcomes` 发布为
矛盾 verdict。`runInjectedApply` 已证明 projection 验证发生在 stdout 写入前，
适合作为 fail-closed 回归入口。

## Progress

- [x] 2026-07-20：确认 worktree/top-level `/private/tmp/dot-m1-cp5-cli-outcome-fix`、branch
  `fix/m1-apply-cli-outcome-validation`、clean base `bf021c9`。
- [x] 2026-07-20：提交本 active ExecPlan 起点（`ecf8a56`）。
- [x] 2026-07-20：表驱动红测证明 succeeded/conflict 会发布 stdout，failed 虽 exit 1 也会先发布
  stdout；`365501d` 增加最小 validator 门禁，窄测通过。
- [x] 2026-07-20：count=5、CLI race、branch diff check 与隔离 cache `make check` 全部通过；
  计划保持 active，等待独立完整复核。
- [x] 2026-07-20：Round 1 独立 review 确认 P2 测试验收缺口：canonical static-deferred
  result 未覆盖 validator 的 status、identity、coverage 与 count 拒绝路径；`7b6ee2c` 以新测试
  commit 补齐矩阵，未修改实现。
- [x] 2026-07-20：Round 1 测试修复后 count=5、CLI race、branch diff check 与隔离 cache
  `make check` 全部通过；保持 active 等待 Round 2 完整复核。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定授权后的独立 fix scope、基线、行为边界与验证方法。

Commit 边界：

    docs(plan): 建立 apply outcome 验证修复计划

### Milestone 2：测试先行并封闭静态 deferred 结果

先在 `internal/cli/apply_test.go` 构造一个包含 planner 静态 deferred prune 的合法计划，以表驱动方式
注入 succeeded、conflict、failed 和 deferred outcome。前三者必须 stdout 为空、stderr 报告不一致、
exit 1；合法 deferred 必须继续展示具体 file conflict 与 deferred prune，并按优先级保持 exit 3。
确认新增测试在现有实现上暴露 succeeded 缺口后，
在 `validateApplyOutcomes` 增加单一一致性检查，不改变 projection 或 runner。

Commit 边界：

    fix(cli): 拒绝提升静态 deferred prune

### Milestone 3：验证并记录交接证据

运行窄测试、重复和 race 检查、branch diff check 与隔离 cache `make check`；更新 living sections，
形成只含计划证据的语义 commit。计划保持 active，交给未参与实现的 reviewer 完整复核。

Commit 边界：

    docs(plan): 记录 apply outcome 修复证据

## Validation and Acceptance

在 `/private/tmp/dot-m1-cp5-cli-outcome-fix` 运行：

    go test ./internal/cli
    go test -count=5 ./internal/cli
    go test -race ./internal/cli
    git diff bf021c9947458da7d8667e3d6247efb36eedb53f...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp5-cli-outcome-fix-check/dot

成功要求：全部命令退出 0；完整 branch 只含本 active plan、validator 最小修复与对应测试；worktree
clean。远端 macOS/Linux CI 未运行，留待 Checkpoint integration 验收。

2026-07-20 本地证据：定向回归和完整 `go test ./internal/cli`、`go test -count=5
./internal/cli`、`go test -race ./internal/cli`、`git diff bf021c9...HEAD --check` 均退出 0。
隔离 `GOCACHE`、`GOLANGCI_LINT_CACHE` 和 binary 输出的 `make check` 通过，包含 tidy/format、
0 lint issues、全仓 race、build 与真实仓库 manifest gate。

Round 1 review 测试修复后，canonical 两项 static-deferred result 逐项覆盖 unknown status、duplicate
导致的 coverage gap、missing/extra count、错误 target、负 index 与越界 index；每项同时由
`projectApplyResult` 和注入 CLI 入口证明 fail closed、stdout 为空且 exit 1。合法 deferred 与原
succeeded/conflict/failed 单调性矩阵继续通过。再次运行 count=5、CLI race、branch diff check 与
隔离 cache `make check`，全部通过（0 lint issues、全仓 race、build、manifest gate）。

## Safety, Authorization, and Recovery

用户已授权本独立 fix Milestone 的 branch/worktree、ExecPlan、修改、stage、commit 与验证。测试复用
完全隔离的合成 fixture，不读取或写入真实 modules、machine config、state、backup、`.env` 或主力
HOME。失败使用新的 fix commit，不 amend/rebase/reset；不切换或合并其他 branch/main。

## Surprises & Discoveries

- Observation: static deferred+succeeded 原缺口仍会被 planner 的 file conflict 推高到 exit 3，
  但这不减轻协议矛盾：它会把未执行的 prune 伪装成成功 outcome，并允许发布可信 stdout。
  Evidence: 新回归在修复前得到具体 file conflict、deferred prune 文本和 exit 3；validator 门禁后
  stdout 为空并 exit 1。
- Observation: static deferred+failed 原本已有最终 exit 1，但仍在 runtime error 前输出 plan verdict。
  Evidence: 红测捕获 stdout 已包含 conflict/deferred 行，证明仅靠错误优先级不能满足 fail closed。
- Observation: 单项 static-deferred fixture 足以证明状态提升，却无法构造等长 duplicate 后留下的
  coverage gap；Round 1 回归改用同一真实 plan 的两个 static-deferred prune，因而能同时固定
  identity、coverage、count 与状态单调性，且仍保留合法输出顺序。

## Decision Log

- Decision: 在 CLI validator 检查 planner 静态 deferred 到 runtime outcome 的单调性，不修改 runner。
  Rationale: planner 已决定该动作不可执行；CLI 在发布输出前负责拒绝跨组件矛盾结果，而正常 runner
  已保持 deferred，无需改变执行路径。

## Outcomes and Handoff

实现与 Round 1 测试验收修复、本地门禁已完成。branch 相对 `bf021c9` 只有本 active ExecPlan、
一个 validator 单调性门禁和对应 fail-closed 矩阵；保持 active 等待未参与实现的 reviewer Round 2
完整复核。远端 macOS/Linux CI 未运行。
