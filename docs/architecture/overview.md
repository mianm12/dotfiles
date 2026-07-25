# 架构概览

本文描述当前 Go 实现的结构，不是产品行为契约。产品规则以
[`../spec/README.md`](../spec/README.md) 为准。

## 数据流

```text
repository desired + machine selection + state + actual filesystem
  -> resolve
  -> plan
  -> execute
  -> verify changed targets
  -> commit state
```

核心业务逻辑与 CLI、文件发布和进程退出分离。`cmd/dot` 只把进程 IO/环境交给 `cli.Run` 并以
其结果退出。

## Package 职责

| Package | 职责 |
| --- | --- |
| `internal/storage` | 原子或不可覆盖的文件发布原语 |
| `internal/core/paths` | HOME target、source 和控制路径边界 |
| `internal/core/state` | ownership state 模型与编解码 |
| `internal/core/config` | repository、machine 和 module 配置解析 |
| `internal/lock` | mutation advisory lock |
| `internal/core/planner` | desired、state 与 actual 的纯计划决策 |
| `internal/core/executor` | mutation 顺序、复核和恢复语义 |
| `internal/cli` | 命令、scope、输出和退出码 |
| `cmd/dot` | 进程入口 |

允许的依赖总体从左向右推进：

```text
storage / paths / state
        -> config / lock
        -> planner
        -> executor
        -> cli
        -> cmd/dot
```

生产 package 不得反向依赖，也不得为了省事越过上述边界；需要改变边界时，先说明职责变化并
同步架构说明。当前依赖方向由完整 diff 和代码审查核对。

## 实现选择

实现使用 Go，优先标准库和窄职责依赖。运行时依赖是：

- Cobra：CLI 解析。
- `go-toml/v2`：配置解析，并通过 `DisallowUnknownFields` 严格加载。
- `gofrs/flock`：单进程 mutation lock。
- `renameio/v2`：state 与机器配置的原子覆盖发布。

测试专用依赖限 `google/go-cmp` 与 `stretchr/testify`。

Local 与新文件的不可覆盖发布不使用 rename：先写 `0600` 临时文件，再以 `os.Link` 发布到
target；已存在时得到 `EEXIST`，同时保证内容完整、不可覆盖和原子出现。

不引入 Viper、虚拟文件系统、DI、事务、workflow、state-machine、日志、color/TUI 或通用
dotfiles framework。Distro 检测解析 `/etc/os-release`，HOME 使用 `os.UserHomeDir`，state
编解码使用标准库 `encoding/json` 与 `DisallowUnknownFields`。

以下细节由实现与测试决定：

- 内部 struct、interface、函数和错误类型。
- State JSON 缩进、字段顺序和可选诊断字段。
- Config/state/lock 的精确路径。
- 原子发布与 link update 的具体系统调用。
- 人类可读输出的排版。
