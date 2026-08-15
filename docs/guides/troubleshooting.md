# 故障排查

排查 `dot` 时先识别输入事实，不要从一条错误文本直接跳到删除 target 或 state。大多数问题都
能映射到[工作模型](../concepts/mental-model.md)的四类输入：repository desired、machine
selection、ownership state 和 actual filesystem。

## 安全的第一组命令

```sh
dot version
dot paths
dot status
dot apply --dry-run
```

- `version` 确认正在运行哪个构建；
- `paths` 只显示当前 invocation 的 HOME、machine config、state 和 lock 路径；
- `status` 与 dry-run 使用同一条循环；
- status 能看完时，即使有 `skip` 也返回成功；含 `skip` 的 dry-run 则返回 `1`。

不要为了“看得更清楚”先运行真实 apply。公开输出和退出码的精确定义见
[CLI 规范](../spec/cli.md)。

## 按现象定位

| 现象 | 先检查 | 正确方向 |
| --- | --- | --- |
| 提示未初始化 | `dot paths` 中的 machine config 是否存在 | 使用正确绝对 repository 运行 `dot init` |
| 命令返回 `2` | flag、位置参数和子命令拼写 | 阅读 `dot help COMMAND`；不要把它当运行时失败 |
| Config/manifest 解析失败 | `dot.toml`、effective `module.toml` 类型与 TOML | 修复 owner 文件；inactive manifest 不应被误当作已加载 |
| `skip` | module、placement、target 和 reason | 检查 actual target 与 ownership；不要 force 覆盖 |
| 控制路径重叠或 target 越界 | `dot paths` 的绝对控制路径及 target | 消除三个前缀重叠，不要移动错误提示本身 |
| applicability 为 `indeterminate` | reason 中缺失的 OS/distro/arch 证据 | 修复平台检测条件，或收缩 direct selection；不要伪装成 not-applicable |
| `warning: state is missing` | 是否首次 apply，state 是否被人工移除 | 可收敛当前 desired，但先接受无法发现已删除 manifest 历史 link 的边界 |
| stale `forget` | reason 与 actual target 是否漂移/保留 | 把它理解为放弃证据，不是删除失败 |
| lock busy | 是否确有另一个 `dot` mutation 进程 | 等待真实进程结束；不要盲删仍在使用的 lock |
| 提示 mutation 可能部分完成 | 已输出的已完成行、当前 status/dry-run | 保持 desired，重跑完整 apply；不要手工执行剩余步骤 |
| Repository 移动或绑定失效 | `dot paths` 后读取 machine config 的 `repository` | 人工修正绝对路径，或在明确清理配置后重新 init；产品不自动 rebind |

## Target skip

一个 active link 遇到普通文件、目录、special 或未知/漂移 symlink 时会 `skip`。Manifest 表达
“希望这里是什么”，不证明“已有东西可以覆盖”。

排查顺序：

1. 从 `skip` 行记录 module、placement 和绝对 target；
2. 阅读对应 `module.toml`，确认 desired source 与 target；
3. 用 `lstat` 语义检查 target 本身，不要因 shell 跟随 symlink 而误判；
4. 判断已有数据由谁维护、是否含秘密、是否需要仓库外备份；
5. 人工解决归属后重新 status/dry-run。

不要添加 force、自动 backup 或把任意现有文件记为 owned 的 fallback；这些能力不在当前产品范围。
完整决策顺序见 [planning 规范](../spec/planning.md#link)。

## Selection 看起来不对

`dot status` 的 `selection` 字段区分 profile、extra 或两者。常见原因：

- `select remove` 只移除了 extra，但 active profile 仍包含 module；
- 手工编辑了 repository profile，却没有在目标机器 apply；
- 编辑了错误 HOME 下的 machine config；
- Module not-applicable，因此被选择但不产生 active placements；
- 某台机器还没有拿到包含最新 `dot.toml` 的 repository revision。

先运行 `dot paths` 确认机器配置，再按[profiles 与平台指南](profiles-and-platforms.md)检查 selection
来源。`apply` 永远不会替你修改 selection。

## State missing、remove 与 forget

- **State missing**：按空账本观察当前 desired，但失去发现已经从 manifest 删除的历史 link 的
  能力；不要把这条 warning 当成“自动恢复了全部历史 ownership”。
- **Remove**：只有 stale link 的 raw dest 仍匹配账本时才删除。
- **Forget**：Actual 已漂移或词法上不安全时只放弃账本并保留数据。

不要手工拼写、降级版本或删除 state 来消除提示。State schema 与安全字段见
[state 规范](../spec/state-and-ownership.md)；如果确需处理旧格式，先在 repository 外归档并按
当前规范明确决策。

## Control path 问题

Machine config、state 和 lock 不能与 repository 树或 managed target 词法重叠。错误会提示运行：

```sh
dot paths
```

检查输出的实际绝对路径前缀，不要只核对默认字符串。不要把 module target 指向 `.config/dot`、
state root 或 repository 内部。详细关系见
[placements 规范](../spec/placements.md#control-path-topology)。

## 部分完成与恢复

Apply 在 lock boundary 校验后获取 lock，只在锁内再跑一次循环；
但它不提供跨 target 事务。
I/O 失败、外部并发变化、state commit 或 lock release 失败都可能发生在部分写入之后。

恢复时：

```sh
dot status
dot apply --dry-run
dot apply
dot apply --dry-run
```

保持同一 desired，允许新的观察从真实文件系统继续收敛。不要只执行某个内部步骤，也
不要因为 target 看起来正确就跳过 state commit 的确认。详见
[mutation 与恢复规范](../spec/mutation-and-recovery.md#中断恢复)。

## 提交可复现的问题

提供最小、脱敏且可验证的证据：

- `dot version`；
- 操作系统与架构；
- 实际命令和退出码；
- `dot status` / dry-run 的循环行与 stderr 提示；
- 最小合成 `dot.toml`、`module.toml` 和临时目录树；
- 预期行为及其对应 spec owner。

路径中可能含用户名，提交前脱敏。不要上传真实 local 内容、秘密、完整私人 repository、machine
config 或 state。开发者应先把问题转化为使用临时 HOME/repository/config/state/lock 的合成
回归测试，验证层次见[测试架构](../architecture/testing.md)。
