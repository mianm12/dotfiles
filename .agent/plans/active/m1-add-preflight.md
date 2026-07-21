# feat/add-preflight：建立安全 add 全批次预检

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，后续 add runner 与 CLI 能调用一个只读、自包含的批次预检入口：它先快照 HOME 中
的普通文件，保守确定当前 profile 内模块，再把本批精确 source 作为虚拟普通文件送入正常
manifest 严格枚举、ignore、`[files]`、suffix、render 与完整 profile 路径边界；只有全部输入
同时满足 state、控制面、碰撞、source no-clobber 与系统 Git 可跟踪性要求时才返回稳定排序的
link/scaffold 动作计划。任何失败都不创建 lock、source、target、state 或临时文件。

## Scope / Non-goals

范围内：

- 普通文件输入规范化、bytes/九位 mode/identity 快照，以及 `*.local`、重复和控制面重叠拒绝。
- 已有 state 拒绝、当前 profile 内保守 module inference 与显式 module 校验。
- manifest prospective overlay：只豁免本批精确候选，复用正常严格枚举和完整 profile 路径边界。
- link/scaffold source 三变体 no-clobber、完全等价遗留 source 续跑与渲染一致性。
- 使用系统 Git 判断 tracked 或可跟踪；repository、local 与 global exclude 均生效，异常 fail closed。
- 自包含、确定排序的 batch/item plan；M1 template 在任何写入前明确拒绝。

明确不做：

- 不创建 `modules/`、不修改 manifest，不执行产品 `git add`/`git commit`，不获取 mutation lock。
- 不发布 source、不修改 target/state，不实现提交时 Precond、runner 或 Cobra CLI。
- 不新增依赖，不实现 M2 managed/template、目录递归、特殊文件或自动创建 module。
- 不读取或修改真实 modules、机器配置、state、backup、`.env` 或主力 HOME。

## Contract and Context

- `docs/02-architecture.md` §2–§6：复用控制面和完整 profile target 身份边界；预检保持只读。
- `docs/03-manifest-spec.md` §2–§6/§8：候选仅按已加入的精确普通 source 豁免，[files]、ignore、
  suffix、路径和严格解码继续成立，无关悬空引用仍失败。
- `docs/04-cli-spec.md` §2–§4.5/§5：全批次预检、Git 可跟踪性、source 变体、M1 template 和 dry-run
  零写入行为。
- `docs/05-apply-engine.md` §1–§7/§9–§10：输入快照、反向映射、完整 profile 碰撞和等价续跑契约。
- `docs/06-templates.md`：scaffold 必须渲染 bytes/mode 等于输入；`*.local` 硬拒绝。
- `docs/08-testing.md`：真实隔离 Git ignore/exclude、候选 overlay、碰撞和零写入证据。
- `docs/09-roadmap.md` §1/§3：只交付 M1 link/scaffold preflight，不预建 mutation。

有效 base 为 clean `main@5d176497a75c9f8e43b413d43f04f3ea41720c51`，branch
`feat/add-preflight`。现有 `manifest.ResolvedProfile.ValidatePathBoundaries` 会从真实 source 树
严格枚举；`runtime.LoadReadOnly` 提供 strict manifest/state/control；`paths` 提供统一 target 与
control identity；缺口是精确 prospective source seam 和 add batch 组合层。

## Progress

- [x] 2026-07-21：确认分配 worktree、branch、base 与 clean 状态；完整阅读执行规则、CP6 规范、
  coordinator 计划、相关 completed ExecPlans 和现有 manifest/runtime/state/path 实现。
- [ ] 提交本 active ExecPlan 起点。
- [ ] 测试先行建立 manifest prospective overlay，同源覆盖严格枚举、ignore/[files]/suffix/render、
  无关悬空和完整 profile target 边界。
- [ ] 测试先行建立 add batch preflight/model、module inference、state/control/source/Git 全批次 gate。
- [ ] 运行窄测、重复/race、双目标编译、完整 diff check 与隔离 cache `make check`；保持 active 等待复核。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，固定范围、单一语义源、零写入与验证边界。

Commit 边界：

    docs(add): 新建 add preflight ExecPlan

### Milestone 2：建立 manifest prospective source overlay

先在 `internal/manifest` 测试中证明当前真实枚举无法接受尚未发布但由 `[files]` 预声明的候选。
增加只读精确候选输入；枚举仍遍历全部真实模块对象并执行同一 ignore/内置 ignore、source 引用、
后缀定级、render 和 target 形成逻辑，只把与候选精确同路径的 missing source 视为带 bytes/mode 的
普通文件。候选不得遮蔽既有对象，无关悬空 `[files]`/hook 继续失败。完整 profile 与 control
boundary 在 overlay 后一次性校验，返回与候选唯一对应的 rendered desired。

验收：普通 link/scaffold 候选通过；manifest ignore/hooks/不匹配 target/kind 拒绝；无关 missing
规则、现存同路径、source/target collision 和完整 profile 其他碰撞均拒绝；真实 repo 树零变化。

Commit 边界：

    feat(manifest): 支持 add prospective source 校验

### Milestone 3：组合 add 全批次只读计划

在新 `internal/add` 包先写真实 FS/Git 测试。入口消费已严格加载的 runtime inputs、显式模式、可选
module 与 path 列表；形成普通文件 snapshot，拒绝 state/control/local/重复，保守推断或校验 module，
计算 source 三变体并调用 manifest prospective API。随后逐候选验证 source 等价/no-clobber、
scaffold render bytes/mode 和系统 Git tracked/ignored 结果。Git adapter 只运行参数固定、repo-relative、
带 `--` 的命令，并允许窄 runner seam 注入启动/退出异常；真实测试隔离 HOME/XDG、system/global
config 和 `.git/info/exclude`/`core.excludesFile`。任一失败不返回部分 plan。

验收：显式/推断 module、输入类型、已有 state、完整碰撞、source variants、等价遗留 source、
tracked/ignore/local/global exclude、Git unavailable、M1 template 与多输入原子预检均有证据；计划
只含后续 runner 所需 snapshot、source/target、kind、content/mode 和 source reuse 事实。

Commit 边界：

    feat(add): 建立全批次安全预检

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| prospective 与正常 manifest/ignore/[files]/render 同源 | manifest overlay tests | 待验证 |
| state/control/module/碰撞全批次 gate | add 真实 FS tests | 待验证 |
| Git tracked/ignore/local/global 与异常 fail closed | 隔离真实 Git + runner seam | 待验证 |
| source 三变体、等价续跑、scaffold 一致性 | add plan tests | 待验证 |
| 零 lock/source/target/state/temp 写入 | fixture 完整树快照 | 待验证 |
| Darwin/Linux | 本地测试、交叉编译与远端 CI | 待验证 |

最终在 `/private/tmp/dot-m1-cp6-add-preflight` 运行相关 package tests、重复/race、双目标 test
binary 交叉编译、`git diff 5d176497a75c9f8e43b413d43f04f3ea41720c51...HEAD --check` 和使用
唯一 `/private/tmp` cache/BINARY 的 `make check`。成功要求命令退出 0、完整 diff 仅含本计划、
manifest prospective seam 与 add preflight/model、worktree clean。

## Safety, Authorization, and Recovery

用户已授权本 branch/worktree 的 active plan、范围内修改、stage、commit 和验证。所有测试只使用
`t.TempDir()` 的合成 HOME/repo/config/state 与隔离 Git 配置；不运行产品 mutation，不读取真实私人
数据。失败用新 fix commit，不 amend/rebase/reset/cherry-pick/squash；不切换或合并 main/其他 branch。

若 prospective 只能靠写真实 `modules/`、放宽无关 missing `[files]`、复制 ignore/path/Git matcher，
或必须改变公开/ownership/state 契约，则更新计划并停止。测试中 Git 初始化只写合成 repo，不做网络
访问。

## Interfaces and Dependencies

不新增依赖。manifest 暴露的 prospective 输入只表达精确 module-relative source bytes/mode，并返回
已通过完整 profile boundary 的 desired；add 包负责 target 输入、module inference、state 与 Git
组合，不获得绕过 manifest 规则的能力。Git 使用标准库 `os/exec` 调系统 Git；窄 seam 只用于故障
分类测试，不实现 matcher。

## Surprises & Discoveries

- Observation: 正常 `enumerateModuleSources` 同时收集全部对象并在末尾严格校验 `[files]`/hook 引用。
  Evidence: `internal/manifest/source.go`。
  Impact: overlay 应进入这一个枚举接缝，而不是为 add 复制定级或 ignore。

## Decision Log

- Decision: manifest prospective overlay 使用内存候选，不在 repo/modules 创建临时文件。
  Rationale: 预检必须零写入；同一枚举接缝可只豁免精确候选并保持其他严格错误。
  Date: 2026-07-21

## Outcomes and Handoff

尚未收口。当前计划建立后进入测试先行实施；保持 active 等待独立复核。
