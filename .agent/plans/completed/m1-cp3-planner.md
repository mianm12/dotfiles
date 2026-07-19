# chore/m1-cp3-orchestration：交付纯计划、diff、dry-run 与 status

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。本次用户明确选择“一条 Checkpoint Goal 编排多个 branch”，因此本计划只
记录跨 Milestone 的 DAG、调度、基线、验收证据和最终结果；每个 Milestone 另有独立 branch、
active ExecPlan、语义 commits 与 review 单元。

## Purpose / Big Picture

完成后，M1 会拥有一个纯只读、自包含的 planner：runtime load 后先形成完整 profile 的结构
desired，只渲染请求 scope，观测 target 快照，按 L/S 与 M1 kind migration 决策，追加 prune
和 run_once hook 计划，再整体验证并稳定展示。`dot diff`、`dot apply --dry-run` 与 `dot status`
全程不取锁、不创建 state/target/temp，也不写 hook 指纹；裸 `dot apply` 在 CP5 前明确拒绝。

## Scope / Non-goals

范围内：

- 编排 `feat/target-observation`、`feat/decision-engine`、`feat/prune-planner`、
  `feat/hook-planner`、`feat/apply-planner`、`feat/plan-cli` 与 `feat/status` 七个 Milestone。
- 固定共享 plan model、target observation、L1–L6/S1a–S3、symlink↔scaffold migration、
  P1–P3、部分 scope、alias 合并、hook fingerprint、完整 plan validation 和公开只读输出。
- 逐分支执行测试先行、语义 commits、独立 review、freshness、closure 与 main 门禁，并从
  checkpoint base 验收完整 Checkpoint。

明确不做：

- 不实现任何真实 target/state/hook mutation、backup、force execute、完整 apply executor 或 add。
- 不实现 managed/rendered 生命周期、adopt、文本 unified diff、workflow、filesystem abstraction、
  通用 planner 依赖或 M2/M3 能力；这些输入继续 fail closed。
- 不改变 state v1、ownership、Precond、公开输出或退出码契约迁就实现。
- 不 fetch、pull、push、rebase、cherry-pick、squash、amend、reset、force、PR、tag 或 Release。

## Contract and Context

- `docs/02-architecture.md` §4–§6：planner 负责 load→resolve→enumerate→scoped render→scan→
  decide→validate，动作计划必须自包含且不修改 target。
- `docs/03-manifest-spec.md`：完整 profile 的结构路径不变量不可被部分 scope 缩小；M1 只支持
  link/scaffold 与字符串 run_once。
- `docs/04-cli-spec.md` §3、§4.2–§4.4、§5：diff/dry-run/status 的退出码、输出与零锁零写入契约。
- `docs/05-apply-engine.md` §1–§5、§8、§10：L/S/P、owned、alias、kind migration、hook 指纹与
  幂等边界；rendered 在 M1 fail closed。
- `docs/06-templates.md`：scaffold 只在动作 scope 渲染且 fail-fast，产物随 plan 自包含。
- `docs/08-testing.md`、`docs/09-roadmap.md`：CP3 先固定纯计划和 dry-run，不预建 executor。

`checkpoint_base` 是本地 `main@bd6f4fcc05a6cd8db2fda1b2fb84baebfb11ab4a`。Plan Gate 时
`HEAD == main == origin/main`，upstream 为 `origin/main` 且 ahead/behind 为 0；未 fetch/pull。
CP2 的 coordinator completed plan 与 main 提交确认前置已本地交付，精确远端双平台 CI 仍按
其 handoff 标为待验收。main clean、只有 main worktree，全部 CP3 目标 branch 均不存在。

现有 `internal/runtime.LoadReadOnly` 已提供无锁的 strict runtime/manifest/state 输入；
`internal/manifest` 已提供完整 profile 结构 desired、scaffold render fail-fast 和 path boundary；
`internal/state` 已提供严格 symlink/scaffold getters；`internal/paths` 已提供 target identity/
topology。真实缺口是尚无共享 planner model、observation/alias join、decision/prune/hook/apply
planner 或 diff/status/dry-run CLI。

## Progress

- [x] 2026-07-19：确认 CP2 已合入；main clean，`main == origin/main == bd6f4fc`，未 fetch/pull；
  候选 branches 不存在。
- [x] 2026-07-19：读取仓库规则、ExecPlan 生命周期、指定规范、当前实现/测试和 completed plans；
  基线 `make check BINARY=/private/tmp/dot-cp3-plan-gate` 在 Darwin/arm64 通过。
- [x] 2026-07-19：三名只读 subagent 完成规范缺口、DAG/共享契约及测试/依赖/双平台审计；
  主 agent 核对后无停止条件。
- [x] 2026-07-19：从 checkpoint base 创建 coordinator branch/worktree，并建立本计划。
- [x] 2026-07-19：以 `6969d01` 提交 coordinator ExecPlan 起点并启动 Wave 1。
- [x] 2026-07-19：`feat/target-observation` 完成共享 model、validated scope/scoped render、
  leaf observation 与 desired/state identity join；首轮 review 的 HOME capability P1 以
  `584473c` 修复，第二轮完整复审 GO。Milestone closure 后 main 以 `712ab85` fast-forward-only
  集成，合入后窄测与 `make check BINARY=/private/tmp/dot-cp3-main-after-observation` 通过，
  worker worktree clean 后无 force 移除。
- [x] 2026-07-19：`feat/decision-engine` 完成 Owned、L/S、force、M1 kind migration 与自包含
  Precondition/state effects；首轮 review 的 target resolution P1 与 scaffold force P2 分别以
  `1905b15`、`5ea7a91` 修复，第二轮完整复审 GO。Milestone closure 后 main 以 `f181f94`
  fast-forward-only 集成，合入后窄测与 `make check BINARY=/private/tmp/dot-cp3-main-after-decision`
  通过，worker worktree clean 后无 force 移除；共享 model 由此冻结供 Wave 3 消费。
- [x] 2026-07-19：Wave 3 启动后发现 `OrphanTarget` 尚未保存计划时 target resolution；按并行
  规则撤销并行并改为 prune→hook 串行。`feat/prune-planner` 补齐 orphan identity，完成 P1–P3、
  partial scope、整模块组与 conflict/no-prune 门控；独立复核 GO。closure 后 main 以 `1a7e0fc`
  fast-forward-only 集成，合入后窄测与 `make check BINARY=/private/tmp/dot-cp3-main-after-prune`
  通过，clean worktree 已移除。
- [x] 2026-07-19：`feat/hook-planner` 以 `1f11fcd` 非重写同步 prune 基线后，完成脚本分类、
  versioned fingerprint、scoped run_once 与自包含 action；独立复核 GO、无 P0–P3。closure 后
  main 以 `385dea8` fast-forward-only 集成，合入后 Hook 测试 20 次与
  `make check BINARY=/private/tmp/dot-cp3-main-after-hook` 通过，clean worktree 已移除。
- [x] 2026-07-19：`feat/apply-planner` 完成唯一只读组合入口、完整 desired/scoped render、
  file/prune/hook 组合与整体验证；首轮 review 的 active P2 删除完整 desired 祖先 P1 以
  `864c828` 修复，round 2 完整复审 GO。closure 后 main 以 `83039c1` fast-forward-only 集成，
  合入后相关五包 20 次与 `make check BINARY=/private/tmp/dot-cp3-main-after-apply` 通过，clean
  worktree 已移除。
- [x] 2026-07-19：`feat/plan-cli` 接入共享 `PlanApply` 的 `dot diff` 与
  `dot apply --dry-run` 投影、1 > 3 > 2 > 0、warning/skip、前置拒绝与 held-lock/整树零写入；
  独立复核 GO、无 P0–P3。closure 后 main 以 `afd13c8` fast-forward-only 集成，合入后
  CLI/planner/runtime 三包 20 次与 `make check BINARY=/private/tmp/dot-cp3-main-after-plan-cli`
  通过，clean worktree 已移除。
- [x] 2026-07-19：`feat/status` 完成同一 `PlanApply` 的 DRIFT/PENDING/ORPHAN/UNASSIGNED
  投影、Clean/0、actionable/2、error/1 与 held-lock/整树零写入；首轮 review 的 notice 写失败后
  已输出可信 verdict P2 以 `446cadb` 修复，round 2 完整复审 GO。closure 后 main 以 `f30645e`
  fast-forward-only 集成，合入后 CLI/planner/runtime 三包 20 次与
  `make check BINARY=/private/tmp/dot-cp3-main-after-status` 通过，clean worktree 已移除。
- [x] 按 DAG 完成七个 Milestone 的实现、复核、closure、freshness 和 main 集成。
- [x] 2026-07-19：三路 Checkpoint Acceptance 首轮完成：平台/架构与数据保护 reviewer GO；规范
  reviewer 发现 1 个有效 P2——status 仅将 `ReasonLinkDrift` 归 DRIFT，其他
  `ActionConflict` 错归 PENDING，偏离 CLI spec 的公开 taxonomy。main 保持 `f30645e` clean，
  已从 current main 创建 `fix/m1-planner-acceptance` 做单一根因修复。
- [x] 2026-07-19：`fix/m1-planner-acceptance` 以 `b1de0fa` 按 action verb 统一全部 file
  conflict→DRIFT，新增 L5/L6 四类 blocker × 有/无历史 state 回归；round 2 从 checkpoint base
  完整复审 GO。freshness 确认 main 未前进后 fast-forward-only 集成，合入后三包 20 次与
  `make check BINARY=/private/tmp/dot-cp3-main-after-acceptance-fix` 通过，clean worktree 已移除。
- [x] 2026-07-19：三名未参与实现的 reviewer 从 `bd6f4fc...main@b1de0fa` 重新完成整体验收；
  规范/公开行为、数据保护与平台/架构三路均 GO、无 P0–P3 finding。主线程最终完整 diff check 与
  `make check BINARY=/private/tmp/dot-cp3-acceptance-final` 通过。
- [x] 从 checkpoint base 完成三路独立 Acceptance，处理有效 finding，并以 `6e1e9ad` 将最终
  code main 合入 coordinator；迁移本计划后创建纯计划 closure，准备 fast-forward-only 合入 main。

## Milestone DAG and Scheduling

共享 plan model 尚不存在。为避免新增 Checkpoint 定义之外的 branch，`target-observation`
先同时稳定最小不可变 model；因此 observation 与 decision 不并行：

```text
target-observation → decision-engine → prune-planner ─┐
          │                           hook-planner ────┼→ apply-planner → plan-cli → status
          └───────────────────────────────┘            │
```

调度与固定集成顺序：

1. Wave 1：`feat/target-observation`，包含最小共享 model、scope/render/hook descriptor 接缝与
   observation/alias join。
2. Wave 2：`feat/decision-engine`，唯一拥有 owned、L/S、M1 kind migration、Precond/state effect。
3. Wave 3：`feat/prune-planner` 与 `feat/hook-planner` 原从同一 wave base 启动；发现 prune 必须
   回改共享 orphan identity contract 后立即撤销并行，实际按 prune→hook 串行。hook branch 以
   明确 merge commit 非重写同步 current main，复测并完整复审后按预定顺序集成。
4. Wave 4–6：`feat/apply-planner`、`feat/plan-cli`、`feat/status` 依次从当时 main 创建。

每个 worker 先确认 `pwd` 与 Git 顶层均为分配 worktree，创建并先提交独立 active ExecPlan；
实现按行为形成多个 commits，运行窄测、完整 diff check、`make check` 并保持 clean。未参与实现
的 reviewer 复用停止写入的 worker worktree；有效 finding 使用新 fix commit，最多三轮完整复核。

## Milestone Contracts

### `feat/target-observation`

建立最小自包含 plan primitives 与只读 target snapshot；完整 desired 与 state 按 target identity
合并，保存 missing/symlink/regular/directory/special、raw link text、bytes/hash/mode 和来源，
单个历史 alias 不成为 orphan，多个 state key 相同 identity 继续 fail closed。补齐 manifest 的
完整结构、scope、scoped render 与 hook descriptor 窄接缝，但不做 decision 或文件 mutation。

### `feat/decision-engine`

纯函数实现 L1–L6、S1a–S3、`--force` 的计划转换及 symlink↔scaffold migration；每个动作携带
执行所需 payload、观测 Precond 和成功/失败 state effect。managed/rendered 拒绝，不做 IO。

### `feat/prune-planner`

复用同一 ownership/identity，按全量或部分 module scope 产生 P1–P3；整模块 orphan 组基于完整
desired；任一 unresolved conflict 使全部 prune deferred，`--no-prune` 不产生 prune 动作。

### `feat/hook-planner`

只读取 M1 字符串 run_once 的 script bytes 与 executable 分类，按模块字节序、模块内声明顺序
产生 fingerprint/skip/run-hook；partial scope 不计划其他模块，旧历史不清理，文件 conflict
不阻塞 hook，绝不执行脚本或写指纹。

### `feat/apply-planner`

成为唯一组合入口：runtime load→完整 desired/path validation→scope 校验与 scoped render→
observation/alias join→decision→prune/hook→plan validation。任何失败返回零可信 plan；结果纯只读、
稳定排序且自包含。

### `feat/plan-cli`

接入 `dot diff` 与 `dot apply --dry-run`，稳定打印 context/action lines 和 1>3>2>0；占锁时仍可
运行且完整隔离树不变。裸 apply、`--adopt` 与非 dry-run mutation 在任何锁/写入前硬拒绝。

### `feat/status`

投影同一 observation/plan 为 DRIFT、PENDING、ORPHAN / PENDING PRUNE、UNASSIGNED；actionable
finding 退出 2，unassigned-only 仍 `Clean.`/0，invalid manifest/state 退出 1 且不宣称 Clean。

## Validation and Acceptance

| 必须成立的性质 | 主要证据 | 状态 |
|---|---|---|
| L/S/P 与 M1 kind migration 完整 | planner table/filesystem tests | 通过 |
| alias 合并、部分 scope、完整 profile collision | planner integration tests | 通过 |
| scoped template fail-fast 与 self-contained payload | manifest/planner tests | 通过 |
| hook 指纹、顺序、scope 与不受 conflict 门控 | hook planner tests | 通过 |
| diff/dry-run/status 输出与退出码 | CLI golden-style assertions | 通过 |
| 全部只读路径零锁零写入 | 隔离根整树快照 + 预占锁 | 通过 |
| 完整 Checkpoint 本地门禁 | checkpoint diff check + make check | 通过 |
| 精确最终 HEAD 远端 macOS/Linux CI | GitHub Actions | 待验收：本 Goal 不 push |

每个 Milestone 运行其窄测、适用重复测试、`git diff <effective-base>...HEAD --check` 与
`make check BINARY=/private/tmp/<unique>`。最终至少运行：

    git diff bd6f4fcc05a6cd8db2fda1b2fb84baebfb11ab4a...main --check
    make check BINARY=/private/tmp/dot-cp3-acceptance

本地平台是 Darwin/arm64；交叉编译不能替代 Linux runtime。精确最终 HEAD 未触发远端 CI 时，
结论必须写“本地验收通过、远端待验收”。

## Safety, Authorization, and Recovery

当前用户 Goal 已明确授权本 Checkpoint 的 coordinator/Milestone/integration-fix/acceptance-fix
branches、`/private/tmp` worktrees、范围内修改、stage、commit、计划迁移、freshness merge 和
本地 fast-forward-only main 集成；该证据只适用于本次 Goal，不由计划延续。

测试只使用 `t.TempDir()` 或 `/private/tmp` 的合成 HOME/repo/config/state/backup，不读取或写入
真实 modules、machine config、state、backup、`.env` 或主力 HOME。纯计划手工验证也必须同时
重定向 HOME、repo、config、state 与 backup。失败保留最近成功 commit；不 amend、rebase、
cherry-pick、squash、reset、force 或删除 branch。只对本 Goal 创建且 clean、已合入 worktree
使用不带 `--force` 的移除。

## Interfaces and Dependencies

共享 plan model 只表达 desired/observed/state/action/Precond/state-effect 与 hook descriptor，
不包含 executor 或文件系统抽象。observation 形成不可变快照；decision 唯一表达 ownership；
prune 复用 ownership；hook 独占 fingerprint；apply planner 只组合；CLI/status 只投影。

CP3 不新增依赖：标准库 `os.Lstat`、`os.Readlink`、`crypto/sha256` 与稳定排序足够。若实现证明
必须新增依赖，先按用户要求完成维护性、license、Go directive、传递图与替换成本调查；出现
需要用户取舍的结果即停止。

## Surprises & Discoveries

- Observation: 共享 planner model 与 desired/state identity join 当前不存在。
  Evidence: 三路 Plan Gate 审计及 `internal/manifest`、`internal/runtime`、`internal/state` API。
  Impact: observation 不与 decision 并行；最小 model 随前者先合入并接受独立 review。

- Observation: `ResolvedProfile.Enumerate` 会渲染完整 profile，而规范要求部分调用只渲染 scope。
  Evidence: `internal/manifest/desired.go` 的 `Enumerate` 与私有 `ValidatedProfile.entries`。
  Impact: target-observation 先补结构 desired→scope→scoped render 的窄接缝，完整 path validation
  仍使用未缩小集合。

- Observation: 沙箱内 Go 1.26 module stat cache 写入被拒会让 golangci-lint 误报无 Go 文件。
  Evidence: 首次基线门禁日志与 `go list`；最小 runtime approval 后相同 `make check` 通过。
  Impact: 后续完整门禁使用已获批的 `make check` 路径，不把环境限制误判为代码失败。

- Observation: 只保存 profile/GOOS/data/modules 仍不足以让 validated capability 约束 scoped render。
  Evidence: Wave 1 首轮 reviewer 证明 HOME A validation 后可用 HOME B 形成混域 template/hook 输入。
  Impact: `ValidatedProfile` 同时绑定完成全局路径校验时的 clean HOME；后续 scope 必须精确匹配。

- Observation: 仅保存 leaf object snapshot 不足以表达未来 executor 的完整提交前提。
  Evidence: Wave 2 reviewer 用祖先 symlink A→B 改指证明两个 leaf 均 missing 时仍可能换了逻辑 target。
  Impact: observation 的不透明 `paths.TargetResolution` 贯穿所有 action Precondition；executor 仍须
  重新解析并复核 control-plane boundary。

- Observation: decision 完成后，orphan 计划对象仍未保存 plan-time target resolution。
  Evidence: Wave 3 启动检查发现 P2 target delete 的 Precondition 无法从 `OrphanTarget` 自包含构造。
  Impact: 撤销 prune/hook 并行；prune 先补共享 orphan identity 并完成 review/main 门禁，hook 随后
  以非重写 freshness merge 消费该 contract，未通过 adapter 或复制逻辑强行并行。

- Observation: 分别有效的 file plan 与 prune plan 仍可能在组合顺序上互相破坏。
  Evidence: apply-planner 首轮 review 证明 active P2 orphan symlink 可成为完整 desired 的祖先；
  create/adopt 后再 prune 会使刚满足的 desired 不可达，partial 还可能影响未请求 module。
  Impact: combined validation 以完整 observed desired 与实际 `DeletesTarget()` 的 P2 resolution 做
  cross-check；full/partial 与非 active 对照回归通过后，round 2 完整复审 GO。

- Observation: status 按单一 reason 而非 action verb 区分 DRIFT/PENDING 会遗漏同类 conflict。
  Evidence: Checkpoint Acceptance 证明 L5 unowned link 与 L6 regular/directory/special blockers 都是
  `ActionConflict`，但旧 projection 仅把 `ReasonLinkDrift` 归 DRIFT。
  Impact: acceptance fix 按封闭 action verb 统一 `ActionConflict`→DRIFT，保持其他 non-skip file
  action→PENDING；退出码、planner/state contract 与 mutation 边界不变。

## Decision Log

- Decision: 不新增 `feat/planner-model` branch，把最小共享 model 纳入 `feat/target-observation`。
  Rationale: Checkpoint 只列出七个候选 Milestone；保守串行先稳定 model，既避免扩大 branch 集，
  也满足“不确定时默认串行”。
  Date: 2026-07-19

- Decision: 只允许 prune 与 hook 有条件并行，固定 prune 先集成。
  Rationale: decision/ownership 已稳定后两者职责和文件范围可分离；任何回改共享 contract 即撤销
  并行，不复制逻辑制造并行。
  Date: 2026-07-19

- Decision: Wave 3 实际改为 prune→hook 串行。
  Rationale: `OrphanTarget.Resolution` 属于共享 plan model 与 P2 Precondition；命中并行撤销条件后，
  先由 prune branch 单一拥有 contract 变更，再让 hook 通过显式 freshness merge 消费 current main。
  Date: 2026-07-19

## Outcomes and Handoff

Checkpoint 3 已完成本地交付。七个 Milestone 按 DAG 依次完成独立 ExecPlan、测试先行、语义
commits、完整 review、closure、freshness 与 main 门禁；Wave 3 因共享 orphan resolution contract
撤销并行，实际安全串行为 prune→hook。所有 worker/acceptance-fix worktree 已在 clean、合入后
无 force 移除，branches 按授权保留。

最终 code/Acceptance HEAD 是 `b1de0fa`。三名未参与实现的 reviewer 从 checkpoint base 重审完整
Checkpoint：规范主线、数据保护、平台/架构均 GO，无 unresolved P0–P3 finding。首轮 Acceptance
唯一 taxonomy P2 已在 `fix/m1-planner-acceptance` 以共享根因修复并重新整体验收。coordinator 以
`6e1e9ad chore(integration): 同步 CP3 最终基线` 非重写合入最终 code main；本计划迁移至
completed 后只再产生纯计划 closure commit。

最终本地证据：

    git diff bd6f4fcc05a6cd8db2fda1b2fb84baebfb11ab4a...main --check
    make check BINARY=/private/tmp/dot-cp3-acceptance-final

以上均退出 0；lint 0 issues、全仓 race/build/doctor-manifest 通过。独立平台 reviewer 另完成相关
六包 20 次、uncached race，以及 Darwin/Linux × amd64/arm64 CLI test binary 交叉编译。本机实际
运行平台为 Darwin/arm64；交叉编译不等于异平台实机运行。精确最终 HEAD 未 push，远端
macOS/Linux CI 与 Linux/异架构实机未运行：本地验收通过、远端待验收。

交付能力是纯只读、自包含 planner，以及 `dot diff`、`dot apply --dry-run`、`dot status`；裸
`dot apply` 与 `--adopt` 在 CP5 前继续于 runtime/lock/write 前明确拒绝。真实 executor、target/state
mutation、hook 执行成功后的 state 持久化、managed/rendered、文本 diff、workflow 与 M2/M3 仍按
roadmap 留待后续 Checkpoint。
