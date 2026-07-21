# fix/m1-add-acceptance：恢复显式 module source mapping 退出码

本 ExecPlan 是 living document。实施期间持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，显式 `dot add -m` 已选择 module 但 prospective source candidate 为零或多个时，继续输出
稳定的 module/source 诊断并退出 1；只有未显式选择时零或多个 prospective candidate 才以
`ErrModuleAmbiguous` 退出 3。所有失败均保持 dry-run 零写入且不泄露 control-plane 绝对路径。

## Scope / Non-goals

范围内：

- 在 preflight 的 module/source selection 单一真相源区分显式 mapping failure 与 inference ambiguity。
- 测试显式 module 下零/多个 selected candidate 的 error identity、稳定诊断、CLI exit 1、dry-run
  零写入与隐私。
- 保持未显式 zero/multiple inference 的 `ErrModuleAmbiguous`/exit 3 以及既定 error matrix。

明确不做：

- 不改变 candidate 生成、module activation、manifest、source publication、持久化或 mutation 语义。
- 不依赖 CLI 文本判断错误类别，不新增依赖，不修改规范，不重开 completed plans。

## Contract and Context

- `docs/04-cli-spec.md` §4.5/§5：显式 `-m` mapping failure 是普通错误；未显式选择时零/多个候选
  是 conflict exit 3。
- `.agent/plans/completed/m1-add-acceptance-activation-exit-code.md`：CLI classifier 只将
  `ErrModuleAmbiguous` 投影为 exit 3。
- 当前 preflight 的 `candidateSelectionError` 无条件 wrap `ErrModuleAmbiguous`，因此显式 module
  selected candidates 为零或多个时被错误投影为 exit 3。

有效 base 为 clean `main@014052c2eac8a1f00914c4a2d0a7490f7ae84aa4`，branch
`fix/m1-add-acceptance`。本轮修复必须保留现有稳定 module/source 诊断，只收紧 error identity。

## Progress

- [x] 2026-07-22：确认 worktree、branch、effective base 与 clean 状态；定位 preflight selection
  单一真相源、CLI classifier 和现有 inference/explicit 回归。
- [ ] 提交本 active ExecPlan 起点。
- [ ] 先补 preflight 与 CLI 回归，证明显式 selected zero/multiple 当前误带
  `ErrModuleAmbiguous` 并退出 3。
- [ ] 在 preflight 单点区分 explicit mapping failure 与 inference ambiguity，形成 fix commit。
- [ ] 运行窄测试、count 5、race、两级 diff check、隔离 `make check` 与 Darwin/Linux add/CLI
  test binary 交叉编译；保持 active 与 worktree clean，交独立 spec/safety review。

## Milestones

### Milestone 1：提交独立计划起点

    docs(add): 新建 explicit source exit-code ExecPlan

### Milestone 2：固定显式 mapping failure 分类

先让 add package 回归覆盖显式 module 下零/多个 selected candidate：诊断稳定、不满足
`errors.Is(ErrModuleAmbiguous)`；让 CLI dry-run 回归固定 exit 1、零写入与隐私。同时保留未显式
zero/multiple inference 的 sentinel/exit 3。随后在 preflight selection 单一真相源做最小实现改动。

    fix(add): 恢复显式 source mapping 退出码

### Milestone 3：记录验证与复核交接

将实现、测试、门禁、平台证据和未验证项写回本 living plan，形成不夹带实现的证据 commit；计划保持
active，等待独立 spec/safety review。

    docs(add): 记录 explicit source 退出码修复证据

## Validation and Acceptance

至少运行：

    go test ./internal/manifest ./internal/add ./internal/cli
    go test -count=5 ./internal/add ./internal/cli
    go test -race ./internal/manifest ./internal/add ./internal/cli
    git diff 014052c2eac8a1f00914c4a2d0a7490f7ae84aa4...HEAD --check
    git diff 5d176497a75c9f8e43b413d43f04f3ea41720c51...HEAD --check
    make check

`make check` 使用隔离 cache 环境。额外交叉编译 Darwin/Linux amd64 的 add/CLI test binary；真实 Linux
与远端 macOS/Linux CI 未实际运行时明确标记待验收。

## Safety, Authorization, and Recovery

用户已授权本 branch/worktree 的新 active plan、范围内修改、stage、commit 与验证。失败使用新
commit，不 amend/rebase/reset/cherry-pick/squash；不操作 main/其他 worktree，不接触真实数据。

## Interfaces and Dependencies

不新增依赖。preflight selection 是 error identity 的单一真相源；CLI 继续只基于 sentinel 分类，不检查
错误文本。

## Surprises & Discoveries

尚无。

## Decision Log

- Decision: 只让未显式 inference failure wrap `ErrModuleAmbiguous`；显式 mapping failure 保留同一稳定
  诊断但使用非 ambiguous error。
  Rationale: error identity 应表达公开冲突分类，而诊断文本独立承载具体 module/source 恢复信息。
  Date: 2026-07-22

## Outcomes and Handoff

尚未完成；等待计划起点、测试、实现、验证与独立复核。
