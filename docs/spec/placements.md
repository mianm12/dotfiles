# Placements 与路径边界

Placement ID 在所属 module 的 `links` 和 `locals` 中共同唯一。Source/example 必须显式声明；
`module.toml` 不会被隐式链接。

## Link

```toml
[[links]]
id = "config"
source = "config"
target = "~/.config/example/config"
```

- Source 相对于 portable root 或 selected variant root。
- Source 必须存在，其顶层对象只能是普通文件或目录，不得是 symlink 或 special；否则为配置
  错误、零 mutation。该约束针对 desired source，与 dangling actual 链的规划规则正交。
- Source 目录内部不递归检查，内部 symlink 由用户负责。
- 文件与目录都作为一个完整 symlink placement，不递归生成单文件 links。
- Target 必须以 `~/` 开头，不支持绝对路径、环境变量、glob 或命令替换。
- Target 规范化后必须位于逻辑 HOME 下。
- `dot` 创建指向绝对 source 的 symlink。

## Local

```toml
[[locals]]
id = "local"
example = "config.local.example"
target = "~/.config/example/config.local"
```

- Example 必须存在且为普通文件，只做字节复制；缺失或类型不符为配置错误、零 mutation。
- Local 的 create、keep、退出 desired 和 provenance 行为由
  [`planning.md`](planning.md#local) 定义。
- `*.local.example -> *.local` 是推荐命名，不是语法要求。

## 简化的路径唯一性

- Target 先展开 HOME 并做词法规范化。
- 对现存 ancestor symlink，解析到其实际父路径；missing suffix 按原名称追加。
- 两个 placements 的规范化 target 或解析后 target 相同时拒绝。
- Directory link 与其他 placement 的后代 target 互斥。
- Target 与 repository、machine config、state 或 lock 的规范化路径和解析后路径不得相等或
  互为祖先、后代；这些检查只使用规范化路径和上述 ancestor 解析，不建设通用控制面身份系统。
- Parent symlink 合法。Link state 保存其上次 resolved target；该值改变时拒绝 update/prune。

不额外探测 case sensitivity、Unicode alias、filesystem type 或 hard-link identity。
