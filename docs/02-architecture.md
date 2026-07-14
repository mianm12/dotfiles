# 02 · 架构:布局、路径约定与数据流

## 1. 仓库布局

```
dotfiles/                        # 单仓库:CLI 源码 + 配置内容
├── go.mod
├── cmd/
│   └── dot/main.go              # 入口,注入 version
├── internal/                    # 见 §5
├── bootstrap.sh                 # 极薄引导脚本,见 07 号文档
├── dot.toml                     # 顶层 manifest,见 03 号文档
├── Brewfile                     # brew bundle 软件清单(由 hooks 消费)
├── docs/                        # 本设计文档集
└── modules/                     # ★ 配置收集目录
    ├── zsh/
    │   ├── dot.toml             # 模块 manifest(可选)
    │   ├── .zshenv
    │   └── .config/zsh/
    │       ├── .zshrc
    │       ├── aliases.zsh
    │       └── .zshrc.local.template     # scaffold:首次生成 ~/.config/zsh/.zshrc.local
    ├── git/
    │   ├── dot.toml
    │   └── .config/git/config.tmpl       # managed:每次 apply 渲染
    ├── vscode/
    │   ├── dot.toml             # 用 [target] 表处理 mac/linux 路径差异
    │   ├── settings.json
    │   └── keybindings.json
    ├── karabiner/               # dot.toml: os = ["darwin"]
    │   └── .config/karabiner/karabiner.json
    └── macos/
        ├── dot.toml             # os = ["darwin"],无文件、只有 hooks
        └── setup.sh             # defaults write…、brew bundle
```

约定:**模块目录内部即 target 相对路径**。`modules/zsh/.config/zsh/.zshrc` 在
`target = "~"` 下落地为 `~/.config/zsh/.zshrc`。这条约定消灭了绝大多数映射配置。

## 2. 目标机器上的磁盘路径约定

| 路径 | 内容 | 覆盖方式 |
|---|---|---|
| `~/.local/bin/dot` | CLI 二进制 | — |
| `~/.local/share/dot/repo/` | 仓库克隆位置 | 机器配置 `repo` 字段 / `DOT_REPO` / `--repo` |
| `~/.config/dot/config.toml` | 机器配置(不入库) | `DOT_CONFIG` |
| `~/.local/state/dot/state.json` | state 清单 | 随 `--home` 联动 |
| `~/.local/state/dot/backup/<RFC3339>/…` | `--force` 覆盖前的备份 | 同上 |

优先级:命令行 flag > 环境变量 > 机器配置 > 内置默认。全部路径解析收敛在
`internal/paths` 一处,并支持隐藏全局 flag `--home <dir>` 将 `~` 与上表全部路径整体
重定向到指定目录——这是集成测试的地基(见 08 号文档),生产代码不允许绕过 `paths` 直接
展开 `~`。

## 3. 机器配置文件(不入库)

```toml
# ~/.config/dot/config.toml —— 由 dot init 生成
profile = "mac"                       # 本机使用的 profile
# repo = "~/src/dotfiles"             # 可选:仓库位置覆盖

[data]                                # 模板变量,见 06 号文档
email = "me@example.com"
machine = "work-mbp"
```

三层各司其职:**顶层 manifest 定义「有哪些组合」,机器配置选择「我是谁」,模块 manifest
描述「我自己怎么装」。**

## 4. apply 数据流

```
                ┌────────────────────────────────────────────────┐
                │ 输入                                            │
                │  · 顶层 dot.toml(profiles/defaults/requires)   │
                │  · 各模块 dot.toml + 模块文件树                  │
                │  · 机器配置(profile、[data])                   │
                │  · state.json(上次 apply 的产物记录)           │
                └───────────────┬────────────────────────────────┘
                                ▼
  ①  requires 检查 ── 版本不满足 → 报错退出,提示 self-update
                                ▼
  ②  resolve:profile → 模块集合(展开 @ 引用、按 os 过滤、去重)
                                ▼
  ③  enumerate:遍历模块文件树,应用 ignore 规则,
      按后缀/manifest 定级:link | managed | scaffold
                                ▼
  ④  plan:desired state 对比(实际文件系统 + state.json)
      → 动作列表:create-link / render / scaffold / prune / skip / conflict
                                ▼
  ⑤  execute:按安全策略执行(原子写、备份、绝不静默覆盖)
                                ▼
  ⑥  持久化 state.json(写临时文件 + rename)
```

`dot diff` = ①—④ 后打印计划;`dot apply --dry-run` 同理。planner 与 executor 严格分离:
**planner 是纯函数(不触碰文件系统写操作),executor 只消费动作列表**,这是可测试性的关键。

## 5. Go 包结构

```
internal/
├── cli/          # cobra 命令定义,薄:解析参数 → 调编排层
├── paths/        # 全部路径解析与 --home 重定向(唯一的 ~ 展开点)
├── manifest/     # TOML 解析、两级合并、profile 展开、requires 解析
├── planner/      # enumerate + plan,纯函数,输出 []Action
├── executor/     # 消费 []Action:symlink/原子写/备份/prune
├── state/        # state.json 读写、条目 CRUD、hash 计算
├── tmpl/         # text/template 封装:变量注入、函数表、渲染
├── gitx/         # git 子进程透传与仓库操作(pull、status)
└── fsutil/       # 原子写、备份复制、链接判定等底层原语
```

依赖方向自上而下单向:`cli → {manifest, planner, executor, state, tmpl, gitx} → {paths, fsutil}`。
`planner` 不 import `executor`;两者通过 `Action` 值类型(定义在 planner)通信。

## 6. 核心类型草案

```go
// planner 输出的动作,executor 的唯一输入
type Action struct {
    Kind   ActionKind // CreateLink | Render | Scaffold | Prune | Skip | Conflict
    Module string
    Source string     // 仓库内绝对路径(link/render/scaffold)
    Target string     // 落地绝对路径
    Reason string     // 供 diff/--dry-run 展示,如 "source changed"、"already linked"
}

// state.json 中的一条记录,详见 05 号文档
type Entry struct {
    Module    string    `json:"module"`
    Kind      string    `json:"kind"`   // symlink | rendered | scaffold
    Source    string    `json:"source"` // 相对仓库根
    Hash      string    `json:"hash,omitempty"` // rendered 产物 sha256
    AppliedAt time.Time `json:"applied_at"`
}
```
