# feat/status：交付纯只读健康巡检

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，用户可以运行 `dot status`，通过同一个 `planner.PlanApply` 严格只读 pipeline 检查
当前完整 profile。命令以稳定 summary 和 DRIFT、PENDING、ORPHAN / PENDING PRUNE、
UNASSIGNED MODULES 分节展示巡检结果；drift、尚未完成的 desired/hook 动作和 orphan 退出 2，
仅 unassigned 或完全 clean 退出 0，runtime/planner/output 错误退出 1 且绝不同时输出 `Clean.`。
命令不取 mutation lock，不创建或修改 target、state、backup、temp，也不执行 hook。

## Scope / Non-goals

范围内：

- 在 `internal/cli` 注册无位置参数、无 command-local flags 的 `status`，仅接受既有全局
  repo/home/profile/verbose/no-color；巡检始终使用完整 profile、默认非 force 且完整收集 orphan。
- 只投影 `ApplyPlan.Context/Observed/FileActions/Prune/Hooks`，不重新读取 state、扫描 target、计算
  ownership、决策、scope、P1–P3 或 hook fingerprint。
- summary 使用 effective module 数和完整 observed desired 文件数；分节固定为 DRIFT、PENDING、
  ORPHAN / PENDING PRUNE、UNASSIGNED MODULES，节内沿用 planner 的稳定顺序。
- `ReasonLinkDrift` 进入 DRIFT；其他非 skip file action 与 pending hook 进入 PENDING；全部 P1/P2/P3
  orphan 无论 active/deferred 都进入 ORPHAN / PENDING PRUNE；UNASSIGNED 只来自
  `ApplyContext.UnassignedModules`。
- scaffold 已有记录后的 target 缺失或任意内容变化仍是规范允许的用户所有产物状态，S1a/S2
  `skip` 不伪报 drift；首次 scaffold、metadata adopt 与 kind migration 等尚未完成的动作进入
  PENDING。
- 覆盖完整 L/S/kind migration/conflict、alias metadata、P1/P2/P3/deferred、hook pending/skip、
  unassigned-only、混合排序、invalid 输入、missing state、occupied lock 与整树零写入。

明确不做：

- 不复用 diff 的 action-line 字符串拼装 status，不改变 diff/dry-run 输出、flags 或退出码。
- 不提供 status module scope、`--force`、`--prune`、`--no-prune` 或 watch/continuous status。
- 不实现 executor、lock、mutation、文本 diff、managed/rendered/adopt M2、workflow、filesystem
  abstraction、`--json`、新依赖或 M2/M3。
- 不修改规范、planner/runtime/state/manifest contract 或持久化格式。

## Contract and Context

- `docs/04-cli-spec.md` §3/§4.4/§5：status 是健康巡检，DRIFT/PENDING/orphan 统一 actionable 2；
  conflict 不返回 3；unassigned 单独不影响 `Clean.`/0；错误不输出可信结果；分节和顺序稳定。
- `docs/02-architecture.md` §4–§6：CLI 只编排与映射，完整计划来自无锁只读 pipeline；计划职责是
  ownership、decision 与 validation 的唯一真相源。
- `docs/03-manifest-spec.md` §2：unassigned 是未被任何声明 profile 引用的 module，不包括仅未被
  current profile 引用的 module。
- `docs/05-apply-engine.md` §1–§5/§8/§10：L/S/kind migration、P1–P3 与 hook action 已形成稳定
  reason/verb；scaffold 产物创建后归用户，缺失或修改不构成未完成 desired。
- `docs/08-testing.md` §2–§4、`docs/09-roadmap.md` §1/§3：公开 status 输出、invalid fail-closed、
  held-lock 与零写入必须成为 M1 门禁，不预建 executor 或 M2/M3。

基线是 clean `main@afd13c84b8af90d3f6da5da597271bfa1de0c6ec`。前六个 CP3 Milestone
已独立 review、closure 并合入。现有唯一 public `planner.PlanApply` 严格 load runtime，形成完整
desired、scoped render、observation、file decisions、prune、hooks 与 combined validation；它在
错误时返回零计划且从不取锁或写入。`ApplyContext.UnassignedModules` 已由严格 manifest load
按“所有 profile 均未引用”产生并稳定排序；file conflict 是同一 plan 的封闭 action/reason，
因此二者没有 taxonomy 歧义。

## Progress

- [x] 2026-07-19：确认 pwd/Git 顶层均为 `/private/tmp/dot-cp3-status-019f795e`，branch
  `feat/status` clean，`HEAD == base == afd13c8`。
- [x] 2026-07-19：读取仓库规则、计划生命周期、coordinator、completed apply/plan-cli plans、
  README、指定规范与 CLI/planner APIs；确认 status taxonomy 可由 ApplyPlan 无歧义投影。
- [x] 2026-07-19：提交 active ExecPlan 起点（`da154de`）。
- [x] 2026-07-19：测试先行固定 status summary、DRIFT/PENDING、Clean、unassigned-only 和
  conflict→2，接入唯一 `PlanApply` 的 status projection（`cf8e114`）。
- [x] 2026-07-19：补齐 P1/P2/P3/deferred、L/S/kind migration、alias、hook、完整 profile、
  invalid 与 no-local-flags 分类矩阵（`aa1edb4`）。
- [x] 2026-07-19：覆盖 missing state、held lock、output error、完整环境隔离和 success/error 整树
  零 mutation（`aa1edb4`）。
- [x] 2026-07-19：更新 README 当前实现事实（`e6ccb4f`）。
- [x] 2026-07-19：运行窄测、20 次重复、race、双平台交叉编译、branch diff check 与
  `make check`；worktree clean，计划保持 active 等待独立 review。
- [x] 2026-07-19：review round 1 提出有效 P2：development notice 的 stderr 写失败时，旧实现已先
  输出 clean/actionable 可信 verdict。回归先在旧实现稳定失败，以 `446cadb` 让 notice 成为 status
  stdout 的前置提交；正常字节契约不变。
- [x] 2026-07-19：round 1 fix 后相关三包普通/20 次/race、Darwin/Linux amd64 交叉编译、完整
  branch diff check 与 `make check` 再次通过；等待 round 2 完整 branch review。

## Milestones

### Milestone 1：提交 ExecPlan 起点

单独提交本计划，固定 status 的单一 plan 数据流、分类、公开分节、退出码与只读边界。

验收：拟提交 diff 只包含 `.agent/plans/active/m1-status.md`。

Commit 边界：

    docs(plan): 建立 status 执行计划

### Milestone 2：接入 status 核心巡检投影

先用真实隔离 runtime fixture 固定 summary、被改指 owned link 的 DRIFT、其他未完成 file/hook
动作的 PENDING、clean/unassigned-only 和 conflict→2；再注册 `status` 并增加独立 projection。
projection 必须先完整构造和校验后再输出，避免错误时泄漏半段可信巡检结果。

验收：同一被改指 link 在 status 为 DRIFT/2，而既有 diff 仍为 CONFLICT/3；no finding 输出
`Clean.`/0；unassigned section 与 `Clean.` 可同时出现。

Commit 边界：

    feat(cli): 接入 status 健康巡检

### Milestone 3：补齐 orphan、迁移与只读安全矩阵

扩充真实 filesystem fixture，覆盖 P1/P2/P3/deferred 无遗漏、L/S/kind migration/alias、hook
pending/skip、scaffold 用户所有权例外、invalid manifest/config/state、missing state root、occupied
mutation lock，以及 success/error 前后完整树快照。测试必须显式重定向 HOME、XDG、DOT_CONFIG
与 DOT_REPO。

验收：orphan 不因 conflict/deferred 或其 P 行分类消失；任何 actionable finding 为 2；任何 strict
load/planner 错误只输出 error 并为 1；所有只读路径不创建 state/lock/temp/backup/target。

Commit 边界：

    test(cli): 固定 status 分类与零写入边界

### Milestone 4：门禁与独立 review 交接

更新 living sections，记录实际 commits、风险与证据；运行相关测试、重复/race、Darwin/Linux
交叉编译、完整 branch diff 与 CI 同等门禁。计划保持 active，由 coordinator 安排未参与实现的
reviewer。

Commit 边界：

    docs(plan): 记录 status 交接证据

## Validation and Acceptance

在本 worktree 根运行：

    go test ./internal/cli ./internal/planner ./internal/runtime
    go test -count=20 ./internal/cli ./internal/planner ./internal/runtime
    go test -race ./internal/cli ./internal/planner ./internal/runtime
    GOOS=darwin GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-status-darwin.test ./internal/cli
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-status-linux.test ./internal/cli
    git diff afd13c84b8af90d3f6da5da597271bfa1de0c6ec...HEAD --check
    make check BINARY=/private/tmp/dot-cp3-status-check/dot

成功判据：命令全部退出 0；branch diff 只含 active plan、status CLI/presentation、隔离测试与必要的
README 当前实现状态；无新依赖、无 planner/runtime/state contract 或 mutation 代码。交叉编译只
证明构建；远端 macOS/Linux CI 未实际运行时标记待验收。

## Safety, Authorization, and Recovery

用户已授权在本 branch/worktree 创建 active plan、修改、stage、commit 和验证该 Milestone。
测试只使用 `t.TempDir()` 合成 HOME/repo/config/state/backup/target/lock，并显式覆盖 HOME、XDG、
DOT_CONFIG 与 DOT_REPO；不读取或修改真实 modules、machine config、state、backup、`.env` 或
主力 HOME。occupied lock 仅位于隔离 state root。失败以新 fix commit 处理，不 amend/rebase/reset；
不切 branch、不操作 main 或其他 worktree。计划保持 active 等待独立 review。

## Interfaces and Dependencies

不新增依赖。status 只用既有 global options 形成 default full-scope `planner.ApplyOptions`，调用唯一
`planner.PlanApply`，再消费 opaque `ApplyPlan` getters。独立 status projection 表达巡检 taxonomy；
它不会改变或借用 diff 的动作行输出契约，也不暴露新 planner 入口。

## Surprises & Discoveries

- Observation: file conflict 存在时，planner 按收敛门控把 P1/P2/P3 全部标记 deferred，但 orphan
  的基础 P 行分类仍完整保留。
  Evidence: 混合 fixture 的 owned orphan 同时携带 `PruneReasonOwned` 与
  `DeferredReason=file-conflict`。
  Impact: status 始终放入 ORPHAN / PENDING PRUNE，并在描述后追加 deferred 原因；不会因 conflict
  漏掉 orphan，也不会把它降成普通 PENDING。

- Observation: status 规范没有 module scope 或 force/prune 等 command-local flags；`--profile`
  选择的是另一个完整 effective profile，不是 partial planner scope。
  Evidence: `docs/04-cli-spec.md` §4.4 没有位置参数/局部 flag，§2 仍统一提供 global profile；
  Cobra no-args/flag tests 与 alternate profile fixture 均通过。
  Impact: status 始终调用 default full-scope `PlanApply`，因此所有 current-profile orphan 都能展示。

- Observation: current run_once fingerprint 只能由同一个 hook planner 稳定获得，测试不应复制其
  versioned hash 编码。
  Evidence: hook-skip fixture 先在隔离环境调用 `PlanApply` 取得 fingerprint，再写入合成 state；
  随后 status 不产生 pending hook。
  Impact: 测试验证公开行为而不建立第二套 fingerprint 算法。

- Observation: `command.PrintErrf` 会把 stderr 写失败留给最终 output tracker，但不会阻止当前
  presentation 继续写 stdout。
  Evidence: round 1 dev-build 回归中，notice writer 返回 `os.ErrClosed` 后命令最终退出 1，但旧
  `printStatusProjection` 已输出 `Clean.` 或完整 actionable sections。
  Impact: status 需要在任何可信 stdout 前通过会返回 error 的真实 stderr 写入提交 notices；不能
  依赖最终 exit override 补救已经发布的 verdict。

## Decision Log

- Decision: UNASSIGNED 只读 `ApplyContext.UnassignedModules`，绝不从 current-profile scope 或 file
  conflict 推断。
  Rationale: manifest 已按“任何 profile 均未引用”形成唯一事实；重新扫描 module/profile 会产生
  第二套清单语义。
  Date: 2026-07-19

- Decision: 只有 `ReasonLinkDrift` 进入 DRIFT；其他 non-skip file action 进入 PENDING，所有 file
  conflict 均使 status 退出 2 而不是 3。
  Rationale: 规范明确把被改指 owned link 视为 DRIFT；其他 action 表示 desired 尚未完成。封闭 reason
  能区分现势 drift 与待创建/收养/迁移/用户阻挡，不需解析文案。
  Date: 2026-07-19

- Decision: S1a scaffold 内容/类型变化和 S2 已记录后的缺失均不作为 status finding。
  Rationale: scaffold 创建即归用户，记录只表示一次性生命周期；这两个 skip 是已满足的 desired，
  若报告 drift 会错误恢复工具所有权语义。S3、metadata adopt 与迁移动作仍属于 PENDING。
  Date: 2026-07-19

- Decision: status 先构造并验证完整 projection，再输出 summary 或 section；section 顺序固定为
  DRIFT → PENDING → ORPHAN / PENDING PRUNE → UNASSIGNED MODULES。
  Rationale: internal mapping 错误必须按运行错误 1 fail closed，不能泄漏半段可信巡检；固定 section
  和 planner 既有节内顺序满足公开确定性。
  Date: 2026-07-19

- Decision: status 不复用 diff action-line formatter，只共享 `PlanApply` 事实输入。
  Rationale: diff 是动作预览，status 是健康 taxonomy；共享字符串会把 CONFLICT/3、verb/reason
  协议和 skip 可见性错误耦合到 status。
  Date: 2026-07-19

- Decision: compatibility notice 是 status summary/sections/Clean 的输出前置条件；通过真实 stderr
  writer 写入并检查结果，失败立即返回。
  Rationale: 运行错误 1 不能与可信健康 verdict 并存；这保持正常 stdout/stderr 字节不变，也不靠
  writer 预探测、吞错或改变 taxonomy。相同路径的 clean/actionable projection 都受回归保护。
  Date: 2026-07-19

## Outcomes and Handoff

实现已完成，计划保持 active，等待 coordinator 安排未参与实现的独立 reviewer：

- `dot status` 只调用唯一 `planner.PlanApply`，再消费 opaque context/observation/file/prune/hook
  getters；没有第二次 runtime/state/target 读取或 ownership/P 行/fingerprint 计算。
- summary、四个稳定 sections、Clean 与 error-only 输出均受 exact/contains 测试保护；同一 link
  drift 在 status 为 DRIFT/2，既有 diff 仍为 CONFLICT/3。
- file reason、P1/P2/P3/deferred、hook pending/skip、alias、kind migration、scaffold 用户所有权、
  alternate full profile 和 unassigned-only 均有真实 filesystem/runtime fixture。
- invalid manifest/config/state、missing state root、occupied mutation lock 与 stdout failure 分别验证
  1/2/0 边界；success/error 前后完整树一致，production path 不获取 lock、不写 target/state/
  backup/temp，也不执行 hook。
- 无新依赖；没有修改 planner/runtime/state/manifest contract、持久化格式、diff/dry-run output、
  executor、watch、unified diff、workflow 或 M2/M3。

提交序列：

    da154de docs(plan): 建立 status 执行计划
    cf8e114 feat(cli): 接入 status 健康巡检
    aa1edb4 test(cli): 固定 status 分类与零写入边界
    e6ccb4f docs(readme): 更新 status 实现状态
    244cc3e docs(plan): 记录 status 交接证据
    446cadb fix(cli): 在 status verdict 前提交 notice

本地验证（2026-07-19，均退出 0）：

    go test ./internal/cli ./internal/planner ./internal/runtime
    go test -count=20 ./internal/cli ./internal/planner ./internal/runtime
    go test -race ./internal/cli ./internal/planner ./internal/runtime
    GOOS=darwin GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-status-darwin.test ./internal/cli
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-status-linux.test ./internal/cli
    git diff afd13c84b8af90d3f6da5da597271bfa1de0c6ec...HEAD --check
    make check BINARY=/private/tmp/dot-cp3-status-review-fix-check/dot

双平台只完成编译，未执行交叉编译产物；精确 HEAD 的远端 macOS/Linux CI 未运行，本地验收通过、
远端待验收。reviewer 应以 `afd13c84b8af90d3f6da5da597271bfa1de0c6ec...feat/status` 为
有效 diff，重点复核 conflict/DRIFT/PENDING 分界、scaffold clean 例外、所有 orphan 无遗漏、
unassigned-only Clean、notice/prerequisite 失败前零可信输出及全部只读路径零锁零写入。round 1
P2 已修复并完成全部门禁；round 2 前不迁移本计划到 completed，也不创建 closure commit。
