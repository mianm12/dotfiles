# 架构概览

本文描述当前 Go 实现的结构，不是产品行为契约。产品规则以
[`../spec/README.md`](../spec/README.md) 为准。

## 数据流

```text
repository desired + machine selection + state + actual filesystem
  -> status / dry-run: converge.Analyze
       -> resolve one immutable ResolvedControls snapshot
       -> resolve selection + state + targets against that snapshot -> Report
  -> apply: converge.Apply
       -> converge.Analyze (full read-only preflight)
       -> acquire the lock path captured by the preflight snapshot
       -> converge.Analyze (fresh locked inputs, fresh ResolvedControls and Plan)
       -> compare machine semantic fingerprint
       -> execute fresh locked Plan + verify changed targets + commit state
  -> release lock
  -> CLI Report / typed-error projection

init/select -> converge.Initialize / SelectAdd / SelectRemove
            -> operation-specific preflight + lock + reload + atomic config publication + release
```

核心业务逻辑与 CLI、文件发布和进程退出分离。`cmd/dot` 只把进程 IO/环境交给 `cli.Run` 并以
其结果退出。CLI 只构造 `converge.Environment`、调用公开函数并投影 `Report` 或 typed error；不
解析 selection、不规划、不读取 state、不拥有 lock，也不提交 target 或 state。

`converge.Analyze` 是唯一完整只读分析入口。每次 analysis 先把 repository、machine config、state
和 lock 的词法路径及解析身份固定为一个不可变 `paths.ResolvedControls`。Selection 与 platform
只解析一次；planner 的完整 target-set 校验和全部 stale control-boundary 判断只消费该快照，
不能接收 raw controls，也不能重新解析 control topology。私有 analysis 最终只保留 Report、
loaded state、ResolvedControls 和 machine semantic fingerprint；公开 `Report` 只包含客观
ModuleFacts、`Plan{Transitions, Problems}` 与 warnings。Planner 对每个 state key 最多构造一个
Transition，并一次性计算私有不可变 FinalState；executor 只执行 Action 和提交该 FinalState。

`converge.Apply` 在锁前调用同一分析实现完成完整零写入检查，并使用该次快照固定的 lock 路径
获取单次 lock。锁内再次调用同一实现，生成一份 fresh ResolvedControls 和 fresh Plan；只执行这份
锁内结果。锁前 Plan、resolved modules 和 state 不进入执行；fingerprint 只用于拒绝锁前后
machine selection 漂移。这里的“一次解析”以单次 analysis 为边界，不会把锁前 filesystem 身份
错误复用到锁内重分析。

Status 与 dry-run 对全部 effective modules 与全部 state-only stale records 运行同一次全量规划。
Selection、control topology 或 target-set blocker 使 Transitions 为空并生成 Problems；不会过滤
blocked module 后再次局部规划。

公开给 CLI 的 core 接口是：

```go
type Environment struct {
    Home       string
    ConfigPath string
    StatePath  string
    LockPath   string
    Platform   config.Platform
}

type Report struct {
    Facts    []ModuleFact
    Plan     Plan
    Warnings []string
}

func Analyze(Environment) (Report, error)
func Apply(Environment) (ApplyResult, error)
func Initialize(Environment, string, []string) (SelectionResult, error)
func SelectAdd(Environment, string) (SelectionResult, error)
func SelectRemove(Environment, string) (SelectionResult, error)
```

这些函数以单次调用固定 HOME 与 control paths，并通过词法 `defer` 释放内部 lock；不存在
可复制、可复用或需要 closing 状态的 Session/Ownership 对象，也不通过通用 request enum 或
callback workflow 组合 selection 操作。Lock release 失败与 mutation 错误合并为窄 typed
error，已完成写入不回滚；重跑命令文案由 CLI 决定。

## Package 职责

| Package | 职责 |
| --- | --- |
| `internal/storage` | 私有控制目录边界，以及私有控制文件的唯一原子覆盖发布原语 |
| `internal/core/paths` | HOME target、source、不可变 ResolvedControls、控制路径边界和路径解析结果分类 |
| `internal/core/state` | ownership state 模型与编解码 |
| `internal/core/config` | repository、machine 和 module 配置解析 |
| `internal/core/converge` | selection resolution、完整分析、全量规划、lock、target mutation、changed-target 复核与 state/config commit 的唯一 owner |
| `internal/cli` | 命令参数、Environment 构造、Report/typed-error 输出和退出码 |
| `cmd/dot` | 进程入口 |

允许的依赖总体从左向右推进：

```text
storage / paths / state
        -> config
        -> converge
        -> cli -> cmd/dot
```

实际允许边由架构测试中的显式表固定。生产 package 不得反向依赖，也不得为了省事越过该表；
需要改变边界时，先说明职责变化并同步架构说明和机械约束。

生产代码的直接第三方依赖同样由架构测试按精确 import path 固定：

| 第三方 package | 唯一 owner |
| --- | --- |
| `github.com/google/renameio/v2` | `internal/storage` |
| `github.com/gofrs/flock` | `internal/core/converge` |
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

Config 与 state 各自负责编码和语义校验；converge 发布 machine config 和提交 ownership
snapshot 时统一调用 `storage.PublishPrivateFile(path, data)`。该函数在相同内容时执行
零 metadata 写入的严格 no-op，在实际发布时负责 `0700/0600` 权限、renameio 原子替换和
temporary file cleanup。
Paths 在系统调用边界把解析失败分类为确定的 namespace 阻塞或不可确定的 I/O 失败；converge
中的规划代码只消费 typed classification，不检查 `PathError` 包装或平台 errno。
`paths.ResolveControls` 是 control topology 的唯一构造入口。其返回值封装已解析身份，只暴露
cleaned lexical paths、整组 placement 校验和单 target overlap 判断；零值 fail closed。Converge
不会保留与该快照并行的 raw-control planner 路径。

不引入 Viper、虚拟文件系统、DI、事务、workflow、state-machine、日志、color/TUI 或通用
dotfiles framework。Distro 检测解析 `/etc/os-release`，HOME 使用 `os.UserHomeDir`，state
编解码使用标准库 `encoding/json` 与 `DisallowUnknownFields`。

以下细节由实现与测试决定：

- 内部 struct、interface、函数和错误类型。
- State JSON 缩进、字段顺序和可选诊断字段。
- Config/state/lock 的精确路径。
- 原子发布与 link update 的具体系统调用。
- 人类可读输出的排版。
