# Modules 与平台解析

本文件拥有 module 发现与 manifest 加载、platform evidence、applicability，以及 portable / variants
解析。哪些 module IDs 被机器选中由 [`selection.md`](selection.md)拥有；link/local placements 的
字段和路径规则由 [`placements.md`](placements.md)拥有。

## Module 发现与加载

Repository 布局：

```text
.
├── dot.toml
└── modules/
    ├── git/
    │   ├── module.toml
    │   ├── gitconfig
    │   └── config.local.example
    └── ghostty/
        ├── module.toml
        ├── config
        ├── macos/
        └── linux/
```

- Module ID 来自 `modules/<id>/`，使用 `[a-z0-9][a-z0-9_-]*`；manifest 固定为
  `module.toml`。
- 只有名字符合该规则且含 `module.toml` 的 `modules/<id>/` 目录才是 module。`modules/` 下
  其他文件、非目录项或不合规目录一律忽略，不报错。
- Module 只能使用 portable 或 variants 其中一种模式，不得混用。
- Variant ID 使用 `[a-z0-9][a-z0-9_-]*`。Placement ID 由
  [`placements.md`](placements.md)定义。

执行 apply/status/dry-run convergence analysis 时，全部 current effective `module.toml` 必须是
regular file，或最终解析为 regular file 的 symlink。Directory、FIFO、socket、device、dangling
symlink 和 symlink loop 必须在读取内容前失败；manifest symlink 的目标不要求位于 repository 内。

Inactive module manifest 延迟类型检查、读取和解析；status 可以在不读取 manifest 的情况下将其
列入 inventory。Init 与 select 只按 [CLI 规范](cli.md#init)和
[CLI select 规则](cli.md#select)加载当前操作必要的 manifest。

## Platform evidence 与 match

Platform 的 `os`、`distro` 和 `arch` 各自包含 value、是否 known，以及 unknown 时的诊断原因；
value 只有在 known 时参与匹配：

```toml
os = ["macos", "linux"]
distro = ["ubuntu", "arch"]
arch = ["x86_64", "aarch64"]
```

- 不同字段之间是 AND，同一字段数组内是 OR；字段缺失表示不限制。
- `os` 是封闭枚举 `{macos, linux}`，出现枚举外值为配置错误。
- `distro` 与 `arch` 是自由小写字符串，逐字比较，不维护发行版或架构注册表。
- `distro` 只允许与 `os = ["linux"]` 一起声明。
- 运行时检测不到 os/distro/arch 本身不是配置错误，但必须保留 unknown 诊断，不能用空 value
  冒充已确定不匹配。
- 不支持否定、正则、优先级、fallback 或 capability 表达式。

## Applicability

Match 结果只有三种：

- `applicable`：所有受约束字段都 known 且匹配；
- `not-applicable`：至少一个受约束字段 known 且不匹配，即使其他字段 unknown；
- `indeterminate`：没有 known mismatch，但至少一个受约束字段 unknown。

Profile 选中的 module 无匹配 variant 时，not-applicable 是合法的非配置错误结果。Extra module
或 `select add MODULE` 检查的 module 使用同一 applicability 解析。

Profile not-applicable 的旧 ownership cleanup 只由
[`planning.md`](planning.md#通用决策规则)定义；indeterminate 和 extra/explicit
not-applicable 的 mutation 边界只由
[`mutation-and-recovery.md`](mutation-and-recovery.md#安全规则)定义；公开事实、诊断与退出码只由
[`cli.md`](cli.md#status-与-dry-run)定义。

## Portable module

```toml
[match]
os = ["macos", "linux"]

[[links]]
id = "config"
source = "gitconfig"
target = "~/.gitconfig"

[[locals]]
id = "local"
example = "config.local.example"
target = "~/.config/git/config.local"
```

`[match]` 可以省略，表示适用于所有受支持平台。Source、example 和 target 继续遵循
[`placements.md`](placements.md)的规则。

## Variants

共享内容但 target 不同时，variant 的 `root` 可以是 `.`：

```toml
[variants.macos]
root = "."

[variants.macos.match]
os = ["macos"]

[[variants.macos.links]]
id = "config"
source = "config"
target = "~/Library/Application Support/example/config"

[variants.linux]
root = "."

[variants.linux.match]
os = ["linux"]

[[variants.linux.links]]
id = "config"
source = "config"
target = "~/.config/example/config"
```

内容也不同时使用不同 root，例如 `root = "macos"` 或 `root = "linux"`。

- `root` 必填；`.` 表示 module 根目录。
- 其他 root 必须是 module 内相对目录，不得是绝对路径或包含 `..` 逃逸。
- 零个 applicable 且没有 indeterminate variant 表示 not-applicable；多个已确定 applicable
  variants 是配置错误。
- 一个已确定 applicable variant 与任意可能匹配的 indeterminate variant 同时存在时，不选择
  variant，整个 module 为 indeterminate。没有已确定 applicable、但至少一个 variant
  indeterminate 时同样为 indeterminate。
- Variant 完整声明自己的 placements，不继承其他 variant 或顶层 placements。
