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
重新分析后，只把 prospective machine、已解析 module 集合和 scope 交给持锁 Session；
Session 固定 HOME 与 controls，selection 发布和 artifact convergence 都不能替换这组边界。
Converge 再次加载 state 并重新规划。Scoped mutation 的 module 投影只包含请求 scope 和具有
module-specific blocker 的 module；完整 prospective selection 仍保存在 machine 中，其他
effective modules 继续通过已解析 module 集合参与 target topology 校验。

唯一内部 mutation 生命周期是：

```go
OpenSession(home, controls)
Session.PublishSelection(machine)
Session.Converge(modules, scope)
Session.Close()
```

CLI 不直接获取、复用或释放 lock，也不直接发布 machine selection。Session 只表达这一条
线性生命周期，不引入通用 workflow、事务或 rollback。Session 和 lock ownership 都只支持
同一指针的串行使用，不得复制或并发调用。Close 失败表示 lock 尚未确认释放；Session 不再
接受 selection 或 artifact mutation，只允许重试 Close。成功释放后，同一 ownership 不得再次
释放。

## Package 职责

| Package | 职责 |
| --- | --- |
| `internal/storage` | 私有控制目录边界，以及私有控制文件的唯一原子覆盖发布原语 |
| `internal/core/paths` | HOME target、source、控制路径边界和路径解析结果分类 |
| `internal/core/state` | ownership state 模型与编解码 |
| `internal/core/config` | repository、machine 和 module 配置解析 |
| `internal/lock` | mutation advisory lock |
| `internal/core/planner` | desired、state 与 actual 的纯计划决策；action 保存结构化 forget 原因 |
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

生产代码的直接第三方依赖同样由架构测试按精确 import path 固定：

| 第三方 package | 唯一 owner |
| --- | --- |
| `github.com/google/renameio/v2` | `internal/storage` |
| `github.com/gofrs/flock` | `internal/lock` |
| `github.com/pelletier/go-toml/v2` | `internal/core/config` |
| `github.com/spf13/cobra` | `internal/cli` |

标准库、仓库内部 import、测试依赖、tool-only module 和间接 module 不进入该表。它只约束
`cmd/`、`internal/` 中 production Go 文件实际存在的直接边；新增或移动第三方 import 必须先
作为架构变化审查。

## 实现选择

实现使用 Go，优先标准库和窄职责依赖。运行时依赖是：

- Cobra：CLI 解析。
- `go-toml/v2`：配置解析，并通过 `DisallowUnknownFields` 严格加载。
- `gofrs/flock`：单进程 mutation lock。
- `renameio/v2`：仅由 `internal/storage` 用于私有 control file 的原子覆盖发布。

测试专用依赖限 `google/go-cmp` 与 `stretchr/testify`。

`golangci-lint` 与 `govulncheck` 通过 `go.mod` 的 `tool` directive 固定版本，并由
`go tool` 调用。它们进入 module graph，但不是生产代码第三方 import allowlist 的一部分。

Local 与新文件的不可覆盖发布不使用 rename：先写 `0600` 临时文件，再以 `os.Link` 发布到
target；已存在时得到 `EEXIST`，同时保证内容完整、不可覆盖和原子出现。

机器配置与 state 各自负责编码和语义校验，再统一调用
`storage.PublishPrivateFile(path, data)`；该函数在相同内容时执行零 metadata 写入的严格
no-op，在实际发布时负责 `0700/0600` 权限、renameio 原子替换和 temporary file cleanup。
Paths 在系统调用边界把解析失败分类为确定的 namespace 阻塞或不可确定的 I/O 失败，planner
只消费 typed classification，不检查 `PathError` 包装或平台 errno。

不引入 Viper、虚拟文件系统、DI、事务、workflow、state-machine、日志、color/TUI 或通用
dotfiles framework。Distro 检测解析 `/etc/os-release`，HOME 使用 `os.UserHomeDir`，state
编解码使用标准库 `encoding/json` 与 `DisallowUnknownFields`。

以下细节由实现与测试决定：

- 内部 struct、interface、函数和错误类型。
- State JSON 缩进、字段顺序和可选诊断字段。
- Config/state/lock 的精确路径。
- 原子发布与 link update 的具体系统调用。
- 人类可读输出的排版。
