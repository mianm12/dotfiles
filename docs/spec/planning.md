# 观察与规划

## Actual filesystem

Target 使用 `lstat` 区分 absent、symlink、regular file、directory 和 special。Local 只关心
目录项是否存在，不读取或跟随已有对象。

## 通用决策规则

选定 scope 内任一 placement 在规划阶段落到 conflict 时，plan 不可执行。Status 的公开投影
与退出码只由 [`cli.md`](cli.md#status-与-dry-run) 定义。执行前后的写入边界见
[`mutation-and-recovery.md`](mutation-and-recovery.md)。执行阶段 update/prune 前的
resolved/raw 复核失败属于 mutation 中途的安全停止。

State 中 placement 的 kind 与当前 desired 的 kind 不一致（同一 ID 在 link 与 local 之间互换）
是 conflict，不尝试自动收敛；恢复方式是改用新 placement ID，或先 `dot remove` 该 module 再
修改 manifest。

Control topology 自身无效时整条规划失败，不能使用 stale 宽容规则绕过。Active target 与
control family 重叠仍是 path conflict。仅当 state placement 已退出 desired，且其历史 target
与当前 control family 重叠时，`dot` 生成 warning 与 `forget`：放弃 ownership/provenance，
禁止 prune，不删除、替换或以其他 placement action 修改对应 target。该规则同时适用于
stale link 与 stale local，只处理当前命令 scope 内的记录；损坏 state、HOME 不匹配和未知 kind
等输入错误不得降级为 forget。

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
Stale link target 在规范化或解析后关系中严格包含 active desired target 时为 conflict，禁止
删除可能承载 active target 的父 symlink。该判定复用
[`placements.md`](placements.md#路径身份与边界) 的同一 target 关系。

Stale link 不满足该守卫时（target 已变成普通文件、目录或 special，raw destination 漂移，或
resolved target 改变）不是 conflict：用户已接管该 target，`dot` 警告并 forget 对应 state
记录、放弃 ownership，不阻塞本轮其余收敛。

该宽容规则仅适用于 stale placement——`dot` 对它唯一想做的动作是删除，放弃删除不触碰任何
用户数据；active placement 的漂移仍按上文判定为 conflict。

## Local

| Actual | 行为 |
| --- | --- |
| absent | 从 example 以 `0600`、完整且不可覆盖的方式 create |
| 任意已存在目录项 | keep；不读取、不比较、不分类、不覆盖 |

Example 更新不触发 local 更新；local 被用户删除后下一次 apply 重新创建。Local 退出 desired
时永不删除；若 state 有记录则提示一次并忘记 provenance。Remove/prune 永不删除 local。
