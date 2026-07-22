# feat/hooks-run-once：交付 M1 run_once 安全执行链

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和
`Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成后，`dot apply` 可以在既有 canonical `ApplyPlan` 内真实执行 M1 string-form `run_once`：
文件与 prune 阶段之后按规范顺序串行运行 hook，向子进程实时透传调用命令的 stdio，在执行前
复核脚本仍是计划时的普通文件、内容与 direct/sh 分类，并把 file、prune 与成功 hook 前缀通过
一个 `state.ChangeSet` 原子提交一次。用户可通过 full/partial apply、失败重试、Store failure、
第二次幂等和完全隔离的 subprocess 集成测试直接观察这些行为。

## Scope / Non-goals

范围内：

- 为 `planner.HookAction` 提供真实 execute-time observation 与 `os/exec` executor；仅执行已经
  通过 canonical plan 验证的 `HookRun`，`HookSkip` 永不启动子进程。
- 在 apply orchestration 中加入 file → prune → hook 顺序；hook 不受 file conflict、prune
  deferred、`--no-prune` 或整模块确认拒绝门控，失败停止尚未启动的 hook 后缀。
- 增加密封 `HookOutcome` 与 Result validator，使尝试、成功/失败/deferred、run_once effect 和
  最终 state commit 都与同一个 `ApplyPlan` 自洽。
- 用一个 `state.ChangeSet` 合并 entry update/delete 与成功 hook 的 `RunOnceUpdate`，一次调用
  `CommitState`；保持成功前缀、失败旧指纹和 Store failure 下的 at-least-once。
- 为 apply CLI 显式传递 stdin/stdout/stderr；hook 继承父环境，再唯一覆盖 HOME、XDG 与 DOT_*，
  cwd 为模块目录，外部输出实时透传且不进入 dot 的确定性摘要。
- 使用绝对 synthetic HOME/repo/config/state/backup/XDG 的真实子进程测试，并显式断言真实 HOME
  sentinel 不变。

明确不做：

- 不实现 init、config publisher、runtime `InitSession`、Cobra init、bootstrap、update 或 release。
- 不实现 M2 table-form/watch/from_env、并行 hook、跨模块依赖图、sandbox、exactly-once、TUI 或
  新持久格式。
- 不引入第三方依赖，不改变 hook 指纹算法、manifest schema、ownership、force、prune 或文件
  Precondition 契约。
- 不读取或修改真实 `modules/`、`*.local`、机器配置、state、backup、`.env` 或 HOME。

## Contract and Context

- `docs/02-architecture.md` §2/§4–§6：mutation 全周期持锁；阶段为 file、收敛时 prune、hooks；
  成功 file 与 hook effects 必须按结果矩阵原子提交。
- `docs/03-manifest-spec.md` §3：M1 只消费严格验证的 string-form run_once；script 是普通文件且
  state key 唯一，hook 引用保持内置 ignore。
- `docs/04-cli-spec.md` §2–§4.2：partial apply 只运行请求模块 hook；dry-run 零执行零写入；退出码
  按运行错误优先。
- `docs/05-apply-engine.md` §2/§4/§6/§8：hook 顺序、cwd/env、stdio、指纹、历史保留、
  at-least-once、失败停止后缀及 conflict 不门控是直接契约。
- `docs/08-testing.md` §2–§4：subprocess 的 HOME/XDG 与全部控制面必须在同一临时根，覆盖顺序、
  failure retry、历史恢复、部分 scope 与真实 HOME 零变化。
- `docs/09-roadmap.md` §1/§3：本 Goal 只交付 M1 string-form run_once，不提前引入 M2 watch。

基线是 clean `main@1df57addac93c48bc1497f1be15aa182a3730ce6`，工作分支为
`feat/hooks-run-once`。已有 `internal/planner/hook_planner.go` 形成自包含 `HookAction`，
`internal/state.Transition` 可一次组合 entry 与 run_once，`internal/apply` 已以密封 Result 执行
file/prune，但 `validateExecutionScope` 对任意 hook 硬拒绝，Result 没有 HookOutcome，且 state
在 hook 阶段之前提交。CLI `environment` 目前没有 stdin，production runner 也未把 stdio 传给
apply。前置计划 `.agent/plans/completed/m1-cp7-prerequisites.md` 已明确要求本 Goal 从 canonical
`plan.Hooks()` 消费动作、将 state commit 后移并保持一次提交。

## Progress

- [x] 2026-07-22：确认分配 worktree、Git 顶层、branch、base 和 clean 状态；读取仓库规则、
  ExecPlan 规范、CP7 目标、相关规范、实现、测试与 completed handoff。
- [x] 2026-07-22：以 `6be0ddb` 提交本 active ExecPlan 起点。
- [x] 2026-07-22：完成 Milestone 1 execute-time hook observation/executor；direct/sh、cwd/env、
  实时 stdio、失败与 regular/bytes/exec-class 失配均由真实隔离子进程测试覆盖。
- [x] 2026-07-22：完成 Milestone 2 apply hook phase、密封 `HookOutcome` 与单一 `ChangeSet`；
  file conflict、prune deferred/确认拒绝不门控 hook，失败后缀零尝试且成功前缀一次提交。
- [ ] Milestone 3：接入 apply CLI stdio、公开投影与端到端验收。
- [ ] 完成窄测、重复/race、跨平台编译、branch diff check、隔离 `make check`，更新 review handoff；
  保持计划 active，等待未参与实现者完整复核。

## Milestones

### Milestone 1：执行前复核并真实运行单个 hook

先在 `internal/executor` 增加失败测试，证明当前缺少 hook executor；再用真实临时脚本固定
HookSkip 零执行、普通文件/bytes/exec class 的 execute-time 复核、direct 与 `sh`、cwd、父环境
继承及 HOME/XDG/DOT_* 唯一覆盖、stdin/stdout/stderr 实时透传和非零退出。executor 只消费
自包含 `planner.HookAction`，不得重新解释 manifest 或 state；脚本在计划后变化、变为 symlink/
目录/特殊对象或执行分类变化时不得启动。

Concrete steps：

    在 repo root 运行：go test ./internal/executor ./internal/planner
    在 repo root 运行：go test -race ./internal/executor ./internal/planner
    预期：真实子进程仅写入 synthetic fixture；全部 observation、I/O 与 failure 分支通过。

验收：

- direct/sh、cwd/env 和 stdio 完全符合计划；覆盖环境变量不存在重复 key。
- 执行前事实失配零启动、零 state effect；非零退出保持可分类错误。
- 真实 HOME sentinel 前后不变。

Commit 边界：

    feat(executor): 执行并复核 run_once hook

### Milestone 2：把 hook phase 与 file/prune effects 一次提交

先扩展 apply fault-injection 测试，暴露当前 scope gate、无 HookOutcome 和提前 state commit 缺口；
随后从 canonical `plan.Hooks()` 建立结果槽，在 file/prune 后无条件进入 hook phase。HookSkip 形成
可信 skip 事实；HookRun 成功产生 `RunOnceUpdate`，失败停止未启动后缀并保留旧指纹。最终用一个
`state.ChangeSet` 组合全部成功 effects，并至多一次调用 `CommitState`。Result validator 必须拒绝
伪造的成功后缀、未尝试却带 effect、失败后继续执行、无 effect 却声称 commit 等矛盾事实。

Concrete steps：

    在 repo root 运行：go test ./internal/apply ./internal/state ./internal/executor
    在 repo root 运行：go test -race ./internal/apply ./internal/state ./internal/executor
    预期：file conflict/prune deferred/no-prune/confirmation rejection 下 hook 仍运行；失败前缀一次落账。

验收：

- full/partial scope 与模块/声明顺序沿用 canonical plan；未请求模块零执行零指纹更新。
- hook 失败停止后缀，但已成功 file/prune/hook effects 仍一次提交；失败项旧指纹保留。
- Store failure 不回滚外部效果且不落指纹，重跑符合 at-least-once；第二次同指纹为幂等 skip。

Commit 边界：

    feat(apply): 串行执行 hook 并原子提交成功效果

### Milestone 3：连接 CLI stdio 与可观察结果

先用真实 CLI fixture 固定 stdin/stdout/stderr 透传、hook failure exit 1、成功/skip 输出、partial
scope、profile 移除恢复和第二次运行，再把 production `os.Stdin` 与测试注入的 reader/writers
显式传入 apply。CLI 只投影已验证 Result，不重新推断 executor/state 协议；hook 自身输出允许与
dot context/action 行实时交错，但 dot 自己的摘要顺序保持稳定。

Concrete steps：

    在 repo root 运行：go test ./internal/cli ./internal/apply ./internal/executor
    在 repo root 运行：go test -race ./internal/cli ./internal/apply ./internal/executor
    预期：真实 Cobra apply 覆盖执行、失败、重跑、partial scope 与完全隔离环境。

验收：

- production 与测试都显式传递调用命令 stdio；hook 输出不被 buffer、截断或重排。
- HookSkip 不再触发 unsupported gate；HookRun 成功/失败分别映射规范输出和退出码。
- dry-run/diff/status 保持零执行，既有 file/force/prune/add 行为不回归。

Commit 边界：

    feat(cli): 接入 apply hook 实时输入输出

## Validation and Acceptance

最终从本 worktree root 运行：

    go test -count=20 ./internal/executor ./internal/apply ./internal/cli ./internal/state ./internal/planner
    go test -race ./internal/executor ./internal/apply ./internal/cli ./internal/state ./internal/planner
    GOOS=darwin GOARCH=amd64 go test -c -o /private/tmp/dot-m1-cp7-hooks-darwin.test ./internal/executor
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-m1-cp7-hooks-linux.test ./internal/executor
    git diff 1df57addac93c48bc1497f1be15aa182a3730ce6...HEAD --check
    GOCACHE=/private/tmp/dot-m1-cp7-hooks-gocache GOLANGCI_LINT_CACHE=/private/tmp/dot-m1-cp7-hooks-lint-cache make check BINARY=/private/tmp/dot-m1-cp7-hooks-check/dot

成功判据是窄测、重复/race、两个目标平台编译、完整 diff check 与本机 `make check` 全部退出 0；
worktree clean，完整 diff 仅含本 Goal 的 executor/apply/CLI/测试与 active ExecPlan。交叉编译不等于
目标平台运行；远端 macOS/Linux CI 与 Linux 实机未运行时必须标为待验收。

## Safety, Authorization, and Recovery

当前任务授权只在 `/private/tmp/dot-m1-cp7-hooks-019f8857` 的 `feat/hooks-run-once` 修改、stage、
commit 本 Goal 文件并运行门禁；不授权切换或修改 main/其他 branch/worktree，不授权 fetch、pull、
push、rebase、cherry-pick、squash、amend、reset 或 force。所有 mutation/subprocess 测试使用一个
`t.TempDir()` 下的绝对 synthetic HOME/repo/config/state/backup/XDG，并断言真实 HOME sentinel
不变；不读取真实私人数据。

每个 milestone 形成独立语义 commit，失败以新 fix commit 修正，不重写历史。若必须修改
`internal/config`、runtime `InitSession`、引入第二次 state Store、新依赖或无法证明脚本 Precondition/
成功前缀/单次提交，则更新本计划并停止报告主线程。

## Interfaces and Dependencies

本 Goal 只依赖标准库 `os/exec` 与现有 planner/state/runtime/apply 类型。executor 接受已验证的
`planner.HookAction` 和显式 stdio；apply 从一个 canonical `ApplyPlan` 派生 hook 顺序，以密封结果
保存实际处置，并使用既有 `state.Transition(Loaded, ChangeSet)` 与一次 runtime `CommitState`。
不新增 Go module 依赖。

## Surprises & Discoveries

- Observation: macOS 临时目录可通过 `/var` 与 `/private/var` 两个 lexical path 指向同一目录，
  hook 子进程报告的物理 cwd 可能使用后者。
  Evidence: direct 与 sh 集成测试的 `$PWD` 都解析为 `/private/var/...`；测试改为先
  `filepath.EvalSymlinks` 再比较同一物理模块目录，没有放宽 cwd 契约。

## Decision Log

- Decision: executor 在启动前重新观测普通文件、bytes 与 direct/sh 分类，并要求结果仍等于
  canonical action；不把计划后的仓库变化当成新动作执行。
  Rationale: hook 是 executor 读取仓库脚本的明确例外，但仍必须只执行已经计划的语义；失配时
  fail closed 可避免用旧指纹记录新脚本。
  Date: 2026-07-22

- Decision: 将 planner 的版本化指纹函数导出为 internal API，由 plan-time 与 execute-time 共用
  单一实现；executor 不复制指纹编码。
  Rationale: 指纹是执行前 Precondition 的比较边界，复制算法会形成两个真相源并可能静默漂移。
  Date: 2026-07-22

- Decision: file、prune 与成功 hook 前缀只形成一个 `ChangeSet`，最终至多一次 Store。
  Rationale: 这同时满足部分成功、失败保留旧指纹与 at-least-once，避免 file state 和 run_once
  形成可见的两次提交窗口。
  Date: 2026-07-22

- Decision: `HookSkip` 形成显式 `ActionSkipped` outcome，但不调用 executor、不计 attempt/effect；
  HookRun 失败后的未启动 HookRun 保持 `ActionDeferred`。
  Rationale: CLI 与 Result validator 可以直接投影实际处置，不必从 plan verb 或聚合计数反推；
  同时清晰区分“历史命中无需执行”和“因前序失败尚未尝试”。
  Date: 2026-07-22

- Decision: 只要 canonical plan 含 HookRun，apply 在任何 file/prune mutation 前要求完整 stdin、
  stdout、stderr；缺失视为 execution protocol 错误。
  Rationale: 子进程 I/O 是本次执行能力的必需输入，延迟到 hook phase 才发现会让调用方在 file
  mutation 后得到本可预检的失败。
  Date: 2026-07-22

## Outcomes and Handoff

尚未实施；本节将在完成本地门禁后记录 commits、验证、风险与未验证平台，并保持计划 active
等待独立复核。
