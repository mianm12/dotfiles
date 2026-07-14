# dot — 个人 dotfiles 管理工具 · 设计文档集

`dot` 是一个单人使用的 dotfiles 管理 CLI,采用「symlink 为主、模板生成为辅」的混合模型。
CLI 源码与配置内容同仓库存放,通过 GitHub Releases 分发二进制,通过 git 同步配置。

## 文档目录

| 编号 | 文档 | 内容 | 读者时机 |
|---|---|---|---|
| 01 | [overview.md](01-overview.md) | 目标、非目标、术语表、关键决策记录(ADR) | 先读,建立共同语言 |
| 02 | [architecture.md](02-architecture.md) | 组件划分、仓库布局、磁盘路径约定、数据流、Go 包结构 | 动手写代码前 |
| 03 | [manifest-spec.md](03-manifest-spec.md) | 顶层/模块两级 manifest 的完整字段规范与合并语义 | 实现 `internal/manifest` 时 |
| 04 | [cli-spec.md](04-cli-spec.md) | 全部命令、flag、退出码、输出格式 | 实现 `internal/cli` 时 |
| 05 | [apply-engine.md](05-apply-engine.md) | 期望状态模型、state.json、apply 算法、add 反向映射、安全策略 | 实现 planner/executor 时 |
| 06 | [templates.md](06-templates.md) | managed / scaffold 双模板语义、变量、函数、drift 检测 | 实现 `internal/tmpl` 时 |
| 07 | [bootstrap-and-release.md](07-bootstrap-and-release.md) | bootstrap.sh、版本策略、requires 约束、update/self-update、git 透传 | 搭发布流水线时 |
| 08 | [testing.md](08-testing.md) | 幂等性契约、--home 重定向、单测/集成/golden 测试 | 与功能开发同步 |
| 09 | [roadmap.md](09-roadmap.md) | M1/M2/M3 里程碑、明确砍掉的功能、风险 | 排期时 |

## 一段话看懂整体设计

仓库根目录是一个标准 Go 项目(`cmd/`、`internal/`),配置内容集中在 `modules/` 下,每个子目录是一个
**模块**,目录内部镜像其在目标机器上的路径结构。顶层 `dot.toml` 负责跨模块事务(profiles 分组、
全局默认值、CLI 最低版本 `requires`),模块内可选的 `dot.toml` 负责模块自身事务(OS 过滤、
target 覆盖、文件级声明、hooks)。`dot apply` 计算期望状态并与文件系统及 state 清单对比,
以文件级 symlink 为默认动作;`.tmpl` 文件每次渲染(managed),`.template` 文件仅首次生成
(scaffold),私有内容通过 `*.local` 约定留在机器本地。新机器由极薄的 `bootstrap.sh` 完成
二进制下载、仓库克隆并移交 `dot init`。

## 文档约定

- 规范用词遵循 RFC 2119:**必须(MUST)**、**不得(MUST NOT)**、**应当(SHOULD)**、**可以(MAY)**。
- 标注 `[M2]` / `[M3]` 的内容属于后续里程碑,M1 实现时只需保证格式不与之冲突。
- 所有示例路径以 macOS 为准,Linux 差异处会显式标注。
