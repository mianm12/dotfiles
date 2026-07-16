# 04 · CLI 规范:命令、Flag 与输出

## 1. 总览

```
dot <command> [flags] [args]

核心      init | apply | diff | status | add | doctor
同步      self-update | update [M2] | git [M2]
辅助      version | state rebuild [M2] | edit [M2]
```

## 2. 全局 flag

| flag | 说明 |
|---|---|
| `--repo <dir>` | 覆盖仓库位置(> `DOT_REPO` > 机器配置 > 默认);路径语法见 02 号文档 §2 |
| `--home <dir>` | **隐藏**。只接受绝对路径;整体重定向 `~`、全部状态路径**及 hook 子进程环境**,测试专用 |
| `--profile <name>` | 本次运行覆盖机器配置中的 profile |
| `-v / --verbose` | 详细输出(含 skip 项与内容 diff) |
| `--no-color` | 关闭彩色输出(pipe 时自动关闭) |

除 `dot git` 对已启动 Git 进程透传退出码外,全部命令使用同一套错误到退出码映射;内部
实现只需传递足以分类的语义化错误。锁边界:实际 mutation 命令(含 [M2] 的
`state rebuild`、`edit`)与 `dot git` 持锁;`diff` / `status` / `doctor` / `version` 及所有
`--dry-run` 形态只读不取锁(02 号文档 §2)。

除 `init` 外,全局 flag 和环境变量只覆盖本次运行,不得改写机器配置。`init` 对 repo/profile
的持久化规则见 §4.1。每个控制面路径输入都必须在首次使用该路径进行文件系统 IO、启动
Git 或写入前,按 02 号文档 §2 解析为与 cwd 无关的唯一绝对路径;相对路径或不受支持的
`~` 写法直接报错。

## 3. 退出码(`dot git` 除外)

| 码 | 含义 |
|---|---|
| 0 | 成功;对 `diff` / `status` 表示「无差异 / 无异常」 |
| 1 | 运行错误(IO 失败、manifest 非法、requires 不满足、不变量校验失败、锁被占用、state fail-closed…) |
| 2 | 无运行错误且无 unresolved conflict,但仍有差异、未完成工作、warning 或未收养项 |
| 3 | 产生动作计划或执行动作的命令存在 unresolved conflict 或选择歧义,需要用户决策 |

同一次运行满足多个条件时按 **1 > 3 > 2 > 0** 取最高优先级。退出码 2 包括
`diff`/`status` 发现差异、`apply` 的整模块确认被拒或存在 deferred prune,以及
`state rebuild` 仍有未收养项。`dot git` 在 Git 启动前发生的 dot 自身错误仍返回 1;
Git 一旦启动则原样透传其退出码(§4.9)。
`status` 是健康巡检而非动作计划:被改指链接等 apply 时会成为 conflict 的现势在此归入
DRIFT 并返回 2,不返回 3;`diff`/dry-run 若计划中确有 conflict 则返回 3。
`doctor` 与 `version` 的诊断型例外分别见 §4.6/§4.10:它们可以在输出诊断后按问题严重度
返回非零,但不得因门禁而省略仍能安全给出的版本或诊断信息。
03 号文档 §8 定义的 development compatibility notice 不属于本表的 warning;它本身不改变
命令的退出码。

## 4. 命令规范

### 4.1 `dot init`

新机器初始化,幂等(重复运行进入「更新变量」模式)。流程:

1. 严格读取已有机器配置(缺失合法),定位仓库并校验控制面家族;仓库不存在则提示走 bootstrap。
2. requires 宽松预读通过后严格解码 manifest。
3. 确定 profile:`--profile` 优先;否则已有且仍合法的 profile 作为默认;仍未确定时交互列出
   顶层 `[profiles]` 键供选择。已有值已不存在时不得静默沿用。
4. 按顶层 `[data]` 声明逐项询问变量。提示默认值按“已有机器配置值 → 非空的
   `from_env` [M2] 快照 → manifest default”选择;回车接受当前默认。显式 `--set`
   始终优先,且显式空字符串与未提供不同。
5. **安全更新机器配置**:严格读取现有文件;本次未指定的 profile/repo/data 必须保留。
   `--profile` 明确选择的 profile 必须持久化;本次显式提供 `--repo` 或 `DOT_REPO` 时,配置
   提交后即使移除该 flag/环境变量,下一进程也必须解析到同一 repo。未提供 repo override
   时保留合法旧 repo;初次使用内置默认时是否省略冗余字段不属于格式契约。
   未知顶层字段、旧文件损坏或合并结果非法时拒绝重写。新配置以 0600 原子替换,
   任一步失败**保留旧配置不变**。初次读取结果是提交前提:全部交互、合并、校验与临时
   产物准备完成后,提交时配置仍须保持相同对象种类、字节和参与决策的权限证据;
   初次缺失则提交时仍须缺失。实现如何维持该前提不属于 CLI 契约。失配时不得覆盖、
   不得进入 apply,提示重新运行。
6. 询问「立即 apply?」,默认 yes。

Flag:`--profile <name>`、`--set key=value`(可重复)、`--yes` 支持无人值守:
`dot init --profile mac --set email=me@x.com --yes`。`--set` 引用未声明键,或无人值守时
仍缺 profile/必填变量,必须在写配置前报错。`init --yes` 同时确认“立即 apply”并把
整模块 prune 的确认授权传给嵌套 apply,但不隐含 `--force` 或 `--adopt`。
需要交互时必须从可用的用户终端读取;若没有终端,只有 profile 与全部 data 已能由 flag、
合法旧配置或声明的默认来源无歧义确定,且 apply 决策由 `--yes` 明确提供时才能继续;
否则在写配置或执行 apply 前失败。
init 的配置提交不依赖旧 state;若用户选择 apply 而 state 非法,配置提交保留、apply 按
ADR-25 拒绝,不得为伪造事务性而回滚有效配置。

### 4.2 `dot apply [module...]`

核心命令,pipeline 见 02 号文档 §4。**prune 作用域随调用形态变化**:

- **无参数(全量)**:应用当前 profile 全部模块;prune 候选 = 全部 state 条目中
  不在 desired 的。
- **指定模块(部分)**:仅应用给定模块;prune 候选 = 仅 `entry.module ∈ 请求集` 的
  孤儿条目;hooks 也只考虑请求模块,不得执行或更新其他 effective module 的指纹。
- 请求的模块 **∉ 当前 profile → 报错**(ADR-18),提示将其加入 profile 后重试。
- 无论全量或部分调用,target 身份唯一、祖先冲突和控制面隔离都按**完整 effective profile**
  校验;部分调用只缩小动作与 prune 作用域,不能绕过配置错误。

| flag | 行为 |
|---|---|
| `-n / --dry-run` | 只打印计划,不取锁且不发起任何文件系统写入,退出码规则同 `diff` |
| `--force` | 把可强制处理的 conflict 计划为 `backup-replace`(目录/特殊文件除外,ADR-29);另允许 S2 在 target 仍缺失时显式重建 scaffold,该分支不替换对象、无需备份 |
| `--adopt` [M2] | 允许收养「内容与渲染结果、mode 均一致但无有效所有权记录」的**普通文件**(05 号文档 §3.2 M3c,managed 专属);adopt 只写 state。mode 不一致时必须先由用户对齐或显式 `--force`;symlink 与无所有权的 scaffold 记录自动补录。M1 构建给出硬错误 |
| `--prune` / `--no-prune` | 是否计划 prune 阶段,默认 `--prune` |
| `-y / --yes` | 跳过交互确认(目前唯一确认点:整模块 orphan 组清理) |

行为要点:

- 执行顺序 `mkdir → 创建/收养 → prune → hooks`(ADR-13);**prune 仅在创建阶段完全
  收敛后执行**(ADR-20):plan 中存在 conflict、执行中出现 error 或 Precond 失配、
  或用户拒绝整模块确认,任一发生 → 本次**全部** prune 转为 deferred(输出中标注,
  不执行),提示消解后重跑。hooks 不受收敛门控(05 号文档 §8)。
- 存在 conflict 且无 `--force` 时,其余创建动作照常执行,conflict 项汇总列出,
  退出码 3(部分成功优于全盘卡死,幂等保证重跑无害)。
- 每个成功的 `backup-replace` 必须在输出中给出其精确备份路径。当前版本不自动删除任何
  成功备份;用户确认不再需要后自行清理,恢复范围见 01 号文档 §3 与 05 号文档 §6。
- 对本次 prune 作用域中的模块 `m`,若完整 effective profile 的 desired 中不存在任何
  `module == m` 的文件条目,且作用域内至少有一个 `entry.module == m` 的孤儿,这些条目
  构成**整模块 orphan 组**(典型于 profile 切换)。判定不得使用部分 apply 的动作子集。
  启用 prune 且存在此组时,在首次 prune mutation 前一次性汇总确认(y/N),列明 P1/P2/P3
  中哪些会删 target、哪些只摘 state;`--yes` 仅跳过确认,不放宽 owned/收敛条件。拒绝使
  本次全部 prune 延迟并退出 2。
- 计划为空(仅 skip)时输出 `Already up to date.`。

### 4.3 `dot diff`

执行 pipeline ②–⑨ 后打印计划,**无锁**、永不写盘(adopt 与 deferred prune 如实展示,
可收养的普通文件标注 `adoptable, rerun apply --adopt`)。`-v` 时:「将重渲染」与
「drift」条目均展示 **实际文件 vs 本次渲染结果** 的 unified diff(06 号文档 §5)。

### 4.4 `dot status`

面向「巡检」而非「预览」,无锁只读,分节输出:

```
Profile: mac (12 modules, 87 files managed)

DRIFT (2)
  ~/.config/git/config          rendered file modified by hand
  ~/.config/zsh/aliases.zsh     symlink re-pointed elsewhere

PENDING (1)
  macos/hooks/setup.sh          run_once not yet executed

ORPHAN / PENDING PRUNE (1)
  ~/.old-config                 owned orphan from previous profile

UNASSIGNED MODULES (1)
  experimental-nvim             not referenced by any profile
```

DRIFT、尚未完成的 desired/hook 动作以及任何 orphan 都是 actionable finding,使 status
退出 2;orphan 无论来自 `--no-prune`、deferred prune 还是 profile 切换都必须显示。
UNASSIGNED 只指未被任何 profile 引用的模块,是合法仓库清单信息,单独存在时不影响
`Clean.` 与退出码 0。state 或 manifest 损坏、版本过新、语义校验失败时,status 报告后
退出 1,不继续产生可信巡检结果,也不得同时宣称 `Clean.`。

### 4.5 `dot add [-m <module>] [--template|--scaffold] [--dry-run] <path>...`

把 `$HOME` 中已有文件收编入库。反向映射与模块推断算法见 05 号文档 §9。要点:

- **只接受普通文件**:目录、symlink、特殊文件一律拒绝(目录请逐文件 add 或手工搬移)。
- **预检与 apply 同源**(ADR-30):任何写入前,校验结果必须等价于把本次待发布 source
  加入后再运行正常的严格 manifest 解析/枚举规则;这允许 `[files]` 在 source 尚不存在时
  预先声明其 mode/kind/target,但任何无关悬空引用仍失败。候选 source 必须恰好
  产生一个 desired entry,且 target 等于输入、kind 符合模式;命中内置忽略、
  `hooks/` 或 ignore pattern → 拒绝。仓库目标及其模板后缀变体已存在时默认拒绝且绝不
  覆盖;唯一例外是期望路径上的 source 可证明与本次本应写入的内容完全等价,此时允许
  不改写 source 而安全续跑。
  多路径输入全部通过路径、唯一性和碰撞校验后才开始执行。
- **Git 可跟踪性前置条件**:尚未被 Git 跟踪的候选 source 必须按有效仓库的完整 ignore
  语义(仓库规则、本地 exclude 与用户级 exclude)可由普通 Git 操作纳入版本控制;若被忽略,
  在任何 source/target/state 写入前拒绝,不得依赖 force。等价遗留 source 也适用;
  已被跟踪的 source 不因后来新增 ignore 而失去安全续跑资格。该检查与 manifest ignore
  相互独立,`*.local` 的硬拒绝优先且无例外。`dot add` 本身不执行暂存或提交。
- **渲染模式建账前一致性**:`--template`/`--scaffold` 的候选 source 必须以当前有效数据
  渲染成功,且渲染字节和 desired mode 与原文件快照完全一致;否则在任何写入前拒绝。
  这防止 managed 下次 apply 自动改写原文件,也防止新机器生成内容或权限意外变化的蓝本。
- **控制面路径硬拒绝**(ADR-33):输入位于有效 repo、机器配置、state/backup 或已安装
  二进制内,或会覆盖这些路径时拒绝。
- **硬拒绝 `*.local` 路径**(06 号文档 §2)。
- 推断唯一命中 → 直接归入;多命中或零命中 → 退出码 3,提示加 `-m`。`-m` 必须满足
  模块名合法性(03 号文档 §6);**指向不存在的模块 → 报错**(不自动创建目录——新
  目录必然 ∉ profile,自动创建与 profile 校验自相矛盾),打印两步指引:
  `mkdir -p modules/<m>` + 将 `"<m>"` 加入 `[profiles]` 对应列表。
- **目标模块 ∉ 当前 profile → 报错**,并打印需手动添加进 `[profiles]` 的确切行
  (ADR-18/28,CLI 不代改 manifest);编辑后重跑即可。
- **默认(link)**:先在不存在、或仅有上述等价 source 的仓库路径准备完整、当次权限一致且
  已验证的 source,并保证 target 提交时原文件仍与预检快照一致,再原子换成指向该 source
  的链接。target 被链接替换是提交点:
  提交前普通错误只能清理仍可证明由本轮创建且未变化的 source/临时产物,否则保留;
  进程中断可能留下已发布 source,但不得改变 target,且重跑必须按等价 source 续跑规则恢复。
  提交后即使 state 落盘失败
  也**不得删除 source**,保留链接并提示重跑 apply,由 symlink 收养规则恢复记账。
- **`--template`** [M2]:仓库侧存为 `.tmpl`;**原文件留在原位作为产物**,登记
  `kind=rendered`(hash = 当前内容)。只有上述渲染字节/mode 一致性成立才可建账;随后用户
  可把机器相关值替换为 `{{ .var }}` 并用 `dot diff` 检查结果。M1 构建给出硬错误。
- **`--scaffold`**:上述渲染字节/mode 一致性成立后,仓库侧存为 `.template`;原文件保留
  为产物,登记 `kind=scaffold`。
- **渲染型模式的提交边界**:`--template`/`--scaffold` 不修改 target;完整 source 必须先于
  state 建账存在,且建账提交时原文件内容/mode 仍等于执行前快照。state 条目提交前发生
  错误时,只能清理仍可证明由本轮创建且未变化的
  source,否则保留并允许等价续跑;条目一旦提交,后续输入失败也不得删除其 source 或回滚
  该成功项。
- `--dry-run` 打印预检结论与将执行的动作,不取锁且不发起任何 source/target/state/lock 写入。

### 4.6 `dot doctor`

环境与配置自检,**严格只读、无锁**。检查项:二进制是否在 PATH、仓库存在且为 git 仓库、
manifest 静态校验(03 号文档 §7,宽松模式报告未知键)、路径合法性、控制面路径冲突、
`[target]` 缺当前 GOOS、target 身份全局唯一性、模板静态扫描(未声明变量)、**state 三态与语义校验诊断**
(缺失/正常/损坏/版本过新/字段残缺,后三者给出手动恢复指引)、state 记录的链接死链或
被改指、机器配置与 state 目录权限(0600/0700)、**已跟踪的 `*.local`**(交互模式警告;
`--manifest-only` 判为错误,ADR-32)、当前 OS 是否受支持。

`--manifest-only` 供 CI;**M1 提供该最小子集**(manifest 静态校验 + 路径合法性 +
target 身份唯一性 + 已跟踪 `*.local`,CI 自 lint 依赖它),完整检查集随 M2。该模式不读取
或要求机器配置/state;未传 `--profile` 时在当前 GOOS 逐个校验所有声明 profile,profile
之间不合并碰撞集合。显式 profile 只缩小 profile 级 desired 校验,仓库级与模块局部检查
仍覆盖全部 manifest;unassigned 模块只做局部检查。requires 不满足不会提前中止诊断,
但和其他错误一样使命令失败。

doctor findings 分两级:会使配置、state 或安全边界不可用的问题是 error(例如 repo/manifest/
requires/OS 非法,控制面或 target 冲突,模板静态错误,state 损坏/过新,敏感路径权限不合格,
以及 manifest-only 发现已跟踪 `*.local`);环境可用但需处理的是 warning(例如 binary 不在
PATH、链接死链/改指、交互 doctor 发现已跟踪 `*.local`)。存在任一 error → 退出 1;无 error
但有 warning → 退出 2;否则退出 0。M1 裸 `dot doctor` 明确报“完整检查需 M2”、提示使用
`--manifest-only` 并退出 1,不得把最小子集伪装成完整巡检。

### 4.7 `dot update` [M2]

配置侧更新,序贯执行(控制面校验 → 锁 → 洁净检查 → 记录旧 commit → pull → requires → apply):

1. 严格读取路径所需的机器配置,解析并校验控制面家族;失败时不得创建 lock、启动 Git 或
   产生其他写入。
2. 取锁。
3. **仓库洁净检查**:working tree 与 index 必须完全干净,**包括普通未跟踪文件**——
   `--ff-only` 遇到不冲突的本地内容仍可能成功,随后 apply 会读到新旧混合的仓库;
   非空即报错,提示先 `dot git status` 处理。Git 通常隐藏的 ignored-untracked 另作保护:
   fast-forward 若会覆盖其中任一路径,必须在工作树改变前拒绝;不会被本次更新触碰的无关
   ignored 文件不阻塞。此检查不解释 current desired,因而不在 pull 前引入 manifest/requires
   门禁;如何在改变工作树前取得稳定结论由实现决定。
4. 保存更新前 commit 供诊断与人工恢复;`git pull --ff-only`(分叉即报错,把决策还给用户
   走 `dot git`)。
5. requires 检查(不满足 → 报错提示 `dot self-update`,不执行 apply)。
6. `apply`(继续处于同一锁所有权下)。

**非事务性边界(ADR-34)**:link 直接指向活跃仓库,所以 pull 修改、删除或改名 source 时
会立即影响 target;requires 或 apply 随后失败也不自动回滚仓库。命令必须报告更新前后
commit 并给出人工恢复指引。`--no-apply` 在 pull 后停止,不校验拉取后的 manifest,也不进入
后续 requires/apply/hooks,
且**不是 link 内容的隔离预览**。拉取到的新 hook 不做单独确认(01 §4 威胁模型):可在
`--no-apply` 后用 `dot diff` 审查尚未执行的动作,再手动 `dot apply`;若新 requires 已超出
当前 CLI,则先 self-update,或用 doctor 诊断,因为 diff 本身受版本门禁。

### 4.8 `dot self-update` [M1]

二进制侧更新:解析 GitHub Releases 的最新版本(或 `--tag v0.4.0` 指定版本),选择当前
平台资产并在安装前校验发布 checksum。只有完整、校验通过的新二进制才能原子替换旧版;
任一步失败必须保留原有可用二进制。替换提交前必须满足 ADR-33 控制面家族隔离;安装路径
与 repo/config/state 重叠时拒绝,不得通过更新二进制顺带修改另一控制面。

### 4.9 `dot git [args...]` [M2]

先严格读取解析 repo 所需的机器配置并校验控制面家族,再取得与 mutation 相同的锁,最后
等价执行 `git -C <repo-dir> <args...>`。不解析子命令或 alias 来猜测其是否只读;Git 启动后
继承 stdio 并透传原始退出码。锁失败等 dot 自身错误返回 1。
仓库目录不存在时给出走 bootstrap 的提示。直接调用外部 Git 不受此锁保护,其并发风险按
01 号文档 §4 明文接受。

### 4.10 `dot version`

无论仓库状态如何都先输出 CLI 版本、commit 与构建时间。当前仓库可读时再输出顶层
`requires` 与满足情况:合法且满足 → 退出 0;缺失、非法或不满足 → 完整报告后退出 1。
仓库尚未安装时输出 `requires=unavailable` 并退出 0,让新机仍可检查二进制;非法路径/机器
配置或其他读取错误仍退出 1。`dev` 构建只放行合法 requires 的版本比较,输出 development
compatibility notice;该 notice 本身不改变退出码。

### 4.11 `dot state rebuild` [M2]

**持锁、只重建 state、不修改 target,是 state fail-closed 的受控恢复例外。**
命令先完整规划新 state,再原子替换;旧 state 文件存在时,原始字节必须先保留为不覆盖
既有文件的备份,备份失败不得替换;成功时报告精确备份路径且当前版本不自动删除。旧 state
缺失时无需虚构备份。

重建必须遵循“能安全确认的记账,需要 target mutation 的保留旧证据”原则:

- 无可用旧记录时,link 仅收养原始链接目标精确符合者;scaffold 对任何已存在 target 只补
  非所有权记录,缺失则不记录。内容与 desired 渲染结果、mode 均一致的普通文件只能报告为
  adoptable,**不得**由 rebuild 建立 rendered 删除所有权;用户须另行运行 `apply --adopt`。
  其余需要创建、替换或用户决策的 desired 均不建账,逐项报告为待处理并退出 2。
- 旧 state 语义合法且同一 target 身份已有记录时,若旧记录仍 owned、而正常 apply 需要
  重链、重渲染或 kind 转换才能达到 desired,rebuild 不得丢弃或改写该所有权证据;新 state
  保留旧 entry 作为待处理记录,逐项报告并退出 2。下一次正常 apply 仍须能够按 L3、M5 或
  ADR-27 的 owned 迁移路径完成动作。
- 当前 desired 为 scaffold 时,合法旧任意 kind 记录都表示一次性生命周期已经满足,即使
  target 缺失也应 state-only 刷新为无所有权 scaffold 记录,绝不渲染。唯一需要 target
  mutation 的分支是旧 symlink 仍 owned:必须先由正常 apply 把链接转换为独立蓝本;
  rebuild 此时按上一条保留旧 symlink/link_dest。旧链接已不 owned 时可直接释放为
  scaffold 记录。
- 旧记录仍 owned 且磁盘已经精确符合当前 desired 时,可以刷新为当前 entry。旧证据已经
  失效时不得凭历史记录创造所有权:只有第一条允许的精确 link 收养或本节明确的 scaffold
  state-only 转换可以刷新;其余旧 entry 原样保留为待处理。普通文件即使恰好符合 desired
  也只报告 adoptable,等待显式 `apply --adopt`。
- 合法旧 state 中不再对应 current desired 的 entry 是 pending prune,必须原样保留、逐项
  报告并退出 2;rebuild 不是 prune,不得绕过 owned/收敛/整模块确认而摘账。合法旧 entry
  若未命中本节明确允许的刷新或 state-only kind 转换,也必须保留为待处理,不能因“重建”
  丢掉正常 apply 仍需的证据。

祖先链被普通文件、悬空链接或特殊对象阻断,或经既有 symlink 落入控制面路径的条目不得
读取或新建/刷新记录;若有语义合法的旧 entry,必须不经观测原样保留为待处理。没有可信旧
记录时才保持不入账。上述逐项问题不阻止其他安全条目提交,但命令退出 2。指向普通目录且
不破坏路径不变量的祖先 symlink 本身不是拒绝理由。
旧 state 合法时保留全部语义合法的 run_once 历史记录,包括当前 profile 暂未引用的 hook;
损坏时因无法信任而丢弃并警告。**版本高于当前 CLI 的 state 不得 rebuild**,只能升级
CLI 或人工处理。M1 的恢复路径仍是手工备走 state 后重新 apply。

### 4.12 `dot edit <target-path>` [M2]

**持锁**。target 必须唯一对应当前 desired;命令从 desired/state 定位仓库 source,不得通过
跟随一个已 drift 的磁盘链接来猜测源文件。编辑器取首个非空的 `VISUAL`、`EDITOR`;两者均
缺失时在打开文件前报错。编辑器继承 stdio,工作目录为模块目录,source 作为最后一个文件
参数;如何把用户提供的编辑器命令拆为进程参数由实现决定。编辑器非零退出使 edit 退出 1,
不执行后续 render 或 state 更新;编辑器已经保存的 source 修改不回滚。

- link:只编辑 source,保存后由链接自然生效,不写 state、不执行 hooks/prune。
- scaffold:只编辑仓库蓝本;无论 target 存在或缺失都不渲染、不修改用户产物或 state。
- managed:编辑器成功后,对该 entry 复用正常决策表与提交前提尝试 re-render;仍按完整
  effective profile 校验全局不变量,但动作作用域只有该 entry,不执行任何 prune 或 hooks。

`edit` 不隐含 `--force`/`--adopt`:target 已 drift 或最终前提失配时,source 编辑保留而
target/state 不动并按 conflict 退出 3;模板/配置/IO 错误退出 1。用户可用 `dot diff` 审查,
再显式运行 `dot apply --force`。

## 5. 输出与日志约定

- 计划行格式:`<verb>  <target>  (<reason>)`,verb ∈ `link | render | scaffold | adopt |
  backup+replace | prune | prune (deferred) | run-hook | skip | CONFLICT`。
  verb 左对齐彩色,`skip` 仅 `-v` 显示。
- 人类输出走 stdout;错误、warning 与 compatibility notice 走 stderr;为脚本消费预留
  `--json` [M3]。
- `apply`、`add`、`update`、`state rebuild`、`edit`,以及完成 profile 选择后的 `init`,在动作
  输出前打印一行上下文:`repo=… profile=… os=darwin`。`self-update` 没有 profile 上下文;
  `dot git` 不注入该行或其他正常输出,以保持 Git 的 stdout/stderr 透传不被污染。
- 输出顺序确定性:模块、文件、prune 条目均稳定排序,便于人工比较和脚本验证。
