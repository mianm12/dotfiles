# 0001: Convergence V3 使用单一状态转换模型

状态：Superseded
替代：[ADR 0002](0002-convergence-v4.md)
日期：2026-08-12

## 背景

当前 convergence 流程用多套相互补偿的表示描述同一事实：analysis 保存当前与历史判断，
plan 同时携带 `Complete`、`Issues` 和通用 `Steps`，execution 再用运行时 active 集合与增量
state 修改纠正 step 顺序带来的歧义。模块报告还以字符串状态机重新解释这些中间结果。

这使“某个 placement 最终应存在、删除还是遗忘”没有唯一 owner。一次目标移动会先被拆成
active 与 stale 两条路径，再依赖 executor 知道哪条 stale 不能删除 state。每层单独看都像在
修补上一层，却没有一处直接表达最终不变量。

与此同时，现有实现已经具备必须保留的安全边界：锁前完整 preflight、锁内重新分析、mutation
前复核、原子 state commit、commit 失败后的可重跑恢复，以及 symlink ancestor、directory link
和 alias identity 等路径语义。重构不能用简化模型为借口移除这些能力。

## 决策

采用一次干净的 Convergence V3 切换，不长期并存 V2/V3 双引擎：

- `converge` 继续单独拥有 lock、target mutation、selection publication 和 state commit；CLI 只负责
  输入、调用与展示。
- 每次 assessment 先生成一次不可变的 resolved-control 快照。analysis、planning 与锁内重分析
  只消费该快照，不在不同层重复解析 config、selection、platform 或路径控制条件。
- planner 对每个逻辑 placement key 只产生一个显式 transition。transition 同时描述已观察事实、
  期望事实、所需动作和原因；同一 key 不再分别产生互相补偿的 active/stale step。
- planner 一次性计算不可变 `FinalState`。executor 只校验前置条件并执行显式动作；成功后发布
  `FinalState`，不在执行途中增量修补 state，也不维护用于纠正 planner 的 active map。
- planning 结果以明确的 transitions 与 problems 表达可执行性，不再保留 `Plan.Complete`、通用
  tagged `Step` bag、字符串 `ModuleReport` 状态机或多份完成状态。
- 持久 state 升级为扁平的 version 3 records。每条记录显式携带 module/placement 复合身份、
  ownership 和最终 target；重复复合身份非法，稳定编码按该复合身份排序。V3 不提供 V2
  migration、兼容读取或 reset 子命令；检测到 V2、未知版本或非法 state 时 fail closed，且不执行
  target、selection 或 state mutation。需要切换的用户必须先在仓库外自行归档或移除旧 state。
- 保留现有命令名和退出码。`status` 与 `apply --dry-run` 的文本改为直接展示 facts、actions 与
  problems，不承诺兼容旧的逐行文案。
- 保留 symlink ancestor、directory link、alias identity、target move、stale prune/forget、
  selection publication、锁内重分析和恢复语义；对应产品规则仍由 `docs/spec/` 的各自 owner 定义。
- 分阶段交付 state、resolved controls、transition planner/executor 和最终清理；每一阶段保持一个
  可验证实现，不增加 feature flag、迁移框架、通用工作流框架或新的第三方依赖。

## 后果

- placement 的最终归属和状态只在 planner 的单个 transition 与 `FinalState` 中表达，executor
  不再依靠步骤顺序或隐藏补偿规则推断意图。
- state version 3 是有意的首次发布前破坏性切换。V2 state 不会被自动读取、升级或覆盖；若实际
  分发范围证明不能接受 reset-only 策略，必须先修改本 ADR 与 state 规范，而不是添加静默 fallback。
- CLI 文案可以简化，但命令、退出码、零写入 blocker、重复收敛 no-op 和失败恢复仍是稳定验收边界。
- 规范 owner 必须与引入相应行为变化的实现检查点同步更新；本 ADR 只记录架构取舍，不替代产品规范。
- 状态转换模型、持久格式和恢复路径已完成实现、反例审查与双平台门禁，本 ADR 因而为
  Accepted。后续若重新引入第二真相源或削弱既有安全语义，必须作为新的架构变化审查。
