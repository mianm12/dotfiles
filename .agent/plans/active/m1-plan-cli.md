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
- [x] 2026-07-19：提交 active ExecPlan 起点（`c5f9e30`）。
- [x] 2026-07-19：测试先行接入 `dot diff` 的稳定计划输出、skip/no-op 与 3/2/0 映射
  （`f01354f`）。
- [x] 2026-07-19：测试先行接入 `dot apply --dry-run` 的同投影、前置拒绝与 flag 契约
  （`c0d871e`）。
- [x] 2026-07-19：覆盖 full/partial、force/no-prune、occupied lock、success/error/refusal 完整树
  零写入，并补充缺失 state root 和进程环境隔离回归（`cb6225b`、`9fbab8b`、`fd70d67`）。
- [x] 2026-07-19：更新 README 当前实现状态与真实 apply 的 build 边界文案
  （`67c9aa1`、`e7c69e1`）。
- [x] 2026-07-19：运行窄测、重复/race、双平台交叉编译、branch diff check 与 `make check`；
  worktree clean，计划保持 active 等待独立 review。

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

- Observation: `planner.PlanApply` 的 public 入口使用 production environment resolver，CLI 测试即使
  注入了 `Environment.LookupEnv`，若不同时重定向真实测试进程的 `DOT_CONFIG`、`DOT_REPO`、
  `HOME` 和 `XDG_*`，仍可能读取开发机配置。
  Evidence: 增加进程环境隔离后，所有 plan CLI fixture 的 runtime 来源与完整树快照均只位于
  `t.TempDir()`。

- Observation: 规范中的 M1 包含后续真实 apply，因此拒绝文案不能笼统声称“真实 apply 不在 M1”。
  Evidence: 文案和测试改为精确说明“当前 build 尚未提供真实 apply”，仍引导使用
  `dot apply --dry-run`。

- Observation: S2 scaffold-deleted 与 P3 deferred prune 都可能只有普通 `skip` 动作；非 verbose
  不能因此静默成 no-op。
  Evidence: presentation 将普通 skip 保持隐藏，同时在 stderr 输出 warning，并以 actionable 2
  收口。

## Decision Log

- Decision: planner/runtime error 不生成 presentation，也不参与 3/2/0 聚合。
  Rationale: `PlanApply` error 返回零可信计划；现有 `run` 会将普通 error 固定映射 1，天然实现
  最高优先级并避免错误旁输出成功结论。
  Date: 2026-07-19

- Decision: diff 与 apply dry-run 共享同一 options 转换、PlanApply 调用和 presentation 函数。
  Rationale: 同一事实源和投影是防止两条公开只读路径行为漂移的最小实现。
  Date: 2026-07-19

- Decision: 先完整构造并校验 presentation projection，再输出 context 和动作行。
  Rationale: 未知 action/reason 等 mapping 错误必须按运行错误 1 fail closed，不能泄漏半段成功输出。
  Date: 2026-07-19

- Decision: `--prune` 与 `--no-prune` 只要同时显式出现就拒绝，不根据最终布尔值猜测用户意图。
  Rationale: 两个互斥公开开关同时出现是输入错误；在 runtime/planner 前拒绝最清晰且可测试。
  Date: 2026-07-19

- Decision: `--adopt` 的 M2 拒绝优先于非 dry-run apply 的当前 build 拒绝。
  Rationale: unsupported capability 应稳定指出真正的契约边界，并保证两者都不进入 runtime、planner
  或 mutation 路径。
  Date: 2026-07-19

## Outcomes and Handoff

实现已完成，计划保持 active，等待 coordinator 安排独立 reviewer：

- `dot diff` 与 `dot apply --dry-run` 共享唯一 `planner.PlanApply` 调用和 presentation projection；
  full/partial scope、force、prune 选择和输出/退出码不会形成双重事实源。
- context、file/prune/hook/warning/no-op 输出和 1 > 3 > 2 > 0 优先级由真实隔离 fixture 覆盖；
  `--adopt`、裸 apply 和互斥 prune flag 在 runtime/planner 前拒绝。
- success、planner error、前置拒绝、missing state root 和 occupied lock 均以完整树快照验证零 mutation；
  production path 不获取 lock，不写 target/state/backup/temp，也不执行 hook。
- 无新依赖；没有修改 runtime/planner contract、持久化格式、executor、status、unified diff、workflow
  或 M2/M3 能力。
- 本地通过窄测、`-count=20`、race、darwin/linux amd64 交叉编译、完整 branch diff check 和
  `make check`。交叉编译只证明构建；远端 macOS/Linux CI 未运行，仍待验收。

提交序列：

    c5f9e30 docs(plan): 建立 plan CLI 执行计划
    f01354f feat(cli): 接入 diff 只读计划
    c0d871e feat(cli): 接入 apply dry-run 拒绝边界
    cb6225b test(cli): 固定只读计划零写入边界
    9fbab8b fix(test): 隔离计划 CLI 进程环境
    fd70d67 test(cli): 覆盖缺失 state 的错误拒绝路径
    67c9aa1 docs(readme): 更新只读计划命令状态
    e7c69e1 fix(cli): 准确说明真实 apply 当前边界

独立 reviewer 应以 `83039c1565d795c38df82a531041d859af14a484...feat/plan-cli` 为有效 diff，
重点复核 presentation 是否纯消费可信 plan、错误前零成功输出、只读路径零锁零写入、partial scope
和 warning/exit 聚合。review 前不迁移本计划到 completed，也不创建 closure commit。
