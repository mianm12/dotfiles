# 09 · 路线图与范围控制

## 1. 里程碑总览

每个里程碑收口的标准是同一句话:**当天起可以(继续)作为唯一的 dotfiles 工具日常使用**。
避免「憋大招式 v1.0」——个人项目最大的风险不是设计不好,而是写一半失去动力。

### M1 · 可用的 stow 替代(目标:第 1–2 周)

| 范围 | 说明 |
|---|---|
| 命令 | `init` `apply` `diff` `status` `add` `version` |
| 链接 | 文件级 symlink、conflict 三态、`--force` 备份、prune |
| 模板 | **仅 scaffold**(`.template`);managed 留到 M2,但后缀/决策表按本文档预留 |
| manifest | 顶层 + 模块两级、profiles、os 过滤、`[target]` 表、ignore、**requires 检查** |
| 基建 | state.json、`--home`、bootstrap.sh、goreleaser 首次发版 |
| 收口标准 | 本人主力 Mac 完全切换到 dot 管理;stow 卸载 |

requires 与 scaffold 进 M1 的原因:二者影响**文件格式与行为契约**,后补有兼容包袱;
其余功能后补皆无痛。

### M2 · 模板与同步闭环(目标:第 3–5 周)

| 范围 | 说明 |
|---|---|
| managed 模板 | `.tmpl` 渲染、drift 检测、`diff -v` 内容对比、`[data]` 声明 + init 收集 |
| 同步 | `update`、`self-update`、`git` 透传 |
| 辅助 | `doctor`(含 manifest lint、模板静态扫描)、`edit`、`--rebuild-state` |
| 收口标准 | `.gitconfig` 差异靠模板解决;第二台机器(或干净虚拟机)全流程跑通 |

### M3 · Linux 与打磨(目标:按需)

| 范围 | 说明 |
|---|---|
| Linux | 真实 Linux 机器落地:路径差异模块化、`linux-pkgs` 模块、发行版脚本 |
| hooks | `run_once` 全语义(M2 若未顺手带上则在此完成) |
| 可选项 | `--json` 输出、age 加密单文件、`--allow-outside-home`、release 签名 |
| 收口标准 | 一台 Linux 服务器/桌面由 bootstrap 一条命令拉起 |

## 2. 明确砍掉/冻结的功能

写下来是为了将来手痒时对照:

| 功能 | 处置 | 理由 |
|---|---|---|
| 深合并/模块继承 | 永久砍 | ADR-7,可预测性优先 |
| manifest 里的条件表达式/循环 | 永久砍 | 复杂度应流向 hook 脚本,不流向配置格式 |
| 钥匙串/密码管理器集成 | 永久砍 | `*.local` 约定已覆盖;真需要加密再用 age(M3 可选) |
| watch/守护进程 | 永久砍 | 显式触发是特性不是缺陷 |
| Windows | 永久砍 | 无场景 |
| 完整 semver 约束语法 | 冻结 | `>=` 够用(ADR-11) |
| 目录级链接模式 | 冻结 | ADR-3;若遇到上千文件的模块再评估性能 |
| 配置分仓 | 冻结 | 若未来 CLI 想开源给他人用,再拆 CLI 出去,配置仓保留 |

## 3. 风险与对策

| 风险 | 对策 |
|---|---|
| 中途弃坑 | 里程碑收口标准 = 立即可用;M1 完成即已回本 |
| requires 忘记提升(唯一的人肉环节) | README 纪律 + `doctor` 检查「模板/manifest 用到的特性 vs requires 版本」的启发式提示 [M2] |
| prune 误删 | 05 号文档 §3 保守判定 + 负面测试;新增 prune 相关逻辑必须先写测试 |
| macOS 系统更新改变 `~/Library` 行为 | 该区域仅经 `[target]` 显式使用;plist 永不托管(经验规则:GUI 偏好走 `defaults write` hook) |
| state.json 手滑删除 | apply 降级可用 + `--rebuild-state` [M2] |

## 4. M1 第一周的建议实现顺序

`paths` → `manifest`(含 requires)→ `planner` link 部分 + 决策表单测 → `fsutil` +
`executor` → `apply/diff` 串通 → `state` + prune → `add` → scaffold → `init` +
bootstrap.sh → goreleaser。每步都保持 `go test ./...` 绿灯,planner 决策表的表驱动
测试先于实现编写(它就是可执行的规格)。
