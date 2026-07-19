# feat/apply-scaffold：建立 scaffold 安全执行内核

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和
`Outcomes and Handoff`，并遵循 `.agent/PLANS.md`。

## Purpose / Big Picture

完成后，内部调用方可以执行 planner 已决定的 M1 scaffold 动作：S1a/S2 对当前记录保持
零 mutation，S1b 或 metadata 漂移只补录 state，S3 在 target 仍缺失时以 no-clobber 方式发布
完整 regular file；symlink→scaffold 只在旧 symlink 仍属于 dot 时原子替换，否则只释放
ownership。执行结果继续明确携带后续 runtime 应提交的成功或失败 `StateEffect`，但本分支
不写 state、不取锁，也不开放真实 `dot apply`。

## Scope / Non-goals

范围内：

- 复用 link executor 已建立的 control/target/ancestor/leaf Precond 和提交结果契约。
- 执行 scaffold 的 S1a–S3 非 force 语义：skip、state-only adopt 与安全创建。
- S3 在 target 父目录准备完整 bytes/mode，再以排他 no-clobber 操作发布；不原地写现有
  regular file 或与其 hard-link 的 inode。
- 执行 symlink→scaffold migration：仍 owned 的 symlink 经最终 Precond 后原子换成独立
  regular file；nonowned 或缺失 target 只释放 ownership，不触碰 target。
- 固定 scaffold→link 沿用既有 link L1/L2/no-record 语义，并明确拒绝 force 才允许的
  `FileReasonScaffoldRebuild`。

明确不做：

- 不实现 state builder/persistence、lock/runtime wiring、backup/force、prune、hooks、CLI 或
  任何公开真实 apply。
- 不改变 planner decision、ownership、state v1、路径身份、恢复或公开输出契约。
- 不引入第三方依赖、通用 filesystem abstraction 或后续 M2/M3 能力。

## Contract and Context

- `docs/02-architecture.md` §4–§6：executor 只消费自包含计划，按计划的成功/失败 state
  处置返回，不重新读取 manifest 或改变 decision。
- `docs/05-apply-engine.md` §3.3/§5–§7：S1a/S2 不重写 target，S1b 只补录；S3 新建不得覆盖
  并发对象；owned kind migration 必须只呈现完整旧/新对象。
- `docs/06-templates.md`：scaffold 是非所有权语义，既有普通文件及 hard-link 不能被原地改写；
  target 最终 bytes 与 ordinary permission bits 必须完整匹配 desired。
- `docs/05-apply-engine.md` §10、`docs/08-testing.md`：成功收敛后的第二次相同运行零 mutation，
  中断恢复只允许依赖实际 target 与 state 真相补录。
- `docs/09-roadmap.md`：本节点只形成 scaffold 安全提交能力，后续 runtime 才连接锁、顺序执行、
  部分成功记账和原子 persistence。

基线是 clean `main@f4522b03d22b6153c25cc44f4c5c847aae30fc0c` 创建的
`feat/apply-scaffold`。前置 `apply-link` 已建立 `internal/executor.ExecuteFile`、共享
`validatePrecondition`、no-clobber L1、state-only L2 和原子 L3；当前真实缺口是 executor 只接受
link action，尚不能消费 planner 已形成的 scaffold skip/adopt/create/kind-migration 分支。

## Progress

- [x] 2026-07-20：确认 worktree、Git 顶层和 branch 均为分配的 `feat/apply-scaffold`，HEAD 为
  `f4522b0` 且 clean；读取规则、规范、completed link plan 与 planner/executor 实现。
- [x] 2026-07-20：以 `1b3583b` 提交本 active ExecPlan 起点。
- [x] 2026-07-20：测试先固定 S1a–S3、state-only adopt、完整发布与 no-clobber；随后完成
  scaffold kind dispatch、三次 Precond 复核、同目录完整临时文件和排他 hard-link 发布。
- [x] 2026-07-20：测试先固定 owned/nonowned symlink→scaffold、scaffold→link no-record 与
  hard-link sibling 隔离；随后完成完整临时 regular file 加 rename 的 owned migration，其他
  release 分支保持 state-only。
- [x] 2026-07-20：executor/planner/paths 窄测、executor 20 次重复、race、Linux/amd64 test
  binary 交叉编译、基线 diff check 与完整 `make check` 通过；更新 handoff，计划保持 active
  等待独立复核。

## Milestones

### Milestone 1：执行安全 scaffold 创建、skip 与补录

先增加真实文件系统测试，覆盖 S1a/S2 current skip、S1b 与 metadata 漂移 state-only adopt、S3
缺失祖先创建、完整 bytes/mode、并发新对象不覆盖，以及显式拒绝 `ScaffoldRebuild`。随后扩展
executor 的 desired-kind dispatch：所有 scaffold 动作先验证封闭 action 形态并复核现势
Precond；skip/adopt 不创建目录、不触碰 target；S3 在 target 父目录准备并关闭完整 regular
file，经第二次 Precond 后使用同目录排他发布操作，提交前失败返回 `OnFailure`，提交后 cleanup
错误仍返回 `OnSuccess`。

Concrete steps：

    go test ./internal/executor -run 'TestExecuteScaffold_(Skip|Adopt|Create|RejectRebuild)'

验收：

- S1a/S2 current 分支 target 和路径集合不变；S1b/metadata 分支只返回计划 upsert effect。
- S3 成功只发布完整 bytes/mode，最终提交点出现并发对象时保留该对象并返回失败 effect。
- `FileReasonScaffoldRebuild` fail closed；没有 force、backup 或隐式 fallback。

Commit 边界：

    feat(executor): 安全创建与补录 scaffold

### Milestone 2：执行 kind migration 并证明 inode 隔离

先增加 migration 与 hard-link 测试：owned symlink→scaffold 成功只从完整旧 symlink 切换为完整
独立 regular file，prepare/最终 Precond/rename 失败保留旧链；nonowned 或 missing migration 只
释放 ownership；已有 scaffold target 即使与 sibling hard-link 也不改 bytes、mode 或 inode；
scaffold→link 继续走既有 L1/L2/no-record planner 分支。实现只为
`FileReasonOwnedLinkToScaffold` 使用同目录完整临时 regular file 加 rename，其他 release 分支沿用
state-only adopt。

Concrete steps：

    go test ./internal/executor -run 'TestExecuteScaffold_(Migration|HardLinkIsolation)'

验收：

- owned symlink 仅在最后 Precond 仍成立时原子替换，成功 target 是独立 regular file。
- nonowned/missing release ownership 以及 S1a/S1b/S2 都不原地修改已有 inode。
- scaffold→link 不复制 migration 逻辑，继续满足 link executor 的 no-clobber/no-record 契约。

Commit 边界：

    feat(executor): 原子迁移 scaffold kind

## Validation and Acceptance

最终从分配 worktree root 运行：

    go test ./internal/executor ./internal/planner ./internal/paths
    go test ./internal/executor -count=20
    go test -race ./internal/executor ./internal/planner ./internal/paths
    git diff f4522b03d22b6153c25cc44f4c5c847aae30fc0c...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp4-scaffold-check

成功判据是所有命令退出 0，完整 diff 只包含本计划与 scaffold executor/test，worktree clean。
当前原生平台为 Darwin/arm64；远端 macOS/Linux CI 未运行时只报告“本地验收通过、远端待验收”。

## Safety, Authorization, and Recovery

测试只操作 `t.TempDir()` 内的合成 HOME/repo/config/state/binary/target，不读取或修改真实
`modules/`、machine config、state、backup、`.env` 或主力 HOME。失败保留最近成功 commit，以新
commit 修复，不 amend、rebase、cherry-pick、squash、reset、force，也不操作 main、coordinator、
其他 worktree 或 branch。若必须改变 planner/state 持久契约、ownership，或无法证明 no-clobber/
完整发布/hard-link 隔离，立即停止并请求裁决。

## Interfaces and Dependencies

不新增依赖。scaffold 继续消费 `planner.FileAction` 和可信 `paths.ControlPlanePaths`，复用
`FileResult`、`validatePrecondition` 与 link 已建立的错误分类；只在 target 父目录引入窄的
temporary regular file/no-clobber publish 操作接缝。后续 runtime 负责顺序执行、聚合成功 effect
与持久化；executor 不导入 state/runtime/CLI，不形成依赖环。

## Surprises & Discoveries

- Observation: Go 标准库没有跨 macOS/Linux 的 no-replace regular-file rename。
  Evidence: `os.Rename` 按 POSIX 语义会覆盖既有 non-directory target；直接
  `OpenFile(O_CREATE|O_EXCL)` 又会在写入期间暴露不完整最终文件。
  Impact: S3 使用同目录已完成临时文件加 `os.Link` 作为排他发布点，随后立即 unlink 临时名字；
  owned symlink migration 仍使用可替换旧 owned 对象的 rename。

- Observation: 初版临时文件 helper 的显式 error return 会在 defer 前把命名 path 结果覆写为空。
  Evidence: 让 `CreateTemp` 返回已关闭文件后，write 失败路径尝试清理空路径并遗留已创建临时
  文件。
  Impact: 所有创建后失败改用 bare return 保留本次临时路径，新增回归证明 prepare 失败会删除
  临时文件；提交后 cleanup 失败仍明确返回成功 effect，不能伪装成未提交。

## Decision Log

- Decision: S3 先在 target 父目录准备完整 regular file，再以同目录排他操作发布；不直接用
  `O_EXCL` 打开最终 target 后逐步写入。
  Rationale: `O_EXCL` 虽能 no-clobber，却会在写入期间暴露不完整 target；规范要求任意中断点
  target 只有完整旧对象或完整新对象。
  Date: 2026-07-20

- Decision: owned symlink→scaffold 使用完整临时 regular file 加同文件系统 rename，release
  ownership 与 ordinary scaffold 收养保持 state-only。
  Rationale: 只有 owned symlink 允许替换；其余 scaffold target 均不属于 dot，不能原地写入或
  借 migration 取得所有权。
  Date: 2026-07-20

## Outcomes and Handoff

Milestone 已达到 review-ready。branch 基线为 `f4522b03d22b`，已形成以下 commits：

    1b3583b docs(executor): 建立 apply scaffold 执行计划
    2eaf2f5 feat(executor): 安全创建与补录 scaffold
    a237c89 feat(executor): 原子迁移 scaffold kind
    07d176a fix(executor): 清理失败的 scaffold 临时文件
    2495105 fix(executor): 检查故障注入文件关闭

`internal/executor.ExecuteFile` 现可消费 scaffold 的 S1a/S2 skip、S1b/metadata state-only adopt、
S3 no-clobber create，以及 symlink↔scaffold migration。S3 在 target 父目录写完、chmod、sync、
close 临时 regular file，最终 Precond 后以 `os.Link` 排他发布并立即删除临时名字；若目标在提交点
出现则保留用户对象。owned symlink→scaffold 使用同目录完整临时 regular file 加 rename，
rename 前失败保留旧链；nonowned/missing release ownership 不触碰 target。已有 regular file 即使
与 sibling hard-link，skip/adopt 也不改变 inode、bytes 或 mode；显式 force
`FileReasonScaffoldRebuild` fail closed。

本地证据：

    go test ./internal/executor ./internal/planner ./internal/paths
    go test ./internal/executor -count=20
    go test -race ./internal/executor ./internal/planner ./internal/paths
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-m1-cp4-scaffold-linux-amd64.test ./internal/executor
    git diff f4522b03d22b6153c25cc44f4c5c847aae30fc0c...HEAD --check
    make check BINARY=/private/tmp/dot-m1-cp4-scaffold-check

全部退出 0；`make check` 包含 tidy/fmt check、lint 0 issues、全仓 race、build 与隔离
doctor-manifest。没有新增依赖、planner/state/CLI/规范改动，也没有实现 persistence、lock/runtime、
backup/force/prune/hooks。当前原生平台为 Darwin/arm64；Linux 只完成交叉编译，远端 macOS/Linux
CI 未运行：本地验收通过、远端待验收。

后续 runtime 必须保留与 link L3 相同的提交点契约：S3 hard-link 发布成功后即使临时名字 cleanup
报错，`ExecuteFile` 仍返回 `TargetMutated=true` 与 `OnSuccess` effect；调用方必须先记账再报告
cleanup 错误。scaffold state 永不构成 target ownership，runtime 不得因 upsert scaffold 记录而把
既有 regular/hard-linked target 当作可重写对象。
