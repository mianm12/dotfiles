# 仓库协作指南

## 沟通

- 默认使用中文，先给结论或结果，再给必要依据。
- 不确定时说明假设和风险；低风险歧义可合理假设并继续，高风险或不可逆操作先确认。
- 不编造文件、命令、测试结果、执行结果或不存在的项目约定。

## 真相源

- `docs/design-baseline.md` 是唯一当前产品与行为契约。
- `docs/cutover-plan.md` 是非规范性的重构切换清单，规定当前工程阶段与顺序；与基线冲突时
  以基线为准。
- `README.md`、代码和测试说明当前已经实现什么，不能用目标设计代替实现证据。
- `docs/archive/` 仅用于历史追溯，没有规范性，也不得指导新增实现。
- `CONTRIBUTING.md` 规定 Git 和通用开发约定。

实现与基线冲突时修复实现和测试；需要改变公开行为、持久格式、ownership、清理语义或接受
风险时，先更新基线。内部 package、类型和算法由最简单的实际实现决定。

## 执行原则

- 修改前先检查工作区状态、相关代码和测试，保留任务开始时已有的 staged、unstaged 和
  untracked 内容。
- 只做任务所需的最小改动，不做无关重构、无关格式化或未来能力预建。
- 修复问题先定位根因；不要用静默 fallback、吞错或 mock 成功路径掩盖问题。
- 只有真实复用、清晰职责或必须集中表达的不变量才能引入抽象。避免无依据的 interface、
  helper、层次和防御性代码。
- 删除、移动、重命名、批量改写、Git 状态变化和其他不可逆操作遵循用户当前授权；对象或
  范围不明确时先询问。

## 私人数据与 mutation 隔离

任务未明确涉及时，不读取或修改真实 `modules/`、`*.local`、`.env`、machine config、state、
lock 或用户 HOME。受版本管理的 example 和隔离临时目录中的合成 fixture 不受此限制。

手动验证 `init`、`apply`、`remove` 等 mutation 命令时，必须同时使用绝对路径的临时 HOME、
repository、config、state 和 lock，并清除或重定向可能影响解析的环境变量。仅覆盖 HOME 不算
完整隔离。

测试使用 `t.TempDir` 和真实文件系统路径行为，不写真实用户目录。涉及重复收敛的场景必须在
首次成功后再次执行相同 apply，并断言没有新的文件系统 mutation。

## 实现边界

- 目标平台是 macOS 和 Linux；Windows 不在范围内。
- MVP 范围和非目标完全以设计基线为准，不为历史能力保留兼容层。
- 优先标准库和已有依赖。新增或替换依赖前说明维护状态、Go 版本、`go.mod`/`go.sum` 影响和
  替换成本。
- Go 代码保持短函数、浅嵌套和明确数据流；注释解释设计意图与边界，不复述代码。
- 二进制只能写入已忽略的 `bin/`、`dist/` 或仓库外临时目录。
- `Makefile` 和 `make help` 是开发命令的当前事实来源。

## 验证与交付

- 任意仓库改动都检查完整任务 diff、相关 untracked 和 `git diff --check`。
- Go、依赖、构建或 CI 改动运行 `make check`；无法在当前平台执行的验证要明确标注未运行。
- mutation 测试只使用合成 fixture，并分别报告本机、交叉编译和远程 CI 的真实证据。
- Branch、stage、commit、push、Pull Request、merge 和 release 按 `CONTRIBUTING.md` 及用户当前
  授权执行；一种 Git 授权不自动扩大到其他操作。
