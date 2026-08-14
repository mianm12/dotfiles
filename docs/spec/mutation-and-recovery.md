# Mutation、锁与恢复

## 执行顺序

真实 artifact convergence 使用 lock-first，并在锁内跑与 status / dry-run 相同的循环：

```text
normalize invocation paths and validate lock acquisition boundary
  -> ensure private state root and lock; acquire advisory lock
  -> strict load machine/repository/state 与全部 effective manifests
  -> resolve selection, check control prefixes and desired
  -> observe actual and emit the same loop lines as status/dry-run
  -> any skip: print the lines, write no business mutation, release lock
  -> chmod controls, then apply target lines in planning order
  -> atomically commit the resulting ownership state
  -> only now complete record / forget rows
  -> release mutation lock
```

锁前不观察 placement，不保存预检结果。锁内加载、解析、观察是真实 mutation 唯一的权威判断。
`status` 与 `--dry-run` 不加锁，调用同一循环函数；其快照不是 apply 的 lease。

## Lock acquisition boundary

获取 lock 前只允许执行纯参数校验与安全取锁所需的只读路径检查：

- HOME、machine config、state 和 lock 必须是无 NUL、有效 UTF-8 的绝对路径并做词法规范化；
- machine config 必须是 config 根的直接子项；state 与 lock 必须是同一 state 根下两个不同的
  直接 sibling；
- config 根必须是直接真实目录；state 根若存在也必须是直接真实目录（均不得是 symlink）；
  现存 control file 若存在必须是
  直接 regular file；
- config 根与 state 根词法上不得相等或互为祖先/后代。

该阶段不读取 machine/repository/manifest/state 内容，不解析 selection/platform，不观察
placement target，也不解析控制路径的 ancestor/entry/resolved 交叉身份。失败时不得创建、
chmod 或修改 root、lock、config、state、parent、target 或 temporary file。

边界通过后只按需创建缺失的私有 state root/lock 并获取 advisory lock；不得在观察 `skip` 前
chmod 已存在的 root 或 lock。根目录 `0700`、控制文件 `0600` 是不变量：权限不对时由同一循环
产生显式 `chmod` 行，这一次算变更。Machine 中的 repository、三个控制
前缀、state 内容、selection、platform、source、desired 与 ownership 均在锁内权威观察中验证。
由此发现的 `skip` 可以留下已创建的私有 state root/lock bookkeeping，但不得修改 machine
config、state、placement parent、target 或 local temporary file。

## 安全规则

- 配置、manifest 与全量收敛集合的严格加载由
  [`cli.md`](cli.md#全量收敛与加载) 定义。
- [`modules-and-platforms.md`](modules-and-platforms.md#applicability) 定义的任意 effective
  indeterminate 和任意 extra not-applicable 都使 desired 不完整，标 `skip` 并整批不写；
  profile not-applicable 的 stale 清理由 [`planning.md`](planning.md#通用决策规则) 定义。
- 一次 mutation 从 acquire 到 release 使用同一个固定 HOME、config、state 和 lock 路径，只获取
  和释放一次 lock。Machine repository 在锁内加载后固定到本次观察与执行。
- 有任何 `skip` 的 apply 是完整、未开始业务 mutation 的结果，不是运行时 error。它不得 chmod
  已存在的 control，也不得写 target 或提交 state；公开输出与退出码由
  [`cli.md`](cli.md#status-与-dry-run) 定义。
- Executor 只按 [`planning.md`](planning.md#循环模型) 的顺序消费锁内刚算出的行，不重新扫描、
  分组或建立第二份清单。每行成功后折叠其 ownership effect；失败时丢弃尚未提交的候选账本。
  父目录准备是 `link` / `file` 的内部步骤。
- 阶段必须满足：全部 `chmod` 成功后才进入 target mutation；全部 `link` / `file` 成功后才进入
  `replace`；全部 `link` / `file` / `replace` 成功后才进入 `remove`。`record` 与 `forget` 不直接
  修改 target，且只有 state commit 成功后才属于完成行。
- Local 以 `0600`、内容完整且不可覆盖的方式发布；新 link 以不可覆盖的 symlink create 发布；
  target 已出现时停止。
- `replace` / `remove` 删除 symlink 前重新读取 raw destination；与该行观察时的 dest 不同则
  停止。不比较 resolved 父路径。删除后重新读取 target，只有确认不存在才继续。
- 动手使用观察时的词法路径；祖先 symlink 由内核跟随。不在创建后核验 resolved 身份。
- State 最后原子提交。提交内容必须恰好等于全部 active desired links。State missing 时，即使
  没有 active links，也在成功 apply 中提交合法空 v5 state；相同输入的后续 apply 不得重写
  （控制文件权限修复除外）。
- State commit 与 lock release 均成功才构成 mutation 成功。配置无法加载、lock/I/O、执行、
  commit 或 release 失败走 error。中途失败只投影已经完成的持久行；原子发布可能已完成但后续
  cleanup 失败时，错误必须保留 `may_have_changed=true`，不得断言 state 未提交。
- 不提供 rollback。失败时保留已经完成的安全步骤。Core failure 只携带 cause、可选失败行与
  `may_have_changed`，不拥有 Recovery taxonomy；CLI 根据具体错误在最外层提示归档、升级或重跑。
  Lock release 失败时：若本次没有 control/target/state 变更，不得把结果说成业务数据已部分改变；
  若已有变更，提示重跑。
- Mutation commands 使用同一把稳定、non-blocking advisory file lock。Lock busy 是普通运行时
  失败。产品不防御同一用户权限的其他进程在检查与 syscall 之间并发替换对象。
- 不建立通用 snapshot/precondition/postcondition framework。删除前只复核 raw destination。

## Selection mutation

`init`、`select add` 与 `select remove` 使用同一 lock-first 边界：纯参数与安全取锁校验 →
acquire 一次 advisory lock → 锁内一次加载/解析/决策 → 原子发布 machine config → release。
它们不执行锁前 repository/selection 观察，不比较 fingerprint，不读取 state，也不观察或修改
target。内容变化时原子发布的新 machine config 自带 `0600`；内容相同不借发布函数隐藏 chmod，
控制权限由 artifact convergence 的显式 `chmod` 行收敛。

并发的 `dot` selection/artifact mutations 由同一 lock 串行化；后取得 lock 的操作读取最新
machine config 并据此作出自己的完整决定。Selection publication 成功后仍由后续 `dot apply`
收敛 target，不属于 artifact mutation 的部分完成状态。

## 中断恢复

必须能通过重跑处理：

| 中断后事实 | 下一次 apply |
| --- | --- |
| link 已创建、state 未提交 | `record` 后提交 ownership |
| link 已更新、state 仍是旧 dest | `record` 后提交 ownership |
| replace 删除旧 link 后中断 | `link` 当前 desired |
| remove 已完成、state 仍有记录 | `forget` stale ownership |
| local 已完整发布、空 v5 state 未提交 | local 无行，并提交当前 link-only state |

用户看到中途失败后必须重新运行完整 `dot apply`，不能手工模拟剩余内部步骤。重复收敛从新的
repository、selection、state 与实际文件系统事实重新观察。
