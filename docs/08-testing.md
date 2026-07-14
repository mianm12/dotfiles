# 08 · 测试策略

## 1. 原则

1. **幂等是最高契约**(精确表述见 05 号文档 §10):每个集成测试在断言业务结果后,必须
   追加「再次 apply → 动作列表仅含 skip 与既有 conflict,无任何 mutation、无 adopt」的
   通用断言。
2. **scan/decide 拆分换取可测性**(02 号文档 §4):decide 是纯函数,单测直接喂内存构造的
   `desired × observed × state` 三元组、断言 `[]Action`,不碰磁盘;scan 是唯一做
   target IO 的薄层,单独用少量文件系统测试覆盖。
3. 危险操作(覆盖、prune)必须有对应的**负面测试**:断言「没有 --force 时文件毫发无损」
  「非 owned 对象永不被删」。

## 2. 测试地基:`--home` 全隔离

隐藏全局 flag `--home <dir>`(02 号文档 §2)把 `~`、state、backup、机器配置**以及
hook 子进程的 `HOME`/`XDG_*` 环境**全部重定向——最后一项靠 executor 统一从 `paths`
取子进程环境实现,生产与测试同一条代码路径,因此测试对隔离性的断言同时验证了生产行为。

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

要求 CLI 入口可进程内调用(`cli.Execute(ctx, args, stdout, stderr) int`),且命令树
每次 Execute 现建(无包级单例,防 flag 状态跨测试污染)。fork 真实二进制的冒烟测试
只留一个(验证 main 装配)。注意 macOS 上 `t.TempDir()` 位于 `/tmp → /private/tmp`
符号链接之下,一切路径比较必须经 `paths` 的 normalize,这本身就是一个回归测试点。

## 3. 分层测试清单

### 3.1 单元测试(表驱动)

| 包 | 重点用例 |
|---|---|
| `manifest` | 两阶段加载(宽松预读只取 requires;严格解码未知键即错;doctor 宽松);合并整键覆盖;ignore 并集特例与优先级;profile @ 展开/环/悬空;[data] 小写命名校验;target 双形态互斥 |
| `planner/decide` | 05 号文档 §3.2/§3.3 决策表**逐行**一个用例:L1–L6、M1–M6、S1–S3、P1–P3,外加 adopt 与陈旧克隆修复(L3)的 normalize 边界 |
| `planner/validate` | target 重复、`foo` vs `foo.tmpl` 碰撞、祖先冲突(`Within` 而非字符串前缀:`/home/a` vs `/home/ab`)、中间目录穿文件 |
| `tmpl` | 缺变量报错、命名空间(大写内建/小写用户)、default 函数、missingkey=error |
| `paths` | 优先级链(flag > env > config > 默认)、--home 重定向完备性、Display/Within、EvalSymlinks 吸收 |
| `state` | 损坏 JSON 降级、版本字段、原子写、flock 互斥 |
| `fsutil` | 原子写中断模拟、symlink 覆盖替换、备份 copy+校验、权限(0600/0700) |

### 3.2 集成测试(fake home)

核心场景(每个都附带幂等断言):

1. **新机器全流程**:构造 repo → init(--set 免交互)→ apply → 断言链接/渲染/scaffold
   产物与权限 → 幂等断言。
2. **conflict 三态**:预置真实文件 → apply 退出码 3 且文件未动、其余动作已完成 →
   `--force` 后备份存在(0600)、内容替换。
3. **链接被改指**:apply 后手工把链接指向别处 → status 报 drift、apply 判 CONFLICT-drift
   不静默修复 → `--force` 恢复。
4. **收养自愈**:手工预建正确链接(或渲染出与模板一致的文件)→ apply 产生 Adopt、
   文件系统零改动 → 第二次 apply 无 Adopt。
5. **执行顺序(改名场景)**:仓库中改名源文件,并使新 target 处预置 conflict →
   apply 后**旧链接仍在**(prune 位于创建之后,且新建失败不影响旧物)→ 解除 conflict
   重跑 → 收敛。
6. **prune 作用域**:模块 A、B 均已 apply;删除 A 的源文件后执行 `apply B` → A 的孤儿
   链接原封不动;执行全量 apply → A 的孤儿被清理。
7. **整模块孤儿确认**:切换 profile 使某模块整体退出 → 无 `--yes` 时要求确认,
   输入 N 则不删;`--yes` 直接清理。scaffold 产物始终不删。
8. **非 owned 不删**:把 state 记录的链接替换为普通文件 → prune 不动文件、state 摘除
   并警告。
9. **ignore = 停止管理**:已管理文件加入 ignore → 下次 apply prune 其链接,仓库源文件
   无损。
10. **不变量拒绝**:两个模块映射同一 target → apply 整体拒绝、**无任何落盘**(含 state)。
11. **add**:唯一命中自动归入;多候选退出码 3;`-m` 新建模块 + 模块 ∉ profile 报错 +
    `--activate` 修改顶层 manifest;**`*.local` 硬拒绝**;add 后 apply 幂等。
12. **os 过滤与 [target] 表**:GOOS 注入 planner(而非读运行时),单平台 CI 双跑
    darwin/linux 逻辑。
13. **requires 与严格解码**:提升 requires → mutation 命令拒绝、self-update/git/version
    不拦;requires 满足但含未知键 → 严格解码拒绝、doctor 宽松诊断。
14. **run_once**:首次执行、指纹不变跳过、脚本内容(或 watch 文件 [M2])变化重跑、
    失败不记账下次重试;**hook 环境隔离**:hook 脚本写 `$HOME/marker` 与
    `$XDG_CONFIG_HOME/marker` → 断言落在 fake home 内、真实家目录无写入。
15. **锁互斥**:持锁状态下第二个 apply 立即报错;diff 不受锁影响。

### 3.3 golden 测试

`diff`、`status`、`apply --dry-run` 的输出是用户接口的一部分,用 golden file 锁定
(`-update` flag 刷新)。输出中的绝对路径经 `paths.Display` 规范化为 `~/` 再比对,
时间戳字段剔除;确定性排序(04 号文档 §5)是 golden 可行的前提。交互确认场景一律
用 `--yes` 走非交互路径。

## 4. CI(GitHub Actions)

- 矩阵:`macos-latest` + `ubuntu-latest`,`go test ./...`(--home 设计保证无特权、
  无副作用)。
- 追加步骤:`go vet`、`golangci-lint`、`dot doctor --manifest-only`(用刚构建的二进制
  lint 仓库自身配置——吃自己狗粮的第一口)。
- tag push 触发 goreleaser(07 号文档 §3)。

## 5. 手动冒烟清单(发版前)

新建 macOS 用户账户或 Linux 容器,走一遍
`curl … | sh → init → 验证 shell 生效 → 改配置 → dot git push → 另一环境 dot update`,
并顺手验证 bootstrap 的 checksum 校验路径(篡改一个字节应当失败)。
频率:M1/M2/M3 里程碑收口时各一次,平时靠自动化。
