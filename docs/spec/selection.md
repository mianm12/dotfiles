# 仓库、机器选择与解析

## Repository desired

仓库中的 `dot.toml`、`modules/<id>/module.toml` 和配置内容描述共享期望。

`dot.toml` 与当前命令 scope 内加载的 `module.toml` 必须是 regular file，或最终解析为 regular
file 的 symlink。Directory、FIFO、socket、device、dangling symlink 和 symlink loop 必须在
读取内容前失败；manifest symlink 的目标不要求位于 repository 内。Scope 外 module manifest
继续按 [`cli.md`](cli.md#命令-scope-与加载) 延迟类型检查、读取和解析。

## Machine config

机器配置保存仓库路径、active profiles 和本机额外 modules：

```toml
version = 1
repository = "/Users/user/dotfiles"
profiles = ["base", "work"]
extra_modules = ["tmux"]
```

有效 module 集合是：

```text
modules(active profiles) union extra_modules
```

Profile 内容只在仓库中人工维护。`init` 写入 profiles；`apply <module>` 和
`remove <module>` 可以确定性重写 `extra_modules`。CLI 重写机器配置时不承诺保留注释和空行。

Machine config 不存在表示机器未初始化；一旦存在，其最终目录项本身必须是 regular file。
类型检查不跟随最终 symlink，因此 symlink-to-regular、dangling symlink、directory、FIFO、
socket 和 device 都必须在读取内容前失败。更高层 ancestor symlink 仍按 control root 规则
处理。

Init 之后调整 active profiles 的受支持方式是先通过
[`dot paths`](cli.md#paths) 定位机器配置，手工编辑其中的 profiles，再执行 `dot apply`；
产品不提供修改 profiles 的命令。命令细节见 [`cli.md`](cli.md)。

## 仓库布局与 Profile

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

顶层 `dot.toml`：

```toml
version = 1

[profiles]
base = ["git", "zsh", "nvim"]
personal = ["ghostty"]
work = ["work-git"]
```

规则：

- `version` 必填，只支持 `1`。
- Module ID 来自 `modules/<id>/`；module manifest 固定为 `module.toml`。
- Module、profile、variant 和 placement ID 使用 `[a-z0-9][a-z0-9_-]*`。
- 只有名字符合该规则且含 `module.toml` 的 `modules/<id>/` 目录才是 module；`modules/` 下其他
  文件、非目录项或不合规目录一律忽略，不报错。
- Profile 值是 module ID 数组，不得重复。
- 多个 active profiles 只做集合并集，顺序不改变语义。
- 空 profile 合法；active profile 列表可为空（例如仅选中空 profile），但 `init` 至少要求一个
  `--profile`。
- Active profile 引用不存在的 module 时配置无效；该失效只针对仓库 profile。extra_modules
  和 state 中引用已删除 module 视为可清理，因为 profile 由仓库权威维护，extra/state 由本机
  维护。
- CLI 不修改仓库 profile。

## Platform 与 Module

Platform 是 resolver 的显式输入，测试必须能够注入合成值。每个字段同时保存 value、是否
known 和 unknown 时的诊断原因；value 只有在 known 时参与匹配：

```toml
os = ["macos", "linux"]
distro = ["ubuntu", "arch"]
arch = ["x86_64", "aarch64"]
```

- 不同字段之间是 AND，同一字段数组内是 OR。
- 字段缺失表示不限制。
- `os` 是封闭枚举 `{macos, linux}`，出现枚举外值为配置错误；`distro`、`arch` 是自由小写
  字符串，逐字比较，不维护发行版/架构注册表。
- `distro` 只允许与 `os = ["linux"]` 一起声明。
- 运行时检测不到 os/distro/arch 本身不是配置错误，但字段必须保留 unknown 诊断，不能用空
  value 冒充已确定不匹配。
- Match 结果为 `applicable`、`not-applicable` 或 `indeterminate`：
  - 任一受约束字段 known 且不匹配时，结果确定为 not-applicable，即使其他字段 unknown。
  - 没有 known mismatch，但至少一个受约束字段 unknown 时，结果为 indeterminate。
  - 所有受约束字段都 known 且匹配时，结果为 applicable。
- 不支持否定、正则、优先级、fallback 或 capability 表达式。
- Profile 选中的 module 无匹配 variant 时是 not-applicable，不报错。
- Extra module 或显式 `apply <module>` 无匹配 variant 时失败。
- Profile module 已确定 not-applicable 时仍按 ownership 规则清理旧 placement。任意 effective
  module 为 indeterminate 时，真实 mutation 整体失败且不得 prune；extra 或显式 module
  已确定 not-applicable 时仍阻止 selection 发布。

Module 只能使用 portable 或 variants 其中一种模式，不得混用。

### Portable

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

`[match]` 可以省略，表示适用于所有受支持平台。

### Variants

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

Variant 规则：

- `root` 必填；`.` 表示 module 根目录。
- 其他 root 必须是 module 内相对目录，不得是绝对路径或包含 `..` 逃逸。
- 零个 applicable 且没有 indeterminate variant 表示 not-applicable；多个已确定 applicable
  variant 是配置错误。
- 一个已确定 applicable variant 与任意可能匹配的 indeterminate variant 同时存在时，不选择
  variant，整个 module 为 indeterminate。没有已确定 applicable、但至少一个 variant
  indeterminate 时同样为 indeterminate。
- Variant 完整声明自己的 placements，不继承其他 variant 或顶层 placements。
