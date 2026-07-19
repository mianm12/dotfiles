# feat/decision-engine：形成纯函数 M1 文件决策

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，planner 可以把一个已经完成 target identity join 的 `ObservedTarget` 纯函数地转换为
自包含动作：M1 的 link/scaffold 决策、所有权判断、metadata adopt、force 分支与两种 kind
迁移都有确定结果，且每个结果携带提交前 target 快照及成功/失败后的 state 处置。该能力不读取
文件系统、不写 state，也不执行动作；表驱动测试可以直接证明 05 号规范每个 M1 决策分支。

## Scope / Non-goals

范围内：

- 唯一实现 symlink/scaffold 的 `Owned`，symlink 只比较 raw `link_dest`，scaffold 恒不 owned。
- 实现 L1–L6、S1a–S3 的自上而下短路语义，包括 metadata adopt、L3 重链、L4/L5 区分、
  可 force 的 symlink/普通文件 backup-replace 与 S2 无备份重建。
- 实现 M1 的 symlink→scaffold 与 scaffold→link：前者在旧链仍 owned 时计划转换成独立蓝本，
  否则只以 state-only adopt 释放所有权；后者按 link 无记录语义决策，旧记录保留到成功动作。
- 最小精炼 `internal/planner/model.go`，使 action 明确保存 reason、Precondition、成功/失败 state
  effect，并能在展示 key 变化时表达用新 key 替换历史 alias。

明确不做：

- 不实现 managed/rendered、prune、hook、CLI、executor、state builder/store、lock 或 filesystem IO。
- 不新增依赖，不引 filesystem abstraction、临时 adapter、重复 ownership 或 M2/M3 行为。
- 不修改公开规范、持久化格式、runtime/manifest/path contract 或其他 worktree。

## Contract and Context

- `docs/05-apply-engine.md` §3.1–§3.4：`owned()`、L/S 表、metadata 刷新及 kind migration 是本切片
  的直接契约；规则按 desired kind 分派并自上而下短路。
- `docs/02-architecture.md` §5–§6：planner 不修改 target，动作必须携带执行所需 payload、观测
  前提和成功后的 state 处置，skip/conflict/失败保留旧 state。
- `docs/04-cli-spec.md` §4.2、§5：force 只把规范允许的 conflict 转为 backup-replace，目录和
  特殊文件仍拒绝；本分支不接输出或退出码。
- `docs/06-templates.md` §1：scaffold 记录只表示一次性生命周期，不提供删除所有权。
- `docs/08-testing.md` §3.1、§3.3：纯规则测试覆盖完整 L/S 矩阵、短路、迁移和 state 处置。
- `docs/09-roadmap.md` §1 M1、§3：只交付 link/scaffold 的纯计划；managed/rendered 必须 fail
  closed，真实 mutation 属后续 Checkpoint。

基线为 clean `feat/decision-engine@712ab85`。`internal/planner/model.go` 已定义 M1 desired、
observation、history 与 action 骨架；`ObserveProfileTargets` 已完成 desired/state identity join，
因此 decision 不再解析路径或读取磁盘。当前缺口是没有纯 decision，也没有单一 ownership
实现；action 仅有一个含义不够明确的 state effect，不能完整表达失败 preserve 或 alias key 替换。

## Progress

- [x] 2026-07-19：确认分配 worktree、Git 顶层、branch 与 clean 基线，读取仓库约定、规范、
  target-observation handoff 和当前 planner model。
- [x] 2026-07-19：以 `e5e4730` 提交本 active ExecPlan 起点。
- [x] 2026-07-19：测试先行暴露缺失的 ownership、完整 L/S 表、force、metadata adopt 与
  成功/失败 state effect，随后完成最小纯函数实现并通过窄测。
- [x] 2026-07-19：以 `118f98e` 提交 ownership、完整 L/S 表、force、metadata adopt 与自包含
  state effect。
- [x] 2026-07-19：测试先行实现 M1 kind migration；以 `e6fe61b` 提交 symlink→scaffold 与
  scaffold→link，managed/rendered 与未知 observation 保持 fail closed。
- [x] 2026-07-19：planner 20 次重复与 race、Darwin/Linux amd64 测试二进制交叉编译、
  `git diff 712ab85...HEAD --check` 和完整 `make check` 全部通过；worker handoff 保持计划 active，
  等待 coordinator 安排独立复核。
- [x] 2026-07-19：Wave 2 首轮 reviewer 提出两项有效 finding：action Precondition 丢失 plan-time
  target identity，且 scaffold force 短路缺少显式回归。先以真实 ancestor symlink A→B 改指测试
  复现 leaf 表象相同但 identity 已变化，以 `1905b15` 把不透明 `TargetResolution` 从 observation
  传到所有 action；再以 `5ea7a91` 固定 S1a/S1b force 不替换和 S2 stale+force 优先重建。
- [x] 2026-07-19：review fix 后 planner/paths 20 次重复与 race、Darwin/Linux amd64 planner
  交叉编译、完整 branch diff check 与 `make check` 再次全部通过。
- [x] 2026-07-19：第二轮对完整 branch 独立复核 GO，无 P0–P3 finding；主线程复跑窄测、
  base diff check 与 `make check BINARY=/private/tmp/dot-cp3-decision-engine-final` 全通过，
  完成 lifecycle closure。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定范围、基线、契约、commit 边界和验证方式。

    git diff --check
    git add .agent/plans/active/m1-decision-engine.md
    git diff --cached --check
    git commit -m 'docs(planner): 新建 decision engine 计划'

### Milestone 2：固定 ownership 与 L/S 决策

先以表驱动测试覆盖 `Owned`、L1–L6、S1a–S3、metadata adopt、force 与所有动作的 target
Precondition、成功/失败 state effect；再实现最小纯函数。模型只做支撑这些规范事实所需的精炼，
不预建 executor。

    go test ./internal/planner
    go test -count=20 ./internal/planner

验收：每个表行与短路边界均有直接断言；skip/conflict/失败 preserve，成功 create/adopt/
backup-replace/scaffold upsert；普通文件以外的 L6 force 仍 conflict。

Commit 边界：

    feat(planner): 实现 link 与 scaffold 决策

### Milestone 3：固定 M1 kind migration 与失效安全

先增加 symlink→scaffold owned/non-owned/missing 与 scaffold→link 的 L1/L2/L5/L6 代表场景，
证明旧记录不会凭迁移创造所有权，且失败前保持旧记录；增加 managed desired/rendered history
拒绝测试，再完成最小实现。

    go test ./internal/planner
    go test -count=20 ./internal/planner

验收：owned symlink 转成独立 scaffold；其他 symlink→scaffold 只 state adopt；scaffold→link
严格按无记录 L 表；不支持类型返回 error 且不产生 action。

Commit 边界：

    feat(planner): 实现 M1 kind 迁移

### Milestone 4：验证并交接 review

从 worktree root 运行重复、race、完整门禁和 branch diff 审计；只更新本 active 计划的真实证据，
不迁移 completed，等待 coordinator 安排独立复核。

    go test -count=20 ./internal/planner
    go test -race ./internal/planner
    git diff 712ab85...HEAD --check
    make check BINARY=/private/tmp/dot-cp3-decision-engine-check/dot

成功判据是全部退出 0，完整 diff 只含本计划与 planner decision/model/tests，worktree clean。

Commit 边界：

    docs(planner): 记录 decision engine 验证

## Validation and Acceptance

| 性质 | 证据 | 状态 |
|---|---|---|
| raw symlink ownership 与 scaffold 非所有权 | `Owned` 表驱动测试 | 本机通过 |
| L1–L6、S1a–S3 与短路顺序 | decision matrix tests | 本机通过 |
| metadata adopt、force 与 state effect | action payload tests | 本机通过 |
| symlink↔scaffold M1 migration | migration table tests | 本机通过 |
| managed/rendered fail closed | invalid-kind tests | 本机通过 |
| 祖先 symlink 改指使 target identity Precondition 失配 | real filesystem regression | 本机通过 |
| S1a/S1b/S2 force 短路 | scaffold decision matrix | 本机通过 |
| 当前平台完整门禁 | `make check` | 本机通过 |
| Darwin/Linux 构建 | amd64 planner test binaries | 交叉编译通过，Linux 未实机运行 |
| 远端 macOS/Linux CI | 精确 branch HEAD | 待验收（本 worker 不 push） |

## Safety, Authorization, and Recovery

本任务授权仅覆盖 `/private/tmp/dot-cp3-decision-engine-019f795e` 中当前 branch 的计划、planner
代码、测试、stage 与语义 commits。实现是纯函数；测试只在 `t.TempDir()` 合成 HOME/root 内
创建目录、普通文件和 symlink，state 路径保持缺失，不读取私人数据。失败保留最近
成功 commit，以新 commit 修复；不切 branch，不 merge、amend、rebase、reset、force，不操作
main/coordinator/其他 worktree。

## Interfaces and Dependencies

decision 消费 `ObservedTarget`，返回自包含 `Action` 与 error。`Owned` 是 decision 与后续 prune
共享的唯一所有权入口；action 的 state effect 表达计划语义，不负责构造或持久化 `state.Snapshot`。
只依赖标准库、当前 `internal/planner` model 与既有 `internal/paths` 不透明 resolution。

## Surprises & Discoveries

- Observation: strict state 的 `AppliedAt` 必然非空，但它按规范只用于诊断，不能参与 metadata
  是否过期的决策。
  Evidence: `HistoricalState` 同时保存 ownership/metadata 与 `AppliedAt`；直接比较整个 struct 会让
  每条合法记录永久落入 adopt。
  Impact: metadata 比较只列举 key/module/kind/source 及 link 的 `link_dest`；upsert 的新时间由
  未来 executor 在动作成功后填入。

- Observation: target observation 会把单个历史 alias 与 current desired 对齐；若 metadata adopt
  只 upsert 新展示 key，旧 key 会残留并在下次 strict load 形成重复 target identity。
  Evidence: `ObservedTarget.State.Key` 可以与 `Desired.Target` 不同，二者仍是同一 identity。
  Impact: `StateEffect.PreviousKey` 明确要求 upsert 同一提交摘除旧 alias；失败分支仍 preserve。

- Observation: leaf 的 kind/link text/content/mode 全部不变，仍不能证明 plan-time target 的逻辑
  位置未改变；既有祖先 symlink 可以在 plan 后从目录 A 改指目录 B，而两侧 leaf 都同为 missing。
  Evidence: reviewer finding 的真实文件系统回归在 `HOME/root/A`、`B` 与 `HOME/current` ancestor
  symlink 上稳定复现；改指前后 `Observation` 相同而 `paths.TargetResolution.Equal` 为 false。
  Impact: `ObservedTarget` 和所有 `Action.Precondition` 保存同一个不透明 plan-time resolution；
  executor 必须执行时重新解析并比较，同时重新运行 control-plane boundary，不能只复核 leaf。

## Decision Log

- Decision: decision 只消费 target-observation 已对齐的值，不再次解析路径或读取 state。
  Rationale: identity/alias join 已有单一实现；重复 join 会产生第二真相源并破坏纯函数边界。
  Date: 2026-07-19

- Decision: action 同时表达成功与失败 state effect，并让 upsert 可替换旧 alias key。
  Rationale: 02 号规范要求成功/失败处置明确；metadata adopt 的展示 key 变化若只 upsert 会残留
  同 identity 旧 key，形成下一次加载的重复 state。
  Date: 2026-07-19

- Decision: 使用封闭 `ActionReason` 分类，而不是让后续 CLI/status 解析人类 reason 字符串。
  Rationale: L4/L5、普通文件/目录/特殊对象和 scaffold 删除状态需要稳定区分；实际展示文案属于
  presentation 层。
  Date: 2026-07-19

- Decision: S2 中显式 force 优先于 metadata adopt；迁入 scaffold 时 force 不改变释放所有权规则。
  Rationale: force 明确授权仅在 target 仍缺失时重建 scaffold，成功动作同时写入当前 metadata；
  迁入 scaffold 的安全原则则要求非-owned 对象永不因 force 被替换。
  Date: 2026-07-19

- Decision: 直接携带 `paths.TargetResolution` 不透明值，不在 planner 复制 identity/ancestor 字段。
  Rationale: paths 是 target identity 与遍历拓扑的单一真相源；该类型不暴露可变内部存储，值复制
  保持只读 snapshot 语义，也避免 planner 建立第二套路径表示。
  Date: 2026-07-19

## Outcomes and Handoff

Milestone 已完成本地收口。branch base 为
`712ab85`；语义 commits 为计划起点 `e5e4730`、完整 M1 ownership/L/S 决策与 action model
`118f98e`、M1 kind migration `e6fe61b`、首轮验证 `4d9cadd`、target identity Precondition 修复
`1905b15` 与 scaffold force 回归 `5ea7a91`，以及本次 review fix 验证记录。

实现新增纯函数 `Owned` 与 `Decide`。完整 L1–L6、S1a–S3 及 force 分支均产生带稳定 reason、
target/source Precondition、desired payload 和成功/失败 state effect 的 action；metadata adopt
可以替换旧 alias key。symlink→scaffold 只有旧链仍 owned 时计划转换成独立蓝本，否则只写
scaffold state 释放所有权；scaffold→link 按无记录 L 表决策且冲突/失败保留旧记录。不支持的
managed/rendered/未知 object kind 返回 `ErrUnsupportedDecisionInput` 和零值 action。

首轮 review 的 P1 已通过 plan-time `TargetResolution` 闭合：所有 decision action 保存
observation 阶段的同一 identity snapshot，真实 ancestor symlink 改指即使 leaf 表象相同也会使
执行时重新解析结果失配；未来 executor 仍须重新校验 control-plane boundary。P2 以独立测试
证明 S1a/S1b 的 force 不触碰现有 target，只有 S2 target 缺失时 force 才优先重建。

本机 Darwin/arm64 上 review fix 后 planner/paths 20 次重复与 race、全仓
`make check BINARY=/private/tmp/dot-cp3-decision-engine-review-fix-check/dot`、完整 branch diff check
均通过；Darwin/Linux amd64 planner 测试二进制交叉编译通过，Linux 未实机运行。未新增依赖，未修改
manifest/runtime/state store、CLI 或 executor。精确 branch HEAD 的远端 macOS/Linux CI 未运行，
因此结论为“本地验收通过、远端待验收”。第二轮对完整 branch 复审 GO，无 P0–P3 finding；
主线程在 review 后再次运行窄测、base diff check 与完整 `make check`，全部通过。当前满足
review-ready 与 lifecycle closure 条件，可由 coordinator fast-forward-only 集成本地 main。
