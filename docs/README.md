# dot — 个人 dotfiles 管理工具 · 设计文档集(v1.5,冻结版)

`dot` 是一个单人使用的 dotfiles 管理 CLI,采用「symlink 为主、模板生成为辅」的混合模型。
CLI 源码与配置内容同仓库存放,通过 GitHub Releases 分发二进制,通过 git 同步配置。

> **v1.5 为冻结版。** 第六轮审查已全部为契约澄清与勘误级问题、零架构缺陷;
> 本轮合入后文档冻结,此后一切问题由 M1 的表驱动测试(决策表的可执行形态)
> 在实现中收敛。

## 文档目录

| 编号 | 文档 | 内容 | 读者时机 |
|---|---|---|---|
| 01 | [overview.md](01-overview.md) | 目标、非目标、**威胁模型**、术语表、ADR(32 条) | 先读,建立共同语言 |
| 02 | [architecture.md](02-architecture.md) | 组件划分、路径与锁边界、pipeline、**强类型核心结构** | 动手写代码前 |
| 03 | [manifest-spec.md](03-manifest-spec.md) | 两级 manifest 字段、两阶段加载、ignore 语义、路径合法性 | 实现 `internal/manifest` 时 |
| 04 | [cli-spec.md](04-cli-spec.md) | 全部命令、flag、退出码、输出格式 | 实现 `internal/cli` 时 |
| 05 | [apply-engine.md](05-apply-engine.md) | owned 谓词、决策表、kind 迁移、收敛式 prune、add | 实现 planner/executor 时 |
| 06 | [templates.md](06-templates.md) | 双模板语义、变量命名空间、fail-fast 渲染、`*.local` 纵深 | 实现 `internal/tmpl` 时 |
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
**prune 仅在创建阶段完全收敛后执行**;自动破坏性动作以**所有权谓词**(symlink 按 state 存证的
`link_dest` 词法比较)为前置,强制替换走独立公式(`--force` ∧ 备份成功 ∧ Precond);**全部
target mutation 执行前复核 Precond**;崩溃后由收养规则(symlink 自动、普通文件显式 `--adopt`)
自愈。新机器由带校验的 `bootstrap.sh` 完成二进制安装、仓库克隆并移交 `dot init`。

## v1.5 修订摘要(相对 v1.4)

1. **add 预检 = 完整解析链模拟**(ADR-30):任何写入前,把新 source 虚拟加入模块文件树跑一遍 `resolve + enumerate + ignore`,要求**恰好产生一个**符合预期的 desired entry(target 与输入一致、kind 匹配模式);否则拒绝并指出命中的忽略规则——堵住「add 进 README.md / hooks/ 下文件 / 命中 ignore 的文件,下轮 apply 把刚建的链接当孤儿删掉」的路径。
2. **update 脏仓库前置检查**:持锁后、pull 前要求 `git status --porcelain` 完全为空(含未跟踪文件);非空即报错——`--ff-only` 遇到不冲突的本地修改仍会成功,随后 apply 会读到新旧混合的仓库。
3. **add 持久化顺序显式化**:仓库副本 `copy(O_EXCL) → chmod → Sync → close → hash 校验 → 复核原文件 → rename 换链`;失败自动清理本次副本与临时链,原文件保留,整体无副作用可重试(两处测试场景的矛盾随之统一)。目录 fsync 仍为已接受残余风险。
4. **核心结构强类型落地**:`DesiredKind` / `EntryKind` / `StateOp` 为独立枚举类型(JSON 序列化为字符串),草图同步;planner 出口断言 + executor 防御式复检 **StateOp×Kind 合法组合**(Delete⇔Prune、Upsert⇒NextEntry≠nil、Skip/Conflict/RunHook⇒Keep)。
5. **`add -m` 不再自动创建模块目录**(砍):新目录必然 ∉ profile,自动创建与 profile 校验自相矛盾;改为报错并打印两步指引(mkdir + 编辑 profiles)。
6. **hook 执行细节定稿**:有可执行位直接 exec、否则 `sh <script>`;工作目录 = 模块目录;继承父环境、再以 paths 注入的 `HOME`/`XDG_*`/`DOT_*` **覆盖**;指纹**不含** profile/data 运行上下文(ADR-31,否则切 profile 即全量重跑;hook 需要上下文时读 `DOT_*` 并自我幂等)。
7. **state 语义校验**:加载时除 JSON 解析外校验 kind 合法、target 位于 HOME、各 kind 证据字段完整(symlink 必有 link_dest、rendered 必有 hash);违规视同损坏,fail-closed(ADR-25 加强)。
8. **`[target]` 表缺当前 GOOS**(且 os 过滤通过)→ 硬错误;**init 重写机器配置**:内存合并校验 → 0600 → Sync → 原子 rename,失败保留旧配置;**state rebuild / edit [M2] 纳入锁范围**。
9. **`*.local` 表述降级为诚实纵深**(ADR-32):`git add -f` 属直接绕过,列为已接受风险;同时 `doctor --manifest-only`/CI 把「已跟踪的 `*.local`」升级为**错误**。
10. **一致性勘误**:ADR-14 措辞改「自动破坏性动作」;09 号 v1.3 残留注记与 NextEntry 旧表述更新;08 号备份权限断言改为「保留原 mode」;add synopsis 补 `--dry-run`;ADR 计数 29→32。

## 历史修订(存档,一行摘要)

- **v1.4**:迁移统一分派(entry=nil 规则)、元数据刷新通则、add 仓库侧预检雏形、StateOp 三态、ObservedKind+Special、备份保留 mode、kind 双词汇表强类型约定。
- **v1.3**:kind 迁移三原则(ADR-27)、Action 补全(LinkDest/NextEntry)、add 原子换链、BackupReplace 分型(ADR-29)、锁单次获取、M1 对 managed 输入硬错误、`--activate` 砍(ADR-28)。
- **v1.2**:收敛式 prune(ADR-20)、收养不对称(ADR-21)、owned 词法化 + link_dest 存证(ADR-22)、Precond 全量复核(ADR-23)、渲染 fail-fast(ADR-24)、state 三态 fail-closed(ADR-25)、mode 经 Render 修正(ADR-26)、威胁模型入册。
- **v1.1**:prune 双作用域、owned 谓词、创建先于 prune、收养与 flock、target 不变量、hooks/ 保留目录、两阶段加载、砍 env 函数、`*.local` 加固、bootstrap 校验。
- **v1.0**:初版:混合模型、modules/ 布局、两级 manifest、requires 铰链、bootstrap 三步。

## 文档约定

- 规范用词遵循 RFC 2119:**必须(MUST)**、**不得(MUST NOT)**、**应当(SHOULD)**、**可以(MAY)**。
- 标注 `[M2]` / `[M3]` 的内容属于后续里程碑,M1 实现时只需保证格式不与之冲突。
- 所有示例路径以 macOS 为准,Linux 差异处会显式标注。
