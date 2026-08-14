# Repository 与机器选择

本文件拥有 repository binding、顶层 profile 定义、machine config 与 effective selection。Module
发现、manifest 加载和平台解析由
[`modules-and-platforms.md`](modules-and-platforms.md)拥有。

## Repository desired 与 `dot.toml`

Repository 中的 `dot.toml` 选择 modules，`modules/` 下的 manifest 与 source 描述它们的内容。
顶层 `dot.toml` 示例：

```toml
version = 1

[profiles]
base = ["git", "zsh", "nvim"]
personal = ["ghostty"]
work = ["work-git"]
```

- `version` 必填，只支持 `1`。
- `[profiles]` 必须显式存在；空表合法。
- Profile ID 使用 `[a-z0-9][a-z0-9_-]*`。
- Profile 值是 [module ID](modules-and-platforms.md#module-发现与加载) 数组，不得重复。
- 空 profile 合法。
- Active profile 引用不存在的 module 时配置无效。Missing module 若仍在 `extra_modules` 中，
  desired 不完整并由 module `skip` 阻断；先 `select remove` 后，其 state records 才成为 stale
  输入。只有未被当前 selection 引用的 state module 才直接按 stale 规则清理。
- CLI 不修改 repository profiles；多个 active profiles 只做集合并集，声明与选择顺序不改变
  语义。

执行 init、select、apply、status 或 dry-run 需要读取 `dot.toml` 时，其最终对象必须是 regular
file，或最终解析为 regular file 的 symlink。Directory、FIFO、socket、device、dangling symlink
和 symlink loop 必须在读取内容前失败；manifest symlink 的目标不要求位于 repository 内。

各命令加载 selection 的范围由 [CLI 规范](cli.md)定义；module manifest 的 eager/deferred
边界由 [`modules-and-platforms.md`](modules-and-platforms.md#module-发现与加载)定义。

## Machine config

Machine config 保存 repository 绝对路径、active profiles 和本机直接选择的 modules：

```toml
version = 1
repository = "/Users/user/dotfiles"
profiles = ["base", "work"]
extra_modules = ["tmux"]
```

- Machine config 不存在表示机器未初始化。
- `version` 必填，只支持 `1`；`repository` 必须是非空绝对路径。
- 一旦存在，其最终目录项本身必须是 regular file。类型检查不跟随最终 symlink，因此
  symlink-to-regular、dangling symlink、directory、FIFO、socket 和 device 都必须在读取内容前
  失败；更高层 ancestor symlink 仍按 control root 规则处理。
- Machine config 中的 active profile 列表可以为空。Profiles 使用集合语义：顺序不改变
  selection，重复 profile 必须拒绝。`init` 新写入配置时按 profile ID 字节序保存；读取已有配置
  时不要求其原始顺序已经规范化。
- `init` 省略 `--profile` 时选择唯一默认 profile `default`；repository 未声明 `default` 时失败。
  一旦显式传入一个或多个 `--profile`，只选择这些 profiles，不再隐式加入 `default`。
- `init` 写入 profiles；`select add MODULE` 和 `select remove MODULE` 确定性重写
  `extra_modules`；`apply` 不修改 machine config。
- CLI 重写 machine config 时不承诺保留注释和空行。

`init` 是 repository 与 active profiles 的幂等绑定操作，不是 reconfigure：

- Machine config 不存在时，校验 repository 与 active profiles 后写入新配置，`extra_modules` 为空；
- 已有配置的规范化 repository 与 active profile 集合都和本次输入相同时，重新校验当前
  repository/profile 后成功 no-op，不重写配置并保留全部 `extra_modules`；
- Repository 或 active profile 集合不同时拒绝，不自动 rebind、增删 profile 或清空
  `extra_modules`。

Init 后修改 active profiles 的受支持方式是先通过 [`dot paths`](cli.md#paths) 定位 machine
config，手工编辑 `profiles`，再执行全量 `dot apply`。产品不提供 profile 修改或 repository
rebind 子命令；命令边界见 [CLI 规范](cli.md)。

## Effective selection

当前 effective module IDs 是：

```text
modules(active profiles) union extra_modules
```

每个 module 的 selection 来源可以是 profile、extra 或两者。集合语义意味着重复来源不会让同一
module 执行两次；移除 extra 来源也不会停用仍由 active profile 选择的 module。

Effective IDs 还要经过 module 存在性与 platform applicability 解析，才能得到当前 desired
placements。Module 不存在、not-applicable、indeterminate 和 variant 选择的规则见
[`modules-and-platforms.md`](modules-and-platforms.md)；stale ownership 的计划规则见
[`planning.md`](planning.md#通用决策规则)。

Selection mutation 在同一 advisory lock 内读取最新输入并只改变 machine config，不同步修改
target 或 state；它不执行锁前 planning 或 fingerprint 比较。`dot apply` 始终收敛当前
完整 effective selection，而不是隐式记住某次 select 命令的单个 module。公开命令行为见
[`cli.md`](cli.md#select)与[`cli.md`](cli.md#apply)。
