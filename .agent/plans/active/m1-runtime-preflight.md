# feat/runtime-preflight：建立可信只读运行前置

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，后续 mutation、只读计划和恢复流程可以先通过一个共享的只读 preflight，取得同一组
effective HOME、机器配置路径、repo、profile、data 与已校验控制面路径。非法机器配置或路径
关系会在 state、lock、manifest 或任何写入之前失败；preflight 本身不会创建 state root、lock
或其他文件。现有 `dot version` 继续允许新机在 config/repo 均缺失时报告
`requires=unavailable`，`doctor --manifest-only` 继续完全不读取机器配置与 state。

## Scope / Non-goals

范围内：

- 在新的窄职责 `internal/runtime` 包建立共享 preflight，复用 `internal/config` 的严格 TOML
  解码与 `internal/paths` 的 effective HOME、config/repo 解析及控制面校验。
- 普通 profile/data 消费入口要求机器配置存在且有效；init 入口允许缺失，并且只有 init 结果
  保留“配置原先缺失”这一提交前提所需状态。
- repository-only 入口供 `version` 使用：已有配置仍完整严格校验，缺失配置则按 repo 覆盖或
  默认路径继续，但不向调用方暴露 config-missing 状态。
- repo 优先级固定为 `--repo > DOT_REPO > machine config > default`；profile 优先级固定为
  `--profile > machine config`。任何持久化 machine repo 即使被高优先级覆盖遮蔽也必须校验。
- 返回与同一 effective HOME/config/repo 对应的 `paths.ControlPlanePaths` 和已经通过
  `paths.ValidateControlPlane` 的控制面；保持全部路径与 cwd 无关。
- 用合成临时根测试严格配置、优先级、缺失策略、控制面冲突及完整零写入边界；接入 version
  后重跑其新机与配置错误回归，并保留 doctor manifest-only 隔离回归。

明确不做：

- 不写或更新 machine config，不实现 init、apply、diff、status、add、self-update 或其他公开
  命令。
- 不读取 state，不创建 state root/state.json/lock，不取锁，不加载 requires 或严格 manifest；
  `version` 自己现有的 requires 预读仍在 preflight 返回之后执行。
- 不改变 `doctor --manifest-only` 的专用静态诊断路径，不让它进入共享 preflight。
- 不新增依赖，不建立通用 DI、planner、state、lock 或 mutation 框架，不预建后续 Milestone。
- 不读取或修改真实 `modules/`、machine config、state、backup、HOME 或私人数据。

## Contract and Context

- `docs/02-architecture.md` §2–§5：路径优先级和语法必须集中；严格机器配置与控制面校验发生在
  lock/state/写入前；路径边界负责 HOME 与控制面身份，CLI 负责编排。
- `docs/03-manifest-spec.md` §8：requires 与严格 manifest 是 preflight 之后的独立加载阶段；
  manifest-only 是不读取 machine config/state 的诊断例外。
- `docs/04-cli-spec.md` §2–§3、§4.1、§4.6、§4.10：全局路径与 profile 覆盖只影响本次运行；
  init 允许缺失旧配置；version 新机仍可报告 binary/repo unavailable；manifest-only 严格只读。
- `docs/05-apply-engine.md` §2、§4–§6：state、lock 与 mutation 均不属于本切片，但未来必须只
  消费已经通过本前置的运行上下文。
- `docs/06-templates.md` §3：preflight 保留严格 machine data 的稳定字符串快照；声明键投影与
  缺值检查属于后续 manifest/render 阶段，不能在此读取环境或 manifest。
- `docs/08-testing.md` §1–§3：非法控制路径零写入，生产与测试复用同一边界；HOME、config、
  repo、state 和 lock fixture 全部位于一个临时根。
- `docs/09-roadmap.md` §1 M1、§3：本切片只完成可信加载基础，不提前实现 planner/mutation。

基线为 clean `main@5b099066`，分支 `feat/runtime-preflight` 从该提交创建。现有
`internal/paths.EffectiveHome`、`Config`、`Repository`、`ResolveControlPlanePaths` 与
`ValidateControlPlane` 已提供共享路径原语；`internal/config.Load` 已严格拒绝未知字段、错误
类型、空 profile/repo 和非法 data key，但把文件缺失普遍表示为合法空状态。现有
`internal/cli/version.go` 手工组合这些步骤，未形成后续命令可复用的完整控制面前置；
`internal/cli/doctor.go` 的 manifest-only 路径则有意不调用 `config.Load`。

本计划把 config-missing 分成三种调用语义，而不改变持久化格式：普通 profile/data 消费者
直接失败并提示运行 init；init 得到显式 missing 状态用于未来提交 Precond；repository-only
消费者把缺失作为没有 machine repo override，继续解析 repo，但结果中没有 missing 标志。
这同时满足“只为 init 保留 config-missing 状态”和 version 新机可用契约。

## Progress

- [x] 2026-07-19：确认 `pwd` 与 Git 顶层均为分配 worktree，分支为
  `feat/runtime-preflight`，HEAD/base 为 `5b099066`，工作树 clean。
- [ ] 提交本 active ExecPlan 起点。
- [ ] 先增加共享 preflight 缺口测试，再完成最小实现与窄测试。
- [ ] 接入 version repository-only 入口并证明 version/manifest-only 例外不变。
- [ ] 完成重复窄测、双平台编译、完整门禁与 diff 审计；保持计划 active，等待独立复核。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只新增并提交本计划，记录范围、策略、验证和授权；不修改生产代码、测试或配置。

    git diff --check
    git add .agent/plans/active/m1-runtime-preflight.md
    git diff --cached --check && git diff --cached
    git commit -m 'docs(runtime): 新建 preflight ExecPlan'

Commit 边界：

    docs(runtime): 新建 preflight ExecPlan

### Milestone 2：建立共享 preflight 与缺失策略

先在 `internal/runtime` 增加测试，覆盖严格配置、所有 repo/profile 优先级、空显式 profile、
持久 repo 被 override 时仍校验、普通/init/repository-only 缺失差异、cwd independence、控制面
冲突和零写入树快照。测试应先因共享入口不存在而暴露缺口，再实现最小 API。返回值必须来自
同一次严格配置读取与路径解析，data 使用独立 map；任何失败不返回部分可信上下文。

    go test ./internal/runtime
    go test -count=20 ./internal/runtime

验收：合法输入得到唯一绝对 HOME/config/repo 与已校验控制面；普通缺失失败，init 缺失可辨，
repository-only 缺失使用默认或 override；所有路径错误和控制面冲突零写入。

Commit 边界：

    feat(runtime): 建立统一只读 preflight

### Milestone 3：接入 version 并固定诊断例外

把 `internal/cli/version.go` 的 HOME/config/repo 手工拼装替换为 repository-only preflight，保留
先输出 build 信息、repo unavailable 成功、非法配置/路径失败和 requires 诊断顺序。
`doctor --manifest-only` 不接入 preflight；用既有及必要新增回归证明缺失/非法 config/state
不被读取，命令不创建 lock。

    go test ./internal/cli ./internal/runtime
    go test -count=20 ./internal/cli ./internal/runtime

验收：version 的既有 stdout/stderr/exit 行为不变，并新增控制面冲突在 repo 读取前失败；
manifest-only 面对非法 machine config/state 仍按静态仓库结果执行且树不变。

Commit 边界：

    refactor(cli): 复用 runtime preflight

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| 严格 machine config 与被遮蔽 repo 仍 fail closed | runtime table tests | 待验证 |
| repo/profile 优先级和 config-missing 三种策略 | runtime table tests | 待验证 |
| cwd 无关且控制面完整隔离 | runtime/path filesystem tests | 待验证 |
| preflight 零 state/lock/文件写入 | 临时树前后快照 | 待验证 |
| version 新机与错误诊断不回归 | CLI tests | 待验证 |
| manifest-only 不读取 config/state | 既有 CLI isolation tests | 待验证 |
| 当前平台完整门禁 | `make check` | 待验证 |
| macOS/Linux 可编译 | 本机测试及交叉编译 | 待验证 |

最终从本 worktree 根运行：

    go test -count=20 ./internal/runtime ./internal/cli
    GOOS=darwin GOARCH=amd64 go test -run '^$' ./internal/runtime ./internal/cli
    GOOS=linux GOARCH=amd64 go test -run '^$' ./internal/runtime ./internal/cli
    git diff 5b099066...HEAD --check
    make check BINARY=/private/tmp/dot-cp2-preflight-check

成功判据是全部命令退出 0，完整 diff 仅含本 Goal 的计划、runtime 实现/测试和必要 CLI 接线，
worktree clean。交叉编译只证明编译，不声称目标平台运行；远端 CI 未实际运行时明确待验收。

## Safety, Authorization, and Recovery

当前任务明确授权在分配 worktree 的本分支创建/修改范围内文件、stage、commit 和运行门禁；
不授权操作 main、其他 worktree、merge、push、rebase、amend 或读取真实私人数据。测试只使用
`t.TempDir()` 的合成 HOME/config/repo/state 路径；preflight 生产路径不含创建或写入调用。

每个 milestone 形成独立 commit。失败时保留最近成功提交，以新 fix commit 修正；不 reset、
restore、clean、amend 或吞错。计划在 worker 交付时保持 `active/`，由 coordinator 安排独立
review、修复、最终门禁和生命周期迁移。

## Interfaces and Dependencies

`internal/runtime` 只依赖现有 `internal/config` 与 `internal/paths`。它提供普通、init 和
repository-only 三个语义明确的入口，避免由调用方传布尔组合出非法策略；不新增第三方依赖。
后续 state/lock/loading Milestone 可以消费普通 preflight 结果，但本分支不规定它们的类型或
执行算法。

## Surprises & Discoveries

暂无。

## Decision Log

- Decision: 用不同入口表达普通、init 与 repository-only 策略，而不是暴露通用
  `allowMissing` 布尔值。
  Rationale: 只有 init 需要保留 missing 作为未来配置提交 Precond；version 只需要在缺失时
  使用 repo override/default，普通 profile/data 消费者必须失败。
  Date: 2026-07-19

- Decision: manifest-only 保持现有专用静态路径，不进入 machine-aware preflight。
  Rationale: `docs/03-manifest-spec.md` §7–§8 明确要求该模式不读取或要求 machine config/state。
  Date: 2026-07-19

## Outcomes and Handoff

尚未实施。完成代码、门禁与 worker 自审后，本计划仍留在 `active/`，等待 coordinator 指派未
参与实现的 reviewer；不得在本 worktree 自行收口为 completed。
