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
- `--profile` 可重复；省略时初始化为空 selection，不要求仓库为此声明无意义的空 profile。
- 参数或锁内校验失败时不写机器配置；安全取锁边界已经通过时可以留下私有 state root/lock
  bookkeeping。成功后提示用户运行 `dot apply` 完成首次收敛。
- 已初始化时拒绝再次 init，不提供 reconfigure/rebind。

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
- Apply 失败且可能已经部分应用时只提示重跑全量 `dot apply`。
- Apply 在锁内形成完整 blocker 时输出完整 analysis 后返回 `1`；这是 blocked outcome，不是运行时
  error，不得输出成功摘要或额外重复 error。

## 全量收敛与加载

- Apply、mutation dry-run 与 status 的 convergence analysis 严格加载 `dot.toml` 和全部 current
  effective `module.toml`，并在同一次全量规划中检查所有 effective placements 与全部 state-only
  stale records。任一 effective manifest 类型异常、不可读、dangling symlink 或 malformed 时，
  整次 analysis fail closed。Init/select 的 manifest 加载例外由各自章节定义。
- Inactive repository module manifest 继续延迟加载；无参数 status 可将其列入 inventory，但
  未加载的 applicability 与 variant 显示为 `-`。
- `dot status MODULE` 与任何其他多余位置参数都是用法错误，返回 `2`，stdout 为空，且不读取或
  修改 control、target 或 state。

## Status 与 dry-run

- CLI 对 status 和 dry-run 建立同一种只读 operation analysis。它包含当前 machine selection、
  客观 ModuleFacts，以及同一份 `Plan{Actions, Issues}`。可执行 Plan 还私有持有一次计算的 link-only
  NextState；CLI 不投影或重算它，blocked Plan 没有可提交 NextState。Warning Issue 不阻断执行，
  任一 blocker Issue 使 Plan 不可执行；不存在独立 Complete 位或第二套完成状态。
- 真实 apply 不接收或复用 CLI 保存的 Report。它先获取 advisory lock，再在锁内调用同一 analysis
  语义形成唯一权威 Report；status/dry-run 的无锁 snapshot 不构成 future apply 的 lease。
- Status 逐行投影事实，不由 core 维护字符串 summary/convergence 状态机：

  - `fact module=MODULE selection=<none|profile|extra|profile+extra> state=<absent|present>`，其中 state
    只表示该 module 是否有 link ownership；
  - 已加载 manifest 时追加 `applicability=<applicable|not-applicable|indeterminate>`；
  - applicable 时追加 `variant=portable` 或具名 variant；
  - indeterminate 时追加诊断 reason。

  Dry-run 与 status 均直接显示语义 Action：

  ```text
  action kind=<kind> module=<module> placement=<placement> target=<quoted-path> [reason=<quoted-reason>]
  ```

  `create-link`、`create-local`、`update`、`adopt`、`repair-state`、`prune` 与 `forget` 是唯一 Action
  kinds；无变化的 link/local 不显示 keep。Action 顺序与真实 mutation 消费的语义顺序相同，parent
  preparation、复核和 state commit 不单独投影。

  Issue 使用：

  ```text
  issue severity=<warning|blocker> code=<code> [module=<module>] [placement=<placement>]
        [target=<quoted-path>] reason=<quoted-reason> recovery=<recovery>
  ```

  Blocker Issues 写 stdout；warning Issues 写 stderr。Reason 必填。Recovery 只能是 `none`、`init`、
  `paths`、`archive-state`、`manual-migration` 或 `rerun-apply`。公开 IssueCode 固定为：

  - warning：`state-missing`、`stale-preserved`；
  - blocker：`selection-indeterminate`、`selection-not-applicable`、`control-topology`、
    `control-boundary`、`target-conflict`、`ownership-conflict`、`topology-conflict`、
    `placement-type-change`。

  Code 到 Recovery 的映射固定为：`state-missing` → `none`，`stale-preserved` →
  `manual-migration`，selection 两项 → `none`，control 两项 → `paths`，其余四项 →
  `manual-migration`。每个 complex stale forget 对应一个 `stale-preserved` warning，不合并多条
  ownership。Issues 按 severity（warning 先于 blocker）、code、module、placement、target、
  reason 稳定排序。

  不能可靠形成 placement Action 的配置、control topology 或 target-set failure 只输出可确定 facts
  与完整 Issues，不伪造局部 Action。Inactive inventory 仍以 fact 表达。
- Status 只要形成完整 analysis 就返回成功，即使其中有 Actions、warnings 或 blocker Issues。
  Mutation dry-run 能形成 Report 后，若 `Plan` 不可执行则完整输出后返回 `1`；否则返回成功。
  Action 或 warning 本身不改变 dry-run 退出码。没有 `--check`。配置、manifest 或 state 无法解析、
  必要输入无法读取，或未分类的文件系统观察失败时 analysis 不完整并返回失败。
- Dry-run 使用与真实命令相同的解析、resolution 和 planner，但不写 config、state、target、
  parent directory、lock 或 temporary file。
- Status 和 dry-run 不取锁；并发 mutation 时结果是 best-effort snapshot。
- 本节只定义 operation analysis 的公开投影；真实 mutation 的重新分析与执行只由
  [`mutation-and-recovery.md`](mutation-and-recovery.md#执行顺序) 定义。
- 真实 Apply 在执行任何 Action 前发现 blocker 时，投影同一完整 Report 后返回 `1`，不额外打印
  error。成功 outcome 中每个 forget Action 的过去式 ownership 提示都从结构化 Action 派生，不
  保存第二份字符串结果。运行时失败不得输出计划 Action 或把未完成 Action 显示为成功；若 typed
  failure 表明可能 partial，按其 recovery 提示重跑。

## 输出与退出码

正常结果、facts、Actions 和 blocker Issues 写 stdout；warning Issues 与错误写 stderr。Blocked
dry-run/apply 的完整 analysis 不在 stderr 重复 blocker。不得输出 local 内容、配置内容或秘密。Control
topology 或 placement/control path conflict 必须列出发生冲突的具体路径，并提示运行
`dot paths` 查看当前 control 文件位置。

| Exit code | 含义 |
| ---: | --- |
| `0` | 成功、完整 status，或可执行的 mutation dry-run |
| `1` | Blocked mutation dry-run/apply，或配置、ownership、lock、文件系统及运行时失败 |
| `2` | CLI 参数或用法错误 |

Status 能把 indeterminate selection、control topology、target-set blocker 或 concrete placement
conflict 表达为 Report 时返回 `0`；mutation dry-run/apply 对同样不可执行的 Report 返回 `1`，同时
保留完整 analysis。Active profile 引用缺失 module、malformed manifest 与多个已确定 matching
variants 等无法可靠形成 Report 的配置失败返回 `1`。`2` 仅用于 CLI 语法错误，例如未知 flag、
`apply/status` 的多余位置参数或 `select add` 缺少 `MODULE` 参数。

不完整 analysis 与运行时 failure 使用 core 提供的 typed recovery，不扫描错误字符串或 IssueCode
补充语义：未初始化使用 `init`，control entry/path failure 使用 `paths`，legacy/invalid/too-new state
内容或 HOME mismatch 使用 `archive-state`，partial mutation 使用 `rerun-apply`，其余为 `none`。
错误信息必须指出 failure stage 及失败 Action（若存在）；不得输出计划 Action 或把未完成 Action
显示为成功，尤其不得声称已经 forget ownership。
