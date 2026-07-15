# 08 · 测试策略

## 1. 原则

1. **幂等是最高契约**(精确表述见 05 号文档 §10):每个集成测试在断言业务结果后,必须
   追加「以相同 flag 再次 apply → 无任何 mutation 动作、无 Adopt;仅 Skip、既有
   Conflict 与随之稳定复现的 deferred Prune」的通用断言。
2. **scan/decide 拆分换取可测性**(02 号文档 §4):decide 是纯函数,单测直接喂内存构造的
   `desired × observed × state` 三元组、断言 `[]Action`,不碰磁盘;scan 是唯一做
   target IO 的薄层,单独用少量文件系统测试覆盖。
3. 危险操作(覆盖、prune)必须有对应的**负面测试**:「没有 --force 时文件毫发无损」
  「非 owned 对象永不被删」「未收敛时 prune 一个都不执行」。

## 2. 测试地基:`--home` 全隔离

隐藏全局 flag `--home <dir>` 把 `~`、state、backup、机器配置**以及 hook 子进程的
`HOME`/`XDG_*` 环境**全部重定向——最后一项靠 executor 统一从 `paths` 取子进程环境
实现,生产与测试同一条代码路径,因此测试对隔离性的断言同时验证了生产行为。

```go
func newTestEnv(t *testing.T) *Env {
    t.Helper()
    home := t.TempDir()
    repo := t.TempDir()
    // 以 fixture 或代码构造 repo:写 dot.toml、modules/**
    return &Env{Home: home, Repo: repo}
}

func (e *Env) Run(t *testing.T, args ...string) Result {
    // 进程内调用 cli.Execute,前置注入 --home、--repo
    // 返回 stdout/stderr/exitCode
}
```

要求 CLI 入口可进程内调用(`cli.Execute(ctx, args, stdout, stderr) int`),命令树每次
Execute 现建(无包级单例,防 flag 状态跨测试污染)。fork 真实二进制的冒烟测试只留
一个。注意 macOS 上 `t.TempDir()` 位于 `/tmp → /private/tmp` 符号链接之下:创建侧
规范化(link_dest 写入前的一次 EvalSymlinks)必须吸收它,此后词法比较才成立——
这本身就是 ADR-22 的回归测试点。

## 3. 分层测试清单

### 3.1 单元测试(表驱动)

| 包 | 重点用例 |
|---|---|
| `manifest` | 两阶段加载(宽松预读只取 requires;严格解码未知键即错;doctor 宽松);合并整键覆盖;ignore 并集特例与优先级(含 hooks 引用不可被 [files] 覆盖 → 校验错误);profile @ 展开/环/悬空;[data] 小写命名;target 双形态互斥;**路径合法性**(模块名 `../x` 拒绝、[files] 键含 `..`/绝对路径拒绝) |
| `planner/enumerate` | hooks/ 保留目录排除;模块树内 symlink/特殊文件 → 报错 |
| `planner/decide` | 05 号文档 §3.2–§3.4 决策表**逐行**:L1–L6、M1–M6(含 M3a/b/c 与 --adopt 开关)、S1–S3、P1–P3;**kind 迁移矩阵全组合**,重点覆盖规则 3 的 entry=nil 推论:旧链换成同内容普通文件 + 转 managed → 仅 adoptable(**不得**借旧账自动收养);rendered→link 遇精确期望链接 → L2 收养、目标缺失 → L1/S3 创建;**元数据刷新通则**:L3 重链落账前崩溃 → 下轮 L2 产出 Adopt 自愈 link_dest;scaffold 跨模块移动 → S1 skip + 刷新 module/source;conflict 存在 → 全部 Prune 带 Deferred 标记 |
| `planner/validate` | target 重复、`foo` vs `foo.tmpl` 碰撞、祖先冲突(`Within` 非字符串前缀:`/home/a` vs `/home/ab`)、中间目录穿文件 |
| `executor` | **过期 Precond 注入**:喂 scan 后被篡改的 Action(Missing→已存在、hash 已变、LinkDest 已变)→ 动作降级 Conflict、文件未动、prune 转 deferred;**BackupReplace 分型**:普通文件 copy+hash、symlink 备份链接本身(不跟随)、目录/特殊文件即使 --force 仍拒绝;备份目录 O_EXCL 排他创建、文件保留原 mode(可执行位不丢);备份失败 → 放弃该 BackupReplace;**StateOp 语义**:Upsert/Delete 仅在动作成功后执行、Delete 仅随 Prune、Keep 动作绝不触碰记账 |
| `tmpl` | 缺变量报错、命名空间(大写内建/小写用户)、default 函数、missingkey=error、fail-fast(一个模板坏 → 整体 error) |
| `paths` | 优先级链、--home 重定向完备性、Display/Within、创建侧 normalize |
| `state` | **三态**:缺失 → 正常空载;损坏 JSON → fail-closed 错误类型;version+1 → 同;原子写;flock 互斥;link_dest 序列化 |
| `fsutil` | 原子写中断模拟、symlink 覆盖替换、备份 copy+校验、权限(0600/0700) |
| hooks 指纹 | (路径,长度,内容)元组编码:内容拼接歧义用例(`"ab","c"` vs `"a","bc"`)必须产生不同指纹 |

### 3.2 集成测试(fake home)

核心场景(每个都附带幂等断言):

1. **新机器全流程**:构造 repo → init(--set 免交互)→ apply → 断言链接/渲染/scaffold
   产物、权限、state 中 link_dest 存证。
2. **conflict 三态 + 收敛门控**:预置真实文件 → apply 退出码 3、该文件未动、其余创建
   已完成、**全部 prune 输出 deferred 且未执行** → `--force` 后备份存在(0600)、
   内容替换、prune 恢复执行。
3. **链接被改指**:apply 后手工把链接指向别处 → status 报 drift、apply 判 L4 conflict
   不静默修复 → `--force` 恢复。
4. **收养不对称**:(a) 手工预建指向正确源的链接 → apply 自动 Adopt、文件系统零改动;
   (b) 手工放置与渲染结果一致的普通文件 → 默认 skip + adoptable 提示、state 无记录 →
   `apply --adopt` 后收编 → 此后模板删除时该文件可被正常 prune(证明收养链路完整)。
5. **执行顺序(改名场景)**:仓库中改名源文件,并使新 target 处预置 conflict →
   apply 后**旧链接仍在**(prune deferred)→ 解除 conflict 重跑 → 新链建立、旧链
   被清、收敛。
6. **死链 prune(ADR-22 回归)**:apply 后从仓库删除源文件 → 旧链接悬空 → 全量 apply
   正常将其 prune(v1.1 的解析式 owned 在此恒失败,本用例防止回归)。
7. **仓库搬家(L3)**:整体移动 repo 目录、更新机器配置 repo 指向 → apply 对每条链接
   静默重链、link_dest 更新、无 conflict。
8. **prune 作用域**:模块 A、B 均已 apply;删除 A 的源文件后执行 `apply B` → A 的孤儿
   原封不动;全量 apply → A 被清理。
9. **整模块孤儿确认**:切换 profile 使某模块整体退出 → 无 `--yes` 时要求确认,输入 N
   → **本次全部 prune 延迟**;`--yes` 直接清理。scaffold 产物始终不删。
10. **非 owned 不删**:把 state 记录的链接替换为普通文件 → prune 不动文件、state 摘除
    并警告。
11. **ignore = 停止管理**:已管理文件加入 ignore → 下次(干净)apply prune 其链接,
    仓库源文件无损。
12. **不变量拒绝**:两个模块映射同一 target → apply 整体拒绝、无任何落盘(含 state)。
13. **add 全分支**:唯一命中自动归入;多候选退出码 3;`-m` 新建模块 + ∉ profile 报错
    并打印待添加行(CLI 不改 manifest);`*.local` 硬拒绝;目录/symlink/特殊文件拒绝;
    **--template [M2]:原文件保留、kind=rendered、替换变量后 apply skip(闭环)**;
    add 后幂等。
14. **state fail-closed**:损坏 state.json → apply/add 拒绝(exit 1)、status/doctor
    正常诊断;`mv` 备走后 apply 恢复并经收养重建;version+1 同理拒绝。
15. **mode 漂移**:chmod 修改 rendered 产物权限 → 下次 apply 产出 render(reason=mode)、
    内容不变权限恢复。
16. **os 过滤与 [target] 表**:GOOS 注入 planner,单平台 CI 双跑 darwin/linux 逻辑。
17. **requires 与严格解码**:提升 requires → mutation 命令拒绝、self-update/git/version
    不拦;requires 满足但含未知键 → 严格解码拒绝、doctor 宽松诊断。
18. **run_once**:首次执行、指纹不变跳过、脚本内容(或 watch 文件 [M2])变化重跑、
    失败不记账下次重试;**存在 conflict 时 hooks 照常执行**(不受收敛门控);
    **hook 环境隔离**:脚本写 `$HOME/marker` 与 `$XDG_CONFIG_HOME/marker` → 断言落在
    fake home 内、真实家目录无写入。
19. **锁边界**:持锁下第二个 apply 立即报错;diff/status 不受锁影响;update 在 pull
    阶段即持锁(可用慢速 hook 模拟占锁窗口)。
20. **kind 迁移(误删回归)**:apply 生成 rendered 产物 → 仓库把 `.tmpl` 改为
    `.template` → apply 产出 Adopt(记账 → scaffold)、文件未动 → 移除模块后全量
    apply → **文件仍在**(P1),v1.2 的误删路径闭死。另测 owned link → managed 的
    无感模板化(`git mv foo foo.tmpl` 后 apply:链换产物、记账迁移、无 conflict);
    以及 **entry=nil 回归**:把 owned 链接手工换成内容恰好等于渲染结果的普通文件 +
    转 managed → 仅提示 adoptable、绝不自动收养,此后 prune 永不删该文件。
21. **add 换链失败原文件完好**:注入 symlink/rename 失败(只读目录等)→ 原路径文件
    仍在且内容未变;仓库副本可安全删除重试。
22. **锁单次获取**:init/update 全流程(内部调 apply)正常完成、无自锁;fork 冒烟
    用例覆盖真实 flock 行为。
23. **拒绝确认的退出码与 M1 硬错误**:整模块孤儿确认输入 N → 退出码 2、prune 全部
    deferred;M1 构建下 `.tmpl` 文件、`kind="managed"`、`add --template`、
    `apply --adopt` 明确报错,绝不静默按 link 处理。
24. **add 仓库侧安全**:仓库已有 `foo.tmpl` 时 `add foo` 拒绝(后缀碰撞);state
    删除后对已入库文件重复 add → 拒绝、仓库文件未被覆盖(排他创建);copy 后、
    rename 前修改原文件 → add 中止、原文件保留新内容、仓库副本被清理;多路径 add
    中任一预检失败 → 全部不执行。

### 3.3 golden 测试

`diff`、`status`、`apply --dry-run` 的输出用 golden file 锁定(`-update` 刷新)。
绝对路径经 `paths.Display` 规范化、时间戳剔除;`prune (deferred)`、`adoptable` 等
标注属于用户接口,纳入 golden。交互确认场景一律用 `--yes` 走非交互路径。

## 4. CI(GitHub Actions)

- 矩阵:`macos-latest` + `ubuntu-latest`,`go test ./...`。
- 追加:`go vet`、`golangci-lint`、`dot doctor --manifest-only`(用刚构建的二进制 lint
  仓库自身配置)。
- tag push 触发 goreleaser(07 号文档 §3)。

## 5. 手动冒烟清单(发版前)

新建 macOS 用户账户或 Linux 容器,走一遍
`curl … | sh → init → 验证 shell 生效 → 改配置 → dot git push → 另一环境 dot update`,
顺手验证 bootstrap 的 checksum 校验路径(篡改一个字节应当失败)。频率:里程碑收口时
各一次,平时靠自动化。
