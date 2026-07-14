# 03 · Manifest 规范:两级结构、加载与合并语义

## 1. 职责划分总则

**模块自身的事写在模块里,跨模块的事写在顶层。** 每个模块目录必须自包含:把目录拷走
即是完整一份该工具的配置(含它的 hook 脚本与数据文件,如 Brewfile)。顶层 manifest
不得出现任何针对单个模块的映射规则。

| 关注点 | 顶层 `dot.toml` | 模块 `modules/<name>/dot.toml` |
|---|---|---|
| CLI 版本要求 `requires` | ✅ | ❌ |
| 全局默认值 `[defaults]` | ✅ | ❌ |
| 全局忽略规则 `[ignore]` | ✅ | 追加自己的 |
| profile 分组 | ✅ | ❌ |
| init 变量声明 `[data]` | ✅ | ❌ |
| OS 过滤、target | 仅提供默认 | ✅ 覆盖 |
| 文件级声明 `[files]` | ❌ | ✅ |
| hooks | ❌ | ✅ |

## 2. 顶层 manifest 完整字段

```toml
# dot.toml(仓库根)
requires = ">=0.3.0"        # 本仓库配置要求的最低 CLI 版本(仅支持 >= 语法,ADR-11)

[defaults]                  # 每个模块 manifest 的兜底值,键与模块级字段同名
target = "~"
os = ["darwin", "linux"]    # 缺省即全平台,通常无需写

[ignore]                    # gitignore 风格 pattern,模块内相对路径匹配
patterns = [".DS_Store", "README.md", "*.md"]
# 内置恒定忽略(无法关闭):dot.toml、.git/、hooks/、*.swp

[profiles]                  # 模块分组;"@name" 引用其他 profile,解析时展开去重
base = ["zsh", "git", "tmux", "nvim"]
mac  = ["@base", "karabiner", "hammerspoon", "macos"]
linux = ["@base", "linux-pkgs"]
work = ["@mac", "work-ssh"]

[data.email]                # 声明 init 时需要收集的模板变量(供 dot init 交互)
prompt = "Git commit email"
default = "me@example.com"

[data.machine]
prompt = "Machine name"

[data.github_user]          # [M2] init 时读取环境变量作为默认值,快照进机器配置;
from_env = "GITHUB_USER"    # 渲染期绝不读环境(ADR-17)
```

约束与校验规则:

1. profile 引用**不得成环**,解析时检测,成环即报错。
2. profile 中引用的模块目录不存在 → 报错(而非静默跳过),防止拼写错误。
3. `modules/` 下存在但未被当前 profile 引用的模块是合法的(`dot status` 列为
   "unassigned"),但**不能被 apply/add 直接操作**——见 ADR-18 与 04 号文档 §4.2/§4.5。
4. `[data]` 用户键**必须以小写字母开头**(与大写开头的内建变量形成强制命名空间,
   06 号文档 §3),违反即解码错误。
5. `[profiles]` 的列表是**顶层 manifest 中唯一会被 CLI 写入的位置**:`dot add --activate`
   会向当前 profile 追加模块名(04 号文档 §4.5)。

## 3. 模块 manifest 完整字段

模块 manifest **整体可选**。没有 `dot.toml` 的模块等价于:全平台、`target` 取顶层默认、
无 hooks、文件级行为全按后缀推断。

写法 A —— 全平台同一位置:

```toml
# modules/zsh/dot.toml
os = ["darwin", "linux"]    # 取值为 GOOS:darwin | linux;缺省 = defaults.os
target = "~"
```

写法 B —— 按 OS 分表(处理路径差异;与写法 A 互斥,同时出现即解码错误):

```toml
# modules/vscode/dot.toml
[target]
darwin = "~/Library/Application Support/Code/User"
linux  = "~/.config/Code/User"
```

文件级声明与 hooks:

```toml
[ignore]
patterns = ["*.bak"]        # 与顶层 ignore 取并集(唯一的并集特例,见 §4)

[files.".config/git/config.tmpl"]   # 键 = 模块内相对路径,精确匹配
mode = "0600"               # 落地权限(仅对 render/scaffold 产物有意义;symlink 不适用)
# kind = "link"             # 显式覆盖后缀推断:link | managed | scaffold
# target = "~/other/path"   # 单文件 target 覆盖(完整落地路径,含文件名)

[hooks]
run_once = [
  "hooks/setup.sh",                                        # 字符串形态 [M1]
  { script = "hooks/brew.sh", watch = ["hooks/Brewfile"] } # 表形态,带依赖 [M2]
]
```

hook 约定:脚本与其数据文件放模块顶层保留目录 `hooks/`(内置忽略,不被链接)。
`[hooks]` 引用了 `hooks/` 之外的路径时兼容执行,但该路径隐式加入本模块 ignore
并打印建议(双保险,防止脚本被链接出去)。执行语义与 hash 规则见 05 号文档 §8。

文件定级优先级(高 → 低):**内置忽略 > `[files]` 显式声明 > ignore patterns > 后缀推断**。
即:显式声明的文件即使匹配 ignore pattern 也被管理;内置忽略不可解除。落地文件名 =
源文件名去掉 `.tmpl` / `.template` 后缀。

## 4. 合并语义(精确定义)

有效配置的计算,对每个模块独立进行:

```
effective(module) = 顶层 [defaults] 打底,被模块 manifest 中出现的键整键覆盖
```

- **整键覆盖(ADR-7)**:模块写了 `os = ["darwin"]`,则顶层 `defaults.os` 完全不参与,
  不做数组并集;`target`(无论字符串或表形态)同理,模块出现该键即整体替换。
- **唯一例外:`ignore.patterns` 取并集**。忽略规则语义上是「都要排除」,覆盖反而危险
  (模块加一条忽略却意外解除了 `.DS_Store` 全局忽略)。此特例必须在文档与 `dot doctor`
  输出中显式可见。
- 模块之间互不引用、互不合并;不存在"模块继承"。
- profile 只是名字集合,不携带任何配置,不参与合并。

## 5. ignore 规则语义

- pattern 相对模块根匹配,语法取 gitignore 子集:`*`、`**`、目录尾 `/`;不支持 `!` 反选
  (需要例外时用 `[files]` 显式声明,见 §3 优先级)。
- 匹配发生在 enumerate(pipeline ⑤),被忽略文件不进入 desired state。
- **「加入 ignore = 停止管理」**:若某文件此前曾被管理(state 有记录),把它加入 ignore 后,
  下次 apply 会将已部署产物按孤儿处理——即被 prune(仅限 owned 产物,05 号文档 §3;
  仓库源文件本身无损)。这是有意语义,不是事故。

## 6. 校验命令

`dot doctor` 对 manifest 做静态检查:TOML 语法、严格解码下的未知键、profile 环、
悬空模块引用、`[files]` 键指向不存在的文件、`target` 表中不支持的 OS 键、target 全局
唯一性(复用 05 号文档 §5 的校验)、模板引用未声明变量。CI 中可跑
`dot doctor --manifest-only` 作为配置的 lint(退出码非零即失败)。

## 7. 两阶段加载与兼容性(ADR-16)

加载协议:

1. **宽松预读**:仅解出顶层 `requires` 字段(忽略其余一切)→ 版本检查。不满足 →
   提示 `dot self-update` 退出;`version=dev`(本地开发构建)放行 + 警告。
2. **严格解码**:requires 通过后,完整解码全部 manifest,`MetaData.Undecoded()` 非空
   即报错。适用于一切 mutation 命令(apply/add/update/init)及 diff。
3. **doctor 例外**:走宽松模式解码并**报告**未知键而非崩溃——它的职责是诊断。

这样设计的原因:提升 `requires` 是文档承认「唯一需要人肉自觉」的步骤;当作者忘记提升、
而新配置含有影响路径或所有权语义的新字段时,旧 CLI 若仅警告后继续 prune,风险不可接受。
严格解码把这种遗忘变成**失效安全**(fail-safe):旧 CLI 明确拒绝,而不是错误执行。

manifest 格式演进规则不变:**新版 CLI 必须能读旧格式;新字段必须有缺省行为;配置一旦
使用新字段,同一 commit 必须提升 `requires`**(07 号文档 §3)。
