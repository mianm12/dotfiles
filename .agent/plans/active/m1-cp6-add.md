# chore/m1-cp6-orchestration：交付 M1 安全 add

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。本 Checkpoint 依用户明确选择，以一条 coordinator Goal 编排多个独立
Milestone branch；这是对默认一 Goal/一 branch 组织方式的有意例外。

## Purpose / Big Picture

完成后，M1 用户可以用 `dot add` 把 HOME 中的普通文件安全收编为 link 或 scaffold。命令在
任何 source、target 或 state 写入前对全部输入完成同源 manifest prospective 校验、完整 profile
路径校验和系统 Git 可跟踪性检查；执行时遵守 source-first、提交时 Precond、部分成功、等价续跑
和原子 state 规则。`--dry-run` 保持无锁零写入，M1 `--template` 明确硬错误。

## Scope / Non-goals

范围内：

- 普通文件输入、保守 module inference、`-m`、当前 profile 与完整 profile 碰撞校验。
- prospective desired 与正常严格 manifest/枚举同源；manifest ignore、Git ignore/local/global
  exclude、`*.local`、控制面路径和 source 变体均在全批次写入前检查。
- link 的 source publication、原子 target 替换、hard-link 隔离、Precond、state 与恢复。
- scaffold 的渲染字节/mode 一致性、source publication、target 零修改、state 提交点与恢复。
- Cobra `add`、确定输出/退出码、dry-run、M1 `--template` 拒绝和 README 当前实现事实。

明确不做：

- 不创建 `modules/`、不创建 module、不修改 manifest、不执行 `git add` 或 `git commit`。
- 不实现 managed/rendered、`--template`、目录递归、特殊文件、M2/M3 或新 state 格式。
- 不引入 go-git、自写 Git ignore matcher、通用 filesystem transaction 或新依赖。
- 不读取或修改真实 modules、机器配置、state、backup、`.env` 或主力 HOME。

## Contract and Context

- `docs/02-architecture.md` §2–§6：control plane、mutation lock、pipeline、路径身份与 state 原子提交。
- `docs/03-manifest-spec.md` §2–§6/§8：严格 manifest、profile、files、ignore、路径和 prospective add 例外。
- `docs/04-cli-spec.md` §2–§4.5/§5：退出码、`add` flags、全输入预检、提交点与输出。
- `docs/05-apply-engine.md` §1–§7/§9–§10：Precond、原子性、反向映射、恢复与幂等。
- `docs/06-templates.md`：M1 scaffold 与 `*.local`；managed/template 留给 M2。
- `docs/08-testing.md`：隔离、Git ignore/exclude、提交点、hard-link、dry-run 与双平台证据。
- `docs/09-roadmap.md` §1/§3：M1 add 的范围和反馈门槛。

Checkpoint base 是本地 clean `main@5d176497a75c9f8e43b413d43f04f3ea41720c51`；
`origin/main@e9e8bac6e5c1406e0db8aeb6e9eca6194aeeddb2`，本地 ahead 144、behind 0；唯一
remote 为 `origin`，main 的 upstream 是 `origin/main`，不存在独立 `upstream` remote。本 Goal
不 fetch/pull。CP5 coordinator closure 是 checkpoint base，相关 coordinator 与 acceptance
ExecPlans 已进入 completed；Plan Gate 时 CP6 目标 branches 均不存在，仅有 main worktree。

现有 runtime 已提供 `BeginMutation`、`LoadReadOnly`、strict `LoadedInputs` 和单次
`LoadedMutation.CommitState`；manifest 已提供严格 Resolve、完整 profile path boundary 与渲染；
state 已提供 `TransitionEntries`；apply/executor 已固定 link/scaffold 的 ownership、Precond、原子
提交与收养语义。真实缺口是 prospective source overlay、add module inference/Git preflight、
add 专用 source publication/runner，以及公开 CLI。apply executor 不能直接承担“先发布 source、
再替换原 target”的反向 add 协议，但其故障分类和同目录原子提交模式可复用。

## Progress

- [x] 2026-07-21：确认 main clean、CP5 已完成、CP6 branches 不存在；记录 checkpoint/main/
  origin/upstream/worktree 状态，未 fetch/pull。
- [x] 2026-07-21：三名只读 subagent 完成规范缺口、DAG/共享契约、测试/依赖/双平台检查；
  无停止条件，不需新依赖。
- [x] 2026-07-21：基线 `make check` 使用 `/private/tmp` 独立 cache/binary 在 Darwin/arm64 通过。
- [x] 2026-07-21：创建 coordinator branch/worktree 与本 active ExecPlan。
- [x] 2026-07-21：Wave 1 实现以 `8d22f24`、`11c72dc` 完成并通过 worker 门禁；round 1
  完整独立复核发现两个 P1（批内 source variant/文件系统别名未整体保留、Git 继承 `GIT_*`
  可偏离 effective repo/config）和一个 P2（BatchPlan 可伪造/修改）。`14d9329`、`339fb10`、
  `8d1fbf8` 分别修复后，round 2 完整复审 GO；closure `669ea06` 经 freshness 与最终门禁后
  FF-only 合入 main。集成窄测与隔离 `make check` 通过，clean worker worktree 已无 force 移除。
- [x] Wave 1：`feat/add-preflight` 独立计划、实现、复核、closure 和 main 集成。
- [x] 2026-07-21：Wave 2 以 source publication、target atomic commit、locked runner 三个行为
  commits 和 cleanup ownership 自审 fix 完成实现；round 1 完整复核发现两个 P2：发布前 source
  temp 清理缺少 bytes/mode 证据，协议违规 executor result 仍被包装为 valid failed outcome。
  `97ce057` 与 `65c3da2` 修复后 round 2 完整复审 GO；closure `cff70b6` 经 freshness 和最终门禁
  FF-only 合入 main。集成窄测与隔离 `make check` 通过，clean worker worktree 已无 force 移除。
- [x] Wave 2：`feat/add-link` 独立计划、实现、复核、closure 和 main 集成。
- [x] 2026-07-21：Wave 3 实现与门禁完成；round 1 完整独立复核 BLOCK，发现一个 P2：
  scaffold executor 在 state 提交前返回 `stateReady + error` 时，runner 仍可能提交 state 并投影
  成功。`4f8f9d6` 按 kind 收紧协议并补回归，round 2 完整复审 GO；closure `9206981` 经
  freshness 与最终门禁 FF-only 合入 main。集成窄测与隔离 `make check` 通过，clean worker
  worktree 已无 force 移除。
- [x] Wave 3：`feat/add-scaffold` 独立计划、实现、复核、closure 和 main 集成。
- [ ] Wave 4：`feat/add-cli` 独立计划、实现、复核、closure 和 main 集成。
- [ ] 三路完整 Checkpoint Acceptance、必要 acceptance fix、coordinator closure 与 main 集成。

## Milestone DAG and Scheduling

```text
feat/add-preflight → feat/add-link → feat/add-scaffold → feat/add-cli
                                                    → Checkpoint Acceptance
```

四个节点严格串行，每个节点在所有依赖 closure、FF-only 合入 main 且集成门禁通过后，才从当时
clean main 创建。`add-preflight` 固定 prospective desired、Git 与全批次 gate；`add-link` 首次
建立 publication、snapshot、成功前缀与 state 协议；`add-scaffold` 消费同一 publication/state
协议；`add-cli` 最后消费完整 runner result/error。link/scaffold 会共同修改 `internal/add` 的
publication、runner、cleanup、state 和故障接缝，不通过复制 adapter 强行并行。

每个 Milestone 使用独立 `/private/tmp` worktree、branch、active ExecPlan、测试先行语义 commits、
窄测、完整 diff check、隔离 cache 的 `make check` 与未参与实现的只读 reviewer。finding 使用新
fix commit，不 amend/rebase/cherry-pick/squash；每个有效基线最多三轮完整 review。

## Milestone Contracts

### `feat/add-preflight`

建立只读、自包含 add batch plan。输入先规范化为 HOME 下普通文件快照；拒绝已有 state、
`*.local`、控制面重叠、重复/碰撞、非法/不存在/非当前 profile module。module inference 只在当前
profile 的 target 与可信 state 证据中保守唯一选择。manifest prospective overlay 只能视本批精确
候选 source 为普通文件，复用正常定级、ignore、显式 `[files]`、suffix、render 与完整 profile
boundary；无关悬空引用仍失败。系统 Git 区分 tracked 与 untracked；untracked 必须不被 repository、
local exclude 或 user global exclude 忽略，Git 不可用或异常 fail closed。source 三种变体整体
no-clobber，唯一续跑例外是期望变体为完全等价普通文件且其他变体不存在。全部输入通过前不获取
mutation lock，也不写 source/target/state/temp。

### `feat/add-link`

在同一 mutation lock 与 strict loaded inputs 上重建 exact-input plan，整体 gate 通过后逐项执行。
source 在其父目录准备完整独立 inode，保存字节与九位 mode、完成 close/sync 后 no-clobber 发布；
不得 rename 或 hard-link 原 target。target 提交前重验普通文件 bytes/mode/identity、祖先拓扑、
control boundary 与 source；在 target 父目录准备 symlink 后原子替换。提交前失败保留 target，只清理
仍可证明本轮创建且未变化的 source；target 提交后 source/link 永不回滚。成功前缀形成 symlink
EntryUpdate 并单次原子提交 state；Store 失败保留 source/link，允许 apply L2 收养。

### `feat/add-scaffold`

复用 link 已定稿的 batch runner、publication、result 与 state success-prefix 协议。`.template`
source 的 prospective render bytes 和 desired mode 必须等于输入快照；执行只发布 source、不修改
target，并在 state 提交前重验 target bytes/mode/identity/祖先。state upsert 是 scaffold 提交点；
其后 source 不得因后续项失败清理，Store 失败允许 apply S1b 收养。等价遗留 source 继续适用 Git
trackability。M1 `--template` 仍在任何 lock/source/state 写入前硬拒绝。

### `feat/add-cli`

注册公开 `add`，支持 `-m`、互斥 `--template|--scaffold`、`--dry-run` 和至少一个 path。CLI 只
投影内部 plan/result/error，不能重做 manifest、Git、Precond 或 state 推断；zero/invalid result +
nil error 必须 fail closed。输出稳定展示 context、动作与成功后的手工 Git 提示，退出码遵守
1 > 3 > 2 > 0；推断零/多候选为 3，运行错误为 1，成功为 0。dry-run 走只读 preflight，无锁且
全新 HOME 零写入。同步 README 当前能力，并以全隔离 E2E 证明 add 后 apply 正常收敛。

## Validation and Acceptance

| 必须成立的性质 | 主要证据 | 状态 |
|---|---|---|
| 输入类型、module inference、`-m`、profile、完整碰撞 | add/manifest/path 单元与真实 FS 测试 | 待验证 |
| manifest/Git ignore、local/global exclude、Git unavailable | 隔离真实 Git 集成与 injected runner | 待验证 |
| 全输入预检、dry-run、M1 template 零写入 | runtime/add/CLI 文件系统测试 | 待验证 |
| source variants、publication、等价续跑与 no-clobber | add publication 故障注入 | 待验证 |
| link 提交点两侧、state failure、hard-link 隔离 | add runner + apply recovery | 待验证 |
| scaffold render/mode、target snapshot、state 提交点 | add runner + apply recovery | 待验证 |
| 多输入部分成功与已提交项不回滚 | add runner/CLI 集成 | 待验证 |
| add 后 apply 收敛并立即重跑无 mutation/adopt | 全隔离 E2E | 待验证 |
| macOS/Linux | 本地 Darwin + 交叉编译 + 远端 CI | 待验证 |

每个节点运行相关窄测、`git diff <effective-base>...HEAD --check` 和使用唯一 `/private/tmp`
cache/BINARY 的 `make check`。Checkpoint 最终至少运行：

    git diff 5d176497a75c9f8e43b413d43f04f3ea41720c51...main --check
    make check BINARY=/private/tmp/dot-m1-cp6-final-acceptance/dot

所有 mutation 测试使用 `t.TempDir()` 或 `/private/tmp` 的合成 HOME/repo/config/state/backup，清除
或重定向 `DOT_CONFIG`、`DOT_REPO`、HOME/XDG 和 Git global/system config，并断言真实 HOME 未
变化。远端 CI 未实际运行时只写“本地验收通过、远端待验收”。

## Safety, Authorization, and Recovery

用户当前 Goal 明确授权本 Checkpoint 的 coordinator/Milestone/integration-fix/acceptance-fix
branches、`/private/tmp` worktrees、范围内修改/stage/commit/计划迁移、freshness merge 和本地
FF-only main 集成；本计划不延续该授权。禁止 fetch/pull/push/rebase/cherry-pick/squash/amend/
reset/force、branch 删除和真实私人数据访问。

失败保留最近成功 commit。仅对本 Goal 创建、已集成且 clean 的 worktree 使用无 `--force`
移除。main 出现 DAG 外提交、semantic conflict、无法证明 mutation/Precond/recovery/零写入、
三轮 review 后仍 blocking，或需要改变公开/持久化/ownership 契约时，更新本计划并停止。

## Interfaces and Dependencies

不新增依赖。Git 可跟踪性由标准库 `os/exec` 驱动系统 Git，并以窄 runner seam 注入故障；真实 Git
集成覆盖 repository `.gitignore`、`.git/info/exclude` 与用户 `core.excludesFile`。候选 source
必须用 repo-relative path 和 `--` 终止选项；tracked 与 ignored 的退出语义显式分类，不能在 Git
错误时 fallback。测试及隐藏 `--home` 必须把 effective HOME/XDG 与 Git global/system config
隔离，不能读取开发机配置。

跨组件共享 contract 是 prospective `ValidatedProfile`/desired、不可变 add batch plan、source
publication/equivalent-source 结果、per-item outcome、成功 state effect 与单次 CommitState。
ownership、Precond、路径和恢复继续由 manifest/paths/add/runtime/state 各自唯一职责表达。

## Surprises & Discoveries

- Observation: 现有 manifest 严格枚举要求 `[files]` source 已实际存在，没有 prospective overlay。
  Evidence: `internal/manifest/source.go` 的枚举与 source reference 校验。
  Impact: preflight 必须在 manifest 内建立只豁免本批精确 source 的 overlay，不能复制枚举规则或
  先写临时 modules。

- Observation: apply link/scaffold executor 的方向与 add 相反，不能直接承担 add target 提交。
  Evidence: apply executor 消费既有 source 创建 target；add 必须先发布新 source，再替换原 target。
  Impact: 新增 add 专用 runner/publisher，但复用既有 path/state/runtime 不变量与故障分类模式。

- Observation: repo 与 HOME 可位于不同文件系统，且输入可能与另一 target 共享 hard-link inode。
  Evidence: 规范只承诺字节和九位 mode，并要求 sibling target 不受 mutation 连带影响。
  Impact: source 与 target 临时项分别在各自父目录发布；source 必须复制成独立 inode。

- Observation: 精确 source 字符串去重不足以表达 add 的全批次 publication 保留集合。
  Evidence: round 1 reviewer 构造同批 `foo` 与显式 link `foo.template`；两者尚不存在时原预检均可
  通过，只有执行发布后才产生冲突。继承的 `GIT_*` 也可让系统 Git 查询错误 index/config。
  Impact: preflight fix 必须在 Git 前按 repository 文件系统身份保留整个 suffix variant family，
  并清除会覆盖 effective repo、index、pathspec 与配置来源的 Git 环境变量。

- Observation: 下游执行不能把公开可构造/可修改的 add plan 当作已验证 capability。
  Evidence: round 1 reviewer 可直接构造或修改 `BatchPlan.Items`、source、kind 与 snapshot bytes。
  Impact: preflight 必须返回带 validity seal 的不可变计划，并通过深复制 accessor 暴露只读数据。

- Observation: 临时 pathname 未被替换不等于其内容和 mode 仍由本轮拥有；invalid executor result
  也不能证明 target 未提交。
  Evidence: add-link round 1 reviewer 对 publication cleanup 与 runner result protocol 的完整审查。
  Impact: 每个 cleanup 阶段必须携带当时可证明的 inode/bytes/mode 证据；协议违规必须返回无效
  result，不能把未知物理事实投影为 `OutcomeFailed`。

- Observation: link 的 `stateReady + error` 可表示 target 已提交后的 cleanup error，但 scaffold 的
  提交点是 state Store，同一结果组合在 Store 前没有合法语义。
  Evidence: add-scaffold round 1 reviewer 发现共享 validator 会把该 scaffold 结果加入 state update，
  随后 Store 并标记成功。
  Impact: runner protocol 必须按 kind 区分该组合；scaffold 应 fail closed、不得 Store，且保留无法
  信任所有权的 source 供后续等价续跑。

## Decision Log

- Decision: 四个 Milestone 严格串行，不启用并行 Wave。
  Rationale: link/scaffold 在 prospective contract 之外仍共享 publication、snapshot、state、cleanup
  与故障接缝；并行会产生同一安全不变量的多处真相源。
  Date: 2026-07-21

- Decision: 不引入新依赖，Git 可跟踪性调用系统 Git。
  Rationale: 规范明确要求完整 Git ignore 语义；系统 Git 已是运行时前置，标准库 adapter 足够且
  避免 go-git 或自写 matcher 漂移。
  Date: 2026-07-21

## Outcomes and Handoff

尚未收口。Plan Gate 已通过；coordinator active plan 建立后进入 `feat/add-preflight`。
