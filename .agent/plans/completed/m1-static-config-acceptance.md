# fix/m1-static-config-acceptance：收口 manifest doctor 的静态可信边界

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，`dot doctor --manifest-only` 会拒绝 unassigned/当前 GOOS inactive 模块中无法形成
合法 entry target 的 source，并在查询目标仓库 Git index 时不读取调用进程 HOME/XDG
中的 global Git config；`make doctor-manifest` 也会把进程 HOME 与 XDG 路径一并放入只创建
HOME 根的绝对临时目录。维护者可在外部 HOME 含损坏 `.gitconfig` 时仍对真实仓库执行稳定、
严格只读的静态检查，且 config/state/lock/backup/binary 继续保持缺失。

## Scope / Non-goals

范围内：

- 为 doctor 的 Git index 查询增加合成外部 HOME 污染回归，并固定 Git 的 HOME/XDG 配置发现
  使用本次 effective HOME，而不是调用进程的其他 HOME。
- 补齐 unassigned/当前 GOOS inactive 模块的本地 target 派生校验，复用既有 desired 枚举规则，
  拒绝去模板后缀后 basename 为空等结构错误。
- 让 Makefile capability gate 同时重定向 `HOME`、`XDG_CONFIG_HOME`、`XDG_STATE_HOME` 与
  `XDG_CACHE_HOME`，仍只预创建临时 HOME 根。
- 运行相关窄测、真实仓库 gate、`make check`、完整 checkpoint diff check 与独立复核。

明确不做：

- 不改变 manifest、requires、profile、desired、template 或 path identity/topology 语义。
- 不创建 config/state/lock/backup/binary 占位，不增加 doctor-local fallback，不跳过 Git 检查。
- 不读取真实 HOME、真实 machine config/state/private data，不实现 M2/M3 能力。
- 不 merge、push、rebase、amend、tag、发布或删除分支。

## Contract and Context

- `docs/03-manifest-spec.md` §7、`docs/04-cli-spec.md` §4.6：manifest-only 严格只读，不读取或
  要求 machine config/state，Git index 查询失败必须是 error。
- `docs/08-testing.md` §2、§4：集成测试的 HOME、配置、state、backup 与 repo 必须全部隔离；
  macOS/Linux CI 运行同一真实仓库门禁。
- `AGENTS.md` 的真实私人数据与测试隔离规则：本 Goal 不得读取主力 HOME 或真实机器配置。

Checkpoint 基线为 `f746262`；修复分支从 clean `main@9020446` 创建。本地 main 比
`origin/main@f2362fa` 超前 13 个提交，精确 HEAD 尚无远端 Actions run。

现有 `internal/doctor.trackedLocalFiles` 清除调用者提供的全部 `GIT_*`，但随后让 Git 按进程
`HOME`/XDG 重新读取 system/global config。`Makefile` 的 gate 只传 `--home`，没有改变进程
HOME。合成 process HOME 中的非法 `.gitconfig` 已使真实仓库 manifest-only 稳定退出 1，证明
隔离缺口成立。

另一个独立缺口是 `Repository.ValidateTemplates` 对全部模块只复用 source 分类和 scaffold
编译，`ValidateModuleRules` 只做 effective module 配置解析；两者都不调用既有
`desiredTarget`。因此 unassigned/inactive 模块的 `.template` 可在 basename 为空时通过 doctor，
但模块进入 profile 后 `Enumerate` 才失败，形成静态门禁 false-clean。

## Progress

- [x] 2026-07-18：完成 checkpoint 只读审查、三路独立复核启动、基线与远端状态确认。
- [x] 2026-07-18：用合成 process HOME 复现 Git global config 污染；未读取真实 HOME。
- [x] 2026-07-18：复核 unassigned/inactive 模块 target 派生缺口，确认 doctor 可漏报。
- [x] 2026-07-18：从 clean main 创建 `fix/m1-static-config-acceptance`。
- [x] 2026-07-18：以 `ecc9ca6` 提交本计划起点。
- [x] 2026-07-18：新增 5 个 unassigned/inactive target 派生失败回归；修改前全部
  false-clean，修复后 `go test -count=20 ./internal/manifest ./internal/doctor` 通过。
- [x] 2026-07-18：新增合成坏 process HOME 回归，修改前 Git 读取外部 `.gitconfig` 并失败；
  修复后 doctor/CLI 窄测与完整两包各 20 次、真实仓库 gate、坏 process HOME 手工复现均通过，
  effective HOME 保持为空。
- [x] 2026-07-18：`make check BINARY=/private/tmp/dot-m1-static-config-acceptance-final`、
  Linux/amd64 交叉编译与 `git diff f746262...HEAD --check` 通过。
- [x] 2026-07-18：三名未参与原实现的 subagent 分别复核规范数据流、路径/零写入与
  工程门禁；主线程逐项核对后均为 GO，未发现 P0–P3。
- [x] 2026-07-18：完成本地收口；最终精确 HEAD 未推送且没有 Actions run，Checkpoint
  保持“本地验收通过、远端待验收”。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本 active 计划，记录已确认根因、范围、验证与授权，不修改实现或测试。

Commit 边界：

    docs(doctor): 新建静态配置验收修复计划

### Milestone 2：补齐全部模块的本地 desired target 校验

新增 manifest/doctor 回归，覆盖 unassigned 与当前 GOOS inactive 模块中的 basename `.template`
及显式 kind 覆盖后缀场景，证明修改前 manifest-only false-clean。模块局部检查必须复用现有
source 分类、target 派生与 target-root/override 规则，但不渲染模板、不形成跨模块 target 集，
也不把 unassigned 并入 profile 碰撞集合。

验证：

    go test -count=20 ./internal/manifest ./internal/doctor

Commit 边界：

    fix(manifest): 校验全部模块的本地 desired target

### Milestone 3：隔离 Git 配置与真实仓库 gate

先在 `internal/doctor` 增加回归：process HOME/XDG 中的损坏 Git config 不影响显式空 effective
HOME 下目标 repo 的 tracked `*.local` 查询，且外部 `GIT_*` 覆盖仍不能重定向 index。修改前
测试必须失败。随后在 Git 子进程环境中清除外部 `GIT_*` 及 process HOME/XDG，并设置本次
effective HOME/XDG；不得禁用 repo-local config，也不得静默忽略 Git 命令错误。

同步修改 Makefile recipe，使真实仓库 gate 的进程 HOME/XDG 也指向同一临时 HOME 下的缺失
路径，同时保留 `--home`、绝对 `--repo` 与清除 `DOT_CONFIG`/`DOT_REPO`。不创建任何新增目录。

验证：

    go test -count=20 ./internal/doctor ./internal/cli
    make doctor-manifest BINARY=/private/tmp/dot-m1-static-config-acceptance
    make check BINARY=/private/tmp/dot-m1-static-config-acceptance

Commit 边界：

    fix(doctor): 隔离 manifest 门禁的 Git 配置

### Milestone 4：全量重新验收、独立复核与收口

重新审查 `f746262...HEAD`，运行相关窄测、真实空 HOME capability gate、完整门禁与
`git diff f746262...HEAD --check`。由未参与本次修复的只读 subagent 复核新增 fix diff；主线程
处理任何实质意见。远端精确 HEAD 没有 macOS/Linux Actions 时，只记录“本地验收通过、远端
待验收”，计划可以收口为本地 review-ready，但 Checkpoint 不标记完整通过。

满足条件后更新 living sections，把本文件移入 `completed/` 并创建只含终态计划迁移的提交。

Commit 边界：

    docs(doctor): 收口静态配置验收修复计划

## Validation and Acceptance

| 必须成立的性质 | 证据 | 状态 |
|---|---|---|
| unassigned/inactive 模块的非法 target 派生被拒绝 | manifest/doctor 回归 | 通过 |
| Git index 查询不读取 process HOME/XDG global config | 合成损坏配置回归 | 通过 |
| 外部 `GIT_*` 不能重定向 repo/index | 既有与新增 doctor 测试 | 通过 |
| gate 只预创建空 HOME 根，machine-local 路径保持缺失 | Makefile/manual capability gate 与树快照 | 通过 |
| CP1 全部本地门禁与 diff check 通过 | 窄测、`make doctor-manifest`、`make check`、Linux/amd64 交叉编译、完整 diff | 通过 |
| 当前 HEAD 远端双平台 CI 实际通过 | GitHub Actions 精确 SHA | 待验收：精确修复 HEAD 无 run |

## Safety, Authorization, and Recovery

用户已明确授权创建/切换本分支、修改范围内代码/测试/Makefile/CI/README/本计划、stage、commit
及 active/completed 迁移；不授权 push/merge/rebase。测试只使用 `t.TempDir()` 或精确
`mktemp -d` 路径，所有 HOME/XDG/repo/config/state/backup 均为合成数据。失败保留最近成功
commit，以新 fix commit 修正；不使用 reset、clean、restore、amend 或隐藏 fallback。

## Surprises & Discoveries

- Observation: `--home` 只决定 dot 的 effective HOME，不能自动改变已启动 Git 对进程 HOME/XDG
  的配置发现。
  Evidence: 合成 process HOME 的非法 `.gitconfig` 使 `git ls-files` 返回 128，而显式 effective
  HOME 为空且完整路径边界检查已通过。
  Impact: 必须同时修复 Git 子进程环境的确定性与 Makefile capability gate 的进程级隔离。

- Observation: 全部模块的静态 source/template 检查没有消费既有 `desiredTarget`，因此非 profile
  模块的本地 target 派生错误被推迟到未来 profile 枚举。
  Evidence: `.template` 去后缀后为空只由 `desiredTarget` 报错；unassigned 模块不进入逐 profile
  `ValidatePathBoundaries`。
  Impact: 模块局部校验必须复用非渲染 desired 结构形成规则，仍保持 profile 间 target 集分离。

- Observation: `origin/main@f2362fa` 的 macOS/Linux Actions 已成功，但它早于本地 main 与本次
  修复；精确修复 HEAD 没有远端 run。
  Evidence: 公共 GitHub Actions API 对 `2828dc7` 返回空 run 列表；远端只存在 main 分支，
  且本 Goal 不授权 push。
  Impact: 该证据只能确认既有 CI 接线曾在双平台成功，不能替代本次最终 commit 的远端验收。

## Decision Log

- Decision: 在 doctor Git 启动边界把 HOME/XDG 重定向到 effective HOME，并在 Makefile 同样
  显式设置 process HOME/XDG；两层分别保证直接调用/测试与真实仓库 gate 不接触主力 HOME。
  Rationale: 仅改 Makefile 会让直接调用和 Go 集成测试仍可能读取真实 HOME；仅改 Git config
  环境则不能让用户要求的空 process HOME gate 名副其实。两者都不改变 repo-local index 语义。
  Date: 2026-07-18

- Decision: 扩展既有模块局部检查，使其在合成 HOME 下运行同一 source 分类和 target 派生；
  不通过构造临时 profile 或新增 doctor-only parser 完成。
  Rationale: 这能覆盖 unassigned/inactive 模块的局部合法性，同时不改变 profile 全局碰撞集合、
  template render 或路径 identity 语义。
  Date: 2026-07-18

## Outcomes and Handoff

本地验收已收口。基线确认是 `f746262`：它是当前 main 的祖先，提交内容仍为实现前规范收口；
其后只有一项规范文档复核，首个实现提交为 `a89d72d`。分支从 clean `main@9020446` 创建，没有
改写既有 CP1 历史。

本计划形成三个语义 commit 边界：计划起点 `ecc9ca6`；`cc293a5` 修复全部模块的本地 desired
target 校验并附回归；`2828dc7` 隔离 doctor Git 的 process HOME/XDG，补齐 Makefile capability
gate 与回归。README、CI、依赖和规范不需要修改。

本地证据包括：相关 manifest/doctor/CLI 测试 20 次重复通过；真实仓库
`make doctor-manifest` 通过；外部 HOME/XDG 与 `GIT_*` 组合污染时，空 effective HOME gate
退出 0 且目录前后保持为空；`make check` 全部通过；Linux/amd64 的 doctor、CLI、manifest
test binary 与 `cmd/dot` 交叉编译通过；checkpoint 与修复区间 diff check 均通过。三路独立
只读复核均为 GO，主线程未接受任何恶意环境加固、M2/M3 扩展或低收益复杂化建议。

远端状态仍是待验收：`origin/main@f2362fa` 的既有 macOS/Linux run 成功，但精确修复 HEAD
没有 Actions run，且本 Goal 不授权 push。因此交付状态只能标记为“本地验收通过、远端待验收”，
不能宣称 M1 Checkpoint 1 已完整通过。
