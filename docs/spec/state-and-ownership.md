# State 与 Ownership

## 职责

State 是 link ownership 和 local provenance 的本机账本，不是 desired，也不保存配置内容、
local 内容、秘密或环境变量。

## 版本与结构

State version 为 `3`。旧 state v1/v2 不受支持；遇到旧版本时必须在获取 mutation lock 前
fail closed，并提示用户通过 [`dot paths`](cli.md#paths) 定位 state 文件，在仓库外归档或移除
旧 state 后重试。程序不提供兼容读取、自动迁移或 reset 子命令。

`dot.toml`、machine config 与 state 使用相互独立的 version，当前分别为 `1`、`1`、`3`；
三者互不关联，state 升级不代表配置版本升级。

State 不存在时按空 state 继续；一旦存在，其最终目录项本身必须是 regular file。类型检查不
跟随最终 symlink，因此 symlink-to-regular、dangling symlink、directory、FIFO、socket 和
device 都必须在读取内容前失败。

逻辑结构：

```json
{
  "version": 3,
  "home": "/Users/user",
  "records": [
    {
      "module_id": "git",
      "placement_id": "config",
      "kind": "link",
      "target": "/Users/user/.gitconfig",
      "resolved_target": "/Users/user/.gitconfig",
      "link_destination": "/Users/user/dotfiles/modules/git/gitconfig"
    },
    {
      "module_id": "git",
      "placement_id": "local",
      "kind": "local",
      "target": "/Users/user/.config/git/config.local"
    }
  ]
}
```

`records` 是必需的 array；空账本使用空 array。每条 record 显式携带 module ID 与 placement ID，
两者组成唯一 identity；重复 identity 必须拒绝，编码按 module ID、placement ID 稳定排序。
非法 ID、unknown field、`null`、损坏 object、缺失安全字段、HOME mismatch、旧版本和过新版本
都严格拒绝。读取不执行 canonical rewrite。

## Ownership 规则

- State 以 module ID 和 placement ID 的复合 identity 索引扁平 record。
- State 与绝对 HOME 绑定；HOME 不一致时拒绝 ownership mutation。
- State 不绑定当前 repository path。仓库移动使 desired destination 改变，按普通 link update
  处理。
- Link ownership 只依赖 target、resolved target 和 raw link destination。
- Local state 只用于退出 desired 时提示，不提供修改或删除权限。
- State 成功后的内容必须反映本轮已验证结果；内部如何计算待发布账本不属于持久格式契约。
- State 的提交时机由 [`mutation-and-recovery.md`](mutation-and-recovery.md#安全规则) 定义。
- Unknown field、缺失安全字段、损坏结构或过新版本拒绝 mutation。安全字段指顶层 `version`、
  `home`、`records`，以及每条 record 的 `module_id`、`placement_id`、`kind`、`target`；link 另需
  `resolved_target` 与 `link_destination`。
- State missing 按空账本继续，因此无法发现已从 manifest 删除的历史 link；对应 input
  warning 的公开投影由 [`cli.md`](cli.md#status-与-dry-run) 定义。
