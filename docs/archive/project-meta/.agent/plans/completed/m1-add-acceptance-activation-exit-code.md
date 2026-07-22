# fix/m1-add-acceptance：恢复显式 module activation 退出码

> [!WARNING]
> 历史工程记录，非当前规范或工作流程。

本 ExecPlan 是 living document。实施期间持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，显式 `dot add -m` 的 nonmembership、current GOOS inactive 或组合 activation 错误继续输出
确切恢复步骤，但 CLI 退出 1；只有未显式选择时零/多 prospective candidate 的
`ErrModuleAmbiguous` 退出 3。strict target mapping、missing/invalid explicit module 也保持 exit 1。

## Scope / Non-goals

范围内：

- 收紧 CLI add error 分类，只把 `ErrModuleAmbiguous` 投影为 conflict/exit 3。
- 覆盖显式 activation、missing/invalid、exact target mapping、inferred ambiguity、dry-run 零写入和隐私。

明确不做：

- 不改变 manifest Resolve、module/source selection、activation facts、恢复文本、持久化或 mutation。
- 不重开前两份 completed acceptance plans，不新增依赖，不修改规范。

## Contract and Context

- `docs/04-cli-spec.md` §4.5/§5：显式 `-m` 不可用是普通错误；零/多推断候选是 conflict exit 3；
  优先级保持 `1 > 3 > 2 > 0`。
- `.agent/plans/completed/m1-add-acceptance.md`：固定候选、profile/OS/target activation facts 与恢复文本。
- `.agent/plans/completed/m1-add-acceptance-exit-code.md`：固定 exact target-missing typed hard error exit 1。

有效 base 为 clean `main@d8fcf2a7616b2f86822f04e96036bbcc56ef269d`，branch
`fix/m1-add-acceptance`。最终三路 Acceptance 发现同根 P2：早期 acceptance fix 把内部
`ErrModuleActivation` 加入 CLI conflict 分类，误将显式 nonmembership/OS-inactive 从历史 exit 1
改为 3。

## Progress

- [x] 2026-07-22：确认 worktree、branch、effective base 与 clean 状态；读取前两份 completed plan、
  CLI classifier、preflight activation tests 与 Acceptance finding。
- [x] 2026-07-22：以 `1eddcbc` 提交本 active ExecPlan 起点。
- [x] 2026-07-22：先修改 CLI 回归并确认旧 classifier 把 explicit nonmembership、OS-inactive 与组合
  activation 投影为 exit 3；以 `90b423b` 将 conflict 分类收紧为仅 `ErrModuleAmbiguous`。
- [x] 2026-07-22：manifest/add/CLI 普通、add/CLI 5 次重复、三包 race、定向 lint、base/checkpoint
  diff check、隔离 `make check` 与 Darwin/Linux amd64 add/CLI test binary 交叉编译通过。
- [x] 2026-07-22：未参与实现的完整工程复核结论为 GO，无 P0–P3 finding；确认 main 仍为有效
  base，worktree clean。
- [x] 2026-07-22：主线程重新运行最终三包测试、两级 diff check 与隔离 `make check`，全部通过；
  完成本计划并迁移至 completed。

## Milestones

### Milestone 1：提交独立计划起点

    docs(add): 新建 activation exit-code ExecPlan

### Milestone 2：收紧 CLI conflict 分类

先修改 CLI 回归证明 explicit nonmembership、OS-inactive、组合 activation 均保留恢复文本但 exit 1；
missing/invalid、exact target-missing 仍 1；只有 inferred zero/multiple candidate 退出 3。随后最小修改
classifier，不改变底层 error identity 与文本。

    fix(cli): 恢复显式 module activation 退出码

## Validation and Acceptance

运行 manifest/add/CLI 普通测试、add/CLI 5 次重复、三包 race、定向 lint，
`git diff d8fcf2a7616b2f86822f04e96036bbcc56ef269d...HEAD --check`、
`git diff 5d176497a75c9f8e43b413d43f04f3ea41720c51...HEAD --check`、隔离 `make check` 与
Darwin/Linux amd64 add/CLI test binary 交叉编译。真实 Linux/远端 CI 标记待验收。

当前上述命令均通过。`make check` 完成 tidy/format diff、0 lint issue、全仓 race、build 与 manifest
check；独立工程复核结论为 GO，无 P0–P3 finding。主线程在 closure 前重新运行最终三包测试、两级
diff check 与隔离 `make check`，全部通过。Darwin/Linux amd64 add/CLI test binary 交叉编译通过；
真实 Linux 主机与远端 macOS/Linux CI 未运行，远端待验收。

## Safety, Authorization, and Recovery

用户已授权本 branch/worktree 的新 active plan、范围内修改、stage、commit 与验证。失败使用新
commit，不 amend/rebase/reset/cherry-pick/squash；不操作 main/其他 worktree，不接触真实数据。

## Interfaces and Dependencies

不新增依赖。preflight error identity 保留内部诊断价值；CLI classifier 是进程退出码的唯一投影点。

## Surprises & Discoveries

- Observation: `ErrModuleActivation` 的内部 identity 对测试/诊断仍有价值，但不属于公开 conflict
  分类。
  Evidence: preflight 可继续 `errors.Is(ErrModuleActivation)`，CLI 只检查 `ErrModuleAmbiguous` 后，
  exact recovery 文本完全不变而 exit 从 3 恢复为 1。
  Impact: 不需删除或替换底层 sentinel；单一最小改动位于 CLI classifier，避免触碰 selection/manifest。

## Decision Log

- Decision: CLI conflict 分类只接受 `ErrModuleAmbiguous`。
  Rationale: 恢复指引与内部 activation identity 不改变显式用户输入错误的公开等级。
  Date: 2026-07-22

## Outcomes and Handoff

- `90b423b` 仅收紧 CLI classifier：只有 `ErrModuleAmbiguous` 映射为 conflict/exit 3。
- 显式 module nonmembership、current GOOS inactive 与组合 activation 错误保留确切恢复文本并退出 1；
  missing/invalid explicit module 与 exact target-missing typed hard error 也保持 exit 1。
- 未显式选择时零/多 prospective candidate 保持 exit 3；dry-run 零写入与隐私回归通过。
- 完整本地门禁与独立工程复核通过；真实 Linux 主机与远端 macOS/Linux CI 待验收。
- 本计划已迁移至 completed。交回主线程确认 closure commit 与 clean worktree 后按 Checkpoint DAG
  集成本 branch。
