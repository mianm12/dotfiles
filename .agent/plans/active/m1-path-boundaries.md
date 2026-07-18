# feat/path-boundaries：M1 控制面与完整 profile 路径边界

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成本 Goal 后，dot 将有一个共享、只读、fail-closed 的全局路径校验入口：它先解析本次
运行的 repo、机器配置、state 家族和已安装 binary，再用 `internal/paths` 已有的 target
identity 与 traversal topology 判断控制面家族是否隔离；对一个完整 effective profile，
同一入口同时拒绝 target 身份碰撞、逻辑祖先冲突、中间目录穿文件以及 desired target 与
控制面重叠。

这里的 effective profile 是顶层 profile 经引用展开、当前 GOOS 过滤和模块 defaults 合并后
的完整模块集合；“完整”意味着部分 apply、单条 planner 请求或未来 add 候选只能在全局路径
校验成功后缩小动作范围，不能先裁剪 desired 再校验。完成后，未来的 doctor、state、planner
和 add 可以传入各自的可信路径来源并复用同一 identity、拓扑、控制面成员和例外定义，无需
各自维护保留路径列表。

可直接观察的结果是：在 `t.TempDir()` 构造的 macOS/Linux 文件系统拓扑中，大小写或 Unicode
别名、祖先 symlink、递归 symlink target 和 `..` 折返中间项均不能绕过碰撞；不同 leaf hard
link 仍是不同 target；任一 identity 无法可靠建立时整次边界校验失败，且不会创建 state、
lock、目录或其他文件。

## Scope / Non-goals

范围内：

- 从 effective HOME 和已严格解析的 repo/config 输入构造绝对、与 cwd 无关的控制面路径：
  repo tree、config file、state root、`state.json`、`lock`、`backup/` 和已安装 binary。
- 在一个共享定义中表达 repo、config、state、binary 四个控制面家族；state root 与其预定
  `state.json`、`lock`、`backup/` 子路径之间的包含关系是唯一允许的家族内部例外。
- 复用 `internal/paths` 的唯一 identity/topology 语义对不同控制面家族做两两相等、双向严格
  祖先和文件系统别名重叠校验；现有 `ResolveTarget` / `TargetResolution` 是否足以表达
  control path 的消费语义先经过实施 gate，不足时不得由 boundary 自行补算法。
- 对完整 effective profile 的结构性 desired entries 校验 target identity 唯一、双向祖先
  冲突和展示路径实际遍历的中间目录项穿文件，并保留冲突双方的 module/source/path 来源。
- 校验每个完整-profile desired entry target 与任一控制面家族的双向重叠。
- 建立跨消费者共享的最小接口层次，使 manifest/doctor、state、planner 和未来 add 复用同一
  path relation 与控制面定义；对完整 profile 的入口必须在任何作用域裁剪之前运行。
- `internal/paths` 与 `internal/manifest` 的纯规则及真实文件系统测试、macOS/Linux CI、完整
  `make check`、完整 diff 检查、独立只读复核和 ExecPlan 收口。

明确不做：

- execute 或提交时 Precond、任何 target/source/state mutation、锁获取、state 加载/解码/
  持久化、ownership、planner 动作决策、prune、backup 写入或 hard-link 内容 mutation 隔离。
- `dot add` 输入解析或建账、完整 `dot doctor` 命令、state consumer、apply planner 或其他
  尚未存在的命令接线；本 Goal 只交付这些消费者可复用的边界与完整-profile 入口。
- 自动创建 HOME、repo、config、state、backup、binary parent 或 target 中间目录；校验保持
  只读。
- 完整 ancestor-chain mutation 可达性校验；05 号文档 §5 第 5 项留给消费具体 mutation 的
  planner/execute 切片。本 Goal 只校验完整 profile 的结构关系与控制面重叠。
- 恶意仓库、恶意 hook、主动并发改变文件系统或其他威胁模型外环境的额外防御。
- Windows、M2 managed/rendered、state rebuild、完整 doctor 或其他后续里程碑能力。
- 为 doctor、state、planner、add 分别建立路径列表、state 特例或字符串 fallback；同一性质
  只能由共享语义入口表达一次。

## Contract and Context

- `docs/02-architecture.md` §2：控制面路径必须按本次 effective 参数解析为绝对、与 cwd 无关
  的路径；repo/config/state/binary 家族彼此不得相等、互为祖先或以文件系统别名重叠；state
  家族内部预定包含是唯一例外。desired/state/add 输入与任一控制面家族双向重叠都必须拒绝。
- `docs/02-architecture.md` §4：apply 在进入 lock 前完成控制面家族校验；pipeline ⑤形成完整
  profile 的结构性 desired，pipeline ⑨才允许对请求作用域做动作层消费。
- `docs/03-manifest-spec.md` §6：manifest target 的词法合法域已由 manifest 层负责；文件系统
  identity、祖先/别名重叠和控制面保留必须复用路径边界，不能以词法前缀代替。
- `docs/03-manifest-spec.md` §7：`doctor --manifest-only` 未来逐 profile 复用全局 target 不变量，
  profile 之间不合并；完整 doctor 才能覆盖机器配置里的 repo override。本 Goal 不实现命令，
  但共享入口不得迫使 doctor 读取 state 或渲染模板。
- `docs/05-apply-engine.md` §5：完整 effective profile 的结构性 desired 必须在 execute 前整体
  满足 target identity 唯一、逻辑祖先、中间目录项和控制面隔离；部分 apply 与 add 候选不得
  缩小这些集合。
- `docs/08-testing.md` §1–§4：doctor 与 mutation 复用同一边界校验；测试必须覆盖 case/
  Unicode、祖先 symlink、完整 traversal trace、控制面隔离和部分 apply 反例，并隔离 HOME、
  repo、config、state 与 backup；macOS/Linux 都运行 `make check`。
- `docs/09-roadmap.md` §1 M1：文件系统 target identity、祖先 topology、完整 profile 全局校验
  和控制面家族隔离属于 M1 安全内核；不得为 M2/M3 预建能力。

当前基线为 2026-07-18 的本地 `main@8075a6c`，其中 `b266cb4` 已合并
`feat/path-identity`。`internal/paths/identity.go` 与 `resolution.go` 提供不透明
`TargetIdentity`、`TargetResolution`、`ResolveTarget`、`Equal` 和 `IsAncestorOf`；resolution
同时记录 canonical 目录链与展示路径实际经过的 symlink traversal trace。leaf symlink 不
跟随，但作为另一条路径的中间项时进入祖先 trace；leaf hard link 不按 inode 合并；无法
权威解释 missing name 时返回 `ErrIdentityUnavailable`。后续边界不得窥探私有 identity 字段，
不得从展示字符串重建关系，也不得在收到 identity error 后降级。

`internal/paths/paths.go` 当前只解析 effective HOME、config 和 repo；尚无 state 家族、安装
binary 或控制面集合类型。`internal/manifest/repository.go` 严格加载所有 manifest，
`Repository.Resolve` 产生字段私有的 `ResolvedProfile`，`ResolvedProfile.Enumerate` 会先调用
私有 `enumerateStructure` 构造完整结构，再渲染全部 scaffold。边界校验只需要结构性
`DesiredEntry.TargetPath`，不能为了 doctor 或部分 apply 被迫渲染模板；因此需要一个受控的
结构枚举/全局校验接缝，但不能把 `ResolvedProfile` 的私有模块集合复制成第二份真相源。

### 已知实施 gate：control path 尚不能完全由 target identity 表达

审计已确认两个相关缺口。第一，`paths.ResolveControlPath` 接受任何非空绝对路径，包括
filesystem root `/`；规范没有排除 repo 或 `DOT_CONFIG` 指向 root。与此同时，
`cleanTargetPath` / `ResolveTarget` 明确拒绝 filesystem root，因为既有 `TargetIdentity` 表示
leaf 目录项位置。第二，target identity 按设计不跟随 leaf symlink；但 repo tree、config
file、state root/member 与 installed binary 是按各自 IO 语义被消费的控制面路径。比如有效
repo 路径本身是指向真实 repo 目录的 symlink 时，直接落在真实 repo tree 内的 desired 仍应
属于文件系统 alias overlap；config/binary 的 leaf symlink 也可能让其目标文件成为实际消费
对象。当前 `TargetResolution.Equal` / `IsAncestorOf` 只表达 target leaf 位置及其进入 leaf
之前的 traversal，不能自行证明这些 control-leaf consumption 关系。

这意味着当前 identity API 不仅无法表达 root，也未证明足以覆盖全部控制面 alias/ancestor
关系。具体应扩展 identity 层的哪一种只读 resolution、不同 control member 是否有不同的 leaf
消费语义，属于前置 path-identity 设计/规范裁决；本计划不能预先发明答案。

因此本 Goal 执行时必须先停在 Milestone 1 的 capability gate：记录可复现测试和影响，向
维护者请求对 path-identity 前置修复或规范取舍的裁决。在 identity 层能够表达这些性质之前，
不得开始 boundary relation 实现，不得在 boundary 层用 `path == "/"`、`filepath.Rel`、
`EvalSymlinks`、字符串前缀、leaf-symlink 特判或“root 与一切重叠”特判伪造结果。若裁决要求
修改规范或扩大本 Goal 到 identity 语义，
先更新本计划的 Scope、Decision Log 和授权状态，再继续；本计划后续 milestone 描述 gate
解除后的实施路径，不代表已经获得该扩展授权。

## Progress

- [x] 2026-07-18：确认工作区为干净的本地 `main@8075a6c`，且
  `feat/path-identity` 已在 `b266cb4` 合入 main；读取 `AGENTS.md`、`CONTRIBUTING.md`、
  `README.md`、`.agent/PLANS.md`、计划生命周期规则、指定规范、路线图、Makefile/CI，以及
  manifest、desired、control path 和 path identity 的当前实现与测试。
- [x] 2026-07-18：发现 control path 接受 filesystem root 而 `ResolveTarget` 无法表达 root，
  且 control leaf symlink 的实际消费对象不由现有 target-leaf resolution 表达；已记录为实施
  前停止条件，未增加字符串/`EvalSymlinks` fallback，未修改生产代码。
- [x] 2026-07-18：完成本 ExecPlan 草案并保存到 `active/`；本次只规划，未创建分支、stage、
  commit 或执行实现测试。
- [x] 2026-07-18：完成草案独立只读复核；将 control leaf-symlink consumption 与 root 一并
  纳入 identity capability gate，并补齐 `.tmpl`、profile 分离和完整无 pathspec diff 验收。
- [x] 2026-07-18：从干净 `main@8075a6c` 创建并切换到 `feat/path-boundaries`，只 stage 本计划，
  以 `4f05174 docs(paths): 新建 path boundaries ExecPlan` 提交计划起点；提交后工作区 clean。
- [x] 2026-07-18：执行 Milestone 1 capability gate 的现有回归：
  `go test -count=1 ./internal/paths -run
  'Test(ResolveControlPath|ResolveTarget_LeafSymlinkIsNotFollowed|ResolveTargetIdentity_BasicRejectsInvalidPath)'`
  通过，确认 control path 接受绝对输入、target root 被拒绝且 leaf symlink 不跟随的当前契约。
- [ ] Milestone 1：已复现 identity capability gate；等待维护者裁决是先独立扩展 path identity，
  还是明确 control root/leaf 的规范语义。裁决前不得实现 boundary fallback；gate 解除后再建立
  控制面路径家族。
- [ ] Milestone 2：以共享 identity/topology 语义校验控制面家族两两隔离。
- [ ] Milestone 3：校验完整 profile 的 target identity 与祖先拓扑不变量。
- [ ] Milestone 4：合并 desired/control-plane 校验并证明部分作用域无法绕过完整 profile。
- [ ] Milestone 5：完成 macOS/Linux 门禁、完整 diff、独立复核和计划收口。

## Execution Start and Commit Discipline

未来获得明确实施与 Git 授权后，先从 repo root 重新检查 `git status --short --branch`、最新
main、`b266cb4` 的祖先关系和本计划仍是唯一相关 untracked/working-tree 内容；若 main 或
工作区已有新改动，先按 `AGENTS.md` 判断是否能安全隔离，不覆盖或带入任务外内容。随后创建
并切换到 `feat/path-boundaries`，只 stage 本计划，以
`docs(paths): 新建 path boundaries ExecPlan` 建立 active plan 起点。branch 已存在或计划已被
他人提交时不得重建、覆盖或重复提交，应先核对 provenance 和 `Progress`。

每个 milestone 都遵循：确认前一 checkpoint clean → 先增加暴露缺口的测试 → 最小实现 →
窄测/重复测试 → `make check` → 检查从该 milestone 起点到当前的完整 diff、相关 untracked 和
`git diff --check` → 更新本计划 → 只 stage 本 milestone → semantic commit → 再次确认 clean。
前一 milestone 未形成可独立验证的成功 commit 前，不进入下一项；review 修复使用新 commit，
不 amend、squash 或改写已完成 checkpoint。

## Milestones

### Milestone 1：解除 identity capability gate并建立控制面家族值

本 milestone 先用 `internal/paths` 测试复现 root control path 以及 control leaf symlink 消费
语义与 `ResolveTarget` 的能力差距，然后暂停请求裁决；这是执行 gate，不允许以 boundary 特判
继续。只有前置 path-identity 修复
已经由独立任务合入 main，或维护者明确授权在更新后的本 Goal 中修复且相应规范/计划已对齐，
才能恢复本 milestone。恢复后，在 `internal/paths/paths.go` 附近集中构造本次运行的绝对控制
面路径：repo 与 config 继续复用现有选择优先级；state root 固定由 effective HOME 派生，并
同时产生 `state.json`、`lock` 和 `backup` 预定成员；installed binary 按规范固定安装位置由
同一 effective HOME 派生。构造过程只做词法解析和结构组装，不读取 state，不创建目录，不
调用 Git，也不以当前 cwd 或当前进程的临时可执行路径替代“已安装 binary”。

控制面成员及其 family 归属必须在一个不可由消费者任意拼装例外的值中表达。state family
成员只在这个构造边界声明一次；doctor/state/planner/add 不再各自列举 `state.json`、`lock`、
`backup`。保持现有 `EffectiveHome`、`Config`、`Repository` 的来源优先级和非法选中值不
fallback 行为。若恢复实施时规范已经明确 installed binary 的另一来源，以规范为准并记录
Decision Log，不能猜测 `os.Executable()`。

预计修改位置：`internal/paths/paths.go` 及同 package 的窄职责新文件、
`internal/paths/paths_test.go`；若 identity 前置修复另有 commit，本 milestone 只在已合入基线
上消费，不把其实现复制进 boundary。

Concrete steps：

    在 repo root 运行：
      go test -count=1 ./internal/paths -run 'Test(ControlPathIdentityCapability|ResolveControlPath_Root|ControlPlanePaths|StateFamily|InstalledBinary)'
    capability gate 的初始预期：root、repo-directory leaf symlink，以及 config/binary/
      state-member file leaf-symlink fixture 证明现有 target resolution 无法完整回答 control
      overlap，更新 Progress 后暂停；不得产生 boundary fallback commit。
    gate 解除后的预期：repo/config/state root/state.json/lock/backup/binary 都是绝对、clean、
      与 cwd 无关的展示路径；非法显式输入报错且不会回退；fixture 无新增目录项。

验收：

- root 与 control leaf symlink 的实际消费关系由 identity 单一语义源可靠表达，或已有明确
  规范裁决；boundary package 内无 root、leaf-symlink、`EvalSymlinks`、字符串 prefix、
  `filepath.Rel` 或 Unicode/case fallback。
- repo symlink 指向真实 tree、config/binary/state member symlink 指向真实 file 时，control 与
  直接写成真实目标路径的其他 control/desired 重叠都能被 identity 层表达；不能以拒绝所有
  control symlink 作为降级。
- 默认 state root 为 effective HOME 下的 `.local/state/dot`，预定成员为其直接约定位置；
  `--home` 改变整组默认路径，repo/config 的显式优先级仍与既有测试一致。
- installed binary 是规范安装位置而非测试进程或 `go test` 临时 executable；所有构造只读。
- state family 的成员和唯一内部包含例外只有一个声明位置。

Commit 边界（gate 解除后）：

    feat(paths): 建立控制面路径家族

本 milestone 的特有停止条件：identity 仍不能表达 root、control leaf symlink 的实际消费对象
或其他规范允许的控制路径；必须改变 path identity 公共语义、规范接受集合或已完成 Goal；
installed binary 来源存在规范歧义；
只能通过字符串 fallback 继续。命中任一条件时更新本计划并暂停，不创建上述 feature commit。

### Milestone 2：校验控制面家族两两隔离

在 gate 解除且控制面值可构造后，增加一个共享、批量、只读的控制面校验入口。它为每个成员
调用 identity 层在 gate 解除后提供的权威 resolution/relation 语义，并对不同 family 的每对
成员检查位置或实际消费对象相等、left 是 right 的严格祖先、right 是 left 的严格祖先，以及
完整 traversal 暴露的 symlink alias 重叠。target-leaf 与 control-leaf 关系如何组合由已复核
的 identity 层契约决定，boundary 只消费结果。state root 与 `state.json`/`lock`/`backup` 的
预定包含关系只因它们属于同一个 state
family 而允许；不得按路径文字或调用方名称跳过比较。state family 内部意外的非预定相等/
别名关系仍应 fail closed，不能把“同 family”解释成任意冲突均可接受。

错误必须标明两个 family/member 的角色和原始展示路径，让未来 doctor 能诊断、mutation 能
拒绝；错误文本格式可以由实现决定，但测试至少固定关系类别和双方 provenance。任一
`ErrIdentityUnavailable`、`ErrPathBlocked`、权限或普通 IO 错误原样进入整体失败链，不能
跳过该成员继续。校验不读取 manifest 或 state 内容。

预计修改位置：`internal/paths` 中与 path identity 相邻的边界实现和测试；target/target 继续
复用 `TargetResolution.Equal` / `IsAncestorOf`，control relation 复用 gate 解除后确立的
identity 层 API，均不读取私有 identity 字段。

Concrete steps：

    在 repo root 运行：
      go test -count=1 ./internal/paths -run 'TestControlPlane(Isolation|StateFamily|Alias|Blocked|ReadOnly)'
      go test -count=10 ./internal/paths -run 'TestControlPlane(Isolation|Alias)'
    预期：不同 family 的相等、双向祖先和 symlink alias 全部拒绝；仅预定 state 包含通过；
      identity 不可用或 IO 错误 fail closed；重复测试稳定且无写入。

验收：

- repo/config/state/binary 四个 family 的全 pair matrix 有测试；交换输入顺序不改变结论。
- 覆盖 repo 包含 config、config 包含 repo、binary 位于 repo、repo 经 ancestor symlink 指向
  state、config 与 binary 为同一 leaf alias，以及无冲突基线。
- state root 包含预定三成员通过；成员经别名变成其他 member 或其他 family 时拒绝，唯一例外
  不扩散到消费者。
- Linux 对无法权威解析的 missing name 返回整体失败，不改用 byte/string equality；macOS
  大小写/Unicode 行为以真实 lookup oracle 为准。

Commit 边界：

    feat(paths): 校验控制面家族隔离

### Milestone 3：校验完整 profile 的 target identity 与祖先拓扑

在共享边界语义中加入带 provenance 的 target 集校验，并在 manifest 侧建立不渲染模板的
完整结构接缝。结构来源必须仍是 `ResolvedProfile` 内部同一份 resolved modules 与
`enumerateStructure` 规则，不能复制 source 枚举、ignore、kind、suffix 或 target override
逻辑。必要时最小拆分 `Enumerate`，让运行时渲染和结构性边界消费共享结构枚举；既有
`Enumerate` 的排序、深拷贝、fail-fast 和模板行为保持不变。

对一个完整 profile 的每个 `DesiredEntry.TargetPath` 解析 `TargetResolution`，然后检查：
同一 leaf identity 只出现一次；任意两 target 不能形成双向严格祖先；任一 desired file leaf
不能出现在另一 target 的完整 traversal ancestors 中。最后一条必须覆盖目录 symlink `A`
作为 leaf 与 `A/child`、`A -> B -> real` 的中间 `B`、以及 link target 中先经过后被 `..`
折返的目录项；同时不能把 `A` leaf 错判为直接写成 `real/child` 的祖先。不同名称的 leaf
hard link 保持允许，因为它们是不同 target identity。

校验失败报告冲突双方 module、source、`Target` 展示值与绝对 `TargetPath`。任一 entry 无法
建立 identity 时整份 profile 失败并返回 nil/无 validated 结果，防止下游误用部分集合。

预计修改位置：`internal/paths` 的共享 target-set validator 及测试，
`internal/manifest/desired.go` 与 `desired_test.go` 的完整结构接缝和适配测试。不得让
`internal/paths` 反向依赖 manifest；paths 接收最小的带标签绝对路径输入，manifest 负责把
完整 `ResolvedProfile` 映射为该输入。

Concrete steps：

    在 repo root 运行：
      go test -count=1 ./internal/paths -run 'TestTargetSet(Identity|Ancestor|Traversal|HardLink|ReadOnly)'
      go test -count=1 ./internal/manifest -run 'TestResolvedProfile(Structure|PathValidation)'
      go test -count=20 ./internal/paths -run 'TestTargetSet(Identity|Ancestor|Traversal)'
    预期：所有碰撞在返回 validated 集合前整体失败；合法 siblings、字符串前缀反例和 leaf
      hard links 通过；manifest 结构路径不渲染 scaffold、不读取 data、不写 target。

验收：

- 大小写/Unicode/祖先 symlink 别名、跨模块相同 target、`.template` 以及显式声明为 scaffold
  的 `.tmpl` suffix 去除碰撞、`[files].target` 碰撞都由 identity 入口拒绝，并报告双方来源。
- `foo` 与 `foo/child`、symlink `A` 与 `A/child`、recursive/`..` traversal 中间项均拒绝；
  `foo`/`foobar`、`A` leaf/直接 `real/child` 与不同 leaf hard links 不产生假阳性。
- 完整结构枚举与既有 `Enumerate` 共享 source/target 规则和稳定顺序；边界校验不依赖模板
  render、RuntimeContext.Data 或机器 state。
- identity/blocker/IO 错误 fail closed；没有部分可信结果或字符串 fallback。

Commit 边界：

    feat(paths): 校验完整 profile target 拓扑

### Milestone 4：合并 desired/control-plane 并锁定完整-profile 入口

建立供后续消费者调用的唯一全局入口：输入是已经严格解析的控制面家族和一个完整
`ResolvedProfile`/等价不可伪造的完整结构来源，输出只在所有控制面与 desired 不变量通过后
返回可供后续 planner 按请求 scope 筛选的结构性 desired。入口顺序为：完整 profile 结构枚举
→ 控制面家族隔离 → 完整 target-set 关系 → 每个 desired 与每个控制面 member 的双向
identity/ancestor/traversal overlap；任一步失败都不返回可执行子集。

部分作用域测试必须构造至少两个模块：请求模块自身无冲突，未请求模块与其形成 identity 或
祖先冲突，或未请求模块落入 repo/state/binary。测试先证明只看请求模块会通过，再通过正式
入口证明整份 profile 拒绝，且没有“scope 参数传入 validator”或“validator 只接收已筛选
slice”的生产调用路径。未来 planner 只能对成功返回的 validated full-profile 结果做动作
筛选；未来 add 则把候选与完整 profile 一起交给同一 relation engine，不能获得新的例外表。

本 milestone 不实现 doctor/state/planner/add 命令，但增加 compile-time/API-level 测试或
package 测试说明四类消费者如何复用：doctor 逐 profile 调完整入口；state 将可信 state target
映射为同一 labeled target 关系；planner 从 validated full profile 再选 scope；add 将 candidate
加入同一完整集合。任何 consumer-specific policy 只能决定“何时调用/输入是否可信”，不能
改变 family 成员、target equality、ancestor 或 overlap 语义。

预计修改位置：`internal/manifest` 与 `internal/paths` 的最小跨 package 接缝及测试。若需要
新增小型 package 解除依赖方向，先证明它只有真实跨消费者职责，且不复制 manifest 或 path
identity 逻辑；不得为尚不存在的 consumer 预建 interface 层级。

Concrete steps：

    在 repo root 运行：
      go test -count=1 ./internal/paths ./internal/manifest -run 'Test(GlobalPathValidation|DesiredControlOverlap|FullProfile|PartialScope)'
      go test -count=10 ./internal/paths ./internal/manifest -run 'Test(GlobalPathValidation|PartialScope)'
    预期：desired 位于 control 内、等于 control、作为 control 祖先、经 symlink alias overlap
      均整体拒绝；未请求模块的冲突仍阻止正式入口返回结果；合法完整 profile 稳定通过且只读。

验收：

- desired 与 repo tree、config file、state root/三个预定 member、binary 的双向 overlap matrix
  全部覆盖，包含真实 symlink alias 与平台名称别名。
- 控制面先自身隔离；不能通过让两个 control family 先冲突再遗漏 desired 检查而得到部分成功。
- 部分 apply 的请求 scope 不进入全局 validator；至少一个未请求模块碰撞和一个未请求模块
  控制面重叠回归证明完整 profile 不变量不能绕过。
- 两个 profile 各自校验均合法、只有把它们合并才会碰撞的 fixture 必须分别通过，证明共享
  入口按 effective profile 逐个校验，不把互斥 profile 合并成一个 target 集。
- 共享实现中没有 doctor/state/planner/add 名称分支或各自例外列表；state family 例外仍只在
  控制面定义处出现一次。
- `ResolvedProfile.Enumerate` 的既有公开行为和测试保持通过；新增入口不读取 state、不渲染
  非必要模板、不 mutation。

Commit 边界：

    test(paths): 固定完整 profile 边界复用

若实现共享接缝本身包含独立生产行为，应将其与测试放入语义匹配的 `feat(paths): ...` commit，
并把上面的 test commit 留给部分作用域与消费者复用回归；不得把未解释的大改动塞入 test
commit。执行时在 Decision Log 记录最终 commit 拆分。

### Milestone 5：双平台门禁、独立复核与收口

在本机运行窄测、重复测试、darwin/linux 交叉编译和完整 `make check`，检查从 branch base
到 HEAD 的完整 diff、相关 untracked、commit 边界与计划 living sections。随后由未参与实现
的只读 subagent 复核所有实质改动，重点检查：identity 单一语义源、root gate 的解决方式、
state family 唯一例外、full-profile-before-scope、symlink traversal、hard-link leaf、错误
fail-closed、只读性和 Non-goals。主 agent 判断意见；实质问题用新的 fix commit 修复并重跑
相关窄测、`make check` 和必要的完整复核，不 amend 已完成 milestone。

真实 macOS 与 Linux 结果由现有 GitHub Actions matrix 的 `make check` 提供。交叉编译只证明
目标平台可构建，不能替代目标 runner 的 lint/测试；如果当前实施授权不含 push/PR，则计划
保持 `active/` 并明确记录双平台 CI 未验证，不得声称 review-ready 或迁移 completed。只有
所有门禁、独立复核、授权和生命周期条件满足后，才更新 Outcomes、把同一计划移入
`completed/` 并创建独立 plan-closure commit。

Concrete steps：

    在 repo root 运行：
      artifact_dir="$(mktemp -d)"
      go test -count=20 ./internal/paths ./internal/manifest
      GOOS=darwin GOARCH=amd64 go test -c -o "$artifact_dir/paths-darwin.test" ./internal/paths
      GOOS=linux GOARCH=amd64 go test -c -o "$artifact_dir/paths-linux.test" ./internal/paths
      GOOS=darwin GOARCH=amd64 go test -c -o "$artifact_dir/manifest-darwin.test" ./internal/manifest
      GOOS=linux GOARCH=amd64 go test -c -o "$artifact_dir/manifest-linux.test" ./internal/manifest
      make check BINARY="$artifact_dir/dot"
      git status --short --branch
      git log --oneline --decorate main..HEAD
      git diff --stat main...HEAD
      git diff main...HEAD
      git diff main...HEAD -- .agent/plans/active/m1-path-boundaries.md internal/paths internal/manifest go.mod go.sum
      git diff main...HEAD --check
    预期：本机所有命令退出 0，diff 只含本 Goal，未新增依赖或意外产物；CI 的 macOS/Linux
      `make check` 均通过；独立复核无未处理实质问题。

验收：

- 本机窄测、重复测试、交叉编译、`make check` 和完整 diff check 均有日期与结果记录。
- GitHub Actions `macos-latest`、`ubuntu-latest` 均通过同一 `make check`；未运行就明确标为
  未验证。
- 独立 subagent 只读复核全部实质 diff；意见和主线程处理结论写入本计划。
- final diff 不包含 state 加载、planner/mutation/add/doctor 命令、依赖变更或无关重构。
- 不带 pathspec 的 `git diff main...HEAD` 已逐文件检查；若实施新增小 package 或计划已迁移到
  `completed/`，它们也必须包含在完整内容复核与 closure staged diff 中，不能只依赖 `--stat`
  或预设 pathspec。
- closure 前 `Progress`、`Surprises & Discoveries`、`Decision Log`、
  `Outcomes and Handoff` 与真实状态一致，计划迁移/commit 获得当次任务授权。

Commit 边界：

    fix(paths): <仅在复核发现实质问题时使用准确摘要>
    docs(paths): 收口 path boundaries ExecPlan

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| control path 全部可由 identity 语义表达 | root 与 control leaf-symlink capability 回归、identity 前置修复/裁决证据 | 已发现缺口，等待 gate 解除 |
| repo/config/state/binary 集中解析 | `ControlPlanePaths`/等价测试，cwd 与 `--home` 反例 | 待验证 |
| state family 唯一预定包含例外 | family member matrix 与 alias 反例；源码复核无消费者例外 | 待验证 |
| 控制面家族两两隔离 | equal、双向 ancestor、symlink/case/Unicode alias 测试 | 待验证 |
| 完整 profile target identity 唯一 | 跨模块、suffix、override、平台 alias 碰撞测试 | 待验证 |
| 无祖先冲突和中间目录穿文件 | 普通 ancestor、`A`/`A/child`、recursive symlink、`..` trace 正反例 | 待验证 |
| leaf hard link 不误合并 | `os.SameFile` 为真但不同 target identity 的全局校验测试 | 待验证 |
| desired 与控制面双向隔离 | 四 family/所有 state member overlap matrix | 待验证 |
| 部分作用域不能绕过完整 profile | 未请求模块 identity 冲突与控制面重叠测试 | 待验证 |
| fail closed 且只读 | identity unavailable、blocked、权限/IO cause 和目录项快照测试 | 待验证 |
| 单一语义源 | 完整 diff 人工检查与独立复核，无 consumer-specific list/fallback | 待验证 |
| 双平台完整门禁 | macOS/Linux CI `make check`、本机重复测试与交叉编译 | 待验证 |

最终成功判据不是“新增某个类型”，而是所有非法 topology 在任何 consumer 获取可执行子集前
整体失败，合法 topology 在 macOS/Linux 都稳定通过，且实现中只有一个控制面成员/例外定义
和一个 identity/topology relation engine。

## Safety, Authorization, and Recovery

当前实施任务已明确授权创建/切换 `feat/path-boundaries`，并在该分支 stage、commit 本 Goal
的计划、实现、测试、复核修复和计划收口改动；已据此创建分支并提交计划起点。当前任务不
授权 merge、push、PR、rebase、amend、tag、删除分支或访问真实私人数据。若双平台 CI 必须
依赖 push/PR 才能执行，缺少相应授权时保持本计划在 `active/` 并报告未验证，不得以省略 CI
降低交付标准。本节记录本次任务证据，不为后续任务延续权限。

所有测试必须使用 `t.TempDir()` 下的合成 HOME、repo、config、state、backup、binary 和
targets；不得读取或修改真实 `modules/`、真实 machine config、state、backup、`.env*` 或
用户 HOME。测试只调用只读 path/manifest 入口，并在成功和失败场景前后比较目录树；构建
产物写入 `mktemp -d` 或已忽略 `bin/`。本 Goal 不运行 `dot apply/add/init/update`。

校验可安全重复：稳定拓扑下结果必须一致，无需清理或补偿。测试或实现中途失败时保留最近
通过的 semantic commit，更新 `Progress` 后修复并新增 commit；不使用 `git reset --hard`、
`git clean`、restore、amend 或其他会覆盖用户内容的命令。若文件系统 topology 在多次只读
解析间变化，本威胁模型不要求对抗主动竞态，但普通 IO/identity 错误必须 fail closed，不能
返回部分 validated 结果。

已知 root/control-leaf capability gate 未解除时，恢复动作只有记录证据并请求裁决；不得创建
临时字符串或 `EvalSymlinks` fallback。identity 前置修复若在另一 branch 完成，应先由人工
review 合入 main，再从更新的 main 继续本 Goal，避免在 boundary branch 复制两套身份语义。

## Interfaces and Dependencies

本 Goal 预期不新增第三方依赖，不修改持久化格式、`go.mod` 或 `go.sum`。若实施发现只有新
依赖才能权威表达文件系统身份，按停止条件暂停并说明维护、平台和替换成本；不得为了绕过
`ErrIdentityUnavailable` 引入近似 Unicode/case 库。

跨 package 协调只要求以下行为契约，具体私有类型和函数名由实现反馈决定：

- paths 层拥有 opaque control-plane value，集中保存 family/member provenance 和 state 的
  预定层级；调用方不能注入新的“允许重叠”标记。
- paths 层拥有对 labeled absolute paths 的 identity/topology relation engine；control/control、
  target/target 和 target/control 都复用它，不暴露 `TargetIdentity` 私有表示或持久化 key。
- manifest 层从一个 `ResolvedProfile` 的私有完整模块集合生成结构性 desired，并在返回给
  scope selector 前调用全局校验；结构枚举与 `Enumerate` 共用同一规则。
- validated 结果只在整组成功时产生；错误保留 path role、module/source 和冲突双方，未来
  doctor 可以诊断而 mutation 可以整体拒绝。
- state 与 add 后续只负责把已经按各自规范判定为可信的输入映射为 labeled path；它们不能
  定义 equality、ancestor、control members 或例外。state 何时加载、add 候选如何形成不在
  本 Goal。

## Surprises & Discoveries

- Observation: `ResolveControlPath` 接受 filesystem root，而 `ResolveTarget` 明确拒绝 root；
  target resolution 也按设计不跟随 leaf symlink，但 control path 的 IO 可能跟随 leaf 到实际
  repo/config/state/binary 对象。
  Evidence: `internal/paths/paths.go:ResolveControlPath` 的 absolute 分支、
  `internal/paths/identity.go:cleanTargetPath` 的 filesystem-root guard、
  `internal/paths/identity_topology_test.go:TestResolveTarget_LeafSymlinkIsNotFollowed`，以及
  `manifest.Load`/`config.Load` 对 repo/config 的跟随式 IO；2026-07-18 在
  `feat/path-boundaries` 运行相关现有回归通过。
  Impact: 当前 identity 未证明能覆盖全部规范允许的控制面输入与 filesystem alias；实施必须
  先暂停解除 capability gate，boundary 层不得添加 root/leaf-symlink/string fallback。
- Observation: 当前没有 state family、installed binary 或控制面集合实现。
  Evidence: `internal/paths/paths.go` 只定义 `EffectiveHome`、`ResolveControlPath`、`Config`
  和 `Repository`；仓库搜索无 state/binary path helper。
  Impact: 控制面成员与唯一例外应从第一次实现起集中建模，避免未来 consumer 各自复制。
- Observation: `ResolvedProfile.Enumerate` 在私有 `enumerateStructure` 后渲染完整 profile 的
  scaffold。
  Evidence: `internal/manifest/desired.go` 的 `Enumerate` 数据流。
  Impact: 全局路径校验需要最小结构接缝，不能为了 doctor 或部分作用域依赖模板 data/render；
  该接缝必须与现有枚举共享同一结构来源。
- Observation: 现有 `TargetResolution.IsAncestorOf` 已同时消费 canonical chain 与完整 symlink
  traversal trace，且明确区分 leaf symlink、真实目录和 leaf hard link。
  Evidence: `internal/paths/resolution.go` 及 identity topology/filesystem tests。
  Impact: boundary 应组合 `Equal`/双向 `IsAncestorOf`，不新写路径前缀、`EvalSymlinks` 或 inode
  判等。

## Decision Log

- Decision: 控制面成员、family 归属和 state 预定包含例外只由 paths 层的一个 opaque 值定义。
  Rationale: doctor/state/planner/add 只应映射输入和选择调用阶段；允许它们各自维护列表会让
  同一安全性质出现多个真相源。
  Date: 2026-07-18
- Decision: 所有 overlap 统一归约为 identity 层提供的权威 relation；target/target 使用
  `TargetResolution.Equal` 与任一方向的 `IsAncestorOf`，control leaf 的实际消费语义必须先在
  capability gate 中得到 identity 层契约，identity 错误整体 fail closed。
  Rationale: 现有 target-leaf 语义正确覆盖 desired，但 root 和 control leaf symlink 未必能由
  同一公开操作表达；把缺口留在 identity 层解决，避免 boundary 出现字符串、`EvalSymlinks`
  或 inode fallback。
  Date: 2026-07-18
- Decision: 完整 profile 的结构枚举与边界校验发生在任何请求 scope 裁剪之前；scope 只能
  消费成功的 validated 结果。
  Rationale: 05 号文档 §5 与 ADR-18 明确禁止部分 apply/add 候选缩小全局路径不变量。
  Date: 2026-07-18
- Decision: 本 Goal 只建立共享边界和 manifest full-profile 接缝，不实现 doctor/state/
  planner/add 命令。
  Rationale: 这些消费者尚未存在或属于后续切片；提前实现会扩大范围并混入 state/mutation
  语义。
  Date: 2026-07-18
- Decision: root 与 control-leaf consumption capability 缺口是实施 gate，不在 boundary 层
  修补。
  Rationale: 用户明确要求 identity 无法表达规范性质时先记录并暂停；改变 root/control
  resolution 或规范接受集合需要独立审查与明确授权。
  Date: 2026-07-18
- Decision: 不新增依赖，不持久化 identity，不跨文件系统 mutation 快照复用 resolution。
  Rationale: 当前 API 是只读、进程内、snapshot-scoped 的唯一语义源；本 Goal 无需改变这些
  边界。
  Date: 2026-07-18

## Outcomes and Handoff

当前已完成计划起点与 Milestone 1 capability gate 的复现。2026-07-18 从干净
`main@8075a6c` 创建 `feat/path-boundaries`，以 `4f05174` 提交本计划；随后现有 paths 窄测通过，
确认 filesystem root 与 control leaf-symlink consumption capability 缺口。本计划按批准的
停止条件暂停在 Milestone 1，尚未修改生产代码或测试，未执行 `make check`、双平台 CI、后续
milestone 或最终独立实现复核，也未访问真实私人数据或进行未授权 Git/托管操作。

后续接手者应从 `feat/path-boundaries@4f05174` 及其后的计划暂停提交恢复，读取本计划的已知
gate，并先取得 path-identity 前置修复或 control root/leaf 规范语义的裁决。gate 解除后才按
Milestone 1–5 顺序推进，每个 milestone 同步更新 living sections、执行窄测与完整门禁、检查
完整 diff 并创建独立 semantic commit。最终只有双平台 CI、独立复核、所有意见处理和计划
生命周期收口均完成，且当次任务授权覆盖对应 Git 操作时，才能声称 `feat/path-boundaries`
达到 review-ready；merge、push、PR 或发布仍不由本计划自身授权。
