# 04 · CLI 规范:命令、Flag 与输出

## 1. 总览

```
dot <command> [flags] [args]

核心      init | apply | diff | status | add | doctor
同步      update | self-update | git
辅助      version | state rebuild [M2] | edit [M2]
```

## 2. 全局 flag

| flag | 说明 |
|---|---|
| `--repo <dir>` | 覆盖仓库位置(> `DOT_REPO` > 机器配置 > 默认) |
| `--home <dir>` | **隐藏**。整体重定向 `~`、全部状态路径**及 hook 子进程环境**,测试专用 |
| `--profile <name>` | 本次运行覆盖机器配置中的 profile |
| `-v / --verbose` | 详细输出(含 skip 项与内容 diff) |
| `--no-color` | 关闭彩色输出(pipe 时自动关闭) |

除 `dot git` 对已启动 Git 进程透传退出码外,全部命令使用同一套错误到退出码映射;内部
实现只需传递足以分类的语义化错误。锁边界:mutation 命令(含 [M2] 的 `state rebuild`、
`edit`)与 `dot git` 持锁;`diff` / `status` / `doctor` 只读不取锁(02 号文档 §2)。

## 3. 退出码(`dot git` 除外)

| 码 | 含义 |
|---|---|
| 0 | 成功;对 `diff` / `status` 表示「无差异 / 无异常」 |
| 1 | 运行错误(IO 失败、manifest 非法、requires 不满足、不变量校验失败、锁被占用、state fail-closed…) |
| 2 | `diff` / `status` 发现差异或 drift(供脚本判断) |
| 3 | 存在 conflict,需要用户决策 |

同一次运行满足多个条件时按 **1 > 3 > 2 > 0** 取最高优先级。`apply` 中「用户拒绝整模块
孤儿确认」或「存在 deferred prune 而无 conflict」以退出码 2 结束——工作未完成,与
`diff` 的「有差异」语义一致。`dot git` 在 Git 启动前发生的 dot 自身错误仍返回 1;
Git 一旦启动则原样透传其退出码(§4.9)。

## 4. 命令规范

### 4.1 `dot init`

新机器初始化,幂等(重复运行进入「更新变量」模式)。流程:

1. 定位仓库(不存在则报错,提示走 bootstrap)。
2. requires 检查(宽松预读)。
3. 交互选择 profile(列出顶层 `[profiles]` 键)。
4. 按顶层 `[data]` 声明逐项询问变量(有 default 或 `from_env` [M2] 命中时回车即接受)。
5. **安全更新机器配置**:严格读取现有文件;本次未指定的 profile/repo/data 必须保留;
   未知顶层字段、旧文件损坏或合并结果非法时拒绝重写。新配置以 0600 原子替换,
   任一步失败**保留旧配置不变**。
6. 询问「立即 apply?」,默认 yes。

Flag:`--profile <name>`、`--set key=value`(可重复)、`--yes` 支持无人值守:
`dot init --profile mac --set email=me@x.com --yes`。`--set` 引用未声明键,或无人值守时
仍缺 profile/必填变量,必须在写配置前报错。

### 4.2 `dot apply [module...]`

核心命令,pipeline 见 02 号文档 §4。**prune 作用域随调用形态变化**:

- **无参数(全量)**:应用当前 profile 全部模块;prune 候选 = 全部 state 条目中
  不在 desired 的。
- **指定模块(部分)**:仅应用给定模块;prune 候选 = 仅 `entry.module ∈ 请求集` 的
  孤儿条目。
- 请求的模块 **∉ 当前 profile → 报错**(ADR-18),提示将其加入 profile 后重试。
- 无论全量或部分调用,target 身份唯一、祖先冲突和控制面隔离都按**完整 effective profile**
  校验;部分调用只缩小动作与 prune 作用域,不能绕过配置错误。

| flag | 行为 |
|---|---|
| `-n / --dry-run` | 只打印计划,不落盘(含 state),退出码规则同 `diff` |
| `--force` | 把可强制处理的 conflict 计划为 `backup-replace`(目录/特殊文件除外,ADR-29);另允许 S2 在 target 仍缺失时显式重建 scaffold,该分支不替换对象、无需备份 |
| `--adopt` [M2] | 允许收养「内容与渲染结果、mode 均一致但无有效所有权记录」的**普通文件**(05 号文档 §3.2 M3c,managed 专属);adopt 只写 state。mode 不一致时必须先由用户对齐或显式 `--force`;symlink 与无所有权的 scaffold 记录自动补录。M1 构建给出硬错误 |
| `--prune` / `--no-prune` | 是否计划 prune 阶段,默认 `--prune` |
| `-y / --yes` | 跳过交互确认(目前唯一确认点:整模块级孤儿清理) |

行为要点:

- 执行顺序 `mkdir → 创建/收养 → prune → hooks`(ADR-13);**prune 仅在创建阶段完全
  收敛后执行**(ADR-20):plan 中存在 conflict、执行中出现 error 或 Precond 失配、
  或用户拒绝整模块确认,任一发生 → 本次**全部** prune 转为 deferred(输出中标注,
  不执行),提示消解后重跑。hooks 不受收敛门控(05 号文档 §8)。
- 存在 conflict 且无 `--force` 时,其余创建动作照常执行,conflict 项汇总列出,
  退出码 3(部分成功优于全盘卡死,幂等保证重跑无害)。
- prune 集合中出现**整模块级孤儿**(典型于 profile 切换)→ 打印汇总并要求确认(y/N),
  `--yes` 跳过;拒绝 = 本次全部 prune 延迟,并以退出码 2 结束(工作未完成)。
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

UNASSIGNED MODULES (1)
  experimental-nvim             not referenced by any profile
```

无异常输出 `Clean.`,退出码 0;有 DRIFT/PENDING 退出码 2。state 损坏、版本过新或
语义校验失败时,status 不崩溃而是报告该事实(fail-closed 诊断入口,05 号文档 §2)。

### 4.5 `dot add [-m <module>] [--template|--scaffold] [--dry-run] <path>...`

把 `$HOME` 中已有文件收编入库。反向映射与模块推断算法见 05 号文档 §9。要点:

- **只接受普通文件**:目录、symlink、特殊文件一律拒绝(目录请逐文件 add 或手工搬移)。
- **预检与 apply 同源**(ADR-30):任何写入前,候选 source 必须按正常解析/枚举规则
  恰好产生一个 desired entry,且 target 等于输入、kind 符合模式;命中内置忽略、
  `hooks/` 或 ignore pattern → 拒绝。仓库目标及其模板后缀变体已存在时默认拒绝且绝不
  覆盖;唯一例外是期望路径上的 source 可证明与本次本应写入的内容完全等价,此时允许
  不改写 source 而安全续跑。
  多路径输入全部通过路径、唯一性和碰撞校验后才开始执行。
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
- **默认(link)**:先在不存在、或仅有上述等价 source 的仓库路径准备完整、权限一致且
  已验证的 source,再于提交前
  复核原文件未变,最后原子换成指向该 source 的链接。target 被链接替换是提交点:
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
  state 建账存在,建账前还须复核原文件内容/mode 仍等于执行前快照。state 条目提交前发生
  错误时,只能清理仍可证明由本轮创建且未变化的
  source,否则保留并允许等价续跑;条目一旦提交,后续输入失败也不得删除其 source 或回滚
  该成功项。
- `--dry-run` 打印预检结论与将执行的动作。

### 4.6 `dot doctor`

环境与配置自检,**严格只读、无锁**。检查项:二进制是否在 PATH、仓库存在且为 git 仓库、
manifest 静态校验(03 号文档 §7,宽松模式报告未知键)、路径合法性、控制面路径冲突、
`[target]` 缺当前 GOOS、target 身份全局唯一性、模板静态扫描(未声明变量)、**state 三态与语义校验诊断**
(缺失/正常/损坏/版本过新/字段残缺,后三者给出手动恢复指引)、state 记录的链接死链或
被改指、机器配置与 state 目录权限(0600/0700)、**已跟踪的 `*.local`**(交互模式警告;
`--manifest-only` 判为错误,ADR-32)、当前 OS 是否受支持。

`--manifest-only` 供 CI;**M1 提供该最小子集**(manifest 静态校验 + 路径合法性 +
target 身份唯一性 + 已跟踪 `*.local`,CI 自 lint 依赖它),完整检查集随 M2。

### 4.7 `dot update`

配置侧更新,**自持锁起序贯执行**(锁 → 洁净检查 → 记录旧 commit → pull → requires → apply):

1. 取锁。
2. **仓库洁净检查**:working tree 与 index 必须完全干净,**包括未跟踪文件**——
   `--ff-only` 遇到不冲突的本地内容仍可能成功,随后 apply 会读到新旧混合的仓库;
   非空即报错,提示先 `dot git status` 处理。如何调用 git 获取稳定状态由实现决定。
3. 保存更新前 commit 供诊断与人工恢复;`git pull --ff-only`(分叉即报错,把决策还给用户
   走 `dot git`)。
4. requires 检查(不满足 → 报错提示 `dot self-update`,不执行 apply)。
5. `apply`(继续处于同一锁所有权下)。

**非事务性边界(ADR-34)**:link 直接指向活跃仓库,所以 pull 修改、删除或改名 source 时
会立即影响 target;requires 或 apply 随后失败也不自动回滚仓库。命令必须报告更新前后
commit 并给出人工恢复指引。`--no-apply` 在 pull 后停止,不进入后续 requires/apply/hooks,
且**不是 link 内容的隔离预览**。拉取到的新 hook 不做单独确认(01 §4 威胁模型):可在
`--no-apply` 后用 `dot diff` 审查尚未执行的动作,再手动 `dot apply`。

### 4.8 `dot self-update`

二进制侧更新:解析 GitHub Releases 的最新版本(或 `--tag v0.4.0` 指定版本),选择当前
平台资产并在安装前校验发布 checksum。只有完整、校验通过的新二进制才能原子替换旧版;
任一步失败必须保留原有可用二进制。

### 4.9 `dot git [args...]`

先取得与 mutation 相同的锁,再等价执行 `git -C <repo-dir> <args...>`。不解析子命令或 alias
来猜测其是否只读;Git 启动后继承 stdio 并透传原始退出码。锁失败等 dot 自身错误返回 1。
仓库目录不存在时给出走 bootstrap 的提示。直接调用外部 Git 不受此锁保护,其并发风险按
01 号文档 §4 明文接受。

### 4.10 `dot version`

输出 CLI 版本、commit、构建时间,以及当前仓库顶层 `requires` 与满足情况
(`dev` 构建注明 requires 检查处于放行状态)。

### 4.11 `dot state rebuild` [M2]

**持锁、只重建 state、不修改 target,是普通 mutation fail-closed 的受控恢复例外。**
命令先完整规划新 state,再原子替换;旧 state 原始文件必须先保留为不覆盖既有文件的备份,
备份失败不得替换。对当前 profile 的 desired 逐项观测:link 仅收养原始链接目标精确符合者;
rendered 仅收养内容与 desired 渲染结果、mode 均一致者;scaffold 对任何已存在 target 只补
非所有权记录,缺失则不记录。其余 link/rendered 不进入新 state 并逐项报告;这不阻止安全的
部分新 state 提交,但命令以退出码 2 表示仍有未收养项。旧 state 合法时保留仍有效的
run_once 记录,损坏时丢弃并警告。**版本高于当前 CLI 的 state 不得 rebuild**,只能升级
CLI 或人工处理。M1 的恢复路径仍是手工备走 state 后重新 apply。

### 4.12 `dot edit <target-path>` [M2]

**持锁**。target 必须唯一对应当前 desired;命令从 desired/state 定位仓库 source,不得通过
跟随一个已 drift 的磁盘链接来猜测源文件。symlink source 保存后由链接自然生效;managed
模板保存后按正常单条 apply 决策尝试 re-render。`edit` 不隐含 `--force`:target 已 drift、
模板渲染失败或最终前提失配时,源文件编辑保留而 target/state 不动,报告原因;用户可先用
`dot diff` 审查,再显式运行 `dot apply --force`。

## 5. 输出与日志约定

- 计划行格式:`<verb>  <target>  (<reason>)`,verb ∈ `link | render | scaffold | adopt |
  backup+replace | prune | prune (deferred) | run-hook | skip | CONFLICT`。
  verb 左对齐彩色,`skip` 仅 `-v` 显示。
- 人类输出走 stdout;错误与警告走 stderr;为脚本消费预留 `--json` [M3]。
- 所有会写文件系统的命令开头打印一行上下文:`repo=… profile=… os=darwin`。
- 输出顺序确定性:模块、文件、prune 条目均稳定排序,便于人工比较和脚本验证。
