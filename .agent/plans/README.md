# ExecPlan 目录

ExecPlan 按执行状态存放，目录位置是状态的单一真相源：

- `active/`：已经接受、正在执行的计划。实施期间持续更新 `Progress`、`Decision Log` 和
  `Surprises & Discoveries`。
- `completed/`：目标与验证均已完成，且 `Outcomes & Retrospective` 已收口的计划。保留这些
  文件用于追溯决策，不再从中领取待办。

新计划直接创建在 `active/`，文件名使用 `<milestone>-<topic>.md`。完成全部验收后，用
`git mv` 将同一文件移入 `completed/`；不要复制成两份，也不要只改正文中的状态文字。
