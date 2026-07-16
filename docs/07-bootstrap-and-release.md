# 07 · Bootstrap、版本与发布

## 1. 问题定义

同一仓库承载两种变更频率不同的产物:**CLI(构建产物,低频)** 与 **配置(仓库内容,高频)**。
因此分发渠道分离——二进制走 GitHub Releases,配置走 git——并用顶层 manifest 的
`requires` 字段 + 严格解码(ADR-16)做双重兼容性铰链。git tag 只是 CLI 的里程碑,
配置更新就是普通 commit。

## 2. bootstrap.sh

职责严格限定为“安装受校验的 CLI → 准备仓库 → 移交 init”,并支持通过
`curl -fsSL https://raw.githubusercontent.com/<user>/dotfiles/main/bootstrap.sh | sh` 调用。
脚本本身是实现文件,本文不复制其源码;它必须满足:

1. 只接受明确支持的 GOOS/GOARCH,无法映射时在下载前报错。
2. 下载目标资产与发布的 checksums,精确选择对应条目并在解包、安装前完成校验;
   缺少条目、校验工具或校验失败都必须停止。
3. 下载、解包和验证发生在临时位置;新二进制完整可用后才在安装目录原子替换旧版本。
   任一步失败不得破坏已安装的可用二进制。
4. 仓库目录不存在时 clone;已存在且是 git 仓库时复用;已存在但不是预期 git 仓库时
   明确失败,不得覆盖目录内容。
5. 成功后调用 `dot init` 并透传其结果,由 CLI 接管 profile、机器配置和 apply。

脚本不得从源码编译、安装 Homebrew/软件或修改 shell rc。运行前应检查并准确报告缺失依赖;
基线依赖为 POSIX `sh`、`curl`、`git`、`tar`、可用的 SHA-256 校验工具及常规 POSIX
文件工具。是否使用 `install` 等便利命令由实现决定,不得成为未声明的隐式依赖。

## 3. 版本策略

- CLI 版本:semver `vMAJOR.MINOR.PATCH` git tag;tag 发布必须提供
  `darwin/arm64`、`darwin/amd64`、`linux/amd64`、`linux/arm64` 的 tar.gz 资产与
  `checksums.txt`,并让二进制报告对应版本和构建元数据。具体发布工具不属于规范。
- **本地开发构建**(没有发布版本元数据)version 为 `dev`:requires 检查放行 +
  打印警告。
- 配置「版本」即 git commit,无独立编号。
- `requires`(顶层 manifest):声明**本套配置需要的最低 CLI 版本**,仅 `>=x.y.z` 语法
  (ADR-11)。维护纪律:**配置改动一旦用到新版 CLI 的能力,同一 commit 必须提升
  requires**——唯一需要人肉自觉的点,写进 README;遗忘时由严格解码兜底(§4)。

## 4. 兼容性矩阵与铰链行为

| 场景 | 行为 |
|---|---|
| 新 CLI + 旧配置 | 必须可用(manifest 向后兼容原则,03 号文档 §8) |
| 旧 CLI + 新配置,requires 满足且无未知键 | 正常工作 |
| 旧 CLI + 新配置,requires 不满足 | pipeline 第②步(宽松预读)即拒绝,提示 `dot self-update`;`self-update`、`git`、`version`、`doctor` 不受拦截。若由 update 拉入,仓库不自动回滚(ADR-34) |
| 旧 CLI + 新配置,**requires 忘记提升**但含未知字段 | 严格解码(第③步)拒绝 mutation 命令——失效安全的第二道防线;`doctor` 宽松模式可诊断;update 已完成的 pull 不回滚 |
| **回滚二进制后 state 版本过新** | mutation 命令 fail closed(ADR-25,05 号文档 §2);提示升级 CLI 或手动处理 state |
| 本地开发构建(version=dev) | requires 放行 + 警告 |

两道防线的分工:`requires` 是**作者显式声明**的兼容界线,报错信息精确;严格解码是
**机械兜底**,牺牲报错友好度换取「绝不带着不理解的字段去 prune」的安全底线。

## 5. 日常同步流程

三种变更情形与对应操作:

| 情形 | 发布侧 | 消费侧(其他机器) |
|---|---|---|
| 只改配置 | `dot git commit && dot git push` | `dot update`(洁净检查 → pull → requires → apply) |
| 只改 CLI | push 代码,打 tag 发 Release | `dot self-update`(配置无需动) |
| 都改且配置依赖新 CLI | 同一 PR:代码 + 配置 + 提升 requires;先发 Release 再 push main | `dot update` 被 requires 拦下 → `dot self-update` → 再 `dot update` |

`dot update` 细节:**自持锁起序贯执行**——取锁 → **洁净检查**(working tree 与 index
必须完全为空,含未跟踪文件;`--ff-only` 遇到不冲突的本地内容仍可能成功,随后 apply
会读到新旧混合的仓库,故必须前置硬检查;非空报错提示走 `dot git`)→
记录旧 commit → `git pull --ff-only`(分叉即报错)→ requires 检查 → apply(复用锁)。
顺序不可变。更新是原地 fast-forward:link 内容会随 pull 立即变化,失败时仓库不自动回滚,
必须报告新旧 commit 与人工恢复指引(ADR-34)。`--no-apply` 在 pull 后停止,不进入后续
requires/apply/hooks,也不是 link 内容的隔离预览;它仍可配合 `dot diff` 审查尚未执行的
动作。拉取到的新 hook 不做单独确认
(01 §4 威胁模型出界)。
`dot self-update` 必须解析 latest 或用户指定版本,在安装前完成资产校验,且只以原子替换
交接完整的新二进制;失败时旧二进制保持可用。下载、暂存和替换机制由实现决定。

## 6. git 透传

`dot git <args...>` 先取得与 mutation 共用的锁,再执行 `git -C <repo-dir> <args...>`;
不解析子命令或 alias,继承 stdio,Git 启动后透传其退出码。锁获取等 dot 自身错误仍使用
统一退出码 1。仓库目录不存在时给出走 bootstrap 的提示。直接调用外部 Git 不受锁保护,
mutation 期间并发修改仓库属于 01 号文档 §4 的已接受第三方竞态。

## 7. 仓库公开性与安全边界

仓库可以公开:配置本身无密级,私密内容由 `*.local` 多层纵深(06 号文档 §2,ADR-32)
压低误入库风险,敏感落地面(机器配置 0600、state/backup 目录 0700)已收紧权限。
设计上**不把「私有仓库」当作任何安全边界**。bootstrap 与 self-update 均经 checksums
校验(防传输损坏);签名验证(cosign/minisign)属于供应链防御,已在威胁模型出界侧,
列 M3 可选。
