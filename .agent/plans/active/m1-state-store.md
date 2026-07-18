# feat/state-store：原子提交 state v1

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，后续 mutation pipeline 可以把一个已经通过 `internal/state` 严格校验的 state v1
Snapshot 确定性编码，并原子发布到权限为 0700 的 state root 内。成功后读者只会看到完整的
新版 state.json 且文件权限为 0600；create、permissions、write、sync、close、rename 或 cleanup
等发布前阶段失败时，已有 state 的字节和 mode 均保持不变并返回带阶段上下文的错误。

本 Goal 只交付持久编码与 store 基础，不接入 CLI/runtime，不自行获取 lock。调用方仍须在
可信 preflight 和既有 mutation lock 下调用。

## Scope / Non-goals

范围内：

- 为 `internal/state.Snapshot` 增加确定性 v1 JSON 编码；拒绝零值或内部不合法 Snapshot，并以
  现有严格 `Decode` 作为持久格式语义的单一校验源。
- 在 `internal/state` 增加原子 store，要求绝对 state root 与其直接子文件路径，复用
  `internal/storage` 的 0700/0600 权限常量和 root 收敛入口。
- 在目标同一目录创建私有临时文件，按 permissions、完整 write、file sync、close、rename 顺序
  提交；只有 rename 成功才越过 state 发布点。
- 逐阶段故障注入并以真实旧文件证明所有发布前失败保留旧 bytes 与 mode；cleanup 失败必须与
  原始失败一同报告，遗留临时文件不得成为 state 真相。
- 覆盖缺失 state 首次创建、已有 state 替换、异常目标拒绝、权限收敛、确定性与完整旧/新可见性，
  并执行窄测、重复、race、双平台编译、diff check 和 `make check`。

明确不做：

- 不接入 runtime、CLI/Cobra、planner/executor，不获取或复用 process lock，不实现公开命令。
- 不实现 state transition/builder、ownership 决策、backup、恢复命令、rendered 的 M2 生命周期或
  M2/M3 能力。
- 不修改现有 state v1 持久格式、错误分类或规范，不读取真实 state、HOME、machine config、
  backup、repo 或 `modules/` 私人数据。
- 不承诺目录元数据的断电持久化，不实现 NFS 多客户端或主动并发篡改防护。

## Contract and Context

- `docs/02-architecture.md` §2、§4–§6：state root/state.json 为 0700/0600；state 组件负责严格
  校验、读取与原子提交；只读方只能看到完整旧版或新版。
- `docs/05-apply-engine.md` §2、§4、§6：state 落盘原子，发布失败不得回滚已提交 target；文件级
  sync 后原子替换，目录元数据的断电持久性不作承诺。
- `docs/08-testing.md` §1–§3：失败边界必须保留旧 state；测试固定提交点结果而不把私有 helper
  顺序变成公共契约。
- `docs/09-roadmap.md` §1 M1、§3：本切片只交付 state v1 原子提交基础，不提前实现 planner 或
  mutation 命令。

基线为 clean `main@2029afe69af5`，已经包含 completed runtime-preflight、state-v1 与
process-lock。`internal/state` 当前只提供严格 Decode/Load 和只读 Snapshot；
`internal/storage` 已提供 state 家族 0700/0600 的唯一权限常量与 `EnsureRoot`，但没有 state
编码或原子发布入口。

本计划不在 store 中取得 lock：process-lock 的 ownership 和 runtime 顺序属于后续
runtime-loading。store 只接受明确的 root/path，先完成全部路径与 Snapshot 编码校验，再建立
root 和准备临时文件；任何 rename 前错误不得修改旧 state inode。原子提交使用标准库在目标
目录创建临时文件并执行 file sync + rename，避免新增依赖并保留逐阶段故障验证能力。

## Progress

- [x] 2026-07-19：确认 `pwd`、Git 顶层、branch、HEAD/base 与 clean 状态分别为分配 worktree、
  `feat/state-store`、`2029afe69af5` 和 clean。
- [x] 2026-07-19：读取仓库约定、CP2 规范、completed preflight/state-v1/process-lock plans，
  并审阅现有 state/storage 代码与测试。
- [x] 2026-07-19：核对候选 renameio/v2 v2.0.2 的 Go 1.25、Apache-2.0、无传递依赖及
  sync/close/rename 封装边界；确定标准库窄实现能减少依赖并提供更精确的失败证据。
- [x] 2026-07-19：以 `89ce410` 单独提交本 active ExecPlan 起点。
- [x] 2026-07-19：先增加 Encode 确定性、严格 round-trip 与 invalid Snapshot 回归，确认因 API
  缺失编译失败；随后实现由现有 Decode 自校验的 v1 encoder，窄测与 20 次重复通过。
- [x] 2026-07-19：先增加 Store 成功、rename 可见性边界、invalid input/异常目标和逐阶段故障
  回归，确认因 API 缺失编译失败；随后实现同目录 temp、0600、完整 write、file sync、close、
  rename 与显式 cleanup，state/storage race 和 20 次重复测试通过。
- [x] 2026-07-19：state/storage 20 次重复与 race、Darwin/Linux amd64 state test binary
  交叉编译、tidy diff、完整 `make check BINARY=/private/tmp/dot-cp2-state-store-check`、base
  diff check 和完整文件范围审计均通过；计划保持 active 等待独立复核。
- [x] 2026-07-19：首轮独立 review 提出两项有效 finding：Encode 未检测 `encoding/json`
  对 invalid UTF-8 的有损替换；Store 虽处理 short write，但缺少 partial count + nil 的直接回归。
- [x] 2026-07-19：先增加两组回归，确认 target/source/link_dest/run_once key 的 invalid UTF-8
  在原实现上错误成功；随后对严格 Decode 结果逐 key/字段做无损 round-trip 比较，并补充
  partial write 返回 `io.ErrShortWrite`、保留旧 bytes/mode 且清理 temp 的证据。
- [x] 2026-07-19：以 `dc00ef0` 提交首轮 review 修复；修复后 state/storage 20 次重复、race、
  Darwin/Linux amd64 交叉编译、tidy diff、base diff check 与完整 `make check` 均通过，等待
  针对完整 branch 的独立复审。

## Milestones

### Milestone 1：建立确定性 v1 编码边界

先在 `internal/state` 增加测试，使用相同语义但不同 map 插入顺序的有效 Snapshot，证明编码
字节稳定、可由现有严格 Decode 重新读取，并拒绝零值 Snapshot。实现只把现有不可变 model
转换为 v1 wire document；字段规则与有效性仍由现有 Decode 复核，不复制 codec 校验逻辑，也
不加入尚无消费者的 state mutation/builder API。

Concrete steps：

    在 repo root 运行：go test ./internal/state
    在 repo root 运行：go test -count=20 ./internal/state
    预期：测试先因 Encode 缺失失败；实现后确定性、round-trip 和 invalid Snapshot 全部通过。

验收：同一 Snapshot 重复编码及等价不同插入顺序得到相同完整 JSON；输出严格可读，零值或
内部非法 Snapshot fail closed。

Commit 边界：

    feat(state): 确定性编码 state v1

### Milestone 2：原子发布并保留旧 state

先增加真实文件系统与故障注入测试，再实现 store。路径校验和 Snapshot 编码发生在首次写入
之前；root 复用 `storage.EnsureRoot` 收敛为 0700。临时文件与目标位于同一目录，使用 0600、
完整写入、file sync、close 后才 rename；rename 成功前所有错误路径清理临时文件，cleanup
失败则保留临时证据并同时报告原始 cause。已有目标不是普通文件时明确拒绝。

Concrete steps：

    在 repo root 运行：go test ./internal/state ./internal/storage
    在 repo root 运行：go test -count=20 ./internal/state ./internal/storage
    在 repo root 运行：go test -race ./internal/state ./internal/storage
    预期：首次测试因 Store 缺失失败；实现后成功发布与全部故障阶段均通过。

验收：首次和替换成功均得到完整 state、state.json 0600、root 0700；create/permissions/write/
sync/close/rename/cleanup 适用失败均返回可追踪 cause，rename 前旧 bytes 与 mode 不变；相对路径、
越界路径、异常目标和 invalid Snapshot 不发布 state。

Commit 边界：

    feat(state): 原子提交 state 文件

### Milestone 3：完整验证与 worker handoff

审阅 base...HEAD 完整 diff、提交边界和依赖图；重复窄测并交叉编译 Darwin/Linux 测试二进制，
运行与 CI 一致的 `make check`。只更新 active plan 的真实进度、发现和本地证据，不迁移到
completed；独立 reviewer 与 plan closure 由 coordinator 后续安排。

Concrete steps：

    在 repo root 运行：go mod tidy -diff
    在 repo root 运行：GOOS=darwin GOARCH=amd64 go test -c -o /private/tmp/dot-cp2-state-store-darwin.test ./internal/state
    在 repo root 运行：GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-cp2-state-store-linux.test ./internal/state
    在 repo root 运行：git diff 2029afe69af5...HEAD --check
    在 repo root 运行：make check BINARY=/private/tmp/dot-cp2-state-store-check
    预期：全部命令退出 0，完整 diff 仅含本 Goal 的 plan、state 实现/测试；worktree clean。

Commit 边界：

    docs(state): 记录 state store 验证

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| state v1 确定性、严格可回读编码 | codec round-trip 与顺序测试 | 本机通过 |
| 首次/替换发布只见完整新文件 | 真实文件系统 store 测试 | 本机通过 |
| state root/state.json 为 0700/0600 | mode 测试 | 本机通过 |
| 所有发布前失败保留旧 bytes/mode | 逐阶段故障注入矩阵 | 本机通过 |
| cleanup 失败保留旧 state 并报告双重 cause | cleanup 故障测试 | 本机通过 |
| 无新增依赖且双平台可编译 | tidy/module audit 与交叉编译 | 通过；Linux 未运行 |
| 当前平台完整门禁 | `make check` | 本机通过 |
| 远端 macOS/Linux CI | 精确 branch HEAD | 待验收 |

## Safety, Authorization, and Recovery

当前任务明确授权只在分配 worktree 的本分支修改、stage、commit 本 Goal 文件并运行门禁；不
授权操作 main、其他 worktree、merge、push、rebase、amend、reset 或真实私人数据。测试全部
使用 `t.TempDir()` 内合成 state root/state.json，构建产物和 cache 指向 `/private/tmp`。

每个 milestone 使用独立 commit；失败后以新的 fix commit 修正，不重写历史。rename 成功是
state 唯一发布点；此前失败清理临时文件但从不删除或 chmod 旧 state，cleanup 失败时报告并
保留非真相临时文件。计划在 worker handoff 时保持 active，等待 coordinator 安排独立复核。

## Interfaces and Dependencies

`internal/state.Encode(Snapshot)` 负责 v1 wire encoding；store 接受已验证 Snapshot 以及绝对
state root/path，并复用 `internal/storage` 权限边界。调用方负责在可信 control-plane preflight
与 mutation lock 下调用，store 不接管 runtime ordering 或 ownership。

候选 `github.com/google/renameio/v2 v2.0.2` 已核对为 Go 1.25、Apache-2.0 且无传递依赖，但其
高层提交把 sync/close/rename 封装为单一步骤。当前标准库所需机制很窄，且逐阶段错误证明是
本 Goal 核心，因此不新增该依赖；若实验显示无法可靠满足同文件系统 temp、file sync、close、
rename 与 cleanup 边界，则停止而不是静默换机制。

## Surprises & Discoveries

- Observation: renameio/v2 的高层原子提交把 sync、close 和 rename 合并在
  `CloseAtomicallyReplace` 中。
  Evidence: v2.0.2 本地 module source 显示该方法依次调用 `Sync`、底层 `Close` 与 `os.Rename`；
  module go.mod 为 Go 1.25 且无 require。
  Impact: 当前切片采用更窄的标准库实现，以便逐阶段故障注入和精确错误上下文，同时避免新增
  依赖；原子提交语义仍是同目录 temp + file sync + rename。

- Observation: `encoding/json.Marshal` 与 decoder 类似，会把 Go string 中的 invalid UTF-8
  替换为 U+FFFD，而不是返回错误。
  Evidence: 首轮 review 回归构造 `valid:true` 的内部 Snapshot；target key、source、link_dest
  与 run_once key 含 `0xff` 时，原 Encode 返回成功且 Decode 得到已经改变的字符串。
  Impact: Encode 在 strict Decode 自校验后逐 map key 和逐字段比较原 Snapshot；任何有损
  round-trip 统一返回 `ErrCorrupt`，不复制另一套 schema。

## Decision Log

- Decision: store 接受有效 Snapshot，并先增加确定性 Encode，不在本 Goal 预建 state builder。
  Rationale: 持久写入需要唯一 wire encoder；state transition API 依赖后续 planner/executor 的
  实际数据流，提前设计会扩大范围。
  Date: 2026-07-19

- Decision: 原子发布使用标准库窄实现，不引入 renameio/v2。
  Rationale: 所需步骤有限，现有规范不承诺目录 fsync；显式阶段能证明旧值保留和 cleanup 错误，
  同时降低依赖与替换成本。
  Date: 2026-07-19

## Outcomes and Handoff

worker 实施、首轮 review 修复与本地验证完成，计划按 Checkpoint 流程保持 active。分支基线为
`2029afe69af5`，语义 commits 为计划起点 `89ce410`、确定性 v1 编码 `cbecd91`、原子 store
`b3b7fa0` 和首轮 review 修复 `dc00ef0`。

实现新增 `internal/state.Encode` 与 `Store`。Encode 用现有严格 Decode 自校验 wire document；
Store 在绝对 root/path 与 Snapshot 校验后复用 storage 建立 0700 root，在目标同目录以 0600
准备临时文件，完整 write、file sync、close 后才以 rename 发布。故障矩阵证明 create、
permissions、write、sync、close、rename 及 cleanup 异常均返回原始 cause，rename 前旧 state
bytes 与 mode 不变；cleanup 自身失败会同时报告并保留非真相临时文件。实现未新增依赖，未
接入 CLI/runtime/lock，也未触及真实私人数据。

首轮独立 review 的两项 finding 均已修复：Encode 现在拒绝 invalid UTF-8 经 JSON marshal
替换造成的有损 Snapshot，Store 的 partial count + nil 路径由回归直接证明返回
`io.ErrShortWrite`、保留旧 state 并清理 temp。当前没有已知未处理 finding，仍需完整独立复审
确认结论。

本地 Darwin/arm64 上 state/storage 20 次重复与 race、完整 `make check`、tidy diff 和 base
diff check 通过；Darwin/Linux amd64 state 测试二进制交叉编译通过。Linux 只完成编译，精确
HEAD 的远端 macOS/Linux CI 未运行，准确结论为“本地验证通过、远端待验收”。尚未完成修复后
独立复审、ExecPlan lifecycle closure 或 main 集成；coordinator 应对完整 branch 安排只读 reviewer。
