# feat/apply-cli：开放 M1 apply 执行入口

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，用户可通过 `dot apply [module...]` 执行 M1 link/scaffold、force backup replacement
与 canonical P1/P2/P3 prune。命令稳定展示计划、warning、每个成功备份的精确路径与最终状态，
按 1 > 3 > 2 > 0 映射运行错误、conflict、未完成工作和成功。dry-run 保持原有无锁零写入路径。

## Scope / Non-goals

范围内：

- 非 dry-run apply 连接 `internal/apply.Run`，传递 full/partial scope、force、prune 与 runtime overrides。
- whole-module prune 通过一次 y/N 确认；`--yes` 直接授权，没有可用终端或 EOF 时拒绝并延迟 prune。
- 输出 context、canonical actions、warning、成功 backup 精确路径、deferred/确认结果和 no-op；输出错误优先退出 1。
- 退出码遵循 1 > 3 > 2 > 0：计划或提交时 conflict 为 3，确认拒绝/deferred 为 2，runtime/output 为 1。
- M1 `--adopt` 在 runtime IO 前硬拒绝；managed/rendered 与 scope 内任意 run_once 继续 fail closed。
- 完整隔离 HOME/repo/config/state/backup 的真实文件系统测试覆盖部分成功 state、重跑收敛与 dry-run 零写入。
- 同步 README 当前实现事实。

明确不做：

- 不实现 hook 执行、managed/rendered、M2 adopt、init/add/self-update 或新依赖。
- 不重算 ownership、Precondition、P1/P2/P3、scope、确认组或 state transition。
- 不改变 state v1、backup 布局、planner/executor contract 或规范文档。

## Contract and Context

- `docs/04-cli-spec.md` §2–§4.4/§5：apply flags、输出、确认、partial scope、dry-run 与退出优先级。
- `docs/05-apply-engine.md` §1–§7/§10：创建先于 prune、force backup、Precondition、部分成功和重跑恢复。
- `docs/06-templates.md`：M1 managed 输入必须明确拒绝，不能按 link 降级。
- `docs/08-testing.md`：mutation fixture 全隔离并证明幂等、零误写、部分成功与真实文件系统行为。
- `docs/09-roadmap.md` M1：本节点只开放已交付 link/scaffold/backup/prune runner；hooks 留给 CP7。

基线 `e0d22431ee812fe46f5464ce1836024a89a2ff13` 已有只读 `planner.PlanApply` 与
`internal/apply.Run`。runner 在 lock 内重新 strict load/plan，先验证 scoped file/prune/hook；
任何 HookRun 或 HookSkip 都在 executor、确认和 state mutation 前拒绝。CLI 当前只开放 dry-run，
且只读 projection 已定义稳定 context/action/warning 词汇。本分支复用这些事实源，不复制决策表。

## Progress

- [x] 2026-07-20：确认 worktree/top-level `/private/tmp/dot-m1-cp5-cli`、branch `feat/apply-cli`、
  clean baseline `e0d2243`，阅读适用规范、实现、测试和 completed ExecPlans。
- [ ] 提交本 active ExecPlan 起点。
- [ ] 测试先行接入 mutation apply、确认、输出与退出码。
- [ ] 补齐隔离真实文件系统、partial/state recovery、幂等与 dry-run 回归；更新 README。
- [ ] 运行窄测、branch diff check、隔离 cache `make check`，保持计划 active 等待独立复核。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定基线、范围、数据流、验证与 commit 边界。

Commit 边界：

    docs(plan): 建立 apply CLI 执行计划

### Milestone 2：执行入口与非交互确认

先在 `internal/cli` 增加测试，证明 mutation apply 调用 runner、传递 scope/flags，并将确认组以稳定
y/N 文本写到 stderr；`--yes` 不读输入，无终端/EOF/非 yes 均返回拒绝而非伪造 runtime error。
再用可注入 runner/确认 reader 的最小 CLI seam 实现 wiring，保留 dry-run 的 `planner.PlanApply` 路径。

验收：`--adopt` 和互斥 prune flags 在 runtime 前失败；scope 内 hook 由 runner 在 mutation 前拒绝；
确认仅在 runner 请求时发生且拒绝不会执行 prune。

Commit 边界：

    feat(cli): 接入 apply 执行与确认

### Milestone 3：执行输出与退出聚合

先固定成功、conflict、deferred、partial success 与 backup 输出，再复用 canonical plan projection，
叠加 runner 的真实结果事实。构造完整 projection 后再输出，backup 仅报告 runner 返回的成功精确路径。

验收：runtime/error/output 为 1；任一 unresolved conflict 为 3；无 conflict 时确认拒绝或 deferred 为 2；
完全收敛为 0；空计划输出 `Already up to date.`。

Commit 边界：

    feat(cli): 输出 apply 执行结果

### Milestone 4：真实文件系统回归与 README

使用 `t.TempDir` 构造完整 HOME/XDG/config/repo/state/backup，显式重定向 `DOT_CONFIG`、`DOT_REPO`，
并断言真实 HOME 不变。覆盖正常 link/scaffold、force backup、P1/P2/P3、partial scope、确认拒绝、
部分成功 state、二次重跑无 target/adopt/Store/backup mutation，以及全新 HOME dry-run 零写入。

Commit 边界：

    test(cli): 固定 apply mutation 安全边界

    docs(readme): 更新公开 apply 状态

## Validation and Acceptance

在 `/private/tmp/dot-m1-cp5-cli` 运行：

    go test ./internal/cli ./internal/apply ./internal/executor ./internal/backup
    git diff e0d22431ee812fe46f5464ce1836024a89a2ff13...HEAD --check
    make check

成功要求：全部命令退出 0；完整 branch 只含本 active plan、CLI wiring/presentation/tests 与 README；
worktree clean。测试不读取或写入真实 modules、machine config、state、backup、`.env` 或主力 HOME。
远端 macOS/Linux CI 未运行，留待 Checkpoint integration 验收。

## Safety, Authorization, and Recovery

用户已授权本 branch/worktree 内创建 active plan、修改、stage、commit 与验证。所有 mutation 测试
使用绝对临时 `--home`、repo、config、state 和 backup，并清除或重定向全部相关环境。失败用新
fix commit 处理，不 amend/rebase/reset；不切换或合并 main，不操作其他 worktree。

## Surprises & Discoveries

暂无。

## Decision Log

- Decision: mutation apply 复用 planner plan projection，再叠加 runner Result，不从文件系统结果重算 action。
  Rationale: planner 是动作、scope、warning 和稳定顺序的唯一真相源；runner 只补充执行事实。
- Decision: hook gate 保持在 `internal/apply.Run` 的 scoped plan validation，不在 CLI 预跑只读 planner。
  Rationale: mutation 必须基于锁内 exact inputs；partial scope 已由同一锁内 planner 缩小。

## Outcomes and Handoff

尚未完成；保持 active 等待实施和独立复核。
