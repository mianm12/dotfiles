# 08 · 测试策略:把规范变成可执行证据

## 1. 原则

测试验证设计文档规定的性质,不替设计文档发明新语义。内部类型、helper、调用顺序和故障
注入方式可以随实现调整;以下外部结果与安全边界不得因重构改变:

1. **幂等**:一次成功收敛后,相同输入再次 apply 不产生文件 mutation 或重复收养。
2. **不误删、不误覆盖**:非 owned 对象不被自动删除;未显式授权且没有可用备份时不覆盖。
3. **失败安全**:最终前提失配、IO 错误或用户拒绝时,未提交动作不产生副作用,prune 延迟;
   已越过提交点的数据保留并可由重跑收敛。
4. **单一语义源**:add 与 apply 使用同一解析/枚举结果;doctor 与 mutation 复用同一边界校验。
5. **兼容失效安全**:未知 manifest、损坏/过新 state 和控制面路径冲突在 mutation 前失败。

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
| manifest | 两阶段加载、无模块 manifest 继承 defaults、整键覆盖与 ignore 并集、profile 环/悬空、OS/target、路径与控制面边界、未知键失效安全 |
| 决策表 | L1–L6、M1–M6(含 M3a–M3d)、S1a–S3、P1–P3 每一行及短路顺序 |
| kind 迁移 | 全组合;旧证据不成立时按新 kind 无记录语义决策;迁入 scaffold 释放所有权并把既有记录视为生命周期已满足,target 缺失不重建 |
| 状态转换 | 成功动作才写账;活动 prune 才删账;skip/conflict/deferred/失败保留旧账;hook 指纹仅成功后更新 |
| 全局校验 | 文件系统等价的 target 别名、后缀碰撞、祖先冲突、中间目录穿文件、控制面路径重叠;部分 apply 仍校验完整 effective profile |
| state | 缺失合法;解析、版本、kind、target、module/source、证据字段异常、多个 key 指向同一 target 及未知/不合格摘要标识均 fail closed;单个历史别名与 desired 合并而不成为 orphan |
| 模板与 hook | missingkey、命名空间、fail-fast;指纹输入集合稳定且不含 profile/data |

决策测试应覆盖表中每一行及边界组合,并围绕输入状态与可观察结果断言;测试组织由实现决定。

### 3.2 文件系统与命令集成测试

| 风险类别 | 必须证明的结果 |
|---|---|
| 新机与幂等 | init/apply 产生正确 link、rendered、scaffold 和 state;第二次无 mutation |
| conflict/force | 默认文件不动;force 只有在备份完整可用且最终前提成立时替换;目录/特殊文件仍拒绝;S2 只在 target 仍缺失时无备份重建 scaffold |
| prune 收敛 | 创建失败、conflict、前提失配或拒绝确认时一个 prune 都不执行;干净重跑后收敛 |
| 所有权 | 改指链接、手改 rendered、非 owned orphan 均不被自动删除;死链仍可按存证清理 |
| 崩溃恢复 | link 已创建未记账可自动收养;rendered 证据过期只提示显式 adopt;scaffold 已存在无记录时自动补非所有权记录;成功动作不会因后续失败丢账 |
| add | 被 ignore、`*.local`、控制面路径、碰撞和非普通文件在写入前拒绝;`--template`/`--scaffold` 仅在渲染字节/mode 与原文件一致时建账;正常 add 后 apply 不产生孤儿 |
| add 提交点 | source 发布后中断仍保留原 target,等价遗留 source 可安全续跑且不等价者不被覆盖;link 替换后 state 失败保留 source/link;template/scaffold 建账后任何后续错误不得清理 source |
| 最终前提 | 临时产物或备份准备期间 target 发生变化时拒绝提交;link source 失效时不得创建死链且全部 prune 延迟 |
| update/锁 | 工作区或 index 不洁净(含未跟踪文件)时 pull 前失败;mutation 与 `dot git` 互斥,只读命令不因锁阻塞;Git 启动后退出码透传;pull 后失败保留新仓库并报告新旧 commit/恢复指引;`--no-apply` 不伪装成 link 隔离预览 |
| hook | cwd、环境覆盖、首次/变化/失败重试、at-least-once 与 conflict 下仍执行符合规范 |
| 恢复/编辑 [M2] | rebuild 备份旧 state,link 精确匹配、rendered 内容/mode 均匹配才收养;不匹配项不入账但安全部分仍提交并退出 2;已有 scaffold 只补非所有权记录;版本过新仍拒绝;edit 不跟随 drift 链猜 source、不隐含 force |

如何制造中断、替换目标或模拟落盘失败由测试实现决定。重要的是覆盖每个提交点的前后两侧,
而不是把某个 helper 的调用顺序固化成公共接口。

### 3.3 不可删除的回归性质

以下是历轮审查发现的数据安全路径,实现重构时必须长期保留回归测试:

- 源改名且新 target conflict 时,旧产物不被 prune。
- 大小写或 Unicode 写法不同但文件系统身份相同的 state/desired 必须合并;旧 key 不得作为
  orphan 删除当前 desired。完整 profile 有别名碰撞时,部分 apply 也必须在 mutation 前拒绝。
- state 中精确 `link_dest` 可以识别并清理死链;相似路径或被改指链接不能冒充 owned。
- rendered/symlink → scaffold 在迁移当次释放所有权;target 缺失时迁移记账但不重建;
  只有随后显式 `--force` 才能按 S2 重建。
- L3 重链后未记账可以由精确期望链接恢复;rendered 的过期 hash 不得自动变成新所有权证据。
- M3c 的 adopt 必须保持 state-only;mode 不匹配的非 owned 文件不得借 adopt 修改。
- scaffold 创建成功但落账失败时,重跑必须补录非所有权记录;用户随后删除不得触发再次生成。
- add 进命中 ignore 的文件(如配置为忽略的 README.md)或 hooks 路径必须被拒绝,不能生成
  下轮即成为 orphan 的产物。
- `add --template`/`--scaffold` 面对会改变字节的模板语法或不匹配的 mode 必须在写入前拒绝。
- 默认 link add 在 source 发布后、target 提交前中断时,重跑只能复用完全等价的 source,不得覆盖
  不等价的仓库内容或永久卡死。
- add 越过 target 替换提交点后,任何错误都不得删除仓库 source。
- `add --template`/`--scaffold` 的 state 一旦建账,后续项失败也不得删除对应 source。
- repo/config/state/backup/binary 不得被 desired、state 或 add 纳入管理。
- 备份或临时产物准备期间 target 改变时,最终 Precond 必须阻止覆盖。
- link source 在 plan 后失效时不得创建死链或执行任何 prune。
- `edit` 面对 drift target 时必须保留源编辑但不覆盖 target,由用户另行显式 force。

## 4. 输出、CI 与冒烟

- `diff`、`status`、`apply --dry-run` 的公开输出必须确定且受测试保护;路径和时间等环境值
  可在断言前规范化。内部 debug 文本不属于输出契约。
- CI 在 macOS 与 Linux 运行测试、静态检查和 `dot doctor --manifest-only`;已跟踪的
  `*.local` 必须使该检查失败。
- 发布前在隔离账户或容器走通 bootstrap → init → apply → git 同步 → update,并验证
  checksum 失败、旧二进制安装失败保护和真实 HOME 零写入。

新增测试只有在发现新风险或公共行为时才回写本文件;普通实现分支直接记录在测试代码中。
