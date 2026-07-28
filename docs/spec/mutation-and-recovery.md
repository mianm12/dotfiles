# Mutation、锁与恢复

## 执行顺序

```text
read-only preflight
  -> strict load config/state 与 scope 内 manifests
  -> build prospective selection
  -> resolve desired and observe actual
  -> validate control topology and supported path conflicts
  -> build candidate plan
  -> acquire mutation lock
  -> strict reload, re-resolve, revalidate and replan
  -> persist changed selection
  -> create parents
  -> create missing locals and new links
  -> update owned links
  -> prune stale owned links
  -> re-read changed targets
  -> atomically commit state
```

## 安全规则

- 配置与 manifest 的严格加载及命令 scope 由
  [`cli.md`](cli.md#命令-scope-与加载) 定义。
- 初次只读 preflight 的 deterministic config、control topology、私有 control root/lock
  目录项类型、path 或 ownership conflict 必须在获取 lock 前失败，零文件系统写入：不得创建
  或 chmod config/state/lock root、lock、temporary file、selection、state、parent 或 target。
- 只有只读 preflight 成功后才能获取 lock；锁内必须重新加载、验证和规划，不执行保存的
  preflight plan。锁内、首次发布 changed selection 之前的复核失败只可以留下 advisory-lock
  bookkeeping，不得写 selection、target 或 state；selection 已发布后的失败按下文中断恢复
  契约处理。
- 机器配置与 state/lock 的私有根目录最终对象必须是真实目录；更高层的 ancestor symlink
  合法。最终对象为 symlink 时 mutation 失败，不跟随、替换或 chmod 其目标。
- 不防御同一用户权限的其他进程在检查与 mutation 之间并发替换私有控制根。
- 不建立通用 action snapshot 或 precondition 系统。
- Local 和新 link 使用不可覆盖创建语义；target 已出现时停止。
- Update/prune 删除 symlink 前重新读取 resolved target 和 raw destination；与 state 不同则停止。
- 新 target 创建和 update 全部成功后才开始 prune。
- Changed target 重新读取符合预期后才进入 state；不建设独立 postcondition framework。
- State 最后原子提交；提交失败时命令失败。
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
