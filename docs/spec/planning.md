# 观察与规划

## Actual filesystem

Target 使用 `lstat` 区分 absent、symlink、regular file、directory 和 special。Local 只关心
目录项是否存在，不读取或跟随已有对象。

## 通用决策规则

选定 scope 内任一 placement 在规划阶段落到 conflict 时，plan 不可执行。Status 的公开投影
与退出码只由 [`cli.md`](cli.md#status-与-dry-run) 定义。执行前后的写入边界见
[`mutation-and-recovery.md`](mutation-and-recovery.md)。

State 中 placement 的 kind 与当前 desired 的 kind 不一致（同一 ID 在 link 与 local 之间互换）
是 conflict，不尝试自动收敛。改用新 placement ID 仍受 actual target 与 ownership 规则约束，
不是通用的类型转换方式；显式转换使用下文的[两阶段 placement 迁移](#两阶段-placement-迁移)。

Active target 的父路径解析链经过一条仍有完整 state ownership 证据的 link 时为 conflict。
该守卫包括 scope 外和仅存在于 state 的 link，避免 active action 先写入其当前 destination，
而该 link 随后更新或清理后让成功结果失去可达性。只有解析链实际经过该 link 目录项才命中；
独立 alias 即使最终解析到同一 destination 也不冲突。State target 的 resolved identity 或
actual raw destination 已漂移时 ownership 不成立，不使用该守卫。

Scope 内非 conflict active link action 若会改变 link 目录项，或当前 state 尚不能完整 owns
该 actual link，则成功后会建立或刷新 ownership。若任一 effective desired target 的当前父
路径解析链经过该 active link 目录项，该 action 为 conflict；比较必须包含 scope 外 desired。
该规则既避免 Adopt、RepairState 或 resolved-drift Keep 首次成功后让相同输入在下一次 apply
才冲突，也避免 scoped Update 切断 scope 外 desired target 的当前可达性。当前 state 已完整
owns 同一 resolved link entry 与 raw destination 的 Keep 不改变 namespace 或建立新 ownership
边界，不使用该 prospective guard；这包括 recorded lexical target 与 desired 不同、但旧
target 仍能证明同一 ownership 的 rebind。CreateLink 的 actual 尚不存在，因此当前解析链不会
经过该目录项。该 guard 只把本轮 scope 内 active link action 视为 parent；scope 外且没有完整
state ownership 的 desired link 不因本规则获得 ownership。

Control topology 自身无效时整条规划失败，不能使用 stale 宽容规则绕过。Active target 与
control family 重叠仍是 path conflict。仅当 state placement 已退出 desired，且其历史 target
与当前 control family 重叠时，`dot` 生成带结构化原因的 `forget`：放弃
ownership/provenance，禁止 prune，不删除、替换或以其他 placement action 修改对应 target。
该 action 是 ownership 放弃原因的唯一真相源，具体的只读预告与成功结果文案由
[`cli.md`](cli.md#status-与-dry-run) 投影。该规则同时适用于 stale link 与 stale local，只处理
当前命令 scope 内的记录；损坏 state、HOME 不匹配和未知 kind 等输入错误不得降级为 forget。

Profile 选中的 module 已确定 not-applicable 时，其旧 state placements 视为退出 desired，
继续按本文件的 stale prune/forget 规则规划 cleanup。Prospective effective indeterminate
module 不生成 placement 或 cleanup action；已由 remove 移出 extra selection 的目标不再
effective，其旧 state placements 仍按 stale prune/forget 规则处理。真实 mutation 阻断边界由
[`mutation-and-recovery.md`](mutation-and-recovery.md#安全规则) 定义。

### 两阶段 placement 迁移

同一 placement ID 在 link 与 local 之间变更 kind，或把一个 directory link 改为其 target 下的
leaf placements，都不能在一次 desired 变更中自动迁移。每个受影响 HOME 必须分别完成：

1. 阶段一的 desired 只删除旧 placement，不加入 replacement。Directory-link 迁移还要求所有
   effective modules 都不声明父路径解析链经过旧 link 的 descendants。
2. 运行覆盖旧 state record scope 的 `dot apply`；module 仍 active 且 applicable 时可以使用
   `dot apply MODULE`。若 mutation 失败或提示可能部分完成，保持阶段一的 desired，并按
   [`mutation-and-recovery.md`](mutation-and-recovery.md#中断恢复) 重跑到成功；不能仅凭 actual
   target 已消失推断 state 已提交。
3. 检查旧 target。Prune 只删除仍匹配 ownership 的 link；forget 和 local cleanup 都保留 actual。
   进入阶段二前，用户必须先安全处理保留的数据或目录项。尤其 local 转 link 时，仍存在的 local
   会让新 link conflict；link 转 local 时，阶段一保留的任意 actual 都会被 local 当作已有目录项
   keep；directory-link 迁移时，旧 parent target 必须 absent 或是用户确认的真实目录。
4. 阶段二再加入 replacement 或 descendants 并 apply；普通 target、ownership 与 conflict 规则
   继续适用。

若 module 已因 selection 移除或已确定 not-applicable 而退出 desired，按
[`cli.md`](cli.md#apply) 与 [`cli.md`](cli.md#remove) 的 selection 来源规则选择全量 cleanup
或 remove，不用显式 apply 把 inactive module 重新加入 extra selection。Indeterminate
applicability 不表示退出 desired，仍按安全规则阻断；应先恢复平台判定或移除对应 selection
来源，仅由 extra 选择时才可使用 remove contraction。多机仓库中每个仍有旧 record 的 HOME
都不得跳过阶段一。

## Link

按以下顺序判定，命中即停：能用 desired 或 state 解释的 actual 才有动作，其余一律 conflict。

1. 其他 module 的 state 已 owns 同一 target → conflict。
2. actual 是 regular file、directory 或 special → conflict。
3. actual absent → 无 state 时 create 并登记；有 state 时按当前 desired create。
4. actual 是 symlink 且 raw destination == desired：
   - 无 state → adopt，只写 state。
   - 有 state 且 state destination == desired → keep（记录的 resolved target 已变则一并修正）。
   - 有 state 且 state destination != desired（state 落后）→ repair state。
5. actual 是 symlink 且 raw destination != desired：
   - 有 state、raw destination == state destination 且 resolved target 未变（仅 desired 改变）→
     update。
   - 有 state、raw destination == state destination 但 resolved target 已变 → 拒绝并按 conflict
     处理。
   - 其余（无 state 的未知 symlink，或已偏离 state）→ conflict。

Stale link 只有在当前 target 仍是 symlink、resolved target 未改变且 raw destination 等于
state 记录时才允许 prune。Dangling symlink 仍按 raw destination 应用同样规则。

Stale link target 与 active desired target 相等时，stale cleanup action 为 forget 旧
ownership；该 action 不覆盖 active placement 按上文规则独立产生的 ownership conflict。
Active target 的父路径解析链经过 stale link 时，由通用 state-owned link 守卫把 active
placement 标记为 conflict；stale cleanup 也必须比较全部 effective desired targets，即使命令
scope 不包含对应 child。只要 child 的父路径解析链仍经过该 link，prune action 同样为
conflict，避免 scoped cleanup 切断 scope 外的 desired target。两种 action 投影复用同一个
traversal 与 ownership 不变量。

同一 plan 内本可 prune 的 state-owned stale link，如果其当前父路径解析链经过一条将在此前
执行 Update 的 active link，则 stale cleanup 为 conflict。否则 Update 会先改变 namespace，
让后续 prune 的 resolved ownership 复核被本轮自身必然破坏，并留下部分完成。该规则只比较
当前 scope 实际生成的 Update/Prune action pair；独立 alias 即使到达相同 destination 也不
命中，scope 外未生成 cleanup action 的 stale record 不阻断 scoped Update，之后若发生 resolved
drift 仍按下文 forget。恢复时先保持 parent 的旧 destination 并完成 stale cleanup，再更新
parent；跨 module 可以在命令 scope 允许时先做 scoped cleanup/remove，同一 module 则分两次
desired 变更。

当前 scope 内其余 Prune action 按实际 traversal 依赖排序：child target 的父路径解析链经过
另一条 stale parent link 时，child 先于 parent；无依赖的 action 保持稳定 state key 顺序，
独立 alias 不建立依赖。多个完整 ownership record 若指向同一个当前 target，只由稳定顺序中的
第一条 action 代表物理 Prune，其余生成说明代表 action 的 forget；不同 target 即使 raw
destination 相同仍分别 Prune。该归一化不增加删除授权，每条实际 Prune 仍独立携带并在执行前
复核自己的 resolved target 与 raw destination。

Stale link 不满足该守卫时（target 已变成普通文件、目录或 special，raw destination 漂移，或
resolved target 改变）不是 conflict：用户已接管该 target，`dot` 生成说明事实原因的 forget
action，放弃对应 state ownership，不阻塞本轮其余收敛。

该宽容规则仅适用于 stale placement——`dot` 对它唯一想做的动作是删除，放弃删除不触碰任何
用户数据；active placement 的漂移仍按上文判定为 conflict。

## Local

| Actual | 行为 |
| --- | --- |
| absent | 从 example create |
| 任意已存在目录项 | keep；不读取、不比较、不分类、不覆盖 |

Example 更新不触发 local 更新；local 被用户删除后下一次 apply 重新创建。Local 退出 desired
时永不删除；若 state 有记录则生成带原因的 forget action，只忘记 provenance。Remove/prune
永不删除 local。
