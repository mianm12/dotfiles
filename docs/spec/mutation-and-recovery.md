# Mutation、锁与恢复

## 执行顺序

```text
converge.Analyze read-only preflight
  -> strict load config/state 与全部 effective manifests
  -> resolve desired and observe actual
  -> validate control topology and supported path conflicts
  -> build Transitions, Actions, Problems and one immutable FinalState
  -> revalidate complete control topology and acquire mutation lock
  -> converge.Analyze again: strict reload, re-resolve, revalidate and replan
  -> reject machine semantic fingerprint drift
  -> create parents
  -> create missing locals and new links
  -> update owned links
  -> prune stale owned links
  -> re-read changed targets
  -> atomically commit state
  -> release mutation lock
```

## 安全规则

- 配置、manifest 与全量收敛集合的严格加载由
  [`cli.md`](cli.md#全量收敛与加载) 定义。
- 初次只读 preflight 的 deterministic config、control topology、私有 control root 及
  config/state/lock 目录项类型、不受支持的路径编码、path 或 ownership conflict 必须在获取
  lock 前失败，零文件系统写入：不得创建或 chmod config/state/lock root、lock、temporary
  file、selection、state、parent 或 target。
- [`selection.md`](selection.md#platform-与-module) 定义的任意 effective indeterminate，以及
  任意 extra not-applicable，都属于同一 preflight 失败边界：整次真实 mutation 在获取 lock 前
  失败，零写入。Profile not-applicable cleanup 是否存在只由
  [`planning.md`](planning.md#通用决策规则) 决定。
- 只有只读 preflight 成功后才能获取 lock；锁内必须通过同一 Analyze 实现重新加载、验证和规划，
  不执行保存的 preflight plan。锁内复核失败只可以留下 advisory-lock bookkeeping，不得写 target
  或 state。
- 一次真实 mutation 从获取到释放 lock 必须使用同一个固定 HOME、repository、config、state
  和 lock 路径；只获取一次 lock，也只释放一次，不建立嵌套 guard 或复用计数。获取 lock
  之前必须完成整组 control topology 与现存 control entry 的只读校验。
- 锁内复核发现 machine semantic fingerprint 漂移时失败，不切换到新 repository 或新 selection
  继续执行。
- Artifact convergence 使用固定 HOME 和 controls；只执行锁内 fresh analysis 生成的 Actions，
  随后复核 changed targets 并提交同一 Plan 已计算好的 FinalState。Executor 不从 Action 顺序
  增量推导或修补 ownership state。锁前 Report、Plan、resolved modules
  和 state 均不得进入执行。
- Lock 释放失败属于 partial mutation 失败；state commit 失败同样返回 partial。已经发布的
  selection、target 或 state 不回滚，具体重跑文案由 CLI 投影。
- [`placements.md`](placements.md#control-path-topology) 定义的私有 control root 路径边界
  必须在 lock 与 mutation 前完成校验；失败时不跟随、替换或 chmod 对应对象。
- 不防御同一用户权限的其他进程在检查与 mutation 之间并发替换私有控制根。
- 不建立通用 Action snapshot 或 precondition framework；Update/Prune Action 只携带删除前必须
  复核的 resolved target 与 raw destination 两项窄事实。
- Local 以 `0600`、内容完整且不可覆盖的方式发布；新 link 以不可覆盖的 symlink create
  发布；两者在 target 已出现时停止。
- Update/prune 删除 symlink 前重新读取 resolved target 和 raw destination；与 state 不同则停止。
- Update/prune 删除后重新读取 target；只有确认不存在才继续，重新出现或无法确认时停止且不推进
  state。
- 新 target 创建和 update 全部成功后才开始 prune；Prune 按
  [`planning.md`](planning.md#link) 生成的 Action 顺序执行。
- Changed target 重新读取符合预期后才允许提交 FinalState；不建设独立 postcondition framework。
- State 最后原子提交；state commit 和 lock release 均成功才构成 mutation 成功。公开成功或
  未完成结果的投影只由 [`cli.md`](cli.md#status-与-dry-run) 定义。Preflight、规划、mutation
  或 state commit 失败时不得单独推进 FinalState。
- 不提供 rollback。失败时保留已经完成的安全动作，报告可能部分应用并要求用户重跑。
- Mutation commands 使用同一把稳定 advisory file lock。Lock busy 作为普通运行时失败。

## 中断恢复

必须能通过重跑处理：

| 中断后事实 | 下一次 apply |
| --- | --- |
| link 已创建、state 未提交 | adopt |
| link 已更新、state 仍是旧 destination | repair state |
| update 删除旧 link 后中断 | create 当前 desired |
| prune 已完成、state 仍有记录 | forget stale state |
| local 已完整发布、state 未提交 | keep 并登记 |

Machine selection 由 `init` 与 `select` 命令在同一 advisory lock 下独立原子发布。它们不读取
state 或执行 artifact convergence；发布失败重跑原命令，发布成功后运行 `dot apply`。因此
selection publication 不属于 artifact mutation 的部分完成状态。
