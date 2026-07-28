# Public CLI

```text
dot init [REPOSITORY] --profile NAME... [--dry-run]
dot status [MODULE]
dot apply [MODULE] [--dry-run]
dot remove MODULE [--dry-run]
dot paths
dot version
dot help
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

## Init

- Repository 省略时使用当前目录，并且必须存在有效 `dot.toml`。
- Init 写入 repository 与 active profiles，然后执行首次全量收敛。
- `--profile` 至少提供一个；要初始化为空机器时显式选中一个空 profile，而不是省略
  `--profile`。
- Preflight 失败时不写机器配置或 artifacts。
- 机器配置提交后 apply 失败时保留 selection，用户通过 `dot apply` 重试。
- 已初始化时拒绝再次 init，不提供 reconfigure/rebind。

## Apply

- `dot apply` 收敛全部 effective modules，并处理 state 中不再 active 的 stale links。
- `dot apply <module>` 对 active module 做 scoped apply。
- 未 active 的 module 在 preflight 成功后加入 `extra_modules` 再收敛。
- Module 不存在、不适用或与其他 effective module/state target 冲突时，不修改 selection。
- Scoped apply 只需检查目标 module 与其他 effective modules/state 的冲突，不要求无关 module
  之间重新证明所有关系。

## Remove

- Active profile 仍选择 module 时拒绝，不修改 selection 或文件系统。
- 要移除 profile 选中的 module，先在仓库 profile 删除引用，再 `dot apply` 收敛 prune。
- Extra module 先从 prospective selection 移除，通过 preflight 后写回配置。
- 删除 state 证明、resolved target 未改变且 raw destination 未漂移的 module links。
- 保留所有 local，并在 state 可用时提示。
- Manifest 已删除但 extra/state 仍有 module 记录时允许清理。
- 已 inactive 且无 state 时成功 no-op；完全未知的 module 失败。

## 命令 scope 与加载

- `dot apply` 的 scope 是全部 effective modules。
- Scoped apply/remove 的 scope 是目标 module 与其他 effective modules。
- 严格加载 `dot.toml`，但只对命令 scope 内的 `module.toml` 做最终类型检查、读取和解析；
  scope 外 module manifest 的异常类型、不可读、dangling symlink 或 malformed TOML 不影响
  本命令。
- Status 与 dry-run 使用对应 mutation 命令的相同 scope。

## Status 与 dry-run

- Status 只读，显示 module activation、variant 和 `converged`、`pending`、`conflict`、
  `not-applicable`、`inactive` 或 `stale`。
- 默认 status 即使发现 pending/conflict 仍返回成功；没有 `--check`。
- Dry-run 使用与真实命令相同的解析、resolution 和 planner，但不写 config、state、target、
  parent directory、lock 或 temporary file。
- Status 和 dry-run 可以在内存中删除兼容的空 state module，但不得因此重写 state。
- Status 和 dry-run 不取锁；并发 mutation 时结果是 best-effort snapshot。
- 真实命令总是重新规划，不执行保存的 dry-run plan。

## 输出与退出码

正常结果、status 和 dry-run plan 写 stdout；错误写 stderr。不得输出 local 内容、配置内容或
秘密。Control topology 或 placement/control path conflict 必须列出发生冲突的具体路径，并
提示运行 `dot paths` 查看当前 control 文件位置。

| Exit code | 含义 |
| ---: | --- |
| `0` | 成功，或有效 status/dry-run |
| `1` | 配置、ownership、lock、文件系统或运行时失败 |
| `2` | CLI 参数或用法错误 |

未知或不适用的 module 属于配置/运行时条件，返回 `1`；`2` 仅用于 CLI 语法错误，例如未知
flag 或 `remove` 缺少 `MODULE` 参数。

运行时失败不要求维护完整的 completed/failed/not-attempted 结果协议。错误信息必须指出失败
动作；已经发生 mutation 时提示本轮可能部分完成并建议重跑，不得把未执行动作显示为成功。
