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
目录不是动作对象:执行时可按需创建 target 侧缺失的中间目录;这些目录不记 state、不参与
prune。从有效 HOME 以下经过 target root 到 entry target 父目录的每个**现存**路径组件
必须可作为目录使用。指向目录的 symlink 是允许的用户文件系统拓扑;普通文件、悬空链接或
特殊对象会阻断路径。祖先 symlink 形成的别名必须计入 target 身份和控制面边界,工具不得
替换祖先来强行继续。该性质适用于 apply/add 的创建与替换、force/prune,以及会据此建立
state 的 adopt/rebuild。只有已有 target mutation 计划的命令可以按需创建缺失祖先,且
不得覆盖同时出现的对象;state-only adopt/rebuild 永不创建目录或 target。无法安全到达
预期逻辑位置时不得继续写入或以 P3 摘除历史记录。安全遍历方式由实现决定。

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

`version`、`entries`、`run_once` 是必填顶层字段,当前 `version = 1`;同一版本中的未知字段
视为格式错误,格式扩展必须提升版本或遵循已声明的向后兼容规则。每个 entry 必须包含
`module`、`kind`、`source`、`applied_at`;symlink 另须 `link_dest`,rendered 另须 `hash`,
且这些证据字段只允许出现在对应 kind 上;scaffold 不得携带会暗示所有权的
hash/link_dest。每个 run_once 记录必须包含 `hash` 与
`executed_at`。时间字段使用 RFC3339 字符串,仅供诊断,不得参与 ownership 或动作决策。
对象成员次序和 JSON 排版不属于格式契约。

键统一使用 `~/` 前缀的规范化**展示形式**,但查找、唯一性与 orphan 判定使用 ADR-35 的
target 身份。单个历史 key 若只是当前 desired 的文件系统别名,必须作为同一条目迁移记账,
不得另列 orphan;多个 state key 指向同一 target 则语义校验失败。`link_dest` 是 symlink 条目的
所有权存证:**创建链接时实际写入的确切字符串**——规范化只在创建侧做一次,此后一切
比较都是字节比较(ADR-22)。state 更新必须原子替换,读取方只能看到完整旧版或完整新版。

**加载校验与三态处理(ADR-25)**:state JSON 任一对象层级出现重复成员名即视为损坏,
包括重复顶层字段、entries/run_once key 与 entry 内字段;不得采用 first-wins、last-wins
或合并结果继续。JSON 解析之外还必须做语义校验——target 键规范且在词法上位于
HOME 内并避开控制面路径;module 名合法;source 是规范化的 `modules/<module>/…` 相对
路径;kind 合法;symlink 的 `link_dest`、rendered 的 hash 等证据存在且格式有效。用于
所有权、最终 Precond 与备份校验的 hash 必须携带受支持的算法标识,并使项目生命周期内的
偶然碰撞可忽略;未知或不满足该性质的标识必须 fail closed,已有标识必须持续可读或迁移。
具体算法与编码由实现选择。run_once key 必须由合法 module 名和规范化相对 script 路径组成,
但历史 entry 的 source 与历史 run_once 的 script 都不要求当前仍存在。任一违规视同损坏。
祖先拓扑后来使一个词法合法 target 暂时不可达或形成控制面别名,属于 §5 的运行时路径
不安全,不得据此抹掉原本语义合法的记录;普通命令拒绝消费,rebuild 按 §3.4 原样保留待处理。

| state 状况 | 行为 |
|---|---|
| 文件缺失 | 合法的全新开始:无历史可 prune、无 drift 检测,正常运行 |
| 解析失败 / **语义校验失败**(损坏) | **fail closed**:依赖旧 state 的阶段拒绝;`status`/`doctor` 可诊断;M1 手工备走损坏文件后重跑 apply;`state rebuild` [M2] 是保留原文件后重建的受控例外 |
| `version` 大于本 CLI 认识的 | 同上 fail closed。真实场景:self-update 后回滚二进制。提示升级 CLI 或手动处理 |

state v1 预留了 `rendered` 形态,但 M1 二进制尚未交付其完整生命周期。M1 读到任何
rendered 条目时必须按“当前 CLI 不支持”fail closed 并提示升级,不得把它当未知 kind
忽略、按 scaffold 保留或在缺少完整语义时 prune。M2 起才可正常消费并写入 rendered。

fail-closed 的精确作用域是任何依据旧 state 判断 ownership、drift、adopt、prune、source
定位或写入新 state 的阶段:`apply`/`add`/`edit` 在动作前拒绝,`diff` 不生成计划并退出 1,
`status` 报告后退出 1。`doctor` 只诊断,`state rebuild` 是受控例外但版本过新的 state 仍
拒绝。init 的机器配置提交与 update 的 pull 不依赖 state,可以先完成;若随后进入 apply,
该阶段仍须拒绝,且前置提交不回滚。`self-update`、`dot git` 与 update 的 pull 前置只校验
repo/config/state/binary **控制面家族彼此隔离**,不读取 state entries,因此损坏或过新 state
不得阻塞这些恢复动作(02 号文档 §2)。

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
本表的 mode 相等、drift 与最终前提统一采用 03 号文档 §3 的权限口径。尤其 M3b 不得以
修改共享 inode 的方式连带改变另一 target 身份;无法隔离时必须在提交前拒绝或降级
conflict,由用户处理硬链接关系。

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

对作用域内每个模块 `m`,若完整 effective profile 的 desired 中没有任何 `module == m`
的文件条目,且作用域内至少存在一个 `entry.module == m` 的候选,这些候选构成一个
**整模块 orphan 组**。判定必须使用完整 desired,不得使用部分 apply 的动作子集。启用
prune 时,所有此类组在首次 prune mutation 前一次性汇总确认;P1/P2/P3 都列出并标明是否
删除 target。拒绝或无法取得确认使本次全部 prune deferred;`--yes` 只跳过确认。

| # | 条件 | 动作 |
|---|---|---|
| P1 | kind == scaffold | 不删文件(ADR-12);state 摘除 + 提示 |
| P2 | owned(entry, observed) 为真 | prune(symlink 删链 / rendered 删文件) |
| P3 | owned 为假 | 不动文件;state 摘除 + 警告(对象已非我们所有) |

**收敛门控(ADR-20)**:prune 仅在创建阶段完全收敛后执行。plan 层:decide 产出的计划
中存在 conflict(未被 `--force` 转化为 backup-replace)→ 全部 prune 标记为
deferred(diff/dry-run 如实展示「prune (deferred)」);执行层:创建阶段出现
error 或 Precond 失配降级 → 剩余及全部 prune 转 deferred;上述整模块确认被拒绝 →
同样本次全部延迟(退出码 2)。deferred 不是 mutation,
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

M1 必须实现旧/新 kind 均属于 symlink/scaffold 的迁移子集;不能因 managed 组合尚未交付而
退回普通决策表。涉及 managed/rendered 的其余组合由 M2 补齐。
state-only rebuild 不是 prune,也不是批量 `--adopt`:无可信旧证据时,内容/mode 恰好符合
desired 的普通文件仍不得取得 rendered 删除所有权。对于语义合法旧 state,rebuild 不得
丢弃任何正常 apply 仍需 target mutation 或 prune 生命周期才能处置的记录;它必须保留旧
entry 并把该项报告为待处理。该集合包括 L3、M5、owned kind 迁移、全部 orphan 以及暂时
无法安全观测的旧 entry。仍 owned 的旧 symlink → scaffold 是其中最敏感的一例:若直接改记
scaffold,会永久跳过规则 1 所需的独立文件转换。

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
  `diff`/`status`/`doctor`/`version` 和 `--dry-run` 形态只读不取锁——state 经原子替换写入,只读方看到完整的
  旧版或新版。
- **崩溃恢复**:state 落盘原子;「已执行未记账」窗口由收养规则收敛——重跑 apply,
  L2 自动补录/刷新,M3c 提示后 `--adopt` 补录,S1b 自动补 scaffold 的非所有权记录。
  目录元数据的断电持久化不作承诺(01 §4 已接受)。

## 5. 全局不变量校验(pipeline ⑨)

plan 完成、execute 之前,用**完整 effective profile 的结构性 desired 集**校验下列 1–4;
部分 apply 和 add 候选不得缩小这些路径不变量。第 5 项校验本次涉及的运行时路径,第 6 项
校验动作计划。**任一违反 →
整体拒绝执行,一个动作都不做,exit 1**(配置 bug,与单文件 conflict 的「部分继续」语义不同):

1. **target 身份唯一**:两个条目不得按目标文件系统语义指向同一逻辑 target;冲突时列出
   双方来源——大小写/Unicode 别名、跨模块撞车、`foo` 与 `foo.tmpl` 去后缀碰撞均在此被抓。
2. **无逻辑祖先冲突**:任一 desired **文件** target 的 leaf 位置不得位于另一 target
   解析后的目录链上;祖先关系按路径语义判断,不得使用字符串前缀判断。因祖先 symlink
   到达的真实目录同样计入该目录链。
3. **中间目录不穿文件**:任一 target 展示路径实际经过的中间目录项不得同时是 desired 文件
   target 的 leaf,无论该中间项是待建目录、现存目录还是指向目录的 symlink;递归展开
   symlink target 时经过、随后被 `..` 折返或不在最终解析目录链上的项仍在此范围。因此
   已有目录 symlink `A` 时,desired `A` 与 `A/child` 必须冲突;`A` 作为 leaf 仍与其目标目录
   身份不同,不得仅因它指向 `real` 就把 `A` 判为直接写成 `real/child` 的祖先。
4. **控制面隔离**:repo/config/state 家族/binary 已两两隔离,且 desired entry target 不得
   与任一控制面家族重叠(ADR-33)。
5. **祖先链可安全到达**:本次可能 mutation、adopt 或 rebuild 的 entry target 满足 §1 的
   目录可用性、target 身份与控制面边界;计划时不满足则在 execute 前整体拒绝。指向目录且
   不破坏这些不变量的祖先 symlink 允许存在。
6. **动作与状态处置一致**:每个动作成功、失败、skip/deferred 后的记账必须符合
   02 号文档 §6;不完整或矛盾的计划视为内部错误,执行前整体拒绝。

上述“整体拒绝”对 state rebuild 的第 5 项有受控例外:祖先链不可安全到达的 desired 逐项
标为未收养,不得穿越或读取;无可信旧 entry 时不得建立记录,有语义合法旧 entry 时则必须
原样保留为待处理。其他安全条目仍可提交,命令退出 2。该例外只缩小恢复结果,不允许任何
target mutation 或隐式摘账。

完整 `dot doctor` 复用 1–5;`--manifest-only` 复用 1–3,并只对无需读取机器配置即可从
本次调用解析出的控制面路径执行第 4 项,不得假装覆盖机器配置中的 override。manifest
路径合法性(03 号文档 §6)在更早的解码阶段执行。

## 6. execute 顺序、Precond 与原子性

阶段顺序:**`mkdir → create-link / render / scaffold / backup-replace / adopt →
prune(仅收敛)→ hooks(不受门控)`**(ADR-13/20)。

**提交时 Precond(ADR-23)**:每个 target mutation 的不可逆提交必须仍针对计划观测的同一
逻辑位置,且对象种类与参与决策的证据仍等于计划快照:缺失仍缺失;symlink 的原始链接目标
未变;普通文件的内容摘要与权限未变;祖先拓扑仍把路径带到同一 target 身份且未落入控制面。
create-link/relink 还必须保证计划中的 source 仍是模块内合法普通文件;source 失效时不得
创建死链,该动作降级 conflict 并使 prune 延迟。link 的内容本来就是实时共享的,因此普通
文件内容变化不要求冻结。

实现可以通过提交前重新观测、不覆盖创建语义、基于目录的文件系统操作或其他组合维持
Precond;规范不固定某一种系统调用顺序。但临时产物、持久化和备份等准备工作不能让提交
继续依赖已经过期的判断。祖先拓扑或叶子证据失配 → 该动作降级 conflict、target/state
不动并使 prune 延迟;不得把无法安全到达的祖先路径降级成 P3 后摘账。不可消除的极小竞态
窗口按 01 §4 明文接受。

**--force 公式(定稿)**:

```
自动替换(L3 重链、M5 重渲染等) = owned(entry, observed) ∧ 提交时 Precond 成立
强制替换(backup-replace)       = 显式 --force ∧ 备份成功 ∧ 提交时 Precond 成立
显式重建缺失 scaffold           = 显式 --force ∧ S2 ∧ target 最终仍缺失
```

`--force` 在计划阶段把可强制处理的 conflict 转成 backup-replace,或把 S2 转成不覆盖
任何现有对象的 scaffold 重建;后者不是强制替换,无需也不能备份缺失对象。执行阶段不再
推测用户意图。**--force 不作用于 prune**——prune 永远只删 owned(P2)。

**原子性与持久化必须满足的性质**:

- target 永远只能看到完整旧对象或完整新对象,不能看到半写文件。
- 新文件/链接不得意外覆盖已存在对象;替换路径只有在决策表允许且提交时 Precond 成立时提交。
- 对一个 target 身份的内容或 mode mutation 不得经共享 hard-link inode 改变另一 target
  身份,也不得让计划中另一条 skip 的后置条件失效;无法保证时必须拒绝或降级 conflict。
- backup-replace 的备份路径不得覆盖旧备份。普通文件备份必须逐字节完整、保留九个普通
  权限位且其摘要等于计划时观测值;symlink 备份原始链接文本本身而非跟随目标。owner、ACL、
  xattr、flags 与时间戳不在当前恢复范围内(01 号文档 §3/§4)。备份数据完成写入并达到普通
  进程崩溃后的文件级持久性后才算“备份成功”。每次成功必须报告精确路径;当前版本永不
  自动删除成功备份。目录和特殊文件不可 backup-replace。
- 已成功动作的 state 变化必须提交;后续动作失败时不得丢弃先前成功记账。state 提交失败
  必须报错,但不得删除已经越过提交点的 source/备份/target;重跑按收养规则恢复。

dot 为原子提交或准备动作创建的临时产物永远不能成为模块 source、desired entry 或 state
真相;崩溃遗留物必须被后续枚举排除,且只有仍可证明属于工具并未变化时才可自动清理。具体
临时布局、命名、排他创建、同步与原子替换组合属于实现;测试只验证上述结果和失败边界。

## 7. 安全策略汇总

1. **绝不静默覆盖非托管文件**:conflict 三态(报错 / `--force` 备份覆盖 / 建议
   `dot add` 收编)是铁律;被改指的链接同样按 conflict 处理(L4/L5)。
2. **自动破坏性动作 = owned() ∧ 提交时 Precond;强制替换 = 显式 `--force` ∧ 可用备份
   ∧ 提交时 Precond**。
3. **prune = owned ∧ 收敛 ∧(整模块时)确认**三重前置;--force 不豁免。
4. 路径写操作前校验:entry target 的 manifest 表示必须在词法上位于 HOME 和声明的
   target root 之下;既有目录 symlink 可以把实际存储位置带到 HOME 之外,这是用户明确的
   文件系统拓扑,不单独拒绝。但其有效 target 身份仍不得与其他 desired 或控制面路径
   重叠,祖先链也必须可安全到达;展示/存储形式与身份判定不得混用,manifest 路径合法性见
   03 号文档 §6。
5. 敏感面权限:机器配置 0600、state 目录 0700、backup 目录 0700(文件保留原 mode,
   目录即保护边界)。

## 8. hooks:run_once 语义与执行细节

- 声明形态见 03 号文档 §3:字符串 [M1],或 `{ script, watch = [...] }` [M2]。
- 同一模块内 script 的文件系统身份必须唯一;同一脚本的别名或重复声明在严格 manifest
  校验阶段失败,不得合并 watch 或依赖顺序覆盖。合法引用使用与目录项一致的规范化路径,
  state key 因而唯一为 `<module>/<normalized-script>`。
- **作用域与顺序**:全量 apply 只考虑当前 effective profile 的 hooks;部分 apply 只考虑
  请求模块,其他模块即使有 pending hook 也不得执行或更新指纹。候选串行执行,模块按规范化
  名称的字节序排列,模块内按 `run_once` 数组声明顺序排列;不提供跨模块依赖图。需要严格
  先后的相关动作应放在同一脚本或同一模块内按数组排序。
- **执行方式**:脚本带可执行位 → 直接 exec;否则以 `sh <script>` 调起。
  **工作目录 = 模块目录**(脚本内相对路径指向自己的 hooks/ 数据文件)。
  **环境 = 继承父环境,再以 `paths` 注入覆盖**:`HOME`、`XDG_CONFIG_HOME`、
  `XDG_STATE_HOME`、`XDG_DATA_HOME`(`--home` 下指向假 home,生产与测试同路径),
  外加 `DOT_MODULE`、`DOT_OS`、`DOT_PROFILE`、`DOT_REPO`、`DOT_TARGET`;
  `DOT_TARGET` 表示该模块当前解析出的 target root。
- **stdio = 继承调用命令并实时透传**:hook 直接使用调用方的 stdin、stdout 与 stderr,工具不
  buffer、截断、解析或重排脚本输出。不同 stream 以及 dot 自身已输出的 context/action 行可按
  实际写入时序交错；这部分外部输出不属于 dot 的确定性摘要契约。脚本需要无人值守时必须自行
  提供非交互参数,不能假设 dot 会代答输入。
- **执行指纹**只由 script 内容、决定“直接 exec 或交给 sh”的执行方式分类,以及每个
  watch 条目的相对路径和文件内容决定。watch 按规范化相对路径字节序进入指纹,所以只重排
  watch 数组不得触发重跑;仅切换脚本执行位导致执行方式变化时必须得到新指纹。输入编码
  必须无歧义。
  具体 hash 与编码由实现选择。指纹**不含 profile/data 等运行上下文**(ADR-31):否则
  切 profile 或改一个模板变量就全量重跑;hook 需要上下文时读 `DOT_*` 并自我幂等。
  成功后写入 `run_once["<module>/<script>"]`,失败不写。改变会使既有指纹整体失效的
  算法属于用户可观察兼容变化,必须有意实施而不能由重构偶然发生。
- **at-least-once,脚本必须自我幂等**:外部效果成功、指纹落盘前崩溃会重跑
  (brew bundle、defaults write 天然幂等)。此为文档化契约。
- run_once 记录是每台机器的历史,不随 profile 切换、模块移除或文件 prune 自动删除;
  相同 module/script 与相同指纹日后重新出现时仍视为已经成功。只有脚本/watch 指纹变化、
  用户显式移除对应记录或 state 无法恢复时才重跑。
- **不受收敛门控**:hooks 与文件收敛无破坏性关联,且新机器常见「预置 .zshrc 引发
  conflict」的场景不应阻塞软件安装。指纹未变照常跳过。
- 脚本非零退出时停止尚未启动的后续 hooks,apply 整体退出码 1且不回滚文件动作;失败脚本
  保留旧指纹,此前成功脚本的新指纹与成功文件动作仍必须提交。提交本身失败则按 at-least-once
  重跑。`--dry-run` 按同一确定顺序只打印,不执行或写指纹。
- `update` 拉取到的新 hook 不做单独确认(01 §4 威胁模型);审查方式:
  `dot update --no-apply` + `dot diff`。这只延迟 hook/apply,link source 已随 pull 生效
  (ADR-34)。

## 9. add 的反向映射与提交契约

输入先规范化并展开 `~`。`*.local`、已有有效 state 记录的路径、非普通文件和控制面路径
直接拒绝;无 state 的等价遗留 source 按下述续跑规则处理。
add 对普通文件的迁移范围同 01 号文档 §3/§4:保证字节与九个普通权限位,不承诺复制
owner/ACL/xattr/flags/时间戳;依赖这些属性的文件应手工处理而不调用 add。
模块推断只考虑当前 profile 中 target 包含该路径的模块;已有 state 中同目录或祖先目录的
条目可用于消除歧义。不能唯一确定时列出候选并要求 `-m`;不存在或不在 profile 的模块
报错并给出手工创建/加入 profile 的指引,CLI 不修改 manifest。推断必须保守,因为猜错
模块会把用户真实文件放入错误的生命周期。

任何写入前,预检结果必须等价于本次将发布的精确 source 已加入后再执行正常严格
manifest/枚举校验;如何得到该结果由实现决定,且不能变成对其他悬空引用的宽松模式。
全部输入必须同时满足:

1. 候选 source 按正常 apply 的解析、枚举和 ignore 规则恰好产生一个 desired entry,
   且 target 与输入、kind 与所选模式一致。
2. 尚未被 Git 跟踪的候选 source 按有效仓库的完整 Git ignore/exclude 语义不被忽略,无需
   force 即可纳入版本控制;此条件与 manifest ignore 独立,等价遗留 source 也不得绕过。
3. 输入之间及与既有 desired 不发生 target、后缀或祖先碰撞。
4. 仓库 source 及 `.tmpl`/`.template` 变体均不存在,不会覆盖仓库真相。重试的唯一例外:
   只有期望路径上的 source 存在、其他变体不存在,且其类型、字节与权限和本次本应写入的
   source 完全等价时,可以不改写它而安全续跑;任一不等仍拒绝。
5. 原文件内容与权限已形成执行前快照,且对应提交发生时该快照仍须成立。
6. `--template`/`--scaffold` 的候选 source 已按当前有效数据完成渲染,渲染字节与
   desired mode 必须分别等于原文件快照;不等即在任何写入前拒绝。

模式结果:

| 模式 | 必须达到的结果 |
|---|---|
| 默认 link | 仓库 source 完整、权限保持且已验证后,仅在原文件快照于提交时仍成立时,以原子方式把 target 换为指向 source 的链接并登记 symlink |
| `--template` [M2] | 仅当候选模板渲染字节/mode 与原文件一致时,仓库保存 `.tmpl`,原文件留作当前 rendered 产物并按其内容建立记录 |
| `--scaffold` | 仅当候选模板渲染字节/mode 与原文件一致时,仓库保存 `.template`,原文件留作用户产物并登记 scaffold |

任何模式下,source 必须完整可用后才允许提交对应 state。source 发布后、相应提交点前
发生普通错误时,只能清理仍可证明由本轮创建且未变化的 source/临时产物;无法证明时保留。
进程中断无法保证清理,但尚未提交的 target 必须保持原样,且重跑可按上述等价性规则复用
source。任何情况下都不得覆盖或静默删除不等价的已有 source。

默认 link 的 target 替换是**提交点**。提交后 source 是用户数据的承载者,即使 state
提交失败也不得删除。此时保留 source 与链接、报告错误并提示重跑 apply,L2 自动收养。
`--template`/`--scaffold` 不修改 target,对应 state 条目提交时原文件仍须与执行前
内容/mode 快照一致;该提交是所有权/一次性生命周期的提交点。条目提交后 source 不得再因
本轮后续错误被清理。多路径执行中后续项失败时,任何
已越过各自提交点的成功项都不得回滚,其状态应提交或留待规定的收养路径。具体复制、同步、
恢复标记、临时链接与清理实现不属于规范。

成功后统一提示用户将仓库变更加入 git 并提交;`dot add` 自身不调用 Git 暂存或提交。

## 10. 幂等契约(精确表述)

**一次 apply 成功收敛并完成 state 提交后,在 repo、配置、flag 与 target 均未变化时
立即重跑,不得再出现 mutation 动作(create-link / render / scaffold / backup-replace /
prune 执行)或 adopt。** 未解决的 conflict 与 deferred prune 可以在未收敛运行中稳定
复现;首次运行在提交点后落账失败时,第二次为恢复而 adopt 也不违反幂等——这些场景不满足
“成功收敛并完成 state 提交”的前提。
