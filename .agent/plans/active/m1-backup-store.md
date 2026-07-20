# feat/backup-store：交付可持久化校验的独立备份存储

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

为 CP5 的 force replace 提供一个独立、只依赖标准库的 backup store。调用方能够为一次运行
建立唯一 batch，把计划时观测到的普通文件或 symlink 保存到精确、可报告且永不覆盖的路径；
只有普通文件内容摘要、九个普通权限位及落盘步骤全部成立，或 symlink 原始 link text 精确
匹配时，保存才报告成功。

## Scope / Non-goals

范围内：

- 在新的 `internal/backup` package 中建立 0700 root、唯一 0700 batch 与 0700 parents。
- 保存 regular bytes 与九个普通权限位，并核对调用方提供的 `sha256:` 计划摘要。
- 保存 symlink 原始 link text，不跟随链接；拒绝目录和特殊对象。
- 以排他创建保证精确 backup path 不覆盖，并在成功返回后保留备份。

明确不做：

- 不连接 planner、executor、apply、state 或 CLI，不改变任何共享 contract。
- 不实现 ownership、Precond、target replace、恢复、清理或通用 filesystem transaction。
- 不读取或修改真实 HOME、modules、machine config、state 或 backup。

## Contract and Context

- `docs/02-architecture.md` §2：backup 位于私有 0700 目录，每个 batch 路径唯一，普通文件保留 mode。
- `docs/05-apply-engine.md` §6–§7：regular 备份必须逐字节完整、保留九个普通权限位、摘要等于计划
  观测；symlink 保存原始 link text；成功备份具备文件级持久性且不自动删除；目录和特殊对象拒绝。
- `docs/08-testing.md` §2–§4：测试使用隔离真实文件系统 fixture，覆盖不覆盖、内容、mode、link text
  与失败边界。
- `docs/09-roadmap.md` §3：本切片只建立 CP5 backup 通用机制，后续 force milestone 负责消费并表达
  ownership、Precond 与替换契约。

基线 `f7da6a63d76103cabcaaf329a878018dbbb333f8` 已有 `internal/storage` 的私有权限常量，尚无
backup store。新 package 接收绝对 backup root、受约束相对 backup path 和绝对 source；它不导入
planner 或解释 target 身份，因而不会提前固化跨组件动作契约。

## Progress

- [x] 2026-07-20：确认 worktree、branch、base 与 clean 状态，读取相关规范和既有实现。
- [x] 2026-07-20：以隔离真实文件系统测试定义 batch、regular、symlink、拒绝与不覆盖行为；
  `f11394d` 与 `541a62e` 分别交付私有 batch 和经校验的对象保存。
- [x] 2026-07-20：`go test ./internal/backup` 与 package-scoped `golangci-lint run` 通过。
- [x] 2026-07-20：完整 diff check 与隔离 cache 的 `make check` 通过；保持计划 active 等待独立复核。
- [x] 2026-07-20：第一轮独立复核确认 P1：首次创建 backup root 的缺失祖先时，仅同步 root 内
  batch，未持久化 root 及其新增祖先的目录项链；已增加同步顺序/失败注入测试并修复。
- [x] 2026-07-20：修复提交 `ae2a9e1` 后，窄测试、完整 diff check 与隔离 cache 的
  `make check` 重新通过；等待修复后的完整独立复审。

## Milestones

### Milestone 1：建立唯一私有 batch 与路径边界

在 `internal/backup` 增加 batch 构造和测试。backup root、batch 与按需父目录都收敛为 0700；
batch 名由 UTC RFC3339Nano 时间与随机后缀组成并排他创建。保存路径必须是 clean、非空且不能
逃逸 batch；最终路径已存在时拒绝，不覆盖。

Concrete steps：

    在 repo root 运行：go test ./internal/backup -run 'TestNewBatch|TestBatchPath'
    预期：私有权限、唯一性、路径拒绝与不覆盖测试通过。

Commit 边界：

    feat(backup): 建立唯一私有备份批次

### Milestone 2：保存并校验 regular 与 symlink

先增加真实 regular/symlink fixture 测试，再实现保存。regular 以排他目标文件流式复制并计算
SHA-256，核对计划摘要和权限，依次完成写入、chmod、sync、close 后才成功；symlink 使用
`Lstat`/`Readlink` 并在精确 link text 匹配后创建同文本链接。目录、FIFO 等特殊对象拒绝。

Concrete steps：

    在 repo root 运行：go test ./internal/backup
    预期：内容、mode、摘要错配、raw link text、目录/特殊拒绝和成功保留全部通过。

Commit 边界：

    feat(backup): 持久保存并校验文件对象

## Validation and Acceptance

在 `/private/tmp/dot-m1-cp5-backup` 运行：

    go test ./internal/backup
    git diff f7da6a63d76103cabcaaf329a878018dbbb333f8...HEAD --check
    GOCACHE=<隔离临时目录> GOLANGCI_LINT_CACHE=<隔离临时目录> make check

成功判据是命令均退出 0，`git status --short` 为空；所有 mutation fixture 位于 `t.TempDir()`。
当前平台原生验证 macOS；远端 Linux CI 不在此 Milestone 内实际运行，交接时必须标记待验收。

## Surprises & Discoveries

- 2026-07-20：普通文件的文件级持久性不仅需要 destination file `Sync`，目录项和新建父目录也
  需要沿 batch 路径同步；实现因此在成功前依次同步 destination parent 至 batch，batch 创建时
  同步 backup root。该处理仍完全位于 store 内部，不改变调用方 contract。
- 2026-07-20：第一轮 review 发现上述实现只覆盖 batch entry，首次 `MkdirAll` 创建的 backup
  root/祖先 entry 仍可能在崩溃后丢失。证据是原 `NewBatch` 成功路径仅调用
  `syncDirectory(cleanRoot)`；修复后从 batch 自底向上同步至创建前最近既有目录。

## Decision Log

- 2026-07-20：backup package 只接受受校验相对保存路径，不从 HOME target 自行推导布局。理由是
  精确报告路径属于 store 职责，而 target identity、scope 与展示映射属于后续共享 contract。
- 2026-07-20：使用标准库 SHA-256 并要求 `sha256:<64 lowercase hex>`。该格式与现有 planner/state
  证据一致，也满足规范要求的稳定摘要，不引入新依赖。
- 2026-07-20：`NewBatch` 在创建前记录最近既有目录，并在 batch 创建后按
  `batch → root → 新增祖先 → 最近既有目录` 顺序逐目录同步；任一步 open/sync/close 失败均不
  返回可用 batch。理由是只有该链完整成立，首次创建 backup root 时也能证明成功备份路径持久。

## Outcomes and Handoff

实现与首轮 review fix 已完成，计划保持 active 等待完整独立复审与 coordinator 执行 closure。分支相对
`f7da6a63d76103cabcaaf329a878018dbbb333f8` 新增纯标准库 `internal/backup`：唯一 0700 batch、
受约束相对路径、0700 parents、regular bytes/mode/SHA-256 计划证据、raw symlink text、排他
不覆盖、持久化同步及目录/特殊对象拒绝。

验证证据：

- `go test ./internal/backup`：通过。
- `golangci-lint run ./internal/backup/...`（隔离 cache）：0 issues。
- `git diff f7da6a63d76103cabcaaf329a878018dbbb333f8...HEAD --check`：通过。
- `make check`（隔离 `GOCACHE`、`GOLANGCI_LINT_CACHE`）：通过，含 race tests、build 与隔离
  `doctor --manifest-only`。
- review fix `ae2a9e1` 后再次运行上述窄测试、diff check 与完整 `make check`：通过；同步顺序和
  中途失败不返回成功由故障注入测试固定。

当前仅在 Darwin/arm64 原生验证；远端 macOS/Linux CI 待 Checkpoint 验收。未连接
planner/executor/state/apply/CLI，也未实现 ownership、Precond 或 replace。
