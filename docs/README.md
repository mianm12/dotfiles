# dot 知识库

这里是 `dot` 的仓库内知识库。它按“我现在想完成什么”组织阅读路径，并通过链接连接教程、
概念、规范、架构和决策；不要求读者先理解目录分类。

## 从这里开始

| 你是谁 / 你要做什么 | 建议路径 |
| --- | --- |
| 第一次接触 `dot` | [安全跑通一次](getting-started.md) → [理解工作模型](concepts/mental-model.md) → [管理 modules](guides/manage-modules.md) |
| 正在使用或排查 `dot` | [故障排查](guides/troubleshooting.md) → [所有权与安全边界](concepts/ownership-and-safety.md) → [公共 CLI 规范](spec/cli.md) |
| 准备修改代码 | [开发者入口](development/README.md) → [产品规范索引](spec/README.md) → [架构概览](architecture/overview.md) |
| 让 Agent 参与开发 | [AGENTS.md](../AGENTS.md) → [共享上下文方式](development/README.md#人与-agent-共用同一套上下文) → 相关 spec owner 与测试 |
| 正在审查设计 | [产品定义](spec/product.md) → [架构概览](architecture/overview.md) → [ADR 索引](decisions/README.md) |
| 遇到陌生术语 | [术语表](glossary.md) |

如果你只想确认项目当前能做什么，先读仓库根目录的
[README](../README.md)。如果你准备提交变更，还需要阅读
[CONTRIBUTING](../CONTRIBUTING.md)；Agent 还必须遵守最近的
[AGENTS.md](../AGENTS.md)。

## 常用任务

| 任务 | 指南 |
| --- | --- |
| 启用、停用、新增或移除 module | [管理 modules 与 placements](guides/manage-modules.md) |
| 组织机器角色，处理 macOS/Linux 差异 | [管理 profiles 与平台差异](guides/profiles-and-platforms.md) |
| 转换 link/local，或把目录 link 拆成 leaf placements | [安全迁移 placements](guides/safe-migrations.md) |
| 定位 selection、target、state、platform 或恢复问题 | [故障排查](guides/troubleshooting.md) |

## 按问题查找

| 问题 | 去哪里 |
| --- | --- |
| 怎样在不碰真实 HOME 的情况下试用？ | [安全跑通一次](getting-started.md) |
| repository、selection、state 和文件系统如何共同工作？ | [工作模型](concepts/mental-model.md) |
| 为什么 `dot` 拒绝覆盖某个 target？ | [所有权与安全边界](concepts/ownership-and-safety.md) |
| 怎样启用或编写一个 module？ | [管理 modules](guides/manage-modules.md) |
| 怎样配置 profiles 或 platform variants？ | [Profiles 与平台差异](guides/profiles-and-platforms.md) |
| 为什么某些 placement 变更必须分两阶段？ | [安全迁移](guides/safe-migrations.md) |
| 某个 warning、problem 或失败应该怎样排查？ | [故障排查](guides/troubleshooting.md) |
| 某个命令或持久格式的精确定义是什么？ | [产品规范索引](spec/README.md) |
| 代码应该放在哪一层、由谁负责？ | [架构概览](architecture/overview.md) |
| 一个行为应该在哪一层测试？ | [测试架构](architecture/testing.md) |
| 为什么采用当前 convergence 模型？ | [ADR 索引](decisions/README.md) |
| 如何分支、验证和交付？ | [贡献约定](../CONTRIBUTING.md) |
| 怎样追溯已经移除的历史文档？ | [历史恢复指针](archive/README.md) |

## 信息架构与权威边界

知识库采用互相链接的页面，但不同页面承担不同职责：

| 页面类型 | 解决的问题 | 权威性 |
| --- | --- | --- |
| 根 README | 这是什么、现在能做什么、下一步去哪 | 当前能力摘要，不定义产品规则 |
| Getting started / 后续 guides | 怎样完成一个具体任务 | 教学与操作路径；精确行为链接到规范 |
| Concepts / glossary | 为什么这样工作、术语如何理解 | 解释层；不建立新契约 |
| `spec/**` | 产品必须怎样表现 | 唯一产品与行为契约 |
| `architecture/**` | 当前实现如何分层、依赖和测试 | 内部工程结构 |
| `decisions/**` | 为什么保留一个难以逆转的选择 | 决策理由，不复制规范 |
| `archive/**` | 如何恢复历史材料 | 无规范性 |

代码、测试与 CI 是“当前实现是否满足契约”的证据，不能代替规范。遇到冲突时，从
[规范索引的 owner](spec/README.md)定位唯一规则，不通过修改教程或 README 改变产品行为。

## 阅读与维护原则

- 从任务入口开始，通过上下文链接深入；不要求顺序通读整个 `docs/`。
- 一个事实只保留一个 owner，其他页面给出简短背景后链接过去。
- 教程使用真实可执行路径，并明确前置条件、预期结果、失败边界和下一步。
- 概念页解释稳定模型，不镜像命令参数、字段清单或实现细节。
- 页面标题和链接文字表达读者问题，避免只暴露内部文件名。
- 新页面必须能从本页或一个已有主题页到达，也必须链接回更权威的依据。
