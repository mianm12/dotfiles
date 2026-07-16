# 02 · 架构:布局、路径约定与 Pipeline

## 1. 仓库布局

```
dotfiles/                        # 单仓库:CLI 源码 + 配置内容
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

CLI 源码也位于此仓库,但其目录与内部包划分不属于配置仓库契约。

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
| `~/.local/state/dot/lock` | 单实例锁文件(ADR-19) | 0600 | 同上 |
| `~/.local/state/dot/backup/<RFC3339Nano-rand>/…` | 覆盖前备份(新路径不得覆盖既有备份) | **目录 0700 / 文件保留原 mode** | 同上 |

**锁边界**:实际执行 mutation 的命令(`apply` `add` `init` `update`,以及 [M2] 的
`state rebuild`、`edit`)与全部 `dot git` 调用持锁;`update` 在仓库洁净检查前取锁,
覆盖随后 pull 与 apply;
`diff` / `status` / `doctor` / `version` 以及命令的 `--dry-run` 形态严格只读、**不取锁**——state 经原子替换写入,只读方看到的
永远是完整的旧版或新版,无撕裂。一次 mutation 的完整周期必须处于同一锁所有权下;
update → apply 等嵌套流程必须复用该所有权且不能自锁。锁原语和函数边界由实现决定。

优先级:命令行 flag > 环境变量 > 机器配置 > 内置默认。全部路径解析必须收敛在同一职责
边界,并支持隐藏全局 flag `--home <dir>` 将 `~`、状态路径和 hook 子进程环境整体重定向。
该边界必须统一提供路径展示、祖先/重叠判断和创建侧规范化能力,禁止各组件自行做字符串
前缀判断。写入 link_dest 前完成一次创建侧规范化,之后所有权比较只使用存证字符串
(ADR-22);helper 名与系统调用属于实现细节。

控制面路径输入必须在首次 IO 前得到唯一、绝对且不依赖当前工作目录的解释:

- 未传 `--home` 时,effective HOME 是平台为当前用户报告的绝对 home;无法得到绝对路径即
  失败,不得回退 cwd。`--home` 只接受非空绝对路径,不做 `~` 展开,并覆盖该值。
- `--repo`、`DOT_REPO`、机器配置 `repo`、`DOT_CONFIG` 及 bootstrap 对应的仓库路径只接受
  非空绝对路径,或恰以 `~` / `~/` 开头的路径;后者相对 effective HOME 展开。其他 `~`
  形式和全部相对路径在任何写入或 Git 启动前拒绝。
- 展开、清理 `.`/`..` 后得到的绝对展示路径用于后续解析;文件系统别名、祖先 symlink 与
  重叠仍按本节的 target 身份规则判断,不能用词法清理代替身份校验。

除 `init` 的明确持久化语义外,flag 与环境覆盖只影响本次运行(04 号文档 §4.1)。

**target root** 是模块级落地目录根,**entry target** 是单个文件的最终落地路径;后文无
歧义处的 target 均指 entry target。**target 身份(ADR-35)** 与展示字符串分离:唯一性、
state 查找、desired/orphan 差集及祖先/重叠判断必须尊重当前文件系统的路径等价语义。
既有祖先 symlink 是用户文件系统拓扑的一部分:指向目录时允许使用,但由它形成的路径别名
仍必须合并为同一 target 身份;叶子 hard link 的不同路径仍是不同 target。两个 desired
路径若具有同一 target 身份,必须在 mutation 前作为碰撞整体拒绝;
单个历史 state key 若只是当前 desired 的别名,
必须作为同一条目迁移记账,不得同时进入 orphan。多个 state key 指向同一 target 属 state
语义损坏并 fail closed。对一个 target 的内容或 mode mutation 不得经共享 inode 连带改变
另一个 target 身份;无法保证隔离时必须拒绝或降级 conflict。如何识别大小写、Unicode、
祖先 symlink、hard link 与尚不存在的叶子由实现决定。

**控制面路径边界(ADR-33)**:根据本次运行的有效参数解析 repo 树、机器配置文件、state
家族(state 根及其 state/lock/backup 预定子路径)和已安装二进制。不同控制面家族之间不得
相等、互为祖先或以文件系统别名重叠;state 家族内部的预定包含关系是唯一例外。任何可能
写入或启动写子进程的命令都必须在首次写入(包括创建 lock)前确认这组**控制面家族彼此
隔离**。

任何可信的 desired entry target、state entry target 或 `dot add` 输入与任一控制面家族
重叠(位于其内、等于它或会覆盖其祖先)也必须拒绝;既有祖先 symlink 不得成为绕过方式。
这部分检查在相应输入将被消费前完成,不要求不消费 state 的恢复路径先理解 state:
`self-update`、`dot git` 与 `update` 的 pull 前置只校验控制面家族,不因损坏或版本过新的
state 被阻塞;后续一旦进入 apply 或其他 state 依赖阶段仍完整 fail closed。mutation 违反
其实际消费的边界时整体拒绝,doctor 只读诊断。如何组织校验由实现决定。
manifest/plan 校验、state 加载和 add 必须复用同一 target 身份与边界定义,不得分别维护
例外列表。

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
描述「我自己怎么装」。**机器配置的顶层只允许 `profile`、`repo`、`data`:profile 为必填
非空字符串,repo 为可选且符合本节控制面路径语法的非空字符串,data 为可选的字符串映射;
未知顶层字段、错误类型或非法值均是配置错误。`[data]` 中符合用户键规则的旧键即使已不再由当前 manifest 声明也
允许保留,但不参与模板输入。任何消费机器配置的命令都必须严格解码,不得忽略错误后回退
默认值;status/doctor 可以诊断,但不能把默认值伪装成有效配置。

init 更新该文件时必须严格读取旧配置,保留本次未指定的 profile/repo/data,对合并结果整体
校验后原子替换;任一步失败保留旧配置(04 号文档 §4.1)。临时文件协议由实现负责满足这一
性质。

## 4. apply pipeline

进入编号 pipeline 前,先严格读取解析 repo/profile/data 所需的机器配置,解析全部控制面家族
并完成 ADR-33 校验;失败时不得创建 lock 或产生其他写入。该只读前置不改变一次 mutation
的锁覆盖范围。

```
 ①  lock      获取进程间排他锁;失败 → 报错「另一 dot 进程运行中」
 ②  requires  宽松预读顶层 manifest 仅取必填 requires → 语法与版本检查
              (不满足 → 提示 self-update 退出;version=dev 仅放行版本比较 + 警告)
 ③  load      严格解码全部 manifest 与机器配置:未知键/非法类型即错
              (ADR-16;doctor 例外走诊断模式)
              state 加载:缺失 = 全新;损坏/版本过新/语义校验失败 = fail closed(ADR-25)
 ④  resolve   profile → 完整模块集合(@ 展开、os 过滤、环/悬空检测、[target] 缺 GOOS 报错)
              部分 apply:校验 请求模块 ⊆ profile,仅缩小文件动作/prune/hooks 作用域(ADR-18)
 ⑤  enumerate 得到完整 profile 的结构性 desired 供路径全局校验;请求作用域内条目进入动作计划
              遍历时应用 hooks/ 保留目录、路径合法性、ignore 与 kind 定级规则
 ⑥  render    动作作用域内全部模板 parse + 渲染,fail-fast:任一失败整体退出(ADR-24)
 ⑦  scan      观测每个 target 的现势,形成计划所用快照
 ⑧  decide    按 desired × observed × state 和 05 号文档 §3 决策表生成动作计划
              存在 conflict → 全部 prune 标记 deferred(ADR-20,plan 层完成)
 ⑨  validate  对完整 effective profile 校验 entry target 身份唯一、祖先冲突、控制面路径隔离
              + 对动作作用域校验状态处置语义一致(05 号文档 §5、本文 §6)
              违反 → 整体拒绝,一个动作都不执行,exit 1
 ⑩  execute   mkdir → create-link/render/scaffold/backup-replace/adopt
              → prune(仅收敛时;执行中出现 error 或 Precond 失配 → 全部转 deferred)
              → hooks(不受收敛门控,见 05 号文档 §8)
              每个 target mutation 提交时仍须满足 Precond(ADR-23)
 ⑪  persist   提交成功动作对应的 state 变化,原子落盘,释放锁
```

凡机器配置参与 repo/profile/data 解析,都必须在依赖其结果前完成上述严格校验。`dot diff`
= ②–⑨ 后打印计划(无锁、不执行、不写 state;adopt 与 deferred prune 均如实
展示);`dot apply --dry-run` 生成同一计划但保持无锁、无任何由 dot 发起的写入。计划必须基于明确的 target 快照;执行阶段为维持
Precond 可以重新观测或使用等价的原子文件系统保证,但不得借此改变决策或补充未计划的
动作。函数拆分由实现决定。

## 5. 组件职责边界

下表规定职责,不规定 Go package 或函数名:

| 组件职责 | 必须保证 |
|---|---|
| CLI 编排 | 解析参数、获取一次锁、组合命令流程、统一映射退出码(`dot git` 的 Git 进程退出码透传除外) |
| 路径边界 | 解析 target root/entry target 与控制面家族、判断身份/祖先/重叠、实施 HOME 边界、生成 hook 环境 |
| Manifest | 两阶段加载、合并、profile 展开、输入与路径校验 |
| 计划职责 | 枚举、渲染、观测并按决策表产生完整计划;不修改 target |
| 执行职责 | 只执行已决定的动作;不得重新解释 manifest,且动作提交时必须维持计划前提 |
| State | 校验、读取与原子提交 state;状态文件损坏时 fail closed |
| Template | 以显式输入完成确定性渲染 |
| Git/发布 | 透传 git 或执行文档规定的同步、更新协议 |

计划与执行职责之间必须通过自包含的动作计划通信;具体内部类型可由实现选择。
文件动作所需内容应在计划阶段准备完毕,hook 执行是读取仓库脚本的明确例外。深层组件
返回语义化错误,CLI 负责用户输出与退出码。

## 6. 动作与状态转换契约

计划阶段必须区分 target 的缺失、symlink、普通文件、目录和特殊文件,并保留决策与最终
复核所需的链接目标、内容摘要和权限。每个动作必须包含执行所需信息、观测前提和成功后的
状态处置;执行阶段不得靠再次读取 manifest 补齐语义。

| 动作结果 | state 条目处置 |
|---|---|
| create-link / render / scaffold / backup-replace 成功 | 写入与实际新产物一致的条目 |
| adopt 成功 | 只写入或刷新条目,不触碰 target;scaffold 补录不建立所有权 |
| 活动 prune 成功(包括只摘除非 owned/scaffold 记录) | 删除对应条目 |
| skip / conflict / deferred prune | 保留原条目不变 |
| 文件动作失败或最终前提失配 | 保留原条目不变;本次 prune 延迟 |
| run-hook 成功 | 文件条目不变;提交新的 run_once 指纹 |
| run-hook 失败 | 文件条目与旧指纹均不变 |

上述矩阵是规范;是否用枚举、构造器或运行时断言实现由代码决定。desired kind
(`link | managed | scaffold`)描述意图,state kind(`symlink | rendered | scaffold`)
描述产物,实现必须避免混用,但不限定内部类型。执行中已有动作成功而后续动作失败时,
必须提交可提交的成功结果;若 state 本身落盘失败,不得回滚已越过提交点的数据,而由
05 号文档规定的收养规则在重跑时收敛。
