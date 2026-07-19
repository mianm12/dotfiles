# feat/plan-cli：交付 diff 与 apply dry-run 只读计划界面

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，用户可以运行 `dot diff` 或 `dot apply --dry-run`，经同一个
`planner.PlanApply` 严格只读 pipeline 得到稳定的运行上下文和 file/prune/hook 动作行。
两个入口对相同参数产生相同计划投影和退出码；运行错误、unresolved conflict、其他
actionable change 与 no-op 分别按 1、3、2、0 映射。命令不取 mutation lock，也不创建或修改
target、state、lock、backup、temp；CP5 前的非 dry-run apply 与 M1 不支持的 `--adopt` 在读取
runtime 或 planner 前明确拒绝。

## Scope / Non-goals

范围内：

- 在 `internal/cli` 接入 `diff [module...]` 与 `apply [module...] --dry-run`，共享同一套
  full/partial、`--force`、prune flag 与 presentation/exit 映射。
- 稳定输出 `repo=… profile=… os=…` 上下文，以及规范 verb、target、reason 动作行；普通
  `skip` 只在 `-v` 显示，skip-only 输出 `Already up to date.`。
- file conflict 优先退出 3；create/adopt/backup/scaffold、active/deferred prune、pending hook
  与 scaffold-deleted warning 退出 2；纯 skip 退出 0；任何 runtime/planner/输出错误退出 1。
- 使用完整隔离 HOME/repo/config/state/backup fixture 证明 success、error 与前置拒绝路径零写入，
  missing state root 不被创建，已有 lock 被占用时仍正常规划且树不变。
- M1 `--adopt`、任何非 dry-run apply 与互相冲突的 prune flags 在 planner 前硬拒绝。

明确不做：

- 不实现 executor、真实 apply、lock 获取、target/state/hook mutation、backup 或确认交互。
- 不实现 status、文本 unified diff、managed/rendered/adopt M2、workflow、filesystem abstraction、
  `--json`、新依赖或 M2/M3。
- 不重新计算 ownership、Precondition、P1–P3、hook fingerprint、scope 或排序；presentation 只消费
  已通过组合校验的 `ApplyPlan`。
- 不改变规范输出词汇、退出码、持久化格式或前置 planner/runtime contract。

## Contract and Context

- `docs/04-cli-spec.md` §3、§4.2–§4.4、§5：退出码优先级、dry-run/diff 无锁零写入、动作行词汇、
  context、skip 可见性和稳定顺序。
- `docs/02-architecture.md` §4–§6：CLI 只负责编排/输出，计划必须来自只读、自包含且 fail-closed
  的 pipeline。
- `docs/05-apply-engine.md` §1–§5、§8、§10：file/prune/hook 的动作与状态语义已经由 planner
  决定；hook 不受 conflict 门控，dry-run 不执行或写指纹。
- `docs/08-testing.md` §2–§4：公开输出受测试保护，完整隔离树证明只读路径无 mutation。
- `docs/09-roadmap.md` §1/§3：M1 先交付纯计划与 dry-run，CP5 前不开放真实 executor。

基线是 clean `main@83039c1565d795c38df82a531041d859af14a484`。前置
`feat/apply-planner` 已完成两轮复核，唯一 public `planner.PlanApply` 会严格 load runtime，形成
完整 desired、scoped render、observation、decision、prune、hook 与 combined validation，并在
任一错误返回零计划。`ApplyPlan` getters 已提供稳定排序和深拷贝；本分支只增加 CLI 投影。

现有 `internal/cli/cli.go` 统一处理 Cobra error、command requested exit code 与输出 writer 失败。
新增命令继续使用该单一出口：planner error 直接返回普通 error，由 `run` 输出 stderr 并返回 1；
只有可信计划成功后才返回 3/2/0。这样自然满足 1 > 3 > 2 > 0，不需要在 planner 错误旁保留
partial 输出。

## Progress

- [x] 2026-07-19：确认 worktree/Git 顶层均为
  `/private/tmp/dot-cp3-plan-cli-019f795e`，branch `feat/plan-cli` clean，
  `HEAD == base == 83039c1`。
- [x] 2026-07-19：读取仓库规则、计划生命周期、coordinator/apply plan、README、指定规范与
  CLI/runtime/planner 实现测试；未发现 contract blocker。
- [ ] 提交本 active ExecPlan 起点。
- [ ] 测试先行接入 `dot diff` 的稳定计划输出、skip/no-op 与 3/2/0 映射。
- [ ] 测试先行接入 `dot apply --dry-run` 的同投影、前置拒绝与 flag 契约。
- [ ] 覆盖 full/partial、force/no-prune、occupied lock、success/error/refusal 完整树零写入。
- [ ] 运行窄测、重复/race、双平台编译、branch diff check 与 `make check`，保持 active 等待 review。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定 base、输出/错误优先级、只读安全边界、行为切片和验收命令。

验收：拟提交 diff 只包含 `.agent/plans/active/m1-plan-cli.md`。

Commit 边界：

    docs(plan): 建立 plan CLI 执行计划

### Milestone 2：接入 diff 稳定计划投影

先用真实隔离 runtime fixture 固定 context、file/prune/hook/skip/conflict 行、verbose 可见性、
稳定顺序、full/partial、force/no-prune 与 no-op；再增加集中 presentation 映射并注册 `diff`。
presentation 只读取 `ApplyPlan` getters，不复制 planner 决策。

验收：invalid runtime/manifest/state 不输出 context、成功计划或 `Already up to date.` 并退出 1；
conflict 优先 3，其他 actionable 2，skip-only 0；相同输入重复输出一致。

Commit 边界：

    feat(cli): 接入 diff 只读计划

### Milestone 3：接入 apply dry-run 与 M1 拒绝边界

先覆盖 `apply --dry-run` 与 diff 的相同 projection、module scope 和 plan flags；再注册 apply flags，
让 `--adopt` 和任何缺少 dry-run 的调用在 PlanApply/runtime/lock/write 前拒绝。`--yes` 在 dry-run
只是不触发确认，不改变计划；互斥 prune flags 明确报 CLI 输入错误。

验收：裸 apply 即使 config/manifest 损坏仍返回“真实 apply 尚未实现”的 M1 错误且不触碰树；
`--adopt` 同样先拒绝；dry-run 通过真实 strict pipeline，与 diff 输出/退出码完全一致。

Commit 边界：

    feat(cli): 接入 apply dry-run 拒绝边界

### Milestone 4：只读安全回归与交接

用完整隔离树对 diff/dry-run 的成功、planner error、CLI 前置拒绝及 missing state root 分别做
before/after 快照；预先取得真实 mutation lock 后运行两个只读入口，证明不竞争也不改写 lock。
更新 living sections 与本地证据，计划保持 active 等待独立 reviewer。

Commit 边界：

    test(cli): 固定只读计划零写入边界

## Validation and Acceptance

在本 worktree 根运行：

    go test ./internal/cli ./internal/planner ./internal/runtime
    go test -count=20 ./internal/cli ./internal/planner ./internal/runtime
    go test -race ./internal/cli ./internal/planner ./internal/runtime
    GOOS=darwin GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-plan-cli-darwin.test ./internal/cli
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-plan-cli-linux.test ./internal/cli
    git diff 83039c1565d795c38df82a531041d859af14a484...HEAD --check
    make check BINARY=/private/tmp/dot-cp3-plan-cli-check/dot

成功判据：命令全部退出 0；完整 branch diff 只含 active plan、CLI presentation/wiring 与隔离测试；
无新依赖、无 runtime/planner contract 或 mutation 代码。交叉编译只证明构建；远端 macOS/Linux
CI 未实际运行时明确待验收。

## Safety, Authorization, and Recovery

用户已授权在本 branch/worktree 创建 active plan、修改、stage、commit 和验证当前 Milestone。
测试只使用 `t.TempDir()` 合成 HOME、repo、config、state、backup、target 和 lock，并显式覆盖
HOME/repo/config；不读取或修改真实 modules、machine config、state、backup、`.env` 或主力 HOME。
occupied-lock fixture 只锁定隔离 state root。失败以新 fix commit 处理，不 amend/rebase/reset；
不切 branch、不操作 main 或其他 worktree。计划保持 active，由 coordinator 安排 review/freshness。

## Interfaces and Dependencies

不新增依赖。CLI 从 global/options 形成 `runtime.Overrides` 与 `planner.ApplyOptions`，只调用
`planner.PlanApply`。集中 presentation 函数消费 opaque `ApplyPlan`，返回语义退出码并通过 Cobra
writer 输出；它不暴露新的 planner 入口或可执行能力。

## Surprises & Discoveries

暂无。

## Decision Log

- Decision: planner/runtime error 不生成 presentation，也不参与 3/2/0 聚合。
  Rationale: `PlanApply` error 返回零可信计划；现有 `run` 会将普通 error 固定映射 1，天然实现
  最高优先级并避免错误旁输出成功结论。
  Date: 2026-07-19

- Decision: diff 与 apply dry-run 共享同一 options 转换、PlanApply 调用和 presentation 函数。
  Rationale: 同一事实源和投影是防止两条公开只读路径行为漂移的最小实现。
  Date: 2026-07-19

## Outcomes and Handoff

尚未完成。计划保持 active，等待实现、验证和独立 review。
