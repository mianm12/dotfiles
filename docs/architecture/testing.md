# 测试架构

本文规定测试所有权和验证层次。跨层产品结果以
[`../spec/acceptance.md`](../spec/acceptance.md) 为准。

## 所有权

- `internal/cli/acceptance_test.go` 是 AC-01 至 AC-19 的唯一跨层验收套件，并机械验证编号集合
  完整。
- CLI 的命令语法、错误映射和输出格式使用独立、按行为命名的测试。
- Config、paths、state、planner 和 executor 在各自 package 覆盖局部模型与失败边界，不复用
  AC 编号。
- `cmd/dot` 只保留最小进程级 smoke；完整公开行为通过 `cli.Run` 验收。
- CLI 合成环境集中在 `internal/cli/testenv_test.go`，不创建跨 package 通用测试框架。

## 合成环境

文件系统测试使用 `t.TempDir` 和绝对路径，显式隔离 HOME、repository、machine config、state
和 lock。测试不得读取或写入真实 HOME、私人 modules、machine config、state 或 lock。

每个成功 mutation 场景再次执行相同 apply，并断言没有新的文件系统 mutation。真实缺陷先
转化为脱敏、最小、合成复现，再进入回归套件和永久门禁。

## 验证层次

- Focused tests：开发期间快速验证变更 package 和直接消费者。
- Acceptance：`make test-acceptance` 验证 AC 编号完整性和跨层产品契约。
- Full gate：`make check` 验证 tidy、format、lint 和全量 race tests。
- Fuzz：`make fuzz` 持续攻击 state decoder 与 target expression 安全边界。
- Vulnerability：`make vuln` 使用固定版本的 `govulncheck` 扫描可达漏洞，作为独立安全验证，
  不加入本地离线 `make check`。
- 双平台 CI：macOS 与 Ubuntu 运行同一 `make check`。

## 架构约束

架构测试解析生产 Go 文件的 imports，并以显式允许边表约束
[`overview.md`](overview.md) 定义的层次。新增反向或越层依赖必须先作为架构变更审查，不能靠
测试白名单静默放行。
