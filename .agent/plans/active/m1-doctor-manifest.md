# feat/doctor-manifest：M1 manifest-only 静态配置门禁

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和
`Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成本 Goal 后，维护者可运行 `dot doctor --manifest-only`，在不读取机器配置或 state、
不取锁且不产生任何文件 mutation 的前提下，对仓库 manifest、所有模块局部规则、模板、
当前 GOOS 上每个 effective profile 的完整 target 路径边界，以及 Git index 中已跟踪的
`*.local` 做确定性静态诊断。任一 error 退出 1，仅有 warning 退出 2，无 finding 退出 0；
M1 裸 `dot doctor` 会明确说明完整巡检属于 M2，并提示改用 `--manifest-only`。

真实仓库根将新增最小 `dot.toml`，只声明 `requires = ">=0.1.0"` 与空的 `mac` profile。
本地和 macOS/Linux CI 都通过同一个 Makefile 完整门禁，以绝对 repo、只创建 HOME 根的
隔离临时目录运行真实仓库 manifest doctor。README 将准确说明当前能力、M1/M2 边界、
真实仓库检查命令、`*.local` 门禁和当前尚无 `modules/` 的事实。

## Scope / Non-goals

范围内：

- 建立稳定排序的 diagnostic findings 聚合，至少区分 error 与 warning，并集中映射退出码。
- 诊断 requires 缺失、非法、不满足与 development build notice；合法但不满足时仍继续依赖
  manifest 结构的检查，缺失/非法或 TOML 损坏时仍继续可独立执行的 Git index 检查。
- 诊断模式复用 `internal/manifest` 的 raw schema、严格字段/值规则、profile 展开、模块局部
  source 检查、模板编译和 `ResolvedProfile.ValidatePathBoundaries`；诊断结果不构造或暴露可供
  mutation 使用的部分可信 `Repository`。
- 未传 `--profile` 时在当前 GOOS 逐一验证全部声明 profile；显式 profile 只缩小 profile 级
  desired/path 检查。模板与模块局部检查覆盖所有模块，unassigned 模块不进入任一 profile
  target 集合，profile 之间不合并 target 集合。
- manifest-only 只根据 `--repo`、`DOT_REPO` 或默认值确定 repo，不读取机器配置中的 repo
  override；控制面只覆盖 effective HOME、本次 flags/环境和默认值能确定的路径。
- Git index 查询失败形成 error；已跟踪 `*.local` 在 manifest-only 中形成 error。
- 在 CLI 行为测试中固定稳定输出、findings 退出码、裸 doctor 拒绝、stdout/stderr 写失败以及
  `--home`、repo、profile 的公开行为。
- 在完全位于一个 `t.TempDir()` 的 fixture 中证明缺失或故意非法的 machine config/state 均不
  被读取，命令前后目录树完全一致、未创建 lock，且真实 HOME 不变化。
- 先以 Linux missing-path capability gate 固定：只有 effective HOME 根和独立有效 Git repo
  存在，config/state/lock/backup/installed binary 都缺失时，空 finding 的 manifest-only 必须
  只读成功。若现有共享 identity 无法满足，只能在不改变规范且不削弱 mutation fail-closed 的
  前提下，以独立 `fix(paths)` commit 完善共享只读证明能力。
- 新增真实根 `dot.toml`、Makefile 真实仓库 doctor 目标和统一 `make check` 接线；仅在 Makefile
  无法使既有双平台 workflow 执行一次门禁时才修改 `.github/workflows/ci.yml`。
- 同步 README 与因长期命令事实确有必要的 AGENTS.md/CONTRIBUTING.md，分里程碑验证、语义
  commit、独立只读复核及 ExecPlan 收口。

明确不做：

- 不读取、修改、创建或伪造真实 `modules/`、machine config、state、backup、lock、target、
  source 或其他私人数据；所有 fixture 均为隔离的 synthetic data。
- 不实现完整 doctor，不检查 PATH、本机权限、link drift、state 语义、机器配置权限或其他 M2
  checker，不预建 checker registry。
- 不实现自动修复、manifest 重写、target/source/state mutation、完整 planner/apply/add、M2
  managed 生命周期或后续路线图能力。
- 不建立第二套 TOML schema、模板规则、target identity、控制面成员或路径例外；不在 doctor
  层增加字符串前缀、词法 identity fallback，不吞 `ErrIdentityUnavailable`，也不降低 mutation
  入口的 fail-closed 边界。
- 不新增第三方依赖，除非标准库、系统 Git 与现有共享入口经有界评估确实无法满足；出现该
  情况先暂停说明维护、兼容性和替换成本。
- 不修改规范迁就实现，不 merge、push、创建 Pull Request、rebase、tag、发布或删除分支。

## Contract and Context

- `docs/02-architecture.md` §2、§4–§5：doctor 严格只读、无锁；控制面路径解析集中于共享
  边界；manifest/plan/doctor 必须复用同一 target identity、祖先拓扑和控制面成员定义。
- `docs/03-manifest-spec.md` §7–§8：manifest-only 的检查集合、profile 分离、unassigned 规则、
  machine-local 隔离、已跟踪 `*.local` 严重度和 requires 诊断继续语义是公开契约。
- `docs/04-cli-spec.md` §3、§4.6：error/warning/clean 分别映射 1/2/0；M1 裸 doctor 必须拒绝，
  不能把 manifest-only 伪装成完整巡检。
- `docs/06-templates.md` §2–§4、§6：tracked `*.local` 是 CI error；全部有效 scaffold 复用 M1
  `text/template` 语法、函数白名单和已声明变量静态编译规则。
- `docs/07-bootstrap-and-release.md` §3–§4：requires 必填且只接受 `>=x.y.z`；development build
  仅跳过合法约束的大小比较，notice 不改变退出码。
- `docs/08-testing.md` §1–§4：doctor 与 mutation 复用同一边界；集成 fixture 完全隔离；
  manifest-only/profile 分离、裸 doctor、严重度、version 兼容性和双平台 CI 属 M1 回归。
- `docs/09-roadmap.md` §1 M1、§3：仅交付 manifest-only，不反向扩大为 M2 完整 doctor；公开
  行为或安全性质发生未决变化时先请求裁决。

当前基线为 `main@f2362fa`，与 `origin/main` 一致并包含 `feat/path-boundaries` 的最终成果；
分支 `feat/doctor-manifest` 从该提交创建。`internal/manifest.Load` 会严格、fail-closed 地读取根与
全部模块 manifest，随后一次性展开全部 profile；`Repository.Resolve` 形成当前 GOOS 的
effective profile；`Repository.ValidateTemplates` 复用 source 分类和 `internal/template.Compile`；
`ResolvedProfile.ValidatePathBoundaries` 复用 `internal/paths.ValidatePathBoundaries` 并先形成完整
结构性 desired。生产 mutation 消费者未来仍只能取得完整严格 `Repository`；doctor 的可继续
诊断必须停留在 findings 层。

现有 `manifest.ReadRequirement` 是宽松 requires 预读，但 TOML 语法损坏会使其失败；
`manifest.Load` 复用 `rawRootManifest` / `rawModuleManifest` 的严格解码和全部校验，但首错即返。
实施需要在 manifest package 内增加诊断编排接缝，使单份 raw schema 和校验规则可产生按阶段
聚合的 findings，同时不向外泄露非法 partial repository。requires 与 strict load 可在结构可信
时分别报告，但同一根因不得制造误导性的重复 finding。

`internal/paths` 当前在 Linux 对任何 missing name 返回 `ErrIdentityUnavailable`。这保持 mutation
fail-closed，却会让 config/state/binary 均不存在的新机 fixture 在控制面校验阶段失败。执行先
写 capability gate；若失败，评估能否由共享 paths API 在只读条件下权威证明该有限路径集合的
身份与边界。若修改共享 path identity/boundary API，必须先更新本计划并由未参与实现的只读
subagent 复核设计；若只能靠近似语义、doctor-local fallback、预创建占位文件或规范变更才能
继续，则命中停止条件。

`internal/cli` 当前只有 `version`。`run` 统一管理 Cobra 错误输出与
`errorTrackingWriter`，命令必须返回一种可由 `run` 区分的 doctor 结果而不绕过现有写失败优先级。
Makefile 的 `check` 当前为 `tidy-check fmt-check lint test-race build`；workflow 已在 macOS 和
Ubuntu 各运行一次 `make check`，因此预期只需把一次真实仓库 doctor 接入 Makefile，workflow
不应制造重复步骤。

## Progress

- [x] 2026-07-18：确认初始工作区 clean；`main` 与 `origin/main` 同为 `f2362fa`；
  `feat/path-boundaries` 是 main 祖先；本地与 remote 均无 `feat/doctor-manifest`。
- [x] 2026-07-18：从 main 创建并切换到 `feat/doctor-manifest`；读取 Goal 指定的仓库规则、
  ExecPlan 约定、规范、路线图，以及 manifest/template/desired/path/CLI/Makefile/CI 当前实现与
  测试；未读取真实 modules 或 machine-local/private data。
- [x] 2026-07-18：创建本 active ExecPlan；Scope / Non-goals、capability gate、milestones、验证、
  授权与停止条件已写入，`git diff --check` 通过，正进入仅包含计划文件的首个语义 commit。
- [ ] 执行 Linux missing-path capability gate；若共享 paths 修复必要，先设计复核，再实现、
  测试、门禁、diff 检查并以独立 `fix(paths)` 提交。
- [ ] 实现 findings、requires 与诊断继续语义，完成测试、门禁、diff 检查和语义提交。
- [ ] 完成全部 manifest/profile/template/path/Git 静态检查，完成测试、门禁、diff 检查和语义
  提交。
- [ ] 接入 Cobra doctor、稳定输出和退出码，完成隔离/只读/写失败测试、门禁、diff 检查和
  语义提交。
- [ ] 创建最小根 `dot.toml`，验证并语义提交。
- [ ] 接入 Makefile 与确有必要的 CI 改动，验证只运行一次并语义提交。
- [ ] 同步 README 与确有必要的长期仓库指南，验证并语义提交。
- [ ] 由未参与实现的只读 subagent 复核全部实质改动；以新的语义 commit 修复意见并完成必要
  复核。
- [ ] 完成最终门禁与 diff 检查，收口 living sections，迁移计划至 `completed/` 并创建
  plan-closure commit。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只创建本 active 计划，把 Scope / Non-goals、capability gate、授权、分里程碑 commit 和停止条件
纳入版本管理；不改生产代码、测试、配置或 CI。执行前检查 staged 内容为空，只 stage 本文件，
检查拟提交 diff 后提交。

Concrete steps：

    在 repo root 运行：git diff --check && git diff -- .agent/plans/active/m1-doctor-manifest.md
    预期：仅新增本计划，且无 whitespace error。
    运行：git add .agent/plans/active/m1-doctor-manifest.md
    运行：git diff --cached --check && git diff --cached --stat && git diff --cached
    运行：git commit -m 'docs(doctor): 新建 manifest doctor ExecPlan'
    预期：提交成功，git status --short 为空。

Commit 边界：

    docs(doctor): 新建 manifest doctor ExecPlan

### Milestone 2：Linux missing-path capability gate 与必要的共享 paths 修复

先增加跨平台 doctor 层集成测试及 Linux 定向测试：fixture 只创建 effective HOME 根与独立有效
Git repo，所有 machine-local 控制面路径缺失，合法空 profile 且没有其他 finding；执行前后树
完全相同。测试必须先在当前实现上证明具体 capability 缺口。若 macOS 已通过而 Linux 因
`ErrIdentityUnavailable` 失败，先把证据和候选共享契约写入 `Surprises & Discoveries` / Decision
Log；只要共享 path API 会变化，就在生产修改前请求未参与实现的只读 subagent 设计复核。

只有复核确认能够可靠证明边界且 mutation 的严格入口仍 fail closed，才在 `internal/paths`
实现最小共享只读证明能力。doctor 只能选择该明确的共享只读契约，不得在 caller 检查字符串、
忽略 sentinel、创建占位路径或复制控制面关系。paths 测试同时固定可靠成功、无法证明仍返回
`ErrIdentityUnavailable`、control/target 冲突和 mutation 默认入口不降级。若无法做到，更新计划
并暂停请求规范裁决。

Concrete steps：

    在 repo root 运行新增 capability 窄测；预期：修改前 Linux-only 测试以
    ErrIdentityUnavailable 暴露缺口，修改后合法 missing-path fixture 退出 0 且树未变化。
    若修改 paths：go test -count=20 ./internal/paths
    运行相关 doctor/manifest 窄测与：make check BINARY=/private/tmp/dot-doctor-capability/dot
    运行 milestone 起点至当前的 git diff --check、完整 diff、stat 与 untracked 检查。

验收：

- 只存在 HOME 根和独立 repo 的合法 manifest-only 严格只读成功。
- config/state root/state.json/lock/backup/binary 不存在，不被预创建；无法可靠证明的其他拓扑仍
  fail closed。
- mutation 使用的现有严格边界入口和安全回归不被降低。

Commit 边界（仅 capability gate 证明必要时）：

    fix(paths): 完善缺失路径的只读边界证明

### Milestone 3：findings、requires 与诊断继续语义

在窄职责 doctor package（预计 `internal/doctor`）建立确定性 finding 值、severity、稳定排序与
exit mapper；warning-only → 2 仅由单元测试固定，不发明公开 warning。建立 repository-level
诊断编排，使 requires 缺失、非法或不满足均为 error；合法但不满足仍继续 manifest/profile/
template/path，缺失/非法与 TOML 损坏仍继续独立 Git index 检查。development build 对合法
requires 跳过比较并产生 notice 输出数据而非 warning。

为避免第二 schema，诊断所需的 raw decoding 和字段/值校验留在 `internal/manifest`，复用现有
raw types 与 helper；只有通过对应完整结构校验后才能形成私有严格 Repository 供后续 profile
检查。非法/partial 数据只转换为 findings，不能被 mutation caller 取得。测试至少覆盖 requires
问题与 unknown key/tracked `*.local` 同次报告、TOML 损坏仍报告 tracked `*.local`、顺序稳定和
notice 不改变退出码。

Concrete steps：

    运行：go test ./internal/doctor ./internal/manifest -run '<findings/requires/diagnostic tests>'
    运行：make check BINARY=/private/tmp/dot-doctor-findings/dot
    运行 milestone 起点至当前的完整 diff、git diff --check 和 untracked 检查。

Commit 边界：

    feat(doctor): 建立 manifest 诊断聚合

### Milestone 4：manifest/profile/template/path/Git 全部静态检查

补齐 manifest-only engine：仓库级与全部 module manifest 严格 schema/未知字段/值、profile
环与悬空、模块局部 source/files/hooks/path 规则、全部有效 scaffold 模板静态编译、当前 GOOS
逐 profile Resolve 与完整 `ValidatePathBoundaries`，以及 `git ls-files` 等系统 Git index 查询。
Git 命令以选定 repo 为 cwd，不解释工作树未跟踪文件；查询启动或退出失败形成 error。

默认 profile 集合取已声明 profile 的稳定副本；显式 profile 必须存在并只影响 profile-level
Resolve/path 循环。profile 逐个校验并保留 profile provenance，不把 entries 合并。模块局部与
模板检查在严格 repository 可用时覆盖所有模块，包括 unassigned 与当前 GOOS inactive 模块。
若某阶段因上游结构损坏无法运行，其他独立阶段仍继续，输出只描述实际完成的检查。

Concrete steps：

    运行：go test ./internal/doctor ./internal/manifest -run '<static/profile/template/git tests>'
    运行：make check BINARY=/private/tmp/dot-doctor-static/dot
    运行 milestone 起点至当前的完整 diff、git diff --check 和 untracked 检查。

验收：

- profile 分离、显式 profile 缩小、unassigned 局部检查、全部模板和模块局部错误均有行为测试。
- target identity/ancestor/control overlap 由 `ResolvedProfile.ValidatePathBoundaries` 报告，不存在
  doctor-local 路径逻辑。
- Git index 查询失败不是 clean；tracked `*.local` 为 error 且稳定排序。

Commit 边界：

    feat(doctor): 完成 manifest 静态检查

### Milestone 5：Cobra doctor、稳定输出与严格只读隔离

新增 `doctor` 子命令和 `--manifest-only` flag。裸 doctor 直接返回明确 M2 边界错误与使用提示，
不运行最小 checker 冒充完整巡检。manifest-only 解析 effective HOME、config 展示路径和 repo
时不调用 `config.Load`：repo 来源仅 flag、环境或默认路径；随后构造共享 control paths 并调用
doctor engine。findings 以确定格式和稳定顺序输出，notice 单独输出；命令结果通过现有 Cobra
错误与 output tracking 机制映射 0/1/2，stdout/stderr 写失败仍优先为 1。

进程级行为测试的 HOME、repo、DOT_CONFIG、DOT_REPO、machine config/state/backup/Git fixture
都位于同一 `t.TempDir()` 根；分别覆盖 machine config/state 完全不存在和故意非法两种场景，
执行前后快照相同，lock 缺失，真实 HOME 只做未变化断言。另覆盖 invalid config 内 repo override
不会被读取，默认/环境/flag repo 优先级，显式 profile、裸 doctor、help 与写失败。

Concrete steps：

    运行：go test ./internal/cli -run 'TestDoctor|TestRoot' -count=10
    运行：make check BINARY=/private/tmp/dot-doctor-cli/dot
    运行 milestone 起点至当前的完整 diff、git diff --check 和 untracked 检查。

Commit 边界：

    feat(cli): 接入 manifest doctor 命令

### Milestone 6：真实根 manifest

创建受版本管理的根 `dot.toml`，内容精确为：

    requires = ">=0.1.0"

    [profiles]
    mac = []

不增加 defaults、data、module、example、placeholder 或 `modules/`。用开发构建和隔离参数验证
该 manifest 在当前平台所有 profile 下 clean；Linux CI 的 `mac` 只作为 profile 名处理，不做
Darwin 过滤。

Concrete steps：

    运行相关 manifest/doctor 窄测与隔离的 dot doctor --manifest-only。
    运行：make check BINARY=/private/tmp/dot-doctor-config/dot
    检查本 milestone 完整 diff 与 git diff --check。

Commit 边界：

    chore(config): 添加根 manifest

### Milestone 7：Makefile 与双平台统一 CI 门禁

Makefile 新增命名清晰的真实仓库 manifest doctor 目标并更新 `make help`。命令从调用侧创建
绝对临时根，只创建 HOME 根；repo 使用 Makefile 求得的绝对仓库根；`DOT_CONFIG` 和
`DOT_REPO` 清除或重定向到同一隔离根；传绝对 `--home` 与 `--repo`，不传 `--profile`；调用侧
trap 清理临时目录。`make check` 只依赖一次该目标，确保本地和 workflow 的 macOS/Linux matrix
自动执行一次。若此接线已足够，不改 workflow；只有真实语义必要时才调整。

Concrete steps：

    运行：make help
    运行：make doctor-manifest BINARY=/private/tmp/dot-doctor-ci/dot
    运行：make check BINARY=/private/tmp/dot-doctor-ci/dot
    检查 make -n 或等价证据确认完整 check 中 doctor 仅一次，且命令不依赖调用者 HOME。
    检查完整 diff、git diff --check 和 untracked。

Commit 边界：

    ci(doctor): 接入真实 manifest 门禁

### Milestone 8：同步仓库当前能力文档

README 改为准确说明已实现 `dot version` 与 `dot doctor --manifest-only`，裸 doctor 的 M1/M2
边界，真实仓库 Makefile 命令，tracked `*.local` 会使 manifest-only/CI 失败，以及根 profile
为 `mac`、尚无 `modules/`。若 Makefile 的长期事实使 AGENTS.md 或 CONTRIBUTING.md 的命令
说明失真，只做必要同步；不记录分支进度，不弱化私人数据规则。

Concrete steps：

    运行文档链接/格式适用检查、make help 和 make check BINARY=/private/tmp/dot-doctor-docs/dot。
    检查本 milestone 完整 diff 与 git diff --check。

Commit 边界：

    docs(repo): 记录 manifest doctor 能力

### Milestone 9：独立复核与 review fixes

在所有实质 milestone commit 完成且工作区 clean 后，由未参与实现的只读 subagent 复核
`main...HEAD` 全部实质改动。上下文包含本计划 Scope / Non-goals、规范与代码分界、requires
继续语义、Linux capability gate、machine-local 纯度、完整 profile/path 共享入口、Git 查询、
CLI 写失败、真实 manifest/CI 和最终验证证据。主 agent 自行验证意见；实质问题以新的语义
commit 修复并重新运行相关窄测、make check、diff 检查。修复改变原结论时复核完整改动，否则
至少复核 fix diff。

Commit 边界（有实质意见时）：

    fix(<scope>): <对应问题的中文摘要>

### Milestone 10：ExecPlan 终态收口

独立复核与所有门禁通过后，更新本计划 Progress、最终证据、Decision Log 和 Outcomes and
Handoff，把同一文件从 `active/` 迁至 `completed/`。拟提交 diff 只能包含该终态更新和迁移；
若出现生产/测试/文档实质变动，退出机械收口并回到新的 fix commit、验证和必要复核。

Concrete steps：

    运行最终 make check、git diff main...HEAD --check、完整无 pathspec diff/stat 和 status 检查。
    迁移计划后运行 git diff --check 与 staged closure diff 检查。
    提交：git commit -m 'docs(doctor): 收口 manifest doctor ExecPlan'
    预期：工作区 clean，计划最近一次成功 commit 位置为 completed/。

Commit 边界：

    docs(doctor): 收口 manifest doctor ExecPlan

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| findings 顺序和 0/1/2 映射确定 | doctor/CLI 单元与行为测试 | 待验证 |
| requires 错误不阻断独立检查，合法不满足继续结构检查 | 同次报告组合测试 | 待验证 |
| schema/profile/module/template 复用单一 manifest 语义 | manifest/doctor 集成测试与 diff review | 待验证 |
| 每个 profile 独立做完整 target/control boundary | profile 分离、显式 profile、unassigned 测试 | 待验证 |
| Linux 新机 missing machine-local 路径可只读证明 | capability gate 与 Linux CI | 待验证 |
| manifest-only 不读 config/state、不取锁、零 mutation | 缺失/非法 fixture、前后树快照、lock/真实 HOME 断言 | 待验证 |
| Git index 无法查询为 error，tracked `*.local` 为 error | synthetic Git repo 行为测试 | 待验证 |
| 裸 doctor 明确拒绝，manifest-only 输出/退出码稳定 | CLI 端到端与 writer failure 测试 | 待验证 |
| 根 `dot.toml` 精确且两个平台验证 `mac` profile | config diff、Makefile gate、GitHub matrix（若本 Goal 不 push 则标未验证） | 待验证 |
| 本地与 CI 共享 Makefile 入口且 doctor 仅运行一次 | Makefile dry-run/行为、workflow review | 待验证 |
| 全部 Go/CI 适用门禁通过 | 每 milestone 与最终 `make check` | 待验证 |
| 全部实质改动通过独立只读复核 | subagent 终审与 review fix 证据 | 待验证 |

最终在 repo root 运行：

    make check BINARY=/private/tmp/dot-doctor-final/dot
    git diff main...HEAD --check
    git diff --stat main...HEAD
    git diff main...HEAD
    git status --short --branch

成功判据：当前平台完整门禁退出 0；完整 diff 只包含本 Goal 授权路径且无 whitespace error；
工作区 clean；独立复核无未处理实质意见。由于本 Goal 不授权 push，GitHub Actions matrix 若未由
其他授权触发，必须明确记录为未验证，不能声称通过。

## Safety, Authorization, and Recovery

当前 Goal 明确授权从满足前置条件的 main 创建并切换 `feat/doctor-manifest`，创建/更新/stage/
commit 本计划和 Goal 范围内实现、测试、根配置、Makefile/必要 workflow/README/必要指南，
以及满足收口条件后的 active→completed 迁移、plan-closure commit 与迁移失败时仅恢复本计划
状态。branch 已成功创建。stage/commit 只纳入当前 milestone 明确文件，操作前后均检查 status
和 staged diff；不 amend、squash 或重写已完成提交。

所有 Git/doctor fixture 的 HOME、repo、config、state、backup 与 synthetic files 位于同一个
`t.TempDir()` 或调用侧绝对临时根。manifest-only 不运行 mutation 命令，不读取真实 machine
config/state，不获取 lock；真实仓库手动 gate 只创建临时 HOME 根，清除或重定向 DOT_CONFIG/
DOT_REPO，并显式传绝对 repo。测试和 Makefile 失败后均由 fixture owner / shell trap 清理；
重复运行不得改变真实仓库、HOME 或 machine-local state。

若 capability gate 修改共享 path API，生产修改前必须更新计划并完成未参与实现的只读设计
复核。无法可靠证明路径边界、必须改变规范性质、只能依赖 fallback/吞错/占位文件、需要新增
依赖、发现任务外 dirty/私人数据冲突，或 runtime approval 后仍无法完成已授权 Git 操作时，
立即更新 Progress 并暂停请求裁决。不得以降低测试、跳过平台或留下未提交 diff 替代。

## Interfaces and Dependencies

预期由 `internal/doctor` 暴露只读 manifest-only engine 与 diagnostic result，CLI 只负责解析
flag、确定无需 machine config 的路径来源、输出与进程退出映射。`internal/manifest` 可以增加
doctor 专用诊断入口，但 raw schema 和严格 validator 仍为单一真相源；入口不得返回非法部分
Repository。`internal/paths` 的严格 boundary 入口保持 mutation 安全语义；任何额外只读证明
模式必须在共享 package 明确表达 capability，并对无法证明继续返回 sentinel。模板只经现有
`internal/template.Compile`，Git index 只经系统 `git`，不新增第三方依赖。

## Surprises & Discoveries

- Observation: Linux 当前对任何 missing name 的共享 identity 解析返回
  `ErrIdentityUnavailable`，因此只存在 HOME 根的新机也无法通过完整控制面校验。
  Evidence: `internal/paths/name_semantics_linux.go` 与
  `internal/paths/boundary_linux_test.go` 明确固定该行为。
  Impact: doctor 实现前必须先跑 capability gate；不能靠 CLI/doctor fallback 或创建控制面占位
  文件绕过，必要修复必须留在共享 paths 并保持 mutation fail-closed。

- Observation: 现有 workflow 已在 macOS/Linux 各执行同一个 `make check`。
  Evidence: `.github/workflows/ci.yml` 的单一 `Run project checks` step。
  Impact: 预期只改 Makefile 即可完成双平台门禁；没有语义需要时不修改 workflow。

## Decision Log

- Decision: 将 goal 文件规定的默认语义边界分别保留为 ExecPlan、可选 paths fix、diagnostic
  aggregation、完整 static engine、CLI、root config、CI、repo docs、review fixes 和 closure
  commits；每个里程碑测试随实现提交。
  Rationale: 每个切片都能独立验证与回退，并避免把 capability/security 改动与用户界面或真实
  配置压在同一提交。
  Date: 2026-07-18

- Decision: manifest diagnostics 可以编排首错阶段，但不得让非法 partial schema 形成可供
  mutation 使用的 Repository；只有完整严格 load 成功后才运行依赖其结构的 profile/template/
  path 检查。
  Rationale: 复用同一 schema/validator 和 production strict loader 的 fail-closed 边界，同时让
  requires/TOML 与 Git index 这类独立检查继续聚合。
  Date: 2026-07-18

## Outcomes and Handoff

尚未收口。当前分支已从满足前置条件的 `main@f2362fa` 创建，强制上下文读取完毕，本 active
ExecPlan 正待形成首个语义 commit；生产代码、测试、真实根 manifest、Makefile/CI 和 README
尚未修改。merge、push、Pull Request、rebase、tag、发布和删除分支不在本 Goal 授权范围。
