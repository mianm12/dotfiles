# fix/m1-apply-core-acceptance：闭合完整 target-set 自穿越校验

本 ExecPlan 是 living document。实施期间持续更新 `Progress`、`Surprises & Discoveries`、
`Decision Log` 和 `Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

修复 CP4 整体 Acceptance 发现的完整 desired target-set 校验缺口。完成后，每个 target 除了与
其他 target 做 identity/ancestor pairwise 比较，还必须证明自己的展示路径不会在到达 leaf 前
穿过同一个 leaf identity。该结构性不变量在完整 effective profile 上成立，因而 partial apply、
S1b state-only adopt 和普通 target mutation 都不能通过 scope 缩小或 execution class 绕过；失败
发生在 planner/executor 之前，整个运行零 mutation。

## Scope / Non-goals

范围内：

- 在共享 `paths.ValidateTargetSet` 单一入口验证每个 resolution 的 self traversal。
- 为单 target 拓扑错误提供保留 provenance 且 `errors.Is(err, ErrTargetOverlap)` 成立的无歧义诊断。
- 以 paths 单元测试固定单 target 反例和普通 alias 正例。
- 以真实隔离 `apply.Run` 固定未选中模块与 S1b state-only 两条绕过路径，断言 executor、target、
  state 均零 mutation，并验证重复运行保持同一诊断。

明确不做：

- 不改变 target identity、pairwise relation、state v1、ownership、scope、CLI、输出或退出码。
- 不把同一检查复制到 manifest/planner/executor，不放宽现有 action-level persisted-state 恢复门禁。
- 不引入 filesystem abstraction、第三方依赖、恶意环境加固或 M2/M3 能力。

## Contract and Context

- `docs/05-apply-engine.md` §5.1–§5.4：完整 effective profile 的结构性 desired 在 execute 前整体
  校验；部分 apply 不得缩小；中间路径穿 desired file leaf 时一个动作都不执行。
- `docs/02-architecture.md` §4–§6：全局 plan validation 失败不能进入 executor。
- `docs/08-testing.md`：覆盖 mutation boundary、部分 apply、恢复和第二次运行零 mutation。

有效基线是 clean `main@34f86091ccd8e1adb5c8018b62a9f983486731ce`；
`fix/m1-apply-core-acceptance` 精确指向该基线且仍由本 Checkpoint 所有，本轮继续复用。未
fetch/pull；`origin/main@e9e8bac`，无 upstream。worker worktree 是
`/private/tmp/dot-m1-cp4-self-traversal`。coordinator 已因本轮 Acceptance finding 在其独立
worktree 以 `fa1f137` reopen，主 main 尚未前进。

根因是 `validateTargetSet` 解析完整集合后只进入 `leftIndex/rightIndex` pair loop；集合只有一个
target 时没有任何 topology predicate 被调用。上一轮新增的
`validateTargetMutationStateReachability` 能表达 `resolution.Traverses(resolution)`，但它只处理
scoped actions 中的 `FileTargetMutation`，职责是保护 state publish 恢复窗口，不能替代完整
desired 的结构校验。

## Progress

- [x] 2026-07-20：验证 reviewer fixture、规范和完整 profile→target-set→scope→action 数据流；
  finding 是共享结构校验遗漏，不是规范冲突。
- [x] 2026-07-20：从 current clean main 复用 acceptance-fix branch/worktree，建立本计划。
- [ ] 2026-07-20：先提交 paths、partial apply 与 S1b state-only 失败回归，证明旧实现会放行。
- [ ] 2026-07-20：在共享 target-set validator 实现 unary self-traversal gate，运行窄测和完整门禁。
- [ ] 2026-07-20：完成独立 review → fix → review、freshness、plan closure 和 FF-only main 集成。
- [ ] 2026-07-20：重新验收完整 `checkpoint_base...main`，更新 coordinator 最终交接。

## Milestones

### Milestone 1：固定完整 profile 的 unary topology 反例

paths fixture 使用 `bridge -> real/`、`detour -> bridge/..` 和唯一 target `detour/bridge`；最终 leaf
identity 与 `bridge` 相同，解析轨迹又在 `..` 折返前经过 `bridge`。旧 validator 因无 pair 必须
错误成功，新增测试先失败。

真实 runner fixture 包含 `bad`/`good` 两个 effective modules：只选择 `good` 时，未选中的 `bad`
self-traversal 仍应让完整 profile fail closed，且 `good` target 不创建。另一场景让 `bad` scaffold
已存在并走 S1b state-only，仍必须在 state Store 前拒绝。环境完整隔离 HOME/XDG/DOT/repo/state，
并比较 target/state bytes 与 metadata。

### Milestone 2：让共享 target-set 表达 unary 与 pairwise 拓扑

解析每个 labeled target 后立即验证 `resolution.Traverses(resolution)`；失败返回零值集合、保留
label/path provenance 并 wrap `ErrTargetOverlap`。使用独立 unary error shape，而不把同一 target
伪装成 `TargetConflictError` 的左右双方，也不扩张 `TargetRelation` 的 pair 语义。pairwise loop、
resolver snapshot 和上层 manifest/control boundary 入口保持不变。

### Milestone 3：复核、freshness 与 Checkpoint 重验收

实现完成后由未参与实现的 reviewer 从 `34f8609...HEAD` 完整检查规范、数据保护和 Go/双平台；
有效 finding 使用新 fix commit，最多三轮，不重写历史。通过后迁移本计划并创建纯计划 closure，
freshness 仍等于有效 base 才 FF-only 合入 main；随后对 `e9e8bac...main` 重新执行三路完整
Acceptance。

## Validation and Acceptance

    go test ./internal/paths -run 'TestValidateTargetSet_(RejectsSelfTraversal|DoesNotInventRelations)'
    go test ./internal/apply -run 'TestRun_RejectsSelfTraversingEffectiveTargetBeforeExecutor'
    go test ./internal/paths ./internal/manifest ./internal/planner ./internal/apply
    go test -race ./internal/paths ./internal/manifest ./internal/planner ./internal/apply
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-m1-cp4-self-traversal-linux.test ./internal/apply
    git diff 34f86091ccd8e1adb5c8018b62a9f983486731ce...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp4-self-traversal-check/dot

验收必须证明 full/partial/state-only 三条入口同样 fail closed、普通 alias 继续合法、重复运行零
mutation。远端 macOS/Linux CI 未实际运行时只报告“本地验收通过、远端待验收”。

## Safety, Authorization, and Recovery

所有 mutation tests 使用 `t.TempDir()` 和完整隔离的 HOME/XDG/DOT/repo/config/state；不读取或
修改真实 `modules/`、`*.local`、machine config、state、backup、`.env` 或主力 HOME。失败保留
最近成功 commit，以新 commit 修复；不 amend、rebase、cherry-pick、squash、reset、force、
fetch、pull、push 或删除 branch。若实现要求改变规范、公开行为、identity 语义或持久化格式，
立即停止请求裁决。

## Interfaces and Dependencies

不新增依赖。`paths.ValidateTargetSet` 继续拥有完整 target identity/topology 的单一真相源；manifest
与 control boundary 继续只消费成功验证的零/完整集合；planner action-level traversal gate 继续只
负责 target mutation→state publish 的旧 state 可恢复性。

## Surprises & Discoveries

- Observation: `TargetResolution.Traverses` 已准确保存 equal-leaf detour，但首次只在 planner 的
  target-mutation 恢复门禁使用。
  Evidence: `internal/paths/identity_topology_test.go` 与
  `internal/planner/apply_plan.go:validateTargetMutationStateReachability`。
  Impact: 本次不修改 resolver；把既有事实提升到更早、更广的 target-set invariant。

- Observation: partial apply 的 module scope 在完整 profile path validation 之后才形成，但 unary
  topology 从未在完整入口检查。
  Evidence: manifest `validateTargetStructure`/`ValidatePathBoundaries` 调用 `ValidateTargetSet`，后者
  只有 pair loop；action gate 又只看 scoped mutation actions。
  Impact: 修复共享 validator 后 full/partial/doctor 等所有规范消费者自然一致，无需 adapter。

## Decision Log

- Decision: 在 `ValidateTargetSet` 解析每个 target 后做 self-traversal gate。
  Rationale: 这是完整 structural desired 的 unary invariant；这里最早拥有同一 resolver snapshot、
  provenance 和所有调用方，能避免 scope/execution-class 绕过。
  Date: 2026-07-20

- Decision: unary 错误独立表达，继续 wrap `ErrTargetOverlap`，不复用 pairwise
  `TargetConflictError`/`TargetRelation`。
  Rationale: 把同一 input 填成 left/right 会制造歧义；新增 relation bit 又会污染明确的双方关系
  模型。结构化 unary provenance 与现有 Go error chain 更清晰。
  Date: 2026-07-20

## Outcomes and Handoff

进行中。尚未声称修复或验收完成。
