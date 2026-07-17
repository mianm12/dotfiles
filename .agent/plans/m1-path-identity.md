# feat/path-identity：M1 只读 target 文件系统身份 ExecPlan

## Purpose

在 `internal/paths` 建立独立于展示路径的只读 target 身份层，为后续 manifest、state、
planner 和提交前 Precond 提供单一路径语义来源，但本计划不接入这些消费者。

完成后，现存祖先 symlink、当前文件系统实际接受的大小写/Unicode 别名和不存在 leaf
得到一致解释；不同目录项即使是同一 inode 的 hard link 也保持不同 target 身份。无法
权威解释不存在名称时返回 `ErrIdentityUnavailable` 并 fail closed，不退回字符串比较。

## Scope / Non-goals

范围内：

- `internal/paths` 中不持久化、无文件系统 mutation 的 target leaf 身份、路径解析拓扑、
  等价与严格祖先关系。
- 现存祖先 symlink、阻断对象、现存或不存在 leaf，以及缺失中间目录形成的虚拟尾部。
- macOS/Linux 大小写、Unicode 与 hard-link 的真实文件系统测试和跨平台编译。
- 每个 milestone 的窄测、`make check`、完整 diff、计划更新、独立 commit 与 clean 检查。

明确不做：

- 控制面家族、完整 effective profile 全局校验、state、planner、mutation、提交时
  Precond 或持久化格式。
- 接入 manifest/apply/add/doctor 等消费者，或改变 CLI/README/规范行为；允许澄清既有路径
  安全性质与内部 identity/topology 分工，但不得降低不变量。
- 对抗恶意环境或主动并发篡改；Windows 不在范围内。
- 通过 `GOOS`、文件系统类型、`strings.EqualFold` 或通用 Unicode normalization 猜测未知
  文件系统语义。

## Required Properties

1. 身份与展示路径分离；`filepath.Clean` 只做词法输入清理，不能成为身份判断。
2. 输入必须是非根的非空绝对路径。身份与 resolution 仅用于相关拓扑未变化的一次只读快照；
   发生文件系统 mutation 后必须重新解析，不序列化、不持久化。
3. 祖先 symlink 跟随相对或绝对 link target；最终 leaf symlink 不跟随，因为 target 身份
   表示目录项位置，不表示 leaf 指向对象。
4. 若 `A` 是 leaf symlink，则 `A` 的 leaf identity 与其真实目标目录不同；解析
   `A/child` 时，resolution 的遍历祖先同时包含 `A` 目录项和解析后的真实目录。因此
   desired `A`/`A/child` 形成中间目录项冲突，但 `A` 不因指向 `real` 就成为直接写成
   `real/child` 的祖先。
5. 现存祖先目录可以使用文件系统对象身份锚定；leaf inode 永不参与 target 等价，避免把
   hard link 的不同目录项合并。
6. 相等只按完整 leaf identity 判断；祖先关系由 resolution 记录的 canonical 目录链与完整
   traversal trace 共同判断。trace 包含递归 symlink target 展开中经过的目录项，即使该项
   随后被 `..` 折返或不在最终 canonical 链上。相同 leaf 不是自身的严格祖先，`foo` 不是
   `foobar` 的祖先。
7. 现存普通文件、悬空 symlink、loop 和特殊对象仅在作为祖先时阻断；作为 leaf 时仍表示
   该目录项位置。
8. 第一个缺失组件后的虚拟尾部必须使用最近现存父目录可权威证明的名称语义。若大小写或
   Unicode 规则无法只读确定，返回 `ErrIdentityUnavailable`，不得生成近似身份。
9. 权限以及未判定为 traversal blocker 的普通 IO 和路径变化错误保留底层 cause；确认的
   `ENOENT`/`ENOTDIR`/`ELOOP` 祖先阻断统一由 `errors.Is(err, ErrPathBlocked)` 判断，不承诺
   同时暴露底层 errno。身份不可用可由 `ErrIdentityUnavailable` 判断；若由只读 capability
   查询失败触发，则同时保留查询错误 cause。
10. 解析只允许 `Lstat`、`Stat`、`Readlink`、目录枚举和只读能力查询；不得创建 probe、目录、
    文件或其他临时对象。
11. 同一稳定拓扑下重复解析结果相同。零值 identity/resolution 无效，不相等也不形成祖先
    关系。

## Internal API

保持最小内部接口，字段全部私有，不提供 `String`、`Key`、marshal 或展示路径方法：

```go
type TargetIdentity struct { /* private */ }
type TargetResolution struct { /* private */ }

func ResolveTargetIdentity(path string) (TargetIdentity, error)
func ResolveTarget(path string) (TargetResolution, error)
func (id TargetIdentity) Equal(other TargetIdentity) bool
func (resolution TargetResolution) Identity() TargetIdentity
func (resolution TargetResolution) Equal(other TargetResolution) bool
func (resolution TargetResolution) IsAncestorOf(other TargetResolution) bool

var ErrPathBlocked error
var ErrIdentityUnavailable error
```

`ErrPathBlocked` 包含“祖先不能作为目录使用”的语义；权限或普通 IO 错误不伪装成 blocker。
所有错误包含发生问题的展示路径，但展示文本不是程序接口。

## Progress

- [x] 2026-07-17：读取指定规范、贡献流程、路线图、Makefile、CI 和 `internal/paths` 当前实现。
- [x] 2026-07-17：确认 `main@0d63454` 干净，并创建 `feat/path-identity`。
- [x] 2026-07-17：完成只读平台研究和 ExecPlan 独立审查；用户接受未知不存在名称语义时
  `ErrIdentityUnavailable`、整体 fail closed。
- [x] 2026-07-17：Milestone 1 完成；窄测与 `make check` 通过，完整 diff/check 无错误，
  以 `feat(paths): 建立 target 文件系统身份` 提交（`d405146`）。
- [x] 2026-07-17：Milestone 2 完成；symlink/blocker/missing 窄测与 `make check` 通过，
  以 `feat(paths): 解析 target 祖先拓扑` 提交（`6195f39`）。
- [x] 2026-07-17：Milestone 3 完成；真实文件系统 oracle、hard-link、交叉编译与
  `make check` 通过，以 `test(paths): 覆盖平台 target 身份语义` 提交（`10e51b1`）。
- [x] 2026-07-17：Milestone 4 完成；`main...HEAD` diff check、最终 `make check`、10 次
  paths 重复测试和独立只读复核通过，以 `docs(paths): 收口 target 身份 ExecPlan` 提交。
- [x] 2026-07-18：后续设计与独立复核确认 bare identity 前缀及“展示前缀 + 最终 canonical
  链”均不足以表达完整 symlink traversal；拆分 leaf identity 与 resolution，以逐组件 walker
  记录递归 link target，补 root/snapshot 契约、规范澄清和回归测试。窄测、10 次 paths 测试、
  darwin/linux 交叉编译、`make check` 和二次独立复核通过，以
  `fix(paths): 完整记录 target 遍历拓扑` 提交（`df0fe9c`）。
- [x] 2026-07-18：最终 review 以 64 层静态 symlink 链复现内核返回 `ELOOP` 而 resolver
  false-success，并发现普通 IO cause 缺少回归保护。新增内核 `Stat` authority、统一 blocker
  分类及双 API 测试；20 次 paths 测试、两项回归 50 次重复、darwin/linux 交叉编译、
  `make check` 和独立复核通过，以 `fix(paths): 以内核校验 target 祖先可达性` 提交
  （`eba7959`）。
- [x] 2026-07-18：再次 review 收窄 blocker/cause 契约，修复 Darwin capability 查询失败未
  匹配 `ErrIdentityUnavailable`，并让 leaf lookup 先于目录枚举，避免 missing leaf 扫描整个
  parent 及 exact leaf 绕过权限错误。新增两项回归，50 次重复、20 次 paths 测试、
  darwin/linux 交叉编译及 `make check` 通过，以 `fix(paths): 收口 target 身份错误边界`
  提交（`dcfed7f`）。

每完成一个 milestone，立即记录日期、测试、commit SHA 和新发现；更新随该 milestone 的
语义 commit 提交。

## Milestones

### Milestone 1：基础身份表示、等价和祖先关系

先写失败测试，再实现：

- 增加不透明 leaf 身份值、resolution、零值规则、等价和严格祖先关系。
- 支持父目录现存且没有祖先 symlink 的 basic path；现存目录项恢复实际名称，不以 leaf
  inode 判等。
- 为不存在 leaf 建立平台只读名称语义边界：能权威分类才生成组件；否则返回
  `ErrIdentityUnavailable`。
- 同步 `internal/paths` package doc，使其包含 control-plane、missing 与 target identity。
- 不重构 `IsMissing`；只运行既有回归证明行为未变。

窄测：

```sh
go test -count=1 ./internal/paths -run 'TestTarget(Identity|Resolution)|TestResolveTargetIdentity_Basic|TestIsMissing'
```

完整门禁与 diff：

```sh
artifact_dir="$(mktemp -d)"
make check BINARY="$artifact_dir/dot"
git status --short
git diff --stat HEAD
git diff HEAD -- .agent/plans/m1-path-identity.md internal/paths
git diff HEAD --check
```

可观察验收：同一路径相等、兄弟不同、strict ancestor 按 resolution topology 工作、零值
无效；现存不同 hard-link leaf 不因 inode 合并；未知缺失名称语义明确失败且无写入。

提交：`feat(paths): 建立 target 文件系统身份`。提交后 `git status --short` 必须为空才进入
Milestone 2。

停止条件：只能退回字符串比较、必须引入 native/cgo、需要持久化格式、或规范无法裁决 leaf
位置语义。命中时先更新 Discoveries，再暂停请求裁决，不提交近似实现。纯 Go 新依赖若确有
必要，必须先说明 `go.mod`/`go.sum` 影响，并以独立 dependency commit 和完整门禁处理；当前
默认是不新增依赖。

### Milestone 2：祖先 symlink、阻断对象和不存在 leaf

先写失败测试，再实现：

- 逐组件处理相对/绝对祖先 symlink、递归 link target 的完整 traversal trace、loop 上限、
  阻断对象和底层 IO 错误。
- leaf symlink 不跟随；同一路径作为后续 child 的祖先时才跟随。
- 支持不存在 leaf 和缺失中间目录尾部；只有最近现存父目录的语义能权威外推时才生成身份，
  否则 fail closed。
- 保持 `IsMissing` 实现与 API 不动，仅补充必要回归；若出现无法避免的重复安全不变量，暂停
  记录后再决定是否做最小抽取，不预建统一遍历框架。

窄测：

```sh
go test -count=1 ./internal/paths -run 'TestResolveTarget(Identity)?_(Symlink|Blocked|Missing|LeafSymlink|EqualAlias)|TestIsMissing'
```

随后运行与 Milestone 1 相同的 `make check`、完整 diff/untracked 和 `git diff HEAD --check`。

可观察验收：目录 symlink 别名与真实路径身份相等；leaf symlink 与目标 identity 不同，
但作为 `A/child` 的中间目录项时进入遍历祖先；`A -> B -> real` 的 `B` 和 link target 中
被 `..` 折返的目录同样进入 trace，直接写成真实路径的 child 不产生伪祖先关系；祖先普通
文件、FIFO/特殊对象、悬空 link 和 loop 拒绝；缺失 leaf/尾部可重复解析或明确不可用；
解析不产生文件系统 mutation。

提交：`feat(paths): 解析 target 祖先拓扑`。提交后工作区必须 clean。

停止条件：阻断对象被误判为缺失、需要替换祖先、symlink 只能靠展示字符串特判，或缺失
尾部需要猜测语义。

### Milestone 3：macOS/Linux 大小写、Unicode 与 hard-link 真实文件系统测试

先写测试，再只做测试暴露出的最小实现修正：

- 在 `t.TempDir()` 的真实文件系统创建 case、NFC/NFD 和 hard-link fixture。
- 通过真实 lookup 独立观测测试目录的大小写/Unicode 行为，再验证现存别名和不存在 leaf 的
  resolver 结果；不可用是显式允许结果，但不得返回错误身份。
- hard-link 测试先证明 `os.SameFile == true`，再断言不同目录项 identity 不同。
- 至少组合一次祖先 symlink 与 case/Unicode 别名。
- 测试不按 `runtime.GOOS` 硬编码预期；平台 build files 必须能在 darwin/linux 编译。

窄测与交叉编译：

```sh
go test -count=1 -v ./internal/paths -run 'TestTargetIdentity_(Case|Unicode|HardLink|SymlinkAlias)'
artifact_dir="$(mktemp -d)"
GOOS=darwin GOARCH=amd64 go test -c -o "$artifact_dir/paths-darwin.test" ./internal/paths
GOOS=linux GOARCH=amd64 go test -c -o "$artifact_dir/paths-linux.test" ./internal/paths
```

随后运行 `make check`、完整 diff/untracked 和 `git diff HEAD --check`。

可观察验收：本机真实文件系统 oracle 与 resolver 一致；现存大小写/Unicode 别名合并；无法
权威解释的不存在名称返回 `ErrIdentityUnavailable`；hard link 始终分离；darwin/linux 均
可编译。由于本 Goal 不授权 push/PR，本地不能宣称已执行 Linux runner；测试必须可由现有
CI 在后续获授权 push 时真实执行，并在 Outcomes 明确这一未执行外部门禁。

提交：`test(paths): 覆盖平台 target 身份语义`。提交后工作区必须 clean。

停止条件：测试依赖 OS 猜测、关键场景被静默 skip、Unicode 近似算法伪装成精确语义，或任一
目标平台不能编译。

### Milestone 4：完整门禁和独立复核

- 检查 `main...HEAD` 完整 diff、所有 commit 边界和任务相关 untracked 文件。
- 运行最终 `make check`。
- 由未参与实现的只读 subagent 复核平台语义、祖先 symlink、缺失 leaf、hard-link、错误
  分类和范围边界；subagent 不修改共享工作区。
- 主线程处理意见。实质问题以新的 `fix(paths): ...` commit 修复，重新执行窄测、`make check`、
  完整 diff、计划更新和 clean 检查；不得 amend 既有 commit。
- 填写 Outcomes & Retrospective，并以独立收口 commit 记录验证证据。

最终命令：

```sh
git status --short --branch
git log --oneline --decorate main..HEAD
git diff --stat main...HEAD
git diff main...HEAD -- .agent/plans/m1-path-identity.md internal/paths go.mod go.sum
git diff main...HEAD --check
artifact_dir="$(mktemp -d)"
make check BINARY="$artifact_dir/dot"
```

提交：`docs(paths): 收口 target 身份 ExecPlan`。若有 review 修复，修复 commit 必须先完成；
最终 commit 后再次确认工作区 clean。

停止条件：门禁失败、独立复核有未解决问题、diff 越界、需要修改规范/持久化，或无法证明
解析只读。

## Concrete Steps

每个 milestone 固定执行：记录起点 → 写能暴露缺口的测试 → 最小实现 → 窄测 → `make check`
→ 检查完整 diff/untracked → 更新本计划 → `git diff HEAD --check` → stage/语义 commit →
确认 clean。任何一步失败都停留在当前 milestone，不提前开始下一项。

分支起点固定为 `main@0d63454`。只允许本 Goal 已授权的 create/switch、stage 和 commit；不
merge、push、rebase、amend、tag、删除分支或访问真实用户数据。

## Validation

| 性质 | 证据 |
|---|---|
| 身份与展示分离 | API 无字符串 key/marshal；关系测试不比较展示路径 |
| 严格祖先 | 自身、canonical 目录链、展示路径与递归 link target 的完整 trace、`..` 折返项、字符串前缀反例和 leaf symlink 正反例 |
| 祖先 symlink | 相对/绝对 alias 与真实目录身份相同 |
| 阻断 fail closed | 普通文件、特殊对象、悬空 link、loop、权限/IO cause |
| 不存在路径 | leaf、缺失尾部、可重复解析与 `ErrIdentityUnavailable` |
| 大小写/Unicode | 真实文件系统 lookup oracle 与 resolver 一致 |
| hard link | `SameFile` 为真但 identity 不同 |
| 只读 | 缺失路径保持缺失，fixture 无新增/删除目录项，代码路径不含创建或写入调用 |
| 每 milestone 门禁 | 窄测、`make check`、diff check、计划更新、commit、clean |
| 最终门禁 | `git diff main...HEAD --check`、`make check`、独立只读复核 |

## Idempotence / Recovery

- resolver 只读；稳定拓扑下可重复调用，无需清理或补偿。
- 测试只使用 `t.TempDir()`；不读取或修改真实 HOME、repo、config、state、backup。
- 构建产物只进入 `mktemp -d` 或已忽略目录。
- 每个 milestone 独立提交。失败时保留最近通过的 commit，不使用 `reset --hard`、`clean`、
  restore 或其他可能覆盖用户数据的命令。
- 错误通过新增回归测试和新 commit 修复；不通过 skip、吞错或 fallback 恢复绿色。

## Surprises & Discoveries

- 2026-07-17：当前 `internal/paths` 只有 control-plane 展示路径和 `IsMissing`，没有 target
  identity；`IsMissing` 已固定悬空 symlink 不等于正常缺失。
- 2026-07-17：leaf hard link 要求排除以 leaf dev/inode 或 `os.SameFile` 作为身份 key；对象
  身份只可用于可遍历祖先目录锚定或消除现存别名，且不能让多个 leaf 目录项合并。
- 2026-07-17：现存别名可由真实 lookup/目录项观测；两个均不存在的不同字节名称没有通用
  只读 collation-key API。APFS/HFS+ 和 Linux casefold 使用不同、版本化 Unicode 规则，
  通用 normalization 不是规范语义。
- 2026-07-17：用户裁决未知不存在名称返回 `ErrIdentityUnavailable` 并整体 fail closed；
  不引入 native/cgo，不设计字符串 fallback。
- 2026-07-17：本 Goal 不授权 push，因此只能编写 Linux 真实文件系统测试并交叉编译；实际
  Linux runner 结果必须在未来获授权 push/PR 后由现有 CI 提供，不能在本次 Outcomes 中
  声称已运行。
- 2026-07-17：Milestone 1 以 `EvalSymlinks` 解析现存 parent，身份保存解析后组件和 leaf
  目录项位置；这足以固定基础关系，缺失 parent、完整 blocker 分类和 leaf alias 恢复留给
  Milestone 2/3。
- 2026-07-17：本机 `make check` 全部通过；沙箱拒绝 Go 写真实 module stat cache 时只输出
  非致命 warning，测试、race、lint 与构建均成功，未修改真实用户数据。
- 2026-07-17：Milestone 2 无需重构 `IsMissing`。resolver 向上寻找最近安全可达目录时复用
  其“悬空 symlink 不算正常缺失”判断，再由 `Stat`/`EvalSymlinks` 区分目录 symlink、非目录、
  loop 和普通 IO 错误。
- 2026-07-17：缺失多级尾部只有在最近现存父目录的名称语义可外推时才建立身份；本机
  macOS 的 ASCII 尾部可用，Linux/Unicode 未知语义仍按裁决返回不可用。
- 2026-07-17：本机临时目录所在文件系统同时接受 `CaseProbe`/`caseprobe` 和 NFC/NFD
  两种现存路径写法；resolver 均恢复为同一真实目录项。ASCII 大小写不同的缺失名称也得到
  同一身份，非 ASCII 缺失名称明确返回 `ErrIdentityUnavailable`。
- 2026-07-17：现存 alias 先由文件系统 lookup 取得对象，再映射回父目录真实目录项；精确
  名称优先。若同一 inode 有多个 hard-link 目录项，仅在权威名称 key 能选出唯一项时接受，
  因而 case alias 不会把 sibling hard link 合并。
- 2026-07-17：darwin/amd64 与 linux/amd64 test binary 交叉编译成功；按 Goal 授权边界未
  push，故 Linux 测试已编写但未在真实 Linux runner 执行。
- 2026-07-17：最终独立 subagent 逐行复核 3 个 milestone commit 和 8 个变更文件，未发现
  P0–P2 或其他必须修复的问题；复核覆盖平台名称语义、symlink 角色、missing tail、blocker
  cause、hard-link 多候选、只读性和范围边界。
- 2026-07-17：`go test -count=10 ./internal/paths` 通过，未发现真实文件系统测试抖动。
- 2026-07-18：后续主线程复核发现 identity equality 与 traversal ancestry 是两个相关但不同
  的关系：目录 symlink `A` 的 leaf identity 不等于目标目录，但 `A/child` 的展示路径确实
  经过 `A`；只保留解析后的组件会漏掉 05 §5 的中间目录项冲突。
- 2026-07-18：两个 resolution 可以拥有同一 leaf identity 却来自不同展示路径，ancestor
  topology 因而不能塞进 `TargetIdentity` 而保持值语义；新增独立 `TargetResolution`，并用
  单次调用内的目录与 missing-name key 缓存避免 symlink 展开重复枚举目录和能力查询。
- 2026-07-18：独立复核发现“展示前缀 + 最终 canonical 链”仍不是完整遍历拓扑；例如
  `A -> B -> real` 会漏掉 `B`，`A -> X/../real` 会漏掉实际必须先作为目录遍历的 `X`。
  resolver 因而改为逐组件展开 symlink，并直接记录完整 traversal trace。
- 2026-07-18：用户态 walker 的 255 次 link guard 不是内核 pathname resolution 上限；当
  target parent 本身是深链 symlink 且真实 leaf 精确存在时，原实现只 `Lstat` parent，随后
  可绕过展示路径 lookup 并返回身份。稳定复现中 `os.Stat(A/child)` 返回 `ELOOP`，resolver
  却成功，因此仅靠 walker 不能权威证明祖先可达。
- 2026-07-18：`ErrPathBlocked` 是确认祖先阻断后的领域分类；若再 multi-wrap `ENOENT`，调用方
  可能把悬空 ancestor 当作正常 missing。只有非 blocker 普通错误保留 cause，capability 查询
  失败则同时保留 `ErrIdentityUnavailable` 与查询 cause。
- 2026-07-18：leaf exact-name 快路径若在 `Lstat` 前只依赖 `ReadDir`，会让“parent 可枚举但
  不可搜索”的 target 错误成功；missing leaf 还会无谓读取并排序整个 parent。先做 leaf
  `Lstat` 同时恢复可达性 authority 并跳过 missing 路径的目录枚举。

## Decision Log

- 2026-07-17：身份是进程内只读值，不建立持久化 key。
- 2026-07-17：leaf 身份由父目录位置与目录项名称决定，leaf inode 永不判等。
- 2026-07-17：ancestor symlink 跟随；leaf symlink 不跟随且不是其目标目录 identity。
- 2026-07-17：未知或无法权威外推的缺失名称语义返回 `ErrIdentityUnavailable`。
- 2026-07-17：保持 `IsMissing` 不动；本切片只增加必要回归，不预建统一框架。
- 2026-07-17：不接入控制面、profile、state、planner、mutation 或 Precond。
- 2026-07-17：每个 milestone 都以测试、完整门禁、计划更新、独立 commit 和 clean 为完成
  条件；review 修复使用新 commit，不 amend。
- 2026-07-17：Milestone 1 不新增依赖；macOS 仅对可由 `Fpathconf` 权威分类的 ASCII 缺失
  名称建 key，Linux 与非 ASCII 未知语义返回 `ErrIdentityUnavailable`。
- 2026-07-17：Milestone 2 保持 leaf 与 ancestor 两种 symlink 角色分离：leaf 目录项不跟随，
  同一路径有后续组件时作为 ancestor 跟随。2026-07-18 补充裁决：前者决定 equality，后者
  必须同时把 symlink 目录项与解析后的真实目录记入 resolution 的遍历祖先，不能只比较
  canonical identity 组件前缀。
- 2026-07-17：Milestone 3 对现存 path 的 case/Unicode 等价以实际 lookup 为准，不自行实现
  Unicode fold；只有不存在名称需要平台 capability，未知时继续 fail closed。
- 2026-07-18：`TargetIdentity` 只表达 leaf 等价，`TargetResolution` 表达一次展示路径解析及
  祖先拓扑；root 不是合法 entry target，入口明确拒绝。所有值只属于相关拓扑未变化的只读
  快照，mutation 后重新解析，不承诺跨快照稳定。
- 2026-07-18：resolution 不从最终 identity 反推祖先；resolver 逐组件跟随 symlink target，
  把每个实际要求为目录的 entry identity 加入 trace，并在 `..` 改变当前位置前保留已遍历
  entry。该规则同时覆盖 chained symlink 与非最终 canonical 前缀。
- 2026-07-18：内核 `Stat` 权威判定 target parent 是否实际可达且为目录，显式 walker 只负责
  恢复真实目录项名称和记录完整 trace；walker 的 255 次限制仅作自身循环保护，不猜测平台
  内核上限。`ENOENT`/`ENOTDIR`/`ELOOP` 是 traversal blocker，权限和其他普通 IO 错误继续
  保留底层 cause 且不伪装成 `ErrPathBlocked`。
- 2026-07-18：blocker 只承诺 `ErrPathBlocked`，不同时承诺底层 errno；只读 capability 查询
  失败属于 `ErrIdentityUnavailable`，并额外保留查询 cause。leaf 先 `Lstat`，仅在确认现存后
  枚举 parent 以恢复真实目录项名称；不为尚未接入的 profile consumer 预建批量 resolver。

## Outcomes & Retrospective

结果：

- Milestone 1：`d405146 feat(paths): 建立 target 文件系统身份`。
- Milestone 2：`6195f39 feat(paths): 解析 target 祖先拓扑`。
- Milestone 3：`10e51b1 test(paths): 覆盖平台 target 身份语义`。
- Milestone 4：`41d1834 docs(paths): 收口 target 身份 ExecPlan`；当时的独立复核未产生
  fix commit。
- 后续设计复核：`df0fe9c fix(paths): 完整记录 target 遍历拓扑`。它将 leaf equality 与
  traversal topology 分层，并以逐组件 symlink walker 取代从最终 identity 反推祖先。
- 最终 review 修复：`eba7959 fix(paths): 以内核校验 target 祖先可达性`。它恢复 kernel
  authority 与 trace collection 的职责分离，并补齐 deep symlink `ELOOP` 和普通 IO cause
  的双 API 回归。
- 再次 review 修复：`dcfed7f fix(paths): 收口 target 身份错误边界`。它明确 blocker 与普通
  cause 的分类，保证 Darwin capability 查询失败可由 `ErrIdentityUnavailable` 识别，并将
  leaf lookup 前置到目录枚举之前。
- 每个 milestone 均按顺序完成失败测试、最小实现、窄测、`make check`、完整 diff/check、
  ExecPlan 更新、独立 commit 和提交后 clean 检查。
- 最终验证通过：`git diff main...HEAD --check`、`make check`、
  `go test -count=20 ./internal/paths`，以及 darwin/amd64、linux/amd64 test binary 交叉编译；
  deep symlink/普通 IO 及本次 capability/permission 回归均各重复 50 次通过。
- 本机真实文件系统 oracle 接受大小写和 NFC/NFD 现存别名；resolver 返回相同身份。ASCII
  缺失名称按只读 case capability 比较，非 ASCII 缺失名称返回 `ErrIdentityUnavailable`。
- hard-link fixture 先证明 `os.SameFile == true`，再证明不同目录项身份不同；case alias 面对
  同 inode 多候选时只映射唯一名称项。
- 原 Milestone 4 复核无 P0–P2；后续主线程发现 leaf symlink 中间项缺口，第一轮独立复核又
  发现 chained symlink 和 `X/../real` 的 P1，两者由 `df0fe9c` 修复。最终 review 继续发现
  内核 symlink limit 的 P1 与普通 IO cause 测试 P3，由 `eba7959` 修复；再次 review 的三项
  P3 分别以计划契约收窄、Darwin 错误分类和 leaf lookup 顺序调整由 `dcfed7f` 收口。
- 最终 diff 涉及本 ExecPlan、`internal/paths` 实现/测试/package doc，以及 `docs/02`、
  `docs/05`、`docs/08` 对既有身份/拓扑不变量的澄清；未新增依赖，未修改 README、持久化
  格式、控制面、state、planner、mutation 或 Precond。

偏差与后续：

- 研究确认不存在名称没有跨文件系统通用只读 collation-key API；按用户裁决，对未知语义
  fail closed，而不是实现 normalization fallback。
- 本 Goal 不授权 push/PR，因此没有真实 Linux runner 结果；Linux 测试和 build 已就绪，需
  后续获授权后由现有双平台 CI 执行。
- 后续消费者应直接复用本 API，并在收到 `ErrIdentityUnavailable` 时整体拒绝依赖身份的
  校验阶段；不得自行添加字符串 fallback 或持久化内部身份字段。

结论：`internal/paths` 已提供只读、fail-closed 的 target leaf identity 与 traversal
resolution；现存祖先 symlink 的递归展开和文件系统别名得到一致解释，不存在路径在可证明
时比较、不可证明时拒绝，leaf hard link 不合并，且未触碰任何持久化、planner 或 mutation
边界。
