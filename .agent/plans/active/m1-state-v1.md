# feat/state-v1：建立严格只读的 state v1 加载边界

本 ExecPlan 是 living document。实施期间必须持续更新 `Progress`、
`Surprises & Discoveries`、`Decision Log` 和 `Outcomes and Handoff`，并遵循
`.agent/PLANS.md`。

## Purpose / Big Picture

完成后，后续 loader、planner 与 state store 可以通过 `internal/state` 读取 state v1，并明确
区分文件缺失、持久格式损坏、版本过新和 M1 暂不支持的 rendered 记录。任意 JSON 对象层级的
重复 member、严格 schema 或永久词法语义错误都会 fail closed；当前文件系统拓扑后来变得不可达、
落入控制面别名或缺少只读 identity capability 时，不会被误标成永久 corrupt。

本切片只建立只读 model、codec、loader 与独立 target identity 校验，不写 state、不创建目录或
lock。所有测试使用 `t.TempDir()` 内的合成 HOME/state，不接触真实私人数据。

## Scope / Non-goals

范围内：

- 新增 `internal/state` 的 state v1 model、严格 JSON codec 和只读文件 loader。
- 在解码前扫描完整 JSON token 流，拒绝任意对象层级重复 member；member 名按 JSON 解码后的
  字符串比较，因此 `a` 与 `\u0061` 同样重复。未知嵌套对象即使随后会被 strict schema 拒绝，
  其中的重复 member 也必须先被识别。
- 严格校验顶层、entry 与 run_once schema；必填字段、类型、未知字段、RFC3339 时间、kind 专属
  证据、规范 `~/` target key、module/source/run_once key 和支持的 `sha256:<hex>` 摘要。
- 区分 missing、corrupt、too-new、unsupported-rendered。合法 rendered 先通过完整 v1 校验，
  再返回 unsupported；畸形 rendered 仍属于 corrupt。
- 将不依赖当前文件系统的结构/词法语义验证与依赖 target identity 的运行时路径验证分层。
  多个 state key 在当前稳定拓扑下解析为同一 target identity 时 fail closed；祖先阻断、控制面
  alias 或 identity capability 不足返回独立的 runtime path validation 错误，不改变 codec 对
  持久数据的 corrupt 判断。
- 覆盖 macOS/Linux 编译、窄测、完整门禁、完整 diff 与 clean worktree。

明确不做：

- 不写 state、不创建 state root/state.json/lock、不取锁、不实现 atomic store、权限修正或恢复。
- 不实现 ownership、planner、executor、apply/add/status/doctor/state rebuild 或 rendered 的 M2
  生命周期。
- 不检查历史 source/script 当前是否存在，不把祖先拓扑后来变化当作格式损坏，不建立
  control-plane alias 的永久持久语义。
- 不修改 CLI、`internal/runtime` preflight、`internal/paths`、Makefile、CI、README、规范、
  `go.mod` 或 `go.sum`；不新增依赖。
- 不读取真实 `modules/`、machine config、state、backup、HOME 或其他私人数据。

## Contract and Context

- `docs/05-apply-engine.md` §2：state v1 的顶层、entry/run_once schema、三态、重复 member、
  rendered M1 fail-closed、target identity 冲突和历史记录语义。
- `docs/02-architecture.md` §2、§4–§6：state 路径来自已解析控制面；state 组件负责校验读取，
  mutation 前 fail closed；当前拓扑安全与持久格式语义必须分开。
- `docs/03-manifest-spec.md` §8：state 加载位于 requires 与严格 manifest 之后的独立阶段；本切片
  不改变 manifest 加载。
- `docs/04-cli-spec.md` §2–§3：state fail-closed 最终映射 exit 1；本切片不接 CLI。
- `docs/08-testing.md` §1–§3：任意层级重复 member、v1 字段、target/module/source/hash 与多个
  key 同 identity 是不可删除回归；祖先暂不可达记录不得被抹掉。
- `docs/09-roadmap.md` §1 M1、§3：M1 交付严格 state v1，但 rendered 只保留格式并整体不支持。

基线为 clean `main@7b43272`。现有 `internal/paths.ResolveTargetIdentity` 提供只读、不透明、只在
当前稳定拓扑内有效的 target identity；它可能返回 `ErrPathBlocked` 或
`ErrIdentityUnavailable`。这些错误描述当前路径验证能力，不是 state JSON 的永久 corrupt 证据。
当前仓库没有 `internal/state`，也没有 store/lock/loader 接线。

## Progress

- [x] 2026-07-19：确认 `pwd` 与 Git 顶层均为分配 worktree，branch 为 `feat/state-v1`，
  HEAD/base 为 `7b43272`，工作树 clean。
- [x] 2026-07-19：以 `0a5edd3` 单独提交本 active ExecPlan 起点。
- [x] 2026-07-19：两轮测试先行分别确认 codec 与 loader API 尚不存在；实现任意对象层级
  duplicate 扫描、严格 v1 model/codec 与错误分类，以 `11db414` 提交。
- [x] 2026-07-19：实现只读 file loader 和独立 target identity validation；blocked、identity
  capability 与持久 corrupt 保持分离，以 `24924a0` 提交。
- [x] 2026-07-19：首次完整 lint 发现测试直接比较 sentinel error；用 `errors.Is` 修正并以
  `c42700a` 提交，没有修改生产行为。
- [x] 2026-07-19：state/paths 20 次窄测、darwin/linux amd64 test binary 交叉编译、完整
  `make check BINARY=/private/tmp/dot-cp2-state-v1-check`、base diff check 与完整 diff 审计通过。
- [x] 2026-07-19：以独立 docs commit 记录本地验证与 worker handoff；该 commit 后确认
  worktree clean，计划保持 active。
- [x] 2026-07-19：首轮独立 review 提出并由主线程确认五项有效 finding：schema member
  case-insensitive 接受、identity collision 未归类 corrupt、invalid UTF-8/unpaired surrogate 被
  replacement rune 接受、RFC3339 parser 接受规范外形式，以及 alias fixture 使用 missing leaf。
- [x] 2026-07-19：先增加能稳定暴露五项 finding 的回归，再以 `f5cce34` 收紧原始 JSON、exact
  schema、时间和 identity 错误边界；state/paths 20 次窄测、双平台编译与 `make check` 通过。
- [x] 2026-07-19：第 2 轮完整 review 发现唯一 P1：`probeVersion` 在 too-new 分流前错误解释
  额外 `Version` member，使未知版本被 v1 case 规则误分类 corrupt。
- [x] 2026-07-19：新增 precedence 回归，固定 decoded duplicate 先于 version、too-new 对额外
  schema 不透明、v1 exact schema 继续生效；以 `d27d4ad` 修复并重新通过 20 次窄测、双平台
  编译、`make check` 与 diff check。
- [ ] 等待第 3 轮未参与实现 reviewer 最终复审完整 branch；计划保持 active。

## Milestones

### Milestone 1：提交 ExecPlan 起点

只提交本计划，记录范围、持久格式与运行时路径安全分层、验证和授权，不修改生产代码或测试。

Commit 边界：

    docs(state): 新建 state v1 ExecPlan

### Milestone 2：严格 state v1 codec 与结果分类

先增加会失败的测试，再建立最小 model 和内存 codec。解码协议先遍历完整 token 流并维护每个
object 自己的 decoded-member set，任何重复立即归类 corrupt；随后仅提取 version 进行版本分流，
`version > 1` 返回 too-new，`version = 1` 执行 strict schema 与完整词法语义校验。合法 rendered
记录在全部字段、摘要与时间通过后返回 unsupported-rendered。版本缺失、非整数、零/负数或其他
无效 v1 文档属于 corrupt。

测试覆盖顶层、entries/run_once map、record、未知嵌套对象和转义同名重复；各 kind 的必填、禁止
证据字段；RFC3339；规范 target/module/source/run_once key；sha256 摘要；历史 source/script 不
存在仍可解码。

Commit 边界：

    feat(state): 严格解码 state v1

### Milestone 3：只读 loader 与 target identity 分层验证

增加只读 loader：明确缺失返回 missing，其他 open/read 错误不伪装成 missing，读取后复用 codec。
loader 不创建 parent 或文件。增加独立 `ValidateTargetIdentities` 一类的只读边界：先把规范 target
key 展开到给定 absolute HOME，再解析 identity；相同 identity 返回 fail-closed 冲突错误。
`ErrPathBlocked`、`ErrIdentityUnavailable` 和其他路径 IO 错误保持 runtime validation cause，不能
变成 `ErrCorrupt`。控制面 overlap 仍留给后续 runtime loading 组合已校验控制面处理。

Commit 边界：

    feat(state): 分离 state 路径运行时验证

### Milestone 4：完整验证与 worker 交付记录

运行窄测、高重复测试、darwin/linux 编译、完整 `make check`、base...HEAD diff check、完整 diff 和
untracked 审计。更新本计划的 Progress、Discoveries、Decision Log 与 Handoff，但保持 active，
不执行生命周期迁移或 closure。

Commit 边界：

    docs(state): 记录 state v1 验证

## Validation and Acceptance

| 必须成立的性质 | 验证证据 | 状态 |
|---|---|---|
| 任意对象层级与 escaped 同名重复 member 均 corrupt | codec table tests | 通过 |
| exact case-sensitive member schema，case variant/双字段均拒绝 | raw schema tests | 通过 |
| invalid UTF-8 与 unpaired UTF-16 surrogate 在 token 前拒绝 | raw byte tests | 通过 |
| v1 strict schema、必填、类型、时间、kind 证据 | codec/model tests | 通过 |
| RFC3339 只接受 strict grammar 与合法日历/offset | time semantic tests | 通过 |
| target/module/source/run_once/hash 词法语义严格 | semantic tests | 通过 |
| missing/corrupt/too-new/unsupported-rendered 可区分 | loader/codec tests | 通过 |
| 畸形 rendered 是 corrupt，合法 rendered 才 unsupported | rendered tests | 通过 |
| 多 target key 同 identity 匹配 corrupt/conflict sentinel | existing-leaf alias tests | 通过 |
| blocked/capability/IO 不被标成 corrupt | identity error tests | 通过 |
| loader 与 validation 全程零写入 | tree snapshot tests | 通过 |
| macOS/Linux 可编译 | darwin/linux amd64 `go test -c` | 通过；Linux 未运行 |
| 当前平台完整门禁 | `make check` | 通过 |

从本 worktree 根运行：

    go test ./internal/state
    go test -count=20 ./internal/state ./internal/paths
    GOOS=darwin GOARCH=amd64 go test -c -o /private/tmp/dot-cp2-state-v1-darwin.test ./internal/state
    GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/dot-cp2-state-v1-linux.test ./internal/state
    git diff 7b43272...HEAD --check
    make check BINARY=/private/tmp/dot-cp2-state-v1-check

成功判据是全部本地命令退出 0，完整 diff 只含本计划与 `internal/state`，worktree clean。交叉编译
只证明编译，不声称目标平台运行；远端 CI 未实际运行时必须标为待验收。

## Safety, Authorization, and Recovery

用户已授权在分配 worktree 的本分支创建/修改范围内文件、stage、commit 和运行门禁；不授权操作
main、其他 worktree、merge、push、rebase、amend、branch 删除或读取真实私人数据。测试只使用
`t.TempDir()` 合成 HOME/state；构建输出进入 `/private/tmp`。

每个 milestone 形成独立 commit。失败保留最近成功 commit，以新 fix commit 修正；不 reset、
restore、clean、amend、吞错或用近似语义继续。worker 交付时计划保持 active，由 coordinator
安排 review、修复和 lifecycle closure。

## Interfaces and Dependencies

`internal/state` 只依赖标准库与现有 `internal/paths`。持久 model 不导出可由调用方绕过验证构造并
写回的公共字段接口；只读 consumer 通过 getter 或副本读取。错误分类应支持 `errors.Is`，使后续
runtime loader 能区分 missing/corrupt/too-new/unsupported-rendered 与 path validation 失败。

本切片不规定 store 或 runtime loader 私有类型。未来 store 必须消费已验证 model 并原子发布；
未来 runtime-loading 必须在其实际消费 state 的模式调用 loader，再单独组合当前 control-plane
与 target identity/path safety，不得把恢复路径不消费的 state 变成前置门禁。

## Surprises & Discoveries

- Observation: `GOOS=linux go test -run '^$'` 会先交叉构建再尝试在 macOS 执行 Linux test
  binary，稳定得到 `exec format error`，不能作为纯编译门禁。
  Evidence: 初次 Linux 命令构建成功后在启动 test binary 时失败；改用带显式 `-o` 的
  `go test -c` 后 darwin/linux amd64 均退出 0。
  Impact: 本计划和最终证据使用 `go test -c`；不把 Linux 交叉编译误报为实际平台运行。

- Observation: 首次 `make check` 的唯一失败来自 errorlint，指出测试以 `==` 比较 sentinel
  error；生产 codec、loader 和 identity 逻辑没有门禁失败。
  Evidence: 将两处分类辅助断言改为 `errors.Is` 后，窄测与完整 `make check` 全部通过。
  Impact: 以独立 `fix(state)` commit 保留审查轨迹，不 amend 已完成实现 commits。

- Observation: Go `encoding/json` 的 struct member 匹配不区分 ASCII case，字符串解码还会把
  invalid UTF-8 与 unpaired UTF-16 surrogate 替换为 U+FFFD；`time.Parse(time.RFC3339, ...)`
  也接受逗号小数和 `+24:00` 等规范外表示。
  Evidence: 首轮 review 回归在原实现上分别得到无错误；raw `[]byte` 测试证明输入在 token/schema
  层前已发生有损替换，时间测试证明标准 parser 比项目要求的 strict grammar 更宽。
  Impact: version=1 在 struct decode 前先校验 UTF-8/surrogate 和 exact member 集，RFC3339 先由
  strict grammar 收窄再调用 `time.Parse` 做日历语义校验。too-new 仍不解释未知版本 schema，
  但顶层 `version` 本身必须精确拼写。

- Observation: alias identity 回归若 leaf 不存在，会把测试结果额外绑定到平台 missing-name
  capability，无法纯粹证明已存在 target alias collision。
  Evidence: 原 fixture 只创建 real directory 与 ancestor symlink，没有创建 `file` leaf。
  Impact: fixture 先创建真实 leaf，再建立 alias；碰撞成功解析后错误链同时匹配 `ErrCorrupt` 与
  `ErrTargetIdentityConflict`，而 blocked/capability/IO 分支继续只匹配 `ErrPathValidation`。

- Observation: exact v1 schema 不能前移到 too-new 分流之前；否则额外 `Version` 等未知版本字段
  会被当前 CLI 误解释并覆盖“版本过新”的恢复诊断。
  Evidence: `{"version":2,"Version":999,"future":...}` 在第 2 轮 review 前返回 ErrCorrupt；
  移除 probe 对非 canonical member 的解释后返回 ErrTooNew。escaped decoded duplicate 仍由最前置
  token scan 返回 ErrCorrupt，`version=1` 的同一 case variant 仍由 exact schema 返回 ErrCorrupt。
  Impact: 加载顺序固定为 raw UTF/surrogate → decoded duplicate → 精确 canonical version probe →
  too-new 分流 → v1 exact schema；不改变已接受的 RFC3339 fail-closed 生成格式子集。

## Decision Log

- Decision: JSON 重复 member 检测先于 version/schema 分类，并覆盖未知嵌套对象。
  Rationale: duplicate 是原始文档歧义，不能因普通 decoder 的 last-wins 或 too-new 分流被隐藏。
  Date: 2026-07-19

- Decision: codec 只判永久格式与词法语义；当前 target identity 冲突使用独立只读验证入口。
  Rationale: 多 key 当前确认为同一 identity 时必须 fail closed，但祖先阻断、控制面 alias 或 Linux
  missing-name capability 不足是运行时路径安全，不能永久污染 corrupt 诊断或诱导用户丢 state。
  Date: 2026-07-19

- Decision: identity 解析全部成功后确认的多 key 同 identity 是 state 语义损坏；仅解析失败保持
  runtime path validation 分类。
  Rationale: 这与 `docs/05-apply-engine.md` §2 的“多个 key 指向同一 target 属语义损坏”一致，
  同时不会把后来形成的 ancestor blocker、control alias capability 或普通 IO 失败永久标 corrupt。
  Date: 2026-07-19

- Decision: version probe 只读取精确小写 canonical `version`，不检查或解释其他 member。
  Rationale: 原始 JSON 歧义必须先拒绝，但版本过新后当前 CLI 不认识其 schema；只有 v1 才能应用
  当前 exact member 集。缺少 canonical `version` 仍是 corrupt。
  Date: 2026-07-19

## Outcomes and Handoff

worker 实施与本地验证完成，计划按 Checkpoint 流程保持 active。分支基线为 `7b43272`；语义
commits 为计划起点 `0a5edd3`、严格 v1 codec/model `11db414`、只读 loader 与 runtime path
分层 `24924a0`、lint 暴露的测试断言修复 `c42700a`、首轮 review 完整修复 `f5cce34`，以及
第 2 轮 too-new 分流修复 `d27d4ad`。

实现新增 `internal/state`，只依赖标准库和现有 `internal/paths`。codec 在 schema 解码前遍历
完整 JSON token 流并按 decoded member 名拒绝任意对象层级 duplicate；v1 完整验证后才把合法
rendered 分类为 M1 unsupported。loader 明确区分 missing 与各 error sentinel；target identity
冲突 fail closed 且归类 `ErrCorrupt`，而 blocked、identity capability 和路径 IO 保留
`ErrPathValidation` 及底层 cause。首轮 review 后又固定 exact case-sensitive schema、invalid
UTF-8/unpaired surrogate 原始文本拒绝与 strict RFC3339 grammar。没有 state 写入、目录/lock
创建、CLI/runtime 接线或依赖变更。

本地证据包括 state/paths 20 次重复通过，darwin/linux amd64 test binary 交叉编译通过，
`make check BINARY=/private/tmp/dot-cp2-state-v1-check` 全部通过，`7b43272...HEAD` diff check 与
完整 name/status/stat 审计 clean；两轮 review fix 后同一组门禁均重新通过。Linux 只完成编译，
远端 macOS/Linux CI 未运行；当前准确状态为“本地验收通过、远端待验收”。下一步由 coordinator
安排第 3 轮最终完整 branch 复审；复审、freshness gate 与 lifecycle closure 完成前，本计划
不得迁移到 completed。
