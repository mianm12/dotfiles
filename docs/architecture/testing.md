# 测试架构

测试证明当前实现是否满足[产品规范](../spec/README.md)和
[架构边界](overview.md)，不反向创造产品规则。本页定义测试放置、隔离和门禁层次；具体命令的
事实来源是 [Makefile](../../Makefile) 与 CI workflows。

## 测试放置

| 层次 | 主要拥有的证据 |
| --- | --- |
| `internal/storage` | 私有文件首次发布、相同内容但权限不对的修复、替换和异常目录项 |
| `internal/core/paths` | 词法 target、控制前缀隔离 |
| `internal/core/state` | State v5 schema、先看 version 再严格解码 |
| `internal/core/config` | Machine/repository/module 配置、profile、platform applicability 与 variants |
| `internal/core/converge` | Selection、同一条循环、lock、execution、commit 与 recovery |
| `internal/cli` | 公开命令、跨层用户故事、输出、退出码和完整失败边界 |
| `cmd/dot` | 最小进程级 smoke |

局部模型在 owner package 测试；用户可观察的完整调用路径在 `internal/cli` 测试。不要把所有行为
塞进进程级测试，也不要为了复用 fixture 创建跨 package 通用测试框架。CLI 合成环境集中在
`internal/cli/testenv_test.go`。

## 必须跨层证明的边界

- Init/select 只发布 machine selection，不读取 state 或修改 target；
- status/dry-run 严格只读，并对完整 effective selection 与 stale state 建立同一 analysis；
- Platform matching 使用注入的 known/unknown OS、distro、arch 合成值，不依赖运行测试的 host；
- apply 在 lock boundary 后获取 lock，锁内再跑同一条循环，有 skip 则不写；
- 含 skip 的 apply 只允许 state root/lock bookkeeping，config、state、target、placement parent 与
  local temporary file 保持不变；
- `link` / `file` / `replace` / `remove` / `record` / `forget` 的磁盘效果与账本效果必须成对验证；
- link/local ownership、forget/remove 与控制前缀不越权 mutation；
- create 失败不进入 replace/remove；删除前只复核 raw dest；
- mutation、state commit 或 lock release 失败使用 typed stage/partial/recovery；失败只投影已完成
  的行；
- 每个成功 mutation 场景重复执行相同 apply，并断言没有新的文件系统 mutation。

循环测试保护可观察语义：相同输入得到稳定的行；词法嵌套 skip、stale forget/remove 和执行后
ownership 由行为反例覆盖。CLI 测试只验证用户能观察的 facts、行、恢复提示和退出码。

## 合成环境与私人数据

文件系统测试使用 `t.TempDir` 和绝对路径，显式隔离 HOME、repository、machine config、state
与 lock。测试不得读取或写入真实 HOME、私人 modules、machine config、state 或 lock。

唯一例外是 config package 的 tracked-repository smoke：它只读当前 checkout 的 `dot.toml` 与
recognized modules，在支持的平台矩阵中验证 tracked 配置；它不读取 HOME 控制文件，也不执行
CLI 或 mutation。

真实缺陷先转化为脱敏、最小、合成复现。无法在合成环境证明的真实机器观察必须单独标为
Operational，不用本地偶然成功替代测试。

## 验证层次

| 层次 | 入口 | 何时使用 |
| --- | --- | --- |
| Focused | `go test` 指定 package / test | 开发期间验证变更 owner 与直接消费者 |
| Fast | `make test` | 快速运行全部 Go tests |
| Full gate | `make check` | Module/tidy/format/lint/race/build/version 的当前平台完整门禁 |
| Fuzz | `make fuzz` | State decoder、target expression、os-release parser 的边界攻击 |
| Vulnerability | `make vuln` | 固定工具版本的可达漏洞扫描 |
| Dual-platform CI | macOS 与 Ubuntu workflows | Pull Request 上运行同一 `make check` |

Fuzz 与 vulnerability 的远程触发、required 状态和版本以当前 workflows / `tools/go.mod` 为准，
不在本文复制调度配置。Coverage 不设置简单全局百分比阈值；永久门禁优先覆盖数据完整性与失败
边界，而不是追求无上下文的数字。

## 依赖变更证据

Production imports 与 module graph 是代码事实，不再复制成测试内的 package/第三方 owner 注册表。
新增 package、反向依赖或第三方依赖时，直接审查 imports、`go.mod`、职责归属和完整 diff；真正的
安全不变量继续由 owner package 与跨层行为测试证明。
