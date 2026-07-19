# feat/hook-planner：形成 M1 run_once 纯计划

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，planner 可以把一个已完成完整 profile 校验与 module scope 选择的
`manifest.ScopedProfile`，连同 missing/strict-loaded state，转换成确定顺序、完全只读的 M1
run_once 计划。每个 hook action 明确保存执行方式、指纹、cwd、target root、运行上下文、环境
覆盖和成功/失败 state 处置；计划只读取脚本并分类，绝不执行脚本或写入指纹。

## Scope / Non-goals

范围内：

- 消费冻结的 `ScopedProfile.Hooks()` 与 strict state `run_once`，对 string-form M1 hook 形成
  `skip` 或 `run-hook`。
- 只读 `Lstat`/`ReadFile` 普通脚本；任一 executable bit 存在时 direct exec，否则计划
  `sh <script>`。
- 使用带版本、长度前缀的无歧义 SHA-256 指纹，只包含 script bytes 与执行分类；M1 没有 watch。
- 保持模块字节序和模块内 manifest 声明顺序；partial scope 只计划 scope hooks，历史
  `run_once` 永不因当前 scope/profile 缺失而删除。
- 在独立 `HookPlan`/`HookAction` 类型中保存 module/script/path/cwd/target root/profile/OS/repo、
  明确 HOME/XDG 与 DOT_* 环境、invocation、fingerprint，以及成功 upsert/失败 preserve。
- 任一脚本或输入错误 fail-fast，并返回零值 plan，不泄漏 partial actions。

明确不做：

- 不执行 hook、不写 state/run_once、不实现 executor、CLI、file plan、prune、lock 或持久化。
- 不让 file conflict 进入 hook planner 输入或阻塞 hook；apply planner 后续独立组合两类计划。
- 不实现 M2 watch/table-form、并行 hook、跨模块依赖图、profile/data 指纹或环境读取。
- 不修改冻结的 `internal/planner/model.go`、decision/ownership、prune 或 manifest 接缝；不复制
  这些逻辑，不新增依赖或 filesystem abstraction。

## Contract and Context

- `docs/05-apply-engine.md` §8：M1 run_once 的顺序、执行分类、cwd、环境、指纹、历史保留、
  at-least-once 和 conflict 不门控 hooks 是直接契约。
- `docs/02-architecture.md` §4–§6：plan 必须只读、自包含；hook 是 executor 读取仓库脚本的明确
  例外，成功才更新指纹，失败 preserve。
- `docs/04-cli-spec.md` §4.2–§4.4、§5：部分 apply 只考虑请求模块，dry-run/diff/status 零写入，
  输出由后续 presentation 负责。
- `docs/03-manifest-spec.md` §3：严格 manifest 已验证 M1 string run_once、普通文件与同模块身份
  唯一；`ScopedProfile.Hooks()` 是冻结的 planner 接缝。
- `docs/08-testing.md` §3.1–§3.3：覆盖指纹、执行方式、声明顺序、partial scope、失败重试、历史
  保留和 conflict 下仍计划。
- `docs/09-roadmap.md` §1 M1、§3：当前只交付 string run_once 纯计划，不引入 M2 watch。

最初的并行 Wave 因 decision contract 尚未完成 freshness 而撤销，未在本 worktree 留下文件改动。
主线程随后在本 branch 非重写同步 current CP3 基线，形成
`1f11fcd chore(integration): 同步 CP3 当前基线`；当前 main `1a7e0fc` 是其祖先，worktree clean。
该同步已经包含完成复核的 decision/ownership、orphan resolution 与 prune contract。共享 model、
decision/prune 和 manifest 接缝现已冻结；本 Goal 只能新增独立 hook planner 类型和实现。

## Progress

- [x] 2026-07-19：确认分配 worktree、Git 顶层、`feat/hook-planner`、clean `HEAD=1f11fcd` 与
  current main ancestry；读取规范、冻结接口及 completed decision/target-observation plans。
- [ ] 提交本 active ExecPlan 起点。
- [ ] 测试先行建立执行分类、versioned fingerprint 与自包含单 hook action。
- [ ] 测试先行组合 strict state、顺序、partial scope、历史保留与 fail-fast 完整 plan。
- [ ] 运行窄测、20 次、race、双平台交叉编译、branch diff check 与 `make check`，保持计划 active。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定 freshness、范围、冻结边界、实现顺序与验证方式。

    git diff --check
    git add .agent/plans/active/m1-hook-planner.md
    git diff --cached --check
    git commit -m 'docs(planner): 建立 hook planner 执行计划'

### Milestone 2：建立 hook observation、指纹与动作 payload

先增加普通/non-executable/executable/目录/symlink/缺失脚本测试与 fingerprint golden/分类变化测试，
再实现只读单 hook planner。SHA-256 输入显式包含算法版本、字段标签、执行分类和长度前缀，避免
拼接歧义；action 保存 future executor 所需的全部输入但不执行。

    go test ./internal/planner
    go test -count=20 ./internal/planner

验收：regular 脚本按 `mode & 0111` 分类；bytes 或分类变化改变指纹，相同输入稳定；非普通或
读取失败返回 error/零 action；direct 与 shell invocation、环境和 state effect 自包含。

Commit 边界：

    feat(planner): 建立 hook 指纹与动作

### Milestone 3：组合 scoped hooks 与 strict state

先以真实 manifest/profile/state fixture 覆盖多模块/模块内声明顺序、missing/相同/变化指纹、
partial scope、额外历史不清理、file conflict 无输入以及末项失败不返回 partial；再组合完整 plan。

    go test ./internal/planner ./internal/manifest ./internal/state
    go test -count=20 ./internal/planner ./internal/manifest ./internal/state

验收：同指纹 skip，missing/变化 run-hook；顺序完全来自 scope hook descriptor；只对当前 hook
产生 preserve/upsert，不生成删除；任一错误返回零值 plan。

Commit 边界：

    feat(planner): 组合 scoped run_once 计划

### Milestone 4：验证并交接 review

运行完整本地门禁、双平台交叉编译与 diff 审计，只更新 active 计划的真实证据，不迁移
completed，等待 coordinator 安排独立复核。

    go test -count=20 ./internal/planner ./internal/manifest ./internal/state
    go test -race ./internal/planner ./internal/manifest ./internal/state
    GOOS=darwin GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-hook-darwin.test ./internal/planner
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-cp3-hook-linux.test ./internal/planner
    git diff 1a7e0fc...HEAD --check
    make check BINARY=/private/tmp/dot-cp3-hook-planner-check/dot

Commit 边界：

    docs(planner): 记录 hook planner 交接证据

## Validation and Acceptance

| 性质 | 证据 | 状态 |
|---|---|---|
| regular/executable 分类与 invocation | hook filesystem tests | 待实施 |
| versioned 无歧义指纹及分类变化 | fingerprint golden/table tests | 待实施 |
| missing/same/changed run_once | strict state plan tests | 待实施 |
| 模块/声明顺序与 partial scope | manifest integration tests | 待实施 |
| 历史不清理、conflict 不门控、fail-fast 零 partial | plan boundary tests | 待实施 |
| 当前平台完整门禁 | `make check` | 待运行 |
| 远端 macOS/Linux CI | 精确 branch HEAD | 待验收（本 worker 不 push） |

## Safety, Authorization, and Recovery

本任务授权只覆盖 `/private/tmp/dot-cp3-hook-planner-019f795e` 中 active 计划、新 hook planner、
测试、stage 与语义 commits。测试只使用 `t.TempDir()` 合成 HOME/repo/state/script，不读取私人
数据，也不执行 fixture hook。失败保留最近成功 commit，以新 commit 修复；不切 branch，不操作
main/其他 worktree，不 merge、amend、rebase、reset 或 force。

## Interfaces and Dependencies

公开入口消费 `manifest.ScopedProfile`、missing/strict-loaded `state.Loaded` 与可信绝对 repo path，
返回不透明 `HookPlan`。`HookPlan.Actions()` 给出深拷贝；每个 `HookAction` 自包含 invocation、环境、
fingerprint 与 run_once state effect。仅使用标准库 SHA-256、二进制长度编码、Lstat/ReadFile 与
既有 manifest/state 类型，不新增依赖。

## Surprises & Discoveries

暂无。

## Decision Log

- Decision: HookAction 使用独立类型，不扩展冻结的共享 file `Action`/`StateEffect`。
  Rationale: run_once state 的 key/hash/时间与 file entry 结构不同；强塞共享类型会污染 contract，
  而独立 action 是 apply planner 需要组合的真实职责边界。
  Date: 2026-07-19

- Decision: 环境以有名字段保存覆盖值，不使用 map 作为计划 contract。
  Rationale: HOME/XDG/DOT_* 是封闭集合；有名字段让完整性可审查，并避免 map 顺序或遗漏成为
  presentation/executor 的隐式行为。
  Date: 2026-07-19

## Outcomes and Handoff

尚未完成；计划保持 active。
