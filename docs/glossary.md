# 术语表

本页用于快速消歧，不拥有产品规则。定义刻意保持简短；需要精确字段、条件或失败行为时，点击
“规则 owner”进入对应规范。

| 术语 | 在本项目中的含义 | 规则 owner |
| --- | --- | --- |
| repository | 包含 `dot.toml`、`modules/`、manifest 与 source 的期望配置仓库 | [selection](spec/selection.md)、[modules](spec/modules-and-platforms.md) |
| desired | Repository 与当前 machine selection 共同表达的目标状态 | [product](spec/product.md)、[selection](spec/selection.md)、[modules](spec/modules-and-platforms.md) |
| machine config | 当前机器绑定的 repository、profiles 与直接 module selection | [selection](spec/selection.md) |
| profile | Repository 中命名的一组 module 选择；不是继承或覆盖系统 | [selection](spec/selection.md) |
| module | 一个可选择、可按平台匹配并包含 placements 的配置单元 | [modules](spec/modules-and-platforms.md) |
| effective module | 由 active profile 或直接 selection 选中并进入当前平台解析的 module | [selection](spec/selection.md)、[modules](spec/modules-and-platforms.md) |
| applicability | 根据 OS、distro、arch 证据得到的 applicable、not-applicable 或 indeterminate 判断 | [modules](spec/modules-and-platforms.md) |
| portable module | 所有适用平台共用一个 manifest 和 source 目录的 module | [modules](spec/modules-and-platforms.md) |
| variant | 同一 module 针对特定平台解析出的 manifest/source 版本 | [modules](spec/modules-and-platforms.md) |
| placement | Module 中一个有稳定 ID 的目标放置声明，种类为 link 或 local | [placements](spec/placements.md) |
| link | 将 HOME 内 target 指向 repository source 的 symlink placement | [placements](spec/placements.md) |
| local | 只在 target 缺失时从 example 初始化、之后由用户维护的私人文件 placement | [placements](spec/placements.md) |
| target | HOME 内由 placement 指向的目标路径 | [placements](spec/placements.md) |
| source / example | Repository 内 link 的共享来源，或 local 首次创建时使用的样例 | [placements](spec/placements.md) |
| state | 保存过去成功验证的 link ownership 证据的私有账本 | [state and ownership](spec/state-and-ownership.md) |
| ownership | 允许 `dot` 更新或清理某个 link 所需的可验证证据，不是对任意路径的声明 | [state and ownership](spec/state-and-ownership.md) |
| actual filesystem | 观察时对词法 target 做 `lstat` 得到的叶子事实 | [planning](spec/planning.md) |
| analysis | 从 repository、selection、state 与文件系统跑同一条循环的过程 | [CLI](spec/cli.md)、[planning](spec/planning.md) |
| convergence | 让全部 effective modules 与历史 stale ownership 回到一致状态的完整过程 | [CLI](spec/cli.md) |
| 行 | 循环产出的一条用户可见结果：`link` / `file` / `replace` / `remove` / `record` / `forget` / `skip` | [planning](spec/planning.md)、[CLI](spec/cli.md) |
| skip | 不会碰该 target；出现任一 `skip` 时整批不写 | [planning](spec/planning.md) |
| dry-run | 与 status 共用同一条循环、展示将要发生的行而不执行 mutation 的 apply 模式 | [CLI](spec/cli.md) |
| mutation | 对 machine config、target、state 或 lock bookkeeping 产生写入的操作 | [mutation and recovery](spec/mutation-and-recovery.md) |
| control paths | 当前 invocation 的 machine config、state 与 lock 等私有控制路径 | [CLI](spec/cli.md)、[placements](spec/placements.md) |
| stale | State 中仍有历史记录、但当前已不再 desired 的 placement | [planning](spec/planning.md) |
| remove | dest 仍匹配时删除 stale link | [planning](spec/planning.md) |
| forget | 放弃旧 link ownership 证据而不删除不能安全修改的数据 | [planning](spec/planning.md) |
| no-op | 输入与已收敛结果不变时，重复 apply 不产生新 mutation | [product](spec/product.md)、[mutation and recovery](spec/mutation-and-recovery.md) |
| fail closed | 关键配置、路径或 ownership 无法确定时拒绝 mutation，而非猜测后继续 | [对应产品规范索引](spec/README.md) |
| ADR | 记录难以逆转且未来仍需理解的工程决策理由，不承担产品规范职责 | [ADR 索引](decisions/README.md) |

## 用词约定

- 文档用 **selection** 表示机器选择，用 **desired** 表示解析 selection 后的当前期望，不混写为
  “已安装”。
- **link** 指 placement 类型时使用英文；“链接”用于 Markdown 导航时不表示 symlink。
- **state** 只指 `dot` 的持久 ownership 账本；命令的临时执行状态用“阶段”或“结果”。
- **规则 owner** 表示唯一具有规范性的定义位置，其他页面只解释并链接。
