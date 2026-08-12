# Architecture Decision Records

本目录只记录难以逆转、跨越多个实现区域，并且未来维护者仍需要理解“为什么”的选择。

## 何时创建

适合 ADR：

- 持久格式或兼容策略；
- 关键依赖或平台边界；
- ownership、mutation 或恢复模型的重大取舍；
- 难以回退的仓库或部署架构。

不适合 ADR：

- 普通 bug fix、局部重构或可直接从代码看出的实现；
- 临时计划、任务进度或 review 记录；
- 为已经完成的历史选择补写理由。

## 格式

文件名使用 `NNNN-short-title.md`，内容保持简短：

```text
# NNNN: 标题

状态：Proposed | Accepted | Superseded
日期：YYYY-MM-DD

## 背景
## 决策
## 后果
```

ADR 解释选择理由，不复制产品规范。若决策改变用户行为，必须同时更新
[`../spec/`](../spec/) 中对应的唯一 owner。

## 记录

- [0001: Convergence V3 使用单一状态转换模型](0001-convergence-v3.md)（Accepted）
