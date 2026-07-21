# feat/add-cli：开放安全 add 命令

本 ExecPlan 是 living document。实施期间持续更新 `Progress`、`Surprises & Discoveries`、
`Decision Log` 和 `Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成后，M1 用户可以通过公开 `dot add` 把 HOME 中普通文件安全收编为 link 或 scaffold。
命令支持 `-m`、互斥的 `--template`/`--scaffold` 与 `--dry-run`；mutation 只调用已经封存的
`internal/add.Run`，dry-run 只用 `runtime.LoadReadOnly` 与 `add.Preflight`。输出稳定展示运行
context、逐项动作和手工 Git 提示，所有无效内部 plan/result 都 fail closed。

## Scope / Non-goals

范围内：

- 注册 `add [-m <module>] [--template|--scaffold] [--dry-run] <path>...`，校验至少一个路径和
  模式互斥。
- M1 `--template` 在 runtime IO、lock、source、target、state 前硬错误。
- dry-run 走严格只读加载与 sealed preflight，无锁且对全新 HOME 零写入。
- CLI 仅投影 sealed add plan/result/error，稳定输出 context、link/scaffold 动作和成功后的手工
  `git add`/`git commit` 提示；退出码遵循 `1 > 3 > 2 > 0`。
- module 推断零/多候选映射为 3，运行错误映射为 1；zero/invalid result + nil error 映射为 1。
- 全隔离 CLI 测试证明 link/scaffold、部分成功、hard-link、dry-run 与 add 后 apply 收敛；同步
  README 当前实现事实。

明确不做：

- 不复制 manifest、Git ignore、Precond、ownership、state 或恢复逻辑；不改变下层持久化契约。
- 不实现 M2 managed/`--template`，不创建 modules、不修改 manifest、不执行 Git stage/commit。
- 不新增依赖，不读取或修改真实 modules、机器配置、state、backup、`.env` 或主力 HOME。

## Contract and Context

- `docs/02-architecture.md` §2–§6：mutation 持锁、dry-run 无锁、CLI/计划/执行职责分离。
- `docs/03-manifest-spec.md` §2–§6/§8：add 仍经 strict manifest 与完整 profile 校验，CLI 不写 manifest。
- `docs/04-cli-spec.md` §2–§4.5/§5：公开 flags、输出、退出码、source-first 与 Git 提示。
- `docs/05-apply-engine.md` §1–§7/§9–§10：反向映射、提交点、恢复与 add 后 apply 幂等。
- `docs/06-templates.md`：M1 scaffold 与 `*.local`，managed/template 留给 M2。
- `docs/08-testing.md`：全隔离、dry-run 零写入、提交点/恢复与公开输出证据。
- `docs/09-roadmap.md` §1/§3：本切片只开放 M1 link/scaffold add。

有效 base 为 clean `main@9206981002dd4c0e0420b5e3c1fde5132aca1737`，branch `feat/add-cli`。
前三个 Milestone 已分别封存 prospective/Git 全批预检、link publication/target commit 和 scaffold
state commit。`internal/add.Preflight` 返回 sealed `BatchPlan`；`internal/add.Run` 返回 sealed
`Result` 并在协议违规时无效。`internal/runtime.LoadReadOnly` 是 dry-run 唯一加载入口。

## Progress

- [x] 2026-07-22：确认分配 worktree、branch、有效 base 和 clean 状态；读取执行规则、CP6 规范、
  coordinator 与前三个 completed plans，以及现有 CLI/add/runtime/apply 实现和测试。
- [ ] 提交本 active ExecPlan 起点。
- [ ] 测试先行固定 flags、投影、输出/退出码和 invalid result fail-closed。
- [ ] 实现只读 dry-run 与 mutation wiring，覆盖全隔离 link/scaffold E2E 与 apply 收敛。
- [ ] 同步 README，运行窄测、race、diff check、隔离 `make check` 和双目标交叉编译。
- [ ] 保持 active 等待未参与实现的完整独立复核。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定 CLI 与下层安全契约分界。

    docs(add): 新建 add CLI ExecPlan

### Milestone 2：固定公开参数与结果投影

先在 `internal/cli` 增加 runner seam 测试，证明命令注册、至少一个 path、`-m`、互斥模式、M1
template 早拒绝、sealed plan/result 投影和退出码优先级。投影只读取 accessors，不重做下层判断；
invalid/zero result + nil error 返回 `add.ErrExecutionProtocol`。动作输出按 plan 顺序稳定，context
取 sealed plan；只有已越过各自提交点的成功项进入 Git 提示。

    feat(cli): 注册安全 add 命令

### Milestone 3：连接只读与 mutation 路径并证明 E2E

dry-run 调 `runtime.LoadReadOnly` 后调用 `add.Preflight`，不取锁；mutation 调 `add.Run`。所有 runtime
override 都来自统一 global options。全隔离 fixture 创建 synthetic HOME/repo/config/state/backup 与
Git repository，直接覆盖 link/scaffold、module inference/显式 module、部分成功、hard-link、
dry-run 零写入及 add→apply→apply 收敛；下层已有 ignore/source/故障点覆盖不在 CLI 重复实现。

    test(cli): 覆盖 add 隔离端到端行为

### Milestone 4：同步当前实现说明

README 将 `add` 列入已实现命令，准确说明 M1 link/scaffold、dry-run、手工 Git 提示和 template
未支持；不改变规范。

    docs(readme): 记录 M1 add 能力

## Validation and Acceptance

运行 `go test ./internal/cli ./internal/add ./internal/apply`、相关重复/race、定向 lint，
`git diff 9206981002dd4c0e0420b5e3c1fde5132aca1737...HEAD --check`，使用 `/private/tmp` 独立
cache/BINARY 的 `make check`，以及 Darwin/Linux amd64 CLI test binary 交叉编译。所有 mutation
测试使用 `t.TempDir()` 的合成 HOME/repo/config/state/backup，显式隔离 DOT/HOME/XDG/Git，并检查
真实 HOME sentinel 不变。真实 Linux 与远端 macOS/Linux CI 未运行时明确标为待验收。

## Safety, Authorization, and Recovery

用户已授权本 branch/worktree 的 active plan、范围内修改、stage、commit 与验证。失败使用新 fix
commit，不 amend/rebase/reset/cherry-pick/squash；不切换或合并 main/其他 worktree。若 CLI 必须
复制下层安全语义、改变公开/ownership/state 契约，或无法证明 dry-run/模板零写入，则更新计划并停止。

## Interfaces and Dependencies

不新增依赖。CLI seam 只替换 `add.Run`、只读加载/预检以覆盖协议投影；production 默认分别绑定
`add.Run`、`runtime.LoadReadOnly` 与 `add.Preflight`。context/action/result 均从 sealed plan/result
accessor 获得，CLI 不获得构造可信 plan/result 的能力。

## Surprises & Discoveries

尚无。

## Decision Log

- Decision: dry-run 与 mutation 分别消费 `Preflight` sealed plan 和 `Run` sealed result，不为二者
  建立新的共享可伪造 DTO。
  Rationale: 下层已经以 validity seal 固定完整预检和执行事实；CLI 应保持纯投影。
  Date: 2026-07-22

## Outcomes and Handoff

尚未收口；本计划提交后进入测试先行实现。
