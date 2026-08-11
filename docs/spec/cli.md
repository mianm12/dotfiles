# Public CLI

```text
dot init [REPOSITORY] [--profile NAME]...
dot select add MODULE
dot select remove MODULE
dot status [MODULE]
dot apply [MODULE] [--dry-run]
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
- Preflight 失败时不写机器配置。成功后提示用户运行 `dot apply` 完成首次收敛。
- 已初始化时拒绝再次 init，不提供 reconfigure/rebind。

## Select

- `dot select add MODULE` 和 `dot select remove MODULE` 只修改 machine config 中的
  `extra_modules`，不读取 state、不规划或修改 target，也不执行 convergence。
- 两个命令都使用 mutation advisory lock，并在锁内重新加载和验证 machine config 与
  repository；成功后提示运行 `dot apply`。
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
- `dot apply <module>` 对 active module 做 scoped apply。
- 未 active 的 module 失败并提示先运行 `dot select add MODULE`；`apply` 永不修改 machine
  selection。
- Scoped apply 失败时提示重跑同一条 `dot apply <module>`；无参数 apply 提示 `dot apply`。

## 命令 scope 与加载

- `dot apply` 的 scope 是全部 effective modules。
- Scoped apply 的 participating set 包含目标 module 与其他 effective modules；
  placement topology 只检查目标 module 与所有 effective modules 的关系，两个都完全不属于
  scope 的 module 之间的冲突不阻断。
- Apply、mutation dry-run 与 status 的 convergence analysis 严格加载 `dot.toml` 和全部 current
  effective `module.toml`。Scoped apply 仍需解析其他 effective placements 才能完成 participating
  target topology 校验，因此 scope 外但 effective 的 manifest 若类型异常、不可读、dangling
  symlink 或 malformed，整次分析 fail closed。Init/select 的 manifest 加载例外由各自章节定义。
- Mutation dry-run 与 `status [MODULE]` 始终观察并解析完整 current effective selection；指定
  `MODULE` 只缩小 inventory，并在该 module inactive 时额外允许检查其 manifest，不模拟
  selection 修改。
- Inactive repository module manifest 继续延迟加载；无参数 status 可将其列入 inventory，但
  未加载的 applicability 与 variant 显示为 `-`。

## Status 与 dry-run

- CLI 对 status 和 dry-run 建立同一种只读 operation analysis。它包含
  当前 machine selection、module selection source、applicability、variant、convergence、
  输入 warning，以及同一份 `Plan{Steps, Issues}`。`Steps` 只包含可执行 placement step；
  plan conflict 和 blocker 都是带 kind、module/placement 定位与 reason 的 `Issues`。
- 真实 apply 的完整零写入 preflight 由 mutation owner 执行，复用同一 selection、state、actual
  与 planner 规则，但不接收或复用 CLI operation analysis。
- Status 保留第二列的 `converged`、`pending`、`conflict`、`not-applicable`、`inactive` 或
  `stale` 摘要。每行固定从 `MODULE  SUMMARY` 开始，只追加有区分度的维度：

  - selection 不是 `none` 时追加 `selection=<profile|extra|profile+extra>`；
  - applicability 仅在不是 `applicable`/`-` 且未与 `not-applicable` 摘要重复时追加；
  - convergence 不是 `-` 且未与摘要重复时追加；
  - 只有具名 variant 才追加 `variant=VARIANT`；portable layout 与 `-` 省略，但名字恰好为
    `portable` 的具名 variant 仍显示 `variant=portable`；
  - 只有非空 reason 才追加带双引号和转义的 `reason=QUOTED_REASON`。

  Analysis 内部的 `-` 仍表示未加载或不存在该维度，不是平台状态。Effective indeterminate
  module 的第二列摘要为 `conflict`，显示 applicability 与平台字段或 variant 歧义 reason，
  不生成 placement step；`status MODULE` 检查未选中的 indeterminate module 时仍显示
  `inactive`，不产生 issue。Concrete placement conflict 逐条显示结构化 issue；module/path
  级合成 conflict 不伪造 placement step，而在 module 行保留完整 reason。Profile module
  已确定 not-applicable 但仍有旧 ownership step 时显示
  `convergence=pending-cleanup`，reason 至少标出对应 cleanup decision。
- 完整分析中的 blocked issue 写 stdout 并包含 reason。Dry-run 和 status 都显示计划执行的 forget
  step 及其结构化
  reason；status 还显示每条 concrete placement conflict；输入 warning 仍写 stderr。
- Status 只要形成完整 analysis 就返回成功，即使其中有 pending、conflict 或 blocker。
  Mutation dry-run 形成完整 analysis 后，若同一 analysis 会因 blocker 或 plan conflict
  被真实 mutation 拒绝，则完整输出后返回 `1`；否则返回成功。Pending、create、update、
  prune 或 forget step 本身不改变 dry-run 退出码。没有 `--check`。配置、manifest 或 state
  无法解析、必要输入无法读取，或未分类的文件系统观察失败时 analysis 不完整并返回失败。
- Dry-run 使用与真实命令相同的解析、resolution 和 planner，但不写 config、state、target、
  parent directory、lock 或 temporary file。
- Status 和 dry-run 可以在内存中删除兼容的空 state module，但不得因此重写 state。
- Status 和 dry-run 不取锁；并发 mutation 时结果是 best-effort snapshot。
- 本节只定义 operation analysis 的公开投影；真实 mutation 的重新分析与执行只由
  [`mutation-and-recovery.md`](mutation-and-recovery.md#执行顺序) 定义。
- CLI 只投影 mutation owner 判定的最终 outcome。成功 outcome 中每个 forget step 的过去式
  ownership/provenance 提示都从结构化 step 派生，不保存第二份字符串结果；失败 outcome
  返回 `1`，不得先输出成功摘要或把未完成 step 显示为成功。若 outcome 表明可能已经部分
  应用，错误提示重跑确认收敛。

## 输出与退出码

正常结果、status 和 dry-run plan 写 stdout；错误写 stderr。Blocked dry-run 的完整 analysis
仍是 stdout 结果，不在 stderr 重复 blocker。不得输出 local 内容、配置内容或秘密。Control
topology 或 placement/control path conflict 必须列出发生冲突的具体路径，并提示运行
`dot paths` 查看当前 control 文件位置。

| Exit code | 含义 |
| ---: | --- |
| `0` | 成功、完整 status，或可执行的 mutation dry-run |
| `1` | Blocked mutation dry-run，或配置、ownership、lock、文件系统及运行时失败 |
| `2` | CLI 参数或用法错误 |

真实 mutation 的 request-specific 未知、inactive module、not-applicable 或 applicability
indeterminate 都返回 `1`。Status 能把 request-specific 未知、不适用、indeterminate 或路径
conflict 表达为完整 blocker 时返回 `0`；mutation dry-run 对同样的完整 blocker返回 `1`，同时
保留完整 stdout。无效 module ID、active profile 引用缺失 module、malformed
manifest 与多个已确定 matching variants 仍是配置失败并返回 `1`。`2` 仅用于 CLI 语法错误，
例如未知 flag 或 `select add` 缺少 `MODULE` 参数。

运行时失败不要求维护完整的 completed/failed/not-attempted 结果协议。错误信息必须指出失败
动作；已经发生 mutation 时提示本轮可能部分完成并建议重跑，不得输出计划 step 或把未完成
动作显示为成功，尤其不得声称已经 forget ownership。
