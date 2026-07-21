# fix/m1-apply-acceptance：闭合 CP5 最终竞态与确认 IO 分类

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，L1 create-link 在最终 symlink 提交遇到 EEXIST 时会作为纯 Precondition mismatch
贯穿 executor→runner→CLI，公开展示具体 `CONFLICT` 并退出 3，同时保留 target/state、延迟 prune。
whole-module 确认读取 EOF 时若 terminal Close 失败，该 IO error 不会被普通确认拒绝吞掉，命令安全
延迟 prune 并按错误优先级退出 1。

## Scope / Non-goals

范围内：

- `internal/executor/link.go` 将 L1 最终 symlink EEXIST 精确分类为纯 `ErrPreconditionMismatch`，不让
  `fs.ErrExist` 成为错误树 child。
- executor 测试固定 pure classifier；apply/CLI 回归固定 runtime outcome、公开 CONFLICT/exit 3、
  target/state 保留和 prune deferred。
- `internal/cli/plan.go` 在 EOF 确认分支合并 warning write 与 terminal Close error；任一 IO 错误
  都保持 error/exit 1，同时确认不被接受、prune 不执行。
- 注入 ReadCloser 覆盖 EOF + close sentinel；必要时覆盖 EOF warning 写失败的组合。

明确不做：

- 不改变 planner 决策、ownership、state v1、backup/prune 格式、公开 flag 或正常确认文案。
- 不扩大恶意并发威胁模型，不消除规范已接受的不可消除竞态窗口。
- 不引入依赖，不实现 hooks、managed/rendered、M2/M3 或其他 Checkpoint。

## Contract and Context

- `docs/04-cli-spec.md` §3/§4.2：运行错误优先退出 1；unresolved conflict 退出 3；确认拒绝仅在无
  运行错误时作为 deferred work 退出 2。
- `docs/05-apply-engine.md` §6：最终 Precondition 失配必须降级 conflict，target/state 不动且 prune
  延迟；create-link 提交时 missing 必须仍 missing。
- `docs/08-testing.md` §1–§3：提交点竞态、失败安全、target/state 保留与真实文件系统结果必须有回归。
- coordinator `.agent/plans/active/m1-cp5-apply.md`：CP5 Acceptance 已验证两项 P2，授权本独立
  acceptance-fix Milestone，不改变 Checkpoint scope。

基线为 clean `main@e6d61899dbc0c70330a62ce186fa9a616c00e92e`。现有
`createMissingLink` 将最终 `operations.symlink` 的 EEXIST 包装为 `ErrPrecondition` 且保留
`fs.ErrExist` child；`IsPurePreconditionMismatch` 因混入该 child 返回 false，runner/CLI 错映射为
runtime error 1。现有 confirmation callback 在 EOF 分支写 warning 后直接返回 `writeErr`，忽略
此前取得的 `closeErr`，可把真实 terminal Close IO 错误降成确认拒绝/exit 2。

## Progress

- [x] 2026-07-21：确认 worktree/top-level `/private/tmp/dot-m1-cp5-acceptance-fix`、branch
  `fix/m1-apply-acceptance`、clean base `e6d6189`；阅读计划规则、coordinator scope、completed CP5
  plans、适用规范与当前实现/测试。
- [x] 2026-07-21：提交 active ExecPlan 起点 `6dd898e`。
- [x] 2026-07-21：测试先行修复 L1 EEXIST pure mismatch 及 runner/CLI 端到端分类，提交
  `42f7587`。
- [x] 2026-07-21：测试先行修复 EOF confirmation Close IO error 聚合，提交 `9bea7ce`。
- [x] 2026-07-21：窄测、5 次重复、race、完整 diff check 与隔离 cache `make check` 均通过；
  保持 active 等待独立复核。

## Milestones

### Milestone 1：L1 最终 EEXIST 进入 pure mismatch

先以注入 `fileOperations.symlink` 在 L1 最终提交返回 `fs.ErrExist`，断言 failure effect、零 target
commit、错误属于 `ErrPreconditionMismatch` 且 `IsPurePreconditionMismatch=true`。再补 runner/CLI
集成，使同一最终竞态映射 `ActionConflict`、stdout 列出具体 CONFLICT、exit 3，并证明 target/state
保留、全部 prune deferred。实现只返回 executor 已有的 precise mismatch sentinel，不把 EEXIST
作为 child 包入错误树。

Commit 边界：

    fix(executor): 将 L1 EEXIST 归类前提失配

### Milestone 2：EOF 确认保留 Close IO error

先注入一个读取 EOF、Close 返回 sentinel 的 ReadCloser，固定确认未接受、prune 不执行、exit 1；
若 warning writer 同时失败，两个错误都必须由 callback 返回。最小实现让 EOF warning writeErr 与
closeErr 通过 `errors.Join` 聚合，不改变纯 EOF 的拒绝/exit 2。

Commit 边界：

    fix(cli): 保留确认 EOF 关闭错误

## Validation and Acceptance

在 `/private/tmp/dot-m1-cp5-acceptance-fix` 运行：

    go test ./internal/executor ./internal/apply ./internal/cli
    go test -count=5 ./internal/executor ./internal/apply ./internal/cli
    go test -race ./internal/executor ./internal/apply ./internal/cli
    git diff e6d61899dbc0c70330a62ce186fa9a616c00e92e...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp5-acceptance-fix-check/dot

成功要求：全部命令退出 0；完整 branch 只包含本 active plan、两项最小分类修复与对应测试；worktree
clean。测试只使用 `t.TempDir`/注入 IO，不读取或写入真实 modules、machine config、state、backup、
`.env` 或主力 HOME。远端 macOS/Linux CI 未运行，留待 Checkpoint Acceptance。

2026-07-21 在提交 `9bea7ce` 上的本地证据：

- `go test ./internal/executor ./internal/apply ./internal/cli` 通过。
- `go test -count=5 ./internal/executor ./internal/apply ./internal/cli` 通过。
- `go test -race ./internal/executor ./internal/apply ./internal/cli` 通过。
- `git diff e6d61899dbc0c70330a62ce186fa9a616c00e92e...HEAD --check` 通过。
- 使用独立 `GOCACHE`、`GOLANGCI_LINT_CACHE` 和 `/private/tmp` binary 的 `make check` 通过；
  包含 tidy diff、format、lint、全仓 race、build 与 synthetic manifest check。

## Safety, Authorization, and Recovery

用户已授权本 branch/worktree 的 active plan、范围内修改、stage、commit 和验证。失败使用新 fix
commit，不 amend/rebase/reset/cherry-pick/squash；不切换或合并 main/其他 branch，不读取私人数据。

## Surprises & Discoveries

- L1 最终 symlink 的 EEXIST 虽已包为 `ErrPrecondition`，但保留 `fs.ErrExist` child 会让
  `IsPurePreconditionMismatch` 正确地拒绝把混合错误降级；修复点应是 executor 错误树，而非 runner
  增加例外。
- EOF warning 写成功不代表确认路径无 IO 错误；ReadCloser 的 deferred Close 已执行，但原返回分支
  丢弃了 `closeErr`，使 CLI 错误优先级从 1 降为 2。

## Decision Log

- Decision: L1 symlink EEXIST 直接返回 existing precise mismatch sentinel，不包装 EEXIST child。
  Rationale: EEXIST 已完整表达“missing 快照失效”；保留 OS error child 会破坏 pure classifier。
- Decision: confirmation EOF 仍表示默认拒绝，但其 warning write 和 terminal Close 是独立 IO 结果。
  Rationale: 拒绝语义不能吞掉运行错误；错误优先级必须保持 1 > 3 > 2 > 0。

## Outcomes and Handoff

实现与本地门禁已完成，保持 active 等待独立 reviewer 对 base `e6d6189...` 到本 branch 的完整实质
diff 复核。当前交付 commits：`6dd898e`（计划）、`42f7587`（L1 pure mismatch）、`9bea7ce`
（EOF Close error）。本地 Linux/macOS 远端 CI 未实际运行；Checkpoint 最终验收仍应标记远端待验收。
