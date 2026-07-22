# chore/design-reassessment：用精简 MVP 替换旧实现

> [!WARNING]
> 历史工程记录，非当前规范或工作流程。

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，`dot` 只实现 `docs/design-baseline.md` 定义的个人 dotfiles MVP：读取 profile、
module 与平台变体，使用 symlink 和一次性 local example 收敛配置，并通过最小 state 支持
安全 update/prune。用户可用 `init`、`status`、`apply` 和 `remove` 完成日常操作；旧实现中的
template、hook、backup、force、add、doctor、通用身份系统和复杂结果协议从代码、测试与公开
文档中删除。

这不是在旧架构上逐项关闭功能。新核心先以小而完整的实现落地并通过合成文件系统测试，公开
CLI 一次切换，最后删除没有运行时调用者的旧 packages。每个阶段保持可构建、可测试，不读取
或修改真实 `modules/`、machine config、state、HOME 或 backup。

## Scope / Non-goals

范围内：

- 保存重设计基线并把旧规范归档为非规范性历史。
- 实现 v1 repository/machine config、module portable/variants、platform match 和 placements。
- 实现 state v2、read-only observe/plan、status 与严格零写入 dry-run。
- 实现一把 mutation lock，以及 `init`、`apply`、`remove` 的 symlink/local 收敛和可重跑恢复。
- 删除新基线排除的旧代码、测试、命令、依赖关系和构建门禁，并把 README 收敛到真实能力。

明确不做：

- 旧 state、manifest 或机器配置的自动迁移与兼容层。
- 软件安装、template、hook、backup、force、rollback、add、doctor、Git 自动化或 Windows。
- 为迁移期建立 adapter、双写、feature flag、通用 VFS、DI、transaction 或 workflow framework。
- 操作真实私人配置或发布、merge、push、release。

## Contract and Context

- `docs/design-baseline.md` 全文：唯一当前产品与行为契约，尤其是 §2 非目标、§7 路径边界、
  §8 state/ownership、§9 决策表、§10 CLI、§11 mutation/恢复。
- `README.md`：重写完成前只用于区分当前旧实现和目标设计；切换后必须改成真实能力说明。
- `AGENTS.md`：私人数据、mutation 测试隔离、依赖与完整门禁要求。

当前 `internal/` 约 4.5 万行，旧实现把 template、hook、backup、force、add、doctor、路径身份、
action precondition 与细粒度结果协议分散到十余个 package。新设计不保留这些 API。可直接保留
的只有 `cmd/dot/main.go`、`internal/buildinfo` 以及 Cobra、`go-toml/v2`、`gofrs/flock` 三个
现有依赖；其他代码只有在语义与新基线完全一致且比重写更简单时才复用，不以“已经写过”为
保留理由。

新核心放在单个 `internal/dot` package 内，按 config、state、resolve、plan、execute 等职责分
文件，不预建子 package 或 interface。`internal/cli` 只负责 Cobra、I/O 与退出码映射。公开 CLI
切换完成后删除旧核心 packages，避免长期存在两套真相源。

## Progress

- [x] 2026-07-23：归档旧规范、建立历史索引并保存唯一新基线；`git diff --check` 通过，归档
  正文逐份与 Git 原文比对一致。
- [x] 2026-07-23：完成旧实现结构盘点；确认当前分支 `chore/design-reassessment` 从 `main`
  的 `8d3cfe1` 开始，任务开始时只有本 Goal 的文档改动。
- [ ] 等待用户授权本 Goal 的 stage 与语义 commits；授权前不进入代码 milestone。
- [ ] Milestone 2：建立最小读取模型与 resolver。
- [ ] Milestone 3：实现 observe、plan、status 和 dry-run。
- [ ] Milestone 4：切换公开 CLI 并实现 mutation 收敛。
- [ ] Milestone 5：删除旧实现并完成全仓收敛、门禁和独立复核。

## Milestones

### Milestone 1：冻结新设计 checkpoint

把旧规范原样移入 `docs/archive/legacy/`，明确其非规范性；以
`docs/design-baseline.md` 取代旧多文档规范，并同步 `docs/README.md`、根 README 和
`AGENTS.md` 的真相源边界。本阶段不改 Go 行为。

Concrete steps：

    在 repo root 运行：git diff --check
    预期：无输出并返回 0。

    逐份比较：Git 中旧 docs 正文与 archive 中去掉存档声明后的正文
    预期：全部相同。

验收：

- 非 archive 文档只把 `docs/design-baseline.md` 当作当前规范。
- archive 清楚声明不得指导新实现。
- 新基线明确列出保留能力、非目标和接受风险，没有延续旧安全框架。

Commit 边界：

    docs(design): 归档旧规范并保存精简基线

### Milestone 2：建立最小读取模型与 resolver

新增 `internal/dot` 的严格 TOML/JSON 读取与纯 resolver：repository、machine config、state v2、
platform、portable/variants、profile union、extra modules、placement 和 HOME target 展开。只实现
新 schema，不提供旧格式 adapter。路径比较只覆盖词法规范化、现存 ancestor symlink 解析、
placement 冲突和控制路径前缀边界。

Concrete steps：

    在 repo root 运行：go test ./internal/dot -run 'Config|State|Resolve|Path'
    预期：portable/variant、profile/extra、严格解码、v1/too-new state 拒绝和路径冲突测试通过。

验收：

- 同一输入产生确定顺序的 effective modules/placements。
- profile module 不适用时 skip，extra/显式 module 不适用时返回错误。
- 不接受旧 `requires`、隐式文件枚举、template/hook/backup 字段。
- 全部测试只使用 `t.TempDir` 下的绝对合成路径。

Commit 边界：

    feat(core): 实现最小配置与平台解析

### Milestone 3：实现 observe、plan、status 和 dry-run

在 `internal/dot` 中实现 `lstat` observation、state ownership 决策、target move、stale link prune
计划和稳定的人类可读 action 列表。Planner 是普通数据与函数，不引入 action interface、通用
precondition 或状态机。通过内部入口和 CLI 投影测试证明 status 只读、dry-run 零写入；此阶段
仍不切换生产 root command。

Concrete steps：

    在 repo root 运行：go test ./internal/dot ./internal/cli -run 'Observe|Plan|Status|DryRun'
    预期：决策表、冲突、先 create/update 后 prune、status 退出码和零写入测试通过。

验收：

- 正确未知 symlink 可 adopt，错误未知 symlink 和普通对象 conflict。
- owned link 只有 actual 仍匹配 state 时才能 update/prune。
- local 只区分 absent/existing，既有对象不读取不分类。
- status/dry-run 不创建 parent、temporary、lock、config 或 state。

Commit 边界：

    feat(plan): 实现只读状态与收敛计划

### Milestone 4：切换公开 CLI 并实现 mutation 收敛

重写 `internal/cli`，只注册新基线命令和退出码；把 `init`、`apply`、`remove` 接到新核心。
Mutation 使用 `gofrs/flock` 的单一 non-blocking file lock，按 selection、create local/link、update、
prune、复核、state-last 的顺序执行。只在新建对象和 update/prune 提交点做基线要求的即时
no-clobber/重读，不恢复旧通用 action 证据框架。

Concrete steps：

    在 repo root 运行：go test ./internal/dot ./internal/cli
    预期：新 CLI、mutation 顺序、锁、部分失败和重跑恢复测试通过。

验收：

- 新 root help 只显示 init/status/apply/remove/version/help。
- 合成 HOME 中全量与 scoped apply、extra 激活、profile remove 拒绝、local 保留行为成立。
- 每个成功 mutation fixture 再执行一次相同 apply，断言无文件系统 mutation。
- 在 selection、create、update、prune、state commit 边界中断后可重跑收敛。

Commit 边界：

    feat(cli): 切换到精简收敛流程

### Milestone 5：删除旧实现并完成仓库收敛

删除已经没有运行时调用者的旧 packages 与对应测试，更新根 `dot.toml`、README、Makefile 和
CI/开发说明。依赖保持现有三项；若实现证明标准库足够，可以减少但不增加依赖。删除只针对
受版本管理的旧源码和规范已排除的能力，不触碰 `modules/` 或本机数据。

Concrete steps：

    在 repo root 运行：make check
    预期：tidy、format、lint、race tests 和 build 在当前 macOS 环境全部通过。

    在 repo root 运行：rg -n 'template|hook|backup|force|doctor|add|requires' cmd internal README.md Makefile
    预期：除明确解释非目标或必要技术词外，不存在旧能力入口或兼容路径。

验收：

- `go list ./...` 只包含新实现实际需要的 packages。
- README 只描述已经实现的精简 CLI，archive 仍保持非规范性。
- 完整任务 diff、untracked、格式、依赖和测试门禁通过。
- 未参与实现的只读 subagent 复核完整实质 diff；主线程处理有效意见后再复核必要范围。

Commit 边界：

    refactor(core): 删除旧实现并收敛仓库

## Validation and Acceptance

最终验收以 `docs/design-baseline.md` §13 的场景为准，全部使用绝对路径合成 fixture。当前平台
运行 `make check`；Linux 只记录 Go 交叉编译或现有 CI 的真实结果，不能把源码阅读称为 Linux
运行验证。

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| 旧规范仅为历史 | archive 内容比较、引用扫描、diff check | 已通过 |
| 配置与平台解析符合 v1 | `internal/dot` 单元/集成测试 | 待实现 |
| status/dry-run 只读 | 合成树快照前后比较 | 待实现 |
| apply/remove 只操作 absent 或 state-owned link | 冲突与漂移集成测试 | 待实现 |
| 部分失败可重跑收敛 | 分阶段故障注入集成测试 | 待实现 |
| 旧重型能力从产品与代码删除 | package/关键词/diff 检查 | 待实现 |
| 当前平台完整门禁 | `make check` | 待运行 |
| 独立复核 | read-only subagent 报告 | 待执行 |

## Safety, Authorization, and Recovery

用户已明确授权归档旧文档、保存新基线以及继续实现收敛；这覆盖任务内文件修改和受版本管理
旧源码的最终删除。当前未获得 Git stage/commit 或计划终态迁移授权。ExecPlan 要求每个
milestone 先形成 checkpoint，故在获得覆盖本 Goal 语义 commits 的授权前停在 Milestone 1，
不以长期未提交代码绕过流程。Branch 已经是 `chore/design-reassessment`，不需创建或切换。

所有 mutation 测试同时显式设置绝对 synthetic HOME、repo、config、state 和 lock，并清除或
重定向 `DOT_CONFIG`、`DOT_REPO`。任务不运行 CLI 指向真实 HOME，不读取 `modules/`、真实 local、
machine config、state 或 backup。测试中断只留下 `t.TempDir` 内容；实现失败保留当前 milestone
diff，前一个 checkpoint 不改写、不 amend。

## Interfaces and Dependencies

新实现保留 Cobra、`go-toml/v2` 和 `gofrs/flock`，不计划增加依赖。`internal/dot` 对 CLI 暴露
少量按用例组织的入口和普通结果值；不为测试建立 production interface。State v2 与 machine/
repository config v1 是公开持久格式，准确字段以 `docs/design-baseline.md` 为准。

## Surprises & Discoveries

- Observation: 旧实现和测试约 4.5 万行，复杂度主要集中在 planner/apply、路径身份、add、hook、
  backup 和通用 precondition/recovery。
  Evidence: `wc -l cmd/dot/main.go internal/*/*.go`；最大测试文件超过两千行。
  Impact: 原地删功能会保留旧 package/API 形状；采用小型新核心并一次切换，随后物理删除旧代码。

- Observation: 当前三项直接依赖与新基线选择完全一致。
  Evidence: `go.mod` 仅直接要求 Cobra、`go-toml/v2` 和 `gofrs/flock`。
  Impact: 重写无需先做依赖迁移，也没有引入配置框架的理由。

## Decision Log

- Decision: 历史规范只归档，不建立兼容优先级或继续修订。
  Rationale: 新设计必须只有一个真相源，避免旧安全模型重新影响实现。
  Date: 2026-07-23

- Decision: 新核心采用单个 `internal/dot` package 的多文件组织，公开 CLI 保留独立 package。
  Rationale: 核心逻辑内聚且规模尚未证明需要更多边界；这比复用十余个旧 package 更能降低
  依赖方向和维护成本。
  Date: 2026-07-23

- Decision: 不提供旧 state/config/manifest 自动迁移。
  Rationale: 基线已经明确 cutover 是人工归档旧 state；兼容层成本只服务一次个人迁移。
  Date: 2026-07-23

## Outcomes and Handoff

当前已完成文档归档和新基线保存，尚未开始 Go 实现。Base 是 `main` 的 `8d3cfe1`，工作分支为
`chore/design-reassessment`。下一步需要用户授权本 Goal 的 stage/semantic commits，随后先提交
Milestone 1，再进入 `internal/dot`。在代码、门禁与独立复核完成前，本计划保持 `active/`，不
迁移到 `completed/`，也不声称 review-ready。
