# feat/force-replace：安全备份并原子替换冲突 target

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，内部 apply runner 能消费 planner 已形成的 canonical `FileBackupReplace`：在最终
Precondition 成立时先持久备份冲突 regular 或 symlink，再次完整复核后以原子 rename 发布期望
link。备份失败时 target 零替换；备份成功后即使 replace、cleanup 或 state Store 失败，也通过
file/apply result 保留并报告精确路径。显式 force 的 S2 scaffold 重建则不备份，只在 target
仍缺失时发布。

## Scope / Non-goals

范围内：

- executor 执行 regular/symlink `FileBackupReplace`，复用 `internal/backup` 与现有精确
  Precondition 分类。
- executor 执行 `FileReasonScaffoldRebuild`，严格要求 S2 missing leaf 且不建立 backup。
- runner 为含 backup-replace 的运行建立唯一 batch，传入 executor，并汇总成功备份精确路径。
- 固定 backup→再次完整 Precondition→原子 replace、部分成功 state、prune deferred 与重跑收敛。

明确不做：

- 不接入公开 CLI，不执行 hook，不实现 managed/rendered 或 M1 `--adopt`。
- 不改变 planner ownership、canonical action、state v1 格式、prune 契约或规范。
- 不引入依赖、通用 rollback、filesystem transaction 或自动清理成功备份。

## Contract and Context

- `docs/05-apply-engine.md` §3.2/§6–§7：force 只替换 planner 允许的 regular/symlink；备份必须
  完整可用，target 提交前重做完整 Precondition；目录和特殊对象仍拒绝；S2 只在仍缺失时重建。
- `docs/05-apply-engine.md` §3.3/§10：file 未收敛时 prune 全部 deferred；已成功动作的 state
  effect 可与后续失败形成部分成功提交，重跑通过 L2/S1b 收敛。
- `docs/08-testing.md` §3.2：真实文件系统覆盖内容/mode/link text、失败边界、零替换与崩溃恢复。
- `docs/09-roadmap.md` M1/CP5：本分支只交付 force replace 内部执行，公开 apply 留给后续节点。

基线 `0499de9fa887b57c4e86efa8be99ca656984bb7e` 已有 canonical planner action、精确
`ErrPreconditionMismatch`、持久化 `internal/backup.Batch`、file/prune runner 与 mixed state
transition。当前 executor 对 `FileBackupReplace` 和 S2 scaffold rebuild 仍 fail closed，runner
也尚未建立 backup batch 或报告路径。

## Progress

- [x] 2026-07-20：确认 worktree、branch、base 与 clean 状态，读取规范、相关实现和 completed plans。
- [ ] 以测试先行交付 executor backup-replace 与 S2 rebuild。
- [ ] 以测试先行连接 runner batch、路径报告、部分成功和 deferred/retry 行为。
- [ ] 运行窄测试、branch diff check 与隔离 cache `make check`，保持计划 active 等待独立复核。

## Milestones

### Milestone 1：executor 持久备份后原子替换

在 `internal/executor` 增加真实 regular/symlink fixture 和失败注入测试，再让 link executor 接受
canonical `FileBackupReplace`。首次完整 Precondition 通过后，按 leaf evidence 调用 batch 保存；
备份成功后准备完整临时 link，再次完整复核并 rename。结果携带精确 backup path，且一旦备份
成功就不会因后续错误丢失。目录、特殊对象和畸形 action 在 mutation 前拒绝。

Concrete steps：

    go test ./internal/executor -run 'TestExecuteLink_BackupReplace|TestValidateFileAction_BackupReplace'

Commit 边界：

    feat(executor): 安全备份并替换 link target

### Milestone 2：显式重建已删除 scaffold

先增加 S2 missing、target 重新出现、祖先/leaf 变化和失败恢复测试，再复用 scaffold 的完整临时
文件准备与排他发布路径。只有 `FileReasonScaffoldRebuild` + `LeafMissing` 可执行，不创建 backup；
发布后的 state Store 失败由现有 S1b 自动收养路径恢复。

Concrete steps：

    go test ./internal/executor -run 'TestExecuteScaffold_ForceRebuild|TestValidateFileAction_ScaffoldRebuild'

Commit 边界：

    feat(executor): 显式重建已删除 scaffold

### Milestone 3：runner 编排 backup batch 与恢复事实

扩展 `internal/apply` operations：只在 scoped executable files 含 backup-replace 时建立一次 batch，
将 capability 传给 executor，并把每个成功备份的精确路径累计到 `Result`。测试覆盖 backup 失败
零替换、replace/cleanup/state Store 失败仍报告、先前成功 state 保留、prune deferred，以及重跑
L2/S1b 收敛。

Concrete steps：

    go test ./internal/apply -run 'TestRun_.*(Backup|Force|Scaffold)'

Commit 边界：

    feat(apply): 编排 force 备份与恢复报告

## Validation and Acceptance

在 `/private/tmp/dot-m1-cp5-force` 运行：

    go test ./internal/backup ./internal/executor ./internal/apply
    git diff 0499de9fa887b57c4e86efa8be99ca656984bb7e...HEAD --check
    GOCACHE=<隔离临时目录> GOLANGCI_LINT_CACHE=<隔离临时目录> make check

成功要求：命令全部退出 0；所有 filesystem mutation 位于 `t.TempDir()`；backup 路径保持存在；
工作树除 active ExecPlan 的进度更新外无未解释内容。当前环境只原生验证 Darwin/arm64，远端
macOS/Linux 留待 Checkpoint Acceptance。

## Surprises & Discoveries

暂无。

## Decision Log

- 2026-07-20：executor 接收已初始化 `*backup.Batch` capability，而不自行从 HOME 或 target 推导
  backup root/batch。理由是 runner 拥有一次 apply 生命周期，executor 只消费 canonical action。
- 2026-07-20：精确 backup relative path 由稳定 target key 构成并保持在 batch 内；最终绝对路径
  从 backup store 返回并原样报告，不增加持久化 state 字段。

## Outcomes and Handoff

尚未完成；保持 active，完成实现和本地门禁后等待未参与实现的 reviewer。
