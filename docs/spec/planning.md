# 观察与规划

`status`、`--dry-run` 和 `apply` 使用同一条收敛循环。循环产出一组行，不是 Plan、Action 或
Issue 类型。公开投影由 [`cli.md`](cli.md#status-与-dry-run) 定义；谁持锁、何时动手由
[`mutation-and-recovery.md`](mutation-and-recovery.md) 定义。

## Actual filesystem

Target 使用 `lstat` 词法路径，区分 absent、symlink、regular file、directory 和 special。不跟随
叶子，也不把祖先 symlink 的解析结果记入身份。Local 只关心一次 `lstat` 能否确认目录项 absent
或 present，不读取或跟随已有对象；若 `lstat` 因祖先不是目录而返回 `ENOTDIR`，则该词法叶子为
unreachable，不得冒充 present。

## 循环模型

一次完整观察读取：全部 effective desired placements、全部 state records、以及它们词法路径上
的 actual。Desired 不完整的 module 也不跳过 owned actual 观察；任何必要路径读不了仍使分析
失败。然后为每个 desired 和每条不再 desired 的账本记录标一行，或保持沉默。

无变化的 link/local 不产出行。`record` 只在叶子已正确、账本缺失或字段不对时出现。

行的种类由 [`cli.md`](cli.md#status-与-dry-run) 拥有：`link`、`file`、`replace`、`remove`、
`record`、`forget`、`chmod`、`skip`。本文件拥有何时标哪一行。

动手顺序（仅 `apply` 且没有任何 `skip` 时）固定为：

1. `chmod`；
2. `link`、`file`；
3. `replace`；
4. `remove`；
5. 原子提交 state；提交成功后 `record`、`forget` 才算完成。

每个阶段内按 module ID、placement ID、target 稳定排序。父目录准备、删除前再读 dest、state
提交是内部步骤，不单独成行。

## 通用决策规则

- 全部 effective desired 与 state-only stale records 进入同一次观察。Profile 选中且已确定
  not-applicable 的 module 退出 desired，其旧 links 按 stale 规则处理。Missing extra-selected、
  indeterminate 或 extra not-applicable module 使该 module 的 desired 不完整：为该 module 标一条
  `skip`，整批不写；其已有 state records 本轮继续参与词法冲突判断，但不得被当作 stale 标
  `remove` 或 `forget`。其他能够完整观察的 module 仍标出自己的行。
- 三个控制前缀互相重叠时无法形成循环，见
  [`placements.md`](placements.md#control-path-topology)。这是分析失败，不是 `skip`。
- Active target 与任一控制前缀词法重叠 → 该 target `skip`。
- 任意不同完整 key 的 state 已占用同一词法 target → `skip`，不自动 transfer；同 module 的不同
  placement 也不例外。
- 两个 active desired 词法路径相等或互为祖先/后代 → 两条都 `skip`。
- 同 key 的 link 与 desired local 互相转换 → `skip`。不能在一次 desired 变更中隐式改类型。
  Local 改成 link 没有 ownership 证据，按普通 link 规则看 actual；已有 local 文件会 `skip`。
- 任意 stale ownership target 与 active desired target 相等或互为祖先/后代 → 两边都 `skip`，
  不得在同一批里先 `forget` 再走进旧目录 link，必须先完成两阶段迁移的阶段一。
- 同 key 的 target 搬家：新旧词法路径没有相等或嵌套时，新路径按 desired 标 `link` /
  `record` / `replace`，旧路径按 stale 标 `remove` 或 `forget`。存在相等或嵌套时两条都
  `skip`。旧记录只有 dest 仍匹配时才能 `remove`；提交后的账本只保留新 target。
- 不比较 resolved 身份，不因祖先 symlink、alias 或“走进托管目录链接”建立拓扑关系。
- 有任何 `skip` 时整批不执行 `chmod`，也不修改 target、parent 或 state。能够观察的部分仍
  标出行，不伪造局部可执行子集。

## Link

Active link 按以下顺序判定，命中即停：

1. 任意不同完整 key 的 state 已占用同一词法 target → `skip`。
2. actual 是 regular file、directory 或 special → `skip`。
3. actual absent → `link`。
4. actual 是 symlink 且 raw destination 等于 desired dest：
   - 无同 key 记录，或记录的 target/dest 与当前不一致 → `record`；
   - 记录已一致 → 无行。
5. actual 是 symlink 且 raw destination 不等于 desired dest：
   - 同 key 记录的 dest 等于当前 raw destination → `replace`；
   - 无记录，或 actual 已偏离记录的 dest → `skip`。

`record` 只改账本。`replace` 删除旧链接前必须再读 raw destination，与观察时不一致则按中途
失败停止，见 [`mutation-and-recovery.md`](mutation-and-recovery.md#安全规则)。

### Stale link

Stale link 只有同时满足以下条件才标 `remove`：

- actual 仍是 symlink；
- raw destination 与账本 dest 相同；
- 词法 target 不落在任一控制前缀上，且与任何 active desired target 不相等也不嵌套。

Dangling symlink 仍按 raw destination 应用同一规则。每个 `remove` 在动手前独立再读 dest。

以下情况标 `forget`，丢账，不删除、替换或跟随 actual：

- target absent；
- actual 已变成普通文件、目录、special，或 raw destination 与账本 dest 不同；
- 词法 target 与控制前缀重叠。

Stale target 与 active desired 相等或嵌套不是 `forget`：两边都标 `skip`，由上一节的两阶段规则
处理。合法 v5 state 已拒绝重复或嵌套 target，因此循环不再为多条冲突 stale records 猜测 owner。

Missing extra-selected、indeterminate 或 extra not-applicable 已使 module 的 desired 不完整时，该
module 的 state records 不是本轮 stale 输入：actual 仍属于必要观察；观察成功后，记录除参与与
其他 desired 的词法冲突判断外保持沉默，由 module 级 `skip` 表达阻断。Profile 引用 missing
module 是配置错误；profile-only not-applicable 不适用此冻结规则。

`forget` 的 reason 必须说明为什么不动文件。不另产 warning 行。普通 drift/absent `forget`
不阻止其他独立行进入同一份观察；但整份观察里只要有 `skip`，这些 `forget` 也不会被执行。

该宽容只适用于已经退出 desired 的 link。Active placement 的不确定事实仍标 `skip` 并整批停。

## Placement 类型与层级迁移

Link/local 类型转换，以及「目录 link」换成其词法子孙上的 leaf placements，必须使用两个
desired 阶段：

1. 阶段一只移除旧 placement，不加入 replacement/descendants；重复 apply，直到旧账本已
   `remove` 或 `forget` 并成功提交 state。
2. 用户处理 `forget` 后仍会挡住新 placement 的 actual，再加入 replacement/descendants 并
   运行阶段二 apply。

一次 desired 里同时保留父链接并加入词法子孙会触发嵌套 `skip`。阶段一 actual 已消失不能单独
证明完成；必须成功提交 state。Mutation 失败时保持当前 desired，按
[`mutation-and-recovery.md`](mutation-and-recovery.md#中断恢复) 重跑。每个仍有旧 ownership
的 HOME 都必须分别完成阶段一。详细操作见
[`安全迁移 placements`](../guides/safe-migrations.md)。

## Local

| Actual | 行为 |
| --- | --- |
| absent | `file`：从 example 拷贝 |
| 任意已存在目录项 | 无行；不读取、不比较、不分类、不覆盖 |
| unreachable（祖先不是目录） | `skip`：无法确认词法叶子 absent 或 present |

Example 更新不触发 local 更新；local 被用户删除后下一次 apply 重新创建。Local 不进入 state，
退出 desired 时没有账本行，也永不由 `remove` 删除。
