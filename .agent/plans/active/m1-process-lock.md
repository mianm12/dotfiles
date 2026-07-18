# feat/process-lock：提供可复用的进程排他锁

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

本 Goal 为后续 mutation runtime 提供进程间非阻塞排他锁。调用方能够明确区分“另一
`dot` 进程正在运行”与锁文件 IO 失败，并把同一个显式 ownership guard 传给嵌套流程，
从而覆盖完整 mutation 周期而不会自锁或被内层提前释放。首次获取锁时统一建立权限为 0700
的 state root 和权限为 0600 的 lock 文件；未调用获取入口的只读流程不会产生任何写入。

## Scope / Non-goals

范围内：

- 在独立 package 中封装 `github.com/gofrs/flock` 的排他、非阻塞 `TryLock`/`Unlock`，返回可
  分类的 busy 与 IO 错误。
- 用显式 owner/guard 表达锁所有权；同路径嵌套复用，错误 owner 或不同路径明确拒绝。
- 建立后续 state store 可复用的窄 storage 权限边界，保证 state root 0700、lock 0600，且
  现存对象类型或权限异常不会被静默接受。
- 以真实 helper 子进程证明竞争、释放、异常退出恢复，并覆盖权限、错误分类和嵌套生命周期。
- 引入依赖、执行 `go mod tidy`、窄测、重复测试、双平台交叉编译与完整 `make check`。

明确不做：

- 不接入 runtime/preflight、CLI/Cobra 或任何公开命令，也不修改 `internal/state`、`internal/paths`、
  Makefile、CI 或 README。
- 不实现 state codec/store/loader、PID registry、daemon、shared lock、等待重试或恶意并发防护。
- 不让只读命令或 package 初始化自动创建 state root、lock 或其他文件。

## Contract and Context

- `docs/02-architecture.md` §2/§4–§5：mutation 与 `dot git` 在控制面校验后取得一次排他锁；
  read-only/dry-run 不取锁；嵌套 mutation 复用同一所有权；state root/lock 权限分别为 0700/0600。
- `docs/05-apply-engine.md` §4：竞争立即失败并报告，完整 mutation 周期持有同一锁，嵌套流程
  不能自锁；只读流程不取锁。
- `docs/08-testing.md` §2/§3.2：HOME/state/lock 必须位于临时根；进程间锁需要少量真实进程
  集成测试，mutation 与 `dot git` 互斥而只读命令不受锁阻塞。
- `docs/09-roadmap.md` §1 M1：本里程碑只交付单实例锁基础，不预建 M2 同步命令。

基线 `7b43272d6a98` 已包含 runtime preflight 与既有 `paths.ControlPlanePaths`，但没有锁或
storage package。本分支不会接入这些消费者；后续 runtime-loading 从已校验的 state root/lock
展示路径调用本 Goal 的获取入口。

主 agent 已核对官方资料，拟采用 `github.com/gofrs/flock v0.13.0`：这是 2025-10-09 tagged
release，BSD-3-Clause，Go directive 为 1.24，pkg.go.dev 约有 1499 known importers；其 `go.mod`
列出 `golang.org/x/sys` 和仓库自身测试依赖。该模块仍为 pre-v1，因此只在窄 adapter 内使用
排他非阻塞 `TryLock`、`Unlock` 与 `SetPermissions`；不使用 shared lock，也不以 `Locked()`
判断 ownership。若本地 module graph、版本或实际传递依赖与上述结论冲突，停止并报告，不
自行换版本。

## Progress

- [x] 2026-07-19：确认 worktree、top-level、branch 与基线分别为分配路径、
  `feat/process-lock` 和 `7b43272d6a98`，工作区 clean。
- [x] 2026-07-19：先以缺失 API 的失败测试固定 storage 权限边界，再实现 `internal/storage`；
  新建及现存 state root/私有文件收敛为 0700/0600，相对路径零写入，非目录/非普通文件明确
  拒绝；`go test ./internal/storage` 通过。
- [x] 2026-07-19：以缺失 API 的失败测试固定 busy/IO/ownership 语义；真实 helper 子进程覆盖
  busy、release 与异常退出恢复，显式 owner/guard 覆盖路径绑定和嵌套不提前解锁。
- [x] 2026-07-19：引入 `gofrs/flock v0.13.0`，用窄 adapter 首次真实使用；`go mod tidy`
  后生产 module graph 仅新增 direct flock 与 indirect `x/sys v0.37.0`，依赖仓库测试模块只进入
  checksum/graph。窄测、20 次重复、race、Darwin/Linux amd64 交叉编译和
  `make check BINARY=/private/tmp/dot-cp2-process-lock-check` 均通过。
- [ ] 完成重复窄测、双平台交叉编译、依赖图审计、diff check 与 `make check`，更新本计划后
  保持 active，等待独立复核。

## Milestones

### Milestone 1：固定 storage 与 ownership 契约

先增加 package 级测试，证明 state root 与 lock 权限、现存对象异常处理以及显式 ownership
的路径绑定和嵌套生命周期。storage 只负责 state 家族共同的安全创建/权限真相源，不读取或
解释 state。ownership 不依赖 package global，也不从底层 flock 的进程内状态反推。

Concrete steps：

    在 repo root 运行：go test ./internal/storage ./internal/lock
    预期：实现前因 package/API 缺失失败；实现后全部通过。

验收：

- Acquire 被调用后 state root/lock 精确为 0700/0600；未调用时零写入。
- 非目录 state root、非普通 lock、无法安全修正的 mode/IO 明确失败。
- 同一路径且同 owner 的嵌套 guard 复用锁；不同 owner/path 被拒绝，内层关闭不提前释放。

Commit 边界：

    feat(storage): 建立 state 存储权限边界

### Milestone 2：引入并验证进程排他锁

引入锁依赖并在窄 adapter 后首次真实使用。真实 helper 子进程通过 ready handshake 和 timeout
协调，不使用 shell 或 sleep：第二进程立即得到 busy，外层释放后可成功获取，持锁进程异常
退出后内核释放锁；IO 错误与竞争错误保持可分类。

Concrete steps：

    在 repo root 运行：go mod tidy
    在 repo root 运行：go test ./internal/lock -count=20
    预期：依赖固定为已调查版本，真实进程场景稳定通过且无等待竞态。

验收：

- 排他锁竞争立即返回 busy；底层 open/permission/type 错误保留 IO cause。
- release 与进程异常退出均允许后续进程重新获取。
- adapter 只暴露本项目所需的 acquire/release 机制，依赖不接管 ownership 或 runtime 顺序。

Commit 边界：

    feat(lock): 提供进程排他锁

### Milestone 3：验证与交接

审阅完整依赖图和本分支 diff，在本机重复运行进程锁测试，交叉编译 Darwin/Linux 测试二进制，
运行与 CI 一致的完整门禁。记录本机证据和远端未验证项，保持 ExecPlan active 交给独立 reviewer。

Concrete steps：

    在 repo root 运行：go test ./internal/storage ./internal/lock -count=20
    在 repo root 运行：go mod tidy && go mod tidy -diff
    在 repo root 运行：go list -m all
    在 repo root 运行：GOOS=darwin GOARCH=amd64 go test -c -o /private/tmp/dot-cp2-lock-darwin.test ./internal/lock
    在 repo root 运行：GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-cp2-lock-linux.test ./internal/lock
    在 repo root 运行：make check BINARY=/private/tmp/dot-cp2-process-lock-check
    在 repo root 运行：git diff 7b43272...HEAD --check
    预期：全部命令退出 0；依赖图与调查一致；worktree clean。

验收：

- 完整 diff 只包含本 Goal 的 storage/lock、测试、依赖文件和 active ExecPlan。
- 本机门禁通过；精确 HEAD 的远端 macOS/Linux CI 未运行时明确标记远端待验收。

Commit 边界：

    docs(lock): 记录进程锁验证结果

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| 非阻塞跨进程排他与 busy/IO 分类 | 真实 helper 子进程测试 | 待验证 |
| release/异常退出后恢复 | 真实 helper 子进程测试 | 待验证 |
| 显式 ownership 嵌套且不提前释放 | owner/guard 生命周期测试 | 待验证 |
| state root/lock 为 0700/0600 | 真实文件系统 mode 测试 | 待验证 |
| 未调用 Acquire 时零写入 | 临时根目录快照 | 待验证 |
| Go module 与双平台门禁 | tidy、依赖图、交叉编译、make check | 待验证 |

最终成功判据是全部窄测、重复测试、module/diff 门禁与 `make check` 退出 0，分支 clean 且独立
reviewer 没有未处理 blocking finding。远端 CI 不在本 worker 授权内。

## Safety, Authorization, and Recovery

当前任务明确授权在本 worktree 修改、stage 和 commit 本 Goal 文件；不授权切分支、merge、
main 操作或触碰其他 worktree。全部测试只使用 `t.TempDir()` 与 helper 子进程参数指定的临时
state root/lock，不读取真实 HOME、state、backup、machine config 或 `modules/`。helper 使用
context timeout，失败时由测试进程终止并由临时目录清理；锁文件是可重复使用的持久 inode，
不靠删除解锁。

若依赖版本/图与调查冲突、无法在两平台表达同一排他语义、ownership 必须依赖 global、或
只能吞掉 unlock/IO 错误才能继续，立即更新本计划并停止报告。

## Interfaces and Dependencies

storage package 提供只围绕 state root 与普通文件权限的窄入口，未来 state-store 可复用，但
本 Goal 不实现 store。lock package 接受显式绝对 state root 和 lock path，并返回显式 owner/
guard；路径必须绑定，嵌套复用只能由已有 owner 发起。

唯一新增生产依赖预期为 `github.com/gofrs/flock v0.13.0`，其通用机制限定为 OS advisory lock。
本项目自己承担调用顺序、错误语义、ownership、权限和嵌套复用契约。

## Surprises & Discoveries

- Observation: `go mod tidy` 会下载并在 `go.sum` 记录 flock 自身测试依赖，但本项目的生产
  `go.mod` 只新增 flock 与 indirect `x/sys`。
  Evidence: `go mod graph` 显示 testify、go-spew、difflib、yaml 等仅位于 flock 的出边；项目
  direct/indirect require 与调查一致。
  Impact: 不构成依赖版本或传递图冲突，无需改变已锁定版本。

- Observation: 首次完整门禁只发现 errorlint 要求 ownership 的底层校验错误也用 `%w` 保留。
  Evidence: `make check` 首轮单一 finding 指向 `internal/lock/lock.go`，修正后 0 issues 且完整
  race/build/doctor 门禁通过。
  Impact: 错误分类同时保留 `ErrOwnership` 和底层校验 cause，未改变公开契约。

## Decision Log

- Decision: 只采用排他非阻塞锁，并用本项目显式 owner/guard 表达嵌套所有权。
  Rationale: 规范只需要单实例 mutation；shared lock、等待重试和底层 `Locked()` 状态都会扩大
  语义或模糊 ownership。
  Date: 2026-07-19

- Decision: state root/lock 权限规则放入独立窄 storage package。
  Rationale: process-lock 与后续 state-store 必须共享 0700/0600 单一真相源，同时避免并行修改
  `internal/state` 或复制权限逻辑。
  Date: 2026-07-19

## Outcomes and Handoff

尚未实施。计划保持 active；完成实现和本地验证后交给未参与实现的只读 reviewer。
