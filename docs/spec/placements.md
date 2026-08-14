# Placements 与路径边界

Placement ID 使用 `[a-z0-9][a-z0-9_-]*`，并在所属 module 的 `links` 和 `locals` 中共同唯一。
Source/example 必须显式声明；`module.toml` 不会被隐式链接。

## Link

```toml
[[links]]
id = "config"
source = "config"
target = "~/.config/example/config"
```

- Source 相对于 portable root 或 selected variant root。
- Source 必须存在，其顶层对象只能是普通文件或目录，不得是 symlink 或 special；否则为配置
  错误且不执行 config、state、placement parent 或 target mutation。Lock-first 已经建立的私有
  state root/lock bookkeeping 可以保留。该约束针对 desired source，与 dangling actual 链正交。
- Selected root、source 与 example 只按声明路径做词法规范化、join 和 containment；绝对路径或
  `..` 逃逸是配置错误。`dot` 不解析 ancestor symlink 来建立另一份 source 身份。
  Source/example 的最终目录项仍按各自叶子类型规则用 `lstat` 校验。
- Source 目录内部不递归检查，内部 symlink 由用户负责。
- Link desired identity 由 source 路径决定，不比较 source 内容；仅发生同一路径下内容变化时
  不要求重建 symlink 或更新 state。
- 文件与目录都作为一个完整 symlink placement，不递归生成单文件 links。
- Target 必须以 `~/` 开头，不支持绝对路径、环境变量、glob 或命令替换。
- Target 规范化后必须位于逻辑 HOME 下。
- `dot` 创建指向绝对 source 的 symlink。

### 目录 source

- 目录 source 表示一个完整 symlink placement。`dot` 只规划、记录和验证 target 这一条目录
  symlink，不递归解释或记录 descendants。
- Source 内新增、删除或修改内容直接反映到 target，不需要再次 apply；通过 target 写入的内容
  也直接进入 repository 中的 source 目录。
- `dot` 不自动递归展开目录、不执行 tree folding，也不把共享目录复制到 target。
- 把已有 directory link 改为其 target 下的 leaf placements 时，必须满足
  [`planning.md` 的类型与层级迁移规则](planning.md#placement-类型与层级迁移)，不能在一次
  desired 变更中同时删除 parent link 并加入 descendants。

目录 link、真实目录加 leaf links 和 locals 的选型建议及操作示例见
[`管理 modules 与 placements`](../guides/manage-modules.md#文件-link目录-link-还是-leaf-placements)。

## Local

```toml
[[locals]]
id = "local"
example = "config.local.example"
target = "~/.config/example/config.local"
```

- Example 必须存在且为普通文件，只做字节复制；缺失或类型不符为配置错误且不修改 config、state、
  placement parent 或 target。
- Local 的 create、已存在 no-op 和无 state 行为由
  [`planning.md`](planning.md#local) 定义。
- `*.local.example -> *.local` 是推荐命名，不是语法要求。

## 路径身份与边界

- HOME、repository、target 以及进入 machine config 或 state 的路径都必须是有效 UTF-8；
  不支持只能用原始字节表示的文件系统路径，并在执行任何业务 mutation 前拒绝。
- Target 身份是去掉 `~/` 后的规范化 HOME-relative 词法路径，例如 `.config/app/config`。
  碰撞与嵌套只比较这一条字符串；观察、控制区比较和 mutation 才在固定 HOME 下派生绝对路径。
- 祖先 symlink 合法。内核在 `lstat` / `symlink` / `mkdir` 时跟随它们；`dot` 不把解析后的
  父路径或沿途链接记为身份，也不因两条不同词法路径落到同一磁盘位置而把它们当成同一个
  target。这是已接受风险，见 [`product.md`](product.md#明确接受的风险)。
- 每次收敛观察都对全部 effective placements 做同一次词法校验。任意两个 active desired
  词法路径相等或互为祖先/后代时，两条都标 `skip`，整批不写，见
  [`planning.md`](planning.md#通用决策规则)。不区分 link/local、source 是文件还是目录，
  也不依赖 actual target 当前类型。State-only stale records 不进入该集合。
- 不额外探测 case sensitivity、Unicode alias、filesystem type 或 hard-link identity。

### Control-path topology

控制区是三个词法前缀：

- repository 树（`machine` 里的绝对仓库路径）；
- config 根（machine config 的直接父目录）；
- state 根（state 与 lock 的共同直接父目录）。

规则：

- Machine config 必须是 config 根的直接子项。
- State 与 lock 必须是同一 state 根下两个不同的直接 sibling。
- Repository 的最终目录项必须是已存在的真实目录。Config 根与 state 根可以尚不存在；存在时
  最终目录项必须是真实目录，不得是 symlink。缺失的 config 根只由 `init` 发布 machine config
  时建立；缺失的 state 根可由 mutation 的取锁 bookkeeping 建立。更高层 ancestor symlink 合法。
- 三个前缀词法规范化后不得相等或互为祖先/后代。
- Active desired target 不得与任一前缀相等、包含它或位于其中；否则该 target 标 `skip`。
- 不解析控制路径的 ancestor/entry/resolved 交叉表示，不建立 control family 图。
- 三个前缀互相重叠时，无法安全形成循环：这是分析失败，不是 placement `skip`。只读命令
  也不在这种布局上猜测模块或账本。
- 已退出 desired 的 stale target 与控制前缀词法重叠时只允许 `forget`，不得 `remove`，见
  [`planning.md`](planning.md#stale-link)。
- Control 文件的公开发现入口及输出契约只由 [`cli.md`](cli.md#paths) 定义。
