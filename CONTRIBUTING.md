# 贡献约定

这是个人项目，但开发过程按可审阅、可回退、持续可验证的方式组织。Agent 的默认交付状态是
“Draft Pull Request 已准备好等待 CI 和人工审阅”，不是自行合入。

## 变更分级

先按最高适用风险选择门禁；一项变更可以同时具有多种性质。

| 类型 | 适用范围 | 必须门禁 |
| --- | --- | --- |
| Routine | 文档、测试、内部重构和普通行为变化 | focused tests；Go 或 CI 变更发布前运行 `make check`；行为变化先改规范并增加对应回归测试；自审完整 diff |
| Safety-critical | 路径边界、计划、mutation、清理、状态、锁和数据完整性变化 | 失败模式清单；完全隔离的合成环境；重复 apply 零 mutation；相关 fuzz；独立缺陷审查；双平台 CI |
| Operational | 对真实机器、真实配置或发布/仓库治理产生影响 | 上述全部；影响报告；回滚方式；用户对该外部操作的单独明确授权 |

Routine 由实现者完成 diff 自审；跨多个规范域或影响面较大的行为变化建议增加独立契约审查。
Safety-critical 必须由另一位评审者或单独的审查任务主动寻找反例、越界 mutation 和错误
恢复缺陷。

## 日常开发流程

1. 从 [`docs/README.md`](docs/README.md) 按任务进入知识库；开发变更可从
   [`docs/development/README.md`](docs/development/README.md) 定位本次规则的唯一 owner，判断
   风险等级和失败边界。
2. Repo-tracked 修改从短生命周期分支开始；Agent 在干净且已确认与远端一致的 `main` 上默认
   按“分支与提交”的统一规则创建 `<type>/<slug>`。若 `main` dirty、ahead、behind 或
   diverged，停止并报告。
3. 修复缺陷时先构造脱敏的最小合成复现，再实现满足当前目标的最小变化并补充对应测试。
4. 开发中运行 focused tests 或 `make test`；发布前运行风险等级要求的完整门禁并检查 diff、
   untracked 和未验证项。
5. Push 短期分支并创建 Draft Pull Request；所有进入 `main` 的变更，包括纯文档，都通过 PR。
   Ready、auto-merge 和立即 merge 需要用户分别明确授权。

普通修复、功能和重构不创建常驻 Goal。只有任务预计跨会话、至少包含三个相互依赖的
里程碑、存在重大未知，或涉及真实迁移时，才使用 `/plan` 或 `/goal`。复杂计划保存在
Issue、Pull Request 或任务会话中；长期结论只沉淀到规范、ADR、代码和测试，主分支不保存
completed plans。

修复—复审最多两轮。第二轮后必须把剩余发现分类为当前 blocker、后续任务或明确接受的
风险，不通过无限扩大当前 Pull Request 来追求抽象上的完美。

## 规范与反馈

改变用户行为、持久化格式、所有权/清理语义、安全不变量或已接受风险时，先更新规范。
内部拆分和实现手段由 architecture 文档、代码和测试表达。

教程、任务指南、概念、术语表和 README 是面向读者的解释与导航层，不建立第二份产品契约。
文档变更先按 [`docs/README.md`](docs/README.md) 的信息架构确认页面职责：精确规则只进入对应 spec
owner，内部结构只进入 architecture，难以逆转的理由才进入 ADR。所有新增页面必须从已有入口
可达，并链接回更权威的依据。

同类反馈第二次出现时，不再只留在对话或 review comment 中：

- 行为契约进入规范；
- 已发生缺陷进入脱敏回归测试；
- 跨 package 不变量进入最小的跨层行为测试；
- 可机械判断的风格或安全规则进入 lint；
- 只影响 Agent 导航与边界的规则进入最近的 `AGENTS.md`。

只有流程已经稳定、会重复执行，并且确实需要专项脚本或参考资料时，才考虑创建 skill。普通
开发流程不包装成项目级 skill。

## 分支与提交

- `main` 是唯一长期分支，保持可构建、可测试且不直接 push；所有集成通过 Pull Request。
- 人工和 Agent 短期分支统一使用 `<type>/<slug>`。`type` 为 `feat`、`fix`、`docs`、
  `refactor`、`test`、`ci`、`chore` 或 `deps`；`slug` 使用简短的小写 kebab-case，
  表达分支的主要变更意图。
- 分支必须包含 `slug`，不创建 `feat`、`fix`、`docs` 等裸父级分支。`type` 只表示分支的
  主要意图，不要求与其中每个提交的类型完全一致。
- Dependabot 等机器人按平台规则生成的分支不受上述命名约束。
- 只使用 squash merge，不使用 merge commit 或 rebase merge。
- GitHub 合入后自动删除远端短期分支；本地分支另行清理。
- 当前不建立版本分支；只有出现并行维护的已发布版本时再引入。

示例：

```text
fix/control-path-overlap
docs/branch-naming
deps/go-minor-update
```

Git 操作按三阶段授权：实现包含创建或切换本地任务分支、编辑和测试；提交包含只暂存当前任务
文件并 commit；发布或开 PR 包含 push 当前任务分支并创建 Draft PR。Ready、auto-merge、
立即 merge、release、本地 `main` 同步和非自动分支清理不由上述授权推导。

提交标题使用 `type(scope): 中文简短摘要`，例如：

```text
feat(cli): 实现版本命令与兼容性检查
fix(paths): 拒绝相对仓库路径
test(apply): 覆盖未收敛时延迟清理
```

一次提交只表达一个可解释变化，并尽量保持可构建、可测试。依赖更新、功能实现、测试补充和
流程调整不应无理由堆进同一提交。

## Pull Request 证据

Pull Request 使用仓库模板，至少说明摘要、风险等级、行为或规范影响和实际验证。Routine
没有额外说明时可以删除 Notes；Safety-critical 和 Operational 必须在 Notes 中列出失败模式、
未验证项和回滚或前向修复方式。

`make check` 是 Go 变更的本地统一入口，CI 在 macOS 与 Linux 上运行同一入口。`make vuln`
使用固定版本的 `govulncheck`，对应相关 Go Pull Request、每周计划和手动触发的非 required
远程 workflow，但不加入本地离线 `make check`。任何一种 Git 或外部系统授权都不会自动扩大
为 merge、release 或真实机器 mutation 的权限。
