# 测试架构

本文规定测试所有权和验证层次。产品结果由对应的 [`../spec/`](../spec/) 规则 owner 定义，
测试提供当前实现证据。

## 所有权

- `internal/cli` 的跨层测试按 init、apply、remove、status、analysis、placement、safety、
  recovery 和 scope 等用户行为域组织。
- CLI 的命令语法、错误映射和输出格式由 `commands_test.go` 覆盖。
- Config、paths、state、planner 和 executor 在各自 package 覆盖局部模型与失败边界，并以
  具体行为命名。
- Storage 覆盖私有文件首次发布、相同内容精确 no-op、替换、权限与异常目录项；config/state
  只覆盖各自编码、语义校验及对统一发布原语的调用结果。
- Paths 覆盖确定阻塞与不可确定 I/O 的 typed resolution classification；planner 测试只验证
  分类对应的 prune/forget 策略，不依赖 errno 或 `PathError` 包装形状。
- Executor 负责 Session 固定 controls、单次 lock ownership、selection publication、
  state reload/replan、Close 后拒绝调用等生命周期测试；lock package 只覆盖一次获取、一次
  释放、busy、崩溃恢复和 I/O 分类，不测试嵌套 guard 或引用计数。
- `cmd/dot` 只保留最小进程级 smoke；完整公开行为通过 `cli.Run` 测试。
- CLI 合成环境集中在 `internal/cli/testenv_test.go`，不创建跨 package 通用测试框架。
- CLI 分别验证 status/dry-run 的 prospective forget、成功 state commit 后的过去式 forget
  结果，以及 mutation/state commit/lock release 失败时不输出未完成 action。

## 合成环境

文件系统测试使用 `t.TempDir` 和绝对路径，显式隔离 HOME、repository、machine config、state
和 lock。测试不得读取或写入真实 HOME、私人 modules、machine config、state 或 lock。

每个成功 mutation 场景再次执行相同 apply，并断言没有新的文件系统 mutation。真实缺陷先
转化为脱敏、最小、合成复现，再进入回归套件和永久门禁。

## 验证层次

- Focused tests：开发期间快速验证变更 package 和直接消费者。
- Fast tests：`make test` 快速运行全部 Go 测试。
- Full gate：`make check` 验证 module checksum、tidy、format、lint 和全量 race tests。
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

架构测试解析生产 Go 文件的 imports，并以显式允许边表约束
[`overview.md`](overview.md) 定义的层次；同时机械限制 lock acquisition 与 machine selection
publication 两个敏感 mutation API 的直接引用位置。CLI 不得依赖 lock 或直接发布 machine
selection，lock acquisition 只属于 executor Session。新增反向依赖、越层依赖或敏感 mutation
引用位置必须先作为架构变更审查，不能靠测试白名单静默放行。
该检查使用 parser 的词法绑定区分 internal import 与同名局部值，但不建设跨函数数据流或
调用图分析。敏感 API 所在 package 也不得复用同名 struct 字段或表达式 key；语法门禁会保守
拒绝这类歧义命名。

同一测试还双向校验 [`overview.md`](overview.md) 定义的生产代码直接第三方依赖精确
allowlist。未列出的第三方 import、错误 owner 和已经不存在的陈旧白名单边都必须失败；
tool、transitive 与测试依赖不属于该边表。
