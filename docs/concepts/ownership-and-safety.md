# 所有权与安全边界

`dot` 的第一优先级不是“尽量把配置部署成功”，而是“不把不确定的数据当作自己可以修改的
数据”。因此，一些看似保守的拒绝和 blocker issue 是产品语义，而不是需要绕过的障碍。

本页解释使用时的判断方式；精确 ownership、路径和 mutation 规则仍由
[产品规范](../spec/README.md)定义。

## 修改前的三个问题

对每个 target，`dot` 都需要回答：

1. **现在想要什么？** Repository desired 与当前 machine selection 决定 placement 是否 active。
2. **实际存在什么？** 文件系统观察区分缺失、匹配 link、普通文件/目录、未知或漂移 symlink。
3. **有什么所有权证据？** State 只能证明过去成功验证并记录过的事实，不能把任意现存路径变成
   `dot` 所有。

只要其中一个关键事实无法可靠确定，mutation 就应停止或保留用户数据。完整决策矩阵见
[planning 规范](../spec/planning.md)。

## Link 与 local 的权限不同

- **Link placement** 可以在 target 缺失时创建 symlink，也可以更新仍满足 ownership 前提的旧
  link。退出 desired 时，只有 ownership 与当前事实仍匹配的 link 才可能被清理。
- **Local placement** 只在 target 缺失时从 example 初始化私人文件。之后该文件由用户维护，
  不进入 ownership state，也不授予覆盖或删除权限。

Source、target、目录 link 和路径身份的精确定义见
[placements 规范](../spec/placements.md)，state 证据见
[state and ownership 规范](../spec/state-and-ownership.md)。

## 为什么已有 target 会阻塞

Manifest 表达期望，不等于删除授权。目标位置已经存在普通文件、目录或无法证明来源的 symlink
时，`dot` 不会自动：

- 覆盖它；
- 把它移动到备份位置；
- 导入到 repository；
- 将它登记为自己创建；
- 通过 force 或 fallback 忽略冲突。

正确做法是先用 `status` 或 `apply --dry-run` 看清 target 和原因，再由用户在仓库外决定保留、
迁移还是删除。`dot` 不替用户做不可逆的数据归属判断。

## 先分析，再 mutation

推荐的日常顺序是：

```sh
dot status
dot apply --dry-run
dot apply
dot status
```

`status` 与 dry-run 是无锁只读分析入口；`apply` 获取 mutation lock 后只形成一次权威计划，避免
把 best-effort 的锁前观察当作执行依据。公开输出和退出码见 [CLI 规范](../spec/cli.md)，内部调用路径见
[架构概览](../architecture/overview.md)。

分析中常见的词可以这样阅读：

- **action**：若计划可执行，需要发生的显式变化；
- **blocker issue**：阻止安全执行的结构化事实；
- **warning issue**：不阻塞执行，但需要用户理解的信息；
- **forget**：丢弃不足以继续授权 mutation 的旧证据，不等于删除用户数据；
- **prune**：仅在规则证明仍由 `dot` 拥有且未漂移时清理历史 link。

这些解释帮助阅读输出，不替代 [planning 规范](../spec/planning.md)中的完整 eligibility。

## 失败后的恢复方式

`dot` 不承诺跨多个 target 的事务或自动 rollback。Apply 可能完成部分 action 后因外部 I/O、
并发变化或 state 提交失败而返回错误。此时：

1. 不要假设“命令失败”等于“什么都没发生”；
2. 阅读 stderr 和已完成结果；
3. 再次运行 `dot status` 或 dry-run；
4. 处理明确 blocker issue；
5. 重跑完整 `dot apply`，不要手工模拟剩余内部步骤。

重复收敛会从新的真实文件系统事实重新计划。更精确的执行顺序和完成边界见
[mutation and recovery 规范](../spec/mutation-and-recovery.md)。

## 控制文件也属于安全边界

Machine config、state 和 lock 不是普通 managed targets。使用 `dot paths` 查看当前 invocation
实际使用的绝对路径；不要凭平台习惯猜测，也不要让 module target 与这些控制路径重叠。

排查或测试时，只替换 HOME 并不总能证明所有上下文都已隔离。仓库测试会显式隔离 HOME、
repository、config、state 和 lock；开发约束见[测试架构](../architecture/testing.md)和
[贡献约定](../../CONTRIBUTING.md)。

## 一条实用判断线

如果某个操作需要 `dot` 猜测“这个已有数据大概可以覆盖或删除”，当前正确结果通常就是拒绝。
先让 ownership 事实变得明确，再收敛；不要添加静默 fallback 来制造成功表象。

需要把原则应用到具体输出时，继续阅读[故障排查](../guides/troubleshooting.md)；需要转换 placement
类型或目录层级时，使用[安全迁移指南](../guides/safe-migrations.md)。
