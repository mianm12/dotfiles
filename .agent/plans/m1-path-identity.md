# feat/path-identity：M1 只读 target 文件系统身份 ExecPlan

## Purpose

在 `internal/paths` 建立独立于展示路径的只读 target 身份层，为后续 manifest、state、
planner 和提交前 Precond 提供单一路径语义来源，但本计划不接入这些消费者。

完成后，现存祖先 symlink、当前文件系统实际接受的大小写/Unicode 别名和不存在 leaf
得到一致解释；不同目录项即使是同一 inode 的 hard link 也保持不同 target 身份。无法
权威解释不存在名称时返回 `ErrIdentityUnavailable` 并 fail closed，不退回字符串比较。

## Scope / Non-goals

范围内：

- `internal/paths` 中不持久化、无文件系统 mutation 的 target 身份表示、解析、等价与严格
  祖先关系。
- 现存祖先 symlink、阻断对象、现存或不存在 leaf，以及缺失中间目录形成的虚拟尾部。
- macOS/Linux 大小写、Unicode 与 hard-link 的真实文件系统测试和跨平台编译。
- 每个 milestone 的窄测、`make check`、完整 diff、计划更新、独立 commit 与 clean 检查。

明确不做：

- 控制面家族、完整 effective profile 全局校验、state、planner、mutation、提交时
  Precond 或持久化格式。
- 接入 manifest/apply/add/doctor 等消费者，或改变 CLI/README/规范行为。
- 对抗恶意环境或主动并发篡改；Windows 不在范围内。
- 通过 `GOOS`、文件系统类型、`strings.EqualFold` 或通用 Unicode normalization 猜测未知
  文件系统语义。

## Required Properties

1. 身份与展示路径分离；`filepath.Clean` 只做词法输入清理，不能成为身份判断。
2. 输入必须是非空绝对路径。身份值仅用于当前进程和当前文件系统快照，不序列化、不持久化。
3. 祖先 symlink 跟随相对或绝对 link target；最终 leaf symlink 不跟随，因为 target 身份
   表示目录项位置，不表示 leaf 指向对象。
4. 若 `A` 是 leaf symlink，则 `A` 不是 `A/child` 或其真实目标下 `child` 的祖先；解析
   `A/child` 时 `A` 作为祖先被跟随，真实目标目录是该 child 身份的祖先。
5. 现存祖先目录可以使用文件系统对象身份锚定；leaf inode 永不参与 target 等价，避免把
   hard link 的不同目录项合并。
6. 相等按完整身份组件判断；祖先关系是严格的组件前缀，自身不是自身祖先，`foo` 不是
   `foobar` 的祖先。
7. 现存普通文件、悬空 symlink、loop 和特殊对象仅在作为祖先时阻断；作为 leaf 时仍表示
   该目录项位置。
8. 第一个缺失组件后的虚拟尾部必须使用最近现存父目录可权威证明的名称语义。若大小写或
   Unicode 规则无法只读确定，返回 `ErrIdentityUnavailable`，不得生成近似身份。
9. 权限、IO 和路径变化错误保留底层 cause；确认的阻断错误可由 `errors.Is(err,
   ErrPathBlocked)` 判断，身份不可用可由 `ErrIdentityUnavailable` 判断。
10. 解析只允许 `Lstat`、`Stat`、`Readlink`、目录枚举和只读能力查询；不得创建 probe、目录、
    文件或其他临时对象。
11. 同一稳定拓扑下重复解析结果相同。零值身份无效，不与任何身份相等，也不形成祖先关系。

## Internal API

保持最小内部接口，字段全部私有，不提供 `String`、`Key`、marshal 或展示路径方法：

```go
type TargetIdentity struct { /* private */ }

func ResolveTargetIdentity(path string) (TargetIdentity, error)
func (id TargetIdentity) Equal(other TargetIdentity) bool
func (id TargetIdentity) IsAncestorOf(other TargetIdentity) bool

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
  `make check` 通过，以 `test(paths): 覆盖平台 target 身份语义` 提交。
- [ ] Milestone 4：完整门禁、独立复核与收口。

每完成一个 milestone，立即记录日期、测试、commit SHA 和新发现；更新随该 milestone 的
语义 commit 提交。

## Milestones

### Milestone 1：基础身份表示、等价和祖先关系

先写失败测试，再实现：

- 增加不透明身份值、零值规则、等价和严格祖先关系。
- 支持父目录现存且没有祖先 symlink 的 basic path；现存目录项恢复实际名称，不以 leaf
  inode 判等。
- 为不存在 leaf 建立平台只读名称语义边界：能权威分类才生成组件；否则返回
  `ErrIdentityUnavailable`。
- 同步 `internal/paths` package doc，使其包含 control-plane、missing 与 target identity。
- 不重构 `IsMissing`；只运行既有回归证明行为未变。

窄测：

```sh
go test -count=1 ./internal/paths -run 'TestTargetIdentity|TestResolveTargetIdentity_Basic|TestIsMissing'
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

可观察验收：同一路径相等、兄弟不同、严格祖先按组件工作、零值无效；现存不同 hard-link
leaf 不因 inode 合并；未知缺失名称语义明确失败且无写入。

提交：`feat(paths): 建立 target 文件系统身份`。提交后 `git status --short` 必须为空才进入
Milestone 2。

停止条件：只能退回字符串比较、必须引入 native/cgo、需要持久化格式、或规范无法裁决 leaf
位置语义。命中时先更新 Discoveries，再暂停请求裁决，不提交近似实现。纯 Go 新依赖若确有
必要，必须先说明 `go.mod`/`go.sum` 影响，并以独立 dependency commit 和完整门禁处理；当前
默认是不新增依赖。

### Milestone 2：祖先 symlink、阻断对象和不存在 leaf

先写失败测试，再实现：

- 逐组件处理相对/绝对祖先 symlink、loop 上限、阻断对象和底层 IO 错误。
- leaf symlink 不跟随；同一路径作为后续 child 的祖先时才跟随。
- 支持不存在 leaf 和缺失中间目录尾部；只有最近现存父目录的语义能权威外推时才生成身份，
  否则 fail closed。
- 保持 `IsMissing` 实现与 API 不动，仅补充必要回归；若出现无法避免的重复安全不变量，暂停
  记录后再决定是否做最小抽取，不预建统一遍历框架。

窄测：

```sh
go test -count=1 ./internal/paths -run 'TestResolveTargetIdentity_(Symlink|Blocked|Missing|LeafSymlink)|TestIsMissing'
```

随后运行与 Milestone 1 相同的 `make check`、完整 diff/untracked 和 `git diff HEAD --check`。

可观察验收：目录 symlink 别名与真实路径身份相等；leaf symlink 与目标不同且不是目标 child
祖先；祖先普通文件、FIFO/特殊对象、悬空 link 和 loop 拒绝；缺失 leaf/尾部可重复解析或
明确不可用；解析不产生文件系统 mutation。

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
| 严格祖先 | 自身、组件前缀、字符串前缀反例和 leaf symlink 边界 |
| 祖先 symlink | 相对/绝对 alias 与真实目录身份相同 |
| 阻断 fail closed | 普通文件、特殊对象、悬空 link、loop、权限/IO cause |
| 不存在路径 | leaf、缺失尾部、可重复解析与 `ErrIdentityUnavailable` |
| 大小写/Unicode | 真实文件系统 lookup oracle 与 resolver 一致 |
| hard link | `SameFile` 为真但 identity 不同 |
| 只读 | 解析前后 fixture 目录项和对象 metadata 不变 |
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

## Decision Log

- 2026-07-17：身份是进程内只读值，不建立持久化 key。
- 2026-07-17：leaf 身份由父目录位置与目录项名称决定，leaf inode 永不判等。
- 2026-07-17：ancestor symlink 跟随；leaf symlink 不跟随且不是其 resolved child 的祖先。
- 2026-07-17：未知或无法权威外推的缺失名称语义返回 `ErrIdentityUnavailable`。
- 2026-07-17：保持 `IsMissing` 不动；本切片只增加必要回归，不预建统一框架。
- 2026-07-17：不接入控制面、profile、state、planner、mutation 或 Precond。
- 2026-07-17：每个 milestone 都以测试、完整门禁、计划更新、独立 commit 和 clean 为完成
  条件；review 修复使用新 commit，不 amend。
- 2026-07-17：Milestone 1 不新增依赖；macOS 仅对可由 `Fpathconf` 权威分类的 ASCII 缺失
  名称建 key，Linux 与非 ASCII 未知语义返回 `ErrIdentityUnavailable`。
- 2026-07-17：Milestone 2 保持 leaf 与 ancestor 两种 symlink 角色分离：leaf 目录项不跟随，
  同一路径有后续组件时作为 ancestor 跟随；因此 leaf identity 不是 resolved child 的祖先。
- 2026-07-17：Milestone 3 对现存 path 的 case/Unicode 等价以实际 lookup 为准，不自行实现
  Unicode fold；只有不存在名称需要平台 capability，未知时继续 fail closed。

## Outcomes & Retrospective

执行完成后填写：

- milestone 和 review-fix commit SHA；
- 每轮窄测、`make check`、diff check 与 clean 证据；
- 本机文件系统的 case/Unicode oracle 结果及未知语义拒绝结果；
- darwin/linux 交叉编译结果，以及未获授权执行的 Linux CI 边界；
- 独立复核意见与对应修复；
- 最终 diff 的范围、依赖、README/规范/持久化影响；
- 与本计划的偏差和后续消费者接入所需的最小工作。

完成结论必须证明：`internal/paths` 已提供只读、fail-closed 的 target 身份原语；现存祖先
symlink 和文件系统别名一致解释，不存在路径在可证明时比较、不可证明时拒绝，leaf hard
link 不合并，且未触碰任何持久化、planner 或 mutation 边界。
