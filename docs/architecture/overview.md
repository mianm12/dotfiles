# 架构概览

本文描述当前 Go 实现的结构，不是产品行为契约。产品规则以
[`../spec/README.md`](../spec/README.md) 为准。

## 数据流

```text
repository desired + machine selection + state + actual filesystem
  -> status / dry-run: CLI OperationAnalysis (module projection + Plan{Steps, Issues})
  -> apply: mutation.Apply
       -> resolve selection + executor.Analyze (full read-only preflight)
       -> acquire lock
       -> reload machine/repository + resolve selection
       -> executor.Execute (fresh analysis + execute + verify + commit state)
  -> release lock
  -> CLI mutation result projection

init/select -> mutation.UpdateSelection
            -> preflight + lock + reload + atomic config publication + release
```

核心业务逻辑与 CLI、文件发布和进程退出分离。`cmd/dot` 只把进程 IO/环境交给 `cli.Run` 并以
其结果退出。CLI OperationAnalysis 只服务 status 与 dry-run：它是只读观察结果，只保存 module
投影、`Plan{Steps, Issues}` 与输入 warning，不夹带 resolved modules、scope 或 loaded state
供执行使用，也不是 executor 输入。
Status 对一次请求只调用 planner 一次；path validation error 自带受影响 placement labels，不靠
逐 module probe 定位。`mutation.Apply` 直接拥有一次 artifact mutation：锁前解析 selection 并
调用 `executor.Analyze` 完成 state、actual 与 planner 的完整零写入检查；成功后获取单次 lock，
锁内重新加载 machine/repository、重新解析 selection，再由 `executor.Execute` 调用同一私有分析
实现重新加载最新 state、重新规划、执行并提交 state。锁前 analysis 不传入 executor 执行。
`UpdateSelection` 独立
拥有 config-only mutation；两条路径不通过 Session 或 callback 组合。Scoped mutation 的 module
投影只包含请求 scope 和具有 module-specific blocker 的 module；完整 current selection 仍保存
在 machine 中，其他
effective modules 继续通过已解析 module 集合参与 target topology 校验。

公开给 CLI 的 mutation 写入口只有：

```go
mutation.UpdateSelection(request)
mutation.Apply(request)
```

CLI 不直接获取、复用或释放 lock，也不直接发布 machine selection。Mutation 函数以单次调用
固定 HOME 与 controls，并通过词法 `defer` 释放内部 lock；不存在可复制、可复用或需要 closing
状态的 Session/Ownership 对象。Lock release 失败与 mutation 错误合并报告，已完成写入不回滚。

## Package 职责

| Package | 职责 |
| --- | --- |
| `internal/storage` | 私有控制目录边界，以及私有控制文件的唯一原子覆盖发布原语 |
| `internal/core/paths` | HOME target、source、控制路径边界和路径解析结果分类 |
| `internal/core/state` | ownership state 模型与编解码 |
| `internal/core/config` | repository、machine 和 module 配置解析 |
| `internal/core/selection` | machine selection 的纯 resolution 与 typed issue |
| `internal/core/planner` | desired、state 与 actual 的纯计划决策；输出 `Plan{Steps, Issues}` |
| `internal/core/executor` | artifact 的只读分析、锁内重新规划、执行、changed-target 复核与 state 提交 |
| `internal/core/mutation` | UpdateSelection/Apply、完整锁前 preflight、control 校验与 lock ownership |
| `internal/core/mutation/internal/lock` | mutation 私有的 advisory lock 实现 |
| `internal/cli` | 命令、只读 operation analysis、scope、输出和退出码 |
| `cmd/dot` | 进程入口 |

允许的依赖总体从左向右推进：

```text
storage / paths / state
        -> config -> selection
        -> planner -> executor
        -> mutation
        -> cli -> cmd/dot
```

实际允许边由架构测试中的显式表固定。生产 package 不得反向依赖，也不得为了省事越过该表；
需要改变边界时，先说明职责变化并同步架构说明和机械约束。

生产代码的直接第三方依赖同样由架构测试按精确 import path 固定：

| 第三方 package | 唯一 owner |
| --- | --- |
| `github.com/google/renameio/v2` | `internal/storage` |
| `github.com/gofrs/flock` | `internal/core/mutation/internal/lock` |
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

`golangci-lint` 与 `govulncheck` 通过 `tools/go.mod` 的 `tool` directive 固定版本，并从仓库
根目录使用 `go tool -modfile=tools/go.mod` 调用。工具 module graph 与产品 module graph
隔离，也不属于生产代码第三方 import allowlist。Makefile 拥有的 Go 命令固定使用
`GOWORK=off`，包括解析 buildinfo package 的 module 查询；被忽略的本地或父目录 workspace
不得改变门禁、生产构建 graph 或版本注入。

Local 与新文件的不可覆盖发布不使用 rename：先写 `0600` 临时文件，再以 `os.Link` 发布到
target；已存在时得到 `EEXIST`，同时保证内容完整、不可覆盖和原子出现。

Config 与 state 各自负责编码和语义校验；mutation 发布 machine config、executor 提交
ownership snapshot 时统一调用 `storage.PublishPrivateFile(path, data)`。该函数在相同内容时执行
零 metadata 写入的严格 no-op，在实际发布时负责 `0700/0600` 权限、renameio 原子替换和
temporary file cleanup。
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
