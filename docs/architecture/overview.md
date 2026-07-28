# 架构概览

本文描述当前 Go 实现的结构，不是产品行为契约。产品规则以
[`../spec/README.md`](../spec/README.md) 为准。

## 数据流

```text
repository desired + machine selection + state + actual filesystem
  -> CLI OperationAnalysis (selection delta + resolve + plan)
  -> status / dry-run projection
  -> executor OpenSession (fixed HOME + controls + lock ownership)
  -> rebuild OperationAnalysis
  -> Session.PublishSelection
  -> Session.Converge (reload state + replan + execute + verify + commit state)
  -> Session.Close
  -> CLI mutation result projection
```

核心业务逻辑与 CLI、文件发布和进程退出分离。`cmd/dot` 只把进程 IO/环境交给 `cli.Run` 并以
其结果退出。OperationAnalysis 是只读观察结果，不是 executor 输入；真实 mutation 在锁内
重新分析后，只把 prospective machine、resolution 和 scope 交给持锁 Session；Session 固定
HOME 与 controls，selection 发布和 artifact convergence 都不能替换这组边界。Converge 再次
加载 state 并重新规划。Scoped mutation 的 module 投影只包含请求 scope 和具有 module-specific
blocker 的 module；完整 prospective selection 仍保存在 machine 中，其他 effective modules
继续通过 resolution 参与 target topology 校验。

唯一内部 mutation 生命周期是：

```go
OpenSession(home, controls)
Session.PublishSelection(machine)
Session.Converge(modules, scope)
Session.Close()
```

CLI 不直接获取、复用或释放 lock，也不直接发布 machine selection。Session 只表达这一条
线性生命周期，不引入通用 workflow、事务或 rollback。Session 的值副本共享同一份 lock
ownership 与关闭状态；Close 失败后只允许重试 Close，不再接受 selection 或 artifact
mutation。

## Package 职责

| Package | 职责 |
| --- | --- |
| `internal/storage` | 原子或不可覆盖的文件发布原语 |
| `internal/core/paths` | HOME target、source 和控制路径边界 |
| `internal/core/state` | ownership state 模型与编解码 |
| `internal/core/config` | repository、machine 和 module 配置解析 |
| `internal/lock` | mutation advisory lock |
| `internal/core/planner` | desired、state 与 actual 的纯计划决策 |
| `internal/core/executor` | Session、lock ownership、selection 发布调度、mutation 顺序、复核和恢复语义 |
| `internal/cli` | 命令、只读 operation analysis、scope、输出和退出码 |
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

实际允许边由架构测试中的显式表固定。生产 package 不得反向依赖，也不得为了省事越过该表；
需要改变边界时，先说明职责变化并同步架构说明和机械约束。

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
