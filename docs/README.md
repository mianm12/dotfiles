# dot — 个人 dotfiles 管理工具 · 设计文档集(v1.1)

`dot` 是一个单人使用的 dotfiles 管理 CLI,采用「symlink 为主、模板生成为辅」的混合模型。
CLI 源码与配置内容同仓库存放,通过 GitHub Releases 分发二进制,通过 git 同步配置。

## 文档目录

| 编号 | 文档 | 内容 | 读者时机 |
|---|---|---|---|
| 01 | [overview.md](01-overview.md) | 目标、非目标、术语表、关键决策记录(ADR) | 先读,建立共同语言 |
| 02 | [architecture.md](02-architecture.md) | 组件划分、仓库布局、磁盘路径约定、pipeline、核心类型 | 动手写代码前 |
| 03 | [manifest-spec.md](03-manifest-spec.md) | 两级 manifest 完整字段、两阶段加载、合并与 ignore 语义 | 实现 `internal/manifest` 时 |
| 04 | [cli-spec.md](04-cli-spec.md) | 全部命令、flag、退出码、输出格式 | 实现 `internal/cli` 时 |
| 05 | [apply-engine.md](05-apply-engine.md) | 所有权谓词、决策表、不变量校验、执行顺序、add 算法 | 实现 planner/executor 时 |
| 06 | [templates.md](06-templates.md) | managed / scaffold 双语义、变量命名空间、drift 展示 | 实现 `internal/tmpl` 时 |
| 07 | [bootstrap-and-release.md](07-bootstrap-and-release.md) | bootstrap.sh(带校验)、版本铰链、兼容性矩阵、同步流程 | 搭发布流水线时 |
| 08 | [testing.md](08-testing.md) | 幂等契约、--home 全隔离、决策表/集成/golden 测试 | 与功能开发同步 |
| 09 | [roadmap.md](09-roadmap.md) | M1/M2/M3 里程碑、砍掉清单、风险 | 排期时 |

## 一段话看懂整体设计

仓库根目录是一个标准 Go 项目(`cmd/`、`internal/`),配置内容集中在 `modules/` 下,每个子目录是一个
**模块**,目录内部镜像其在目标机器上的路径结构(模块内 `hooks/` 为保留目录,存放脚本与其数据,
不参与链接)。顶层 `dot.toml` 负责跨模块事务(profiles 分组、全局默认值、CLI 最低版本 `requires`),
模块内可选的 `dot.toml` 负责模块自身事务。`dot apply` 经「严格解码 → 枚举 → 渲染 → 观测 → 纯决策 →
不变量校验 → 执行」的 pipeline 产生并执行动作:文件级 symlink 为默认;`.tmpl` 每次渲染(managed),
`.template` 仅首次生成(scaffold);私有内容通过 `*.local` 约定留在本机。**创建先于清理**的执行顺序、
**所有权谓词**门禁一切破坏性动作、**收养规则**让崩溃后自愈。新机器由带校验的 `bootstrap.sh`
完成二进制安装、仓库克隆并移交 `dot init`。

## v1.1 修订摘要(相对 v1.0)

1. **prune 作用域**:部分 apply 只清理所涉模块的孤儿;引入「state 条目必属当前 profile」不变量;整模块孤儿需确认(05 §5、04 §4.2)。
2. **所有权谓词 `owned()`**:state 记录不再等同所有权;被手工改指的链接判为 CONFLICT-drift 而非静默修复(05 §3)。
3. **执行顺序反转**:`mkdir → 创建 → prune → hooks`,消灭「改名场景下旧配置先删、新配置失败」的丢失窗口;本次运行出错则跳过 prune(05 §6,ADR-13)。
4. **收养规则**:与 desired 一致但无 state 记录的产物补录,崩溃后重跑自愈;单实例 flock 锁(05 §4/§7)。
5. **全局不变量校验**:target 重复、前缀/祖先冲突、`foo` 与 `foo.tmpl` 去后缀碰撞 → 整体拒绝执行(05 §5)。
6. **hooks/ 保留目录**:hook 脚本与 Brewfile 移入模块内 `hooks/`,不被链接;run_once 支持 `watch` 依赖文件 [M2](03 §3、05 §8)。
7. **manifest 两阶段加载**:宽松预读 `requires` → mutation 命令严格解码,未知键即错(失效安全第二道防线)(03 §7)。
8. **模板收紧**:砍掉 `env` 函数,改为 `[data].from_env` 于 init 快照;内建变量大写/用户变量小写强制命名空间(06 §3/§4)。
9. **`*.local` 加固**:`dot add` 硬拒绝、doctor 检查 `git ls-files`、机器配置/state/backup 收紧权限(06 §2、05 §7)。
10. **其他**:bootstrap 补 checksum 校验与原子安装;drift diff 降级为「实际文件 vs 本次渲染」;`--rebuild-state` 移出 doctor;幂等契约措辞精确化;`--home` 隔离扩展到 hook 子进程环境;run_once 最小实现提前进 M1。

## 文档约定

- 规范用词遵循 RFC 2119:**必须(MUST)**、**不得(MUST NOT)**、**应当(SHOULD)**、**可以(MAY)**。
- 标注 `[M2]` / `[M3]` 的内容属于后续里程碑,M1 实现时只需保证格式不与之冲突。
- 所有示例路径以 macOS 为准,Linux 差异处会显式标注。
