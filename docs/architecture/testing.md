# 测试架构

本文规定测试所有权和验证层次。产品结果由对应的 [`../spec/`](../spec/) 规则 owner 定义，
测试提供当前实现证据。

## 所有权

- `internal/cli` 的跨层测试按 init、select、apply、status、analysis、placement、safety、
  recovery 和 full convergence 等用户行为域组织。
- CLI 的命令语法、错误映射和输出格式由 `commands_test.go` 覆盖。
- Config、paths、state、planner 和 executor 在各自 package 覆盖局部模型与失败边界，并以
  具体行为命名。
- Storage 覆盖私有文件首次发布、相同内容精确 no-op、替换、权限与异常目录项；config/state
  只覆盖各自编码和语义校验；mutation/executor 覆盖编码结果进入统一发布原语的调用边界。
- Paths 覆盖确定阻塞与不可确定 I/O 的 typed resolution classification；planner 测试只验证
  分类对应的 prune/forget 策略，不依赖 errno 或 `PathError` 包装形状。
- Mutation 负责固定 controls、单次 lock ownership、selection publication、锁前完整 artifact
  preflight、锁内重新解析的跨层测试；直接调用 `mutation.Apply` 的 deterministic blocker 必须
  验证 state root 与 lock 均未创建。其 internal lock 只覆盖获取、释放、busy 和异常目录项。
  Executor 覆盖共用只读 Analyze、锁内 fresh plan、plan 执行、changed-target 复核、state commit
  与恢复事实。
- `cmd/dot` 只保留最小进程级 smoke；完整公开行为通过 `cli.Run` 测试。
- CLI 合成环境集中在 `internal/cli/testenv_test.go`，不创建跨 package 通用测试框架。
- CLI 分别验证 status/dry-run 的当前 selection forget、成功 state commit 后的过去式 forget
  结果，以及 mutation/state commit/lock release 失败时不输出未完成 step。

## 合成环境

文件系统测试使用 `t.TempDir` 和绝对路径，显式隔离 HOME、repository、machine config、state
和 lock。测试不得读取或写入真实 HOME、私人 modules、machine config、state 或 lock。

唯一例外是 config package 的 tracked-repository smoke：它只读当前 checkout 的 `dot.toml`
与 recognized modules，验证实际仓库配置可以在支持的平台矩阵中解析；不读取 HOME、machine
config、state 或 lock，也不执行 CLI 或 mutation。

每个成功 mutation 场景再次执行相同 apply，并断言没有新的文件系统 mutation。真实缺陷先
转化为脱敏、最小、合成复现，再进入回归套件和永久门禁。

## 验证层次

- Focused tests：开发期间快速验证变更 package 和直接消费者。
- Fast tests：`make test` 快速运行全部 Go 测试。
- Full gate：`make check` 验证 module checksum、tidy、format、lint、全量 race tests，并
  构建生产二进制、校验 `version` 构建信息。
- Fuzz：`make fuzz` 持续攻击 state decoder、target expression 与 os-release ID parser
  安全边界。独立 workflow 只在每周计划或手动触发时运行，不响应 Pull Request，也不作为
  required check；Pull Request 继续只运行确定性门禁。Fuzz 失败时保留 Go 写出的最小失败
  输入，供本地回归。
- Vulnerability：`make vuln` 使用 `tools/go.mod` tool directive 固定的 `govulncheck` 扫描可达
  漏洞，不加入本地离线 `make check`；仓库 workflow 在相关 Go Pull Request、每周计划和手动
  触发时运行它，但不作为 required check。
- 双平台 CI：macOS 与 Ubuntu 在 Pull Request 上运行同一 `make check`，并作为 `main` 的
  required checks。
- Coverage：不设置简单的全局百分比阈值；永久门禁优先直接覆盖 control/placement topology、
  platform indeterminate 零写入、state/input 类型、forget/prune、mutation 恢复与重复收敛等
  关键安全行为。

## 架构约束

架构测试只解析生产 Go 文件的 imports，并以显式允许边表约束
[`overview.md`](overview.md) 定义的层次。Lock 实现位于 mutation 私有的 Go `internal`
package；machine selection 没有公开 publication API，由 mutation 组合 config 编码与 storage
发布。因此 ownership 由 package/API 结构表达，不再维护按函数名扫描 AST 的第二套白名单。
新增反向依赖或越层依赖必须先作为架构变更审查，不能靠测试白名单静默放行。

同一测试还双向校验 [`overview.md`](overview.md) 定义的生产代码直接第三方依赖精确
allowlist。未列出的第三方 import、错误 owner 和已经不存在的陈旧白名单边都必须失败；
tool、transitive 与测试依赖不属于该边表。
