# 跨层验收

以下编号是稳定的跨层产品验收标识：

- **AC-01**：空白 macOS/Linux 机器按 profiles init，第二次 apply no-op。
- **AC-02**：Init 遇到已有普通 link target 时 preflight 零 mutation。
- **AC-03**：Profile module 无匹配 variant 时 skip；显式 apply 时失败且不加入 extras。
- **AC-04**：Link source 只改变内容时 symlink 和 state no-op。
- **AC-05**：Placement 新增时 create，删除时只 prune 有 state 证据且未漂移的 link；已漂移的
  stale target 警告并 forget state，不阻塞其余收敛。
- **AC-06**：Link target 改变时先建立新 target，再 prune 旧 target。
- **AC-07**：`apply <module>` 激活 extra module，重复 apply no-op。
- **AC-08**：`remove <module>` 取消 extra、删除 owned links、保留 locals；profile module
  remove 被拒绝。
- **AC-09**：Local 只在 absent 时创建；任何已有目录项都 keep；example 更新不覆盖。
- **AC-10**：正确未知 symlink adopt；state-owned symlink 被改指后 conflict；placement 同 ID
  改变 kind 后 conflict。
- **AC-11**：Parent symlink 改变 resolved target 后 update/prune 被拒绝。
- **AC-12**：精确 target、解析后 target 冲突，或 target 与 control path 任一方向重叠时，在
  preflight 阶段零 mutation。
- **AC-13**：Selection、local create、link create/update、prune 和 state commit 中断后可重跑
  收敛。
- **AC-14**：State missing 可以警告并继续；state corrupt、v1 或 too-new 时拒绝 mutation。
- **AC-15**：第二个 mutation process 失败；status/dry-run 不创建 lock，且 dry-run 严格零写入。
- **AC-16**：Active profile 引用已删除 module 时 mutation 前失败、零写入；extra/state 中的
  已删除 module 允许 `remove` 清理。
- **AC-17**：Scope 外 module 的 manifest 损坏不阻塞 scoped `apply`/`remove`；仅显式操作该坏
  module 时失败。
- **AC-18**：Link source 或 local example 缺失/类型不符时配置错误、零 mutation。
- **AC-19**：未知 distro 下 portable 模块适用、distro-gated variant 为 not-applicable；`os`
  出现枚举外值为配置错误。

所有成功 mutation 场景都追加一次相同 apply，并断言不再发生文件系统 mutation。

测试使用绝对路径的合成 HOME、repository、config、state 和 lock，不读取或写入真实私人
配置。测试所有权和分层规则见 [`../architecture/testing.md`](../architecture/testing.md)。
