# 02 · 架构:布局、路径约定与 Pipeline

## 1. 仓库布局

```
dotfiles/                        # 单仓库:CLI 源码 + 配置内容
├── go.mod
├── cmd/
│   └── dot/main.go              # 薄入口:os.Exit(cli.Execute(...))
├── internal/                    # 见 §5
├── bootstrap.sh                 # 带校验的引导脚本,见 07 号文档
├── dot.toml                     # 顶层 manifest,见 03 号文档
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
        ├── dot.toml             # os = ["darwin"],只有 hooks 无链接文件
        └── hooks/               # ★ 保留目录:不参与链接
            ├── setup.sh         # defaults write…、brew bundle
            └── Brewfile         # 由 setup.sh 消费;经 watch 参与 hash [M2]
```

两条目录约定:**模块目录内部即 target 相对路径**——`modules/zsh/.config/zsh/.zshrc` 在
`target = "~"` 下落地为 `~/.config/zsh/.zshrc`,这条约定消灭了绝大多数映射配置;**模块顶层
`hooks/` 是保留目录**——存放脚本与其数据文件(如 Brewfile),enumerate 时内置忽略,绝不
被链接到 target。Brewfile 放模块内而非仓库根,维持「模块自包含」原则。

## 2. 目标机器上的磁盘路径约定

| 路径 | 内容 | 权限 | 覆盖方式 |
|---|---|---|---|
| `~/.local/bin/dot` | CLI 二进制 | 0755 | — |
| `~/.local/share/dot/repo/` | 仓库克隆位置 | — | 机器配置 `repo` / `DOT_REPO` / `--repo` |
| `~/.config/dot/config.toml` | 机器配置(不入库) | **0600** | `DOT_CONFIG` |
| `~/.local/state/dot/` | state 目录 | **0700** | 随 `--home` 联动 |
| `~/.local/state/dot/state.json` | state 清单 | 0600 | 同上 |
| `~/.local/state/dot/lock` | 单实例 flock 锁文件(ADR-19) | 0600 | 同上 |
| `~/.local/state/dot/backup/<RFC3339>/…` | `--force` 覆盖前的备份 | **目录 0700 / 文件 0600** | 同上 |

backup 收紧权限的原因:`--force` 备份的可能正是含密钥的私有文件。

优先级:命令行 flag > 环境变量 > 机器配置 > 内置默认。全部路径解析收敛在
`internal/paths` 一处,并支持隐藏全局 flag `--home <dir>` 将 `~` 与上表全部路径整体重定向
——这是集成测试的地基(08 号文档)。`paths` 同时是 **hook 子进程环境的唯一来源**:executor
启动 hook 时由 `paths` 显式注入 `HOME`、`XDG_CONFIG_HOME`、`XDG_STATE_HOME`、`XDG_DATA_HOME`,
生产与测试走同一条代码路径,保证 `--home` 隔离对子进程同样生效。生产代码不允许绕过
`paths` 直接展开 `~`;`paths` 额外提供 `Display(abs) string`(转回 `~/` 形态)与
`Within(path, root) bool`(祖先判定,禁止散落的字符串前缀比较)两个原语。

## 3. 机器配置文件(不入库)

```toml
# ~/.config/dot/config.toml —— 由 dot init 生成,权限 0600
profile = "mac"                       # 本机使用的 profile
# repo = "~/src/dotfiles"             # 可选:仓库位置覆盖

[data]                                # 模板变量(用户键必须小写开头,见 06 号文档 §3)
email = "me@example.com"
machine = "work-mbp"
```

三层各司其职:**顶层 manifest 定义「有哪些组合」,机器配置选择「我是谁」,模块 manifest
描述「我自己怎么装」。**

## 4. apply pipeline

```
 ①  lock      获取 flock(~/.local/state/dot/lock);失败 → 报错「另一 dot 进程运行中」
 ②  requires  宽松预读顶层 manifest 仅取 requires → 版本检查
              (不满足 → 提示 self-update 退出;version=dev 放行 + 警告)
 ③  load      严格解码全部 manifest:未知键即错(ADR-16;doctor 例外走宽松模式)
 ④  resolve   profile → 模块集合(@ 展开、os 过滤、环/悬空检测)
              部分 apply:校验 请求模块 ⊆ profile,否则报错(ADR-18)
 ⑤  enumerate 遍历模块文件树:hooks/ 保留目录与 ignore 规则过滤,
              按优先级定级:link | managed | scaffold(优先级见 03 号文档 §5)
 ⑥  render    全部模板 parse + 渲染(fail fast;渲染只依赖显式输入,ADR-17)
 ⑦  scan      观测每个 target 的现势 → Observed 快照(唯一读 target 的阶段)
 ⑧  decide    纯函数:desired × observed × state → []Action(05 号文档 §3 决策表)
 ⑨  validate  全局不变量:target 唯一性、前缀冲突(05 号文档 §5)
              违反 → 整体拒绝,一个动作都不执行,exit 1
 ⑩  execute   mkdir → create-link/render/scaffold/adopt → prune → hooks(ADR-13)
              本次运行有 error → 跳过 prune;破坏性动作前按 Precond 复核(ADR-14)
 ⑪  persist   state 原子落盘,释放锁
```

`dot diff` = ①–⑨ 后打印计划(不执行、不写 state,Adopt 仅展示);`dot apply --dry-run` 同理。
**scan 与 decide 的拆分是可测试性的关键**:scan 是唯一做 target IO 的一段,把现实世界快照成值;
decide 是决策表的直译纯函数,单测直接喂内存构造的三元组、断言 `[]Action`,不碰磁盘。

## 5. Go 包结构

```
internal/
├── cli/          # cobra 命令定义 + 编排;每次 Execute 现建命令树(无包级单例)
├── paths/        # 全部路径解析、--home 重定向、hook 子进程环境、Display/Within
├── manifest/     # 两阶段加载(宽松预读/严格解码)、合并、profile 展开、requires
├── planner/      # enumerate / render / scan / decide / validate;decide 与 validate 为纯函数
├── executor/     # 消费 []Action:symlink/原子写/备份/prune/hook 子进程;Precond 复核
├── state/        # state.json 读写、flock、条目 CRUD、hash
├── tmpl/         # text/template 封装:变量注入、函数表、命名空间校验
├── gitx/         # git 子进程透传与仓库操作(pull、status、ls-files)
└── fsutil/       # 原子写、备份复制、链接判定等底层原语
```

依赖方向自上而下单向:`cli → {manifest, planner, executor, state, tmpl, gitx} → {paths, fsutil}`。
`planner` 不 import `executor`;两者通过 `Action` 值类型通信。错误 → 退出码的映射集中在
`cli.Execute` 一处,深层包只返回带语义标记的错误(sentinel / 错误类型)。

## 6. 核心类型

```go
// scan 的输出:target 现势的不可变快照
type Observed struct {
    Kind     ObservedKind // Missing | Symlink | RegularFile | Dir
    LinkDest string       // Kind==Symlink 时,原始 Readlink 值
    Hash     string       // Kind==RegularFile 且条目需比对时的 sha256
}

// decide 的输出、executor 的唯一输入:执行所需信息完备,executor 不读仓库、不做渲染
type Action struct {
    Kind    ActionKind  // CreateLink | Render | Scaffold | Prune | Adopt |
                        // RunHook | Skip | Conflict
    Module  string
    Source  string      // 仓库内绝对路径(hook 则为脚本路径)
    Target  string      // 落地绝对路径
    Content []byte      // Render/Scaffold:plan 阶段已完成的渲染产物
    Mode    fs.FileMode // 落地权限([files].mode,缺省 0644)
    Precond Observed    // plan 时的观测;executor 破坏性动作前复核,不符则降级警告跳过
    Reason  string      // 供 diff/--dry-run 展示,如 "source changed"、"adopted"
}

// state.json 中的一条记录,详见 05 号文档 §2
type Entry struct {
    Module    string    `json:"module"`
    Kind      string    `json:"kind"`   // symlink | rendered | scaffold
    Source    string    `json:"source"` // 相对仓库根
    Hash      string    `json:"hash,omitempty"` // rendered 产物 sha256
    AppliedAt time.Time `json:"applied_at"`
}
```

hook 升格为 `Kind=RunHook` 的动作参与统一计划:dry-run 与 diff 中和文件动作一起展示,
执行顺序由 executor 的阶段划分保证(hooks 恒最后)。中间目录创建(mkdir)不建模为
Action:executor 在文件动作前按需 `MkdirAll` 真实目录(ADR-3),不记 state、不参与 prune。
