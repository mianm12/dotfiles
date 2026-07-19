# refactor/m1-planner-contract：固化输出失败边界与 file action contract

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，维护者可以明确区分“完整业务 projection”与“stdout/stderr 传输失败”：只读计划仍在
完整形成后才输出，两个 stream 不伪装成可回滚事务，任一写失败仍稳定退出 1，`status` 的可信
verdict 前置规则和 `doctor`/`version` 的诊断例外保持不变。同时，planner 与未来 executor 之间
不再暴露含 prune/hook 残余分支的泛化 `Action`；file、prune、hook 三个执行阶段分别使用明确的
concrete action contract，组合校验会拒绝不完整的 source Precondition、矛盾的 file decision
和错误的 state effect。

## Scope / Non-goals

范围内：

- 澄清 `docs/04-cli-spec.md` §5 的跨 stream 写失败边界，并以 plan CLI 回归证明 stderr 写失败
  返回 1、已成功写出的完整 plan stdout 不回滚；正常 stdout/stderr 字节和顺序不变。
- 将 planner 的共享 file `Action`/`ActionVerb`/`ActionReason` 完整收窄为
  `FileAction`/`FileVerb`/`FileReason` 类型族，移除已经由 `PruneAction`、`HookAction` 取代的
  verb 和不再表示真实分支的 `HasDesired`。
- 保持 `Precondition`、`StateEffect` 作为 file/prune 的真实共享 contract，并补齐 file verb 的
  source requirement、prune 的零 source requirement、封闭 file reason 与 canonical decision
  校验。
- 更新 planner/CLI 测试与调用方，保持所有公开计划内容、退出码、排序、纯只读和数据保护性质。

明确不做：

- 不实现或预建 executor、mutation、state builder、backup、真实 apply、add 或新依赖。
- 不引入通用 action interface、generic union、`FilePlan` wrapper、filesystem/output abstraction，
  也不把 stdout/stderr 缓冲成伪事务。
- 不改变 decision、ownership、L/S/P、kind migration、prune/hook、持久化格式或正常 CLI 输出。
- 不重写 completed ExecPlans 或既有 commits，不修改 README 中的能力边界。

## Contract and Context

- `docs/02-architecture.md` §4–§6：planner 必须只读并以自包含动作与 executor 通信；file、prune、
  hook 的成功/失败 state 处置由计划决定，executor 不重新解释 manifest。
- `docs/04-cli-spec.md` §3/§4.3–§4.4/§4.6/§4.10/§5：输出错误优先级为 1；status 的可信 verdict
  不得与错误并存，doctor/version 仍应提供安全诊断，stdout 与 stderr 承担不同内容。
- `docs/05-apply-engine.md` §3–§5/§8：file、prune、hook 是不同执行阶段；target/source Precondition
  和 state effect 必须完整，不能把 action family 混为一套可选字段协议。
- `docs/08-testing.md` §1/§3：测试固定公开结果和安全性质，不把 helper 调用顺序发明成产品语义。
- `docs/09-roadmap.md` §1/§3：当前仍是 M1 link/scaffold planner contract；内部类型调整不扩大
  executor 或 M2/M3 能力。

基线是 clean `main@6322af40e3b256f96e228b3a126a181ce2989f5b`，本地 main 比
`origin/main@bd6f4fcc05a6cd8db2fda1b2fb84baebfb11ab4a` ahead 68；本任务不 fetch、pull、push。
`ApplyPlan` 当前分别保存 file action slice、`PrunePlan` 和 `HookPlan`，但早期共享 model 的
`ActionVerb` 仍保留未消费的 prune/run-hook 常量，且所有合法 `Action` 都强制
`HasDesired=true`。`validateFileActions` 已复核 target snapshot/state effect，但尚未验证 source
requirement 的封闭形态；CLI presentation 能在后续映射错误前保持零输出，但跨 stream 写入由
`commandOutput` 最终汇总，不能也不应回滚已经写出的字节。

## Progress

- [x] 2026-07-19：确认 main/worktree clean、branch 不存在、基线与 origin 状态；建立
  `refactor/m1-planner-contract` 独立 worktree。
- [x] 2026-07-19：提交本 active ExecPlan，固定范围、里程碑、验证与停止边界。
- [x] 2026-07-19：以 `cdb9868` 澄清跨 stream 非事务边界，以 `dee1db7` 固定 diff/dry-run
  clean/actionable development notice 写失败语义；CLI 窄测通过且隔离树不变。
- [x] 2026-07-19：以 `8025f91` 收窄完整 file action 类型族，移除残余 verb 与
  `HasDesired`；planner/CLI 联合测试通过，旧类型族 `rg` 无残留。
- [x] 2026-07-19：新增 mutation 回归先证明五类畸形 plan 均被旧校验接受，再以 `4751862`
  闭合 file source、prune 零 source 与 file reason 结构校验；窄测和联合测试通过。
- [x] 2026-07-19：完成窄测、planner/CLI 20 次重复、全仓 race、完整 diff check 与两次
  `make check`；本地 Darwin/arm64 全部门禁通过。
- [x] 2026-07-19：独立 reviewer 首轮发现合法 enum 可组成矛盾 decision 的 P2；以 `5e931be`
  复用纯 `Decide` 校验 canonical tuple 和完整 state effect，新增三项先失败 mutation 回归；
  reviewer 第二轮重审完整分支 GO，无 P0–P3 finding。
- [x] 2026-07-19：完成 Outcomes/Handoff，将本计划迁入 `completed/`，进入纯计划收口 commit。
- [x] 2026-07-19：合并前独立复核发现 prune canonical validation P1 与一个 FileAction 测试
  命名 P3；按用户授权以 `3f9348b` reopen，在原分支以新 commits 完成 test→fix→review，
  不先合入 main。
- [x] 新增五项 mutation 回归，先证明旧校验会接受 P3 target delete、deferred action 激活、
  action/group 丢失和错误 `WouldDeleteTarget`；以 `69609c5` 重算并校验 canonical prune plan，
  以 `4930503` 修正 `FileActionClone` 测试命名。
- [ ] 重新完成重复测试、完整门禁、独立复审和 plan closure。

## Milestones

### Milestone 1：固定跨 stream 输出失败边界

目标是让规范与测试准确表达当前命令语义，而不是用输出重排制造不存在的原子保证。在
`docs/04-cli-spec.md` §5 明确 stdout/stderr 是独立 stream：任一写失败最终退出 1，已经成功写出的
内容不回滚；命令章节明确要求可信 verdict 前置时，必须先检查其必要输出。在
`internal/cli/plan_test.go` 使用 development notice 的失败 writer 证明 diff/dry-run 得到 exit 1，
但此前完整形成并成功写出的 stdout 保持；保留 status、doctor、version 既有差异化回归。正常输出
内容和顺序不得改变。

Concrete steps：

    在 repo root 运行：go test ./internal/cli -run 'Test(ReadOnlyPlan|Status|Doctor|Version)'
    预期：全部通过；新增回归在旧代码缺少规范证据，但不要求改变正常输出实现。

验收：

- docs 不承诺跨 stream 事务或回滚，也不放宽任何输出错误的 exit 1。
- status 仍在 notice 失败时零可信 verdict；doctor/version/plan 的安全输出保留。

Commit 边界：

    docs(cli): 明确跨流输出失败边界
    test(cli): 固定计划输出失败语义

### Milestone 2：建立明确的 file action 类型族

目标是让 `ApplyPlan` 的三个阶段在类型层清晰对应 `FileAction`、`PruneAction`、`HookAction`，不再
保留早期总和模型残余。直接重命名 file 类型、枚举及常量，不建立 alias/adapter；删除
`ActionPrune`、`ActionRunHook` 和 `HasDesired`，更新 decision、prune composition、apply validation、
CLI presentation/status 与全部测试。`Desired`、`Observation`、`Precondition`、`StateEffect` 继续
表达现有真实共享事实，不作无证据扩展。

Concrete steps：

    在 repo root 运行：go test ./internal/planner ./internal/cli
    预期：全部通过；rg 不再发现旧 Action 类型族、两个残余 verb 或 HasDesired。

验收：

- `ApplyPlan.FileActions()` 只返回 `[]FileAction`；prune/hook 继续各自 concrete type。
- 不存在 compatibility alias、泛化 action interface 或行为/输出变化。

Commit 边界：

    refactor(planner): 收窄 file action contract

### Milestone 3：闭合 action Precondition 结构校验

目标是让 future executor 只消费结构完整的 valid `ApplyPlan`。先增加内部 mutation 回归，证明
create-link/backup-replace 缺少或错配 regular source requirement、其他 file verb 携带 source
requirement、prune 携带 source requirement、未知 non-empty file reason 都被 combined validation
拒绝；再在 `internal/planner/apply_plan.go` 集中实现形态校验。独立 review 进一步证明封闭 enum
仍能组成矛盾 tuple；最终组合层调用纯 `Decide` 复核语义与 state effect，不手写第二份 L/S/P。

Concrete steps：

    在 repo root 运行：go test ./internal/planner -run 'TestValidateApplyPlan'
    在 repo root 运行：go test ./internal/planner ./internal/cli -count=20
    预期：所有结构破坏回归稳定被拒绝，正常计划保持确定。

验收：

- source requirement 仅出现于规范允许的 file verb，路径与 desired source 一致。
- prune source requirement 始终为空；未知 file reason 不能进入 valid plan。
- combined validation 不复制 decision/prune/hook 业务规则；file 语义一致性直接复用纯 `Decide`。

Commit 边界：

    fix(planner): 闭合 action 前提结构校验

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| 正常 CLI 输出/退出码不变，写失败仍为 1 | CLI 窄测与新增 stderr failure 回归 | 通过 |
| 三个 action family 边界明确且无兼容残余 | `rg`、planner/CLI 编译与测试 | 通过 |
| source Precondition/closed reason fail closed | combined validation mutation tests | 通过 |
| prune action/group 与 canonical plan 完全一致 | 五项 mutation 回归与纯 `PlanPrune` 重算 | 窄测通过 |
| planner/diff/status 保持只读与完整数据流 | 既有隔离 fixture、重复测试、race | 通过 |
| 当前平台完整门禁 | `make check BINARY=/private/tmp/dot-m1-planner-contract-check` | 通过 |
| 完整任务 diff 可审阅 | `git diff 6322af4...HEAD --check` 与独立复核 | 通过 |

最终在 Darwin/arm64 运行窄测、`-count=20`、race、完整 diff check 与 `make check`。远端 macOS/
Linux CI 未由本任务运行时，准确记录“本地验收通过、远端待验收”，不以交叉编译代替实机证据。

## Safety, Authorization, and Recovery

用户于当前任务明确要求按已确认方案实施，范围覆盖本 branch/worktree 内的计划、规范澄清、CLI
测试、planner/CLI 内部类型与校验、stage、语义 commits、独立复核和计划生命周期收口；本计划不
产生后续授权。所有 branch/worktree 操作由主 agent 在 `/private/tmp/dot-m1-planner-contract-019f795e`
串行完成，不读取或修改真实 modules、机器配置、state、backup、`.env` 或主力 HOME。测试只使用
既有 `t.TempDir()` 隔离 fixture；`make doctor-manifest` 由 Makefile 创建假 HOME。

本任务不执行真实 mutation 命令。失败保留最近成功 commit，以新 fix commit 修复；不 amend、
rebase、cherry-pick、squash、reset、force、push、PR 或删除 branch。worktree 必须保持 clean，只有
在确认分支已按当前用户授权完成交付后才考虑安全移除；是否合入 main 不由本计划自行扩展。

## Interfaces and Dependencies

不新增依赖。跨 package contract 是 `planner.ApplyPlan.FileActions() []FileAction`、现有
`PrunePlan`/`PruneAction` 和 `HookPlan`/`HookAction`；CLI 只消费这些 typed values 做 projection。
`Precondition` 与 `StateEffect` 继续由 file/prune 共享，因为二者真实复用同一 target snapshot 和
state entries 处置域。未来 executor 必须消费 valid `ApplyPlan` 并执行提交前复核，本任务不规定其
package、循环或 IO helper。

## Surprises & Discoveries

- Observation: stdout/stderr 的非事务行为不是 plan 特例。
  Evidence: version 与 doctor 已有 stderr failure 回归保留安全 stdout；status 规范另行要求可信
  verdict 的 notice 前置。
  Impact: 不实现全局 diagnostics-first 或 output transaction，只澄清命令级边界。

- Observation: `Precondition` 的 source 字段不是 prune/hook union 残余，而是 file/prune 共享 target
  校验之上的显式可选 source requirement。
  Evidence: create-link/backup-replace 要求 source 仍为普通文件，prune 只复用 target resolution 与
  observation；已有 ExecPlans 明确要求共享 Precondition。
  Impact: 保留共享类型，通过 combined validation 闭合 allowed shape，不预建新的类型层。

- Observation: combined validation 只比较 target snapshot，确实会接受缺失/错配 source、未知
  file reason 和 prune source 残留。
  Evidence: `TestValidateApplyPlan_RejectsInvalidActionShape` 在修复前五个子场景均得到 nil error，
  修复后全部稳定拒绝。
  Impact: 这是 future executor 边界上的真实 contract bug，当前修复收益高且不改变任何正常计划。

- Observation: 封闭 `FileVerb` 与 `FileReason` 仍不足以证明二者与 desired/observation 构成同一个
  决策；错误的合法组合甚至可以把真实 conflict 伪装成 presentation 会忽略的 skip。
  Evidence: 独立 reviewer 构造 `FileSkip + FileReasonTargetMissing` 后，首轮 combined validation
  仍返回 nil；跨 desired kind verb 和被污染的 state entry 同样可通过。
  Impact: 以纯 `Decide` 作为唯一 semantic oracle，并保留独立 payload shape 校验；三项 mutation
  回归在修复前失败、`5e931be` 后通过，第二轮完整 review GO。

- Observation: prune 的字段级结构校验没有证明 action 集合、deferred 状态和 confirmation groups
  是输入的唯一合法投影，因而会接受危险升级或不完整计划。
  Evidence: `TestValidateApplyPlan_RejectsNonCanonicalPrunePlan` 的五个场景在修复前都得到 nil error：
  P3 unowned 被改成 target delete、deferred action 被激活、action 被遗漏、group 被遗漏，以及
  `WouldDeleteTarget` 被改写。
  Impact: validation 保留独立 payload shape 检查，再调用纯 `PlanPrune` 重算 canonical actions/groups
  并逐项比较；不复制 prune ownership、P1/P2/P3 或 grouping 规则。

## Decision Log

- Decision: 保持 command-specific output semantics，不建立跨 stream 事务抽象。
  Rationale: 两个 stream 无法原子提交，doctor/version 的诊断职责与 status 的 verdict 前置具有规范
  差异；完整 projection 与最终 write-error priority 已是正确边界。
  Date: 2026-07-19

- Decision: 使用三个 phase-specific concrete action family，并完整重命名 file family，不保留 alias。
  Rationale: file、prune、hook 的执行顺序和 state 域不同；Go concrete types 比 interface/type switch
  union 更清晰，当前尚无 executor 消费者，是消除早期泛化残余的最低迁移成本时点。
  Date: 2026-07-19

- Decision: combined validation 独立校验 payload shape，并调用纯 `Decide` 证明 file semantic
  tuple 与完整 state effect 一致。
  Rationale: 只检查 closed enum 不能阻止不可能组合；直接复用 `Decide` 保持 ownership、L/S/P
  单一真相源，也避免手写 verb/reason/kind 矩阵。`Decide` 无 IO、mutation 或 validation 回调，
  不形成递归或 executor 重解释。
  Date: 2026-07-19

- Decision: planning 与 validation 共用 `pruneOptionsForScope`，validation 以纯 `PlanPrune` 作为
  canonical oracle，并继续保留 plan 自身的结构检查。
  Rationale: 这样让 prune ownership、P1/P2/P3、deferred 与 confirmation grouping 只有一个业务
  真相源，同时仍能给畸形 payload 明确报错；`PlanPrune` 无 IO、mutation 或 validation 回调，
  不形成递归，当前规模下重算成本可忽略。
  Date: 2026-07-19

## Outcomes and Handoff

本计划因合并前复核发现有效 P1/P3 于 2026-07-19 reopen。下述内容是首次 closure 时的历史
handoff；在 canonical prune plan 修复、完整复审与第二次 closure 完成前，本分支不再是
merge-ready，main 继续保持在既有基线。

本任务已在 `refactor/m1-planner-contract` 完成本地交付，main 保持 clean 且仍位于基线
`6322af40e3b256f96e228b3a126a181ce2989f5b`；本计划未自行扩大授权合入 main。实现提交为：

    cdb9868 docs(cli): 明确跨流输出失败边界
    dee1db7 test(cli): 固定计划输出失败语义
    8025f91 refactor(planner): 收窄 file action contract
    4751862 fix(planner): 闭合 action 前提结构校验
    5e931be fix(planner): 校验 canonical file decision

交付结果是三个明确的 concrete action family；`FileAction` 不再携带 prune/hook 残余或冗余
`HasDesired`。正常 plan/diff/dry-run/status 输出与退出码不变；跨 stream 写失败边界得到规范和
零写入回归。combined validation 会拒绝 source shape、prune source、未知 reason、不可能
verb/reason/kind tuple 及错误 state effect，同时不复制 L/S/P 决策表。

最终本地证据：

    go test ./internal/planner -run 'TestValidateApplyPlan' -count=1
    go test ./internal/planner ./internal/cli -count=20
    git diff 6322af40e3b256f96e228b3a126a181ce2989f5b...HEAD --check
    make check BINARY=/private/tmp/dot-m1-planner-contract-check

以上均退出 0；`make check` 包含 tidy/fmt check、lint 0 issues、全仓 race、build 与隔离
doctor-manifest。未参与实现的 reviewer 从基线审查完整分支，首轮 P2 以新 commit 修复，第二轮
完整重审 GO，无 unresolved P0–P3 finding；其独立 `make check` 也通过。本机实际运行平台为
Darwin/arm64；未 push，远端 macOS/Linux CI 与 Linux 实机未运行：本地验收通过、远端待验收。

未实现 executor、mutation、state builder、backup、真实 apply/add、M2/M3 能力或新依赖；这些
仍按 roadmap 留待后续 Checkpoint。
