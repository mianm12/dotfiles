# dot

`dot` 是个人使用的 macOS/Linux dotfiles 管理 CLI。它以 symlink 集中管理共享配置，并从
`*.local.example` 在目标缺失时初始化不进入 Git 的本机内容。

## 当前能力

当前 Go 实现支持 macOS/Linux、profiles、portable 或 platform variants modules、link/local
placements、dry-run、ownership state 和单进程 mutation lock。

公开命令为 `init`、`select add`、`select remove`、`status`、`apply`、`paths`、`version` 和
`help`。完整行为和安全边界以[产品规范集合](docs/spec/README.md)为准，当前实现证据以代码、
测试和 CI 为准。

## 开始使用

第一次接触项目，建议先按[安全入门教程](docs/getting-started.md)在临时 HOME 中跑通完整流程；
它不会读取或修改真实机器的 config、state、lock 或配置 target。

确认工作模型和 target 后，在已经 clone 的真实 checkout 中运行：

```sh
./bootstrap.sh --preview-apply
./bootstrap.sh
~/.local/bin/dot status
```

Bootstrap 默认把独立 binary 安装到 `~/.local/bin/dot`，用 repository 的 `default` profile
初始化 machine config，再执行全量 apply。`--preview-apply` 仍会安装和 init，只预览最后一步；
安装目录不在 `PATH` 时脚本会提示，但内部仍使用绝对 binary 路径。

`select` 只改本机 selection，`apply` 才收敛 target。`apply` 不会覆盖已有普通文件、目录或未知
symlink；启用 module 前应先人工检查并迁移冲突 target。`dot` 不负责安装软件，也不提供自动
导入、backup 或 rollback。工作原理见[核心心智模型](docs/concepts/mental-model.md)，数据安全
边界见[所有权与安全](docs/concepts/ownership-and-safety.md)。

## 日常更新与清理

- Git 更新由用户在 `dot` 外部完成。只改了 repository 配置内容时，symlink source 内容立即可见；
  profile、module 或 placement 拓扑变化再运行 `dot apply --dry-run` 和 `dot apply`。
- Go CLI 源码变化后运行 `make install`，或重跑 `./bootstrap.sh`。`INSTALL_DIR=/absolute/path`
  可以覆盖默认安装目录。
- 停用 module 时先编辑 active profile，或对直接 selection 运行 `dot select remove MODULE`，再
  apply；历史 link 按 `remove`/`forget` 规则处理，local 文件仍被保留。
- Binary 与 control data 的卸载是人工运维动作。先用 `dot paths` 确认当前路径；产品不提供会
  顺手删除它们的 `clean` 或 `uninstall` 命令。

## 开发验证

```sh
make test
make check
make fuzz
make vuln
```

`make test` 快速运行全部 Go 测试与隔离 bootstrap smoke；`make check` 执行依赖整洁度、格式、
静态分析、全量 race tests，并构建生产二进制、校验 `version` 构建信息，CI 在 macOS 与 Linux
上运行同一入口。
其余目标分别验证 state、target expression 与 os-release 三个安全边界 fuzz，以及可达漏洞。
变更风险分级和证据要求见
[贡献约定](CONTRIBUTING.md)。

## 文档

- [知识库首页](docs/README.md)：按新用户、使用者、开发者和设计审查任务查找内容；
- [安全入门教程](docs/getting-started.md)：在隔离 HOME 中完成第一次收敛；
- [工作模型](docs/concepts/mental-model.md)：理解 desired、selection、state、filesystem 与同一条收敛循环；
- [使用指南](docs/README.md#常用任务)：管理 modules、profiles、平台差异、迁移与故障排查；
- [产品规范索引](docs/spec/README.md)：唯一产品行为契约；
- [开发者入口](docs/development/README.md)：从需求定位到代码、测试和交付；
- [贡献与 Git 约定](CONTRIBUTING.md)：变更分级、验证证据与 PR 流程。
