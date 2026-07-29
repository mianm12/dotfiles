# Public CLI

```text
dot init [REPOSITORY] [--profile NAME]... [--dry-run]
dot status [MODULE]
dot apply [MODULE] [--dry-run]
dot remove MODULE [--dry-run]
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
- Init 写入 repository 与 active profiles，然后执行首次全量收敛。
- `--profile` 可重复；省略时初始化为空 selection，不要求仓库为此声明无意义的空 profile。
- Preflight 失败时不写机器配置或 artifacts。
- 机器配置提交后 apply 失败时保留 selection，用户通过 `dot apply` 重试。
- 已初始化时拒绝再次 init，不提供 reconfigure/rebind。
- First init 遇到缺失 state 属于预期路径；warning 先明确这一点，再以条件句提醒：如果同一 HOME
  曾被 dot 管理，则无法发现已退出 desired 的旧 link。

## Apply

- `dot apply` 收敛全部 effective modules，并处理 state 中不再 active 的 stale links。
- `dot apply <module>` 对 active module 做 scoped apply。
- 未 active 的 module 在 preflight 成功后加入 `extra_modules` 再收敛。
- Module 不存在、不适用或与其他 effective module/state target 冲突时，不修改 selection。
- Scoped apply 在 selection 已提交后失败时提示重跑同一条 `dot apply <module>`；无参数 apply
  仍提示 `dot apply`。

## Remove

- 只有 active profile 选择且不在 `extra_modules` 中的 module 才拒绝 remove，不修改 selection
  或文件系统。
- 同时由 profile 与 `extra_modules` 选择时，remove 删除冗余 extra：applicable module 继续按
  profile selection 收敛；not-applicable module 按既有 profile cleanup 规则处理；
  indeterminate 或配置错误仍在 selection 写入前失败。
- Remove 的 selection 已提交后若 cleanup、结果输出或 lock release 失败，统一提示运行
  `dot apply`，按已持久化的当前 selection 恢复收敛。
- 要移除 profile 选中的 module，先在仓库 profile 删除引用，再 `dot apply` 收敛 prune。
- Extra module 先从 prospective selection 移除，通过 preflight 后写回配置。
- 对目标 module 投影 [`planning.md`](planning.md) 产生的 prune/forget action；CLI 不另行定义
  link 或 local cleanup eligibility。
- Manifest 已删除但 extra/state 仍有 module 记录时允许清理。
- 仅由 extra 选择的 module 即使当前已确定 not-applicable 或为 indeterminate，remove 仍可
  收缩 selection，并只依据既有 state ownership 投影 cleanup。该 selection 写入与 mutation
  边界由 [`mutation-and-recovery.md`](mutation-and-recovery.md#安全规则) 定义。
- 已 inactive 且无 state 时成功 no-op；完全未知的 module 失败。

## 命令 scope 与加载

- `dot apply` 的 scope 是全部 effective modules。
- Scoped apply/remove 的 participating set 包含目标 module 与其他 effective modules；
  placement topology 只检查目标 module 与所有 effective modules 的关系，两个都完全不属于
  scope 的 module 之间的冲突不阻断。
- 严格加载 `dot.toml`，但只对命令 scope 内的 `module.toml` 做最终类型检查、读取和解析；
  scope 外 module manifest 的异常类型、不可读、dangling symlink 或 malformed TOML 不影响
  本命令。
- Mutation dry-run 使用 prospective selection 对应的 scope。`status [MODULE]` 始终观察当前
  machine selection；指定 `MODULE` 只缩小 inventory 并允许检查该 module manifest，不模拟
  `add-extra`。
- 无参数 status 继续延迟加载 inactive repository module manifest；未加载的 applicability 与
  variant 显示为 `-`。

## Status 与 dry-run

- CLI 对 status、dry-run 和 mutation preflight 建立同一种只读 operation analysis。它包含
  prospective machine selection、selection delta、module selection source、applicability、
  variant、convergence、placement actions、reason、输入 warning 与 blocker。
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
  不生成 placement action；`status MODULE` 检查未选中的 indeterminate module 时仍显示
  `inactive`，不产生 blocker。Concrete placement conflict 逐条显示结构化 action；module/path
  级合成 conflict 不伪造 placement action，而在 module 行保留完整 reason。Profile module
  已确定 not-applicable 但仍有旧 ownership action 时显示
  `convergence=pending-cleanup`，reason 至少标出对应 cleanup decision。
- Dry-run 在 placement actions 之前显示非空 selection delta：`create`、`add-extra` 或
  `remove-extra`；`add-extra`/`remove-extra` 同时显示 module ID。完整分析中的 blocker 写
  stdout 并包含 reason。Dry-run 和 status 都显示计划执行的 forget action 及其结构化
  reason；status 还显示每条 concrete placement conflict；输入 warning 仍写 stderr。
- Status 与 dry-run 只要形成完整 analysis 就返回成功，即使其中有 pending、conflict 或
  blocker；没有 `--check`。配置、manifest 或 state 无法解析、必要输入无法读取，或未分类的
  文件系统观察失败时 analysis 不完整并返回失败。
- Dry-run 使用与真实命令相同的解析、resolution 和 planner，但不写 config、state、target、
  parent directory、lock 或 temporary file。
- Status 和 dry-run 可以在内存中删除兼容的空 state module，但不得因此重写 state。
- Status 和 dry-run 不取锁；并发 mutation 时结果是 best-effort snapshot。
- 本节只定义 operation analysis 的公开投影；真实 mutation 的重新分析与执行只由
  [`mutation-and-recovery.md`](mutation-and-recovery.md#执行顺序) 定义。
- CLI 只投影 mutation owner 判定的最终 outcome。成功 outcome 中每个 forget action 的过去式
  ownership/provenance 提示都从结构化 action 派生，不保存第二份字符串结果；失败 outcome
  返回 `1`，不得先输出成功摘要或把未完成 action 显示为成功。若 outcome 表明可能已经部分
  应用，错误提示重跑确认收敛。

## 输出与退出码

正常结果、status 和 dry-run plan 写 stdout；错误写 stderr。不得输出 local 内容、配置内容或
秘密。Control topology 或 placement/control path conflict 必须列出发生冲突的具体路径，并
提示运行 `dot paths` 查看当前 control 文件位置。

| Exit code | 含义 |
| ---: | --- |
| `0` | 成功，或有效 status/dry-run |
| `1` | 配置、ownership、lock、文件系统或运行时失败 |
| `2` | CLI 参数或用法错误 |

真实 mutation 的 request-specific 未知 module、init/apply 请求的 not-applicable 或
applicability indeterminate，以及 remove 的 profile-selected blocker 都返回 `1`。仅由
extra 选择的 remove 目标按上文 selection contraction 规则处理；其他 prospective effective
indeterminate module 仍返回 `1`。
Status/dry-run 能把 request-specific 未知、不适用、indeterminate、profile-selected remove、
已初始化 init 或路径 conflict 表达为完整 blocker 时返回 `0`；无效 module ID、active profile
引用缺失 module、malformed manifest 与多个已确定 matching variants 仍是配置失败并返回
`1`。`2` 仅用于 CLI 语法错误，例如未知 flag 或 `remove` 缺少 `MODULE` 参数。

运行时失败不要求维护完整的 completed/failed/not-attempted 结果协议。错误信息必须指出失败
动作；已经发生 mutation 时提示本轮可能部分完成并建议重跑，不得输出计划 action 或把未完成
动作显示为成功，尤其不得声称已经 forget ownership。
