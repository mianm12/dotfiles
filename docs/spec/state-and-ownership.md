# State 与 Ownership

## 职责

State 是 link ownership 和 local provenance 的本机账本，不是 desired，也不保存配置内容、
local 内容、秘密或环境变量。

## 版本与结构

State version 为 `2`，用于区别不兼容的旧 state v1。旧版本不受支持；遇到 v1 时拒绝
mutation，并提示用户通过 [`dot paths`](cli.md#paths) 定位 state 文件，归档或移除旧 state
后重试。

`dot.toml`、machine config 与 state 使用相互独立的 version，当前分别为 `1`、`1`、`2`；
三者互不关联，state 取 `2` 仅为与旧不兼容 state 区分，不代表配置版本升级。

State 不存在时按空 state 继续；一旦存在，其最终目录项本身必须是 regular file。类型检查不
跟随最终 symlink，因此 symlink-to-regular、dangling symlink、directory、FIFO、socket 和
device 都必须在读取内容前失败。

逻辑结构：

```json
{
  "version": 2,
  "home": "/Users/user",
  "modules": {
    "git": {
      "placements": {
        "config": {
          "kind": "link",
          "target": "/Users/user/.gitconfig",
          "resolved_target": "/Users/user/.gitconfig",
          "link_destination": "/Users/user/dotfiles/modules/git/gitconfig"
        },
        "local": {
          "kind": "local",
          "target": "/Users/user/.config/git/config.local"
        }
      }
    }
  }
}
```

本检查点新增的兼容规则只针对空 module：合法 module ID 对应的 module object 若缺少
`placements` 或其值为空 object，按非 canonical state v2 输入读取。加载时在内存中删除该空
module，并保留下次成功 mutation 需要重写 state 的事实。Canonical state v2 不包含空 module；
编码待发布 state 时若仍含空 module 必须拒绝，而不是静默删除或写出。不升级 state version。

该兼容规则不放宽其他输入：非法 module ID、unknown field、`null`、损坏 object、实际
placement 缺失安全字段、HOME mismatch、state v1 和过新版本仍严格拒绝。空 module 不含
ownership 或 provenance，内存删除后不进入 module inventory。

## Ownership 规则

- State 按 module 和 placement ID 组织。
- State 与绝对 HOME 绑定；HOME 不一致时拒绝 ownership mutation。
- State 不绑定当前 repository path。仓库移动使 desired destination 改变，按普通 link update
  处理。
- Link ownership 只依赖 target、resolved target 和 raw link destination。
- Local state 只用于退出 desired 时提示，不提供修改或删除权限。
- State 成功后的内容必须反映本轮已验证结果；内部使用重建或局部更新不属于契约。
- State 只在选定 scope 的真实 mutation 成功后原子提交。删除 scope 外空 module 是整个文档的
  canonical representation 整理，不是 scope 外 ownership action。
- Unknown field、缺失安全字段、损坏结构或过新版本拒绝 mutation。安全字段指顶层 `version`、
  `home`，以及每个 placement 的 `kind`、`target`；link 另需 `resolved_target` 与
  `link_destination`。其余为可选诊断字段。
- State missing 按空 state 继续，但警告无法发现已从 manifest 删除的历史 link。
