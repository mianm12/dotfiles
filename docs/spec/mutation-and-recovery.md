# Mutation、锁与恢复

## 执行顺序

```text
read-only preflight
  -> strict load config/state 与 scope 内 manifests
  -> build prospective selection
  -> resolve desired and observe actual
  -> validate control topology and supported path conflicts
  -> build candidate plan
  -> revalidate complete control topology and acquire mutation lock
  -> strict reload, re-resolve, revalidate and replan
  -> persist changed selection
  -> create parents
  -> create missing locals and new links
  -> update owned links
  -> prune stale owned links
  -> re-read changed targets
  -> atomically commit state
  -> release mutation lock
```

## 安全规则

- 配置与 manifest 的严格加载及命令 scope 由
  [`cli.md`](cli.md#命令-scope-与加载) 定义。
- 初次只读 preflight 的 deterministic config、control topology、私有 control root 及
  config/state/lock 目录项类型、不受支持的路径编码、path 或 ownership conflict 必须在获取
  lock 前失败，零文件系统写入：不得创建或 chmod config/state/lock root、lock、temporary
  file、selection、state、parent 或 target。
- [`selection.md`](selection.md#platform-与-module) 定义的任意 prospective effective
  indeterminate，以及 init/apply 中 extra/explicit selection 的 not-applicable，都属于同一
  preflight 失败边界：整次真实 mutation 在获取 lock 前失败，零写入。Remove 仅由 extra 选中的
  module 是 selection 收缩：目标不再 prospective effective，因此其当前 not-applicable 或
  indeterminate 不阻断 remove；cleanup 只能依据既有 state ownership 规划。Profile
  not-applicable cleanup 是否存在只由 [`planning.md`](planning.md#通用决策规则) 决定。
- 只有只读 preflight 成功后才能获取 lock；锁内必须重新加载、验证和规划，不执行保存的
  preflight plan。锁内、首次发布 changed selection 之前的复核失败只可以留下 advisory-lock
  bookkeeping，不得写 selection、target 或 state；selection 已发布后的失败按下文中断恢复
  契约处理。
- 一次真实 mutation 从获取到释放 lock 必须使用同一个固定 HOME、repository、config、state
  和 lock 路径；只获取一次 lock，也只释放一次，不建立嵌套 guard 或复用计数。获取 lock
  之前必须完成整组 control topology 与现存 control entry 的只读校验。
- Selection 只能发布到该 mutation 固定的 config 路径，且 machine repository 必须与固定
  repository 相同。锁内复核发现 repository 漂移时失败，不切换到新 repository 继续执行。
- Artifact convergence 使用固定 HOME 和 controls，再次只读校验 controls、重新加载最新
  state、重新规划、执行、复核 changed targets 并提交 state；不得接收或执行锁前、锁内
  analysis 保存的 plan。
- Lock 释放失败属于 mutation 失败；已经发布的 selection、target 或 state 不回滚。
- [`placements.md`](placements.md#control-path-topology) 定义的私有 control root 路径边界
  必须在 lock 与 mutation 前完成校验；失败时不跟随、替换或 chmod 对应对象。
- 不防御同一用户权限的其他进程在检查与 mutation 之间并发替换私有控制根。
- 不建立通用 action snapshot 或 precondition 系统。
- Local 以 `0600`、内容完整且不可覆盖的方式发布；新 link 以不可覆盖的 symlink create
  发布；两者在 target 已出现时停止。
- Update/prune 删除 symlink 前重新读取 resolved target 和 raw destination；与 state 不同则停止。
- Update/prune 删除后重新读取 target；只有确认不存在才继续，重新出现或无法确认时停止且不推进
  state。
- 新 target 创建和 update 全部成功后才开始 prune；Prune 按
  [`planning.md`](planning.md#link) 生成的 action 顺序执行。
- Changed target 重新读取符合预期后才进入 state；不建设独立 postcondition framework。
- State 最后原子提交；state commit 和 lock release 均成功才构成 mutation 成功。公开成功或
  未完成结果的投影只由 [`cli.md`](cli.md#status-与-dry-run) 定义。加载时若发现兼容的非
  canonical 空 module，任意成功真实 mutation 都提交 canonical state，即使 selection、target
  和 ownership action 均为 no-op。Preflight、规划、mutation 或 state commit 失败时不得为
  canonicalization 单独写盘。
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
| selection 已更新、artifact 未完成 | 继续收敛 selection |
| local 已完整发布、state 未提交 | keep 并登记 |
