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
- [x] 2026-07-21：Wave 4 实现与门禁完成；round 1 完整独立复核 BLOCK，发现 P1：sealed
  `Result.Valid` 未固定成功 link/publication 与计数的完整关系，仍可能把不完整 nil-error result
  投影为 exit 0；以及 P2：link 已提交而 state Store 失败时缺少规范要求的重跑 `apply` 恢复指引。
  `dd6bdc4` 与 `ca98d74` 分别修复后 round 2 完整复审 GO；closure `0ddb42a` 经 freshness
  与最终门禁 FF-only 合入 main。集成窄测与隔离 `make check` 通过，clean worker worktree 已
  无 force 移除。
- [x] Wave 4：`feat/add-cli` 独立计划、实现、复核、closure 和 main 集成。
- [x] 2026-07-21：首次三路完整 Checkpoint Acceptance 中，安全与工程主轴 GO；规范主线
  BLOCK，发现两个同根 P2：module 推断歧义未列出候选；显式 module 不在当前 profile 的
  手工指引不是包含既有成员/引用的确切 TOML 行。已从 clean main 创建
  `fix/m1-add-acceptance`，进入独立计划、修复与重新验收。
- [x] 2026-07-21：acceptance fix round 1 完整复核 BLOCK，发现两个 P2：合法 dotted profile
  名未作为 TOML key 引号，生成的行会改变 key 语义；已在 profile 展开中但因当前 GOOS inactive
  的 module 得到重复/no-op profile 行。进入新 fix commits 与 round 2 完整复审。
- [x] 2026-07-21：acceptance fix round 2 完整复核 BLOCK；round 1 的两个单独边界已闭合，
  仍有一个组合态 P2：module 同时不在 expanded profile 且当前 GOOS inactive 时，只输出 profile
  行，应用后仍不可用。进入最后一轮组合诊断 fix 与 round 3 完整复审。
- [x] 2026-07-21：acceptance fix round 3 完整复核仍 BLOCK：`InProfile=true`、`OSReady=true`、
  `TargetReady=false` 时 strict `Resolve` 在 add 诊断前返回 target mapping 错误，显式 module 得不到
  module-local target 恢复步骤。已命中“三轮 review 后仍有 blocking finding”停止条件；暂停在
  clean `main@0ddb42a` 与 clean fix branch `ba61b30`，不继续第四轮补丁或集成。
- [x] 2026-07-22：用户明确授权突破三轮 review 上限；Goal 恢复 active，三处 worktree/HEAD
  复核 clean。进入 strict `Resolve` 提前截断的第四轮 fix 与完整复审，授权不扩大其他范围。
- [x] 2026-07-22：`fef58ed` 仅对 strict Resolve 返回且精确匹配显式 requested module/GOOS 的
  target-mapping typed error 投影恢复指引；无 `-m`、其他 module 与其他 strict 错误保持原样。
  Round 4 完整复审 GO；closure `15cdfca` 经 freshness 与门禁 FF-only 合入 main，集成测试与
  `make check` 通过，clean acceptance worktree 已无 force 移除。
- [x] 2026-07-22：重新执行三路完整 Checkpoint Acceptance；规范主线 GO，安全与工程主线
  对同一根因 BLOCK：缺当前 GOOS target mapping 是 manifest resolve 硬错误，恢复指引可保留，
  但显式 `-m` 不得将 exit 1 降类为 conflict/3。复用已与 main 同步的 acceptance-fix branch，
  建立新的独立 active ExecPlan 修复并复核该退出码契约。
- [x] 2026-07-22：`e379b41` 保留 typed target-mapping manifest error 与恢复指引，但移除
  `ErrModuleActivation` 分类，使显式 `-m` 仍 exit 1；普通 ambiguity、OS/profile activation
  保持 exit 3。独立完整复审 GO；closure `d8fcf2a` 经 freshness/门禁 FF-only 合入 main，
  集成测试与 `make check` 通过，clean worktree 已无 force 移除。
- [x] 2026-07-22：再次三路完整 Checkpoint Acceptance；规范与安全主线 GO，工程主线发现
  一个 P2：早期 acceptance fix 把显式 module 非当前 profile/当前 GOOS inactive 从原 exit 1
  改成 exit 3，但规范只明确零/多候选推断为 3。建立新的独立 follow-up plan，恢复仅
  `ErrModuleAmbiguous` 映射 3，activation 保留指引但作为普通错误退出 1。
- [x] 2026-07-22：`90b423b` 恢复 CLI 仅把推断零/多候选的 `ErrModuleAmbiguous` 映射 3；
  显式 invalid/missing/nonmembership、OS/组合 activation、typed target-mapping 和其他 strict
  错误均 exit 1。独立工程完整复审 GO；closure `014052c` 经 freshness/门禁 FF-only 合入
  main，集成测试与 `make check` 通过，clean worktree 已无 force 移除。
- [x] 2026-07-22：终态三路完整 Acceptance 发现同一退出码矩阵的 P2：显式 `-m` 后零/多
  source candidate 仍复用 `ErrModuleAmbiguous`，导致 exit 3；但规范与 completed activation
  plan 均限定只有未显式选择的零/多推断为 3。建立独立 follow-up，使 explicit source mapping
  失败保留候选诊断但作为普通错误 exit 1。
- [x] 2026-07-22：`c72c3f4` 在 preflight 单一真相源只让 inference 分支 wrap
  `ErrModuleAmbiguous`；explicit selected zero/multiple 保留稳定候选诊断但 exit 1。规范与安全
  两路完整复审 GO；closure `711bda8` 经 freshness/门禁 FF-only 合入 main，集成测试与
  `make check` 通过，clean worktree 已无 force 移除。
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

- Observation: CLI 的 nil-error 完整性检查不能弥补 sealed `Result` 自身缺失的按 kind 计数不变量；
  state Store 失败的可信部分结果还必须携带公开恢复指引。
  Evidence: add-cli round 1 reviewer 可在 package 内构造全 succeeded/state committed 但零 publication/
  target commit 的 sealed result，并确认真实 link Store failure 输出没有提示重跑 `apply`。
  Impact: `Result.Valid` 必须成为物理事实计数的单一真相源；CLI 只基于可信 result 增加恢复提示，
  不复制下层 ownership 或收养判断。

- Observation: module selection 的安全拒绝成立不等于公开恢复诊断满足规范；数量与泛化文字不足以
  支持用户无猜测修复 manifest。
  Evidence: 首次 Checkpoint Acceptance 发现歧义错误只给候选数，非当前 profile 错误只写
  `add "m" to [profiles].p`，没有候选 module/source 或包含既有成员/引用的确切 TOML 行。
  Impact: acceptance fix 应从已解析的稳定 profile 声明生成只读诊断，继续不修改 manifest，也不在
  CLI 复制 manifest 语义。

- Observation: 可采用的 manifest 诊断必须同时保留 TOML key 语义与 OS activation 语义。
  Evidence: acceptance fix round 1 reviewer 证明合法 `work.mac` profile 不加引号会变为 dotted key；
  已由 profile 引入但当前 OS inactive 的 module 重复追加 profile 声明仍不会生效。
  Impact: manifest helper 必须正确引用 key，并区分 profile membership 与 effective OS activation，
  为 inactive module 给出真正改变有效性的最小手工指引。

- Observation: profile membership 与 OS/target readiness 是可同时失败的独立维度，恢复诊断不能用
  互斥分支只报告其一。
  Evidence: acceptance fix round 2 reviewer 构造现有 module 同时不在 expanded profile、受
  root/module OS defaults 过滤且可能缺当前 OS target mapping；仅采用 profile 行仍无法重试。
  Impact: add 必须组合 manifest helper 的全部可信恢复事实，按 profile、OS、target 顺序输出。

- Observation: strict profile `Resolve` 自身可能在 active module 缺当前 GOOS target mapping 时提前
  失败，使显式 module 的可信 activation facts 无法到达 add 诊断层。
  Evidence: round 3 reviewer 复现 profile 已含 module、OS 已 active、target table 只含另一 OS；
  `Resolve` 直接报错，三维组合诊断唯一剩余格不可达。
  Impact: 后续若获准继续，应让该可分类错误在不掩盖其他 effective module 严格错误的前提下投影
  requested-module 指引；本 Goal 当前按三轮上限停止，未实施该建议。

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

尚未收口。四个 Milestone 已按 DAG、独立 review 与 freshness 门禁 FF-only 合入 clean main；首次
Checkpoint Acceptance 的安全/工程主轴 GO，规范主线的 module 恢复诊断触发 acceptance fix。
acceptance fix 已闭合候选列表、确切 profile 行、dotted key、OS inactive 与八格组合诊断；最后
通过 strict Resolve typed error 精确匹配 requested module/GOOS，使缺当前 OS target mapping 的
唯一剩余格可达，而无 `-m`、其他 module 与其他 strict 错误仍 fail closed。Round 4 完整复审 GO，
`fix/m1-add-acceptance@15cdfca` 已 FF-only 合入 clean main 并通过集成门禁，worker worktree 已
移除。重新验收时安全与工程 reviewer 一致确认 typed target-mapping error 的恢复文字正确，但它
仍是 manifest hard error；显式 `-m` 当前错误地包装 `ErrModuleActivation` 并退出 3，违反
`1 > 3`。`e379b41` 已保留 typed hard error、恢复步骤与 exit 1，并由独立完整 review 确认
不影响普通 ambiguity/activation 的 exit 3；follow-up closure `d8fcf2a` 已 FF-only 合入 main
并通过集成门禁。下一步对最终 `checkpoint_base...main` 再做三路完整验收；真实 Linux 与远端
macOS/Linux CI 未运行。该轮工程 reviewer 进一步用 `0ddb42a` 历史与 CLI 规范确认：显式
profile/OS activation 原为 exit 1，acceptance fix 曾无意改成 3；Checkpoint 因此仍未通过，需以
独立 follow-up 恢复只有真正 module/source ambiguity 才映射 conflict/3。
`90b423b` 已完成该恢复，保持所有手工指引、零写入与隐私断言；工程 reviewer 完整复审 GO，
closure `014052c` 已 FF-only 合入 main 并通过集成门禁。下一步对最终
`checkpoint_base...main` 再次执行三路完整验收。
终态 reviewer 随后确认 `selectCandidate` 的 explicit 分支仍对零/多 source wrap
`ErrModuleAmbiguous`；这与上述“只有 inferred zero/multiple 为 3”的已定契约不一致。需在
preflight 单一真相源拆分 explicit mapping error identity 后再验收。
`c72c3f4` 已完成拆分，未改变 candidate 生成或 CLI 文本分类；规范与安全 reviewer 完整复审
均 GO，closure `711bda8` 已 FF-only 合入 main 并通过集成门禁。下一步对最终
`checkpoint_base...main` 重新执行三路完整 Acceptance。
