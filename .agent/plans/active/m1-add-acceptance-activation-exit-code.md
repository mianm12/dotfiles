# fix/m1-add-acceptance：恢复显式 module activation 退出码

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
- [ ] 提交本 active ExecPlan 起点。
- [ ] 测试先行恢复显式 activation exit 1，运行完整本地门禁。
- [ ] 保持计划 active、worktree clean，等待未参与实现的完整工程复核。

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

## Safety, Authorization, and Recovery

用户已授权本 branch/worktree 的新 active plan、范围内修改、stage、commit 与验证。失败使用新
commit，不 amend/rebase/reset/cherry-pick/squash；不操作 main/其他 worktree，不接触真实数据。

## Interfaces and Dependencies

不新增依赖。preflight error identity 保留内部诊断价值；CLI classifier 是进程退出码的唯一投影点。

## Surprises & Discoveries

暂无。

## Decision Log

- Decision: CLI conflict 分类只接受 `ErrModuleAmbiguous`。
  Rationale: 恢复指引与内部 activation identity 不改变显式用户输入错误的公开等级。
  Date: 2026-07-22

## Outcomes and Handoff

尚未完成；等待实现、验证与独立复核。
