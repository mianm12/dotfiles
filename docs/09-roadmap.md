# 09 · 路线图与范围控制

## 1. 里程碑总览

每个里程碑收口的标准是同一句话:**当天起可以(继续)作为唯一的 dotfiles 工具日常使用**。
避免「憋大招式 v1.0」——个人项目最大的风险不是设计不好,而是写一半失去动力。

> v1.5 为冻结版:`planner/decide` 的表驱动测试就是决策表的可执行形态,
> 由它担任后续审查员;剩余问题在实现中修比在文档上推演便宜。

### M1 · 可用且安全的 stow 替代(目标:第 1–3 周)

| 范围 | 说明 |
|---|---|
| 命令 | `init` `apply` `diff` `status` `add` `version`、`doctor --manifest-only`(最小子集:manifest 静态校验 + 路径合法性 + target 唯一性 + 已跟踪 `*.local`,CI 自 lint 依赖);完整 doctor 随 M2 |
| 引擎 | 文件级 symlink、**owned() 词法谓词 + link_dest 存证**、conflict 三态(含 L4 改指)、`--force → BackupReplace`(**分型语义 + 排他备份、保留原 mode**)、prune(双作用域 + 整模块确认 + **收敛门控/deferred**)、symlink 自动收养、**kind 迁移**(scaffold 相关行)、全局不变量校验(含 StateOp×Kind 断言)、创建先于 prune、**Precond 全量复核**、StateOp/NextEntry 落账、flock 锁(**单次获取**)、**state 三态 + 语义校验 fail-closed** |
| 模板 | **仅 scaffold**(`.template`),渲染 fail-fast;**managed 相关输入硬错误**:`.tmpl`、`kind="managed"`、`add --template`、`apply --adopt` 在 M1 明确报错,绝不静默按 link 处理(M 表与迁移表 managed 行随 M2 落地) |
| add | link/scaffold 两模式、**完整解析链预检(ADR-30)**、仓库侧排他创建、rename 前复核、失败自动清理副本 |
| hooks | run_once 最小实现(字符串形态,单脚本元组指纹);`hooks/` 保留目录;执行细节(exec/sh、cwd=模块目录、环境覆盖注入);不受收敛门控 |
| manifest | 两级结构、profiles、os 过滤、`[target]` 双形态(缺当前 GOOS 硬错误)、ignore(优先级 + 停止管理)、路径合法性校验、两阶段加载(requires 预读 + 严格解码) |
| 基建 | state.json(link_dest + 语义校验)、`--home` 全隔离(含 hook 子进程环境)、bootstrap.sh(校验 + 原子安装)、goreleaser 首发 |
| 收口标准 | 本人主力 Mac 完全切换到 dot 管理;stow 卸载 |

*说明:M1 相比最初构想大了一圈(owned/收养/不变量/锁/收敛门控/fail-closed/解析链
预检),原因一致——这些要么属于**持久化契约**(state 字段与语义、run_once 键格式、
requires 行为),要么属于**安全底线**(误删/误覆盖/丢配置防护)。契约后补有兼容债,
底线后补等于先裸奔;而 watch、edit、from_env 之类后补皆无痛,留在后面。*

### M2 · 模板与同步闭环(目标:第 4–6 周)

| 范围 | 说明 |
|---|---|
| managed 模板 | `.tmpl` 渲染、drift 检测(hash + mode)、`diff -v`(实际 vs 本次渲染)、`apply --adopt`、`add --template`、kind 迁移 managed 行、`[data]` 声明 + init 收集、`from_env` |
| 同步 | `update`(洁净检查 + 自 pull 持锁)、`self-update`、`git` 透传 |
| hooks | `watch` 依赖文件(表形态 + 组合元组指纹) |
| 辅助 | `doctor` 全量(模板静态扫描、state 语义诊断、权限巡检)、`edit`(持锁)、`state rebuild`(持锁) |
| 收口标准 | `.gitconfig` 差异靠模板解决;第二台机器(或干净虚拟机)全流程跑通 |

### M3 · Linux 与打磨(目标:按需)

| 范围 | 说明 |
|---|---|
| Linux | 真实 Linux 机器落地:路径差异模块化、`linux-pkgs` 模块、发行版脚本 |
| 可选项 | `--json` 输出、age 加密单文件、`--allow-outside-home`、release 签名、drift 三方对比的产物快照缓存 |
| 收口标准 | 一台 Linux 服务器/桌面由 bootstrap 一条命令拉起 |

## 2. 明确砍掉/冻结的功能

写下来是为了将来手痒时对照:

| 功能 | 处置 | 理由 |
|---|---|---|
| 深合并/模块继承 | 永久砍 | ADR-7,可预测性优先 |
| manifest 里的条件表达式/循环 | 永久砍 | 复杂度应流向 hook 脚本,不流向配置格式 |
| 模板 `env` 函数 | 永久砍 | ADR-17;环境值走 `[data].from_env` |
| 模板 parse-tree 审查(禁 range/define) | 永久砍 | 过度工程;立场是不推荐、不辅助 |
| **update 拉到新 hook 的单独确认** | 永久砍 | 威胁模型出界(01 §4);`--no-apply` + `diff` 已覆盖审查需求 |
| **`observed` 第三类 state 条目** | 永久砍 | 收养不对称(ADR-21)以一个 flag 达成同等安全,免去第三种持久化语义 |
| **`Entry.mode` 字段与 chmod 动作** | 永久砍 | ADR-26,mode 漂移经复用 Render 修正 |
| **`Error` ActionKind** | 永久砍 | 渲染 fail-fast(ADR-24)后无存在必要 |
| **`add --activate`(CLI 写 manifest)** | 永久砍 | ADR-28;保格式 TOML 编辑脆弱、重序列化丢注释;报错 + 打印待添加行即可 |
| **`add -m` 自动创建模块目录** | 永久砍 | 新目录必然 ∉ profile,与 profile 校验自相矛盾;两步指引即可 |
| **add 目录/递归收编** | 永久砍 | 逐文件 add 已够用;递归移动 + 批量换链的失败模式复杂 |
| **目录/特殊文件的 BackupReplace** | 永久砍 | ADR-29;要求手工移走一次,换执行器简单可证 |
| **run_once 指纹卷入 profile/data** | 永久砍 | ADR-31;切 profile 即全量重跑不可接受 |
| 钥匙串/密码管理器集成 | 永久砍 | `*.local` 纵深已覆盖;真需要加密再用 age(M3 可选) |
| 并发执行支持 | 永久砍 | ADR-19,flock 防事故即可 |
| watch/守护进程 | 永久砍 | 显式触发是特性不是缺陷 |
| Windows | 永久砍 | 无场景 |
| 完整 semver 约束语法 | 冻结 | `>=` 够用(ADR-11) |
| 目录级链接模式 | 冻结 | ADR-3;遇到上千文件的模块再评估 |
| drift 三方对比的内容快照 | 冻结 → M3 可选 | 「实际 vs 本次渲染」已满足决策需要 |
| 配置分仓 | 冻结 | 若未来 CLI 开源给他人用,再拆 CLI 出去 |

## 3. 风险与对策

| 风险 | 对策 |
|---|---|
| 中途弃坑 | 里程碑收口标准 = 立即可用;M1 完成即已回本 |
| requires 忘记提升 | 严格解码(ADR-16)失效安全兜底;README 纪律 + doctor 启发式 [M2] |
| prune 误删 | owned(词法存证)∧ 收敛门控 ∧ Precond 复核 ∧ 整模块确认,四重防线;P1–P3 与门控均有负面测试 |
| 改名/移动丢配置 | 创建先于 prune + 收敛门控(ADR-13/20);集成场景 5 锁定 |
| 死链清不掉(v1.1 缺陷) | owned 词法化(ADR-22);集成场景 6 防回归 |
| 用户文件被误收养后误删(v1.1 缺陷) | 收养不对称(ADR-21);集成场景 4b 锁定 |
| kind 切换后误删(v1.2 缺陷) | 迁移三原则 + entry=nil 规则(ADR-27);集成场景 20 防回归 |
| add 产物被下轮当孤儿删除(v1.4 缺陷) | 解析链预检(ADR-30);集成场景 25 防回归 |
| update 读到新旧混合仓库 | 洁净检查前置(porcelain 为空);集成场景 26 |
| 第三方进程与 apply 竞态 | Precond 全量复核(ADR-23);残余微秒窗口已在威胁模型明文接受 |
| macOS 系统更新改变 `~/Library` 行为 | 该区域仅经 `[target]` 显式使用;plist 永不托管(GUI 偏好走 defaults hook) |
| state 损坏/版本过新/字段残缺 | fail-closed(ADR-25 含语义校验)防毁尸灭迹;手动 mv 恢复路径 + 收养重建;`state rebuild` [M2] |
| 测试写穿真实 home | `--home` 全隔离含 hook 子进程环境;集成场景 18 显式断言 |

## 4. M1 的建议实现顺序

`paths`(Display/Within/创建侧 normalize)→ `manifest`(两阶段加载 + 路径合法性 +
[target] 校验)→ `planner/decide` 决策表(**表驱动测试先于实现——它就是可执行的
规格**)→ `planner/validate` 不变量(含 StateOp 断言)→ `fsutil` + `state`(三态 +
语义校验 + flock + link_dest)→ `executor`(阶段顺序 + Precond 全量复核 + 收敛门控 +
StateOp/NextEntry 落账)→ `apply/diff` 串通 → prune + 收养 + kind 迁移(scaffold
相关行)→ `add`(link/scaffold 两模式,解析链预检 + 原子换链)→ scaffold(fail-fast
渲染)→ run_once 最小实现 → `init` + bootstrap.sh → goreleaser。每步保持
`go test ./...` 绿灯。
