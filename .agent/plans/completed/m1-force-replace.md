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
- [x] 2026-07-20：以测试先行交付 executor backup-replace（`9eb17fd`）与 S2 rebuild
  （`154110f`）；`82b8cbb` 补充最终 leaf/source 失效的精确分类回归。
- [x] 2026-07-20：以测试先行连接 runner batch、精确路径报告、失败保留和重跑收敛
  （`f66371e`）。
- [x] 2026-07-20：backup/executor/apply 窄测试、branch diff check 与隔离 cache `make check`
  通过；保持计划 active 等待独立复核。
- [x] 2026-07-20：第一轮独立 review 确认两个 P2：backup preparation 的 evidence/IO 错误未
  精确分类，S2 publish `EEXIST` 只属于广义 Precondition。`6e6c3f7`、`b1e1df2` 和
  `004b09c` 修复 pure/mixed error tree；`08ce774` 固定 post-commit cleanup 与 state Store
  失败仍报告并保留精确 backup path。
- [x] 2026-07-20：review fixes 后窄测试、完整 branch diff check 与隔离 cache `make check`
  重新通过；等待完整复审。
- [x] 2026-07-20：Round 2 完整复审确认功能与安全无 blocking，提出一个有效 P3：`Run`
  注释仍声称不执行 backup。`7b0a712` 校正为 runner 实际执行 force backup、但不连接 CLI 或
  执行 hooks；窄测试和完整门禁重新通过，等待 Round 3 完整复审。
- [x] 2026-07-20：Round 3 未参与实现的 reviewer 完整复审结论为 GO、无 findings；main 仍精确
  等于有效 base `0499de9`。最终 freshness 窄测试、base...HEAD diff check 与隔离 cache
  `make check` 全部通过，计划完成并迁移到 completed。

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

- 2026-07-20：`ExecuteFile` 已被大量非-force 测试与调用方消费；为避免让所有安全的非备份动作
  接收无意义 capability，保留原入口并新增 `ExecuteFileWithBackup`。runner 只对 canonical
  `FileBackupReplace` 分派新入口。
- 2026-07-20：backup 后 source 的 `Lstat` 失败仍是 runtime IO，不是纯 evidence mismatch；target
  内容/hash 变化则是精确 mismatch。故障注入测试固定两类错误不会混淆，且都保留 backup path。
- 2026-07-20：backup store 原先把摘要、mode、inode 和 raw link text 的明确变化与 IO 使用同一
  普通 error；runner 因而无法把准备期间的确定失配安全降级 conflict。store 现在提供 sentinel
  与整棵 error tree 分类；混入 destination cleanup IO 时明确不是 pure mismatch。
- 2026-07-20：`hardLink` 的 `EEXIST` 是提交点明确出现新 leaf 的证据，但 cleanup 失败会通过
  `errors.Join` 混入运行错误；executor 的递归 pure classifier 能精确区分这两种结果。
- 2026-07-20：Round 2 复审发现 `Run` 的历史注释仍描述 CP4 职责，虽然不影响行为，但会误导
  后续 CLI milestone 对 backup 所有权的判断；已随实现事实校正，不改变任何 contract。

## Decision Log

- 2026-07-20：executor 接收已初始化 `*backup.Batch` capability，而不自行从 HOME 或 target 推导
  backup root/batch。理由是 runner 拥有一次 apply 生命周期，executor 只消费 canonical action。
- 2026-07-20：精确 backup relative path 由稳定 target key 构成并保持在 batch 内；最终绝对路径
  从 backup store 返回并原样报告，不增加持久化 state 字段。
- 2026-07-20：只把 `backup.IsPureEvidenceMismatch` 映射为 `ErrPreconditionMismatch`；open、copy、
  chmod、sync、cleanup 以及任何 mixed tree 保留 runtime error。理由是只有纯 evidence 失配才能
  安全成为 unresolved conflict，其他失败仍需显式暴露。

## Outcomes and Handoff

目标已完成并通过三轮独立复审。当前 branch 交付：

- regular/symlink canonical backup-replace，保存 bytes/mode 或 raw link text 后再次完整 Precondition
  并原子 rename；backup 失败零替换，成功路径不会因后续错误丢失。
- S2 scaffold 只在 target 仍 missing 时无备份重建。
- runner 每次 force apply 复用唯一 batch，通过 `Result.BackupPaths` 报告恢复事实；file 未收敛仍按
  既有契约延迟 prune，成功 state effect 仍能部分提交。

Round 1 reviewer 的两个 P2 已由 `6e6c3f7`、`b1e1df2`、`004b09c` 修复；regular/symlink
preparation evidence、mixed cleanup、S2 pure `EEXIST`/`EEXIST+cleanup`、真实 force
post-commit cleanup 与 state Store 失败均有回归测试。Round 2 的注释 P3 已由 `7b0a712`
修复；Round 3 完整复审 GO、无 findings。

最终 freshness 时 main 仍为有效 base `0499de9`。验证证据：

- `go test ./internal/apply ./internal/backup ./internal/executor -count=1`：通过。
- `git diff 0499de9...HEAD --check`：通过。
- 隔离 `GOCACHE`、`GOLANGCI_LINT_CACHE` 的 `make check`：通过，含 lint、race tests、build
  与隔离 manifest check。

仅在 Darwin/arm64 原生验证；本地验收通过，远端 macOS/Linux 待 Checkpoint Acceptance。
