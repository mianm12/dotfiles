# chore/m1-cp5-orchestration：交付 backup、force、prune 与公开 apply

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。本 Checkpoint 依用户明确选择，以一条 coordinator Goal 编排多个独立
Milestone branch；这是对默认一 Goal/一 branch 组织方式的有意例外。

## Purpose / Big Picture

完成后，M1 的真实 `dot apply` 能在同一锁周期内执行 link/scaffold、显式 force 的可恢复
替换、受收敛门控的 P1/P2/P3 prune，并按成功前缀原子提交 state。用户能看到每个成功备份的
精确路径；dry-run 继续无锁零写入。作用域内存在任何 `run_once` 时，CP7 前真实 apply 在
file、backup、prune 或 state mutation 前拒绝，不静默跳过 hook。

## Scope / Non-goals

范围内：

- 独立 backup store：普通文件字节与九位权限、symlink 原始 link text、0700 边界、不覆盖、
  文件级持久化和精确路径。
- `--force` 的 regular/symlink backup-replace 与 S2 缺失 scaffold 显式重建；提交时 Precond、
  原子替换、目录/特殊对象拒绝。
- P1/P2/P3 active prune、full/partial scope、整模块确认、收敛/deferred 门控、mixed
  upsert/delete state transition、部分成功与重跑收敛。
- 公开 non-dry-run `apply` 的 flags、确认、输出、错误/退出码映射，以及 README 当前实现边界。

明确不做：

- managed/rendered、`--adopt`、hook 执行、M2/M3 能力、state v1 格式变化。
- 目录/特殊对象备份替换、owner/ACL/xattr/flags/timestamp、自动清理备份。
- afero、通用 rollback、filesystem transaction 或其他新依赖。

## Contract and Context

- `docs/02-architecture.md` §2/§4/§6：控制面、锁周期、pipeline、动作结果与 state 处置。
- `docs/04-cli-spec.md` §2–§4.4/§5：apply scope/flags/确认/输出与退出码。
- `docs/05-apply-engine.md` §1–§7/§10：ownership、L/S/P、Precond、force/backup、收敛与幂等。
- `docs/06-templates.md`：M1 只消费 link/scaffold，scaffold 永不拥有 target。
- `docs/08-testing.md`：真实文件系统、提交点、失败恢复、dry-run 与双平台证据。
- `docs/09-roadmap.md` §1/§3：M1 安全内核与先执行后公开的边界。

Checkpoint base 是本地 clean `main@f7da6a63d76103cabcaaf329a878018dbbb333f8`；
`origin/main@e9e8bac6e5c1406e0db8aeb6e9eca6194aeeddb2`，本地 ahead 72；仅有 origin，
不存在 `upstream/main`。本 Goal 不 fetch/pull。CP4 completed coordinator 明确已完成并合入。
Plan Gate 时 CP5 目标 branches 均不存在，只有 main worktree。

现有 planner 已形成 `FileBackupReplace`、S2、P1/P2/P3、scope、confirmation groups、
Precondition 与 StateEffect；CP4 runner 已提供锁内 exact-input plan、file success-prefix 和单次
state Store。真实缺口在 backup 持久化、force/prune executor、mixed transition 和 CLI wiring。
当前 runner 会把 `executor.ErrPrecondition` 当普通 error；CP5 必须按规范把执行期 Precond
失配分类为 unresolved conflict，保留 target/state、延迟全部 prune，并最终映射 exit 3。

## Progress

- [x] 2026-07-20：确认 main clean、CP4 完成、目标 branch 不存在；记录 checkpoint/main/origin/
  upstream/worktree 状态，未 fetch/pull。
- [x] 2026-07-20：三名只读 subagent 完成规范缺口、DAG/共享契约、测试/依赖/双平台复核；
  无停止条件，不需新依赖。
- [x] 2026-07-20：基线 `make check` 在 Darwin/arm64 通过；Go/lint cache 隔离到 `/private/tmp`。
- [x] 2026-07-20：创建 coordinator branch/worktree 与本 active ExecPlan。
- [x] 2026-07-20：backup-store 第一轮完整 review 的目录链持久化 P1 由 `ae2a9e1` 修复；round 2
  完整复审 GO，无 P0–P3，正在执行最终门禁与 plan closure。
- [x] 2026-07-20：prune-executor 第一轮 P1 由 `4782f91` 精确错误分类修复；round 2 GO；以
  `4e92a11` freshness 合入 backup main 后再次完整 review GO，closure `0499de9` FF-only 合入。
- [x] 2026-07-20：Wave 1 完成。backup `79d3713`、prune `0499de9` 均在 main 上通过窄测与
  隔离 cache 的 `make check`；两个 worker worktree 已确认 clean/合入后无 force 移除。
- [x] 2026-07-20：force-replace 第一轮两个 P2 由 `6e6c3f7`、`b1e1df2`、`08ce774`、
  `004b09c` 修复；round 2 的失真注释 P3 由 `7b0a712` 修复；round 3 完整 review GO。
- [x] 2026-07-20：force-replace closure `e0d2243` FF-only 合入 main；backup/executor/apply 窄测
  与隔离 cache `make check` 通过，worker worktree clean/已合入后无 force 移除。
- [ ] 2026-07-20：apply-cli 第一轮完整 review 发现 runner 聚合计数无法精确投影运行期 file
  conflict 与部分 prune 后的 deferred 边界 P2；finding 已验证有效，worker 正新增逐 action outcome
  和公开输出/exit 回归，之后完整复审。
- [ ] 2026-07-20：apply-cli round 2 确认逐 action contract 已闭合第一轮 P2，但发现 prune
  `ActionConflict` 仍被展示为 deferred 的 P2；worker 正校正为具体 `CONFLICT` target，之后进行
  本有效基线第三轮完整 review。
- [ ] 2026-07-20：**停止条件命中**。apply-cli round 3 确认 round 2 P2 已修复，但发现新的
  blocking P2：outcome validator 未拒绝“planner 已 deferred 的 prune 却报告 succeeded”，可导致
  stdout 显示 deferred 而 exit 0。按本 Goal 规则第三轮后停止，不继续补丁、不 closure、不合入 main；
  等待用户裁决后续处理方式。
- [x] 2026-07-20：用户明确授权把上述 P2 建立为新的串行 fix Milestone，使用独立 ExecPlan
  和新的 review 轮次；Goal 已恢复 active。fix 从 clean `feat/apply-cli@bf021c9` 创建，通过后
  FF-only 回并 apply-cli，再完成其完整复核、closure 与 main 集成。
- [ ] Wave 2：force-replace 独立计划、实现、复核、closure 和 main 集成。
- [ ] Wave 3：apply-cli 独立计划、实现、复核、closure 和 main 集成。
- [ ] 三路完整 Checkpoint Acceptance、必要 fix、coordinator closure 与 main FF-only 集成。

## Milestone DAG and Scheduling

```text
Wave 1 @ checkpoint_base
├── feat/backup-store ──┐
└── feat/prune-executor ├──> feat/force-replace ──> feat/apply-cli
                        │
预定集成：backup-store ─┘ then prune-executor

apply-cli ──> fix/m1-apply-cli-outcome-validation ──> apply-cli closure
```

backup-store 只允许新增独立 backup 机制及测试，不修改 planner/state/executor/apply/CLI shared
contract；满足此边界时与 prune-executor 从同一 wave base 并行。若该节点必须触碰共享 contract，
取消并行并停止重新规划。main 预定先集成 backup-store；prune-executor 随后执行 freshness gate，
以明确 `chore(integration): 同步 CP5 当前基线` merge commit 非重写同步 current main，重新门禁和
完整 review。force-replace 只从两者合入且 main 门禁通过后的 main 创建；apply-cli 同理最后创建。

每个 Milestone 使用独立 `/private/tmp` worktree、branch、active ExecPlan、测试先行语义 commits、
窄测、完整 diff check、`make check` 与未参与实现的 reviewer。review finding 用新 fix commit，
不 amend/rebase/cherry-pick/squash；最多三轮完整 review。

## Milestone Contracts

### `feat/backup-store`

只提供通用备份机制：在有效 backup root 下建立唯一不覆盖的 0700 批次/父目录，regular 逐字节
复制、保留九位权限并验证计划摘要，symlink 保存 raw link text 且不跟随；完成 write/chmod/
sync/close 后才返回精确路径。成功备份永不自动删除。拒绝目录/特殊对象，不理解 planner、
ownership、replace 或 state。

### `feat/prune-executor`

执行 canonical P1/P2/P3：P1/P3 在完整 target/control/leaf Precond 成立时只删除 state，P2 只在
exact owned symlink（含死链）仍成立时删除 leaf 后删账；任何拓扑或证据失配不得按 P3 摘账。
扩展 state transition 支持 file upsert 与 prune delete 的同一候选提交并保留 run_once。runner
按 files→confirmation→prune→single Store 编排；plan conflict、file error/Precond、确认拒绝使
全部 prune deferred。已成功前缀仍提交。定义确认 callback、convergence/result/error 分类供 CLI
直接消费。作用域内任何 HookAction（run 或 skip）都在执行 preflight 阶段整体拒绝。

### `feat/force-replace`

executor 消费 backup store：首次完整 Precond→成功持久备份→再次完整 Precond→原子替换
regular/symlink；备份不能延长快照。replace 失败后已成功备份保留并向 runner 报告精确路径；
目录/特殊对象继续 conflict。S2 仅在 target 最终仍缺失时以 no-clobber scaffold 创建，无备份。
state Store 失败不回滚 target/backup，重跑按 L2/S1b 收养。

### `feat/apply-cli`

non-dry-run `apply` 调用内部 runner，连接终端确认与 `--yes`，稳定打印 context、动作、warning、
成功 backup 路径和 Already up to date；退出码按 1 > 3 > 2 > 0。`--adopt` 继续在 runtime IO 前
硬拒绝，managed/rendered fail closed，run_once 不执行也不静默 skip。dry-run 复用只读 planner，
保持无锁零写入。同步 README 当前实现事实。

## Validation and Acceptance

| 必须成立的性质 | 主要证据 | 状态 |
|---|---|---|
| backup bytes/mode/raw link text/0700/不覆盖/持久化/精确路径 | backup 真实 FS 与故障注入 tests | 待验证 |
| force Precond、regular/symlink、目录/特殊拒绝、S2 | executor + apply integration | 待验证 |
| P1/P2/P3、scope、deferred、确认、dead-link ownership | planner/executor/apply integration | 待验证 |
| mixed state、部分成功、Store 失败恢复、run_once 保留 | state/apply tests | 待验证 |
| 公开输出、1/3/2/0、确认拒绝、重跑幂等 | CLI 隔离 tests | 待验证 |
| dry-run 全新 HOME 零写入；run_once 在 action mutation 前拒绝 | CLI/apply filesystem tests | 待验证 |
| macOS/Linux | 本地 Darwin + 远端 CI | 本地待验收；远端待验收 |

每个节点在其 worktree 运行窄测、`git diff <effective-base>...HEAD --check` 和使用唯一 `/private/tmp`
BINARY/cache 的 `make check`。Checkpoint 最终至少运行：

    git diff f7da6a63d76103cabcaaf329a878018dbbb333f8...main --check
    make check BINARY=/private/tmp/dot-m1-cp5-acceptance/dot

全部 mutation 验证使用 `t.TempDir()` 或 `/private/tmp` 的合成 HOME/repo/config/state/backup，清除
或重定向 `DOT_CONFIG`、`DOT_REPO` 与 HOME/XDG，并断言真实 HOME 未变化。远端 CI 未实际运行时
只写“本地验收通过、远端待验收”。

## Safety, Authorization, and Recovery

用户当前 Goal 明确授权本 Checkpoint 的 coordinator/Milestone/integration-fix/acceptance-fix
branches、`/private/tmp` worktrees、范围内修改/stage/commit/计划迁移、freshness merge 和本地
FF-only main 集成；本计划不延续该授权。禁止 fetch/pull/push/rebase/cherry-pick/squash/amend/
reset/force、branch 删除和真实私人数据访问。

失败保留最近成功 commit。仅对本 Goal 创建、已集成且 clean 的 worktree 使用无 `--force`
移除。main 出现 DAG 外提交、semantic conflict、无法证明 mutation/Precond/恢复/零写入、三轮
review 后仍 blocking，或需要改变公开/持久化/ownership 契约时，更新本计划并停止。

## Interfaces and Dependencies

不新增依赖。backup store 只提供 copy/hash/mode/raw link text/sync/unique-path 通用机制；
ownership、Precond、replace 与恢复仍由 planner/executor/apply 表达。共享 contract 是 canonical
FileAction/PruneAction、executor result、mixed state transition、runner convergence/result 与确认
callback；prune-executor 先定稿，force 扩展 backup result，apply-cli 最后消费。

## Surprises & Discoveries

- Observation: planner 已完成 force/S2/P1–P3 纯计划，但 CP4 scope gate 主动拒绝这些执行能力。
  Evidence: `internal/planner/decision.go`、`prune.go`、`prune_plan.go`、`internal/apply/scope.go`。
  Impact: CP5 不重写决策表，只补 executor/runner/CLI。

- Observation: `state.TransitionEntries` 只支持 upsert，runner 把任何 executor error 当运行错误。
  Evidence: `internal/state/transition.go`、`internal/apply/run.go`。
  Impact: prune 节点必须建立 mixed transition 和 Precond→conflict 单一分类，CLI 不得重复推断。

- Observation: HookSkip 仍证明作用域内存在 run_once；仅拒绝 HookRun 会静默跳过已执行 hook。
  Evidence: planner hook action model 与 CP5 明确门禁。
  Impact: CP5 真实 apply preflight 拒绝任何 scope HookAction；partial scope 仍只受请求模块影响。

- Observation: 首次创建 backup root 时，只 sync root 不能持久化 root 在 state root 中的目录项。
  Evidence: backup-store round 1 review 对 `internal/backup/batch.go` 与 `save.go` 的完整数据流审查。
  Impact: backup store 必须在报告成功前同步本轮新建目录链；否则 force 的“备份成功”前置不成立。

- Observation: 现有 executor 的 `ErrPrecondition` 同时包装纯证据失配、观测 IO，并可能与 cleanup
  error 组合；单独使用 `errors.Is` 不能决定是否降级 conflict。
  Evidence: prune-executor round 1 review 对 file/prune executor 到 runner 错误链的完整审查。
  Impact: executor 必须提供精确、可组合的 mismatch 分类；任何 IO/cleanup 成分保持运行错误。

- Observation: backup store 在复制过程中也会检测 target evidence 变化；S2 no-clobber 的 EEXIST
  同样是 missing 前提失效，而不是普通 IO。
  Evidence: force-replace round 1 review 对 backup.Save*、executor 与 runner 分类链的审查。
  Impact: 两条路径必须进入同一 pure mismatch 协议；一旦混入 cleanup/IO 仍按运行错误处理。

- Observation: 静态 ApplyPlan 加聚合 `UnresolvedConflicts`/`PruneAttempts` 不能重建执行期每个
  action 的最终展示状态。
  Evidence: apply-cli round 1 review 对 runner Result 与 `projectApplyResult` 的交叉审查。
  Impact: runner 必须提供逐 action outcome；CLI 只投影事实，不按计数或字符串前缀猜测。

- Observation: 跨组件 outcome validator 还必须保持 planner 的静态安全门禁单调不减；执行结果
  不能把 planner 已 deferred 的 prune 提升为 succeeded。
  Evidence: apply-cli round 3 review 构造 deferred plan + succeeded outcome，当前可输出 deferred
  文案却返回 exit 0。
  Impact: 这是有效 P2，但第三轮完整 review 后仍存在 blocking finding，已按停止条件暂停。

## Decision Log

- Decision: Wave 1 条件并行，预定 backup-store 后 prune-executor 集成。
  Rationale: backup 机制可独立于 action/state；prune 修改 shared runner/state。若边界漂移即取消并行。
  Date: 2026-07-20

- Decision: force-replace 等待 Wave 1 全部集成，apply-cli 最后。
  Rationale: force 会扩展 executor/result，CLI 消费所有最终确认、convergence、backup 和退出契约。
  Date: 2026-07-20

- Decision: 锁创建不视为被 run_once 门禁禁止的 file/state mutation。
  Rationale: 规范 pipeline 要求 mutation command 先持锁再 strict load；run_once 只能在 load/plan 后发现。
  门禁仍必须先于任何 target、backup、prune 或 state 提交，且不得执行或静默跳过 hook。
  Date: 2026-07-20

- Decision: 将 apply-cli 第三轮 P2 作为新的串行 fix Milestone，而不继续在原 review 单元堆补丁。
  Rationale: 用户在停止后明确授权独立 ExecPlan 与新的 review 轮次；新节点只闭合 planner
  deferred prune 与 outcome 单调性，不扩大公开行为、state 格式或 CP5 Scope。
  Date: 2026-07-20

## Outcomes and Handoff

尚未完成。Plan Gate 已通过；下一步提交 coordinator ExecPlan，然后启动 Wave 1。
