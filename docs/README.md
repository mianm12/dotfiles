# dot — 个人 dotfiles 管理工具 · 设计文档集(v1.3,实现前冻结版)

`dot` 是一个单人使用的 dotfiles 管理 CLI,采用「symlink 为主、模板生成为辅」的混合模型。
CLI 源码与配置内容同仓库存放,通过 GitHub Releases 分发二进制,通过 git 同步配置。

> **v1.3 为实现前冻结版。** 四轮审查的问题已从架构分叉收敛到规格自洽性、再到边角语义,
> 本轮(kind 迁移、add 原子性、锁重入等)合入后冻结;后续问题由 M1 的表驱动测试
> (决策表的可执行形态)在实现中收敛,不再推演文档。

## 文档目录

| 编号 | 文档 | 内容 | 读者时机 |
|---|---|---|---|
| 01 | [overview.md](01-overview.md) | 目标、非目标、**威胁模型**、术语表、ADR(26 条) | 先读,建立共同语言 |
| 02 | [architecture.md](02-architecture.md) | 组件划分、仓库布局、路径与锁边界、pipeline、核心类型 | 动手写代码前 |
| 03 | [manifest-spec.md](03-manifest-spec.md) | 两级 manifest 字段、两阶段加载、ignore 语义、**路径合法性** | 实现 `internal/manifest` 时 |
| 04 | [cli-spec.md](04-cli-spec.md) | 全部命令、flag(含 `--adopt`)、退出码、输出格式 | 实现 `internal/cli` 时 |
| 05 | [apply-engine.md](05-apply-engine.md) | owned 谓词、决策表、**kind 迁移**、收敛式 prune、Precond 全量复核、add | 实现 planner/executor 时 |
| 06 | [templates.md](06-templates.md) | 双模板语义、变量命名空间、fail-fast 渲染、drift 展示 | 实现 `internal/tmpl` 时 |
| 07 | [bootstrap-and-release.md](07-bootstrap-and-release.md) | bootstrap.sh、版本铰链、兼容矩阵、同步与锁 | 搭发布流水线时 |
| 08 | [testing.md](08-testing.md) | 幂等契约、--home 全隔离、决策表/集成/golden 测试 | 与功能开发同步 |
| 09 | [roadmap.md](09-roadmap.md) | M1/M2/M3、砍掉清单、风险 | 排期时 |

## 一段话看懂整体设计

仓库根目录是一个标准 Go 项目(`cmd/`、`internal/`),配置内容集中在 `modules/` 下,每个子目录是一个
**模块**,目录内部镜像其在目标机器上的路径结构(模块内 `hooks/` 为保留目录)。顶层 `dot.toml`
负责跨模块事务(profiles、全局默认、`requires`),模块内可选 `dot.toml` 负责模块自身事务。
`dot apply` 经「严格解码 → 枚举 → 全量渲染(fail-fast)→ 观测 → 纯决策 → 不变量校验 → 执行」
产生并执行动作:文件级 symlink 为默认;`.tmpl` 每次渲染(managed),`.template` 仅首次生成
(scaffold);私有内容以 `*.local` 约定留在本机。安全内核由四件事构成:**创建先于清理**且
**prune 仅在创建阶段完全收敛后执行**;一切破坏性动作以**所有权谓词**(symlink 按 state 存证的
`link_dest` 词法比较)为前置;**全部 target mutation 执行前复核 Precond**;崩溃后由收养规则
(symlink 自动、普通文件显式 `--adopt`)自愈。新机器由带校验的 `bootstrap.sh` 完成二进制安装、
仓库克隆并移交 `dot init`。

## v1.3 修订摘要(相对 v1.2)

1. **kind 迁移规则**(05 §3.4,ADR-27):堵住「`.tmpl → .template` 切换后旧 rendered 记账导致未来 prune 误删用户蓝本」的路径。三原则:所有权只能被证据延续;迁入 scaffold = 释放所有权(metadata-only,永远安全);迁出 scaffold = 等同无记录。owned link → managed 的自动迁移让「`git mv foo foo.tmpl` 模板化既有配置」无感完成。
2. **Action 补全**:`DesiredKind` 扩展到全部创建/替换动作(BackupReplace 靠它得知备份后建什么);新增 `LinkDest`(planner 侧规范化、executor 逐字写入)与 `NextEntry`(动作成功后才提交的记账,机械保证「迁移只在成功后落账」);`Adopt` 泛化为 state-only 记账动作(补录 / 迁移 / 元数据刷新)。
3. **add 加固**:只接受普通文件;换链改为「临时链 + 原子 rename」,失败时原文件必然完好;仓库副本保留 mode(含可执行位);**`--activate` 砍掉**(ADR-28,CLI 从此不写任何 manifest,报错并打印待手动添加的行)。
4. **BackupReplace 分型语义**(ADR-29):普通文件 copy+hash;symlink 备份链接本身(不跟随);目录/特殊文件即使 `--force` 也拒绝;备份目录 RFC3339Nano + 随机后缀、排他创建。
5. **锁可重入边界**:进程内只取锁一次,init/update 内部编排走 `applyLocked()`,消除 flock 同进程双重获取的自锁隐患。
6. **M1 范围矛盾消解**:managed 相关输入(`.tmpl`、`kind="managed"`、`add --template`、`apply --adopt`)在 M1 构建**硬错误**,绝不静默按普通链接处理。
7. **退出码细化**:优先级 1 > 3 > 2 > 0;拒绝孤儿确认或存在 deferred prune(无 conflict 时)→ 退出码 2。
8. **已知限制入册**(01 §4):state 丢失后 scaffold「用户有意删除」的记忆(S2)随之丢失,重跑会再生成一次蓝本——零破坏,接受。

## v1.2 修订摘要(相对 v1.1,存档)

1. **收敛式 prune**:任一 conflict / error / Precond 失配 / 用户拒绝确认 → 本次**全部** prune 延迟(plan 层标记 `deferred`,diff/dry-run 如实展示);`--force` 成功消解 conflict 视为收敛。取代 v1.1「error 跳、conflict 不跳」的双轨规则,改名场景不再可能"新旧皆无"(ADR-20)。
2. **收养不对称**:symlink 收养保持自动(证据无歧义、最坏零损失);普通文件收养降级为显式 `apply --adopt`,默认仅提示——内容巧合不构成所有权(ADR-21)。
3. **state 三态**:缺失 = 合法全新;损坏 / 版本过新 = **fail closed**(mutation 命令拒绝,status/doctor 可诊断;M1 恢复路径 = 手动备走 state 文件)(ADR-25)。
4. **owned(symlink) 词法化**:`Entry` 新增 `link_dest` 存证,所有权 = `Readlink` 与记录值的字节比较,不解析文件系统——**死链(prune 主场景)恢复可判**;删除 v1.1 的 L3 尾部启发式,仓库搬家改由 state 精确证据驱动静默重链(ADR-22)。
5. **Precond 复核扩展到全部 target mutation**(不再限于破坏性动作):Missing 必须仍 Missing、已存在者必须仍匹配记录;失配 → 降级 conflict 并触发第 1 条。CreateLink 借 `os.Symlink` 的 EEXIST 免费原子复核;rename 覆盖路径的微秒窗口列为已接受剩余风险(ADR-23)。
6. **模板渲染统一 fail-fast**:plan 阶段全量渲染,任一失败整体退出、execute 不启动;v1.1 的"单文件降级"与之矛盾,删除;`Error` ActionKind 随之不需要(ADR-24)。
7. **--force 公式定稿**:自动替换 = owned ∧ Precond;强制替换 = 显式 --force ∧ 备份成功 ∧ Precond,由 planner 直接产出 `BackupReplace` 动作;**--force 不作用于 prune**。
8. **`add --template` 修正**:原文件留在原位作为产物、登记 `kind=rendered`(v1.1 会错误地把模板链接出去);替换变量后渲染一致 → 下次 apply skip,闭环。
9. **路径合法性规则**(03 新节):模块名限单个安全路径段;`[files]` 键、hook/watch 路径必须相对且不逃逸模块目录;模块文件树内禁止 symlink 与特殊文件。
10. **不变量措辞修正**:约束「新建/更新的 state 条目属于本次 profile」;历史条目允许作为孤儿存在,由 prune 生命周期收敛(修正 ADR-18)。
11. **hooks 语义补全**:明确 at-least-once、脚本必须自我幂等;指纹改为对 `(路径, 长度, 内容)` 元组序列 hash(消除裸拼接歧义);`[hooks]` 引用路径统一归**内置忽略**层级(不可被 `[files]` 覆盖);update 拉到新 hook 不做单独确认,审查方式 = `--no-apply` + `dot diff`。
12. **杂项**:mode 漂移经复用 `Render`(reason="mode")修正,不引入 chmod 动作与 `Entry.mode`(ADR-26);锁边界澄清(diff/status 只读不取锁,update 自 `git pull` 起持锁);01 新增「威胁模型与剩余风险」一节。

## v1.1 修订摘要(相对 v1.0,存档)

prune 双作用域与 profile 不变量;owned() 谓词引入;执行顺序反转(创建先于 prune);收养规则与
flock 锁;target 全局不变量校验;`hooks/` 保留目录与 watch;manifest 两阶段加载(严格解码);
砍 `env` 函数改 `from_env` 与变量命名空间;`*.local` 四道加固;bootstrap 校验与原子安装;
drift diff 降级为「实际 vs 本次渲染」;run_once 最小实现提前进 M1。

## 文档约定

- 规范用词遵循 RFC 2119:**必须(MUST)**、**不得(MUST NOT)**、**应当(SHOULD)**、**可以(MAY)**。
- 标注 `[M2]` / `[M3]` 的内容属于后续里程碑,M1 实现时只需保证格式不与之冲突。
- 所有示例路径以 macOS 为准,Linux 差异处会显式标注。
