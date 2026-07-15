# 05 · Apply 引擎:所有权、决策表与执行

## 1. 期望状态模型

对 profile 内每个模块,enumerate 产出文件条目,定级优先级见 03 号文档 §3
(内置忽略(含 hooks 引用)> `[files]` > ignore patterns > 后缀推断):

| 级别 | 判定 | apply 动作 |
|---|---|---|
| **link**(默认) | — | target 处创建指向仓库源文件的绝对路径 symlink |
| **managed** | `.tmpl` 后缀或 `kind="managed"` | 渲染模板,产物写入 target |
| **scaffold** | `.template` 后缀或 `kind="scaffold"` | target 不存在时渲染写入一次 |

目录不是动作对象:target 侧缺失的中间目录由 executor `MkdirAll` 创建为**真实目录**
(ADR-3),不记 state、不参与 prune。

managed 与 scaffold 的**渲染发生在 plan 阶段**(pipeline ⑥),且 **fail-fast**
(ADR-24):任一模板 parse 或渲染失败,整次运行报错退出,execute 不启动。产物 bytes
随 `Action.Content` 交给 executor——executor 不读仓库、不做渲染。

## 2. state.json 结构与三态处理

```json
{
  "version": 1,
  "entries": {
    "~/.config/zsh/.zshrc": {
      "module": "zsh",
      "kind": "symlink",
      "source": "modules/zsh/.config/zsh/.zshrc",
      "link_dest": "/Users/me/.local/share/dot/repo/modules/zsh/.config/zsh/.zshrc",
      "applied_at": "2026-07-14T10:00:00Z"
    },
    "~/.config/git/config": {
      "module": "git",
      "kind": "rendered",
      "source": "modules/git/.config/git/config.tmpl",
      "hash": "sha256:9f2c…",
      "applied_at": "2026-07-14T10:00:00Z"
    },
    "~/.config/zsh/.zshrc.local": {
      "module": "zsh",
      "kind": "scaffold",
      "source": "modules/zsh/.config/zsh/.zshrc.local.template",
      "applied_at": "2026-07-14T10:00:00Z"
    }
  },
  "run_once": {
    "macos/hooks/setup.sh": { "hash": "sha256:ab31…", "executed_at": "2026-07-14T10:00:01Z" }
  }
}
```

键统一存 `~/` 前缀的规范化路径(`paths.Display`)。`link_dest` 是 symlink 条目的
所有权存证:**创建链接时实际写入的确切字符串**——规范化只在创建侧做一次,此后一切
比较都是字节比较(ADR-22)。整文件读入、修改、临时文件 + rename 写回。

**三态处理(ADR-25)**:

| state 状况 | 行为 |
|---|---|
| 文件缺失 | 合法的全新开始:无历史可 prune、无 drift 检测,正常运行 |
| 解析失败(损坏) | **fail closed**:mutation 命令拒绝执行;`status`/`doctor` 可运行并报告;M1 恢复路径 = 手动 `mv state.json state.json.bak`(把「损坏」显式转化为「缺失」),重跑 apply 后由收养规则重建 |
| `version` 大于本 CLI 认识的 | 同上 fail closed。真实场景:self-update 后回滚二进制。提示升级 CLI 或手动处理 |

**不变量(ADR-18,v1.2 措辞)**:新建/更新的 state 条目必须属于本次 effective profile;
历史条目允许作为孤儿暂存(profile 刚切换、`--no-prune`、prune 被延迟时必然出现),由
prune 生命周期收敛。执行手段:`apply <module>` 与 `add` 的 profile 校验。

## 3. 所有权谓词与决策表

### 3.1 owned() 谓词(ADR-14 / ADR-22)

「state 有记录」只证明*曾经*创建。一切破坏性动作必须以下述谓词为前置:

```
owned(entry, observed) =
  kind == symlink : observed 为链接 ∧ observed.LinkDest == entry.link_dest   # 纯字节比较
  kind == rendered: observed 为普通文件 ∧ observed.Hash == entry.hash
  kind == scaffold: 恒 false(产物创建即归用户,ADR-12)
```

symlink 判定**不解析文件系统**(不用 EvalSymlinks):比较的是 `Readlink` 原始值与
创建时的存证。因此**悬空链接可判**——源文件被删后链接必然悬空,而这正是 prune 的
主场景;解析式判定在此恒失败,曾是 v1.1 的致命缺陷。

### 3.2 决策表(decide 纯函数的规格)

managed 条目已在 plan 阶段渲染完成,记产物 hash 为 `newHash`、期望权限为 `mode`。
link 条目的「期望 dest」= 本次将写入的字符串(创建侧规范化后的源绝对路径)。
规则**自上而下短路匹配**。

**link 条目**(源 S → 目标 T):

| # | 条件 | 动作 |
|---|---|---|
| L1 | T 不存在 | create-link(执行经裸 `os.Symlink`,EEXIST 即天然原子复核) |
| L2 | T 是 symlink ∧ Readlink == 期望 dest | skip;state 无记录 → **adopt**(自动,ADR-21:证据无歧义,删链最坏零损失) |
| L3 | T 是 symlink ∧ state 有记录 ∧ Readlink == entry.link_dest(≠ 期望 dest) | create-link(自家旧链:仓库搬家或源路径变化;重链 + 更新 link_dest,打印说明) |
| L4 | T 是 symlink ∧ state 有记录 ∧ Readlink ≠ entry.link_dest | **CONFLICT-drift**(链接被手工改指;`--force` → BackupReplace 恢复) |
| L5 | T 是 symlink 指向他处 ∧ state 无记录 | CONFLICT(含用户手工建的等价相对链接——写法不同即不匹配,保守是有意的) |
| L6 | T 是普通文件 / 目录 | CONFLICT(`--force` → BackupReplace;建议 `dot add` 收编) |

L3 与 v1.1 尾部启发式的区别:静默修复的资格来自 **state 精确存证**(记录的确是我们
写过的字符串),而不是路径长得像;无 state 的旧 stow 链接一律落 L5,正确。

**managed 条目**:

| # | 条件 | 动作 |
|---|---|---|
| M1 | T 不存在 | render |
| M2 | T 非普通文件(symlink/目录) | CONFLICT |
| M3a | actualHash == newHash ∧ Mode == mode ∧ state 有记录 | skip(记录 hash 过期则静默刷新) |
| M3b | actualHash == newHash ∧ Mode ≠ mode ∧ state 有记录 | render(reason="mode";重写同样字节顺带落权限,ADR-26) |
| M3c | actualHash == newHash ∧ state 无记录 | 默认 skip + 提示 adoptable;`--adopt` → Adopt(Mode 不符则改产出 render,一步完成记账与修权限) |
| M4 | state 无记录(且 actual ≠ new) | CONFLICT |
| M5 | actualHash == 记录 hash(产物未被手改) | render(源或变量变更) |
| M6 | 其余(actual ≠ 记录 ∧ actual ≠ new) | **CONFLICT-drift**(手改;`--force` → BackupReplace 重渲染) |

M3c 是收养不对称的落点(ADR-21):内容巧合不构成所有权,自动收养会让用户文件在
模板删除/模块退出 profile 后变成可自动 prune 的产物——这正是 v1.1 被审查抓住的越权。

**scaffold 条目**:

| # | 条件 | 动作 |
|---|---|---|
| S1 | T 存在(任意形态) | skip(永不覆盖) |
| S2 | T 不存在 ∧ state 有记录 | skip + 提示(用户有意删除;`--force` 重建) |
| S3 | T 不存在 ∧ state 无记录 | scaffold |

### 3.3 prune(方向相反:从 state 找孤儿)

**作用域**:全量 apply → 全部 state 条目;`apply <module...>` → 仅 `entry.module ∈ 请求集`。
候选 = 作用域内且不在本次 desired 的条目。

| # | 条件 | 动作 |
|---|---|---|
| P1 | kind == scaffold | 不删文件(ADR-12);state 摘除 + 提示 |
| P2 | owned(entry, observed) 为真 | prune(symlink 删链 / rendered 删文件) |
| P3 | owned 为假 | 不动文件;state 摘除 + 警告(对象已非我们所有) |

**收敛门控(ADR-20)**:prune 仅在创建阶段完全收敛后执行。plan 层:decide 产出的计划
中存在 Conflict(未被 `--force` 转化为 BackupReplace)→ 全部 Prune 标记
`Deferred=true`(diff/dry-run 如实展示「prune (deferred)」);执行层:创建阶段出现
error 或 Precond 失配降级 → 剩余及全部 prune 转 deferred;**整模块级孤儿**的交互确认
(y/N,`--yes` 跳过)被拒绝 → 同样本次全部延迟。deferred 不是 mutation,只是展示。
代价:无关模块的孤儿清理被一并推迟到下次干净运行——可接受,prune 是家务不是急务。

## 4. 崩溃与并发边界

- **单实例锁(ADR-19)**:mutation 命令对 `~/.local/state/dot/lock` 取 flock,失败即
  报错;`update` 自 `git pull` 起持锁。`diff`/`status`/`doctor` 只读不取锁——state
  经原子 rename 写入,只读方看到完整的旧版或新版。
- **崩溃恢复**:state 落盘原子;「已执行未记账」窗口由收养规则收敛——重跑 apply,
  L2 自动补录,M3c 提示后 `--adopt` 补录。父目录 fsync 级持久化不做(01 §4 已接受)。

## 5. 全局不变量校验(pipeline ⑨)

plan 完成、execute 之前,对 desired 全集做校验;**任一违反 → 整体拒绝执行,一个动作
都不做,exit 1**(配置 bug,与单文件 conflict 的「部分继续」语义不同):

1. **target 唯一**:按 target 绝对路径建 map,重复键报错并列出两个来源——跨模块撞车、
   `foo` 与 `foo.tmpl` 去后缀碰撞均在此被抓。
2. **无祖先冲突**:任一 desired **文件** target 是另一 target 的祖先(`paths.Within`,
   非字符串前缀)→ 报错。
3. **中间目录不穿文件**:target 落地路径的待建中间目录不得与任何 desired 文件条目重合。

`dot doctor` 复用同一校验;路径合法性(03 号文档 §6)在更早的解码阶段执行。

## 6. execute 顺序、Precond 复核与原子性

阶段顺序:**`mkdir → create-link / render / scaffold / backup-replace / adopt →
prune(仅收敛)→ hooks(不受门控)`**(ADR-13/20)。

**Precond 全量复核(ADR-23)**:每个 target mutation(CreateLink/Render/Scaffold/
BackupReplace/Prune)执行前,用 `Action.Precond` 复核现势——`Missing` 必须仍
Missing;Symlink 必须仍同 LinkDest;RegularFile 必须仍同 Hash。失配 → 该动作降级
Conflict(计入未收敛 → prune 延迟),绝不带着过期观测去覆盖。CreateLink@Missing 的
复核是免费的:裸 `os.Symlink` 遇到已存在目标天然 EEXIST,无需 temp+rename;其余
rename 覆盖路径复核后仍有微秒窗口,为已接受剩余风险(01 §4)。flock 只防另一个 dot,
防不了编辑器与应用程序——这正是复核必须扩展到全部 mutation 的原因。

**--force 公式(定稿)**:

```
自动替换(L3 重链、M5 重渲染等) = owned/记录证据 ∧ Precond 复核通过
强制替换(BackupReplace)        = 显式 --force ∧ 备份成功 ∧ Precond 复核通过
```

`--force` 由 planner 消化:对应 Conflict 直接产出为 `BackupReplace`(executor 不理解
force 语义);**--force 不作用于 prune**——prune 永远只删 owned(P2)。

原子性原语(`internal/fsutil`,executor 不写任何裸 `os.WriteFile`):

- 写文件:同目录临时文件(`CreateTemp(dir, ".dot-tmp-*")`)→ 写入 → `Chmod` 到目标
  权限 → `Sync` → `Rename`。
- 换链接:`Symlink` 到临时名 + `Rename` 覆盖(同卷 rename 原子);新建链接直接
  `os.Symlink`(见上)。
- 备份:copy + hash 校验(不用 rename,规避 EXDEV),目录 0700、文件 0600;备份失败
  则放弃该 BackupReplace。
- state 保存在所有动作之后一次落盘;中途 error 时把已执行动作的记账立即落盘一次,
  最小化未记账窗口。

## 7. 安全策略汇总

1. **绝不静默覆盖非托管文件**:conflict 三态(报错 / `--force` 备份覆盖 / 建议
   `dot add` 收编)是铁律;被改指的链接同样按 conflict 处理(L4/L5)。
2. **破坏性动作 = owned() ∧ Precond;全部 mutation = Precond 复核**双重前置。
3. **prune = owned ∧ 收敛 ∧(整模块时)确认**三重前置;--force 不豁免。
4. 路径写操作前校验:规范化后必须位于 target 根之下,M1 直接拒绝 `~` 之外的 target
   (`--allow-outside-home` [M3]);manifest 路径合法性见 03 号文档 §6。
5. 敏感面权限:机器配置 0600、state 目录 0700、backup 0700/0600。

## 8. hooks:run_once 语义

- 声明形态见 03 号文档 §3:字符串 [M1],或 `{ script, watch = [...] }` [M2]。
- **执行指纹** = 对 `(路径, 内容长度, 内容)` 元组序列(script 在前、watch 按声明序)
  做 sha256,长度前缀编码——消除裸内容拼接的边界歧义。RunHook 动作携带 Fingerprint,
  **成功后**写入 state 键 `run_once["<module>/<script>"]`;失败不写,下次重试。
- **at-least-once,脚本必须自我幂等**:外部效果成功、指纹落盘前崩溃会重跑
  (brew bundle、defaults write 天然幂等,要求不苛刻)。此为文档化契约。
- **不受收敛门控**:hooks 与文件收敛无破坏性关联,且新机器常见「预置 .zshrc 引发
  conflict」的场景不应阻塞软件安装。指纹未变照常跳过。
- 子进程环境由 `paths` 注入:`HOME`、`XDG_CONFIG_HOME`、`XDG_STATE_HOME`、
  `XDG_DATA_HOME`(`--home` 下指向假 home),外加 `DOT_MODULE`、`DOT_OS`、
  `DOT_PROFILE`、`DOT_REPO`、`DOT_TARGET`。
- 脚本非零退出:apply 整体退出码 1(不回滚文件动作);`--dry-run` 只打印。
- `update` 拉取到的新 hook 不做单独确认(01 §4 威胁模型);审查方式:
  `dot update --no-apply` + `dot diff`。

## 9. add 的反向映射算法

输入 `dot add <abs-path>`(先 normalize、解 `~`):

```
0. path 匹配 *.local → 硬拒绝(06 号文档 §2 约定)。
1. path 已在 state 中 → 报错「已被管理」。
2. 候选收集:遍历当前 profile 的模块(os 过滤后),path 位于其 target 根之下者,
   得候选 (module, 模块内相对路径)。
3. 亲缘加权:候选中,state 显示已管理 path 同目录或祖先目录下其他文件的 → 「强候选」。
4. 决策:恰一强候选 → 采用;零强候选且恰一候选 → 采用;
   其余 → 退出码 3,列出候选,要求 -m(-m 须满足模块名合法性)。
5. profile 校验(ADR-18):目标模块 ∉ profile → 报错;--activate 则把模块名追加进
   当前 profile 列表并提示 commit。
6. 执行(按模式分支):
   默认(link):   copy → modules/<m>/<rel>;校验副本 hash;删除原文件;
                   os.Symlink 建链;登记 kind=symlink + link_dest。
   --template:    copy → modules/<m>/<rel>.tmpl;原文件留在原位作为产物;
                   登记 kind=rendered(hash = 当前内容);打印「请将机器相关值替换为
                   {{ .var }}」——替换后渲染一致则下次 apply skip,闭环。
   --scaffold:    copy → modules/<m>/<rel>.template;原文件保留为产物;
                   登记 kind=scaffold。
   最后统一打印「记得 dot git add && commit」。
```

推断刻意保守(宁可要求 `-m`):add 移动/登记的是用户真实文件,猜错模块的代价远大于
多敲一个 flag。

## 10. 幂等契约(精确表述)

**任意初始状态下、以相同 flag 连续两次 apply,第二次的动作列表不得包含任何 mutation
动作(create-link / render / scaffold / backup-replace / prune 执行)与 Adopt。**
允许出现:Skip、既有 Conflict、以及因 conflict 持续存在而稳定复现的 deferred Prune
——它们不改变文件系统,不违反契约。这是 08 号文档集成测试对每个场景的通用附加断言。
