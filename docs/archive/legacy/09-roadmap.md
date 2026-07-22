# 09 · 路线图与范围控制

> [!WARNING]
> 历史存档，非当前规范。当前设计见 [`../../design-baseline.md`](../../design-baseline.md)。

本文只划分交付范围,不是内部包、类型或函数的实现计划。里程碑顺序可以随代码反馈调整;
01–07 号文档规定的安全性质和公开行为不能因排期调整而降级。

## 1. 里程碑

### M1 · 可用且安全的 stow 替代

目标是能够作为主力机器唯一的 dotfiles 工具使用:

| 范围 | 交付结果 |
|---|---|
| 命令 | `init` `apply` `diff` `status` `add` `version` `self-update`、`doctor --manifest-only` |
| 文件模型 | link + scaffold;managed 相关输入在 M1 明确报不支持,不得按 link 静默处理 |
| 安全内核 | ownership 存证、文件系统 target 身份、祖先拓扑与 hard-link 副作用隔离、完整 profile 全局校验、conflict/force、可用备份、target/source 提交时 Precond、创建先于 prune、收敛门控、控制面家族隔离 |
| 状态 | state v1 格式、重复成员与严格语义校验、原子提交、link/scaffold 崩溃收养及二者 kind 迁移、scaffold 显式重建、单实例锁;遇到预留 rendered 条目整体 fail closed |
| add | link/scaffold 建账一致性、与 apply 同源的预检、Git 可跟踪性、仓库不覆盖、各模式提交边界、等价遗留 source 续跑与失败恢复 |
| 数据 | `[data]`、严格机器配置与 init 变量收集/提交前提;渲染期不读取环境 |
| hooks | 字符串形态 run_once、保留目录、规定的 cwd/环境、at-least-once |
| 基建 | `--home` 全隔离、受校验的 bootstrap、首个 Release 和双平台 CI |

收口标准:主力 Mac 完全切换到 dot,stow 卸载;08 号文档的 M1 安全回归全部通过。

### M2 · 模板与同步闭环

| 范围 | 交付结果 |
|---|---|
| managed | `.tmpl`、drift(hash/mode)、`diff -v`、显式 adopt、`add --template` 建账一致性、补齐 managed/rendered kind 迁移 |
| 数据 | `from_env` 仅在 init 快照 |
| 同步 | update 洁净检查/pull/requires/apply、原地更新风险与 commit 恢复信息、带锁且透传退出码的 git |
| hooks | watch 依赖文件 |
| 辅助 | 完整 doctor、无隐式 force 的 edit、允许安全部分建账的受控 state rebuild |

收口标准:模板解决机器差异,第二台机器或干净虚拟机全流程跑通。

### M3 · Linux 与按需打磨

真实 Linux 落地后再按需求评估 `--json`、age 加密、release 签名和 drift 内容快照。
未进入规范的候选能力不应提前改变 M1/M2 的持久化格式。

## 2. 明确不做或暂不做

| 功能 | 处置 | 理由 |
|---|---|---|
| 深合并、模块继承、manifest 条件语言 | 不做 | 可预测性优先,复杂逻辑交给 hook |
| 模板 `env` 函数 | 不做 | 环境值经 init 快照,保持渲染可复现 |
| CLI 自动修改 manifest | 不做 | 保格式编辑和重序列化都不值得引入 |
| add 自动建模块、递归收编目录 | 不做 | 生命周期和失败回滚过于复杂 |
| 目录/特殊文件 backup-replace | 不做 | 手工移走换取简单可证的覆盖边界 |
| owner/ACL/xattr/flags/时间戳的完整搬移或备份 | 不做 | 当前只承诺字节、普通权限位与链接文本;依赖丰富元数据的文件手工处理 |
| 自动清理历史备份 | 当前不做 | 单人规模下手工清理成本低,避免清理策略本身误删唯一恢复副本 |
| run_once 指纹包含 profile/data | 不做 | 避免无关上下文变化触发全量重跑 |
| 并发执行、守护进程、Windows | 不做 | 不符合单人工具定位 |
| manifest 直接声明 HOME 外 target | 当前不做 | 会改变路径边界与 state 契约,需要未来单独设计而非一个放宽 flag;既有祖先 symlink 指向外部目录属于用户文件系统拓扑,不等于开放此语法 |
| 完整 semver、目录级链接、配置分仓 | 不纳入当前范围 | 当前收益不足以抵消复杂度 |

## 3. 实现门槛与反馈规则

建议按可验证结果推进,不绑定包名:

1. 先完成路径、manifest、机器配置和 state 的加载/校验,让非法输入在依赖它的计划或
   mutation 阶段前失败,同时保留 init 配置提交与 update pull 的明确例外。
2. 实现纯计划与 dry-run,用决策表测试固定 ownership、迁移和 prune 语义。
3. 实现 link/scaffold 的安全提交、状态落盘与崩溃恢复,再开放真实 apply。
4. 加入 prune、force/backup 和 add,逐个通过提交点前后的失败测试。
5. 最后接入 hooks、init、bootstrap 与发布流程,完成端到端切换。

实现发现问题时按以下门槛处理:

- 改变数据所有权、不可逆操作边界、公开命令行为、持久化格式或接受风险 → 先更新规范文档。
- 只影响内部类型、函数拆分、算法、日志或测试组织 → 直接在代码和测试中解决。
- M2/M3 的未决实现不得反向扩大 M1 文档;到对应里程碑前再依据既有性质补公共契约。

路线图完成度由可运行代码和测试判定,不以增加设计文档版本号代替实现证据。
