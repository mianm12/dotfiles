# 产品定义

## 目标与设计原则

`dot` 是个人使用的 macOS/Linux dotfiles 管理 CLI。它把分散的配置集中到 Git 仓库，通过
symlink 应用共享配置，通过 local example 初始化不进入 Git 的本机内容。

设计优先级依次是：

1. 不覆盖或删除用户已有数据。
2. 满足多机器、profile、平台差异和重复收敛的核心需求。
3. 保持实现简单，允许明确接受低概率边缘风险。
4. 不为假想的通用性、安全性或未来功能预建框架。

仓库、机器选择、state 和实际文件系统进入同一条收敛循环。`status`、`--dry-run` 和
`apply` 使用同一函数；只有 `apply` 在持锁后再跑一遍并可能动手。

相同输入成功 apply 后再次执行必须是 no-op。权限修复算一次变更，之后才是 no-op。

## 范围

产品包含：

- macOS 与 Linux；Linux 重点支持 Ubuntu 和 Arch。
- profiles、modules、platform variants 和多个 placements。
- 文件或目录 symlink。
- 目标缺失时从 `*.local.example` 初始化本机 local 文件；已存在时不读取或覆盖。
- `init`、`select add`、`select remove`、`status`、`apply`、`paths`、`version` 和 `help`。
- mutation dry-run、link-only ownership state 和单进程锁。

## 非目标

产品不包含：

- 软件、Homebrew、APT、Pacman 或系统包安装。
- HOME 外 target 声明。
- Profile 继承、include、exclude、覆盖或条件 DSL。
- Module 依赖、hook、脚本或任意命令执行。
- Template 渲染、环境变量注入、秘密管理或加密。
- 自动 Git pull、commit、push、release 或自更新。
- 自动导入普通文件、`add` 命令或自动修改仓库 manifest。
- Force、backup、rollback 或跨路径事务。
- Windows、daemon、watch、外部并发协调、JSON CLI 输出或完整 doctor。
- 不同 `(module, placement)` identity 之间的 ownership transfer。

## 明确接受的风险

以下是产品边界，不是等待补齐的欠账：

- Repository、manifest 和本机配置由用户本人维护，不对抗恶意输入。
- 只用一把文件锁避免多个 `dot` mutation 并发；不协调编辑器、软件或用户同时修改文件。
- Mutation 采用 lock-first。安全取锁边界通过后，后续因 `skip` 整批拒绝可以留下私有 state
  root/lock bookkeeping，但不会修改 machine config、state、placement parent 或 target。
- Status 与 dry-run 不加锁；与 apply 并发时是 best-effort 快照。退出码和恢复方式由
  [`cli.md`](cli.md#status-与-dry-run) 定义。
- Target 身份只比词法路径，见 [`placements.md`](placements.md#路径身份与边界)。祖先
  symlink 由内核在 syscall 时跟随。两条不同词法路径落到同一磁盘位置、或观察与动手之间
  祖先被换绑，是已接受的边缘风险。不比较 resolved 身份，不追踪 parent-link traversal，
  不保证识别大小写、Unicode normalization、filesystem alias 或 hard-link inode。
- Source/example 也只做声明路径的词法 containment 和最终叶子类型检查；祖先 symlink、alias
  穿越及观察后换绑同样不属于产品防御边界。
- `dot` 不修改已有普通文件内容。
- 不提供断电事务或完整 durability 保证。原子文件发布用于避免暴露半写配置，不承诺跨多个
  target 的原子性。
- 预检通过后动手中途失败时，可以部分完成；输出只报已经发生的事，恢复方式是停止并重跑。
- State 丢失后可以恢复当前 desired，但无法发现已经从 manifest 删除的历史 link；对应公开
  提示由 [`cli.md`](cli.md#status-与-dry-run) 定义。
- State JSON 重复成员遵循 `encoding/json` 后写获胜；不扫描 UTF-16 代理项。v1–v4 不读取。
- 仓库目录被移动或机器配置指向失效时，`dot` 不自动重新绑定；恢复方式是通过
  [`dot paths`](cli.md#paths) 定位并人工修正机器配置中的 `repository`，或删除机器配置后
  重新 `init`。

## 公共与内部边界

产品目标平台只有 macOS 与 Linux。用户可观察行为由本规范集合拥有；内部 package、类型、
算法、系统调用、测试 fixture 和输出排版由架构、实现与测试决定。

只有真实用户故事、已发生故障或不可接受的数据损失路径可以扩大产品边界。需要增加新的安全
证明、持久化字段或公开行为时，应先说明失败案例、实现成本和不实现的现实后果。
