# dot 重构切换计划（cutover）

> [!NOTE]
> 本文是**非规范性**的工程切换清单，只描述如何把当前旧实现收敛到
> [`design-baseline.md`](design-baseline.md)。产品与行为契约以基线为唯一真相源；本文与基线
> 冲突时以基线为准。检查点完成的判断依据是代码、测试与 `make check`，不是本文的勾选状态。

## 0. 策略

采用**混合式**：先减法（砍掉非目标命令与特性，保持 `make check` 绿），再按依赖顺序把核心重写到
基线（允许自由合并 package），最后做全量验收。

- 不采用一次性 nuke `internal/` 重建：分阶段能让 `make check` 在每个检查点边界保持绿，风险可控，
  且早期就见到大幅 LOC 下降。
- 每个检查点边界要求 `make check` 绿；核心重写阶段旧测试随被删特性一并移除，新测试对齐 §13。
- **保留的基础设施**：`storage`（原子发布）、`lock`、`buildinfo`、`cmd/dot`。
- **测试契约**：基线 §13 的验收 1–19 是核心回归目标；所有成功 mutation 场景追加一次相同 apply 并
  断言无新 mutation。

## 1. 现状 → 目标差距

当前 `internal/` 约 17.3k 行源码 + 28.1k 行测试；目标预计降到个位数千行源码量级。

| 处置 | 对象 | 依据 |
|---|---|---|
| 删（叶子） | `internal/add`、CLI `add`/`doctor`/`diff` 命令、`internal/doctor` | §2 非目标：无 add/doctor；diff 由 `--dry-run` 覆盖 |
| 删（先拆耦合） | `internal/backup`、`internal/template`、`internal/datakey`、`manifest/requires` | §2 非目标：无 backup/template/秘密；基线无模块最低版本要求 |
| 大幅并入 | `internal/runtime`（MutationSession / 可信运行上下文 / Compatibility） | §11「不建立通用 action snapshot 或 precondition 系统」、§14 禁 workflow/state-machine |
| 坍缩重写 | `internal/paths`（1646 行 / 控制面身份系统 / case·unicode·fs 探测） | §3、§7.3：只用词法规范化 + ancestor symlink 解析 |
| 语义重写 | `internal/state`（v1 → v2）、`internal/planner`（有序决策 §9）、`internal/apply`+`executor`（§11 流水线） | §8、§9、§11 |
| 新建 | `dot remove` 命令；非交互 `dot init`（现为交互式） | §10.1、§10.3 |
| 保留/简化 | `storage`、`lock`、`buildinfo`、`config`、`manifest`、`cli` | 基础设施与在范围命令 |

## 2. 阶段与检查点

每个检查点给出：目标 / 删或建 / 验证（→ §13 编号）/ 退出条件。

### 阶段 A — 减法（保持绿）

**CP0 · 砍非目标命令与叶子包**
- 建/删：删 `internal/add`、`internal/doctor`、CLI `add`(cli/add.go)、`doctor`(cli/doctor.go)、
  `diff`(cli/plan.go) 及其测试；重连 `cli.go` root 只保留 init/apply/status/version（help 自带）。
- 验证：`make check` 绿；`dot help` 只列在范围命令。
- 退出：树上无 add/doctor/diff 引用；LOC 明显下降。

**CP1 · 拆除非目标特性**
- 建/删：从 `apply`/`executor` 移除 backup-on-replace（未托管既有对象一律 conflict，不备份）；
  从 `manifest/desired` 移除 template 渲染（source 为字面文件/目录）；移除 `manifest/requires` 与
  `runtime` 的 Compatibility/Requirement；折叠或删除 `datakey`；随后删 `backup`/`template` 包。
- 验证：`make check` 绿（相关特性测试同步删除）。
- 退出：无 backup/template/requires/datakey 残留；apply/status 在旧 state 语义下仍可运行但特性已收窄。

### 阶段 B — 核心重写（检查点边界绿）

**B1 · paths 坍缩**（manifest/planner 依赖它，先行）
- 重写为 §7.3：HOME 展开 + 词法规范化 + 现存 ancestor symlink 解析；删 control_identity/
  control_plane/control_validation/name_semantics/identity 等；不探测 case/unicode/fs/hard-link。
- 验证：路径唯一性与边界（→ §13 #11、#12）。

**B2 · config / manifest**
- dot.toml + machine config + module.toml 解析与校验：portable/variants、match token（`os` 封闭枚举、
  distro/arch 自由串、未知平台不报错）、`modules/` 识别规则、source/example 存在性、lazy-scoped 校验。
- 验证：variant skip/fail、scope 外坏 module 不阻塞、source/example 缺失（→ §13 #3、#17、#18、#19）。

**B3 · state v2**
- 新 state 模型/编解码（§8）：拒绝 v1/corrupt/too-new、绑定绝对 HOME、安全字段集；state missing 警告续跑。
- 验证：state missing/corrupt/v1/too-new（→ §13 #14）。

**B4 · planner（决策 §9）**
- 实现有序 link 判定（§9.1 五步）+ local（§9.2）+ stale prune 的 resolved/raw 守卫。
- 验证：source 内容变更 no-op、增删 placement、target 改变先建后删、local keep、adopt/conflict、
  parent symlink 漂移拒绝（→ §13 #4、#5、#6、#9、#10、#11）。

**B5 · executor + mutation 编排（§11）**
- 不可覆盖创建、删除前 resolved/raw 复核、先建后 prune、改后重读入 state、末尾原子提交；把 `runtime`
  编排并入 §11 线性流水线，去掉 MutationSession/precondition 框架，单把 advisory lock。
- 验证：首次收敛后重复 apply 零 mutation；各中断点重跑收敛（→ §13 #1、#7、#13）。

**B6 · CLI：init / status / apply / remove**
- 非交互 `init`（§10.1，`--profile` 必填）、`apply`（§10.2 全量 + scoped + 加 extra）、
  **新建** `remove`（§10.3）、`status` 与 `--dry-run`（§10.4）、退出码（1 vs 2）。
- 验证：init preflight 零 mutation、apply extra 激活重复 no-op、remove 语义、profile module remove 拒绝、
  并发第二进程失败、dry-run 零写入、profile 引用已删 module 失败（→ §13 #2、#7、#8、#15、#16）。

### 阶段 C — 验收与文档

**C1 · 全量验收**
- 跑通 §13 全部 1–19，含"追加一次相同 apply 断言无新 mutation"；合成 HOME/repo/config/state/lock。
- 本机 `make check` 绿 + 交叉编译（macOS/Linux）+ CI 证据，分别报告。

**C2 · 文档收口**
- README/AGENTS 只描述已实现能力；清理对已删能力的残留引用；最终 `make check`。

## 3. 顺序约束与风险

- **B1 先于 B2/B4**：manifest 与 planner 依赖 paths 的解析语义。
- **B3 state v2 是断裂点**：cutover 时按 §8 人工归档旧 v1 state，不做自动迁移；本机真实 state 不参与
  开发（用合成 fixture）。
- **CP1 是耦合拆除，非纯删**：backup/template/requires 深入 apply/executor/manifest，须连带改调用点。
- **runtime 并入而非保留**：其"可信运行上下文/MutationSession"是基线明令禁止的编排层，B5 化简为 §11 的
  线性步骤，勿原样搬运。
- **init 由交互改非交互**：现有交互式 init 的 TTY 决策路径整体替换为 flag 驱动。

## 4. 完成定义（DoD）

- §13 的 1–19 全绿；重复 apply 零 mutation 断言通过。
- 无 add/backup/doctor/template/requires/diff 残留；无 state v1 代码路径；无 case/unicode/fs 探测。
- `make check` 本机绿，并有 macOS/Linux 交叉编译与 CI 的真实证据。
- README/AGENTS 与实现一致；如实现反馈要求扩基线，走 §14 流程（先给失败案例、成本与不实现后果）。
