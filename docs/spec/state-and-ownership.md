# State 与 Ownership

## 职责

State 是 link ownership 的本机缓存，不是 desired，也不是磁盘的真理。它不保存 local
provenance、配置内容、local 内容、秘密、环境变量或解析后的路径。Local 从 example 首次创建后
完全由用户拥有，state 不授予对 local 的覆盖或删除权限。

## 版本与结构

State version 为 `5`。v1–v4 不受支持；锁内权威加载发现旧版本时 fail closed，并提示用户通过
[`dot paths`](cli.md#paths) 定位 state 文件，在程序外归档或移除后重试。程序不提供兼容读取、
自动迁移或 reset 子命令。发现高于当前支持版本的 state 时同样 fail closed，但必须保留该文件
并改用支持该格式的新版 `dot`；旧二进制不得建议归档、删除或覆盖过新 state。

`dot.toml`、machine config 与 state 使用相互独立的 version，当前分别为 `1`、`1`、`5`；三者
互不关联，state 升级不代表配置版本升级。

State 不存在时按空账本继续，并写一条公开提示；一旦存在，其最终目录项本身必须是 regular
file。类型检查不跟随最终 symlink，因此 symlink-to-regular、dangling symlink、directory、
FIFO、socket 和 device 都必须在读取内容前失败。

逻辑结构：

```json
{
  "version": 5,
  "home": "/Users/user",
  "links": [
    {
      "module": "git",
      "placement": "config",
      "target": ".gitconfig",
      "dest": "/Users/user/dotfiles/modules/git/gitconfig"
    }
  ]
}
```

`links` 是必需的 array；空账本使用空 array。每条 link 显式携带 module ID 与 placement ID，
两者组成唯一 identity；重复 identity 必须拒绝。`target` 是规范 HOME-relative 词法路径，不带
`~/` 或前导 `/`；`dest` 是 `dot` 写入 symlink 的绝对 raw destination。编码按 module ID、
placement ID 稳定排序。

所有 link target 必须唯一，且不能相等或互为祖先/后代。违反该 antichain 不变量时整份 v5
state 无效；不得部分加载、自动分配 owner 或在循环里用 `forget` 修复。

## 解码

先读取顶层 `version`，再决定是否继续：

- 小于 `5`：按旧版本拒绝，建议归档；
- 大于 `5`：按过新拒绝，保留文件；
- 等于 `5`：严格解码当前形状。

只对 version `5` 拒绝 unknown field、`null`、损坏 object、缺失字段、非法 ID、非绝对或非规范
HOME、非绝对 dest、非规范 HOME-relative target、重复 key、target antichain 冲突与 HOME
mismatch。不先扫描整份文档的 UTF-16 代理项或重复 JSON 成员。
重复成员遵循 `encoding/json` 后写获胜，见
[`product.md`](product.md#明确接受的风险)。读取不执行 canonical rewrite。

## Ownership 规则

- State 以 module ID 和 placement ID 的复合 identity 索引。
- State 与绝对 HOME 绑定；HOME 不一致时拒绝 ownership mutation。
- State 不绑定当前 repository path。仓库移动使 desired dest 改变，按普通 `replace` 处理。
- 一条记录只证明：我们曾在这个 HOME-relative 词法 target 上写下过这个 dest。磁盘与账本冲突时以磁盘为准，
  判定规则由 [`planning.md`](planning.md#link) 拥有。
- 不保存 resolved target，不用解析结果证明所有权。
- Desired 完整且没有任何 `skip` 时，从已加载 state 克隆候选账本，按行折叠 ownership：
  `link` / `replace` / `record` 写入当前 target 与 dest；`remove` / `forget` 仅在候选账本仍
  匹配该旧记录时删除。全部行成功后，账本必须恰好包含全部 active desired links。有 `skip`
  或中途失败时不得提交。
- 成功 apply 即使没有 active links，也提交合法的空 v5 state；相同输入再次 apply 必须不重写
  它（控制文件权限修复除外，见 [`mutation-and-recovery.md`](mutation-and-recovery.md#安全规则)）。
- State missing 按空账本继续，因此无法发现已从 manifest 删除的历史 link；对应提示由
  [`cli.md`](cli.md#status-与-dry-run) 定义。
