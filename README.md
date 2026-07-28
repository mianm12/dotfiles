# dot

`dot` 是个人使用的 macOS/Linux dotfiles 管理 CLI。它以 symlink 集中管理共享配置，并从
`*.local.example` 一次性初始化不进入 Git 的本机内容。

## 当前能力

当前 Go 实现支持 macOS/Linux、profiles、portable 或 platform variants modules、link/local
placements、dry-run、ownership state 和单进程 mutation lock。

公开命令为 `init`、`status`、`apply`、`remove`、`paths`、`version` 和 `help`。完整行为和
安全边界以[产品规范集合](docs/spec/README.md)为准，当前实现证据以代码、测试和 CI 为准。

## 快速开始

```sh
make build
bin/dot init /absolute/path/to/dotfiles --profile base
bin/dot status
```

需要调整 profiles、修正 repository 绑定或定位旧 state 时，先查看当前 binary 使用的本机
文件位置：

```sh
bin/dot paths
```

该命令只显示路径，不读取或创建这些文件。

当前仓库提供跨 macOS/Linux 的 `starship` module。它默认不在空 `base` profile 中，可按机器
单独启用：

```sh
bin/dot apply starship --dry-run
bin/dot apply starship
```

`apply` 不会覆盖已有普通文件、目录或未知 symlink；启用 module 前应先人工检查并迁移冲突
target。`dot` 不负责安装软件，也不提供自动导入、backup 或 rollback。

## 开发验证

```sh
make test
make check
make fuzz
make vuln
```

`make test` 快速运行全部 Go 测试；`make check` 执行依赖整洁度、格式、静态分析和全量 race
tests，CI 在 macOS 与 Linux 上运行同一入口。其余目标分别验证两个安全边界 fuzz 和可达漏洞。
变更风险分级和证据要求见[贡献约定](CONTRIBUTING.md)。

## 文档

- [产品规范索引](docs/spec/README.md)
- [架构概览](docs/architecture/overview.md)
- [测试架构](docs/architecture/testing.md)
- [文档索引](docs/README.md)
- [历史恢复指针](docs/archive/README.md)
- [贡献与 Git 约定](CONTRIBUTING.md)
