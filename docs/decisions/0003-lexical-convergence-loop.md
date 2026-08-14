# 0003: 词法身份与单一收敛循环

状态：Accepted
日期：2026-08-14

## 背景

Convergence V4 删掉了 Transition / schedule / FinalState 的多重真相，但把 HOME 建成了路径
类型检查器：target 同时比较 lexical 与 resolved，控制区做三套表示交叉，账本再持久化
`ResolvedTarget`，规划用关系代数堵搬家留下的洞。产品原则写着个人工具、接受低概率边缘风险、
不对抗恶意输入、不协调他人同时改文件；实现却为祖先 symlink 和 alias 穿越预建了编译器。

审查指出的大部分复杂度来自这个产品选择，不是实现手滑。继续在 V4 上补 `os.Root`、Layout
或 Outcome 类型，只会把敌对命名空间模型修得更完整。

## 决策

- Target 身份只认规范化 HOME-relative 词法路径；绝对路径只在固定 HOME 下派生。碰撞、嵌套
  只比这一条字符串，控制越界比较其绝对派生值。祖先 symlink 由内核在 syscall 时跟随；换绑
  和「两条写法同一块盘」是已接受风险。
- 控制区是仓库、config 根、state 根三个词法前缀。根必须是真目录。不做 family、不做
  lexical/entry/resolved 交叉。
- `status`、`--dry-run`、`apply` 使用同一条观察循环。只读不加锁；apply 持锁后再跑一遍。
  有任何 `skip` 则整批不写。中途失败只报已经发生的事。
- 用户可见的行是 `link` / `file` / `replace` / `remove` / `record` / `forget` / `chmod` / `skip`。没有
  Plan、Issue、七个 Decision 作为产品词表。`status` 有 `skip` 仍退出 0；dry-run/apply 则非零。
- State 改为 version 5：`{home, links: [{module, placement, target, dest}]}`。Target 是规范
  HOME-relative identity，全部 target 构成 antichain；dest 是所有权证据，磁盘是真理。v1–v4
  归档，不迁移。解码先看 version，只对 v5 严格；重复 JSON 成员交给 `encoding/json`。
- 控制目录/文件 `0700` / `0600` 是不变量，权限偏差以显式 `chmod` 行收敛；任何 `skip` 阻止
  这类修复，缺失 state root/lock 的取锁 bookkeeping 是唯一例外。
- 保留：目录也可以是一条 link、local 拷一次、forget≠remove、lock-first、叶子不覆盖、删除前
  再读 dest、原子提交、成功后再跑 no-op、不协调并发改文件。

产品规则由 `docs/spec/**` 对应 owner 定义。本 ADR 只固定为什么换契约，不固定 Go 类型名。

## 后果

- V4 的关系代数、`ResolvedTarget`、双遍路径身份和 Issue taxonomy 不再是正确实现。I1
  实现切口已经用 HOME-relative identity、state v5 和单一循环替代这些机制。
- 回滚到只认 v4 的二进制时，必须同时恢复归档的旧 state。v5 不提供兼容读取。
- 用户可以把 `~/access` 指到一条托管目录链接，再声明 `~/access/child`；工具不会拦住。词法
  嵌套（`~/.config/app` 与 `~/.config/app/child` 同时 desired）仍整批拒绝。
- 实现必须一次切开身份、循环、state 和 CLI，不提交「规划已词法、执行仍 resolved」的混合体。
