# fix/m1-add-acceptance：保持 target mapping strict error 退出码

本 ExecPlan 是 living document。实施期间持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，显式 `dot add -m` 命中该 module 的 current GOOS target mapping 缺失时，仍会附带已验证的
module-local 恢复步骤，但错误身份保持 strict manifest `ModuleTargetMappingError`，CLI 退出 1。
普通 module/source ambiguity 以及 Resolve 成功后的 OS-inactive/nonmembership selection conflict
继续退出 3，维持 `1 > 3` 优先级。

## Scope / Non-goals

范围内：

- 保留 strict Resolve typed target error，并在显式 module 精确匹配时附加既有 activation guidance。
- target-missing exact/no-`-m`/other-module、普通 ambiguity、OS-inactive/nonmembership 的退出码回归。
- 继续证明 dry-run 零写入、隐私与 Darwin/Linux 兼容。

明确不做：

- 不改变 manifest target 语义、profile/OS activation、候选选择、持久化、ownership 或 mutation。
- 不重开 `.agent/plans/completed/m1-add-acceptance.md`，不新增依赖，不修改规范。

## Contract and Context

- `docs/03-manifest-spec.md` §2/§6：active module 缺 current GOOS target mapping 是严格 resolve 错误。
- `docs/04-cli-spec.md` §4.5/§5：恢复指引不降低错误等级；退出优先级保持 `1 > 3 > 2 > 0`。
- `docs/05-apply-engine.md` §9：显式 module 的恢复信息可附加，但 add 不修改 manifest。
- `.agent/plans/completed/m1-add-acceptance.md`：前一 acceptance fix 已固定 typed error identity、
  activation facts 与恢复文本，本计划只纠正 target-missing 的 CLI 分类。

有效 base 为 clean `main@15cdfcad48e1d3cb59cd4e4c7da4fc55be5af582`，branch
`fix/m1-add-acceptance`。最终三路 Checkpoint Acceptance 中 spec GO；safety 与 engineering 发现同根
P2：exact target-missing 被包装为 `ErrModuleActivation`，使 strict manifest error 从 exit 1 降为 3。

## Progress

- [x] 2026-07-22：确认分配 worktree、branch、effective base 与 clean 状态；读取 completed
  acceptance plan、typed Resolve/preflight/CLI 实现及最终 Acceptance finding。
- [x] 2026-07-22：以 `278420a` 提交本 active ExecPlan 起点。
- [x] 2026-07-22：先修改回归并确认旧实现把 exact target-missing 包装为
  `ErrModuleActivation`/exit 3；以 `e379b41` 抽出纯 activation steps，exact path wrap 原 typed
  manifest error 并附步骤，selection activation 继续使用 conflict identity。
- [x] 2026-07-22：manifest/add/CLI 普通、add/CLI 5 次重复、三包 race、定向 lint、branch-base 与
  checkpoint diff check、隔离 `make check`、Darwin/Linux amd64 add/CLI test binary 交叉编译通过。
- [x] 2026-07-22：未参与实现的 reviewer 对完整 `15cdfca...HEAD` branch 复审 GO，无 P0–P3
  finding；主线程确认 main 仍为 effective base、worktree clean。
- [x] 2026-07-22：主线程重跑最终 manifest/add/CLI tests、branch/checkpoint diff check 与隔离
  `make check` 通过；完成 Outcomes/Handoff 并迁移计划到 `completed/`。

## Milestones

### Milestone 1：提交独立计划起点

    docs(add): 新建 add acceptance exit-code ExecPlan

### Milestone 2：保持 strict error identity 并附恢复指引

先修改 preflight/CLI 回归证明 exact target-missing 的 typed manifest error 与 `next:` 文本可同时成立，
且 exit 1；no-`-m`/other module 仍原样 exit 1，普通 ambiguity、OS-inactive 与 nonmembership 仍 exit 3。
随后让 exact path wrap 原 `ModuleTargetMappingError` 并附 guidance，不再 wrap `ErrModuleActivation`。

    fix(add): 保持 target mapping 错误等级

## Validation and Acceptance

运行 manifest/add/CLI 普通测试、add/CLI 5 次重复、三包 race、定向 lint，
`git diff 15cdfcad48e1d3cb59cd4e4c7da4fc55be5af582...HEAD --check`、
`git diff 5d176497a75c9f8e43b413d43f04f3ea41720c51...HEAD --check`、隔离 cache/BINARY 的
`make check` 与 Darwin/Linux amd64 add/CLI test binary 交叉编译。真实 Linux/远端 CI 标记待验收。

当前上述命令均通过。`make check` 完成 tidy/format diff、0 lint issue、全仓 race、build 与 manifest
check；独立完整 review GO 后，主线程再次完成最终三包测试、两级 diff check 与隔离 `make check`。
真实 Linux 主机与远端 macOS/Linux CI 未运行，远端待验收。

## Safety, Authorization, and Recovery

用户已授权本 acceptance fix branch/worktree 的新计划、范围内修改、stage、commit 与验证。失败用
新 commit，不 amend/rebase/reset/cherry-pick/squash；不操作 main/其他 worktree，不接触真实数据。

## Interfaces and Dependencies

不新增依赖。manifest typed error 继续是 strict target 语义单一真相源；add 只附恢复文本；CLI 按
错误 identity 分类，不从文本推断等级。

## Surprises & Discoveries

- Observation: 恢复步骤与错误分类可以独立组合，无需为附加文本创建新的 error identity。
  Evidence: `fmt.Errorf("%w\n%s", targetErr, steps)` 保持 `errors.As` 命中
  `*manifest.ModuleTargetMappingError`，同时不再 `errors.Is(ErrModuleActivation)`。
  Impact: CLI 继续只按 error identity 判级；exact target-missing exit 1，selection activation 与普通
  ambiguity exit 3，文本不会意外改变 `1 > 3`。

## Decision Log

- Decision: target-missing guidance wrap 原 typed manifest error，不 wrap selection activation sentinel。
  Rationale: 恢复信息是诊断增强，不得把 strict manifest error 降级为 user conflict。
  Date: 2026-07-22

## Outcomes and Handoff

本 fix 已完成并通过独立完整复核。`e379b41` 把 activation guidance 拆为不携带错误身份的稳定步骤：
exact target-missing 以原 `*manifest.ModuleTargetMappingError` 为 error chain 根并附步骤，因此 CLI
保持 strict exit 1；no-`-m`/other-module 继续原样 exit 1；Resolve 成功后的 OS-inactive、
nonmembership selection activation 与普通 ambiguity 保持 exit 3。测试同时证明 dry-run 零写入、
错误 identity、隐私与 `1 > 3`。

本地已通过 manifest/add/CLI 普通、add/CLI 5 次重复、三包 race、定向 lint、branch-base/checkpoint
diff check、隔离 `make check` 与 Darwin/Linux amd64 add/CLI test binary 交叉编译。reviewer 对完整
branch 给出 GO，无 P0–P3 finding；主线程 freshness 与最终门禁通过。真实 Linux 与远端 CI 待验收。

本计划现迁移至 `completed/`；handoff 为主 agent 确认纯计划 closure/worktree clean，并按
acceptance-fix freshness 与 fast-forward 流程集成本地 main。无 unresolved blocking finding。
