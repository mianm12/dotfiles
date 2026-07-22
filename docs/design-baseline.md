# dot MVP 设计基线

> [!IMPORTANT]
> 本文是 `dot` 重设计后唯一具有规范性的产品与行为基线。当前代码仍是待替换的旧实现；
> README、代码和测试决定目前已经实现什么，本文不能作为实现完成的证据。

## 1. 目标与设计原则

`dot` 是个人使用的 macOS/Linux dotfiles 管理 CLI。它把分散的配置集中到 Git 仓库，通过
symlink 应用共享配置，通过 local example 初始化不进入 Git 的本机内容。

设计优先级依次是：

1. 不覆盖或删除用户已有数据。
2. 满足多机器、profile、平台差异和重复收敛的核心需求。
3. 保持实现简单，允许明确接受低概率边缘风险。
4. 不为假想的通用性、安全性或未来功能预建框架。

仓库、机器选择、state 和实际文件系统共同形成计划：

```text
Desired repository + Machine selection + State + Actual filesystem -> Plan
```

相同输入成功 apply 后再次执行必须是 no-op。

## 2. 范围与非目标

MVP 包含：

- macOS 与 Linux；Linux 重点支持 Ubuntu 和 Arch。
- profiles、modules、platform variants 和多个 placements。
- 文件或目录 symlink。
- `*.local.example` 到本机 local 文件的一次性复制。
- `init`、`status`、`apply`、`remove`、`version` 和 `help`。
- mutation dry-run、最小 ownership state 和单进程锁。

MVP 不包含：

- 软件、Homebrew、APT、Pacman 或系统包安装。
- HOME 外 target 声明。
- Profile 继承、include、exclude、覆盖或条件 DSL。
- Module 依赖、hook、脚本或任意命令执行。
- Template 渲染、环境变量注入、秘密管理或加密。
- 自动 Git pull、commit、push、release 或自更新。
- 自动导入普通文件、`add` 命令或自动修改仓库 manifest。
- Force、backup、rollback 或跨路径事务。
- Windows、daemon、watch、外部并发协调、JSON CLI 输出或完整 doctor。
- 跨 module ownership transfer。

## 3. 明确接受的风险

以下是产品边界，不是等待补齐的欠账：

- Repository、manifest 和本机配置由用户本人维护，不对抗恶意输入。
- 只用一把文件锁避免多个 `dot` mutation 并发；不协调编辑器、软件或用户同时修改文件。
- `status` 和 dry-run 不取锁，并发 mutation 时可能看到短暂中间状态；用户可以重跑。
- 路径唯一性只比较规范化路径和解析现存 ancestor symlink 后的路径。不保证识别所有大小写、
  Unicode normalization 或特殊文件系统别名。
- 不分析不同路径之间的 hard-link inode 关系；`dot` 不修改已有普通文件内容。
- 不提供断电事务或完整 durability 保证。原子文件发布用于避免暴露半写配置，不承诺跨多个
  target 的原子性。
- OS 或文件系统在计划后返回错误时，命令可以部分完成并失败；恢复方式是停止并重跑。
- State 丢失后可以恢复当前 desired，但无法发现已经从 manifest 删除的历史 link，只做警告。

## 4. 真相源与机器选择

### 4.1 Repository desired

仓库中的 `dot.toml`、`modules/<id>/module.toml` 和配置内容描述共享期望。

### 4.2 Machine config

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

### 4.3 State

State 是 link ownership 和 local provenance 的本机账本，不是 desired，也不保存配置内容、
local 内容、秘密或环境变量。

### 4.4 Actual filesystem

Target 使用 `lstat` 区分 absent、symlink、regular file、directory 和 special。Local 只关心
目录项是否存在，不读取或跟随已有对象。

## 5. 仓库布局与 Profile

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

- `version` 必填，MVP 只支持 `1`。
- Module ID 来自 `modules/<id>/`；module manifest 固定为 `module.toml`。
- Module、profile、variant 和 placement ID 使用 `[a-z0-9][a-z0-9_-]*`。
- Profile 值是 module ID 数组，不得重复。
- 多个 active profiles 只做集合并集，顺序不改变语义。
- 空 profile 和空 active profile 列表合法。
- Active profile 引用不存在的 module 时配置无效。
- CLI 不修改仓库 profile。

## 6. Platform 与 Module

Platform 是 resolver 的显式输入，测试必须能够注入合成值：

```toml
os = ["macos", "linux"]
distro = ["ubuntu", "arch"]
arch = ["x86_64", "aarch64"]
```

- 不同字段之间是 AND，同一字段数组内是 OR。
- 字段缺失表示不限制。
- `distro` 只允许与 `os = ["linux"]` 一起声明。
- 不支持否定、正则、优先级、fallback 或 capability 表达式。
- Profile 选中的 module 无匹配 variant 时是 not-applicable，不报错。
- Extra module 或显式 `apply <module>` 无匹配 variant 时失败。

Module 只能使用 portable 或 variants 其中一种模式，不得混用。

### 6.1 Portable

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

### 6.2 Variants

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
- 零个匹配表示 not-applicable；多个匹配是配置错误。
- Variant 完整声明自己的 placements，不继承其他 variant 或顶层 placements。

## 7. Placements 与路径边界

Placement ID 在所属 module 的 `links` 和 `locals` 中共同唯一。Source/example 必须显式声明；
`module.toml` 不会被隐式链接。

### 7.1 Link

```toml
[[links]]
id = "config"
source = "config"
target = "~/.config/example/config"
```

- Source 相对于 portable root 或 selected variant root。
- Source 顶层对象只能是普通文件或目录，不得是 symlink 或 special。
- Source 目录内部不递归检查，内部 symlink 由用户负责。
- 文件与目录都作为一个完整 symlink placement，不递归生成单文件 links。
- Target 必须以 `~/` 开头，不支持绝对路径、环境变量、glob 或命令替换。
- Target 规范化后必须位于逻辑 HOME 下。
- `dot` 创建指向绝对 source 的 symlink。

### 7.2 Local

```toml
[[locals]]
id = "local"
example = "config.local.example"
target = "~/.config/example/config.local"
```

- Example 必须是普通文件，只做字节复制。
- Target absent 时以 `0600`、完整且不可覆盖的方式发布。
- 任意目录项已经存在时一律 keep：不读取、不比较、不分类、不覆盖。
- Example 更新不触发 local 更新；local 被用户删除后下一次 apply 重新创建。
- Local 退出 desired 时永不删除；若 state 有记录则提示一次并忘记 provenance。
- `*.local.example -> *.local` 是推荐命名，不是语法要求。

### 7.3 简化的路径唯一性

- Target 先展开 HOME 并做词法规范化。
- 对现存 ancestor symlink，解析到其实际父路径；missing suffix 按原名称追加。
- 两个 placements 的规范化 target 或解析后 target 相同时拒绝。
- Directory link 与其他 placement 的后代 target 互斥。
- Target 不得等于或位于 repository、machine config、state 或 lock 路径内部；这些检查只使用
  规范化路径和上述 ancestor 解析，不建设通用控制面身份系统。
- Parent symlink 合法。Link state 保存其上次 resolved target；该值改变时拒绝 update/prune。

不额外探测 case sensitivity、Unicode alias、filesystem type 或 hard-link identity。

## 8. State 与 Ownership

重设计使用 state version `2`，用于明确区别当前旧实现已经使用但结构不兼容的 state v1。
MVP 不自动迁移旧 state；遇到 v1 时拒绝 mutation，并提示用户在 cutover 时人工归档旧 state。

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

规则：

- State 按 module 和 placement ID 组织。
- State 与绝对 HOME 绑定；HOME 不一致时拒绝 ownership mutation。
- State 不绑定当前 repository path。仓库移动使 desired destination 改变，按普通 link update 处理。
- Link ownership 只依赖 target、resolved target 和 raw link destination。
- Local state 只用于退出 desired 时提示，不提供修改或删除权限。
- State 成功后的内容必须反映本轮已验证结果；内部使用重建或局部更新不属于契约。
- State 只在选定 scope 成功后原子提交。
- Unknown field、缺失安全字段、损坏结构或过新版本拒绝 mutation。
- State missing 按空 state 继续，但警告无法发现已从 manifest 删除的历史 link。

## 9. 决策规则

### 9.1 Link

| State / Actual | 行为 |
|---|---|
| 无 / absent | create 并登记 |
| 有 / absent | 按当前 desired create |
| 无 / 正确 symlink | adopt，只写 state |
| 有 / 与 state、desired 一致 | keep |
| 有 / actual 仍等于旧 destination，desired 已改变 | update |
| 有 / actual 已等于新 desired，state 落后 | repair state |
| 无 / 错误 symlink | conflict |
| 有 / actual 已偏离 state | conflict |
| 任意 / regular、directory、special | conflict |
| 其他 module 已记录同一 target | conflict |

Stale link 只有在当前 target 仍是 symlink、resolved target 未改变且 raw destination 等于 state
记录时才允许 prune。Dangling symlink 仍按 raw destination 应用同样规则。

### 9.2 Local

| Actual | 行为 |
|---|---|
| absent | create |
| 任意已存在目录项 | keep |

Remove/prune 永不删除 local。

## 10. Public CLI

```text
dot init [REPOSITORY] --profile NAME... [--dry-run]
dot status [MODULE]
dot apply [MODULE] [--dry-run]
dot remove MODULE [--dry-run]
dot version
dot help
```

### 10.1 Init

- Repository 省略时使用当前目录，并且必须存在有效 `dot.toml`。
- Init 写入 repository 与 active profiles，然后执行首次全量收敛。
- Preflight 失败时不写机器配置或 artifacts。
- 机器配置提交后 apply 失败时保留 selection，用户通过 `dot apply` 重试。
- 已初始化时 MVP 拒绝再次 init，不提供 reconfigure/rebind。

### 10.2 Apply

- `dot apply` 收敛全部 effective modules，并处理 state 中不再 active 的 stale links。
- `dot apply <module>` 对 active module 做 scoped apply。
- 未 active 的 module 在 preflight 成功后加入 `extra_modules` 再收敛。
- Module 不存在、不适用或与其他 effective module/state target 冲突时，不修改 selection。
- Scoped apply 只需检查目标 module 与其他 effective modules/state 的冲突，不要求无关 module
  之间重新证明所有关系。

### 10.3 Remove

- Active profile 仍选择 module 时拒绝，不修改 selection 或文件系统。
- Extra module 先从 prospective selection 移除，通过 preflight 后写回配置。
- 删除 state 证明、resolved target 未改变且 raw destination 未漂移的 module links。
- 保留所有 local，并在 state 可用时提示。
- Manifest 已删除但 extra/state 仍有 module 记录时允许清理。
- 已 inactive 且无 state 时成功 no-op；完全未知的 module 失败。

### 10.4 Status 与 dry-run

- Status 只读，显示 module activation、variant 和 `converged`、`pending`、`conflict`、
  `not-applicable`、`inactive` 或 `stale`。
- 默认 status 即使发现 pending/conflict 仍返回成功；MVP 没有 `--check`。
- Dry-run 使用与真实命令相同的解析、resolution 和 planner，但不写 config、state、target、
  parent directory、lock 或 temporary file。
- Status 和 dry-run 不取锁；并发 mutation 时结果是 best-effort snapshot。
- 真实命令总是重新规划，不执行保存的 dry-run plan。

## 11. Mutation、锁与恢复

```text
acquire mutation lock
  -> load strict config/state/manifests
  -> build prospective selection
  -> resolve desired and observe actual
  -> validate supported path conflicts
  -> build plan
  -> persist changed selection
  -> create parents
  -> create missing locals and new links
  -> update owned links
  -> prune stale owned links
  -> re-read changed targets
  -> atomically commit state
```

规则：

- Deterministic config、path 或 ownership conflict 在 mutation 前失败，选定 scope 零写入。
- 不建立通用 action snapshot 或 precondition 系统。
- Local 和新 link 使用不可覆盖创建语义；target 已出现时停止。
- Update/prune 删除 symlink 前重新读取 resolved target 和 raw destination；与 state 不同则停止。
- 新 target 创建和 update 全部成功后才开始 prune。
- Changed target 重新读取符合预期后才进入 state；不建设独立 postcondition framework。
- State 最后原子提交；提交失败时命令失败。
- 不提供 rollback。失败时保留已经完成的安全动作，报告可能部分应用并要求用户重跑。
- Mutation commands 使用同一把稳定 advisory file lock。Lock busy 作为普通运行时失败。

必须能通过重跑处理：

| 中断后事实 | 下一次 apply |
|---|---|
| link 已创建、state 未提交 | adopt |
| link 已更新、state 仍是旧 destination | repair state |
| update 删除旧 link 后中断 | create 当前 desired |
| prune 已完成、state 仍有记录 | forget stale state |
| selection 已更新、artifact 未完成 | 继续收敛 selection |
| local 已完整发布、state 未提交 | keep 并登记 |

## 12. 输出与退出码

正常结果、status 和 dry-run plan 写 stdout；错误写 stderr。不得输出 local 内容、配置内容或
秘密。

| Exit code | 含义 |
|---:|---|
| `0` | 成功，或有效 status/dry-run |
| `1` | 配置、ownership、lock、文件系统或运行时失败 |
| `2` | CLI 参数或用法错误 |

运行时失败不要求维护完整的 completed/failed/not-attempted 结果协议。错误信息必须指出失败
动作；已经发生 mutation 时提示本轮可能部分完成并建议重跑，不得把未执行动作显示为成功。

## 13. MVP 验收

至少验证：

1. 空白 macOS/Linux 机器按 profiles init，第二次 apply no-op。
2. Init 遇到已有普通 link target 时 preflight 零 mutation。
3. Profile module 无匹配 variant 时 skip；显式 apply 时失败且不加入 extras。
4. Link source 只改变内容时 symlink 和 state no-op。
5. Placement 新增时 create，删除时只 prune 有 state 证据且未漂移的 link。
6. Link target 改变时先建立新 target，再 prune 旧 target。
7. `apply <module>` 激活 extra module，重复 apply no-op。
8. `remove <module>` 取消 extra、删除 owned links、保留 locals；profile module remove 被拒绝。
9. Local 只在 absent 时创建；任何已有目录项都 keep；example 更新不覆盖。
10. 正确未知 symlink adopt；state-owned symlink 被改指后 conflict。
11. Parent symlink 改变 resolved target 后 update/prune 被拒绝。
12. 精确 target 或解析后 target 冲突在 preflight 阶段零 mutation。
13. Selection、local create、link create/update、prune 和 state commit 中断后可重跑收敛。
14. State missing 可以警告并继续；state corrupt、v1 或 too-new 时拒绝 mutation。
15. 第二个 mutation process 失败；status/dry-run 不创建 lock，且 dry-run 严格零写入。

所有成功 mutation 场景都追加一次相同 apply，并断言不再发生文件系统 mutation。

测试使用绝对路径的合成 HOME、repo、config、state 和 lock，不读取或写入真实私人配置。

## 14. 实现边界

MVP 使用 Go。优先使用标准库和窄职责依赖；计划采用 Cobra、`go-toml/v2`、`gofrs/flock`，
并在实现 state/config 原子发布时评估 `renameio/v2`。不引入 Viper、虚拟文件系统、DI、事务、
workflow、state-machine、日志或通用 dotfiles framework。

以下内容由实现与测试决定，不属于产品契约：

- 内部 package、struct、interface、函数和错误类型。
- State JSON 缩进、字段顺序和可选诊断字段。
- Config/state/lock 的精确路径。
- 原子 local publish 与 link update 的具体系统调用。
- 测试 fixture、故障注入方法和人类输出排版。

只有真实用户故事、已发生故障或不可接受的数据损失路径可以扩大本基线。若实现反馈要求增加
新的安全证明、通用抽象、持久化字段或公开行为，应先说明具体失败案例、实现成本和不实现的
现实后果，再决定是否修改设计。
