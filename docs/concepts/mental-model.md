# `dot` 的工作模型

理解 `dot` 最有效的方法不是先背命令，而是把它看成一个收敛器：它比较“这台机器应该是什么
样子”和“现在实际是什么样子”，打出几行将要做或不会做的事，并在 `apply` 时按同一条循环动手。

本页是解释层。精确产品行为由[产品规范](../spec/README.md)拥有。

## 四类输入，一条循环

```mermaid
flowchart LR
    R["Repository desired\ndot.toml + modules + sources"]
    M["Machine selection\nprofiles + extra modules"]
    S["Ownership state\n过去写下的 target 和 dest"]
    F["Actual filesystem\n当前 target 事实"]
    L["同一条循环\nlink / replace / skip ..."]
    R --> L
    M --> L
    S --> L
    F --> L
    L --> O["status / dry-run\n只读，不加锁"]
    L --> A["apply\n持锁后再跑一遍并动手"]
```

- **Repository desired** 描述可选择的 modules、placements 和 source，是用户维护的期望来源。
- **Machine selection** 描述当前机器启用了哪些 profiles 和直接 modules，不等于已经完成部署。
- **Ownership state** 是缓存：某个词法路径上我们曾经写过哪个 dest。磁盘才是真理。
- **Actual filesystem** 是观察时 `lstat` 到的叶子。已有普通文件、目录和别人的 symlink 不会
  因为 manifest 想要同一路径就自动变成 `dot` 所有。
- **循环** 把上述输入收成几行。`status` / dry-run 打印它；`apply` 持锁后再算一遍，有
  `skip` 就整批不写。

规范中的完整输入和循环规则分别见
[selection](../spec/selection.md)、[modules and platforms](../spec/modules-and-platforms.md)、
[placements](../spec/placements.md)、
[state and ownership](../spec/state-and-ownership.md) 与 [planning](../spec/planning.md)。

## Selection 与 convergence 是两步

`init`、`select add` 和 `select remove` 改变的是 machine selection；它们不会顺手修改 target。
`apply` 读取当前完整 selection，统一观察所有 effective modules，并处理已经不再 active 的历史
ownership。

把两步分开有两个直接结果：

1. 你可以先明确“想启用什么”，再通过 `status` 或 dry-run 审查文件系统变化；
2. `apply` 不需要猜测某次命令只想处理哪个 module，它始终让整台机器回到一个一致状态。

例如默认 profile 的首次流程是：

```text
init -> default profile selects starship
  -> starship is desired but target is pending
  -> apply
  -> selection, state and filesystem agree
```

命令级契约见 [CLI 规范](../spec/cli.md)。

## State 是证据，不是授权捷径

Desired 只能说明“现在希望这里有什么”，不能证明“现在这里的东西是谁创建的”。State 保存
过去写下的词法 target 和 dest，让 `dot` 能区分自己拥有且 dest 仍匹配的 link，与不应触碰的
用户数据。

因此：

- 删除 manifest 条目不会自动赋予删除任意 target 的权限；
- state 丢失后仍可创建当前 desired，但可能失去发现历史 link 的能力；
- dest 对不上时只丢账，不覆盖用户数据。

需要从用户角度理解这些边界，继续阅读
[所有权与安全边界](ownership-and-safety.md)；持久格式和精确判定只在
[state 规范](../spec/state-and-ownership.md)中定义。

## 收敛不变量

一次成功 apply 后，在 repository、machine selection、state 和实际文件系统都不变化的条件下，
再次 apply 必须是 no-op。这一性质让“重跑完整命令”成为部分失败后的主要恢复方法，而不需要
隐藏 rollback 或逐 module 补偿流程。控制文件权限修复算一次变更，之后才是 no-op。

Apply 仍可能在操作系统错误或外部并发修改下部分完成。锁、执行顺序、提交边界和中断恢复由
[mutation and recovery 规范](../spec/mutation-and-recovery.md)定义。

## 不在这个模型里的事情

`dot` 管理配置文件的选择与放置，不负责安装软件、运行 hooks、渲染模板、管理秘密、同步 Git
或提供 backup/rollback。完整范围和明确接受的风险见[产品定义](../spec/product.md)。

遇到问题时，先按四类输入定位：

1. Repository desired 是否写对？
2. 当前 machine selection 是否真的包含该 module？
3. State 是否存在、有效并与当前 HOME 对应？
4. Actual filesystem 是否有冲突或漂移？

这比从某一条错误文本反推内部函数更接近系统真正的判断方式。
