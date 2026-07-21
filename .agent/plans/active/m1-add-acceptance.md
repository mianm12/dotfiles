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
- [x] 2026-07-22：以 `623e41a` 提交本 active ExecPlan 起点。
- [x] 2026-07-22：先补 manifest/add/CLI 回归并确认旧实现失败：helper 缺失、歧义只报告数量且
  暴露合成 HOME 绝对路径、显式 module 只输出泛化 profile 文字；以 `7621996` 建立单一
  manifest 展示 helper 并补全公开诊断。
- [x] 2026-07-22：manifest/add/CLI 普通、add/CLI 5 次重复、三包 race、定向 lint、fix 与完整
  checkpoint diff check、独立 cache/BINARY `make check`、Darwin/Linux amd64 add/CLI test binary
  交叉编译通过；自审完整 branch 无新增 blocking finding。
- [x] 2026-07-22：Round 1 reviewer 报告两个有效 P2：dotted profile 被未引用 TOML key 错解；已在
  profile 但因 GOOS inactive 的 module 得到重复/no-op profile 行。以 `ea7aa0d` 测试先行修复：
  profile key 合法编码并 round-trip；manifest 单一 helper 区分 expanded membership、effective
  OS/target，生成 module-local 激活指引。
- [x] 2026-07-22：Round 1 fix 后重新通过三包普通、add/CLI 5 次重复、三包 race、定向 lint、
  fix/checkpoint diff check、隔离 `make check` 与 Darwin/Linux amd64 add/CLI test binary 交叉编译。
- [x] 2026-07-22：Round 2 reviewer 报告一个有效 P2：不属于当前 expanded profile 与 GOOS/target
  未激活可以同时成立，旧互斥分支只报告 profile 行。以 `221a46e` 增加可信 `OSReady` 并组合全部
  activation facts，稳定输出 profile → OS → target 步骤，要求完成所有列出编辑后再重试。
- [x] 2026-07-22：Round 2 fix 后重新通过三包普通、add/CLI 5 次重复、三包 race、定向 lint、
  fix/checkpoint diff check、隔离 `make check` 与 Darwin/Linux amd64 add/CLI test binary 交叉编译。
- [x] 2026-07-22：Round 3 reviewer 报告一个有效 P2：InProfile/OS ready 但 current GOOS target mapping
  缺失时，strict Resolve 早于显式 module 诊断失败。用户明确授权突破三轮上限；以 `fef58ed`
  将该失败分类为带可信 module/GOOS accessor 的 manifest error，并只在显式请求精确匹配时投影。
- [x] 2026-07-22：Round 3 fix 后重新通过三包普通、add/CLI 5 次重复、三包 race、定向 lint、
  fix/checkpoint diff check、隔离 `make check` 与 Darwin/Linux amd64 add/CLI test binary 交叉编译。
- [ ] 保持计划 active、worktree clean，等待 Round 4 完整 branch 复审。

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

当前上述本地命令均通过。`make check` 完成 tidy/format diff、0 lint issue、全仓 race、build 与
manifest check；Round 1 fix 后已完整重跑。双目标只完成交叉编译，真实 Linux 主机和远端
macOS/Linux CI 未运行。

## Safety, Authorization, and Recovery

用户已授权本 acceptance fix branch/worktree 的计划、范围内修改、stage、commit 与验证。失败使用
新 fix commit，不 amend/rebase/reset/cherry-pick/squash；不操作 main 或其他 worktree。测试不运行
真实 `dot add` 环境。若确切 TOML 行无法从严格加载的直接 profile 声明无歧义生成，或修复必须
改变 manifest 公开契约/持久化格式，则更新计划并停止。

## Interfaces and Dependencies

不新增依赖。manifest 是 profile 直接声明与 TOML 展示的单一事实源；add 继续拥有 module/source
候选选择；CLI 只按既有错误分类和 sealed plan/result 投影，不复制 parser/profile 语义。

## Surprises & Discoveries

- Observation: `manifest.Repository` 同时保存 profile 的直接声明与展开集合，但原有 accessor 只
  暴露 profile 名和 effective module，无法无损重建带 `@profile` 引用的声明行。
  Evidence: `rootSpec.declaredProfiles` 保留解码后的数组顺序；`expandedProfiles` 已排序并展开引用。
  Impact: 新 helper 只消费前者，并返回新切片编码结果；add/CLI 不接触 parser 私有结构。

- Observation: prospective candidate 已按 module/source 字节序稳定排序，但旧错误丢弃了这些事实，
  同时直接打印 target 绝对路径。
  Evidence: `ResolvedProfile.ProspectiveCandidates` 的返回契约与测试；修复前回归实际输出合成 HOME。
  Impact: add 在选择边界按既有顺序分组展示 module/source，并把 target 转为现有 `~/...` display。

- Observation: 合法 profile 名允许 `.`，但 TOML bare key 中的 `.` 表示 dotted key，而不是名称字符。
  Evidence: Round 1 reviewer 以 `work.mac` 证明旧 helper 生成的行无法作为同名 profile 重新解析。
  Impact: manifest 只对 TOML bare-key 字符集内的名称使用 bare key，其余使用 basic string key；测试
  将生成行重新 `Load` 并 `Resolve("work.mac")`。

- Observation: “属于 expanded profile”与“当前 GOOS effective”是两种不同事实；另外 target table
  整键覆盖使 OS 激活可能同时需要 target 编辑。
  Evidence: direct/`@profile` module 可因 module 或 root defaults 的 `os` 被过滤；若 effective target
  table 缺当前 GOOS，只追加 `os` 会在下一次 Resolve 失败。
  Impact: manifest helper 复用 Resolve 的 effective OS/target 合并函数，返回 membership、module-local
  OS 行、target readiness 与需保留的既有 target 行。add 在 target 缺失时明确两项编辑均必需，要求
  用户选择目标路径且绝不猜值。

- Observation: profile、OS 与 target 三个激活维度不是互斥分类；module 可以只属于另一 profile，
  同时继承不含当前 GOOS 的 defaults，并缺少当前 GOOS target。
  Evidence: Round 2 reviewer 与新增 module/root defaults、无 module manifest、跨 `@profile` 回归。
  Impact: add 不再在首个缺失维度提前返回；它消费同一 activation snapshot，按 profile exact line、
  module-local OS line、target 用户选择的稳定顺序投影全部失败维度，并明确需整体完成后重试。

- Observation: active module 缺 current GOOS target mapping 会在严格 `Repository.Resolve` 内失败，
  因而正常的显式 module 校验没有机会消费 activation facts。
  Evidence: Round 3 reviewer 构造 InProfile=true/OSReady=true/TargetReady=false；旧 CLI 只输出通用
  manifest error、exit 1，缺少 module-local target 恢复步骤。
  Impact: Resolve 保持 strict-first，但该单一语义错误携带只读 module/GOOS identity；add 仅在显式
  module 与 identity 精确相等时重新读取同一 activation facts 并投影 conflict。其他 module、无 `-m`
  或其他 Resolve 错误继续原样 fail closed。

## Decision Log

- Decision: 确切 profile 行基于直接声明成员追加 module，不使用 expanded effective module 集。
  Rationale: 展开结果会丢失 `@profile` 引用和原声明顺序，无法满足“可直接采用且不丢现有成员”的
  恢复契约。
  Date: 2026-07-22

- Decision: candidate 诊断只在同一 module 有多个 source 时展开 source 列表，所有歧义都至少输出
  module 列表。
  Rationale: module 是用户选择 `-m` 的公开单位；仅在 `-m` 仍不足以唯一选择时展示仓库相对 source，
  既保留可恢复信息，也不暴露控制面绝对路径。
  Date: 2026-07-22

- Decision: OS-inactive module 不再输出 profile 行，而是在 `modules/<module>/dot.toml` 给出基于
  effective OS 集的 module-local `os` 行。
  Rationale: module 已经 direct 或经 `@profile` 展开属于当前 profile；改 root defaults 会影响其他
  module，重复 profile member 又不会激活当前 GOOS。
  Date: 2026-07-22

- Decision: effective target table 缺当前 GOOS 时不生成虚构 target 值。
  Rationale: target 路径是用户意图且不能从其他 OS 安全推断；诊断应列出需保留的 effective mapping，
  要求在同一 module manifest 选择并加入当前 GOOS target，然后再应用 OS 行，且声明两项均必需。
  Date: 2026-07-22

- Decision: activation 诊断采用组合步骤而非互斥错误类型。
  Rationale: 每个步骤都改变下一次严格 Resolve 的输入；若省略同时存在的后续失败维度，用户应用
  第一步后仍会得到此前可预见但未报告的失败，不满足可恢复诊断契约。
  Date: 2026-07-22

- Decision: target mapping 分类位于 manifest Resolve 边界，恢复投影位于 add preflight 边界。
  Rationale: manifest 是 effective OS/target 语义的单一真相源；add 才知道用户是否显式请求了同一
  module。两者通过 typed error 的 module/GOOS identity 精确连接，不允许按错误字符串或模糊类型
  猜测，也不掩盖其他 strict Resolve 错误。
  Date: 2026-07-22

## Outcomes and Handoff

尚未完成；等待实现、验证与独立复核。
