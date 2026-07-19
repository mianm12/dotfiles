# feat/apply-link：建立 link 安全执行内核

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和
`Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成后，内部调用方可以执行 planner 已决定的 M1 link 动作：L1 在 target 仍缺失且 source、
祖先拓扑与控制面边界仍满足计划前提时无覆盖地创建绝对 symlink；L2 只返回 state upsert 而不
触碰 target；L3 把仍属于 dot 的旧 symlink 原子替换为完整新 symlink。每个结果明确携带应由
后续 runtime 提交的成功或失败 `StateEffect`，但本分支不写 state、不取锁，也不开放真实
`dot apply`。

## Scope / Non-goals

范围内：

- 新建内部 file executor 的最小 link 切片，消费 `planner.FileAction` 与可信
  `paths.ControlPlanePaths`。
- 共享提交前复核 target identity、leaf observation、控制面隔离和 regular link source；为 L1
  安全创建缺失祖先并使用 no-clobber symlink 创建。
- L2 adopt 保持 state-only；L3 使用 target 同文件系统的完整临时 symlink 原子替换旧链，失败时
  保留旧 target。
- 用真实 `t.TempDir()` 文件系统固定前提失配、祖先阻断/改指、source 失效、并发新对象、完整
  旧/新对象和逐动作 state effect。

明确不做：

- 不实现 scaffold mutation、state builder/persistence、lock/runtime wiring、backup/force、prune、
  hooks、CLI 或任何公开真实 apply。
- 不重新解释 manifest，不改变 planner decision、ownership、state v1、路径身份或公开输出契约。
- 不引入通用 filesystem abstraction 或第三方依赖；故障测试只使用与提交边界相邻的窄操作接缝。

## Contract and Context

- `docs/02-architecture.md` §4–§6：executor 只消费自包含计划，按计划的成功/失败 state 处置返回，
  不重新读取 manifest 或改变 decision。
- `docs/05-apply-engine.md` §3.2：本切片执行 L1 create-link、L2 state-only adopt、L3 owned relink；
  L4–L6、force 与 backup 不在范围。
- `docs/05-apply-engine.md` §5–§7：提交前必须重新证明 target/source/祖先/control Precond；新对象
  不得覆盖并发出现的对象，替换必须只产生完整旧/新对象。
- `docs/05-apply-engine.md` §10、`docs/08-testing.md`：成功收敛后的同输入不再 mutation；link 创建
  未落账由 L2 自动补录。
- `docs/09-roadmap.md` §3：先建立 link/scaffold 安全提交与崩溃恢复，再由后续节点连接 runtime。

基线是 clean `main@e9e8bac6e5c1406e0db8aeb6e9eca6194aeeddb2` 创建的
`feat/apply-link`。当前 `internal/planner` 已形成 canonical `FileAction`：`Precondition` 保存
plan-time target resolution、leaf observation 和 link source requirement，成功/失败分支分别保存
`StateEffect`；`internal/paths` 已提供同一文件系统语义的 target/control boundary 校验。真实缺口
是仓库没有任何 executor 或 `FileAction` 的 mutation 消费者。

## Progress

- [x] 2026-07-20：确认 worktree、Git 顶层和 branch 均为分配的 `feat/apply-link`，HEAD 为
  `e9e8bac` 且 clean；读取规则、规范、completed plans 与 planner/paths 实现。
- [ ] 提交本 active ExecPlan 起点。
- [ ] 测试先行完成共享 Precond、缺失祖先、L1 no-clobber 与 L2 state-only adopt。
- [ ] 测试先行完成 L3 完整旧/新 symlink 原子替换与失败保留。
- [ ] 运行窄测、完整 diff check、`make check`，更新 handoff 并保持计划 active 等待独立复核。

## Milestones

### Milestone 1：固定 L1/L2 与共享提交前提

先增加真实文件系统测试，覆盖 L1 缺失祖先创建、精确绝对 symlink、并发出现 target 不覆盖、
source 非普通文件/消失、leaf 或祖先 identity 变化、控制面 alias，以及 L2 adopt target 零 mutation。
随后建立最小 executor：所有可提交动作先复核 plan-time target resolution 与 observation，重新执行
target/control boundary；L1 在创建祖先后再次复核，并在最终 `os.Symlink` 上依赖排他创建语义。
函数 error 表示动作未成功，结果使用 `OnFailure`；nil error 使用 `OnSuccess` 并报告 target 是否
发生 mutation。state-only adopt 不创建祖先。

Concrete steps：

    在 worktree root 运行：go test ./internal/executor -run 'TestExecuteLink_(Create|Adopt|Precondition)'
    预期：测试先因实现缺失失败，完成最小实现后全部通过。

验收：

- L1 只在 target 仍缺失且 source 为 regular file 时创建精确 symlink；任何新对象均不覆盖。
- L2 target bytes/link text/mode 和路径集合不变，只返回计划给出的 upsert effect。
- 任何 target/source/ancestor/control 前提失配都返回失败 effect，未提交 target 不变。

Commit 边界：

    feat(executor): 执行安全 link 创建与收养

### Milestone 2：实现 L3 原子重链

先增加 L3 测试证明准备或提交失败时旧链文本仍完整，成功时 target 只从完整旧链切换到完整新链，
临时产物不成为 target 真相。实现只接受 planner 的 owned-link-stale create-link 分支，在 target 父
目录内准备完整临时 symlink，准备后再次执行共享 Precond/source/control 复核，再用同文件系统
rename 提交。cleanup 仅处理本次明确创建且尚未发布的临时目录/链接；rename 成功后不尝试回滚。

Concrete steps：

    在 worktree root 运行：go test ./internal/executor -run 'TestExecuteLink_Relink'
    预期：成功、prepare failure、Precond failure 与 rename failure 场景全部通过。

验收：

- L3 成功返回 upsert effect 和 `TargetMutated=true`；旧链不存在半写窗口。
- rename 前任一失败保留旧链与 failure effect；不把 L4/L5/backup 当作 L3 执行。

Commit 边界：

    feat(executor): 原子重建 owned link

## Validation and Acceptance

最终从分配 worktree root 运行：

    go test ./internal/executor ./internal/planner ./internal/paths
    go test ./internal/executor -count=20
    git diff e9e8bac6e5c1406e0db8aeb6e9eca6194aeeddb2...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp4-link-check

成功判据是所有命令退出 0，完整 diff 只包含本计划与 link executor/test，worktree clean。当前原生
平台为 Darwin/arm64；远端 macOS/Linux CI 未运行时只报告“本地验收通过、远端待验收”。

## Safety, Authorization, and Recovery

测试只操作 `t.TempDir()` 内的合成 HOME/repo/config/state/binary/target，不读取或修改真实
`modules/`、machine config、state、backup、`.env` 或主力 HOME。失败保留最近成功 commit，以新
commit 修复，不 amend、rebase、cherry-pick、squash、reset、force，也不操作 main、coordinator、
其他 worktree 或 branch。若必须改变 planner/state 持久契约、ownership 或无法证明 no-clobber/
原子替换，立即停止并请求裁决。

## Interfaces and Dependencies

不新增依赖。新 executor 只依赖标准库、`internal/planner` 和 `internal/paths`。对外暴露的最小内部
结果包含 `StateEffect` 与 target mutation 标志；后续 scaffold 复用同一 Precond/提交骨架，runtime
负责顺序执行、聚合成功 effect 与持久化。executor 不导入 state/runtime/CLI，不形成依赖环。

## Surprises & Discoveries

暂无。

## Decision Log

- Decision: link executor 直接消费 `FileAction`，不复制 L 表或重新构造 state entry。
  Rationale: planner 已通过 canonical validation 固定 decision 与 effect；executor 只校验安全提交
  所需的封闭动作形态和现势 Precond。
  Date: 2026-07-20

- Decision: L3 使用 target 父目录内的完整临时 symlink加同文件系统 rename。
  Rationale: L1 的排他 `symlink` 负责 no-clobber；L3 必须替换仍 owned 的旧链，同目录 rename 在
  macOS/Linux 上提供完整旧/新目录项边界，且不需要新依赖。
  Date: 2026-07-20

## Outcomes and Handoff

尚未完成；计划保持 active。
