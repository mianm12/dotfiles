# 08 · 测试策略:把规范变成可执行证据

## 1. 原则

测试验证设计文档规定的性质,不替设计文档发明新语义。内部类型、helper、调用顺序和故障
注入方式可以随实现调整;以下外部结果与安全边界不得因重构改变:

1. **幂等**:一次成功收敛后,相同输入再次 apply 不产生文件 mutation 或重复收养。
2. **不误删、不误覆盖**:非 owned 对象不被自动删除;未显式授权且没有可用备份时不覆盖。
3. **失败安全**:最终前提失配、IO 错误或用户拒绝时,未提交动作不得产生破坏性副作用,
   target/state 保持详细契约规定的状态且 prune 延迟;已发布但无法安全清理的 source/backup/
   临时准备结果可以按恢复契约保留,并必须可由重跑安全识别。已越过提交点的数据不回滚。
4. **单一语义源**:add 与 apply 使用同一解析/枚举结果;doctor 与 mutation 复用同一边界校验。
5. **兼容失效安全**:未知 manifest 在依赖它生成 desired/计划/state 前失败;损坏/过新 state
   在依赖旧 state 的阶段前失败;init 配置提交与 update pull 的明确例外不被误伤。

## 2. 隔离测试环境

集成测试必须把 HOME、机器配置、state、backup、repo 以及 hook 子进程的 HOME/XDG 环境
全部放入临时根目录,并显式断言真实 HOME 没有变化。生产和测试必须经过同一套路径边界;
不得为测试增加绕过安全校验的专用实现。

测试可以按成本选择进程内或真实进程执行;必须有少量端到端场景覆盖进程间锁、子进程和
安装替换。输入输出注入方式、helper 与 fixture 布局由测试代码决定。

## 3. 分层验证

### 3.1 纯规则测试

| 规范 | 必须覆盖 |
|---|---|
| manifest | requires 与 profiles 必填/语法/两阶段加载、顶层/defaults/module/files/hooks/data 精确 schema、target 两种形态与 os 组合、无模块 manifest 继承 defaults、整键覆盖、ignore 并集与完整匹配子集、profile 环/悬空、target root/entry target 合法域、mode、from_env 名称、run_once/watch 唯一、未知键失效安全 |
| 机器配置/路径 | 仅允许 profile/repo/data 顶层、类型与值严格;旧 data 字符串键可保留但不成为未声明模板输入;声明 data 缺失不回退 manifest/env;控制面路径输入展开为与 cwd 无关的唯一绝对路径,非法相对/`~` 写法零写入;任何消费者不得非法后回退默认 |
| 决策表 | L1–L6、M1–M6(含 M3a–M3d)、S1a–S3、P1–P3 每一行及短路顺序 |
| kind 迁移 | 全组合;旧证据不成立时按新 kind 无记录语义决策;迁入 scaffold 释放所有权并把既有记录视为生命周期已满足,target 缺失不重建 |
| 状态转换 | 成功动作才写账;活动 prune 才删账;skip/conflict/deferred/失败保留旧账;hook 指纹仅成功后更新 |
| 全局校验 | 大小写/Unicode/既有祖先 symlink 形成的 target 别名、后缀碰撞、解析后逻辑祖先、展示路径中间目录项穿文件、控制面家族两两隔离及与 entry target 重叠;已有目录 symlink `A` 时 desired `A`/`A/child` 冲突,但不把 `A` leaf 与直接写成 `real/child` 的路径误判为祖先;部分 apply 仍校验完整 effective profile |
| state | v1 必填/分 kind 字段与诊断时间字段、缺失合法;任意层级重复 JSON member、解析、未知字段、版本、kind、target、module/source、证据字段异常、多个 key 指向同一 target 及未知/不合格摘要标识均 fail closed;单个历史别名与 desired 合并而不成为 orphan |
| 模板与 hook | missingkey、变量命名空间、函数白名单、mode 口径、fail-fast;run_once 精确 schema/script/watch 去重;watch 重排不改指纹、执行方式变化会改指纹,且指纹不含 profile/data |

决策测试应覆盖表中每一行及边界组合,并围绕输入状态与可观察结果断言;测试组织由实现决定。

### 3.2 文件系统与命令集成测试

| 风险类别 | 必须证明的结果 |
|---|---|
| 新机与幂等 | init/apply 产生正确 link、rendered、scaffold 和 state;第二次无 mutation |
| dry-run 纯度 | apply/add 的 dry-run 不创建 state 目录、lock、source、target 或任何临时文件,即使在全新 HOME 也没有 dot 发起的写入 |
| init 提交边界 | 交互/准备期间配置被创建或改变时不覆盖且不 apply;无终端且非交互输入不完整时零写入;非法旧配置不回退默认;显式 `--repo`/`DOT_REPO` 在移除覆盖后的下一进程仍解析到同一仓库 |
| 加载/恢复边界 | 非法 state 使 apply/add/diff 拒绝且 status 退出 1,但不阻止 init 的安全配置提交、self-update、dot git 或 update pull;这些前置只校验控制面家族,后续 apply 仍拒绝;M1 读到预留的 rendered 条目按不支持 fail closed;doctor 在 requires 不满足时继续收集诊断 |
| conflict/force | 默认文件不动;force 只有在备份字节/普通 mode 或链接文本完整可用且最终前提成立时替换,逐项报告精确路径且后续运行不自动删除;目录/特殊文件仍拒绝;S2 只在 target 仍缺失时无备份重建 scaffold |
| prune 收敛 | 创建失败、conflict、前提失配或拒绝确认时一个 prune 都不执行;整模块组按完整 desired 判定且 P1/P2/P3 一次汇总;干净重跑后收敛 |
| 所有权 | 改指链接、手改 rendered、非 owned orphan 均不被自动删除;死链仍可按存证清理 |
| 崩溃恢复 | link 已创建未记账可自动收养;rendered 证据过期只提示显式 adopt;scaffold 已存在无记录时自动补非所有权记录;成功动作不会因后续失败丢账 |
| add | 被 manifest ignore、Git ignore/exclude、`*.local`、控制面路径、碰撞和非普通文件在写入前拒绝;候选加入后的等价校验允许仅与请求 source 对应的 `[files]` 预声明 mode/kind/target,无关悬空引用仍拒绝;命令不暂存;`--template`/`--scaffold` 仅在渲染字节/mode 与原文件一致时建账;正常 add 后 apply 不产生孤儿 |
| add 提交点 | source 发布后中断仍保留原 target,等价遗留 source 可安全续跑且不等价者不被覆盖;link 替换后 state 失败保留 source/link;template/scaffold 建账后任何后续错误不得清理 source |
| 提交前提 | 临时产物或备份准备期间 target/祖先拓扑发生变化且使计划证据失效时拒绝提交;指向目录且不破坏身份/控制面边界的祖先 symlink 正常工作,普通文件、悬空链接、特殊对象或通向控制面的别名不得穿越或摘账;link source 失效时不得创建死链且全部 prune 延迟 |
| hard link 隔离 | 两个不同 entry target 共享 inode 时,对一个的内容/mode mutation 不得改变另一个;无法隔离则提交前拒绝或 conflict |
| update/锁 | 工作区或 index 不洁净(含普通未跟踪文件)、或 fast-forward 将覆盖 ignored-untracked 路径时在工作树改变前失败;不会被更新触碰的 ignored 文件不阻塞;mutation 与 `dot git` 互斥,只读命令不因锁阻塞;Git 启动后退出码透传;pull 后失败保留新仓库并报告新旧 commit/恢复指引;`--no-apply` 不校验拉取后 manifest、不伪装成 link 隔离预览 |
| hook | cwd、环境覆盖、首次/内容变化/执行方式变化/失败重试、at-least-once、模块字节序与数组声明顺序、前项失败停止后项且保留此前成功指纹、profile 移除再恢复同指纹不重跑,以及 conflict 下仍执行符合规范;部分 apply 不运行或更新未请求模块 hook |
| 恢复/编辑 [M2] | rebuild 备份并报告旧 state 路径;无可信证据时 link 精确匹配才收养,rendered 即使内容/mode 匹配也只报 adoptable;合法旧 orphan 与祖先暂不可达 entry 原样保留待处理,安全部分仍可提交并退出 2;L3/M5/跨 kind target mutation 证据不得丢;合法旧任意 kind 面对当前 scaffold 缺失时按迁移规则记账且不渲染,其中仍 owned 的旧 symlink→scaffold 保留旧证据直到正常 apply 转换;版本过新仍拒绝。edit 各 kind 行为、编辑器缺失/非零、完整 profile 校验、managed 单条决策和不执行 prune/hooks 均符合 §4.12 |
| status/doctor/version | status 对 drift/orphan 退出 2、UNASSIGNED 单独不影响 0、非法 state/manifest 退出 1 且不报 Clean;diff 对同一被改指链接按计划 conflict 退出 3;manifest-only 不读 config/state并按当前 GOOS 逐 profile 校验,profile 间不合并;M1 裸 doctor 拒绝;doctor error/warning 分别映射 1/2;version 始终报告 binary,并覆盖 repo 缺失、requires 满足/不满足/非法分支及 dev compatibility notice 不改变退出码 |

如何制造中断、替换目标或模拟落盘失败由测试实现决定。重要的是覆盖每个提交点的前后两侧,
而不是把某个 helper 的调用顺序固化成公共接口。

### 3.3 不可删除的回归性质

以下是历轮审查发现的数据安全路径,实现重构时必须长期保留回归测试:

- 源改名且新 target conflict 时,旧产物不被 prune。
- 大小写、Unicode 或既有祖先 symlink 写法不同但文件系统身份相同的 state/desired 必须
  合并;旧 key 不得作为 orphan 删除当前 desired。完整 profile 有别名碰撞时,部分 apply
  也必须在 mutation 前拒绝。单独存在且指向普通目录的祖先 symlink 不应被当作恶意环境拒绝。
- 已有目录 symlink `A` 时,desired 文件 `A` 与 `A/child` 必须因中间目录项穿文件而拒绝;
  `A` leaf 与其目标目录仍是不同身份,直接写成目标真实路径的 child 不得仅因该 symlink
  产生伪祖先关系。
- 递归 symlink target 展开的中间项不能遗漏:若 `A -> B -> real`,则 `B` 与 `A/child` 冲突,
  但不与直接写成 `real/child` 的路径冲突;link target 中被 `..` 折返的既有目录也按实际
  遍历项覆盖。
- state 中精确 `link_dest` 可以识别并清理死链;相似路径或被改指链接不能冒充 owned。
- rendered/symlink → scaffold 在迁移当次释放所有权;target 缺失时迁移记账但不重建;
  只有随后显式 `--force` 才能按 S2 重建。
- L3 重链后未记账可以由精确期望链接恢复;rendered 的过期 hash 不得自动变成新所有权证据。
- 对合法 state 执行 rebuild 不得丢掉仍 owned 的 L3/M5 或跨 kind 待迁移证据;重建后的正常
  apply 必须保持原本的自动安全收敛能力。
- state 缺失/损坏时,与 desired 渲染字节和 mode 巧合一致的用户普通文件不得被 rebuild
  建立 rendered 所有权;只能提示用户显式 `apply --adopt`。
- rebuild 不得摘除合法旧 orphan,也不得因祖先暂时不可达而丢弃旧 entry;恢复拓扑后的正常
  apply 必须仍能执行原本的 prune/收敛语义。
- M3c 的 adopt 必须保持 state-only;mode 不匹配的非 owned 文件不得借 adopt 修改。
- scaffold 创建成功但落账失败时,重跑必须补录非所有权记录;用户随后删除不得触发再次生成。
- add 进命中 ignore 的文件(如配置为忽略的 README.md)或 hooks 路径必须被拒绝,不能生成
  下轮即成为 orphan 的产物。
- add 的候选 source 命中任一有效 Git ignore/exclude 时必须零写入;已跟踪 source 与
  ignored-untracked 等价遗留 source 分别覆盖允许和拒绝分支。
- `add --template`/`--scaffold` 面对会改变字节的模板语法或不匹配的 mode 必须在写入前拒绝。
- 默认 link add 在 source 发布后、target 提交前中断时,重跑只能复用完全等价的 source,不得覆盖
  不等价的仓库内容或永久卡死。
- add 越过 target 替换提交点后,任何错误都不得删除仓库 source。
- `add --template`/`--scaffold` 的 state 一旦建账,后续项失败也不得删除对应 source。
- repo/config/state/backup/binary 不得被 desired、state 或 add 纳入管理。
- repo/config/state 家族/binary 彼此不得相等、互为祖先或以文件系统别名重叠;在 lock/Git/
  self-update 写入前拒绝。
- 备份或临时产物准备期间 target 改变时,提交时 Precond 必须阻止覆盖;测试断言结果与
  提交边界,不固化某个 helper 的复核调用顺序。
- dot 的崩溃遗留临时产物不得被枚举成 source/desired/state;只有仍可证明归工具且未变化时
  才能清理。
- link source 在 plan 后失效时不得创建死链或执行任何 prune。
- `edit` 面对 drift target 时必须保留源编辑但不覆盖 target,由用户另行显式 force。
- 部分 apply 不得执行未请求模块的 pending hook,也不得写入其 run_once 指纹。

### 3.4 里程碑验收集合

测试随首次暴露对应公共行为的里程碑成为门禁,而不是等“全功能”后补做。M1 至少覆盖
L/S/P 决策、symlink↔scaffold 迁移、requires/严格配置/state fail-closed、link/scaffold
apply/add、force/backup/prune/最终前提/崩溃收养、status、`doctor --manifest-only`、
bootstrap 与 self-update。M2 增加 managed 的 M1–M6、rendered/adopt/`add --template`、
涉及 managed/rendered 的 kind 迁移、update/git、from_env/watch、edit、state rebuild 与
完整 doctor。决策表编号 M1–M6 不表示路线图里程碑。

## 4. 输出、CI 与冒烟

- `diff`、`status`、`apply --dry-run` 的公开输出必须确定且受测试保护;路径和时间等环境值
  可在断言前规范化。内部 debug 文本不属于输出契约。
- CI 在 macOS 与 Linux 运行测试、静态检查和 `dot doctor --manifest-only`;已跟踪的
  `*.local` 必须使该检查失败。
- M1 发布前在隔离账户或容器走通 bootstrap → init → apply → self-update,并验证管道启动时
  交互来自用户终端、无终端时停在 init 前并提示直接运行非交互 init、checksum 失败、旧
  二进制安装失败保护、无关既有 Git 仓库拒绝、canonical clone 安全复用、自定义 repo 在
  init 后持久化和真实 HOME 零写入。M2 再把 git 同步 → update 加入同一冒烟链路。

新增测试只有在发现新风险或公共行为时才回写本文件;普通实现分支直接记录在测试代码中。
