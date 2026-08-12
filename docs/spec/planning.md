# 观察与规划

## Actual filesystem

Target 使用 `lstat` 区分 absent、symlink、regular file、directory 和 special。Local 只关心目录项
是否存在，不读取或跟随已有对象。

## Plan 模型

一次完整 planning 生成：

- 一串按语义 phase 排列的 Actions，唯一表达可执行变化及公开顺序；
- 结构化 Issues，其中 warning 不阻断执行，任一 blocker 使 plan 不可执行；
- 在完整 active desired set 已确定且 Plan 可执行时，由全部 active desired links 直接计算的完整
  NextState。

Planner 不保存每个 key 的 Transition、Action 索引或独立 execution schedule。NextState 不从 Action
顺序推导；blocked Plan 没有可提交 NextState。无文件系统或 ownership 变化的 placement 不生成 keep
Action。

Action 顺序固定为：

1. `create-local`、`create-link`；
2. `update`；
3. `adopt`、`repair-state`；
4. `prune`、`forget`。

每个 phase 内按 module ID、placement ID、target 稳定排序。Action 表示语义变化，不逐项展示
parent preparation、复核或 state commit 等内部 syscall。Status、dry-run 与 blocked apply 的公开
投影由 [`cli.md`](cli.md#status-与-dry-run)定义；真实执行边界由
[`mutation-and-recovery.md`](mutation-and-recovery.md)定义。

## 通用决策规则

- 全部 effective desired placements 与 state-only stale links 必须进入同一次 planning。Profile 选中
  且已确定 not-applicable 的 module 退出 desired，其旧 links 继续按 stale 规则处理；indeterminate
  module 不表示退出 desired，并产生 blocker。
- Control topology 自身无效时整条 planning 被阻断，不能使用 stale 宽容规则绕过。Active target
  与 control family 重叠同样是 blocker。Stale link 与 control family 重叠时只允许 forget，不能
  prune 或以其他 Action 修改 actual。
- 其他 module 的 state 已 owns 同一 active target 时产生 ownership blocker，不自动 transfer。
- Active desired target 的当前父路径解析链经过任何仍有完整 state ownership 的 managed link
  entry 时产生 topology blocker。普通、不由 `dot` ownership state 管理的 ancestor symlink 继续
  按 [`placements.md`](placements.md#路径身份与边界)允许。
- Adopt、repair-state 或 update 若会建立或改变一个被其他 active/stale target 当前 traversal 的
  managed link namespace，则产生 topology blocker。Planner 不建立跨 placement 调度来规避该
  blocker；用户必须按下文两阶段迁移。
- 同一 link key 的 target move 在新旧 target 没有 equality、alias、ancestry 或 traversal 依赖时
  可以一次规划：先 create/adopt 新 target，再 prune/forget 旧 target，并只在 NextState 保存新
  ownership。存在上述依赖时产生 topology blocker。
- Existing v4 link ownership 与同 key 的当前 desired local 构成 placement-type blocker；不能在
  一次 desired 变更中隐式把 link 转成 local。Local 转成 link 没有 provenance state 可用，仍按
  actual target 的普通 link 规则判断，已有 local 文件会阻断覆盖。

能够可靠加载并观察的独立部分应继续形成 Actions 与 Issues；但 control topology、target set 或
配置错误导致某部分无法可靠规划时，不伪造该部分 Action。完整 Report 表示所有可确定事实和问题
都已表达，不表示 blocker 后仍存在一份局部可执行计划。

## Link

Active link 按以下顺序判定，命中即停：

1. 其他 module 的 state 已 owns 同一 target → blocker。
2. actual 是 regular file、directory 或 special → blocker。
3. actual absent → `create-link`。
4. actual 是 symlink 且 raw destination 等于 desired：
   - 无同 key ownership → `adopt`；
   - ownership destination 落后但 actual 已等于 desired → `repair-state`；
   - ownership、resolved target 与 actual 已一致 → 无 Action。
5. actual 是 symlink 且 raw destination 不等于 desired：
   - 同 key ownership 完整解释 actual，且 resolved target 未漂移 → `update`；
   - ownership 的 resolved target 已漂移 → blocker；
   - 无 ownership 或 actual 已偏离 ownership → blocker。

`adopt` 与 `repair-state` 只改变 NextState；`update` 删除旧 link 前继续携带并复核原 raw destination
与 resolved target。

### Stale link

Stale link 只有同时满足以下条件才允许 `prune`：

- actual 仍是 symlink；
- raw destination 和 resolved target 都与 state ownership 相同；
- target 与所有 active targets、control families 和其他 stale targets 均无 lexical/resolved
  equality、alias、ancestor、descendant 或实际 traversal 关系。

Dangling symlink 仍可按 raw destination 和 resolved identity 应用同一规则。每个实际 prune 在执行
前独立复核自己的 ownership；不同 link 之间不建立 DAG 或 child-first 拓扑排序。

以下情况生成带结构化原因的 `forget`，删除 NextState 中的 ownership，但不删除、替换或跟随
actual：

- target absent；
- actual 已变成普通文件、目录、special 或 raw destination 漂移；
- resolved identity 漂移或路径被现存对象阻断而无法继续证明 ownership；
- target 与 active、control 或其他 stale target 存在上述复杂关系；
- 多条 stale records 指向同一 lexical/resolved target。

复杂关系产生的 forget 必须同时产生 `stale-preserved` warning Issue，明确 actual 被保留并需要
人工迁移；所有参与关系的 stale records 都 forget，不选取某一条代表物理删除。普通 drift/absent
forget 由 Action reason 说明，不阻断其他独立收敛。

该宽容规则仅适用于已经退出 desired 的 link：放弃删除不会触碰用户数据。Active placement 的
不确定事实仍 fail closed。

## Placement 类型与层级迁移

Link/local 类型转换、directory link 与 descendants 的替换，以及 managed-link traversal blocker
必须使用两个 desired 阶段：

1. 阶段一只移除旧 link，不加入 replacement/descendants；重复 apply，直到旧 ownership 已 prune
   或 forget 并成功提交 state。
2. 用户处理被 forget 后保留且会阻塞新 placement 的 actual，再加入 replacement/descendants 并
   运行阶段二 apply。

阶段一 actual 已消失不能单独证明完成；必须成功提交 state。Mutation 失败时保持当前 desired，按
[`mutation-and-recovery.md`](mutation-and-recovery.md#中断恢复)重跑。每个仍有旧 ownership 的
HOME 都必须分别完成阶段一。详细操作见
[`安全迁移 placements`](../guides/safe-migrations.md)。

## Local

| Actual | 行为 |
| --- | --- |
| absent | 生成 `create-local`，从 example 创建 |
| 任意已存在目录项 | 无 Action；不读取、不比较、不分类、不覆盖 |

Example 更新不触发 local 更新；local 被用户删除后下一次 apply 重新创建。Local 不进入 state，退出
desired 时没有 ownership/provenance Action，也永不由 prune 删除。
