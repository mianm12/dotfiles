# 测试架构

本文说明当前测试所有权和验证层次。跨层产品结果以
[`../spec/acceptance.md`](../spec/acceptance.md) 为准。

## 当前所有权

- `internal/cli` 同时存在完整跨层套件和部分重叠的 CLI 套件；Config、paths、state、planner 与
  executor 也保留带验收编号的局部行为测试。
- 当前没有唯一的 Go 验收文件；产品契约由规范拥有，完整可执行证据由现有测试集合与
  `make check` 共同提供。
- CLI fixture 保持在 `internal/cli` package 内，不创建跨 package 通用测试框架。
- `cmd/dot` 只负责进程入口接线，完整公开行为由 `cli.Run` 的测试覆盖；当前没有独立进程级
  smoke。

## 合成环境

文件系统测试使用 `t.TempDir` 和绝对路径，显式隔离 HOME、repository、machine config、state
和 lock。测试不得读取或写入真实 HOME、私人 modules、machine config、state 或 lock。

每个成功 mutation 场景再次执行相同 apply，并断言没有新的文件系统 mutation。真实缺陷先
转化为脱敏、最小、合成复现，再进入回归套件和永久门禁。

## 当前验证层次

- Focused tests：开发期间快速验证变更 package 和直接消费者。
- Full gate：`make check` 验证 tidy、format、lint 和全量 race tests。
- 双平台 CI：macOS 与 Ubuntu 运行同一 `make check`。

## 依赖边界

生产 Go package 的预期依赖方向由 [`overview.md`](overview.md) 定义，当前通过完整 diff 与
代码审查核对。新增反向或越层依赖必须先作为架构变更审查。
