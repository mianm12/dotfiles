# Mutation、锁与恢复

## 执行顺序

真实 artifact convergence 使用 lock-first 的单次权威分析：

```text
normalize invocation paths and validate lock acquisition boundary
  -> ensure private state root and lock; acquire advisory lock
  -> strict load machine/repository/state 与全部 effective manifests
  -> resolve selection, complete control topology and desired
  -> observe actual and build one Plan{Actions, Issues[, NextState when executable]}
  -> blocker: return the complete blocked outcome
  -> execute semantic Actions in Plan order
  -> re-read changed targets
  -> atomically commit NextState
  -> release mutation lock
```

锁前不执行完整 analysis，不保存 preflight Plan，也不计算 machine fingerprint。锁内加载、解析、
观察和 planning 是真实 mutation 唯一的权威判断。

## Lock acquisition boundary

获取 lock 前只允许执行纯参数校验与安全取锁所需的只读路径检查：

- HOME、machine config、state 和 lock 必须是无 NUL、有效 UTF-8 的绝对路径并做词法规范化；
- machine config 必须是 config root 的直接子项；state 与 lock 必须是同一 state root 下两个不同的
  直接 sibling；
- config root、state root 及现存 config/state/lock entry 的 lexical、entry 与 resolved 身份必须
  足以证明创建 lock 不会跟随或覆盖异常对象；root 的最终对象不得是 symlink，现存 control file
  必须是直接 regular file；
- config family 与 state family 在此时可得到的身份中不得相等或互为祖先/后代。

该阶段不读取 machine/repository/manifest/state 内容，不解析 selection/platform，不观察 placement
target，也不建立 Plan。失败时不得创建、chmod 或修改 root、lock、config、state、parent、target
或 temporary file。

边界通过后可以确保私有 state root/lock 并获取 advisory lock。Machine 中的 repository、完整
control topology、state 内容、selection、platform、source、target set 与 ownership 均在锁内权威
analysis 中验证。由此发现的 blocker 可以留下已创建的私有 state root/lock bookkeeping，但不得
修改 machine config、state、placement parent、target 或 local temporary file。

## 安全规则

- 配置、manifest 与全量收敛集合的严格加载由
  [`cli.md`](cli.md#全量收敛与加载)定义。
- [`modules-and-platforms.md`](modules-and-platforms.md#applicability)定义的任意 effective
  indeterminate 和任意 extra not-applicable 都产生 blocker；profile not-applicable 的 stale cleanup
  由 [`planning.md`](planning.md#通用决策规则)定义。
- 一次 mutation 从 acquire 到 release 使用同一个固定 HOME、config、state 和 lock 路径，只获取
  和释放一次 lock，不建立嵌套 guard 或复用计数。Machine repository 在锁内加载后固定到本次
  analysis 与执行。
- Blocked Apply 是完整、未开始业务 mutation 的 outcome，不是运行时 error。它不得执行任何
  Action 或提交 NextState；公开输出与退出码由 [`cli.md`](cli.md#status-与-dry-run)定义。
- Executor 只顺序消费 Plan Actions，不按 Decision 重新扫描、分组或重排，也不从 Action 增量推导
  state。Parent preparation 是对应 create Action 的内部步骤，不是第二套 schedule。
- Action phase 必须满足：全部 create 成功后才进入 update；全部 create/update 成功后才进入
  prune。Adopt、repair-state 和 forget 不直接修改 target。
- Local 以 `0600`、内容完整且不可覆盖的方式发布；新 link 以不可覆盖的 symlink create 发布；
  target 已出现时停止。
- Update/prune 删除 symlink 前重新读取 resolved target 与 raw destination；与 Action 携带的旧
  ownership 不同则停止。删除后重新读取 target，只有确认不存在才继续。
- 每个 create/update 后重新读取 changed target 并验证预期类型、resolved identity 与 raw
  destination；全部 changed targets 通过后才允许提交 NextState。
- State 最后原子提交。State missing 时，即使没有 active links，也在成功 apply 中提交合法空 v4
  state；相同输入的后续 apply 不得重写。
- State commit 与 lock release 均成功才构成 mutation 成功。配置无法加载、lock/I/O、执行、commit
  或 release 失败走 typed error；failure 必须携带 stage、是否可能 partial、recovery 和可定位的
  Action（若存在），CLI 不通过字符串或 IssueCode 反推恢复方式。
- Failure stage 只使用 `input`、`lock`、`analysis`、`execute`、`state-commit`、
  `selection-commit` 与 `lock-release`。Blocker outcome 也必须成功释放 lock；release 失败会覆盖
  blocked/success outcome，返回 `lock-release` partial failure。
- 不提供 rollback。失败时保留已经完成的安全 Action；一旦 Action、state commit 或 selection
  publication 可能开始，错误必须标为 partial 并要求用户重跑完整命令。
- Mutation commands 使用同一把稳定、non-blocking advisory file lock。Lock busy 是普通运行时
  failure。产品不防御同一用户权限的其他进程在检查与 syscall 之间并发替换对象。
- 不建立通用 snapshot/precondition/postcondition framework。Update/Prune 只携带删除前必须复核的
  resolved target 与 raw destination 两项窄事实。

## Selection mutation

`init`、`select add` 与 `select remove` 使用同一 lock-first 边界：纯参数与安全取锁校验 → acquire
一次 advisory lock → 锁内一次加载/解析/决策 → 原子发布 machine config → release。它们不执行
锁前 repository/selection planning，不比较 fingerprint，不读取 state，也不观察或修改 target。

并发的 `dot` selection/artifact mutations 由同一 lock 串行化；后取得 lock 的操作读取最新 machine
config 并据此作出自己的完整决定。Selection publication 成功后仍由后续 `dot apply` 收敛 target，
不属于 artifact mutation 的部分完成状态。

## 中断恢复

必须能通过重跑处理：

| 中断后事实 | 下一次 apply |
| --- | --- |
| link 已创建、state 未提交 | adopt 后提交 ownership |
| link 已更新、state 仍是旧 destination | repair-state 后提交 ownership |
| update 删除旧 link 后中断 | create 当前 desired |
| prune 已完成、state 仍有记录 | forget stale ownership |
| local 已完整发布、空 v4 state 未提交 | local no-op，并提交当前 link-only NextState |

用户看到 partial failure 后必须重新运行完整 `dot apply`，不能手工模拟剩余内部步骤。重复收敛从
新的 repository、selection、state 与实际文件系统事实重新规划。
