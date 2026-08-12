# State 与 Ownership

## 职责

State 是 link ownership 的本机账本，不是 desired，也不保存 local provenance、配置内容、local
内容、秘密或环境变量。Local 从 example 首次创建后完全由用户拥有，state 不授予对 local 的覆盖
或删除权限。

## 版本与结构

State version 为 `4`。旧 state v1–v3 不受支持；锁内权威加载发现旧版本时 fail closed，并提示
用户通过 [`dot paths`](cli.md#paths) 定位 state 文件，在程序外归档或移除后重试。程序不提供兼容
读取、自动迁移或 reset 子命令。

`dot.toml`、machine config 与 state 使用相互独立的 version，当前分别为 `1`、`1`、`4`；三者
互不关联，state 升级不代表配置版本升级。

State 不存在时按空账本继续，并产生结构化 warning Issue；一旦存在，其最终目录项本身必须是
regular file。类型检查不跟随最终 symlink，因此 symlink-to-regular、dangling symlink、directory、
FIFO、socket 和 device 都必须在读取内容前失败。

逻辑结构：

```json
{
  "version": 4,
  "home": "/Users/user",
  "links": [
    {
      "module_id": "git",
      "placement_id": "config",
      "target": "/Users/user/.gitconfig",
      "resolved_target": "/Users/user/.gitconfig",
      "link_destination": "/Users/user/dotfiles/modules/git/gitconfig"
    }
  ]
}
```

`links` 是必需的 array；空账本使用空 array。每条 link 显式携带 module ID 与 placement ID，
两者组成唯一 identity；重复 identity 必须拒绝，编码按 module ID、placement ID 稳定排序。非法
ID、unknown field、`null`、损坏 object、缺失安全字段、HOME mismatch、旧版本和过新版本都严格
拒绝。读取不执行 canonical rewrite。

## Ownership 规则

- State 以 module ID 和 placement ID 的复合 identity 索引 link ownership。
- State 与绝对 HOME 绑定；HOME 不一致时拒绝 ownership mutation。
- State 不绑定当前 repository path。仓库移动使 desired destination 改变，按普通 link update
  处理。
- Link ownership 只依赖 target、resolved target 和 raw link destination。
- Active desired set 完整确定且 Plan 可执行时，planner 从全部 active desired links 直接计算完整
  NextState；executor 不从 Action 顺序增量推导或修补它。Blocked Plan 没有可提交 NextState。
- Adopt 与 repair-state 只改变 ownership；forget 只删除 ownership。三者都不修改 target，但属于
  可展示的语义 Action。
- 全部 target Actions 成功并复核后才能提交 NextState；blocker 或失败不得单独推进 state。提交
  时机由 [`mutation-and-recovery.md`](mutation-and-recovery.md#安全规则)定义。
- State missing 按空账本继续，因此无法发现已从 manifest 删除的历史 link；对应 warning Issue 的
  公开投影由 [`cli.md`](cli.md#status-与-dry-run)定义。
- 成功 apply 即使没有 active links，也提交合法的空 v4 state；相同输入再次 apply 必须不重写它。
