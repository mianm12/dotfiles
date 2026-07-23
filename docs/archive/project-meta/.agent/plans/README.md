# ExecPlan 目录

> [!WARNING]
> 历史工程记录，非当前规范或工作流程。

ExecPlan 的内容和执行规则见 `.agent/PLANS.md`。本文件只规定计划文件的存储生命周期。对已
纳入 Git 的计划，最近一次成功 commit 记录的目录位置是生命周期状态的单一真相源；新建但
尚未提交的计划必须位于 `active/`。工作树中尚未提交的 `active/` → `completed/` 迁移是
closure-in-progress，反向迁移是 reopen-in-progress；两者都不改变最近一次成功状态。

- `active/`：正在编写、等待实施、执行中或因 review/CI 重新开启的计划。文件位于此目录不
  代表已经获得实施授权。
- `completed/`：目标、验证和要求的独立复核均已完成，且 `Outcomes and Handoff` 已收口的
  计划。保留这些文件用于追溯决策，不再从中领取待办。

既有 `completed/` 计划无需为适配新模板或章节名称而机械改写；若同一 Goal 在合并前重新
开启，则从 reopen 开始遵循当前约定。

恢复任务时，只有同时满足以下条件，才能确认尚未提交的 closure-in-progress 或
reopen-in-progress 仍属于同一计划：

- 当前 HEAD 中的计划路径仍处于迁移来源状态。
- worktree 和 index 中的迁移可唯一归属于同一计划的状态更新与路径变更，未混入实现改动、
  其他计划或任务外内容。

确认产物归属后，只有用户当前任务明确授权继续迁移及相应 stage/commit，或明确授权恢复，
才能为对应操作接管该迁移。对恢复任务开始时已有的 staged 内容，只能将其中已确认属于该
pending transition 的部分纳入对应 commit，其他既有 staged 内容必须保持不变；继续授权允许
stage 并提交其余已确认属于该 transition 的 worktree 内容，恢复授权只允许执行恢复。识别
迁移或读取 handoff 不产生或延续授权。

需要保存的新计划直接创建在 `active/`，文件名使用 `<milestone>-<topic>.md`。完成全部验收，
但计划状态迁移或用于收口的 stage、commit 尚未获得当前任务授权时，文件保持在 `active/`，
`Progress` 标记为等待授权，并立即请求必要的最小操作；不得声称已经 completed。授权应明确
覆盖从 `active/` 到 `completed/` 的迁移及必要的失败恢复，计划本身不能为当前或后续任务
提供授权。

要求的独立复核不可用或尚未完成时，计划保持在 `active/`，`Progress` 记录等待复核或收口
受阻；不得开始迁移、stage 终态收口、创建 plan-closure commit 或声称 review-ready。

满足全部收口条件并获得授权后，将同一文件移入 `completed/` 并纳入 plan-closure commit，
不要复制成两份，也不要只改正文中的状态文字。只有该 commit 成功才建立最终的 `completed/`
状态。

迁移期间的 diff check、stage、commit 或 runtime approval 失败时，最近一次成功 lifecycle
状态仍是事实来源，不得声称已经进入目标状态。若能在既有授权和相同交付标准内安全修正并
重试，且不会混入其他改动，可以保留当前 pending transition；否则在授权范围内把当前计划的
路径和 index 恢复到迁移前状态。缺少恢复授权或无法安全继续时，记录迁移受阻并立即请求所需
授权。

同一 Goal 在合并前因人工 review 或 CI 需要重新开启时，先确认当前任务已经授权状态迁移、
stage、plan-reopen commit 及必要的失败恢复；未授权则先请求。获得授权后，将原文件移回
`active/`，在 `Progress` 记录原因，并在修改实现前创建 plan-reopen commit。该 commit 成功
才建立 active 状态，之后按 `.agent/PLANS.md` 完成修复、验证、fix commit 和复核。

plan-reopen commit 前中断或失败时，按上述 pending transition 规则继续或恢复。已经合并的
工作出现新缺陷时，通常新建 fix 计划，不在历史 completed 计划中追加待办。
