# 产品规范索引

`docs/spec/**` 整体是 `dot` 唯一具有规范性的产品与行为契约。README、代码、测试与 CI 说明
当前实现状态，不能替代产品规范。

## 权威边界

每条产品规则只由一个文件拥有。其他规范需要相关规则时只链接到 owner，不复制定义；出现
冲突时以 owner 文件为准。

| 文件 | 唯一职责 | 主要依赖 |
| --- | --- | --- |
| [`product.md`](product.md) | 产品目标、范围、非目标和接受风险 | 无 |
| [`selection.md`](selection.md) | 仓库 desired、机器选择、profile、platform applicability 与 module resolution | product |
| [`placements.md`](placements.md) | link/local 声明、source、target、路径关系与 control-path topology | product、selection |
| [`state-and-ownership.md`](state-and-ownership.md) | state schema、版本、安全字段和 ownership 证据 | product、placements |
| [`planning.md`](planning.md) | 实际文件系统观察与 create/update/prune/keep/forget action eligibility | selection、placements、state |
| [`mutation-and-recovery.md`](mutation-and-recovery.md) | mutation 顺序、锁、完成边界、提交和中断恢复 | planning、state |
| [`cli.md`](cli.md) | 公开命令、命令 scope、公开投影和退出码 | selection、planning、mutation |

内部 package、依赖方向和测试组织不属于产品契约，见 [`../architecture/`](../architecture/)。
难以逆转且需要长期保留理由的工程选择使用 [`../decisions/`](../decisions/) 中的 ADR。

## 修改规则

- 改变用户行为、持久格式、ownership、清理语义、安全不变量或已接受风险时，先修改对应
  owner，再修改实现和测试。
- PR 引用具体规范文件和标题。
- 不给普通段落和句子分配永久编号。
- 术语或示例可以在 owner 中解释一次；其他文件用链接导航。
- 规范描述目标行为，代码和测试仍是“当前是否已经实现”的证据。
