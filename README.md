# dot

`dot` 是个人使用的 macOS/Linux dotfiles 管理 CLI。它以 symlink 集中管理共享配置，并从
`*.local.example` 一次性初始化不进入 Git 的本机内容。

## 当前状态

项目正在进行替换式重设计。[设计基线](docs/design-baseline.md)是后续实现唯一的产品与行为
契约。当前 Go 代码仍是待删除的旧实现，尚不能证明新基线已经完成，也不应按旧行为继续扩展。

开始实现前后的能力差异以代码和测试为准；目标设计不得被描述为已经可用。涉及 mutation 的
手动实验必须使用隔离的合成 HOME、repository、config、state 和 lock，不得作用于真实配置。

## 文档

- [当前设计基线](docs/design-baseline.md)
- [重构切换计划](docs/cutover-plan.md)（非规范性工程清单）
- [文档索引](docs/README.md)
- [历史存档](docs/archive/README.md)
- [贡献与 Git 约定](CONTRIBUTING.md)

重设计前的规范、实现说明和工程计划只保留用于追溯，不参与当前设计或实现裁决。
