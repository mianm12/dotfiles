# 测试架构

本文规定测试所有权和验证层次。产品结果由对应的 [`../spec/`](../spec/) 规则 owner 定义，
测试提供当前实现证据。

## 所有权

- `internal/cli` 的跨层测试按 init、apply、remove、status、placement、safety、recovery 和
  scope 等用户行为域组织。
- CLI 的命令语法、错误映射和输出格式由 `commands_test.go` 覆盖。
- Config、paths、state、planner 和 executor 在各自 package 覆盖局部模型与失败边界，并以
  具体行为命名。
- `cmd/dot` 只保留最小进程级 smoke；完整公开行为通过 `cli.Run` 测试。
- CLI 合成环境集中在 `internal/cli/testenv_test.go`，不创建跨 package 通用测试框架。

## 合成环境

文件系统测试使用 `t.TempDir` 和绝对路径，显式隔离 HOME、repository、machine config、state
和 lock。测试不得读取或写入真实 HOME、私人 modules、machine config、state 或 lock。

每个成功 mutation 场景再次执行相同 apply，并断言没有新的文件系统 mutation。真实缺陷先
转化为脱敏、最小、合成复现，再进入回归套件和永久门禁。

## 验证层次

- Focused tests：开发期间快速验证变更 package 和直接消费者。
- Fast tests：`make test` 快速运行全部 Go 测试。
- Full gate：`make check` 验证 tidy、format、lint 和全量 race tests。
- Fuzz：`make fuzz` 持续攻击 state decoder 与 target expression 安全边界。
- Vulnerability：`make vuln` 使用固定版本的 `govulncheck` 扫描可达漏洞，不加入本地离线
  `make check`；仓库 workflow 在 PR、`main` push、每周计划和手动触发时运行它。将该状态设为
  required check 属于合入后的独立 operational 授权。
- 双平台 CI：macOS 与 Ubuntu 运行同一 `make check`。

## 架构约束

架构测试解析生产 Go 文件的 imports，并以显式允许边表约束
[`overview.md`](overview.md) 定义的层次。新增反向或越层依赖必须先作为架构变更审查，不能靠
测试白名单静默放行。
