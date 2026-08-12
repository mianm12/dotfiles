# 开发者入口

本页把“我要修改 `dot`”路由到正确的规范、代码和验证入口。它不保存阶段性计划，也不复制
产品契约或 Git 流程。

## 十五分钟建立上下文

按以下顺序阅读，通常不需要先通读整个仓库：

1. [根 README](../../README.md)：项目范围与当前已实现入口；
2. [工作模型](../concepts/mental-model.md)：repository、selection、state、filesystem 与 plan；
3. [产品规范索引](../spec/README.md)：找到本次行为的唯一规则 owner；
4. [架构概览](../architecture/overview.md)：确认 package 职责与依赖方向；
5. [测试架构](../architecture/testing.md)：确定回归测试和门禁归属；
6. [贡献约定](../../CONTRIBUTING.md)：风险分级、分支、验证与交付；
7. [AGENTS.md](../../AGENTS.md)：Agent 的仓库级执行边界。

只在需要理解难以逆转的历史取舍时阅读 [ADR](../decisions/README.md)；不要用 ADR 或 archive
替代当前规范。

## 先判断变更属于哪一层

| 你要改变什么 | 先看哪里 | 主要证据 |
| --- | --- | --- |
| 公开命令、输出类别或退出码 | [CLI 规范](../spec/cli.md) | `internal/cli` 测试与进程 smoke |
| repository、profile、machine selection | [selection 规范](../spec/selection.md) | `core/config`、`core/converge` 与 CLI 跨层测试 |
| module 发现、applicability、variants | [modules 规范](../spec/modules-and-platforms.md) | `core/config`、`core/converge` 与 CLI 跨层测试 |
| source、target 或路径关系 | [placements 规范](../spec/placements.md) | `core/paths` 与 safety/placement 测试 |
| ownership 或持久 state | [state 规范](../spec/state-and-ownership.md) | `core/state`、planning/execution 测试 |
| action eligibility 或冲突判定 | [planning 规范](../spec/planning.md) | converge planning 与 CLI analysis 测试 |
| mutation 顺序、锁、提交或恢复 | [mutation 规范](../spec/mutation-and-recovery.md) | converge execution/recovery 与 CLI 测试 |
| 内部 package 或依赖方向 | [架构概览](../architecture/overview.md) | 架构测试与 imports |
| 开发流程、CI 或交付方式 | [贡献约定](../../CONTRIBUTING.md) | Makefile、workflow 与 PR 证据 |
| 纯教学或导航问题 | [知识库首页](../README.md) | 链接检查、事实核对与人工可读性审查 |

改变公开行为、持久格式、ownership、清理语义或安全不变量时，先修改对应 spec owner，再修改
实现和测试。纯内部实现应由最简单的当前代码决定，不为了让文档看起来完整而预建抽象。

## 代码地图

| 路径 | 职责 |
| --- | --- |
| `cmd/dot` | 最小进程入口 |
| `internal/cli` | 命令参数、环境构造、公开输出与退出码 |
| `internal/core/config` | Repository、machine、module 配置加载与解析 |
| `internal/core/paths` | HOME target、source 与 control topology 边界 |
| `internal/core/state` | Ownership state 模型与编解码 |
| `internal/core/converge` | Selection、analysis、planning、lock、mutation 与 commit |
| `internal/storage` | 私有控制文件的原子发布原语 |

这只是导航摘要；允许依赖边和第三方依赖 owner 以
[架构概览](../architecture/overview.md)为准，并由架构测试机械约束。

## 开发循环

```text
定位规范 owner
  -> 构造隔离的失败/验收场景
  -> 做最小且完整的实现
  -> focused tests
  -> 自审规范、代码、测试与 diff
  -> 按风险等级运行完整门禁
  -> Draft PR 等待 CI 与人工审阅
```

命令的事实来源是仓库根 [Makefile](../../Makefile)。先运行：

```sh
make help
```

常规开发使用 focused `go test` 或 `make test`；Go、依赖、构建或 CI 相关变更发布前运行
`make check`。Fuzz、vulnerability、双平台 CI 及 Safety-critical 的额外证据要求见
[测试架构](../architecture/testing.md)和[贡献约定](../../CONTRIBUTING.md)。

任何 mutation 手动验证都必须使用绝对临时 HOME、repository、config、state 与 lock；仅修改
HOME 环境变量不能自动证明隔离完整。优先把行为转化为使用 `t.TempDir` 的合成测试。

## 修改文档时

1. 先确定页面类型：task、concept、spec、architecture、decision 还是 reference；
2. 检查是否已经存在同一事实的 owner；
3. 教程说明目标、前置条件、步骤、预期结果、失败边界和下一步；
4. 概念页解释模型与取舍，具体字段和条件链接到规范；
5. Spec 只写可验收的产品契约，不混入长教程、迁移操作或实现过程；
6. Architecture 描述当前代码结构，不用目标设计替代现状；
7. 检查所有新增页面可从[知识库首页](../README.md)到达，且链接回权威依据。

## 人与 Agent 共用同一套上下文

不建立只供 Agent 阅读的平行设计文档。一次开发任务需要的最小上下文包通常只有四部分：

1. **目标与验收**：要改变的用户故事、明确不做什么、怎样证明完成；
2. **规则 owner**：本页“先判断变更属于哪一层”定位到的具体 spec 标题；
3. **当前证据**：相关代码调用路径、最小合成测试和当前命令输出；
4. **执行边界**：最近的 `AGENTS.md`、风险等级、允许的 mutation 和 Git 授权。

人类和 Agent 都应从同一个 owner 继续追链接，只读取当前任务相关的页面，而不是把整个 `docs/`
当成长提示词一次性注入。这样可以减少陈旧摘要、互相矛盾的设计说明和“看起来合理但未经当前
实现验证”的补全。

对 Agent 来说，最有价值的上下文不是更多文字，而是明确的 owner、稳定术语、真实命令和可
验证边界。文档若与代码或测试冲突，不要静默选择更方便的一方：先判断是实现漂移还是契约确实
需要改变，再按对应层修复。
