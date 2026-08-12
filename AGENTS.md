# 仓库协作指南

## 沟通

- 默认使用中文，先给结论，再给必要依据。
- 不确定时说明假设与风险；低风险歧义可合理假设，高风险或不可逆操作先确认。
- 不编造文件、命令、测试结果、执行结果或项目约定。

## 仓库地图

- `docs/README.md`：按读者任务组织的知识库入口与页面职责边界。
- `docs/getting-started.md`、`docs/concepts/`、`docs/glossary.md`：教程、心智模型与术语解释，
  不建立产品契约。
- `docs/development/README.md`：开发导航，把变更路由到规范、架构、测试和贡献流程。
- `docs/spec/README.md`：产品规范索引和规则 owner；`docs/spec/**` 整体是唯一产品契约。
- `docs/architecture/`：内部结构、依赖方向和测试所有权。
- `docs/decisions/`：只记录难以逆转且需要保留理由的 ADR。
- `README.md`：用户入口与当前已实现能力。
- `CONTRIBUTING.md`：变更分级、开发流程、Git 与交付约定。
- `Makefile` 与 `make help`：开发命令的事实来源。
- 代码、测试和 CI：当前实现状态的证据，不能用目标设计代替。
- `docs/archive/README.md`：历史恢复指针，没有规范性。

实现与规范冲突时修复实现和测试。需要改变公开行为、持久格式、ownership、清理语义或接受
风险时，先更新对应规范 owner；内部 package、类型和算法由最简单的实际实现决定。

## 开发原则

- 修改前检查工作区状态以及相关规范、代码和测试。
- 保留任务开始时已有的 staged、unstaged 和 untracked 内容。
- 只做任务所需改动，不做无关重构、格式化或未来能力预建。
- 修复问题先定位根因；不以静默 fallback、吞错或 mock 成功路径掩盖问题。
- 重复逻辑、共享校验、API 契约漂移、状态同步或数据安全问题出现时，先找不变量，并让它只在
  一个地方表达。
- 只有真实复用、清晰职责或必须集中表达的不变量才能引入抽象。
- 优先标准库和已有依赖；新增或替换依赖前说明维护、版本和替换成本。
- Go 代码保持短函数、浅嵌套和明确数据流；注释解释意图与边界，不复述代码。

## 私人数据与 mutation 隔离

- 任务未明确涉及时，不读取或修改真实 `modules/`、`*.local`、`.env`、machine config、
  state、lock 或用户 HOME。
- 受版本管理的 example 和隔离临时目录中的合成 fixture 不受此限制。
- 手动验证 `init`、`apply`、`remove` 等 mutation 命令时，必须同时使用绝对临时 HOME、
  repository、config、state 和 lock，并清除或重定向会影响解析的环境变量。
- 仅覆盖 HOME 不算完整隔离。
- 测试使用 `t.TempDir` 和真实文件系统行为，不写真实用户目录。
- 重复收敛场景须再次执行相同 apply，并断言没有新的文件系统 mutation。

## 权限与工作区保护

- 删除、移动、重命名、批量改写和其他可能丢失内容的操作，范围不明确时先确认。
- 不覆盖、回滚或清理用户已有改动，除非用户明确授权。
- Repo-tracked 修改实行 branch-first。处于干净且已确认与远端一致的 `main` 时，实现请求默认
  授权按 `CONTRIBUTING.md` 的统一规则创建并切换 `<type>/<slug>`；若 `main` dirty、ahead、
  behind 或 diverged，停止并报告，不自动同步或改写。
- Git 操作按三阶段授权：实现包含创建或切换本地任务分支、编辑和测试；提交包含只暂存当前
  任务文件并 commit；发布或开 PR 包含 push 当前任务分支并创建 Draft PR。
- Ready、启用 auto-merge、立即 merge、release、真实机器 mutation、本地 `main` 同步和
  非自动分支清理仍分别授权。
- Agent 的默认交付终点是等待 CI 和人工审阅的 Draft PR，不自行 merge。
- 真实机器 mutation 属于独立的 operational 授权，代码修改授权不包含它。

## 验证与交付

- 按 `CONTRIBUTING.md` 判断变更类型并满足对应门禁。
- 任意仓库改动都检查完整 diff、相关 untracked 和 `git diff --check`。
- 开发中运行 focused tests 或 `make test`；Go、依赖、构建或 CI 改动在发布前完整运行一次
  `make check`。
- Mutation 验证只使用合成绝对路径；分别报告本机、交叉平台和远程 CI 的真实证据。
- 无法执行的验证必须列为未验证项，不以推测或旧结果代替。
- 二进制只能写入已忽略的 `bin/`、`dist/` 或仓库外临时目录。
