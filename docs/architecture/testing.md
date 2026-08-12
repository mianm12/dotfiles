# 测试架构

测试证明当前实现是否满足[产品规范](../spec/README.md)和
[架构边界](overview.md)，不反向创造产品规则。本页定义测试放置、隔离和门禁层次；具体命令的
事实来源是 [Makefile](../../Makefile) 与 CI workflows。

## 测试放置

| 层次 | 主要拥有的证据 |
| --- | --- |
| `internal/storage` | 私有文件首次发布、相同内容 no-op、替换、权限和异常目录项 |
| `internal/core/paths` | Target/source 解析、typed control topology、确定阻塞与不确定 I/O 分类 |
| `internal/core/state` | State schema、严格解码、版本、稳定编码和安全字段 |
| `internal/core/config` | Machine/repository/module 配置、profile、platform applicability 与 variants |
| `internal/core/converge` | Selection resolution、analysis、reconcile、lock、execution、commit 与 recovery |
| `internal/cli` | 公开命令、跨层用户故事、输出、退出码和完整失败边界 |
| `cmd/dot` | 最小进程级 smoke |
| `internal/architecture` | Production package 登记、允许依赖方向与第三方能力 owner |

局部模型在 owner package 测试；用户可观察的完整调用路径在 `internal/cli` 测试。不要把所有行为
塞进进程级测试，也不要为了复用 fixture 创建跨 package 通用测试框架。CLI 合成环境集中在
`internal/cli/testenv_test.go`。

## 必须跨层证明的边界

- Init/select 只发布 machine selection，不读取 state 或修改 target；
- status/dry-run 严格只读，并对完整 effective selection 与 stale state 建立同一 analysis；
- Platform matching 使用注入的 known/unknown OS、distro、arch 合成值，不依赖运行测试的 host；
- apply 在 lock boundary 后获取 lock，锁内只做一次权威 analysis，再执行、复核 changed targets 并
  提交 state；
- blocked apply 只允许 state root/lock bookkeeping，config、state、target、placement parent 与
  local temporary file 保持不变；
- link/local ownership、forget/prune、目录 traversal 与 control boundary 不越权 mutation；
- create 失败不进入 update/prune，update/prune 在 mutation 前继续复核 raw/resolved identity；
- mutation、state commit 或 lock release 失败使用 core typed stage/partial/recovery；结果输出失败
  也必须按可能 partial 提示重跑，且两类失败都不把未完成 Action 投影成成功；
- 每个成功 mutation 场景重复执行相同 apply，并断言 target、state、config 与 lock 内容和 metadata
  都没有新 mutation。

Planner 测试保护可观察语义：输入相同得到字节稳定的 Actions、Issues 与 NextState；phase 顺序、
拓扑 blocker、stale preserve/forget 和 next-state ownership 均由行为反例覆盖。测试不反射 Plan 私有
字段、不保护复制层或索引表，也不要求某个可替换的内部表示继续存在。CLI 测试只验证用户能观察的
facts/actions/issues、summary、恢复提示和退出码。

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

`internal/architecture/dependencies_test.go` 解析 production Go imports，把架构边界作为单向政策：

- 新 production package 未登记时失败；
- 实际 internal import 越过允许方向时失败；
- 新的直接第三方依赖未登记时失败，已登记依赖由错误 package import 时也失败；
- 删除已经不需要的 internal edge 或第三方 import 直接通过，不要求允许项必须保持活跃。

Lock、target mutation、machine selection publication 和 state commit 由
`internal/core/converge` 的 package/API 边界集中表达，不再建立按函数名扫描的第二套 ownership
白名单。新增反向依赖或越层依赖必须先作为架构变化审查，不能靠放宽断言或添加占位 import 通过。
