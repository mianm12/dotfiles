# fix/m1-cp4-target-pair-priority：稳定 target pair 语义优先级

> [!WARNING]
> 历史工程记录，非当前规范或工作流程。

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

修复完整 target-set 同时含 ancestor 与 equal pair 时的错误选择。完成后，共享 paths
validator 会在全部 target 成功解析后优先报告任意 equal identity；runtime 因而始终把多个
state key 指向同一 target 分类为损坏，而不受 key 顺序或其他 ancestor 冲突遮蔽。可以通过
paths 单元测试和真实 `LoadReadOnly` 隔离 fixture 直接观察该结果。

## Scope / Non-goals

范围内：

- 固定完整 resolve 后 `equal pair > 首个 ancestor pair > unary self traversal` 的稳定优先级。
- 保留无 equal 时首个 ancestor 的输入顺序、结构化错误、`errors.Is/As` 和 provenance。
- 固定 runtime 的 `ErrCorrupt` / `ErrTargetIdentityConflict` 分类及只读零 mutation 证据。
- 明确 resolution failure 仍先于 relation selection。

明确不做：

- 不聚合或一次展示全部 target 冲突。
- 不在 runtime/state 增加第二套 identity 扫描或第二次 filesystem snapshot。
- 不改变 state v1、ownership、Precondition、mutation、公开退出码或真实 apply 范围。
- 不实现 M2 rebuild，也不修改规范迁就实现。

## Contract and Context

- `docs/02-architecture.md` §2/§4–§6：target identity 与路径拓扑由共享边界定义，state 损坏
  必须在消费前 fail closed。
- `docs/05-apply-engine.md` §2/§5：多个 state key 指向同一 target 是语义损坏；ancestor 后来
  不可安全到达属于合法 state 的 runtime path failure，两者不能混淆。
- `docs/08-testing.md` §3.1/§3.3：target identity、state fail-closed 与历史别名冲突必须长期回归。

分支从 clean `main@09147de39657c489df8e6c723bd3dacfd0c7228f` 创建；origin/main 仍是
`e9e8bac`，未 fetch/pull。当前 `internal/paths/target_validation.go` 在 pair nested loop 的首个
non-none relation 上立即返回；`internal/runtime/loading.go` 仅在该单个错误含 equal relation 时
包装 `state.ErrCorrupt`。这形成了依赖输入顺序的隐藏跨 package 契约。

## Progress

- [x] 2026-07-20：重新验证 source/control/sink、规范与现有 tests；确认仍 fail closed，但错误
  分类违反 state 语义和未来恢复边界。
- [x] 2026-07-20：从 current clean main 创建专用 fix branch/worktree 并建立本计划。
- [x] 2026-07-20：补齐 earlier ancestor / later equal、无 equal 首个 ancestor、resolution-first
  和四 key state 分类回归；修复前窄测仅两个核心断言按预期失败，控制测试通过。
- [x] 2026-07-20：共享 validator 已实现 `equal > 首个 non-equal pair > unary`；任务 diff
  check、相关五 package 窄测及隔离输出路径下的 `make check` 全部通过。
- [x] 2026-07-20：两名未参与实现的 reviewer 分别完成规范/数据保护与 Go/测试完整
  branch review，均给出 GO，无 P0–P3 finding。
- [x] 2026-07-20：更新 Outcomes/Handoff，迁移计划并创建纯 plan-closure commit。

## Milestones

### Milestone 1：固定可复现的复合冲突证据

在 `internal/paths/target_validation_test.go` 建立 earlier ancestor + later equal 以及无 equal 时
首个 ancestor 稳定性的同层测试；在 `internal/runtime/loading_test.go` 用四个排序 state key 和
`alias -> real` 证明当前分类错误，并复用 `loadReadOnlyWithoutMutation` 固定 whole-tree 与 lock
零 mutation。另加 resolution failure 优先的边界测试，避免修复扩大为部分解析聚合。

Concrete steps：

    在 repo root 运行：go test ./internal/paths ./internal/runtime
    修复前预期：复合 pair priority 与 runtime corrupt 分类断言失败；既有控制测试继续通过。

Commit 边界：

    test(paths): 固定 target pair 类型优先级

### Milestone 2：在共享 validator 关闭根因

只修改 `internal/paths/target_validation.go` 的 pair 选择：完整 resolve 后保存首个 non-equal
conflict，继续扫描；遇 equal 立即返回；无 equal 才返回保存的 ancestor，然后进入既有 unary
检查。不得改变公开错误类型、Left/Right/Relation、输入切片或 filesystem resolution 次数。

Concrete steps：

    在 repo root 运行：go test ./internal/paths ./internal/runtime ./internal/manifest ./internal/planner ./internal/apply
    预期：原复现不再出现 ErrPathValidation；pure ancestor、equal+unary、single unary 与合法集合不变。

Commit 边界：

    fix(paths): 全局优先 equal target 冲突

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| earlier ancestor 不遮蔽 later equal | paths structured error regression | 通过 |
| 无 equal 时首个 ancestor 与 unary 顺序不变 | paths negative controls | 通过（新增控制与既有全套） |
| 多 state key equal identity 始终分类为 corrupt | runtime isolated loading regression | 通过；whole-tree 与 lock 零 mutation |
| resolution failure 保持可信前置 | paths resolution-first regression | 通过 |
| 完整仓库行为与格式不回归 | diff check + `make check` | 本地 Darwin/arm64 通过；远端待验收 |
| 独立完整 branch review | 规范/安全 reviewer + Go/测试 reviewer | 两份 GO；无 P0–P3 finding |

最终从本 worktree root 运行：

    git diff 09147de39657c489df8e6c723bd3dacfd0c7228f...HEAD --check
    go test ./internal/paths ./internal/runtime ./internal/manifest ./internal/planner ./internal/apply
    make check BINARY=/private/tmp/dot-m1-cp4-target-pair-priority-final/dot

本地平台为 Darwin/arm64。Linux 仅可补交叉编译证据；远端 macOS/Linux CI 未运行时必须明确
标为待验收。

## Safety, Authorization, and Recovery

用户当前 CP4 Goal 已授权 integration/acceptance fix branches、`/private/tmp` worktrees、范围内
修改、语义 commits、计划迁移、freshness merge 与本地 FF-only main 集成。该证据只适用于当前
任务。测试只使用 `t.TempDir()` 合成 HOME/repo/config/state，不读取真实 modules、machine
config、state、backup、`.env` 或主力 HOME。

改动只影响只读 target-set 错误选择，不执行 mutation。失败时保留最近 commit，不 amend、
rebase、cherry-pick、squash、reset 或 force；review 阻塞时计划保持 active。

## Interfaces and Dependencies

不新增依赖或公开类型。`paths.ValidateTargetSet` 继续是唯一 identity/topology validator；runtime
继续消费单个 `TargetConflictError`，但其 equal/ancestor 分类不再隐式依赖输入中第一个冲突。

## Surprises & Discoveries

- Observation: 修复前 `go test ./internal/paths ./internal/runtime` 只在新增的复合 paths priority
  和 runtime corrupt 分类断言失败；实际错误分别选择 `earlier parent` / `earlier child` 以及
  `state.ErrPathValidation`，与根因分析一致。无 equal 首个 ancestor 和 resolution-first 控制通过。
  Evidence: 使用隔离 `GOCACHE=/private/tmp/dot-m1-cp4-target-pair-priority-go-cache` 的窄测输出；
  runtime fixture 的 whole-tree 与 lock 零 mutation 断言先于分类断言通过。
- Observation: sandbox 不允许 `golangci-lint` 向用户 cache 写入 facts，因此完整门禁输出 cache
  warning；lint 本身完成并报告 `0 issues`，后续 race tests、build 与 manifest check 全部成功。
  Evidence: `make check BINARY=/private/tmp/dot-m1-cp4-target-pair-priority-worker/dot` exit 0。
- Observation: 两名独立 reviewer 均确认共享 validator 是合适的唯一修复点；额外拆 helper、
  identity map 或 runtime 二次扫描会复制契约而不降低现有 ancestor 检查的 O(n²) 复杂度。
  Evidence: 两份完整 `09147de...58ec9dd` branch review 均为 GO；另通过新增场景 20 次重复
  运行、相关 package race/vet 与 Linux/amd64 test binary 编译。

## Decision Log

- Decision: 选择共享 validator 的稳定 pair 优先级，不做 runtime 二次扫描或错误聚合。
  Rationale: 保持单一真相源、同一 resolver snapshot 和现有错误 API；复杂度仍为 O(n²)，只在
  多重冲突时选择语义更强的 equal。
  Date: 2026-07-20
- Decision: 保持 resolve loop 完全不变，只在完整 resolve 成功后的 pair selection 中延迟返回
  首个 non-equal conflict；因此 resolution failure 仍先于任何关系选择，resolver 次数不变。
  Date: 2026-07-20

## Outcomes and Handoff

本计划已完成，可以进入 freshness gate 与本地 main 集成：

- `1a46538 test(paths): 固定 target pair 类型优先级`：新增复合冲突、稳定顺序、resolution-first
  和 runtime 四 key/零 mutation 回归，并保存修复前失败证据。
- `27e5224 fix(paths): 全局优先 equal target 冲突`：仅修改共享 validator 的 pair 选择；未改
  runtime、公开错误类型、依赖或 filesystem resolve 次数。
- 本地 `git diff 09147de...HEAD --check`、相关五 package tests 与 `make check` 通过。
  根 agent 复跑相同窄测与隔离 cache/output 下的 `make check` 亦通过。
- 两名未参与实现的 reviewer 对完整 branch 分别复核规范/数据保护和 Go/测试/跨平台，均
  给出 GO，无 P0–P3 finding；Linux/amd64 已补 test binary 编译证据。
- 修复保留全部 target 先 resolve、单一 resolver snapshot、首个 ancestor provenance 与 unary
  顺序；没有 state v1、公开 API、ownership、Precond、mutation、依赖或 runtime 数据流变化。
- 本地 Darwin/arm64 验收通过；远端 macOS/Linux CI 未实际运行，仍待远端验收。
