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
            └── Brewfile         # 由 setup.sh 消费;经 watch 参与指纹 [M2]
```

两条目录约定:**模块目录内部即 target 相对路径**——`modules/zsh/.config/zsh/.zshrc` 在
`target = "~"` 下落地为 `~/.config/zsh/.zshrc`;**模块顶层 `hooks/` 是保留目录**——存放
脚本与其数据文件,内置忽略,绝不被链接。Brewfile 放模块内而非仓库根,维持「模块自包含」。
模块文件树内**不允许出现 symlink 与特殊文件**(fifo/socket/设备),enumerate 报错
(03 号文档 §6)。

## 2. 目标机器上的磁盘路径与锁边界

| 路径 | 内容 | 权限 | 覆盖方式 |
|---|---|---|---|
| `~/.local/bin/dot` | CLI 二进制 | 0755 | — |
| `~/.local/share/dot/repo/` | 仓库克隆位置 | — | 机器配置 `repo` / `DOT_REPO` / `--repo` |
| `~/.config/dot/config.toml` | 机器配置(不入库) | **0600** | `DOT_CONFIG` |
| `~/.local/state/dot/` | state 目录 | **0700** | 随 `--home` 联动 |
| `~/.local/state/dot/state.json` | state 清单 | 0600 | 同上 |
| `~/.local/state/dot/lock` | 单实例 flock 锁文件(ADR-19) | 0600 | 同上 |
| `~/.local/state/dot/backup/<RFC3339Nano-rand>/…` | 覆盖前备份(`O_EXCL` 排他创建) | **目录 0700 / 文件保留原 mode** | 同上 |

**锁边界**:mutation 命令(`apply` `add` `init` `update`,以及 [M2] 的
`state rebuild`、`edit`)持锁;`update` 自 `git pull` 起即持锁(pull 改仓库、planner
读仓库,读写同锁);`diff` / `status` / `doctor` 只读**不取锁**——state 经原子 rename
写入,只读方看到的永远是完整的旧版或新版,无撕裂。**锁不可重入,进程内只获取一次**:
cli 层顶层命令取锁,内部编排(init/update 调 apply)调用假定已持锁的 `applyLocked()`,
引擎入口自身不取锁——flock 对同进程的新 fd 会阻塞,双重获取即实现级自锁。

优先级:命令行 flag > 环境变量 > 机器配置 > 内置默认。全部路径解析收敛在
`internal/paths` 一处,并支持隐藏全局 flag `--home <dir>` 将 `~` 与上表全部路径整体重定向
——这是集成测试的地基(08 号文档)。`paths` 同时是 **hook 子进程环境的唯一来源**:executor
启动 hook 时由 `paths` 提供 `HOME`、`XDG_CONFIG_HOME`、`XDG_STATE_HOME`、`XDG_DATA_HOME`
覆盖注入,生产与测试走同一条代码路径。生产代码不允许绕过 `paths` 直接展开 `~`;`paths`
额外提供 `Display(abs) string`(转回 `~/` 形态)与 `Within(path, root) bool`(祖先判定,
禁止散落的字符串前缀比较)两个原语。规范化(`EvalSymlinks` 等)只发生在**创建侧**:写入
link_dest 之前做一次,之后的一切所有权比较都是字节比较(ADR-22)。

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
描述「我自己怎么装」。** init 重写该文件时:内存合并并校验 → 临时文件写入(0600)→
`Sync` → 原子 rename;任一步失败保留旧配置(04 号文档 §4.1)。

## 4. apply pipeline

```
 ①  lock      获取 flock;失败 → 报错「另一 dot 进程运行中」
 ②  requires  宽松预读顶层 manifest 仅取 requires → 版本检查
              (不满足 → 提示 self-update 退出;version=dev 放行 + 警告)
 ③  load      严格解码全部 manifest:未知键即错(ADR-16;doctor 例外走宽松模式)
              state 加载:缺失 = 全新;损坏/版本过新/语义校验失败 = fail closed(ADR-25)
 ④  resolve   profile → 模块集合(@ 展开、os 过滤、环/悬空检测、[target] 缺 GOOS 报错)
              部分 apply:校验 请求模块 ⊆ profile(ADR-18)
 ⑤  enumerate 遍历模块文件树:hooks/ 保留目录、路径合法性、ignore 规则过滤,
              定级 link | managed | scaffold(优先级见 03 号文档 §3)
 ⑥  render    全部模板 parse + 渲染,fail-fast:任一失败整体退出,不进入执行(ADR-24)
 ⑦  scan      观测每个 target 的现势 → Observed 快照(唯一读 target 的阶段)
 ⑧  decide    纯函数:desired × observed × state → []Action(05 号文档 §3 决策表)
              存在 Conflict → 全部 Prune 标记 deferred(ADR-20,plan 层完成)
 ⑨  validate  全局不变量:target 唯一性、前缀冲突(05 号文档 §5)
              + StateOp×Kind 合法组合断言(§6)
              违反 → 整体拒绝,一个动作都不执行,exit 1
 ⑩  execute   mkdir → create-link/render/scaffold/backup-replace/adopt
              → prune(仅收敛时;执行中出现 error 或 Precond 失配 → 全部转 deferred)
              → hooks(不受收敛门控,见 05 号文档 §8)
              每个 target mutation 执行前复核 Precond(ADR-23)
 ⑪  persist   state 原子落盘,释放锁
```

`dot diff` = ②–⑨ 后打印计划(无锁、不执行、不写 state;Adopt 与 deferred Prune 均如实
展示);`dot apply --dry-run` 同理但取锁。**scan 与 decide 的拆分是可测试性的关键**:
scan 是唯一做 target IO 的一段,把现实世界快照成值;decide 是决策表的直译纯函数,
单测直接喂内存构造的三元组、断言 `[]Action`,不碰磁盘。

## 5. Go 包结构

```
internal/
├── cli/          # cobra 命令定义 + 编排;每次 Execute 现建命令树(无包级单例)
├── paths/        # 全部路径解析、--home 重定向、hook 子进程环境、Display/Within
├── manifest/     # 两阶段加载(宽松预读/严格解码)、合并、profile 展开、路径合法性
├── planner/      # enumerate / render / scan / decide / validate;decide 与 validate 为纯函数
├── executor/     # 消费 []Action:symlink/原子写/备份/prune/hook 子进程;Precond 复核
├── state/        # state.json 读写(三态 + 语义校验)、flock、条目 CRUD、hash
├── tmpl/         # text/template 封装:变量注入、函数表、命名空间校验
├── gitx/         # git 子进程透传与仓库操作(pull、status --porcelain、ls-files)
└── fsutil/       # 原子写、备份复制、链接判定等底层原语
```

依赖方向自上而下单向:`cli → {manifest, planner, executor, state, tmpl, gitx} → {paths, fsutil}`。
`planner` 不 import `executor`;两者通过 `Action` 值类型通信。错误 → 退出码的映射集中在
`cli.Execute` 一处,深层包只返回带语义标记的错误。

## 6. 核心类型(强类型,禁止字符串直比)

```go
// 三个独立枚举;EntryKind/DesiredKind 的 JSON/展示序列化为字符串
type DesiredKind uint8 // KindLink | KindManaged | KindScaffold        —— 描述意图
type EntryKind   uint8 // EntrySymlink | EntryRendered | EntryScaffold —— 描述产物
type StateOp     uint8 // OpKeep | OpUpsert | OpDelete
// 对应关系 link↔symlink、managed↔rendered,经单一映射函数 entryKindFor(DesiredKind)

// scan 的输出:target 现势的不可变快照
type Observed struct {
    Kind     ObservedKind // Missing | Symlink | RegularFile | Dir | Special(fifo/socket/设备)
    LinkDest string       // Kind==Symlink 时,Readlink 原始值(不解析)
    Hash     string       // Kind==RegularFile 且条目需比对时的 sha256
    Mode     fs.FileMode  // Kind==RegularFile 时的权限(mode 漂移检测,ADR-26)
}

// decide 的输出、executor 的唯一输入:执行所需信息完备,executor 不读仓库、不做渲染
type Action struct {
    Kind        ActionKind   // CreateLink | Render | Scaffold | BackupReplace |
                             // Prune | Adopt | RunHook | Skip | Conflict
    Module      string
    Source      string       // 仓库内绝对路径(hook 则为脚本路径)
    Target      string       // 落地绝对路径
    DesiredKind DesiredKind  // 全部创建/替换动作与 Conflict 均携带
                             // (BackupReplace 靠它得知备份后建什么)
    Content     []byte       // Render/Scaffold/BackupReplace(managed/scaffold):渲染产物
    Mode        fs.FileMode  // 落地权限([files].mode,缺省 0644)
    LinkDest    string       // CreateLink/BackupReplace(link):将写入的确切字符串
                             // (planner 侧完成规范化,executor 逐字写入,ADR-22)
    Precond     Observed     // plan 时观测;执行前复核,失配 → 降级 Conflict(ADR-23)
    StateOp     StateOp      // 动作成功后对该 target 记账的处置
    NextEntry   *state.Entry // StateOp==OpUpsert 时要提交的记账
    Deferred    bool         // Prune:因未收敛而延迟,仅展示不执行(ADR-20)
    Fingerprint string       // RunHook:执行成功后写入 state 的指纹
    Reason      string       // 供展示:"source changed"、"mode"、"adopted"、"kind migrated"
}

// state.json 中的一条记录,详见 05 号文档 §2
type Entry struct {
    Module    string    `json:"module"`
    Kind      EntryKind `json:"kind"`                // symlink | rendered | scaffold
    Source    string    `json:"source"`              // 相对仓库根
    Hash      string    `json:"hash,omitempty"`      // rendered:上次产物 sha256
    LinkDest  string    `json:"link_dest,omitempty"` // symlink:创建时写入的确切字符串(ADR-22)
    AppliedAt time.Time `json:"applied_at"`
}
```

**StateOp × Kind 合法组合**(planner 出口断言、executor 防御式复检,违规即程序 bug,
直接报错终止):`OpDelete` ⇔ `Prune`;`OpUpsert` ⇒ `NextEntry != nil`,适用于
CreateLink/Render/Scaffold/BackupReplace/Adopt;`Skip`/`Conflict`/`RunHook`/deferred
Prune ⇒ `OpKeep`。

其余设计要点:**没有 `Error` 动作**——渲染失败在 pipeline ⑥ fail-fast(ADR-24),执行期
IO 失败由 executor 记录并触发 prune 延迟。**`Adopt` 泛化为 state-only 记账动作**:补录、
kind 迁移记账(05 号文档 §3.4)、元数据刷新都由它承载。**记账经 StateOp + NextEntry 随
动作提交**:executor 在动作成功后机械执行、失败不动 state——「迁移只在成功后提交」与
「绝不误删记账」由类型保证。`BackupReplace` 由 planner 在 `--force` 下直接产出(取代对应
Conflict),executor 无需理解 force 语义。hook 是 `Kind=RunHook` 的动作,携带
Fingerprint,参与统一计划展示,执行恒在最后阶段。中间目录创建(mkdir)不建模为 Action:
executor 在文件动作前按需 `MkdirAll` 真实目录(ADR-3),不记 state、不参与 prune。
