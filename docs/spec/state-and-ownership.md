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

## Ownership 规则

- State 按 module 和 placement ID 组织。
- State 与绝对 HOME 绑定；HOME 不一致时拒绝 ownership mutation。
- State 不绑定当前 repository path。仓库移动使 desired destination 改变，按普通 link update
  处理。
- Link ownership 只依赖 target、resolved target 和 raw link destination。
- Local state 只用于退出 desired 时提示，不提供修改或删除权限。
- State 成功后的内容必须反映本轮已验证结果；内部使用重建或局部更新不属于契约。
- State 只在选定 scope 成功后原子提交。
- Unknown field、缺失安全字段、损坏结构或过新版本拒绝 mutation。安全字段指顶层 `version`、
  `home`，以及每个 placement 的 `kind`、`target`；link 另需 `resolved_target` 与
  `link_destination`。其余为可选诊断字段。
- State missing 按空 state 继续，但警告无法发现已从 manifest 删除的历史 link。
