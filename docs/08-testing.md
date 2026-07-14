# 08 · 测试策略

## 1. 原则

1. **幂等是最高契约**:每个集成测试在断言业务结果后,必须追加「再次 apply → 计划为空」
   的断言。任何破坏幂等的改动应当被测试网当场抓住。
2. **planner 纯函数化换取可测性**(02 号文档 §4):plan 阶段不写盘,单测直接喂
   内存构造的输入、断言 `[]Action`,无需真实文件系统。
3. 危险操作(覆盖、prune)必须有对应的**负面测试**:断言「没有 --force 时文件毫发无损」。

## 2. 测试地基:`--home` 重定向

隐藏全局 flag `--home <dir>`(02 号文档 §2)把 `~`、state、backup、机器配置全部重定向。
集成测试骨架:

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

要求 CLI 入口可进程内调用(`cli.Execute(ctx, args, stdout, stderr) int`),测试不 fork
子进程——快、可断点、覆盖率统计直接生效。fork 真实二进制的冒烟测试只留一个
(验证 main 装配无误)。

## 3. 分层测试清单

### 3.1 单元测试(表驱动)

| 包 | 重点用例 |
|---|---|
| `manifest` | 两级合并整键覆盖;ignore 并集特例;profile @ 展开/环检测/悬空引用;requires 解析;未知键警告 |
| `planner` | 05 号文档 §3 决策表**逐行**一个用例:link 六态、managed 五态、scaffold 三态、prune 四态 |
| `tmpl` | 缺变量报错、内建变量注入、default/env 函数 |
| `paths` | 优先级链(flag > env > config > 默认)、--home 重定向完备性 |
| `state` | 损坏 JSON 降级、版本字段、原子写 |
| `fsutil` | 原子写中断模拟、symlink 覆盖替换 |

### 3.2 集成测试(fake home)

核心场景(每个都附带幂等断言):

1. **新机器全流程**:构造 repo → init(--set 免交互)→ apply → 断言链接/渲染/scaffold
   产物 → 再 apply 计划为空。
2. **conflict 三态**:预置真实文件 → apply 退出码 3 且文件未动 → `--force` 后备份存在、
   内容替换。
3. **drift**:手改 rendered 产物 → status 退出码 2 并列出 → apply 不覆盖 →
   `--force` 覆盖。
4. **prune**:apply 后从 repo 删除源文件 → apply 删除对应链接;scaffold 产物不删。
5. **scaffold 生命周期**:首次生成 → 手改后 apply 不动 → 删除产物后 apply 不重建。
6. **os 过滤与 [target] 表**:darwin/linux 双跑(GOOS 注入 planner 而非读运行时,
   使单平台 CI 可测双平台逻辑)。
7. **add**:唯一命中自动归入;多候选退出码 3;`-m` 新建模块;add 后 apply 计划为空。
8. **requires**:提升 repo 中 requires → 各命令拒绝执行、self-update/git 不受拦。
9. **run_once**:首次执行、hash 不变跳过、hash 变化重跑、脚本失败不记账下次重试。

### 3.3 golden 测试

`diff`、`status`、`apply --dry-run` 的输出是用户接口的一部分,用 golden file 锁定
(`-update` flag 刷新)。输出中的绝对路径规范化为 `~/` 再比对,时间戳字段剔除。

## 4. CI(GitHub Actions)

- 矩阵:`macos-latest` + `ubuntu-latest`,`go test ./...`(集成测试因 --home 设计
  无需特权、无副作用)。
- 追加步骤:`go vet`、`golangci-lint`、`dot doctor --manifest-only`(用刚构建的
  二进制 lint 仓库自身配置——吃自己狗粮的第一口)。
- tag push 触发 goreleaser(07 号文档 §3)。

## 5. 手动冒烟清单(发版前)

新建 macOS 用户账户或 Linux 容器,走一遍
`curl … | sh → init → 验证 shell 生效 → 改配置 → dot git push → 另一环境 dot update`。
频率:M1/M2/M3 里程碑收口时各一次,平时靠自动化。
