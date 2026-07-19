# feat/apply-planner：组合 M1 纯只读完整计划

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，调用方只需给出 runtime overrides、CLI version、module scope 与 M1 file/prune options，
即可通过唯一入口得到稳定、自包含的只读 apply plan。入口严格加载 manifest/config/state，先对
完整 effective profile 形成结构 desired 并完成全局路径校验，只渲染请求 scope，再组合 target
observation、state alias、L/S/kind migration、P1–P3 与 run_once hook。任一阶段失败都返回零计划；
整个过程不取锁、不写 target/state/temp/backup，也不执行 hook。

## Scope / Non-goals

范围内：

- 在 `internal/planner` 新增唯一 public apply-plan 组合入口和不可变结果值，复用
  `runtime.LoadReadOnly`、manifest validated scope、`ObserveProfileTargets`、`Decide`、
  `PlanPrune` 与 `PlanHooks`。
- 对完整 profile 校验 collision、target identity/topology 和控制面边界；module scope 只缩小
  scaffold render、file decisions、prune candidates 与 hooks。
- 把 scope 内 rendered desired 精确回填到完整结构 desired，保留完整 desired 用于 state alias
  join 与 prune whole-module 判断；file actions 只从 scope targets 产生。
- 保存 profile/OS/arch/hostname/HOME/repo/requires、scope、unassigned modules、完整 observation、
  file/prune/hook actions，提供不共享 slice/bytes 的 presentation getters。
- 在返回前整体校验 action/state-effect/Precondition、scope、顺序及 hook context；任何不完整或
  不支持的 managed/rendered 输入 fail closed。

明确不做：

- 不接 Cobra、输出或退出码；`diff`、`apply --dry-run` 与 `status` 由后续 Milestone 投影同一 plan。
- 不取 lock、不执行 file/prune/hook、不写 state、不创建 backup/temp/target/source。
- 不实现 managed/rendered、文本 diff、executor、add、workflow、filesystem abstraction、依赖或
  M2/M3；不复制 ownership、Precond、prune 分类或 hook fingerprint 逻辑。
- 不修改规范、持久化格式、既有共享 planner/manifest/runtime/state contract。

## Contract and Context

- `docs/02-architecture.md` §4–§6：只读 pipeline 为 load→完整 desired→scoped render→scan→
  decide→validate，动作必须自包含；dry-run/diff 无锁零写入。
- `docs/03-manifest-spec.md`：partial scope 不能缩小完整 profile 的 collision/path validation；M1
  string run_once 与 link/scaffold 是当前可消费输入。
- `docs/04-cli-spec.md` §3、§4.2–§4.4、§5：本计划将成为后续 diff/dry-run/status 的单一事实源，
  但本分支不映射公开输出与退出码。
- `docs/05-apply-engine.md` §1–§5、§8、§10：L/S/P、alias、kind migration、prune 收敛门控与
  hook conflict 独立性由既有组件表达；本层只组合并校验 payload。
- `docs/06-templates.md`：scaffold 只在 action scope fail-fast render，plan 保存完成 bytes。
- `docs/08-testing.md`、`docs/09-roadmap.md` §1 M1/§3：覆盖完整 profile/partial、alias、迁移、
  fail-closed、确定顺序和整树零写入，不预建 executor 或 managed。

基线是 clean `main@385dea875b7e5ffacef34a9e39490704ab090d88`。此前 target observation、
decision、prune 与 hook Milestones 已各自完成独立 review 并合入：`ValidatedProfile` 绑定完整
path validation 与 HOME，`ScopedProfile` 只渲染 module scope；`ObservedProfile` 对完整 desired
与 strict state 做 identity/alias join；`Decide` 是 ownership 与 L/S/kind migration 的唯一真相源；
`PlanPrune` 按显式 scope 过滤 orphan 但用传入 profile 的完整 desired 判断 whole-module groups；
`PlanHooks` 只读取 scoped descriptors/state/script，不受 file conflict 门控。

## Progress

- [x] 2026-07-19：确认 worktree/Git 顶层均为
  `/private/tmp/dot-cp3-apply-planner-019f795e`，branch `feat/apply-planner` clean，
  `HEAD == main == 385dea8`。
- [x] 2026-07-19：读取仓库规则、指定 CP3 规范、coordinator 上下文、四个前置 completed plans、
  runtime/manifest/planner/state 实现与相关测试；未发现规范或共享 contract blocker。
- [x] 2026-07-19：以 `ccf487a docs(planner): 建立 apply planner 执行计划` 提交 active
  ExecPlan 起点。
- [x] 2026-07-19：测试先行组合完整结构 desired、scope render、observation 与 scope-only file
  decisions，以 `c71fd7d feat(planner): 组合完整 desired 与 scope 决策` 提交。
- [x] 2026-07-19：测试先行接入 strict runtime load、prune/hooks、整体 validation 与不可变
  presentation input，以 `3565be4 feat(planner): 建立纯只读 apply 计划入口` 提交。
- [x] 2026-07-19：窄测、相关五包 20 次/race、darwin/linux amd64 编译、branch diff check 与
  `make check` 全部通过；计划保持 active，等待 coordinator 安排独立 review。
- [x] 2026-07-19：review round 1 提出有效 blocking P1：active P2 orphan prune 可删除完整
  desired 的祖先 symlink。full/partial filesystem regressions 先在旧实现失败；以 fix commit
  `864c828` 增加完整 desired × active delete topology 校验。
- [x] 2026-07-19：P1 fix 后相关五包 20 次/race、darwin/linux amd64 编译、完整 branch diff
  check 与 `make check` 再次通过；等待 round 2 完整 branch review。
- [x] 2026-07-19：未参与实现的 reviewer 从 `385dea8...HEAD` 完整执行 round 2，确认首轮 P1
  与 full/partial/非 active 对照边界均正确，结论 GO、无 P0–P3 finding；主线程随后重复五包
  20 次、完整 diff check 与 `make check`，全部通过。

## Milestones

### Milestone 1：提交 ExecPlan 起点

单独提交本计划，固定 base、数据流、共享职责边界、失败特征与验证方式。

验收：拟提交 diff 只包含 `.agent/plans/active/m1-apply-planner.md`。

Commit 边界：

    docs(planner): 建立 apply planner 执行计划

### Milestone 2：组合完整 observation 与 scope file decisions

先增加 package-level 测试，使用真实 manifest、target 和 strict state fixture，证明完整 entries 中
只有 scope scaffold 得到渲染 bytes，state alias/kind migration 仍经完整 identity join，file action
只含 scope modules 且稳定排序。随后在新 `internal/planner/apply_plan.go` 中增加私有组合 helper；
scope 回填只按 manifest 已保证唯一的 module/source identity 匹配，不重新解释 path 或 ownership。

Concrete steps：

    go test ./internal/planner
    go test -count=20 ./internal/planner

验收：partial 不决策未请求 module；完整 collision 在 scope 前失败；未请求 template render 错误不
阻塞 partial；scope alias 和 symlink↔scaffold migration 形成既有 `Decide` 结果；任一错误返回零值。

Commit 边界：

    feat(planner): 组合完整 desired 与 scope 决策

### Milestone 3：建立唯一 apply-plan 入口并组合 prune/hooks

先增加通过真实 `runtime.LoadReadOnly` 的集成测试，覆盖 full/partial、invalid manifest/state、
managed/rendered fail closed、conflict 仍有 hook、prune whole-module group、确定顺序和前后整树快照。
再实现 public `PlanApply` 与 immutable `ApplyPlan`/context getters，按固定 pipeline 调用前置组件并在
返回前执行结构 validation。validation 只检查各前置组件已经决定的封闭 payload 一致性，不重新
计算 ownership、Precond、fingerprint 或 P 行。

Concrete steps：

    go test ./internal/planner ./internal/runtime ./internal/manifest ./internal/state
    go test -count=20 ./internal/planner

验收：每个 error 都返回零可信 plan；结果包含完整 observation 和 scope file/prune/hook action；
hook 不因 file conflict 消失；调用前后 fixture tree 完全相同，state/lock/temp/backup 均不创建。

Commit 边界：

    feat(planner): 建立纯只读 apply 计划入口

### Milestone 4：完整门禁并交接独立 review

更新 living sections 的真实 commits、发现、决定、风险与证据；运行重复/race、Darwin/Linux 编译、
完整 branch diff 和 CI 同等门禁。计划保持 active，不迁移 completed，由 coordinator 安排未参与实现
的 reviewer。

    go test -count=20 ./internal/planner ./internal/runtime ./internal/manifest ./internal/paths ./internal/state
    go test -race ./internal/planner ./internal/runtime ./internal/manifest ./internal/paths ./internal/state
    GOOS=darwin GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-apply-darwin.test ./internal/planner
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-apply-linux.test ./internal/planner
    git diff 385dea8...HEAD --check
    make check BINARY=/private/tmp/dot-cp3-apply-planner-check/dot

Commit 边界：

    docs(planner): 记录 apply planner 交接证据

## Validation and Acceptance

| 必须成立的性质 | 证据 | 状态 |
|---|---|---|
| 完整 collision/path validation 先于 scope | partial 请求下的完整 target collision 集成测试 | 通过 |
| 只渲染/决策请求 scope | 未请求坏模板 + full fail-fast、unknown scope tests | 通过 |
| alias、kind migration 与 complete desired prune 分工 | alias symlink→scaffold、partial/full whole-module tests | 通过 |
| conflict 不阻塞 hook，prune 正确 deferred | default/force/no-prune combined tests | 通过 |
| invalid manifest/state、managed/rendered 零计划 | fail-closed table tests | 通过 |
| deterministic、自包含、getter 深拷贝 | repeated plan、getter mutation、combined validation tests | 通过 |
| 全部路径零锁零写入 | 每个 success/error fixture tree snapshot；missing state root/lock 断言 | 通过 |
| active prune 不删除 desired leaf/祖先 | full/partial ancestor、equal identity 与非 active 对照 tests | 通过（round 1 fix） |
| 独立完整复审 | round 2 重审 `385dea8...HEAD`、重复/race/双平台编译与完整门禁 | GO，无 P0–P3 finding |
| 当前平台完整门禁 | Darwin/arm64 `make check BINARY=/private/tmp/dot-cp3-apply-planner-review-check/dot` | 通过 |
| 双平台编译证据 | darwin/amd64、linux/amd64 planner test binary | 通过（未执行二进制） |
| 远端 macOS/Linux CI | 精确 branch HEAD | 待验收（本 worker 不 push） |

## Safety, Authorization, and Recovery

用户已明确授权在本 worktree/branch 创建 active plan、修改、stage、commit 和验证本 Milestone。
所有测试使用 `t.TempDir()` 合成 HOME/repo/config/state/targets/hooks，显式 `--home` 与 `--repo`，
不读取真实 modules、machine config、state、backup、`.env` 或主力 HOME。测试不调用 mutation
session、lock、Store 或 hook 子进程。失败保留最近 semantic commit，以新 fix commit 修正；不切
branch，不操作 main/其他 worktree，不 merge/rebase/amend/reset/force。计划保持 active 等待 review。

## Interfaces and Dependencies

不新增依赖。唯一 public 组合入口消费 `runtime.Overrides`、CLI version、module scope、force 与
no-prune，并返回 opaque `ApplyPlan`。结果 getters 给出独立 context/slice/bytes 值；完整 observation
保留给后续 status，file/prune/hook plans 保留给后续 presentation/executor。所有输入事实与决策均
来自已合入 contract，本层只负责有序编排与跨组件结构一致性。

## Surprises & Discoveries

- Observation: partial scope 的 file decisions 与 hooks 可直接按 `ScopedProfile.Modules()` 过滤，
  但 prune whole-module group 和 state alias join 仍需要完整 desired 视图。
  Evidence: `PlanPrune` 从 `ObservedProfile.Targets()` 建立 desired module 集，且
  `ObserveProfileTargets` 同时完成 desired/state identity join 与 orphan 分类。
  Impact: scope render 后只把对应 entries 的 Content 回填完整结构 desired，再完成一次完整
  observation；file decisions 单独按 scope module 过滤，不创建第二套 alias/prune 逻辑。

- Observation: `ScopedProfile.Modules()` 在 full scope 仍返回全部 effective modules，而
  `PruneOptions` 明确要求 `Full` 与 `Modules` 互斥。
  Evidence: 首次完整入口测试在 `PlanPrune` 返回
  `full prune cannot include module scope`；partial 路径正常。
  Impact: 组合层 full 时只设置 `Full=true`，仅 partial 传递 Modules；presentation context 仍保存
  full modules，不改变 manifest 或 prune contract。

- Observation: strict `state.Decode` 已在 runtime load 阶段直接拒绝任何 rendered entry，managed
  desired 则在完整 desired observation 转换前拒绝，即使它位于未请求 module。
  Evidence: public `PlanApply` fail-closed table 对 partial alpha 分别注入 rendered state 与 beta
  managed source，均返回零 plan 且 fixture tree 不变。
  Impact: apply planner 不增加第二套 M1 kind 过滤或 fallback，保持两个既有边界的错误来源。

- Observation: desired 与 state 分别通过 target-set 校验、alias join 也正确时，active P2 orphan
  仍可能是某个完整 desired 路径实际经过的祖先 symlink。
  Evidence: review round 1 fixture 中 owned `~/dir -> owned` 是 orphan，而 desired
  `~/dir/child` 经它到达；旧 combined validation 同时返回 create-link 与 active target prune。
  Impact: create/adopt→prune 顺序会先满足 child 再删除其祖先，因此必须在 combined validation
  对完整 desired resolutions 与实际 `DeletesTarget()` prune resolutions 做 cross-check。

## Decision Log

- Decision: apply planner 通过一个 production `PlanApply` 入口调用 `runtime.LoadReadOnly`，不暴露
  绕过 strict load/path validation 的第二个 public “loaded plan”入口。
  Rationale: diff/dry-run/status 应共享同一 fail-closed pipeline；测试使用真实隔离文件系统而不是
  注入 filesystem abstraction。
  Date: 2026-07-19

- Decision: 完整 observation 可以读取非 scope target leaf，但只有 scope targets 进入 `Decide`；
  scaffold parse/render 仍严格只在 scope 内发生。
  Rationale: 完整 observation 保留单 alias 与 prune whole-module 所需的同一事实视图；额外读取是
  纯只读且规范只要求 action/prune/hooks scope 缩小，没有授权把非 scope target 变成动作。
  Date: 2026-07-19

- Decision: combined validation 只检查前置组件结果的封闭枚举、排序、scope、Precondition 快照、
  success/failure effect 与 hook runtime payload 一致性。
  Rationale: 整体计划必须在 presentation/executor 消费前 fail closed，但 ownership、P1–P3、
  fingerprint 与路径 identity 的计算继续只由 `Decide`、`PlanPrune`、`PlanHooks` 和 observation
  提供，组合层不重新决策。
  Date: 2026-07-19

- Decision: prune topology cross-check 只拒绝 active P2 与 desired 相同 identity，或 active P2 是
  desired 的祖先；使用既有 `TargetResolution.Equal/IsAncestorOf`。
  Rationale: P1/P3 只删 state，deferred/no-prune 本轮不删 target，不能误拦；相反方向中 prune
  删除 desired 后代不删除 desired leaf，link 场景还会被路径阻断或 file conflict 门控，当前没有
  规范破坏证据，因此不扩成对称祖先禁令，也不使用字符串前缀。
  Date: 2026-07-19

## Outcomes and Handoff

实现、两轮独立 review 与主线程最终门禁已完成。相对 base `385dea8` 的 commits 为计划
`ccf487a`、完整 desired/scope file 组合
`c71fd7d`、唯一入口/整体 validation `3565be4`、首轮交接计划 `239b67b`，以及 review P1 fix
`864c828` 与 review 记录 `b961c36`。round 2 结论为 GO、无 P0–P3 finding。

本地验证（2026-07-19，均退出 0）：

    go test -count=20 ./internal/planner ./internal/runtime ./internal/manifest ./internal/paths ./internal/state
    go test -race ./internal/planner ./internal/runtime ./internal/manifest ./internal/paths ./internal/state
    GOOS=darwin GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-apply-review-darwin.test ./internal/planner
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-apply-review-linux.test ./internal/planner
    git diff 385dea8...HEAD --check
    make check BINARY=/private/tmp/dot-cp3-apply-planner-review-check/dot

结果新增唯一 `PlanApply`，它在真实 strict runtime load 后按规范顺序返回完整 observation、scope
file actions、prune 与 hooks；结果稳定、getter 深拷贝，所有 error 返回零 plan。未新增依赖，未修改
shared ownership/manifest/runtime/state contract，也没有 CLI/executor/mutation。双平台只完成编译，
精确 HEAD 远端 macOS/Linux CI 未运行；本地验收通过、远端待验收。实际 file/prune/hook execute、
state 持久化、输出和退出码属于后续 Milestone，未在本分支验证。review round 1 的 blocking P1
已修复并重跑全部门禁，round 2 已对完整 branch 复审通过；本计划迁移到 completed 后可由纯计划
commit 收口。
