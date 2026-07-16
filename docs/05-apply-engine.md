# 05 · Apply 引擎:所有权、决策表与执行

## 1. 期望状态模型

对 profile 内每个模块,enumerate 产出文件条目,定级优先级见 03 号文档 §3
(内置忽略(含 hooks 引用)> `[files]` > ignore patterns > 后缀推断):

| 级别 | 判定 | apply 动作 |
|---|---|---|
| **link**(默认) | — | target 处创建指向仓库源文件的绝对路径 symlink |
| **managed** | `.tmpl` 后缀或 `kind="managed"` | 渲染模板,产物写入 target |
| **scaffold** | `.template` 后缀或 `kind="scaffold"` | target 不存在时渲染写入一次 |

模块顶层 `hooks/` 为保留目录,连同 `[hooks]` 引用的脚本路径一起被排除在枚举之外。
目录不是动作对象:执行时按需创建 target 侧缺失的中间目录,且必须是**真实目录**
(ADR-3);这些目录不记 state、不参与 prune。

managed 与 scaffold 的**渲染发生在 plan 阶段**(pipeline ⑥),且 **fail-fast**
(ADR-24):任一模板 parse 或渲染失败,整次运行报错退出,execute 不启动。执行计划必须
自包含渲染产物;进入执行阶段后不得重新读取模板或再次渲染。

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
      "hash": "<algorithm>:<digest>",
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
    "macos/hooks/setup.sh": { "hash": "<algorithm>:<digest>", "executed_at": "2026-07-14T10:00:01Z" }
  }
}
```

键统一使用 `~/` 前缀的规范化**展示形式**,但查找、唯一性与 orphan 判定使用 ADR-35 的
target 身份。单个历史 key 若只是当前 desired 的文件系统别名,必须作为同一条目迁移记账,
不得另列 orphan;多个 state key 指向同一 target 则语义校验失败。`link_dest` 是 symlink 条目的
所有权存证:**创建链接时实际写入的确切字符串**——规范化只在创建侧做一次,此后一切
比较都是字节比较(ADR-22)。state 更新必须原子替换,读取方只能看到完整旧版或完整新版。

**加载校验与三态处理(ADR-25)**:JSON 解析之外必须做语义校验——target 键规范且位于
HOME 内并避开控制面路径;module 名合法;source 是规范化的 `modules/<module>/…` 相对
路径;kind 合法;symlink 的 `link_dest`、rendered 的 hash 等证据存在且格式有效。用于
所有权、最终 Precond 与备份校验的 hash 必须携带受支持的算法标识,并使项目生命周期内的
偶然碰撞可忽略;未知或不满足该性质的标识必须 fail closed,已有标识必须持续可读或迁移。
具体算法与编码由实现选择。历史 orphan 不要求 source 当前仍存在。任一违规视同损坏。

| state 状况 | 行为 |
|---|---|
| 文件缺失 | 合法的全新开始:无历史可 prune、无 drift 检测,正常运行 |
| 解析失败 / **语义校验失败**(损坏) | **fail closed**:普通 mutation 拒绝执行;`status`/`doctor` 可诊断;M1 手工备走损坏文件后重跑 apply;`state rebuild` [M2] 是保留原文件后重建的受控例外 |
| `version` 大于本 CLI 认识的 | 同上 fail closed。真实场景:self-update 后回滚二进制。提示升级 CLI 或手动处理 |

**不变量(ADR-18)**:新建/更新的 state 条目必须属于本次 effective profile;历史条目
允许作为孤儿暂存(profile 刚切换、`--no-prune`、prune 被延迟时必然出现),由 prune
生命周期收敛。执行手段:`apply <module>` 与 `add` 的 profile 校验。

## 3. 所有权谓词与决策表

### 3.1 owned() 谓词(ADR-14 / ADR-22)

「state 有记录」只证明*曾经*创建。一切**自动**破坏性动作必须以下述谓词为前置
(强制替换走 §6 的独立公式):

```
owned(entry, observed) =
  symlink : observed 是链接 ∧ 原始链接目标 == entry.link_dest   # 纯字节比较
  rendered: observed 是普通文件 ∧ 内容摘要 == entry.hash
  scaffold: 恒 false(产物创建即归用户,ADR-12)
```

symlink 判定**不解析最终目标**:比较的是磁盘上的原始链接目标与
创建时的存证。因此**悬空链接可判**——源文件被删后链接必然悬空,而这正是 prune 的
主场景;解析式判定在此恒失败,因此不能作为所有权依据。

### 3.2 决策表规范

managed 条目已在 plan 阶段渲染完成,记产物 hash 为 `newHash`、期望权限为 `mode`。
link 条目的「期望 dest」= 本次将写入的字符串(创建侧规范化后的源绝对路径)。
决策表按 **desired kind** 分派;若 state 记录的 kind 与 desired kind 不一致,
**先适用 §3.4 的迁移规则**。规则**自上而下短路匹配**。

**link 条目**(源 S → 目标 T):

| # | 条件 | 动作 |
|---|---|---|
| L1 | T 不存在 | create-link(必须使用不覆盖并发新对象的创建语义) |
| L2 | T 是 symlink ∧ 原始链接目标 == 期望 dest | skip;state 无记录**或记录元数据过期**(link_dest/source/module 与现值不符——如 L3 重链成功但落账前崩溃)→ **adopt**(自动补录/刷新,ADR-21:证据无歧义,删链最坏零损失) |
| L3 | T 是 symlink ∧ state 有记录 ∧ 原始链接目标 == entry.link_dest(≠ 期望 dest) | create-link(自家旧链:仓库搬家或源路径变化;重链 + 更新 link_dest,打印说明) |
| L4 | T 是 symlink ∧ state 有记录 ∧ 原始链接目标 ≠ entry.link_dest | **CONFLICT-drift**(链接被手工改指;`--force` → backup-replace 恢复) |
| L5 | T 是 symlink 指向他处 ∧ state 无记录 | CONFLICT(含用户手工建的等价相对链接——写法不同即不匹配,保守是有意的) |
| L6 | T 是普通文件 / 目录 / 特殊文件 | CONFLICT(普通文件:`--force` → backup-replace;目录/特殊文件:`--force` 也拒绝,提示手工移走,ADR-29;建议 `dot add` 收编) |

L3 与旧尾部启发式的区别:静默修复的资格来自 **state 精确存证**(记录的确是我们写过的
字符串),而不是路径长得像;无 state 的旧 stow 链接一律落 L5,正确。

**managed 条目**:

| # | 条件 | 动作 |
|---|---|---|
| M1 | T 不存在 | render |
| M2 | T 非普通文件(symlink/目录/特殊文件) | CONFLICT |
| M3a | actualHash == newHash ∧ Mode == mode ∧ state 证据拥有当前文件(entry.hash == actualHash) | skip;module/source 过期 → adopt(metadata),只刷新非所有权元数据 |
| M3b | actualHash == newHash ∧ Mode ≠ mode ∧ state 证据拥有当前文件 | render (mode),使权限收敛且内容保持一致(ADR-26) |
| M3c | actualHash == newHash ∧ Mode == mode ∧ state 无记录或记录 hash 不拥有当前文件 | 默认 skip + 提示 adoptable;`--adopt` [M2] → adopt,只建立新 hash 证据而不触碰文件 |
| M3d | actualHash == newHash ∧ Mode ≠ mode ∧ state 无记录或记录 hash 不拥有当前文件 | CONFLICT-mode-unowned;不得以 adopt 修改权限。用户可先对齐 mode,或显式 `--force` 经 backup-replace 收敛并建账 |
| M4 | state 无记录(且 actual ≠ new) | CONFLICT |
| M5 | actualHash == 记录 hash(产物未被手改) | render(源或变量变更) |
| M6 | 其余(actual ≠ 记录 ∧ actual ≠ new) | **CONFLICT-drift**(手改;`--force` → backup-replace 重渲染) |

M3c 是收养不对称和 rendered 崩溃恢复的共同落点(ADR-21):内容巧合不构成所有权,
mode 不一致也不能借 adopt 触碰尚未拥有的文件。即使旧 entry 仍在,其 hash 不拥有当前
文件时也不得借旧账自动刷新。render 已成功但
state 未落盘的场景因此会提示显式 adopt,而不是永久保留过期证据或自动认领用户文件。

**scaffold 条目**:

| # | 条件 | 动作 |
|---|---|---|
| S1a | T 存在(任意形态) ∧ state 有 scaffold 记录 | skip(永不覆盖);展示 key/module/source 过期 → adopt(metadata) |
| S1b | T 存在(任意形态) ∧ state 无记录 | adopt (scaffold):只补 scaffold 记录,不建立所有权、不触碰 target |
| S2 | T 不存在 ∧ state 有 scaffold 记录 | skip + 提示(用户有意删除);元数据过期 → adopt(metadata);`--force` → scaffold(显式重建,无需备份,提交前复核仍缺失) |
| S3 | T 不存在 ∧ state 无记录 | scaffold |

S1b 把“蓝本已经存在”记作一次性生命周期已经满足。它不会让 `owned()` 变真,只保证
scaffold 创建成功但 state 落盘失败、或首次接管已有文件后,用户未来删除 target 不会被
误当作从未生成而再次创建。

**元数据刷新规则**:target 身份已唯一匹配时的展示 key 以及 module/source 是非所有权元数据;
需要刷新时决策从 skip 变为 state-only
adopt,而不是让 skip 暗中写账。`link_dest` 与 rendered hash 是所有权证据,不得套用通则:link_dest 只在
L2 已观测到精确期望链接时自动刷新;过期 hash 只按 M3c 经显式 `--adopt` 建立。
scaffold 同 kind 跨模块移动或源改名时按 S1a 产生 adopt(metadata)。

### 3.3 prune(方向相反:从 state 找孤儿)

**作用域**:全量 apply → 全部 state 条目;`apply <module...>` → 仅 `entry.module ∈ 请求集`。
候选 = 作用域内且按 target 身份不在本次 desired 的条目。路径展示字符串不同但身份相同的
state/desired 必须先合并为同一条目,不得一边 adopt、一边作为 orphan prune。

| # | 条件 | 动作 |
|---|---|---|
| P1 | kind == scaffold | 不删文件(ADR-12);state 摘除 + 提示 |
| P2 | owned(entry, observed) 为真 | prune(symlink 删链 / rendered 删文件) |
| P3 | owned 为假 | 不动文件;state 摘除 + 警告(对象已非我们所有) |

**收敛门控(ADR-20)**:prune 仅在创建阶段完全收敛后执行。plan 层:decide 产出的计划
中存在 conflict(未被 `--force` 转化为 backup-replace)→ 全部 prune 标记为
deferred(diff/dry-run 如实展示「prune (deferred)」);执行层:创建阶段出现
error 或 Precond 失配降级 → 剩余及全部 prune 转 deferred;**整模块级孤儿**的交互确认
(y/N,`--yes` 跳过)被拒绝 → 同样本次全部延迟(退出码 2)。deferred 不是 mutation,
只是展示。代价:无关模块的孤儿清理被一并推迟到下次干净运行——可接受,prune 是家务
不是急务。

### 3.4 kind 迁移(ADR-27)

同一 target 的 state 记录 kind 与 desired kind 不一致时(源文件在仓库里被改了后缀或
`[files].kind`),按三原则迁移;任何记账变更都只在对应动作成功后提交:

> 所有权只能被证据延续,不能被类型切换凭空创造;
> 迁入 scaffold = 释放所有权,永远安全;迁出 scaffold = 等同无记录。

**统一分派规则**(下方矩阵只是其展开):

1. **迁入 scaffold**:旧记录本身表示该 target 已经历过工具管理,因此一律释放为 scaffold
   并视为一次性生命周期已经满足。只有 target 当前仍是 owned symlink 时才自动转换为
   独立蓝本文件;其余情况只 adopt(kind→scaffold),target 存在或缺失都不触碰。缺失表示
   保留用户删除记忆,需要重建时由用户随后显式 `--force` 触发 S2。
2. **迁向 link/managed,旧证据 owned**(symlink:原始链接目标 == link_dest;rendered:
   hash==记录)→ 自动替换产物,记账迁移。
3. **迁向 link/managed,旧证据不成立或旧 kind 为 scaffold** → **按“新 kind 无记录”
   语义进入决策表**:旧记录不得提供任何所有权证据——否则「用户换上的、内容恰好等于
   渲染结果的普通文件」会被 M3a 借旧账自动收养、日后被 prune 误删,重蹈 ADR-21 覆辙。
   由此:同内容且 mode 一致的普通文件落 M3c(adoptable,需显式 `--adopt`),mode 不一致
   则落 M3d conflict;目标恰好已是精确期望链接 → L2 自动收养;目标缺失 → link 走 L1、
   managed 走 M1 正常创建;其余 → conflict。旧记录保留至
   成功动作提交新记录后才覆盖旧记录;在此之前每次运行仍适用本规则,不会自我毒化。

| 旧 kind(entry) | 新 kind(desired) | 展开 |
|---|---|---|
| rendered | scaffold | 规则 1:adopt(kind→scaffold),target 无论存在或缺失都不动;此后模块移除命中 P1,永不删 |
| symlink | scaffold | 规则 1:当前仍 owned → 自动换成独立蓝本;否则只 adopt(kind→scaffold),包括 target 缺失 |
| symlink | managed | 规则 2/3:owned → render 覆盖自家链并迁移记账;不 owned → 按无记录语义走 M 表 |
| rendered | link | 规则 2/3:owned → create-link 替换自家产物;不 owned → 按无记录语义走 L 表(恰是期望链接 → L2 收养;缺失 → L1;其余 → conflict) |
| scaffold | link / managed | 规则 3:按无记录语义走新表;成功后迁移记账 |

**词汇表对应**:desired kind 使用 link/managed,持久化 state 使用 symlink/rendered。
两套词汇语义不同,实现必须明确映射且不得混用;内部表示方式由代码决定。

本节必须防住的缺陷:`foo.tmpl` 曾生成文件(entry=rendered)→ 仓库改成
`.template` → 旧规则只 skip 而记账仍是 rendered → 未来移除模块时 P2 按旧 hash 判
owned 并删除——违反 ADR-12;规则 1 在切换发生的那次 apply 就释放所有权,路径闭死。
原子的「切换 + 移除」同一次发生时(中间从未 apply),记账仍是 rendered 且 hash 匹配
→ 按 owned rendered 产物清理,属契约之内。

## 4. 崩溃与并发边界

- **单实例锁(ADR-19)**:mutation 命令(含 [M2] 的 state rebuild / edit)与 `dot git` 必须通过
  `~/.local/state/dot/lock` 获得进程间排他锁,失败即报错;`update` 自洁净检查与 `git pull`
  起持锁。一次 mutation 的嵌套流程必须复用同一锁所有权且不能自锁;如何传递锁由实现决定。
  `diff`/`status`/`doctor` 只读不取锁——state 经原子替换写入,只读方看到完整的
  旧版或新版。
- **崩溃恢复**:state 落盘原子;「已执行未记账」窗口由收养规则收敛——重跑 apply,
  L2 自动补录/刷新,M3c 提示后 `--adopt` 补录,S1b 自动补 scaffold 的非所有权记录。
  目录元数据的断电持久化不作承诺(01 §4 已接受)。

## 5. 全局不变量校验(pipeline ⑨)

plan 完成、execute 之前,用**完整 effective profile 的结构性 desired 集**校验下列 1–4;
部分 apply 和 add 候选不得缩小这些路径不变量。第 5 项只校验本次动作计划。**任一违反 →
整体拒绝执行,一个动作都不做,exit 1**(配置 bug,与单文件 conflict 的「部分继续」语义不同):

1. **target 身份唯一**:两个条目不得按目标文件系统语义指向同一逻辑 target;冲突时列出
   双方来源——大小写/Unicode 别名、跨模块撞车、`foo` 与 `foo.tmpl` 去后缀碰撞均在此被抓。
2. **无祖先冲突**:任一 desired **文件** target 不得是另一 target 的祖先;祖先关系按
   路径语义判断,不得使用字符串前缀判断。
3. **中间目录不穿文件**:target 落地路径的待建中间目录不得与任何 desired 文件条目重合。
4. **控制面隔离**:desired target 不得与 repo/config/state/backup/binary 重叠(ADR-33)。
5. **动作与状态处置一致**:每个动作成功、失败、skip/deferred 后的记账必须符合
   02 号文档 §6;不完整或矛盾的计划视为内部错误,执行前整体拒绝。

`dot doctor` 复用 1–4;路径合法性(03 号文档 §6)在更早的解码阶段执行。

## 6. execute 顺序、Precond 复核与原子性

阶段顺序:**`mkdir → create-link / render / scaffold / backup-replace / adopt →
prune(仅收敛)→ hooks(不受门控)`**(ADR-13/20)。

**最终 Precond 复核(ADR-23)**:每个 target mutation 都必须在不可逆提交前确认对象种类与
参与决策的证据仍等于计划快照:缺失仍缺失;symlink 的原始链接目标未变;普通文件的
内容摘要与权限未变。create-link/relink 还必须确认计划中的 source 仍是模块内合法普通
文件;source 失效时不得创建死链,该动作降级 conflict 并使 prune 延迟。link 的内容本来
就是实时共享的,因此普通文件内容变化不要求冻结。
临时产物准备、持久化和 backup-replace 的备份等耗时工作必须在最终复核之前完成;
复核通过后立即提交。失配 → 该动作降级 conflict、文件不动并使 prune 延迟。
新建路径应使用不会覆盖既有对象的语义;其他提交后的极小窗口按 01 §4 明文接受。

**--force 公式(定稿)**:

```
自动替换(L3 重链、M5 重渲染等) = owned(entry, observed) ∧ 最终 Precond 复核通过
强制替换(backup-replace)       = 显式 --force ∧ 备份成功 ∧ 最终 Precond 复核通过
显式重建缺失 scaffold           = 显式 --force ∧ S2 ∧ target 最终仍缺失
```

`--force` 在计划阶段把可强制处理的 conflict 转成 backup-replace,或把 S2 转成不覆盖
任何现有对象的 scaffold 重建;后者不是强制替换,无需也不能备份缺失对象。执行阶段不再
推测用户意图。**--force 不作用于 prune**——prune 永远只删 owned(P2)。

**原子性与持久化必须满足的性质**:

- target 永远只能看到完整旧对象或完整新对象,不能看到半写文件。
- 新文件/链接不得意外覆盖已存在对象;替换路径只有在决策表允许且最终 Precond 成立时提交。
- backup-replace 的备份路径不得覆盖旧备份。普通文件备份必须内容完整、权限保持且其摘要
  等于计划时观测值;symlink 备份链接本身而非跟随目标。备份数据完成写入并达到普通进程
  崩溃后的文件级持久性后才算“备份成功”。目录和特殊文件不可 backup-replace。
- 已成功动作的 state 变化必须提交;后续动作失败时不得丢弃先前成功记账。state 提交失败
  必须报错,但不得删除已经越过提交点的 source/备份/target;重跑按收养规则恢复。

临时文件、排他创建、同步与原子替换的具体组合属于实现;测试只验证上述结果和失败边界。

## 7. 安全策略汇总

1. **绝不静默覆盖非托管文件**:conflict 三态(报错 / `--force` 备份覆盖 / 建议
   `dot add` 收编)是铁律;被改指的链接同样按 conflict 处理(L4/L5)。
2. **自动破坏性动作 = owned() ∧ 最终 Precond;强制替换 = 显式 `--force` ∧ 可用备份
   ∧ 最终 Precond**。
3. **prune = owned ∧ 收敛 ∧(整模块时)确认**三重前置;--force 不豁免。
4. 路径写操作前校验:按 target 身份必须位于 HOME 和声明的 target 根之下,且不得与
   控制面路径重叠;规范化字符串只用于展示和存储,manifest 路径合法性见 03 号文档 §6。
5. 敏感面权限:机器配置 0600、state 目录 0700、backup 目录 0700(文件保留原 mode,
   目录即保护边界)。

## 8. hooks:run_once 语义与执行细节

- 声明形态见 03 号文档 §3:字符串 [M1],或 `{ script, watch = [...] }` [M2]。
- **执行方式**:脚本带可执行位 → 直接 exec;否则以 `sh <script>` 调起。
  **工作目录 = 模块目录**(脚本内相对路径指向自己的 hooks/ 数据文件)。
  **环境 = 继承父环境,再以 `paths` 注入覆盖**:`HOME`、`XDG_CONFIG_HOME`、
  `XDG_STATE_HOME`、`XDG_DATA_HOME`(`--home` 下指向假 home,生产与测试同路径),
  外加 `DOT_MODULE`、`DOT_OS`、`DOT_PROFILE`、`DOT_REPO`、`DOT_TARGET`。
- **执行指纹**只由 script 文件以及每个 watch 条目的相对路径和文件内容决定,输入顺序
  稳定且编码无歧义;
  具体 hash 与编码由实现选择。指纹**不含 profile/data 等运行上下文**(ADR-31):否则
  切 profile 或改一个模板变量就全量重跑;hook 需要上下文时读 `DOT_*` 并自我幂等。
  成功后写入 `run_once["<module>/<script>"]`,失败不写。改变会使既有指纹整体失效的
  算法属于用户可观察兼容变化,必须有意实施而不能由重构偶然发生。
- **at-least-once,脚本必须自我幂等**:外部效果成功、指纹落盘前崩溃会重跑
  (brew bundle、defaults write 天然幂等)。此为文档化契约。
- **不受收敛门控**:hooks 与文件收敛无破坏性关联,且新机器常见「预置 .zshrc 引发
  conflict」的场景不应阻塞软件安装。指纹未变照常跳过。
- 脚本非零退出:apply 整体退出码 1(不回滚文件动作);`--dry-run` 只打印。
- `update` 拉取到的新 hook 不做单独确认(01 §4 威胁模型);审查方式:
  `dot update --no-apply` + `dot diff`。这只延迟 hook/apply,link source 已随 pull 生效
  (ADR-34)。

## 9. add 的反向映射与提交契约

输入先规范化并展开 `~`。`*.local`、已有有效 state 记录的路径、非普通文件和控制面路径
直接拒绝;无 state 的等价遗留 source 按下述续跑规则处理。
模块推断只考虑当前 profile 中 target 包含该路径的模块;已有 state 中同目录或祖先目录的
条目可用于消除歧义。不能唯一确定时列出候选并要求 `-m`;不存在或不在 profile 的模块
报错并给出手工创建/加入 profile 的指引,CLI 不修改 manifest。推断必须保守,因为猜错
模块会把用户真实文件放入错误的生命周期。

任何写入前,全部输入必须同时满足:

1. 候选 source 按正常 apply 的解析、枚举和 ignore 规则恰好产生一个 desired entry,
   且 target 与输入、kind 与所选模式一致。
2. 输入之间及与既有 desired 不发生 target、后缀或祖先碰撞。
3. 仓库 source 及 `.tmpl`/`.template` 变体均不存在,不会覆盖仓库真相。重试的唯一例外:
   只有期望路径上的 source 存在、其他变体不存在,且其类型、字节与权限和本次本应写入的
   source 完全等价时,可以不改写它而安全续跑;任一不等仍拒绝。
4. 原文件内容与权限已形成执行前快照,供提交前最终复核。
5. `--template`/`--scaffold` 的候选 source 已按当前有效数据完成渲染,渲染字节与
   desired mode 必须分别等于原文件快照;不等即在任何写入前拒绝。

模式结果:

| 模式 | 必须达到的结果 |
|---|---|
| 默认 link | 仓库 source 完整、权限保持且已验证后,最终复核原文件;以原子方式把 target 换为指向 source 的链接并登记 symlink |
| `--template` [M2] | 仅当候选模板渲染字节/mode 与原文件一致时,仓库保存 `.tmpl`,原文件留作当前 rendered 产物并按其内容建立记录 |
| `--scaffold` | 仅当候选模板渲染字节/mode 与原文件一致时,仓库保存 `.template`,原文件留作用户产物并登记 scaffold |

任何模式下,source 必须完整可用后才允许提交对应 state。source 发布后、相应提交点前
发生普通错误时,只能清理仍可证明由本轮创建且未变化的 source/临时产物;无法证明时保留。
进程中断无法保证清理,但尚未提交的 target 必须保持原样,且重跑可按上述等价性规则复用
source。任何情况下都不得覆盖或静默删除不等价的已有 source。

默认 link 的 target 替换是**提交点**。提交后 source 是用户数据的承载者,即使 state
提交失败也不得删除。此时保留 source 与链接、报告错误并提示重跑 apply,L2 自动收养。
`--template`/`--scaffold` 不修改 target,建账前还必须复核原文件仍与执行前内容/mode 快照
一致;对应 state 条目提交是所有权/一次性生命周期的提交点。条目提交后 source 不得再因
本轮后续错误被清理。多路径执行中后续项失败时,任何
已越过各自提交点的成功项都不得回滚,其状态应提交或留待规定的收养路径。具体复制、同步、
恢复标记、临时链接与清理实现不属于规范。

成功后统一提示用户将仓库变更加入 git 并提交。

## 10. 幂等契约(精确表述)

**一次 apply 成功收敛并完成 state 提交后,在 repo、配置、flag 与 target 均未变化时
立即重跑,不得再出现 mutation 动作(create-link / render / scaffold / backup-replace /
prune 执行)或 adopt。** 未解决的 conflict 与 deferred prune 可以在未收敛运行中稳定
复现;首次运行在提交点后落账失败时,第二次为恢复而 adopt 也不违反幂等——这些场景不满足
“成功收敛并完成 state 提交”的前提。
