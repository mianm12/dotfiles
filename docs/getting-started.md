# 安全跑通一次 `dot`

本页帮助第一次接触项目的人在一个临时 HOME 中完成 `init → dry-run → apply → status`，
不会读取或修改真实用户的 machine config、state、lock 或配置 target。完成后，你会看到
repository、机器选择和文件系统收敛之间的基本关系。

精确命令行为以[公共 CLI 规范](spec/cli.md)为准；这里是一条可操作的学习路径，不是第二份
产品契约。

## 前置条件

- macOS 或 Linux；
- Git；
- `go.mod` 声明的 Go 版本；
- 当前 shell 位于本仓库根目录。

先确认仓库和工具入口：

```sh
git status --short --branch
make help
```

本教程会构建 `bin/dot`，但不会安装系统软件或修改真实 HOME。

## 1. 创建隔离环境并构建

```sh
DOT_TUTORIAL_ROOT="$(mktemp -d)"
DOT_TUTORIAL_HOME="$DOT_TUTORIAL_ROOT/home"
mkdir -p "$DOT_TUTORIAL_HOME"
make build
```

两个变量都必须是绝对路径。之后每次调用 `dot` 都显式传入相同的临时 HOME；不要省略命令前的
`HOME="$DOT_TUTORIAL_HOME"`。

```sh
HOME="$DOT_TUTORIAL_HOME" bin/dot paths
```

输出的 `home`、`machine_config`、`state` 和 `lock` 都应位于 `$DOT_TUTORIAL_HOME` 下。如果不是，
先停止，不要运行后续 mutation 命令。

## 2. 初始化机器选择

```sh
HOME="$DOT_TUTORIAL_HOME" bin/dot init "$PWD"
```

`init` 记录当前绝对 repository 路径，并选择仓库的 `default` profile；它不会创建 Starship
target。可以再次用 `paths` 定位刚生成的 machine config：

```sh
HOME="$DOT_TUTORIAL_HOME" bin/dot paths
```

## 3. 检查默认 selection

仓库的 `default` profile 当前选择跨 macOS/Linux 的 `starship` module。`init` 与 convergence
刻意分离，因此 target 仍未创建：

```sh
test ! -e "$DOT_TUTORIAL_HOME/.config/starship.toml"
```

这种“选择已确定、文件系统尚未收敛”的状态是工作模型的一部分。机器特有的直接 module 仍用
`select add` 管理；本教程无需重复选择已经由 profile 激活的 starship。

## 4. 先看计划，再执行

```sh
HOME="$DOT_TUTORIAL_HOME" bin/dot apply --dry-run
```

确认输出只计划在临时 HOME 中创建 `~/.config/starship.toml` 后，再执行：

```sh
HOME="$DOT_TUTORIAL_HOME" bin/dot apply
HOME="$DOT_TUTORIAL_HOME" bin/dot status
```

成功后，临时 HOME 中的 Starship 配置是指向仓库 source 的 symlink；`status` 不应再报告待执行
循环行或 `skip`：

```sh
ls -l "$DOT_TUTORIAL_HOME/.config/starship.toml"
HOME="$DOT_TUTORIAL_HOME" bin/dot apply --dry-run
```

最后一次 dry-run 应显示已经收敛。相同输入下重复 apply 不产生新的文件系统 mutation，是
`dot` 的核心不变量之一。

## 5. 清理临时环境

先打印并检查待删除路径：

```sh
printf '%s\n' "$DOT_TUTORIAL_ROOT"
```

确认它确实是本教程刚创建的临时目录后，可以删除它：

```sh
test -n "$DOT_TUTORIAL_ROOT" && rm -rf -- "$DOT_TUTORIAL_ROOT"
```

这只删除隔离 HOME；仓库中的 `bin/dot` 属于被忽略的构建产物，可按日常开发习惯保留。

## 在真实 HOME 使用前

不要把教程中的临时 HOME 变量直接替换掉就盲目 apply。真实使用至少先完成以下检查：

1. 阅读 [`dot.toml`](../dot.toml) 和准备启用的 `modules/<id>/module.toml`；
2. 运行 `dot paths`，确认当前 machine config、state 和 lock 的位置；
3. 检查每个 target 是否已经存在普通文件、目录或未知 symlink；
4. 使用 `dot apply --dry-run` 阅读全部循环行；
5. 只有确认冲突已经由你人工处理后才运行 `dot apply`。

`dot` 不会自动导入、备份或覆盖未知数据。需要理解原因时阅读
[所有权与安全边界](concepts/ownership-and-safety.md)；需要精确规则时回到
[产品规范索引](spec/README.md)。

真实新机已经 clone 仓库后，可以用根目录的 `./bootstrap.sh --preview-apply` 安装/init 并只预览
最后的 apply，确认后再运行 `./bootstrap.sh`。Bootstrap 不 clone/pull repository，也不安装 Go、
Git 或包管理器。

## 接下来读什么

- [工作模型](concepts/mental-model.md)：理解一次 convergence 的四类输入；
- [管理 modules 与 placements](guides/manage-modules.md)：开始组织真实配置；
- [Profiles 与平台差异](guides/profiles-and-platforms.md)：为多台机器建立 selection；
- [所有权与安全边界](concepts/ownership-and-safety.md)：理解为什么某些 target 会被拒绝；
- [故障排查](guides/troubleshooting.md)：从循环行和 stderr 提示定位输入事实；
- [术语表](glossary.md)：快速查询 module、placement、state 等词；
- [开发者入口](development/README.md)：开始阅读和修改代码。
