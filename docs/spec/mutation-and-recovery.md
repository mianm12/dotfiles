# Mutation、锁与恢复

## 执行顺序

```text
acquire mutation lock
  -> load config/state 与 scope 内 manifests（strict）
  -> build prospective selection
  -> resolve desired and observe actual
  -> validate supported path conflicts
  -> build plan
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
- Deterministic config、path 或 ownership conflict 在 mutation 前失败，选定 scope 零写入。
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
