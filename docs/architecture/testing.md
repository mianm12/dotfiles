# 测试架构

测试证明当前实现是否满足[产品规范](../spec/README.md)和
[架构边界](overview.md)，不反向创造产品规则。本页定义测试放置、隔离和门禁层次；具体命令的
事实来源是 [Makefile](../../Makefile) 与 CI workflows。

## 测试放置

| 层次 | 主要拥有的证据 |
| --- | --- |
| `internal/storage` | 私有文件首次发布、相同内容 no-op、替换、权限和异常目录项 |
| `internal/core/paths` | Target/source 解析、control topology、确定阻塞与不确定 I/O 分类 |
| `internal/core/state` | State schema、严格解码、版本、稳定编码和安全字段 |
| `internal/core/config` | Machine/repository/module 配置、profile、platform applicability 与 variants |
| `internal/core/converge` | Selection resolution、analysis、planning、lock、execution、commit 与 recovery |
| `internal/cli` | 公开命令、跨层用户故事、输出、退出码和完整失败边界 |
| `cmd/dot` | 最小进程级 smoke |
| `internal/architecture` | Production internal imports 与第三方 owner 的精确 allowlist |

局部模型在 owner package 测试；用户可观察的完整调用路径在 `internal/cli` 测试。不要把所有行为
塞进进程级测试，也不要为了复用 fixture 创建跨 package 通用测试框架。CLI 合成环境集中在
`internal/cli/testenv_test.go`。

## 必须跨层证明的边界

- Init/select 只发布 machine selection，不读取 state 或修改 target；
- status/dry-run 严格只读，并对完整 effective selection 与 stale state 建立同一 analysis；
- Platform matching 使用注入的 known/unknown OS、distro、arch 合成值，不依赖运行测试的 host；
- apply 锁前零写入 preflight、锁内 fresh analysis、changed-target 复核和 state commit；
- deterministic blocker 不创建 state root、lock、target 或临时文件；
- link/local ownership、forget/prune、目录 traversal 与 control boundary 不越权 mutation；
- mutation、state commit 或 lock release 失败后不把未完成 Action 投影成成功；
- 每个成功 mutation 场景重复执行相同 apply，并断言没有新的文件系统 mutation。

Planner 的内部断言应直接覆盖一个 key 一个 Transition、Action 顺序和 FinalState；CLI 测试只验证
用户能观察的 facts/actions/problems/warnings，不复制 planner 的内部状态机。

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

## 架构约束测试

`internal/architecture/dependencies_test.go` 解析 production Go imports，并双向检查
[架构概览](overview.md#package-地图)中的内部依赖边和第三方 owner：

- 代码出现未列出的 package/import/owner 时失败；
- allowlist 保留已经不存在的 package 或 edge 时也失败；
- 标准库、tests、tools 与 transitive dependencies 不混入 production 表。

Lock、target mutation、machine selection publication 和 state commit 由
`internal/core/converge` 的 package/API 边界集中表达，不再建立按函数名扫描的第二套 ownership
白名单。新增反向依赖或越层依赖必须先作为架构变化审查，不能靠单向放宽测试通过。
