# codex/m1-cp7-prerequisites：闭合 CP7 前的执行协议与状态边界

> [!WARNING]
> 历史工程记录，非当前规范或工作流程。

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

本 Milestone 在 CP6 已交付的安全 `add` 与 CP7 的 M1 `run_once` executor 之间建立可信基线。
完成后，CP6 的测试不再受宿主 `DOT_*` / `GIT_*` 环境污染；真实 `apply` 只执行同一个有效
canonical `ApplyPlan` 中的动作；runner 返回给 CLI 的结果是密封且自洽的事实模型；state 包可在
保持 state v1 格式和单次 `CommitState` 的前提下，把 entry 与未来成功 hook 的 `run_once` 变化
合成一个候选 Snapshot。用户可通过 hostile-env 回归、injected protocol 测试和完整 `make check`
直接观察这些性质。

## Scope / Non-goals

范围内：

- 修复 `internal/add` 测试 fixture 的 HOME、XDG、DOT 与 Git 环境隔离，并补 hostile-env 回归。
- 移除 `internal/apply.executionPlan` 的 plan/actions 多真相源；真实 runner 只消费有效
  `planner.ApplyPlan`。
- 将 apply Result 改为私有字段、密封构造和只读 accessor；在 apply 层闭合 plan、逐项 outcome、
  物理提交、state effect、backup、确认与最终 state commit 的关系，CLI 只投影有效结果。
- 在不改变现有公开行为的前提下，把 apply orchestration 按 canonical preparation、file、prune、
  transition/result 职责在同 package 内拆分，保留部分成功与安全重跑语义。
- 扩展 state transition，使 entry upsert/delete 与 `run_once` upsert 通过同一严格 API 合成一个
  Snapshot；现有 add/apply 行为继续复用该单一真相源。
- 在规范中明确 CP7 hook 子进程 stdio 的透传边界，以及其输出不属于 dot 确定性摘要契约。

明确不做：

- 不执行 hook、不移除当前 `run_once` apply gate、不增加 hook outcome、subprocess、context 或
  shared hook observation；这些在 CP7 中与真实 executor 一起设计和验证。
- 不实现 M2 watch/table-form、并行 hook、跨模块依赖图、sandbox 或 exactly-once。
- 不实现 init/update/self-update/bootstrap/release，不提前修复它们的锁前 context refresh、config
  publisher 或交互模型。
- 不改变 state v1 持久格式、公开 CLI 输出/退出码、ownership、Precondition、backup、prune、
  manifest 或 accepted-risk 契约；不新增依赖、通用 transaction、WAL 或 package hierarchy。
- 不读取或修改真实 `modules/`、机器配置、state、backup、`.env` 或 HOME。

## Contract and Context

- `docs/01-overview.md` §4：保护对象是工具自身 bug 和日常事故，不扩展到恶意仓库或对抗性并发。
- `docs/02-architecture.md` §4–§6：canonical plan、执行职责、成功 effect 合并和单次 state 原子提交。
- `docs/04-cli-spec.md` §3–§5：退出优先级、apply 输出与确定性摘要。
- `docs/05-apply-engine.md` §6–§8：部分成功、Precond、hook at-least-once 与成功前缀落账。
- `docs/08-testing.md` §1–§4：mutation fixture 必须隔离 HOME/repo/config/state/backup 与 Git 配置。
- `docs/09-roadmap.md` §1/§3：本 Goal 只准备 M1 string-form run_once 的执行边界，不交付 CP7 hook。

当前基线是 clean `main@012820fb006a5c35b339a2a083b78335eb8c65d0`。CP6 本地 `make check`
在普通环境通过，但 `internal/add/preflight_test.go` 的 helper 继承 ambient `DOT_CONFIG` 与
`GIT_CONFIG_GLOBAL`；合成 hostile env 已证明其可读取或写入 fixture 外路径。生产 add 的
`gitEnvironment` 会过滤这些变量，因此缺口属于测试证据边界，不是已证实的生产越界。

`internal/apply/run.go` 当前由 `executionPlan` 同时保存 `value/files/prune/groups/hooks`，runner
执行独立 slices 却把 `value` 交给 CLI；`Result` 又暴露多个可矛盾字段，完整性校验散落在 CLI。
本 Goal 必须把 plan 与 result 两端都改成单一真相源，同时保持 production runner 当前已验证的
file、backup、prune、partial success、conflict 和 state Store 恢复行为。

`internal/state.TransitionEntries` 目前只处理 entries 并保留全部 `run_once`。state v1 已包含严格
run_once schema，runtime 只允许一次成功 `CommitState`；因此本 Goal 增加的是内存 transition
能力，不是持久化迁移。

## Progress

- [x] 2026-07-22：完成只读审查、三路独立复核、当前基线 `make check` 与 hostile-env 缺口定位。
- [x] 2026-07-22：创建 active ExecPlan，冻结本 Goal 的 Scope / Non-goals 和串行 milestone 顺序。
- [x] 2026-07-22：用户明确授权创建 `codex/m1-cp7-prerequisites`、按下述边界 stage/commit、
  完成计划 `active/` → `completed/` 迁移及 closure commit；不含 merge、push、rebase、历史重写
  或分支删除。runtime approval 后已创建分支。
- [x] 2026-07-22：Milestone 1 关闭 CP6 add 测试环境隔离债；fixture 显式绑定 synthetic
  `DOT_CONFIG`/`DOT_REPO`，helper Git 复用生产过滤边界，hostile DOT/GIT sentinel 回归与
  `go test -race ./internal/add` 通过。
- [x] 2026-07-22：Milestone 2 收拢 canonical apply plan、sealed Result 与同包 phase 结构；
  production runner 在 mutation 前拒绝 zero/invalid plan，file/prune/commit phase 分离，CLI 不再
  重建 executor/state 协议，`go test -race ./internal/apply ./internal/cli` 通过。
- [x] 2026-07-22：扩展门禁发现 `internal/add` 恢复测试仍读取旧 Result fields；改为只读
  accessor 后 `go test -race ./internal/add` 通过，未改变生产路径。
- [x] 2026-07-22：Milestone 3 统一 entries/run_once state transition，并明确 hook stdio
  实时透传与确定性摘要边界；`go test -race ./internal/state ./internal/add ./internal/apply
  ./internal/cli` 通过。
- [x] 2026-07-22：freshness gate `make check
  BINARY=/private/tmp/dot-m1-cp7-prerequisites/dot` 通过（tidy、format、lint、全仓 race、build、
  synthetic HOME manifest check）；待独立复核、必要 fix 与终态计划迁移。
- [x] 2026-07-22：两路独立只读复核完成；修复动态 prune deferred 后仍恢复执行、未尝试
  backup 携带物理事实两个 Result validator 缺口，并恢复 CLI runtime outcome/错误优先级覆盖。
  复核者确认无剩余 P0–P3 findings。
- [x] 2026-07-22：复核修复后的完整 `make check
  BINARY=/private/tmp/dot-m1-cp7-prerequisites/dot` 再次通过。
- [x] 2026-07-22：完成 Outcomes / CP7 handoff，并随本 closure commit 将计划迁入
  `completed/`，建立 review-ready 终态。

## Milestones

### Milestone 1：hostile env 下的 CP6 测试仍严格隔离

先增加能证明 ambient `DOT_CONFIG`、`DOT_REPO`、`GIT_CONFIG_GLOBAL`、`GIT_DIR`、`GIT_INDEX_FILE`
等变量不能改变 fixture 解析或 helper Git 写入位置的回归，再让 `newAddFixture` 显式绑定 synthetic
config/repo，并让 helper Git 使用与生产相同的过滤规则。测试只能在 `t.TempDir()` 内写入，并以
fixture 外 synthetic sentinel 未变化为验收；不得依赖当前 shell 恰好没有这些变量。

Concrete steps：

    在 repo root 运行：go test -race ./internal/add
    预期：普通与 hostile-env 子测试均通过，所有 Git/config 写入只存在于各自 t.TempDir。

验收：

- ambient DOT/GIT 变量不能让 fixture 读取或写入其临时根之外。
- Git local/global exclude 语义测试仍使用真实 Git 并继续通过。
- production `gitEnvironment` 行为不放宽。

Commit 边界：

    test(add): 隔离 fixture 的宿主配置环境

### Milestone 2：apply 执行协议只有一个可信 plan 和一个可信 result

先增加 invalid plan、actions/groups 分叉、nil-error 不完整结果、counter/effect/state commit 矛盾的
失败关闭测试，再让 runner dependency 只返回 `planner.ApplyPlan`。runner 必须先验证 plan，随后
从它取得 files/prune/groups/hooks；测试故障注入下沉到 phase dependency，而不是伪造与 plan
分叉的 actions。Result 采用私有状态与成功 seal，每个可执行 action 的 outcome 保存足以验证
尝试、物理提交、state effect 和 backup 的事实，聚合值由可信事实派生或受单一 validator 约束。
CLI 只通过 accessor 投影，不能重建 execution protocol。

同时将 `runWithOperations` 的 file、prune 和最终 state 阶段拆入同 package 的短函数/文件；拆分
必须保持 create-before-prune、conflict 门控 prune、部分成功仍提交 state、cleanup error 后收养、
Store failure 不回滚 target，以及确定性输出/退出码。

Concrete steps：

    在 repo root 运行：go test -race ./internal/apply ./internal/cli
    预期：全部既有恢复/幂等/CLI 测试与新增 protocol rejection 测试通过。

验收：

- runner 在任何 executor、backup、confirmation、prune 或 state mutation 前拒绝 invalid plan。
- 不存在 plan value 与实际执行 actions/groups/hooks 的并行事实源。
- zero/forged/incomplete Result 不能被 CLI 投影；当前 production 的所有部分成功事实仍可表达。
- CLI 不校验 executor/state 细节，只映射有效 result 到输出与退出码。

Commit 边界：

    refactor(apply): 密封计划执行与结果协议

### Milestone 3：一次 transition 合并 entry 与 run_once 效果

在 state package 内增加一个严格 transition 输入模型，原子应用 entry upsert/delete 和 run_once
upsert，复用现有 key、SHA-256 与 RFC3339 校验。缺失/loaded 基线、重复或冲突 update、空变化、
保留未涉及记录、返回值不共享 map 都必须测试。`TransitionEntries` 可保留为兼容薄 wrapper，现有
add/apply 改为或继续通过同一实现路径；本 Goal 不产生任何 run_once 写调用，也不改变 JSON。

同步 `docs/04-cli-spec.md` / `docs/05-apply-engine.md`，明确未来 hook 继承调用命令 stdin/stdout/
stderr，hook 自身输出属于外部透传 stream，可实时交错且不纳入 dot 的确定性摘要契约；dot 自身
context/action/verdict 顺序继续稳定。

Concrete steps：

    在 repo root 运行：go test -race ./internal/state ./internal/add ./internal/apply ./internal/cli
    预期：state transition、现有 mutation 和 CLI 行为全部通过，编码后的 state v1 结构不变。

验收：

- 一个 candidate Snapshot 能同时包含 entry 与 run_once 成功效果，但 runtime 仍只提交一次。
- invalid run_once key/hash/time、重复 key 与含糊输入 fail closed，零值 Snapshot 不泄漏。
- 规范不要求 buffer、截断、解析或重排任意 hook 输出，不改变现有命令摘要契约。

Commit 边界：

    refactor(state): 统一 apply 与 hook 状态转换

    docs(hooks): 明确子进程输出透传边界

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| CP6 add tests 不受 ambient DOT/GIT 环境影响 | hostile-env + real Git fixture 测试 | 已通过 |
| apply 只执行 valid canonical plan | apply seam/protocol 测试 | 已通过 |
| Result 完整、自验证且 CLI 只投影 | apply/CLI injected result 测试 | 已通过 |
| 既有 file/backup/prune/恢复/幂等不回归 | apply/add/CLI race tests | 已通过 |
| entries 与 run_once 可形成一个 strict Snapshot | state transition/codec/store 测试 | 已通过 |
| 公开行为与 state v1 不变，hook stdio 边界已明确 | docs diff + repository diff review | 已通过 |
| Go、依赖、构建、lint、race、manifest 全门禁 | `make check` | 已通过 |
| 未参与实现者完整复核 | read-only subagent review | 已通过，无剩余 P0–P3 finding |

最终从 repo root 使用独立 `/private/tmp` cache/BINARY 运行：

    git diff <goal-base>...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp7-prerequisites/dot

远端 macOS/Linux CI、真实 Linux 和真实私人配置不属于本 Goal 的本地验收证据，不得声称已运行。

## Safety, Authorization, and Recovery

当前任务已授权本 Goal 范围内的代码、测试、规范和计划文件修改，以及创建
`codex/m1-cp7-prerequisites`、按本计划语义边界 stage/commit，并在门禁与独立复核通过后将同一
计划迁至 `completed/` 创建 closure commit；不含 merge、push、pull/fetch、rebase、amend、reset、
force、branch 删除、PR 或 release。授权来自 2026-07-22 用户消息“授权”，仅适用于本次 Goal。

所有测试 mutation 使用 `t.TempDir()` 或唯一 `/private/tmp` cache/BINARY；hostile env 的“外部”
对象仍位于另一个 synthetic 临时根，显式 sentinel 证明未改写。失败时保留当前语义 commit 之前的
已验证 checkpoint，不重写历史；新的问题用后续 fix commit 处理。真实私人数据路径仅核对 Git
状态，不展开内容。

## Interfaces and Dependencies

不新增依赖或持久化版本。跨 package 的必要契约只有：apply 从 planner 获得一个有效
`ApplyPlan`；state transition 接收严格的 entry/run_once 变化并返回一个有效 Snapshot；CLI 通过
apply Result accessor 读取已验证事实。具体 phase helper 与私有字段布局由实现反馈决定。

## Surprises & Discoveries

- Observation: CP6 fixture 使用生产代码能过滤的 Git 环境，但测试 helper 自身没有复用该边界。
  Evidence: `internal/add/preflight_test.go` 的 `runGit` 直接 append `os.Environ()`，hostile
  `GIT_CONFIG_GLOBAL` 合成验证会在 fixture 外创建文件。
  Impact: 先修测试证据边界，再把普通环境下的 `make check` 当成可靠验收。

- Observation: apply 的 fault-injection seam 允许用零 `ApplyPlan` 配独立 actions 测试 executor。
  Evidence: `internal/apply/run_test.go` 的 `runSeamFixture.operations(executionPlan)`。
  Impact: 测试结构本身固化了 production gate 的 split-brain，需要在重构协议时一起迁移。

- Observation: CLI 的旧协议测试通过跨 package 构造可矛盾 `apply.Result`，同时让 CLI 成为
  executor/state 不变量的第二维护者。
  Evidence: `internal/cli/apply_test.go` 直接设置 plan、outcomes、counter 与 commit 摘要；密封
  Result 后这些构造不再可编译。
  Impact: 矛盾事实回归迁入 `internal/apply/result_test.go`，CLI 保留真实 mutation 集成测试与
  zero Result fail-closed 测试，只消费只读 accessor。

- Observation: `internal/add` 的恢复测试是 apply Result 的另一个跨 package 消费者，Milestone 2
  的目标包门禁未编译到它。
  Evidence: 扩展到四包 race 后，六处旧字段访问编译失败；全部只读语义已有对应 accessor。
  Impact: 增加独立测试适配 commit，并把后续 Milestone 与最终门禁都覆盖 state/add/apply/CLI。

- Observation: 首次完整 lint 发现 Result accessor 隔离测试有一个无效 append，且 CLI 还保留
  已失去调用方的 `planMutationFixture`。
  Evidence: `make check` 的 `ineffassign` 与 `unused` 各报告一处；删除后完整门禁通过。
  Impact: 这两处仅是测试重构遗留，不影响生产协议；freshness gate 必须从头重跑而不能沿用前次
  race 结果。

- Observation: sandbox 中 Go 在 build 后尝试更新默认 module download stat cache 时报告一次
  `operation not permitted`，但 build、manifest check 与 make 目标均成功。
  Evidence: `make check` 最终输出 `Manifest check passed.`，命令退出 0；所有显式 cache 与 binary
  已定向到 `/private/tmp`。
  Impact: 记录为非阻塞环境噪声，不把它描述为构建失败，也不修改仓库工具链规避 runtime 限制。

- Observation: `validPruneOutcomes` 原先只把 conflict/failed 当作动态 prune 停点，因此可接受
  `succeeded → deferred → succeeded` 的伪造序列。
  Evidence: 独立安全复核指出 `ActionDeferred` 未设置 stopped；三项真实 canonical prune plan
  的矛盾回归在修复前可通过 validator。
  Impact: active deferred 同样封闭后缀；static deferred 仍按 plan 原样跳过，不改变确认拒绝与
  file 未收敛时全部 deferred 的合法结果。

- Observation: backup batch 初始化失败需要 `ActionFailed/attempted=false`，但旧 validator 未同时
  拒绝逐项 target commit、state effect 或 backup path。
  Evidence: 独立安全复核构造的矛盾 facts 可被 `Valid(true)` 接受。
  Impact: 未尝试 backup action 现在只允许三个物理事实全为空；已尝试后保留 backup 或提交点的
  既有失败恢复语义不变。

- Observation: CLI 跨包伪造 Result 的旧测试删除后，runtime outcome 的文本映射覆盖不足。
  Evidence: 工程复核确认只剩真实 file conflict 与 zero Result 边界，prune conflict/deferred/failed
  及 executed-result runtime error 优先级没有等价局部测试。
  Impact: 增加纯 outcome-map 投影测试与由真实 `applyrunner.Run` 生成 sealed Result 的错误优先级
  测试，不开放构造器或把协议校验搬回 CLI。

## Decision Log

- Decision: 本 Goal 不实现 hook executor，但交付它依赖的 canonical plan/result 和统一 state
  transition。
  Rationale: 这些是现有安全协议债，可独立验证；subprocess observation、context 与 HookOutcome
  只有进入 CP7 真实执行流后才有足够反馈，提前实现会预建抽象。
  Date: 2026-07-22

- Decision: 保留现有 package 边界，只在 `internal/apply` 内按 phase 分文件。
  Rationale: 当前依赖图清晰无循环；问题是单文件与协议多真相源，不是 package 职责缺失。
  Date: 2026-07-22

- Decision: hook stdio 采用实时透传且排除在 dot 确定性摘要契约之外。
  Rationale: 支持 brew 等交互/长时任务，避免无界 buffer、截断和延迟；dot 自己的摘要仍保持稳定。
  Date: 2026-07-22

- Decision: `Result.Valid` 同时验证 canonical action coverage 与 file/confirmation/prune/state commit
  的跨阶段关系，聚合计数全部从密封逐项事实派生。
  Rationale: 仅隐藏字段不能防止 apply 包内部回归；阶段门控与物理事实必须在同一 validator 中
  fail closed，CLI 不应知道这些细节。
  Date: 2026-07-22

- Decision: state 暴露 `Transition(Loaded, ChangeSet)` 作为 entry/run_once 统一真相源，保留
  `TransitionEntries` 为薄 wrapper。
  Rationale: CP7 可在不改 state v1、不引入第二次 Store 的情况下合并成功 hook 指纹；现有
  add/apply 无需提前知道 hook，兼容路径也不会复制校验逻辑。
  Date: 2026-07-22

## Outcomes and Handoff

本 prerequisite Milestone 已完成。相对 `main@012820fb006a5c35b339a2a083b78335eb8c65d0`
的结果如下：

- CP6 add fixture 不再继承可改变 config/repo/Git 行为的宿主 `DOT_*` / `GIT_*` 环境；hostile
  env sentinel 与真实 Git 回归证明 mutation 只发生在 synthetic 临时根。
- production apply 从持锁 inputs 只取得一个有效 canonical `ApplyPlan`，再从同一值派生
  files/prune/groups/hooks；zero/invalid plan 在 executor、backup、confirmation、prune 与 state
  mutation 前失败关闭。测试 seam 只在低层 phase 注入动作，不再构成 production 第二真相源。
- `Result` 已密封：plan、逐项尝试、target commit、state effect、backup、确认与最终 commit
  关系由 apply 层单一 validator 维护；CLI 仅从 detached accessor 映射文本和退出码。独立复核
  发现的动态 prune suffix 与未尝试 backup facts 两个缺口已修复并有回归。
- file、prune/confirmation 与 state commit 已拆成同 package phase，既有部分成功、cleanup error、
  Store failure、收养恢复和幂等行为保持不变。
- state v1 未迁移；新的 `Transition(Loaded, ChangeSet)` 可先整体校验、再原子合并 entry
  upsert/delete 与 `run_once` upsert，`TransitionEntries` 是兼容薄 wrapper。invalid key/hash/time、
  duplicate key、missing/loaded/no-op、输入 map 隔离和 Encode/Decode round trip 均已覆盖。
- 规范明确未来 hook 继承调用命令 stdin/stdout/stderr 并实时透传；外部输出可跨 stream 与 dot
  摘要交错，不属于确定性摘要契约，dot 不 buffer、截断、解析或重排。

最终本地证据：`make check BINARY=/private/tmp/dot-m1-cp7-prerequisites/dot` 在
`fe6228d4feca` 通过，包括 tidy diff、format diff、lint、全仓 `go test -race ./...`、build 与
synthetic HOME manifest check；两路未参与实现的只读复核在修复后均报告无剩余 P0–P3 finding。
Go 尝试更新 sandbox 外 module stat cache 时仍打印非致命 `operation not permitted`，不影响命令
退出 0 或上述门禁结果。

CP7 应从这里继续，而不是在本 Goal 预建的部分：

1. 设计真实 hook observation/executor 与 HookOutcome，并从 canonical `plan.Hooks()` 消费动作；
   保留当前 unsupported gate，直到整个执行、结果和测试切片能一起解除。
2. hooks 位于 file/prune 后且不受文件收敛门控；串行执行，失败即停止未启动后缀。成功前缀产生
   `state.RunOnceUpdate`，失败项保留旧指纹。
3. 当前 file/prune phase 末尾即 commit state；CP7 接 hook 时需把最终 commit 上移到包含 hook
   phase 的 orchestration 尾部，用一个 `state.ChangeSet` 合并 file/prune 与成功 hook effects，仍只
   调用一次 `CommitState`。不要先提交 file state 再单独写 hook state。
4. hook subprocess 直接继承 stdio 与规范化 cwd/env；测试必须把 HOME、全部 XDG、repo、config、
   state、backup 放在同一 synthetic 绝对根，并断言真实 HOME 未变化。

本地未执行且不得外推的证据仍包括：远端 macOS/Linux CI、真实 Linux、真实私人配置，以及尚未
实现的 hook subprocess/stdio/成功前缀单次落账集成。上述内容属于 CP7 的真实实现与验收范围。
