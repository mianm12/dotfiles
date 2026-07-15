# 01 · 概览:目标、威胁模型与关键决策

## 1. 背景与动机

现有工具在个人场景下各有别扭之处:GNU Stow 无法模板化、目录折叠会把运行时垃圾卷进仓库;
chezmoi 全 generate 模型丢失了「改了就是改了」的直觉,且概念负担偏重。`dot` 的定位是
**为一个人、两类系统(macOS → Linux)量身定制的最小完备工具**,同时作为一个可长期维护的
Go 练手项目。

## 2. 目标(Goals)

1. 一条命令从裸机到可用环境:`curl … | sh` → `dot init` → 完成。
2. 混合模型:默认 symlink(双向、直觉),模板文件才 generate(单向、可参数化)。
3. 支持 macOS 与 Linux 的路径/内容差异,不靠复制粘贴两份配置。
4. 私有内容(密钥、公司配置)通过 `*.local` 约定留在本机,并以多道机制保证永不入库。
5. `apply` 严格幂等、可 dry-run;一切破坏性动作以所有权谓词为前置、一切 target mutation
   以 Precond 复核为前置;**清理仅在创建收敛后进行**——部分失败的最坏结果是「新旧并存」,
   永远不是「新旧皆无」。
6. CLI 与配置同仓库,但版本解耦:配置更新走 `git pull`,二进制走 Releases,
   以 `requires` + 严格解码双重铰链保证兼容安全。

## 3. 非目标(Non-Goals)

明确不做,避免范围蔓延:

- **多用户/团队特性**:权限体系、共享 profile 市场、远端配置中心。
- **密钥托管**:不集成系统钥匙串、1Password。私密内容走 `*.local`;若未来需要同步
  加密文件,直接集成 age 加解密单个文件即可(M3 之后再议)。
- **并发执行**:不支持并发,单实例 flock 锁防止事故(ADR-19),仅此而已。
- **包管理器抽象层**:不封装 brew/apt 的统一接口,软件安装交给模块内 hooks 脚本
  (如 `brew bundle` 消费模块自带的 Brewfile)。
- **配置内容的语义理解**:不解析 zshrc/vimrc 内容,只管文件级别的分发。
- **Windows 支持**。
- **守护进程/文件监听**:所有动作由用户显式触发。

## 4. 威胁模型与剩余风险

安全设计的裁决标尺,全部文档的取舍以此为准:

> **保护对象是「用户数据不被工具自身的 bug 与日常事故损毁」,不是「对抗恶意环境」。**

**防御的**(必须做):误删非我们创建的内容、误覆盖用户手改的产物、部分失败导致配置丢失、
崩溃后状态错乱、私有内容误入公开仓库、配置手误(路径逃逸、target 撞车)。

**不防御的**(明文出界):恶意仓库内容(仓库是用户自己的,hook 本来就是任意代码)、
恶意构造的 manifest、对抗性的并发第三方进程、供应链攻击(checksum 校验防传输损坏,
签名验证列 M3 可选)。

**已接受的剩余风险**(廉价手段已用尽,消灭需要不成比例的复杂度):

| 风险 | 缓解现状 | 接受理由 |
|---|---|---|
| rename 覆盖路径上 Precond 复核后的微秒竞态窗口 | 复核已把窗口从秒级缩到微秒级;CreateLink 路径借 EEXIST 完全消除 | 彻底消除需 renameat2 级手段,超出定位(ADR-23) |
| 断电级持久化(不做父目录 fsync) | state 写入本身原子;收养规则重跑自愈 | state 是可重建的缓存性质数据 |
| hook 副作用成功、指纹落盘前崩溃 → 重跑 | 语义定为 at-least-once,要求脚本自我幂等 | brew bundle / defaults write 天然幂等,要求不苛刻 |

## 5. 术语表

| 术语 | 定义 |
|---|---|
| **模块(module)** | `modules/` 下的一个子目录,是分发的最小单元,内部结构镜像 target 路径。贯穿代码、manifest、CLI 的统一用语 |
| **profile** | 顶层 manifest 中定义的模块分组,支持 `@` 嵌套引用 |
| **target** | 模块内容要落地的根目录,默认 `~` |
| **保留目录 `hooks/`** | 模块顶层的 `hooks/` 目录,存放 hook 脚本及其数据文件,内置忽略,不参与链接 |
| **managed 模板** | `.tmpl` 后缀,每次 apply 重新渲染,产物归工具管,手改视为 drift |
| **scaffold 模板** | `.template` 后缀,目标不存在时生成一次,之后永不覆盖,产物归用户 |
| **`*.local` 约定** | 共享配置固定 source 同名 `.local` 文件;`.local` 本身不入库 |
| **state 清单** | `~/.local/state/dot/state.json`,记录所有由 dot 创建的文件 |
| **`link_dest` 存证** | symlink 条目在 state 中记录的、创建链接时写入的确切字符串;所有权判定的唯一依据(ADR-22) |
| **机器配置** | `~/.config/dot/config.toml`,不入库,记录本机 profile 与模板变量 |
| **desired state** | 由 manifest + 模块文件树 + 机器配置推导出的「apply 后应有的文件系统状态」 |
| **所有权 `owned()`** | 判定「磁盘上的对象是否仍是我们的产物」的谓词(05 §3.1);state 有记录 ≠ owned |
| **收敛(converged)** | 本次创建阶段的全部 desired 动作成功完成:无 conflict、无 error、无 Precond 失配降级、无用户拒绝确认;prune 的执行前提(ADR-20) |
| **deferred prune** | 因未收敛而延迟的清理动作:在计划中标记展示、本次不执行 |
| **drift** | 实际文件系统状态偏离产物应有状态(managed 产物被手改、链接被改指等) |
| **孤儿(orphan)** | state 清单中存在、但已不在本次 desired state 中的条目 |
| **收养(adopt)** | 磁盘对象与 desired 一致但 state 无记录时补录 state;symlink 自动,普通文件需显式 `--adopt`(ADR-21) |
| **mutation 动作** | 改变文件系统的动作:create-link / render / scaffold / backup-replace / prune(执行) |

## 6. 关键决策记录(ADR)

| # | 决策 | 备选项 | 理由 |
|---|---|---|---|
| ADR-1 | 实现语言 **Go** | Rust、脚本语言 | 交叉编译单二进制,裸机可跑;`text/template` 标准库白送模板功能 |
| ADR-2 | **混合模型**:symlink 默认 + 模板 generate | 全 symlink / 全 generate | 90% 文件不需模板,保留双向直觉;仅在必要处付出单向复杂度 |
| ADR-3 | **文件级链接**,不做目录折叠 | stow 式目录级链接 | 防止程序运行时垃圾进入仓库;链接数量多可接受 |
| ADR-4 | CLI 与配置**同仓库** | 分仓 | 单人场景版本天然对齐,bootstrap 只克隆一个东西;解耦靠 Releases + requires |
| ADR-5 | 收集目录命名 **`modules/`** | `packages/`、`config/` | 与 Go package 概念区隔;"module" 成为全局统一术语 |
| ADR-6 | manifest 用 **TOML** | YAML、JSON | 注释友好、无缩进陷阱、解析成熟(BurntSushi/toml) |
| ADR-7 | 合并语义**整键覆盖**,不深合并 | 深合并、数组拼接 | 可预测性 > 灵活性;半年后仍能一眼看懂生效配置 |
| ADR-8 | 双模板后缀:`.tmpl` / `.template` | 单后缀 + manifest 标记 | 文件名自说明;短后缀 = 高频渲染,长后缀 = 一次性蓝本 |
| ADR-9 | 模块发现**靠目录存在**,profiles 只分组不枚举 | manifest 显式注册 | 避免「加模块改两处」双重记账 |
| ADR-10 | state 用**单个 JSON 文件** | bolt/sqlite | 个人配置规模几百条;JSON 可 diff、可手工急救 |
| ADR-11 | `requires` 仅支持 `>=x.y.z` | 完整 semver 约束 | 解析器 20 行写完;个人项目不需要范围锁定 |
| ADR-12 | scaffold 产物**永不自动删除** | 随模块删除清理 | scaffold 产物含用户手写内容,是用户数据不是工具产物 |
| ADR-13 | 执行顺序**创建先于 prune** | prune 先行 | 孤儿集与创建集 target 必不相交,反转无冲突;失败最坏结果为「新旧并存」 |
| ADR-14 | 破坏性动作以 **owned() 谓词**为前置 | 信任 state 记录 | state 只证明「曾经创建」;现势必须复核 |
| ADR-15 | **收养规则**兜住崩溃窗口 | 逐动作即时落盘、WAL | 「执行了未记账」经重跑自愈,复杂度远低于日志方案 |
| ADR-16 | manifest **两阶段加载**:requires 宽松预读 + mutation 严格解码 | 未知键仅警告 | 带着不理解的字段去 prune 风险不可接受;严格解码是 requires 忘提升时的失效安全 |
| ADR-17 | 模板**不提供 `env` 函数**,环境值经 `[data].from_env` 于 init 快照 | 渲染时读环境 | 渲染只依赖显式输入,否则 plan 不可复现、drift 误报 |
| ADR-18 | **新建/更新的 state 条目必须属于本次 effective profile**;历史条目允许作为孤儿存在,由 prune 生命周期收敛 | 允许游离模块 / 全量条目强不变量 | 否则部分 apply 建立的条目会被全量 apply 误清;而 profile 切换后的过渡期决定了强不变量不可能持久成立(v1.2 修正措辞) |
| ADR-19 | **单实例 flock 锁** | 无锁 / 完整并发支持 | 不支持并发,但 10 行代码防住 update 与手动 apply 撞车 |
| ADR-20 | **prune 仅在创建阶段完全收敛后执行**;任一 conflict/error/Precond 失配/拒绝确认 → 本次全部 prune 延迟(deferred) | error 跳过、conflict 不跳(v1.1) | v1.1 规则在改名场景仍会「新旧皆无」;单一收敛条件比双轨规则更少分支;代价仅是家务性清理推迟 |
| ADR-21 | **收养不对称**:symlink 自动、普通文件显式 `--adopt` | 全自动 / 全显式 / observed 第三类条目 | 链接指向仓库精确源是无歧义证据且删链零损失;文件内容巧合不构成所有权,自动收养会让用户文件变成可自动删除的产物;第三类 state 语义复杂度不配收益 |
| ADR-22 | symlink 所有权 = `Readlink` 与 state 存证 `link_dest` 的**词法比较** | EvalSymlinks 解析比较(v1.1)/ 尾部启发式 | 解析式判定在死链(prune 主场景)上恒失败;词法比较不要求叶子存在;删除尾部启发式,仓库搬家由「记录精确匹配 ∧ 期望已变」的 state 证据驱动 |
| ADR-23 | **全部 target mutation 执行前复核完整 Precond**;失配降级 conflict | 仅破坏性动作复核(v1.1) | flock 管不住第三方进程;scan 时 Missing 的 target 可能在执行前被创建;CreateLink 借 EEXIST 免费原子复核;rename 微秒窗口列为已接受剩余风险 |
| ADR-24 | 模板渲染 **fail-fast**:plan 全量渲染,任一失败整体退出 | 单文件降级、其余继续(v1.1) | 与「executor 只消费 Action」自洽(无需 Error 动作);渲染失败即配置 bug,不该半套上线 |
| ADR-25 | state **fail-closed**:损坏/版本过新拒绝 mutation;缺失 = 合法全新 | 一律降级为无历史模式(v1.1) | 无历史模式跑完会覆盖损坏文件、毁尸灭迹;「版本过新」在有 self-update 的系统里真实存在(回滚二进制后) |
| ADR-26 | mode 漂移经**复用 Render**(reason="mode")修正 | Entry.mode + chmod 动作 | 重写同样字节顺带落权限,省一个 ActionKind 和一个 state 字段;symlink 权限不管 |

## 7. 与现有工具的能力对照

| 能力 | stow | chezmoi | dot |
|---|---|---|---|
| symlink 双向直觉 | ✅ | ❌ | ✅(默认) |
| 模板/机器差异 | ❌ | ✅ | ✅(按需) |
| 文件级链接 | ⚠️ 需 --no-folding | n/a | ✅ 唯一模式 |
| 一次性脚手架 | ❌ | ⚠️ create_ 前缀 | ✅ scaffold 一等公民 |
| 孤儿清理 | ⚠️ -D 手动 | ✅ | ✅ state 存证 + owned + 收敛门控 |
| CLI/配置版本铰链 | n/a | n/a | ✅ requires + 严格解码 |
