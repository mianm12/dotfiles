# Public CLI

```text
dot init [REPOSITORY] [--profile NAME]...
dot select add MODULE
dot select remove MODULE
dot status
dot apply [-n|--dry-run]
dot paths
dot version
dot help [COMMAND]
```

## Paths

- `dot paths` 显示当前 invocation 使用的 HOME、machine config、state 和 mutation lock 路径。
- 输出固定为 `home`、`machine_config`、`state`、`lock` 四条 `key=<absolute path>` 记录，
  路径经过词法规范化，但不解析现存 ancestor symlink。具体目录布局不是跨版本持久格式。
- 命令不要求机器已经 init，不读取或解析 machine config、repository、state、lock 或 platform
  信息，也不检查文件存在性、类型、权限、内容或控制路径拓扑。
- 命令不创建目录、文件、lock 或 temporary file，不执行修复，也不输出 repository 或文件内容。
- 命令不接受位置参数；参数错误返回 `2`。HOME 无法取得、为空或不是绝对路径时返回 `1` 且
  stdout 为空；成功返回 `0`，正常结果只写 stdout。

## Help 与 Version

- `dot help` 显示公开命令；`dot help COMMAND` 显示一个公开命令的帮助。未知 topic 或多余参数
  返回 `2`，命令不读取 repository、machine config、state 或 platform，也不执行 mutation。
- `dot version` 不接受参数，按顺序输出
  `version=<value>`、`commit=<value>`、`build_time=<value>` 三行。未注入构建信息的开发版本
  使用 `dev`、`unknown`、`unknown`。

## Init

- Repository 省略时使用当前目录，并且必须存在有效 `dot.toml`。
- Init 只校验 repository 与 active profiles，然后写入 machine config；不读取 state、不观察或
  修改 target，也不执行首次收敛。
- `--profile` 可重复；省略时选择 `default`，repository 未声明该 profile 时失败。显式传入时只
  选择参数给出的 profiles，不隐式加入 `default`。Profiles 使用集合语义，重复值必须拒绝。
- 参数或锁内校验失败时不写机器配置；安全取锁边界已经通过时可以留下私有 state root/lock
  bookkeeping。成功后提示用户运行 `dot apply` 完成首次收敛。
- 已初始化且规范化 repository 与 profile 集合相同时，`init` 在锁内重新校验 repository/profile
  后成功 no-op，保留已有 `extra_modules`，不读取 state 或 target。任一绑定不同则拒绝；不提供
  reconfigure/rebind。

## 安装与 bootstrap

- `make install` 构建并复制独立的 `dot` 可执行文件；默认目标是 `~/.local/bin/dot`，调用方可用
  绝对 `INSTALL_DIR` 覆盖目录。新建安装目录和最终 binary 使用 `0755`；已有安装目录不改权限。
  Binary 先写同目录临时文件再 rename，不能把仓库内 binary 的 symlink 暴露为安装结果；最终
  `dot` 路径已是目录（含 symlink-to-directory）时必须拒绝，不能把临时文件移进该目录后假成功。
- Repository 根目录的 `bootstrap.sh` 是薄工作流：它从已 clone 的自身 checkout 依次运行
  `make install`、已安装 binary 的 `dot init <repository>` 和 `dot apply`。它不解析 placements、
  state 或 plan，不复制 convergence 逻辑。
- `bootstrap.sh --preview-apply` 仍会安装 binary 并执行幂等 init，只把最后一步改为
  `dot apply --dry-run`；该名称明确不承诺整段脚本只读。除这个可选参数外，其他参数在任何
  mutation 前作为用法错误拒绝。
- Bootstrap 使用绝对 installed-binary 路径完成内部调用；安装目录不在 `PATH` 时只警告，不使
  workflow 失败。脚本安全重跑：binary 可重新安装、相同 init 为 no-op、apply 收敛当前 checkout。
- Git 生命周期在 workflow 外部。Bootstrap 不 clone、pull、切换 branch、处理凭据或解决冲突，
  也不安装 Go、Git、包管理器、shell 配置或修改 `PATH`。Repository 只有配置内容变化时，外部
  Git 更新后直接 `dot apply`；Go CLI 源码变化时先 `make install` 或重跑 bootstrap。
- 不增加 `sync`、`update`、`clean`、`setup` 等 CLI 子命令。卸载 binary 或控制数据是显式人工
  运维动作，不属于 convergence。

## Select

- `dot select add MODULE` 和 `dot select remove MODULE` 只修改 machine config 中的
  `extra_modules`，不读取 state、不规划或修改 target，也不执行 convergence。
- 两个命令都先获取 mutation advisory lock，再在锁内只加载和验证一次最新 machine config 与
  repository；不执行锁前 selection planning 或 fingerprint 比较。成功后提示运行 `dot apply`。
- `select add` 要求 module 存在、配置有效且当前平台 applicability 为 applicable。Module 已由
  active profile 或 `extra_modules` 选择时成功 no-op，不写入冗余 extra。
- `select remove` 只删除直接 extra selection。直接 selection 不存在时成功 no-op；若 active
  profile 仍选择该 module，结果明确说明 module 仍然 active。
- `select remove` 允许目标 module manifest 已删除、not-applicable、indeterminate 或 malformed，
  因为收缩 selection 不需要解析目标 manifest；active profile 自身无效仍属于配置失败。
- Selection 修改不会同步清理 state 或 target；这些 stale ownership 由下一次 `dot apply` 按当前
  selection 收敛。
- 不提供旧的 `dot remove` 命令或兼容 alias，也不为 select 命令提供 dry-run。

## Apply

- `dot apply` 收敛全部 effective modules，并处理 state 中不再 active 的 stale links。
- `apply` 永不修改 machine selection；先用 `dot select add/remove` 改变 selection，再运行全量
  `dot apply`。
- `-n` 是 `--dry-run` 的等价短参数；两者遵循[Status 与 dry-run](#status-与-dry-run)定义的
  只读分析规则。
- `dot apply MODULE` 是用法错误，返回 `2`，stdout 为空，且不读取或修改 control、target 或
  state；不提供 compatibility alias、弃用期或静默忽略。
- Apply 失败且可能已经部分应用时，只投影已经完成的行并提示重跑全量 `dot apply`。
- Apply 在锁内观察到任何 `skip` 时输出完整循环行后返回 `1`；这是整批拒绝，不是运行时
  error，不得输出成功摘要或额外重复 error。

## 全量收敛与加载

- Apply、mutation dry-run 与 status 严格加载 `dot.toml` 和全部 current effective
  `module.toml`，并在同一次循环中检查所有 effective placements 与全部 state-only stale
  records。任一 effective manifest 类型异常、不可读、dangling symlink 或 malformed 时，整次
  观察 fail closed。Init/select 的 manifest 加载例外由各自章节定义。
- Missing extra-selected、indeterminate 与 extra not-applicable module 是可投影的 `skip`；只要该
  selection 仍存在，其 state records 不投影 `remove` / `forget`。Active profile 引用 missing
  module 仍是配置错误并返回 `1`。
- Inactive repository module manifest 继续延迟加载；无参数 status 可将其列入 inventory，但
  未加载的 applicability 与 variant 显示为 `-`。
- `dot status MODULE` 与任何其他多余位置参数都是用法错误，返回 `2`，stdout 为空，且不读取或
  修改 control、target 或 state。

## Status 与 dry-run

- `status` 投影模块清单，再加上与 dry-run / apply 同一条循环算出的行。CLI 不维护第二套
  summary/convergence 状态机，也不投影内部账本结构。
- 真实 apply 不接收或复用只读快照。它先获取 advisory lock，再在锁内调用同一循环；
  status/dry-run 的无锁快照不构成 future apply 的 lease。
- Status 逐行投影事实：

  - `fact module=MODULE selection=<none|profile|extra|profile+extra> state=<absent|present>`，其中
    state 只表示该 module 是否有 link ownership；
  - 已加载 manifest 时追加 `applicability=<applicable|not-applicable|indeterminate>`；
  - applicable 时追加 `variant=portable` 或具名 variant；
  - indeterminate 时追加诊断 reason。

  Status、dry-run 与 apply 预检使用同一套行：

  ```text
  <op> module=<module> placement=<placement> target=<quoted-path> [reason=<quoted-reason>]
  chmod control=<config-root|config|state-root|state|lock> path=<quoted-path> mode=<0700|0600>
  ```

  `op` 只能是：

  | op | 含义 |
  | --- | --- |
  | `link` | 叶子空，将建 symlink |
  | `file` | local 叶子空，将拷贝 example |
  | `replace` | 叶子是我们的旧链接，将换成新 dest |
  | `remove` | 不再 desired，dest 仍匹配，将删链接 |
  | `record` | 叶子已正确，只补账 |
  | `forget` | 丢账，不碰文件 |
  | `chmod` | 修复现存私有控制目录或文件权限 |
  | `skip` | 不会碰；有任一 `skip` 则整批不写 |

  无变化的 link/local 不显示。`forget` 必须带 reason。排序：先全部 `skip`，再按
  [`planning.md`](planning.md#循环模型) 的动手顺序。父目录准备、复核和 state commit 不单独
  投影。

  State 文件不存在时，stderr 写一行
  `warning: state is missing; links removed from desired configuration cannot be discovered`。
  该提示不改变 status 退出码，也不单独阻断 apply。

  三个控制前缀互相重叠、必要输入读不了、或配置/manifest/state 无法解析时，分析不完整，不
  伪造循环行。Inactive inventory 仍以 fact 表达。
- Status 只要能看完清单和循环（含 `skip`）就返回 `0`。Mutation dry-run 能看完后，若存在
  `skip` 则完整输出后返回 `1`；否则返回 `0`。`record` / `forget` / 无行本身不改变 dry-run
  退出码。没有 `--check`。
- Dry-run 使用与真实 apply 相同的加载和循环，但不写 config、state、target、parent
  directory、lock 或 temporary file。
- Status 和 dry-run 不取锁；并发 mutation 时结果是 best-effort snapshot。
- 本节只定义公开投影；真实 mutation 的锁内再观察与执行只由
  [`mutation-and-recovery.md`](mutation-and-recovery.md#执行顺序) 定义。
- 真实 apply 在动手前看到 `skip` 时，投影同一完整循环后返回 `1`，不额外打印 error。成功时
  投影实际执行的行；没有任何行时写 `converged`。运行时失败只投影已经完成的行，再在
  stderr 写 error；不得把未做的行打成完成，尤其不得在 state commit 成功前声称已经 record 或
  forget ownership。错误可附 `may_have_changed=true|false`，但不公开 stage/recovery 分类协议。

## 输出与退出码

正常结果、facts 和循环行写 stdout；state-missing 提示与错误写 stderr。整批拒绝的 dry-run/
apply 不在 stderr 重复 `skip`。不得输出 local 内容、配置内容或秘密。控制前缀重叠或
placement 越界必须列出发生冲突的具体路径，并提示运行 `dot paths`。

| Exit code | 含义 |
| ---: | --- |
| `0` | 成功 apply、完整 status（即使有 `skip`），或不含 `skip` 的 mutation dry-run |
| `1` | 含 `skip` 的 mutation dry-run/apply，或配置、ownership、lock、文件系统及运行时失败 |
| `2` | CLI 参数或用法错误 |

三个控制前缀重叠、读不了必要输入、Active profile 引用缺失 module、malformed manifest 与
多个已确定 matching variants 等无法可靠观察的配置失败，三条命令都返回 `1`。`2` 仅用于
CLI 语法错误，例如未知 flag、`apply/status` 的多余位置参数或 `select add` 缺少 `MODULE`。

运行时错误的恢复提示由 CLI 根据错误原因附加，不扫描循环行补充语义：未初始化使用
`dot init`，控制路径失败使用 `dot paths`，legacy/invalid state 或 HOME mismatch 使用归档
state，too-new state 使用更新的 `dot` 并要求保留现有文件，中途失败使用重跑完整命令。
