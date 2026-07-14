# 09 · 路线图与范围控制

## 1. 里程碑总览

每个里程碑收口的标准是同一句话:**当天起可以(继续)作为唯一的 dotfiles 工具日常使用**。
避免「憋大招式 v1.0」——个人项目最大的风险不是设计不好,而是写一半失去动力。

### M1 · 可用且安全的 stow 替代(目标:第 1–2.5 周)

| 范围 | 说明 |
|---|---|
| 命令 | `init` `apply` `diff` `status` `add` `version` |
| 引擎 | 文件级 symlink、**owned() 谓词**、conflict 三态(含 CONFLICT-drift)、`--force` 备份、prune(双作用域 + 整模块确认)、**收养规则**、**全局不变量校验**、**创建先于 prune**、Precond 复核、**flock 单实例锁** |
| 模板 | **仅 scaffold**(`.template`);managed 留到 M2,但后缀/决策表按本文档预留 |
| hooks | **run_once 最小实现**(字符串形态,指纹 = 脚本内容);`hooks/` 保留目录 |
| manifest | 两级结构、profiles、os 过滤、`[target]` 双形态、ignore(优先级 + 停止管理语义)、**两阶段加载(requires 预读 + 严格解码)** |
| 基建 | state.json、`--home` 全隔离(含 hook 子进程环境)、bootstrap.sh(校验 + 原子安装)、goreleaser 首发 |
| 收口标准 | 本人主力 Mac 完全切换到 dot 管理;stow 卸载 |

*说明:M1 比最初构想膨胀了一圈(owned/收养/不变量/锁/最小 hooks),原因一致——这些
要么属于**持久化契约**(state 语义、run_once 键格式、requires 行为),要么属于**安全
底线**(误删/误覆盖防护)。契约后补有兼容债,底线后补等于先裸奔;而 profiles 打磨、
watch、edit 之类后补皆无痛,所以留在后面。*

### M2 · 模板与同步闭环(目标:第 3–5 周)

| 范围 | 说明 |
|---|---|
| managed 模板 | `.tmpl` 渲染、drift 检测、`diff -v`(实际文件 vs 本次渲染)、`[data]` 声明 + init 收集、`from_env` |
| 同步 | `update`、`self-update`、`git` 透传 |
| hooks | `watch` 依赖文件(表形态 + 组合指纹) |
| 辅助 | `doctor` 全量(manifest lint、模板静态扫描、`git ls-files '*.local'`、权限巡检)、`edit`、`state rebuild` |
| 收口标准 | `.gitconfig` 差异靠模板解决;第二台机器(或干净虚拟机)全流程跑通 |

### M3 · Linux 与打磨(目标:按需)

| 范围 | 说明 |
|---|---|
| Linux | 真实 Linux 机器落地:路径差异模块化、`linux-pkgs` 模块、发行版脚本 |
| 可选项 | `--json` 输出、age 加密单文件、`--allow-outside-home`、release 签名、drift 三方对比所需的产物快照缓存 |
| 收口标准 | 一台 Linux 服务器/桌面由 bootstrap 一条命令拉起 |

## 2. 明确砍掉/冻结的功能

写下来是为了将来手痒时对照:

| 功能 | 处置 | 理由 |
|---|---|---|
| 深合并/模块继承 | 永久砍 | ADR-7,可预测性优先 |
| manifest 里的条件表达式/循环 | 永久砍 | 复杂度应流向 hook 脚本,不流向配置格式 |
| 模板 `env` 函数 | 永久砍 | ADR-17,渲染必须只依赖显式输入;环境值走 `[data].from_env` |
| 模板 parse-tree 审查(禁 range/define) | 永久砍 | 过度工程;立场是不推荐、不辅助,而非技术封锁 |
| 钥匙串/密码管理器集成 | 永久砍 | `*.local` 四道机制已覆盖;真需要加密再用 age(M3 可选) |
| 并发执行支持 | 永久砍 | ADR-19,flock 防事故即可 |
| watch/守护进程 | 永久砍 | 显式触发是特性不是缺陷 |
| Windows | 永久砍 | 无场景 |
| 完整 semver 约束语法 | 冻结 | `>=` 够用(ADR-11) |
| 目录级链接模式 | 冻结 | ADR-3;若遇到上千文件的模块再评估性能 |
| drift 三方对比的内容快照 | 冻结 → M3 可选 | 「实际 vs 本次渲染」已满足决策需要(06 号文档 §5) |
| 配置分仓 | 冻结 | 若未来 CLI 想开源给他人用,再拆 CLI 出去,配置仓保留 |

## 3. 风险与对策

| 风险 | 对策 |
|---|---|
| 中途弃坑 | 里程碑收口标准 = 立即可用;M1 完成即已回本 |
| requires 忘记提升(唯一的人肉环节) | **风险已降级**:严格解码(ADR-16)构成失效安全的第二道防线;README 纪律 + doctor 启发式提示 [M2] 仍保留 |
| prune 误删 | owned 谓词 + Precond 复核 + 整模块确认三重防线;决策表 P1–P3 均有负面测试;新增 prune 相关逻辑必须先写测试 |
| 改名/移动丢配置 | 创建先于 prune(ADR-13)+ error 跳过 prune;集成测试场景 5 锁定 |
| macOS 系统更新改变 `~/Library` 行为 | 该区域仅经 `[target]` 显式使用;plist 永不托管(GUI 偏好走 `defaults write` hook) |
| state.json 手滑删除/损坏 | apply 降级可用 + 收养规则重跑自愈;`state rebuild` [M2] 兜底 |
| 测试写穿真实 home | `--home` 全隔离含 hook 子进程环境(同一代码路径),集成场景 14 显式断言 |

## 4. M1 的建议实现顺序

`paths`(含 Display/Within/normalize)→ `manifest`(两阶段加载 + requires)→
`planner/decide` 决策表(**表驱动测试先于实现,它就是可执行的规格**)→
`planner/validate` 不变量 → `fsutil` + `state`(含 flock)→ `executor`(顺序 + Precond)
→ `apply/diff` 串通 → prune + 收养 → `add` → scaffold → run_once 最小实现 →
`init` + bootstrap.sh → goreleaser。每步保持 `go test ./...` 绿灯。
