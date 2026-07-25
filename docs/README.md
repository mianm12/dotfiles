# dot 文档

## 当前入口

- [`spec/README.md`](spec/README.md)：唯一产品与行为规范集合，以及每条规则的 owner。
- [`architecture/overview.md`](architecture/overview.md)：当前实现结构和依赖方向。
- [`architecture/testing.md`](architecture/testing.md)：测试所有权与机械门禁。
- [`decisions/README.md`](decisions/README.md)：未来 ADR 的适用边界。
- [`../CONTRIBUTING.md`](../CONTRIBUTING.md)：变更分级、验证证据与 Git 交付。
- [`archive/README.md`](archive/README.md)：只读历史 Git 指针。

仓库根 [`README.md`](../README.md) 说明当前可用入口；代码、测试与 CI 是当前实现状态的
证据，不能替代规范。

## 权威规则

- `docs/spec/**` 整体是唯一产品规范；每条规则只能由一个文件拥有。
- 其他文档只链接 owner，不复制产品定义。
- 内部 package、依赖方向和测试组织属于 architecture，不构成产品行为。
- 难以逆转且需要长期保留理由的选择使用 ADR；不为历史选择补写 ADR。
- 历史内容没有规范性，不得指导新增代码或兼容行为。
