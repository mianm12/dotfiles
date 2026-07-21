# feat/add-link：安全发布 link source 并提交 target

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和
`Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成后，内部 add runner 能在一个 mutation lock 周期内重新加载 strict inputs、按原请求重建
完整 batch preflight，并且只有全批次通过后才逐项执行默认 link 收编。每项先把输入 bytes 与
九位 mode 复制成 repo 中独立、完整且 no-clobber 的 source，再在提交时 Precond 仍成立时把原
target 原子替换为 symlink。后项失败不回滚已提交前缀，成功前缀通过一次原子 state Store 记账；
target 或 state 提交点两侧的失败均可按规范安全重跑。

## Scope / Non-goals

范围内：

- 默认 link 的 source 独立 inode publication、no-clobber、文件与父目录 sync、保守 cleanup。
- target bytes/mode/identity、祖先拓扑、control boundary 与 source 的最终 Precond；同目录完整
  symlink 准备与原子替换。
- 同一 mutation lock 下 strict load、exact-input batch preflight、逐项执行、成功前缀
  `EntryUpdate` 与单次 `CommitState`。
- source/target/state 故障注入、hard-link sibling 隔离、多输入部分成功、等价续跑与 apply L2
  恢复证据。

明确不做：

- 不实现 scaffold、managed/`--template`、Cobra/CLI、README 或公开输出/退出码。
- 不创建 module、不修改 manifest、不执行 `git add`/`git commit`，不改变 state v1 或 ownership。
- 不引入新依赖，不建立通用 filesystem transaction，不读取或修改真实私人数据。

## Contract and Context

- `docs/02-architecture.md` §2/§4–§6：add mutation 的完整周期持同一锁，strict loaded plan 与
  executor 职责分离；成功动作才产生 state upsert，Store 失败不回滚已提交对象。
- `docs/03-manifest-spec.md` §2–§6/§8：locked preflight 仍复用 strict manifest、prospective desired
  与完整 profile 路径校验；不修改 manifest/module。
- `docs/04-cli-spec.md` §4.5：默认 link 的 source-first、target 提交点、等价续跑、多输入前缀与
  Git 手工提交边界。
- `docs/05-apply-engine.md` §4–§7/§9–§10：最终 Precond、完整旧/新对象原子可见、hard-link
  隔离、保守 cleanup、L2 崩溃收养与幂等。
- `docs/08-testing.md` §2–§3：合成 HOME/repo/config/state/backup、提交点两侧与临时产物故障证据。
- `docs/09-roadmap.md` §1/§3：本切片只交付 M1 link add 内部 mutation，不预建后续能力。

有效 base 为 clean `main@669ea06c2a7fbf4807c1392eee3170d5bed74b58`，branch `feat/add-link`。
前置 `feat/add-preflight` 已封存 `BatchPlan`/`ItemPlan`/`Snapshot`，并提供同源 prospective、
Git trackability、source variant 与完整 batch gate。`runtime.BeginMutation`、`MutationSession.Load`
和 `LoadedMutation.CommitState` 提供同一锁内 strict inputs 与单次原子 state capability；
`state.TransitionEntries` 可从 missing/loaded 基线形成 symlink upsert。现有 apply executor 的方向是
从既有 source 生成 target，不能替代 add 的反向 source publication，但其 Precond 与原子临时
symlink模式可作为实现参照。

## Progress

- [x] 2026-07-22：确认分配 worktree、branch、有效 base 与 clean 状态；完整阅读执行规则、CP6
  coordinator、completed preflight 计划、相关规范和 add/runtime/state/apply/executor 实现。
- [x] 2026-07-22：以 `5cfedca` 提交本 active ExecPlan 起点。
- [x] 2026-07-22：测试先行建立独立 source publication 与保守 cleanup；普通窄测和格式检查通过。
- [x] 2026-07-22：测试先行建立 link target 原子提交、source/target/ancestor 最终 Precond、
  hard-link sibling 隔离和可验证 per-item result；add/paths 窄测通过。
- [x] 2026-07-22：测试先行建立锁内 exact-input batch runner、执行结果 fail-closed、成功前缀
  单次 state 提交、Store 失败后的 apply L2 收养及等价 source 续跑；相关窄测通过。
- [x] 2026-07-22：add/apply/paths 普通、5 次重复、race 与定向 lint 通过；完整 base diff check、
  独立 cache/BINARY `make check` 和 Darwin/Linux amd64 add test binary 交叉编译通过。保持 active
  等待独立复核。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定 scope、提交点、恢复与验证边界。

Commit 边界：

    docs(add): 新建 add link ExecPlan

### Milestone 2：建立独立 source publication

在 `internal/add` 先用真实文件系统和窄 operations seam 覆盖 create/write/short-write/chmod/
sync/close/no-clobber publish/父目录 sync/cleanup。publication 在 source 父目录生成普通临时文件，
复制 snapshot bytes 和九位 mode，文件完整同步关闭后才 no-clobber 发布；不得 rename 或 hard-link
原 target。既有等价 source 不改写。发布前失败保留 target 并清理可证明的 temp；发布后 target
提交前失败只删除仍与本轮证据一致的新 source/temp，cleanup 无法证明或失败时保留并报告。

验收：source bytes/mode 完整且 inode 与 target/hard-link sibling 独立；所有故障点不覆盖既有
source，等价 source 重用，不等价 source 由 locked preflight 在 mutation 前拒绝。

Commit 边界：

    feat(add): 建立独立 source 发布协议

### Milestone 3：提交 link target 并验证执行协议

为单项 link 执行增加不可伪造/可校验结果。在 source 完整可用后，于 target parent 准备完整
symlink；原子替换前重新证明原 target 仍是相同普通文件（bytes、九位 mode、文件 identity）、
祖先拓扑/leaf identity 与 control boundary 未变，并证明 source 仍位于预期 module source 且为
完整普通文件。提交前失败 target 不变并按 publication ownership cleanup；原子替换为提交点，
其后 source/link 永不回滚，cleanup error 也必须保留 committed fact。zero/矛盾 result 不能产生
state effect。

验收：target/source/ancestor 最终 Precond、symlink temp create/publish/cleanup、提交点前后错误和
hard-link sibling bytes/mode/inode 不变均有测试；成功 link text 精确指向 source。

Commit 边界：

    feat(add): 原子提交 link target

### Milestone 4：编排 locked batch 与成功前缀 state

增加内部 runner，入口只接受 runtime overrides、CLI version 与原始 add `Request`。runner 先
`BeginMutation`/strict `Load`，在该 locked inputs 上重新调用 exact request `Preflight`；完整 plan
通过后才执行。执行期在首个失败处停止，已越过 target 提交点的前缀生成 symlink `EntryUpdate`，
并从 strict loaded baseline 形成一个 candidate、最多调用一次 `CommitState`。Store 失败保留
source/link；后项失败仍提交前项。结果逐项记录 deferred/succeeded/failed 与 source/target/state
提交事实，并在任何 nil/zero/矛盾依赖返回时 fail closed。

验收：锁内计划失败零 source/target/state mutation；多输入部分成功只提交成功前缀；Store 失败
保留物理结果并由正常 apply L2 收养；source 发布后 target 前的等价续跑、完整成功后的重复执行
和 state recovery 均可收敛。

Commit 边界：

    feat(add): 编排 link batch 与 state 提交

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| source 独立 inode、bytes/mode、no-clobber、sync | publication 真实 FS + seam tests | 本地通过 |
| temp/source cleanup 只删除本轮未变对象 | ownership/cleanup fault tests | 本地通过 |
| target/source/ancestor/control 最终 Precond | link executor mutation tests | 本地通过 |
| target 原子提交与提交后不回滚 | rename/cleanup/state failure tests | 本地通过 |
| locked exact-input 全批次 gate | runner/runtime tests | 本地通过 |
| 多输入成功前缀单次 state commit | runner integration tests | 本地通过 |
| state failure 后 apply L2、等价续跑、重复收敛 | add + apply recovery tests | 本地通过 |
| Darwin/Linux | 本地测试、双目标交叉编译、远端 CI | Darwin 本地通过；双目标交叉编译通过；远端待验收 |

最终在 `/private/tmp/dot-m1-cp6-add-link` 运行相关 add/apply/runtime/state package tests、5 次重复、
race、定向 lint、`git diff 669ea06c2a7fbf4807c1392eee3170d5bed74b58...HEAD --check`、唯一
`/private/tmp` cache/BINARY 的 `make check`，以及 Darwin/Linux test binary 交叉编译。成功要求
命令退出 0、完整 diff 仅含本计划与 link mutation 实现/测试、worktree clean。真实 Linux 主机和
远端 macOS/Linux CI 未运行时明确标记远端待验收。

## Safety, Authorization, and Recovery

用户已授权本 branch/worktree 的 active plan、范围内修改、stage、commit 和验证。测试只使用
`t.TempDir()` 的合成 HOME/repo/config/state/backup，显式隔离 DOT/HOME/XDG/Git；不运行涉及真实
数据的命令。失败使用新 fix commit，不 amend/rebase/reset/cherry-pick/squash；不切换或合并
main/其他 branch。

target 提交前失败必须保留原 target；只能清理仍可证明为本轮创建且未变化的 source/temp。target
提交后不删除 source/link；state Store 失败由 apply L2 恢复。若跨平台 no-clobber、原子替换、
cleanup ownership 或最终 Precond 无法证明，或实现需要改变 state v1/ownership/公开行为，则更新
本计划并停止。

## Interfaces and Dependencies

不新增依赖。共享 contract 是 preflight sealed plan、per-item publication/target commit result、
成功 symlink `EntryUpdate` 和单次 runtime `CommitState`。publication 只处理完整普通文件和
no-clobber/cleanup 机制，不理解 manifest/ownership；runner 只消费 locked strict inputs 和可验证
执行结果，不重做 path/manifest/Git 规则。后续 scaffold 可复用 publication 与 batch/state 框架，
但本分支不实现 scaffold 分支或 adapter。

## Surprises & Discoveries

- Observation: add source 临时文件位于 module source parent；普通隐藏前缀本身不会被 manifest
  内建规则排除，进程中断后可能被枚举成 desired。
  Evidence: `docs/03-manifest-spec.md` 的内建 `*.swp` ignore 与 `internal/manifest` 正常枚举路径。
  Impact: publication 临时名固定以 `.swp` 结尾，复用正常 manifest ignore，不增加第二套枚举例外。

- Observation: 既有 `TargetResolution.Equal` 只比较 leaf identity，不能证明准备期间实际经过的
  祖先拓扑未改变。
  Evidence: `internal/paths/resolution.go` 的 opaque ancestors 与 executor 现有 leaf 比较。
  Impact: paths 增加只读 `SameTopology` 比较，add target 提交前同时要求 leaf 与祖先序列一致；
  不改变持久化或公开 CLI。

- Observation: source 已 no-clobber 发布后，摘除临时名称本身也可能失败；只记录最终 source
  ownership 会让同 inode 的崩溃临时名称失去可验证 cleanup 路径。
  Evidence: publication temp-remove fault test 在 source publish 后返回错误。
  Impact: publication result 同时携带 source 与尚存 temp 的 inode/bytes/mode 证据；提交前 cleanup
  对二者分别重验，无法证明时保留并报错，target 提交后不再调用 source cleanup。

- Observation: 唯一随机 temp pathname 只能证明创建时排他，不能证明 cleanup 时该目录项仍由
  本轮拥有。
  Evidence: 自审 replacement regressions 分别在 source write failure 与 target rename failure
  前替换 temp path；仅按 pathname cleanup 会误删替代物。
  Impact: `b90dfcd` 为 source temp、target temp symlink/directory 与新建 source directories 记录
  inode/mode/link text 证据并在删除前重验；不匹配时保留并报告。

## Decision Log

- Decision: 保持 `add-preflight → add-link → add-scaffold` 严格串行，并在本分支先固定
  publication/result/state 成功前缀协议。
  Rationale: link 与 scaffold 共享 source/state/cleanup 安全不变量，复制实现会形成多处真相源。
  Date: 2026-07-22

- Decision: source no-clobber publication 对已经完整同步的独立临时副本使用 `link(2)`，随后摘除
  临时名称；不 hard-link 或 rename 原 target。
  Rationale: 该方式在 Darwin/Linux 都提供排他、不覆盖的单目录项发布，并保持 source inode 与
  target/hard-link sibling 独立；直接 rename 缺少可移植 no-clobber 语义。
  Date: 2026-07-22

## Outcomes and Handoff

实现已完成，计划保持 active 等待独立复核。`e35dd07` 建立 source 独立 inode、no-clobber、
文件/目录 sync 与等价 reuse；`8d9ed9a` 建立 target/source/ancestor/control 最终 Precond 和同父
目录 symlink 原子替换；`ca22590` 建立锁内 exact-input runner、可验证结果、成功前缀单次 state
提交、Store failure L2 收养与等价续跑；`b90dfcd` 以独立 fix commit 收紧全部临时产物 cleanup
ownership。

本机已通过 add/apply/paths 普通测试、5 次重复、race、定向 lint、完整
`669ea06c...HEAD` diff check，以及独立 `GOCACHE`/`GOLANGCI_LINT_CACHE`/BINARY 的 `make check`；
Darwin/Linux amd64 add test binary 交叉编译通过。真实 Linux 主机和远端 macOS/Linux CI 未运行，
远端待验收。尚缺未参与实现的完整 branch review，因此当前不迁移计划、不创建 closure commit，
也不声称 review-ready。
