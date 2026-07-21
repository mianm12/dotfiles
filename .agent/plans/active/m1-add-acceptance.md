# fix/m1-add-acceptance：补全 add module 恢复诊断

本 ExecPlan 是 living document。实施期间持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，`dot add` 因 module 无法唯一推断、显式 module 不存在或不属于当前 effective profile
而安全拒绝时，会给出稳定、可直接采用且不丢失现有 profile 声明的恢复信息。歧义诊断列出
候选 module，并在同一 module 存在多个可能 source 时列出 source；profile 修复诊断从严格解析的
根 manifest 只读生成确切 TOML 行，CLI 仍不修改 manifest。

## Scope / Non-goals

范围内：

- 为严格加载的 manifest 增加当前 profile 直接声明成员的只读展示 accessor，并以确定 TOML
  字符串编码生成“保留原顺序/`@profile` 引用、追加 module”的确切行。
- 补全 add module inference 的稳定候选诊断，以及不存在/不在 profile 的两步手工恢复指引。
- 覆盖公开 CLI exit 3、dry-run 零写入和不泄漏内部绝对 source 路径。

明确不做：

- 不修改 manifest、module、source、target 或 state，不改变 module 推断、profile 展开、ownership、
  Precond、持久化格式与退出码契约。
- 不新增依赖，不实现 M2/M3，不读取或修改真实 modules、机器配置、state、backup、`.env` 或 HOME。

## Contract and Context

- `docs/03-manifest-spec.md` §2/§6：profile 直接成员可包含 module 与 `@profile` 引用；CLI 不写
  manifest，而应打印确切手工编辑行。
- `docs/04-cli-spec.md` §4.5/§5：零/多 module 候选退出 3 并要求 `-m`；不存在 module 给出 mkdir 与
  profile 两步指引，不在 effective profile 时给出确切 TOML 行。
- `docs/05-apply-engine.md` §9：module 推断必须保守，不能唯一确定时列出候选；不存在或不在
  profile 时提供手工恢复路径。
- `docs/08-testing.md`：add dry-run 零写入，公开错误与安全拒绝需由隔离测试证明。

有效 base 为 clean `main@0ddb42a518a878a5f6eb69943d15e83c6fa39573`，checkpoint base 为
`5d176497a75c9f8e43b413d43f04f3ea41720c51`。首次三路 Checkpoint Acceptance 的安全与工程主轴
GO；规范主线发现两个同根 P2：歧义只报告数量，profile 恢复只给泛化文字。本分支仅修复这一
公开诊断契约，并保持前三个 mutation Milestone 与 CLI 投影的已复核安全边界。

## Progress

- [x] 2026-07-22：确认分配 worktree、branch、effective base 与 clean 状态；阅读执行规则、四份
  completed Milestone ExecPlan、coordinator Acceptance 结论、相关规范和现有 manifest/add/CLI。
- [ ] 提交本 active ExecPlan 起点。
- [ ] 测试先行修复候选与 profile 确切行诊断，运行窄门禁并自审。
- [ ] 保持计划 active、worktree clean，等待未参与实现的完整 branch 复核。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定 Acceptance finding、非目标和验证边界。

    docs(add): 新建 add acceptance ExecPlan

### Milestone 2：建立 manifest profile 展示事实

先在 `internal/manifest` 测试证明直接成员的副本语义与确切 TOML 行保持声明顺序、profile 引用和
正确字符串转义，再增加最小只读 accessor/helper。helper 只消费严格解码后的 `declaredProfiles`，
不重新解析文件、不展开 profile，也不写 manifest。

验收：`base = ["core", "@shared", "app"]` 一类输出可直接采用；调用方修改返回值不改变
repository；未知 profile 明确失败。

### Milestone 3：投影完整 add module 恢复诊断

先补 `internal/add` 与 `internal/cli` 回归：多 module、同 module 多 source、确定排序、显式 module
不在 profile、module 不存在、dry-run 零写入与 exit 3。随后让 preflight 的既有 candidate 事实生成
稳定诊断；显式 module 错误消费 manifest helper 生成 profile 行。只展示 module 与仓库相对 source，
不输出控制面绝对路径。

    fix(add): 补全 module 恢复诊断

## Validation and Acceptance

最终在本 worktree 运行相关 manifest/add/CLI tests、add/CLI 5 次重复、相关 race、定向 lint，
`git diff 0ddb42a518a878a5f6eb69943d15e83c6fa39573...HEAD --check`、
`git diff 5d176497a75c9f8e43b413d43f04f3ea41720c51...HEAD --check`、独立 `/private/tmp`
cache/BINARY 的 `make check`，以及 Darwin/Linux amd64 add/CLI test binary 交叉编译。所有 mutation
测试仅使用 `t.TempDir()` 合成 HOME/repo/config/state/backup；真实 Linux 与远端 CI 标记待验收。

## Safety, Authorization, and Recovery

用户已授权本 acceptance fix branch/worktree 的计划、范围内修改、stage、commit 与验证。失败使用
新 fix commit，不 amend/rebase/reset/cherry-pick/squash；不操作 main 或其他 worktree。测试不运行
真实 `dot add` 环境。若确切 TOML 行无法从严格加载的直接 profile 声明无歧义生成，或修复必须
改变 manifest 公开契约/持久化格式，则更新计划并停止。

## Interfaces and Dependencies

不新增依赖。manifest 是 profile 直接声明与 TOML 展示的单一事实源；add 继续拥有 module/source
候选选择；CLI 只按既有错误分类和 sealed plan/result 投影，不复制 parser/profile 语义。

## Surprises & Discoveries

暂无。

## Decision Log

- Decision: 确切 profile 行基于直接声明成员追加 module，不使用 expanded effective module 集。
  Rationale: 展开结果会丢失 `@profile` 引用和原声明顺序，无法满足“可直接采用且不丢现有成员”的
  恢复契约。
  Date: 2026-07-22

## Outcomes and Handoff

尚未完成；等待实现、验证与独立复核。
