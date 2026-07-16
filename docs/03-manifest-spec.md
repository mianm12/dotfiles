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

顶层只允许 `requires`、`defaults`、`ignore`、`profiles`、`data` 五组字段;未知键按
ADR-16 失败。内建缺省为 `target = "~"`、全平台生效和空 ignore;`[defaults]` 只允许
`os` 与 `target`,且只覆盖明确出现的键。因此仓库可以省略整个 `[defaults]`,模块也仍有
完整、确定的有效配置。defaults 的 `target` 可以是字符串,也可以写成仅含
`darwin`/`linux` 键的 `[defaults.target]` 表;两种形态互斥,`os` 可以与任一形态共存。

```toml
# dot.toml(仓库根)
requires = ">=0.3.0"        # 本仓库配置要求的最低 CLI 版本(仅支持 >= 语法,ADR-11)

[defaults]                  # 每个模块 manifest 的兜底值,键与模块级字段同名
target = "~"
os = ["darwin", "linux"]    # 缺省即全平台,通常无需写

[ignore]                    # 本文 §5 定义的 gitignore 子集,按模块内相对路径匹配
patterns = [".DS_Store", "README.md", "*.md"]
# 内置恒定忽略(无法关闭):根 dot.toml、任意 .git 路径、根 hooks/、hook 引用路径、*.swp

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

精确表结构:`[defaults]` 仅含 `os`/`target`;顶层 `[ignore]` 仅含可选字符串数组
`patterns`;每个 `[data.<key>]` 仅含可选字符串 `prompt`、`default`、`from_env`。表存在但
没有可选字段时等价于空设置;未知字段一律非法。

约束与校验规则:

1. 顶层 `requires` 是**必填字符串**,只接受 `>=MAJOR.MINOR.PATCH`(三个非负十进制整数,
   不带 `v` 或其他约束)。缺失或格式非法是 manifest 错误,不得解释为无下限;
   `version=dev` 只放行版本比较,不放行缺失或非法字段。
2. `[profiles]` 必须存在且至少声明一个 profile;每个值是字符串数组。profile 名必须
   匹配 `^[A-Za-z0-9][A-Za-z0-9._-]*$`;成员中的 `@name` 仅表示引用该 profile,其余
   字符串表示模块名。引用**不得成环**,解析时检测,成环即报错。空 profile 本身合法。
3. profile 中引用的模块目录不存在,或引用拼写与目录项实际名称不完全一致 → 报错(而非
   依赖大小写不敏感文件系统静默命中),防止拼写错误、跨平台差异和同一 hook 被别名重复执行。
4. `modules/` 下存在但未被**任何已声明 profile** 引用的模块是合法的,`dot status` 列为
   "unassigned"。只是不在当前 profile、但被其他 profile 引用的模块不属于 unassigned;
   两者都**不能被本次 apply/add 直接操作**(ADR-18,04 号文档 §4.2/§4.5)。
5. `[data.<key>]` 声明一个字符串变量;`prompt`、`default` 与 `from_env` 出现时也必须
   是字符串,机器配置中的对应值统一为字符串。用户键必须匹配
   `^[a-z][A-Za-z0-9_]*$`(与大写开头的内建变量形成强制命名空间,并保证可用 `.key`
   直接引用;06 号文档 §3),违反即解码错误。`from_env` 出现时必须匹配
   `^[A-Za-z_][A-Za-z0-9_]*$`,表示按该精确名称读取环境变量;空值或其他写法非法。
   `prompt` 缺失时以 key 本身作为交互标签。
   init 选择默认值时依次使用已有机器配置值、非空的 `from_env` 快照、manifest
   `default`;三者均无且未显式提供时该变量为必填。空字符串是合法显式值,不得与“缺失”
   混为一谈。`default`/`from_env` 只帮助 init 生成机器配置,渲染时不作为隐式 fallback;
   当前 manifest 声明的任一 data 键在机器配置中缺失时,需要渲染的命令必须在 plan 前失败
   并提示重新运行 init。
6. **CLI 不写任何 manifest**(ADR-28):`dot add` 遇到目标模块 ∉ 当前 profile 时报错,
   并打印需手动添加进 `[profiles]` 的确切行,而非代为修改——保格式 TOML 编辑脆弱、
   整文件重序列化丢注释与排版,均不值得为低频操作引入。

## 3. 模块 manifest 完整字段

模块 manifest **整体可选**,存在时顶层只允许 `os`、`target`、`ignore`、`files`、`hooks`。
没有 `dot.toml` 的模块继承顶层 `[defaults]` 和 §2 的内建
缺省、无 hooks,文件级行为全按后缀推断;因此顶层未声明 target/os 时仍等价于
`target = "~"`、全平台生效。

target 写法 A —— 所有生效平台使用同一位置:

```toml
# modules/zsh/dot.toml
os = ["darwin", "linux"]    # 取值为 GOOS:darwin | linux;缺省 = defaults.os
target = "~"
```

target 写法 B —— 按 OS 分表(处理路径差异;与字符串形态互斥):

```toml
# modules/vscode/dot.toml
[target]
darwin = "~/Library/Application Support/Code/User"
linux  = "~/.config/Code/User"
```

`os` 与 `[target]` 的 OS 键只允许 `darwin`、`linux`;其他值是 manifest 错误。`os` 是生效
平台过滤器,可以与字符串或表形态 target 共存;先过滤,再解析当前平台的 target。

**表形态硬约束**:模块经 os 过滤在当前平台生效、而 `[target]` 表缺少当前 GOOS 键 →
resolve 阶段**硬错误**(而非静默跳过或回退默认——那会把配置 bug 变成静默行为差异)。

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

模块 `[ignore]` 仅含可选字符串数组 `patterns`;每个 `[files."<path>"]` 仅含可选字符串
`mode`、`kind`、`target`;`[hooks]` 的精确结构见下文。顶层 defaults 与模块的 `os` 都必须是
无重复的字符串数组,元素只允许 `darwin`/`linux`,空数组合法且表示该层不在任何平台生效。
两处 target 表都至少含一个支持的 OS 键,各值均为字符串;继承后的模块若在当前平台生效却
缺对应 target,仍按下文硬错。

`[files].mode` 出现时必须是恰好四个 ASCII 字符、匹配 `^0[0-7]{3}$` 的 TOML 字符串,
表示 `0000`–`0777` 的普通权限位;缺省为 `"0644"`。它只允许用于 effective kind 为
managed 或 scaffold 的条目,用于 link 是 manifest 错误,不得静默忽略。磁盘 mode 符合
声明意味着九个普通权限位相等且 setuid/setgid/sticky 均未设置;文件类型不属于 mode。
link 不管理 mode:target 访问到的是当前 checkout 中 source 的权限,跨机器只享有 Git 本身
对 executable bit 的可移植性,不承诺保存任意 Unix mode。需要精确权限的文件必须使用
managed/scaffold 并显式声明 `mode`。
`[files].kind` 出现时必须是 `link | managed | scaffold` 之一。每个 `[files]` 键以及
hook 的 script/watch 都必须指向模块内现存的普通文件;不存在、是目录或是特殊对象均为
manifest 错误,不能留到执行阶段再猜测。唯一例外是 `dot add` 对本次将发布 source 的预检:
校验结果必须等价于“这些精确 source 已作为普通文件加入后的仓库”再走同一套严格规则,
从而让 `[files]` 预先声明候选的 mode/kind/target;与本次候选无关的悬空引用仍是错误,候选也
必须最终唯一映射回输入 target。如何得到该等价结果属于实现,预检不修改 manifest。引用的
每个既有路径段还必须与实际目录项名称逐字节一致,不得因当前文件
系统大小写/Unicode 宽松而让同一配置跨平台指向不同对象。

hook 约定:脚本与其数据文件放模块顶层保留目录 `hooks/`。`[hooks]` 引用的一切路径
(script 与 watch)**统一归入内置忽略层级**:不参与链接、**不可被 `[files]` 覆盖**——
`[files]` 声明了 hook 引用的路径直接判校验错误。引用 `hooks/` 之外的路径时兼容执行,
但打印建议移入 `hooks/`。执行语义与指纹规则见 05 号文档 §8。
`[hooks]` 当前只允许可选的 `run_once` 数组,缺省为空。数组元素只能是非空 script 路径
字符串 [M1],或只含必填非空字符串 `script` 与可选字符串数组 `watch` 的 inline table [M2];
table 未知键、错误类型和空路径均非法,`watch` 缺省为空。同一元素的 watch 按文件系统身份
不得重复。同一模块内,按当前文件系统身份指向同一个 script 的路径最多声明一次
`run_once`;字符串形态等价于同 script、空 watch 的表形态。重复声明即使使用不同路径写法
或 watch 集也必须在严格校验时报错,不得通过覆盖 state、合并 watch 或执行顺序消歧。

文件定级优先级(高 → 低):**内置忽略(含 hooks 引用)> `[files]` 显式声明 >
ignore patterns > 后缀推断**。即:显式声明的文件即使匹配 ignore pattern 也被管理;
内置忽略不可解除。落地文件名 = 源文件名去掉 `.tmpl` / `.template` 后缀。

## 4. 合并语义(精确定义)

有效配置的计算,对每个模块独立进行:

```
effective(module) = 顶层 [defaults] 打底,被模块 manifest 中出现的键整键覆盖
```

- **整键覆盖(ADR-7)**:模块写了 `os = ["darwin"]`,则顶层 `defaults.os` 完全不参与,
  不做数组并集;`target`(无论字符串或表形态)同理,模块出现该键即整体替换。
- **唯一例外:`ignore.patterns` 取并集**。忽略规则语义上是「都要排除」,覆盖反而危险。
  此特例必须在文档与 `dot doctor` 输出中显式可见。
- 模块之间互不引用、互不合并;不存在"模块继承"。
- profile 只是名字集合,不携带任何配置,不参与合并。

## 5. ignore 规则语义

- 匹配输入是以 `/` 分隔、不带开头 `/` 的规范化模块相对路径;匹配**区分大小写且不做
  Unicode 折叠**,不随宿主文件系统改变。
- 支持的语法只包含普通字符、`/`、`*`、作为完整路径段的 `**`,以及可选的开头 `/` 和
  末尾 `/`:去掉可选末尾 `/` 后,既不以 `/` 开头、内部也无 `/` 的 pattern 匹配任意层级
  basename;以 `/` 开头或含内部 `/` 的 pattern 从模块根匹配;`*` 不跨 `/`;`**` 跨零个
  或多个路径段。`*` 可匹配零个或多个字符,包括开头的 `.`;末尾 `/` 只匹配目录。任一
  规则匹配目录时,该目录的全部后代一并忽略。
- 不支持 `!` 反选、`?`、字符类、转义或其他 glob 扩展;除可选首尾 `/` 外不得有空段、
  `.` 或 `..` 段,连续三个以上 `*` 非法,`**` 只能独占完整路径段。空 pattern 或其他
  不可解析 pattern 是 manifest 错误,不得降级成另一种解释。需要覆盖用户 ignore 时用
  `[files]` 精确声明(见 §3 优先级)。
- 内置规则精确为:模块根 `/dot.toml`、任意层级 basename 为 `.git` 的路径(若为目录则含
  全部后代)、模块根 `/hooks/`、
  `[hooks]` 引用的规范化精确路径,以及任意层级 basename `*.swp`;内置规则不可由
  `[files]` 覆盖。
- 匹配发生在 enumerate(pipeline ⑤),被忽略文件不进入 desired state。
- **「加入 ignore = 停止管理」**:若某文件此前曾被管理(state 有记录),把它加入 ignore 后,
  下次 apply 会将已部署产物按孤儿处理——即被 prune(仅限 owned 产物,且受收敛门控,
  05 号文档 §3.3/§6;仓库源文件本身无损)。这是有意语义,不是事故。
- **`dot add` 必须与正常 apply 使用同一套解析和枚举语义**(ADR-30,05 号文档 §9):
  试图 add 一个会被忽略的文件(README.md、`hooks/` 内容、命中模块 ignore)会在任何
  写入前被拒绝——保证 add 的产物是下轮 apply 的合法 desired。仓库自身的 Git ignore
  是另一项独立前置条件(04 号文档 §4.5),不能由本节匹配结果替代。如何复用由实现决定。

## 6. 路径合法性规则

所有来自 manifest 的路径输入在解码后立即校验,违者报错:

1. **模块名**(目录名、`-m` 参数、profile 引用)必须是单个安全路径段:匹配
   `^[A-Za-z0-9][A-Za-z0-9._-]*$`,不得含 `/`,不得为 `.` 或 `..`;对现有模块的引用必须
   与目录项名称逐字节一致,不借宿主文件系统做大小写或 Unicode 宽松匹配。
2. **`[files]` 键、hooks 的 script 与 watch 路径**必须为相对路径;词法规范化后不得
   指向模块目录之外,也不得为绝对路径。
3. manifest 中模块 `target`(**target root**)和 `[files].target`(**完整 entry target**)
   只接受 `~` 或 `~/...`;只展开开头的 `~` 为本次有效 HOME,不接受 `~user`、`$VAR`/
   `${VAR}`、相对路径、绝对路径或 NUL。`~/...` 必须是无空段、`.`、`..` 与尾随 `/` 的
   规范写法。
   target root 可以等于 HOME;entry target 必须是 HOME 的真后代。未声明 `[files].target`
   时,entry target = target root + 去掉模板后缀的模块相对路径;声明时该值是完整落地路径,
   且仍必须是 target root 的真后代。所有结果与 cwd/repo 无关。
4. **模块文件树内不允许 symlink 与特殊文件**(fifo/socket/设备):enumerate 遇到即
   报错。需要链接语义的内容本来就应由 dot 在 target 侧生成,仓库里出现 symlink
   几乎必是失误(且是目录逃逸的载体)。
5. **控制面路径保留**(ADR-33):控制面家族必须彼此隔离;任何 entry target 不得与本次
   有效 repo、机器配置、state 家族或已安装二进制重叠。该限制同时适用于 state 加载和
   `dot add`,不能被 `[files].target` 或命令行覆盖解除。
6. **target 身份唯一**(ADR-35):碰撞、祖先关系、控制面重叠与 state 匹配按当前文件系统
   的等价语义判断,不能只比较规范化字符串;既有祖先 symlink 形成的别名也不能绕过,
   具体识别方式不属于 manifest 格式。

以上是 manifest 的词法与静态边界。运行时从 HOME/target root 到 entry target 父目录的
现存祖先必须可作为目录使用:指向目录的 symlink 是允许的用户拓扑,普通文件、悬空链接或
特殊对象则会阻断路径。工具不得替换这些祖先,也不得因祖先别名绕过 target 唯一性或控制面
边界;如何安全遍历与维持提交前提由实现决定(05 号文档 §1/§6)。

## 7. 校验命令

`dot doctor` 对 manifest 做静态检查:requires 缺失/非法或不满足、TOML 语法、未知键、profile 环、
悬空模块引用、`[files]` 键指向不存在的文件或 hook 引用路径、`target` 表中不支持的
OS 键或**缺当前 GOOS**、路径合法性(§6 全部词法规则)、控制面路径冲突、target 身份全局唯一性
(复用 05 号文档 §5)、模板引用未声明变量、**已被 git 跟踪的 `*.local` 文件**(交互模式警告;
`--manifest-only`/CI 模式判为**错误**,ADR-32)。`--manifest-only` 不读取或要求机器配置
与 state:未传 `--profile` 时在当前 GOOS 上逐一展开并校验每个声明 profile,全局 target
不变量按 profile 分别计算,不得把互斥 profile 合并成一个集合;传入 `--profile` 只缩小
profile 级校验,仓库级语法、引用和模块局部检查仍覆盖全部 manifest。unassigned 模块接受
局部检查但不加入任何 profile 的碰撞集合。CI 在 macOS/Linux 分别运行不带 profile 的
`dot doctor --manifest-only`。该模式的控制面检查只覆盖无需读取机器配置即可由默认值、环境
和本次 flag 确定的路径;不得宣称已经检查机器配置内的 repo override。完整 doctor 才按
有效机器配置覆盖全部控制面家族。

## 8. 两阶段加载与兼容性(ADR-16)

加载协议:

1. **宽松预读**:只读取必填的顶层 `requires` → 校验语法并比较版本。不满足时提示
   `dot self-update`;`version=dev`(本地开发构建)仅放行版本比较并警告。
2. **严格解码**:凡命令或阶段需要解释 manifest 以生成 desired、计划或新 state,都必须在
   requires 通过后完整解码全部 manifest;任何未知字段或非法值使该阶段停止。
3. **诊断例外**:`doctor` 不因 requires 不满足而提前停止,以诊断模式报告未知键及其他
   可继续发现的错误;`version` 只预读并报告。`self-update` 与 `git` 不受 manifest 门禁。

| 命令或阶段 | manifest 契约 |
|---|---|
| `apply`、`add`、`diff`、`status`、`edit`、`state rebuild` | requires + 严格解码 |
| `init` 的配置阶段 | requires + 严格解码;随后 apply 另受 state 门禁 |
| 普通 `update` | pull 后对新仓库执行 requires + 严格解码的 apply 阶段 |
| `update --no-apply` | pull 后停止,不校验**拉取后的** requires/manifest |
| `doctor` / `doctor --manifest-only` | 报告 requires,诊断模式继续;后者不读机器配置/state |
| `version` | 只预读并报告;缺失/非法仍是配置错误 |
| `self-update`、`git` | 不读取 manifest 即可运行 |

这样设计的原因:提升 `requires` 是「唯一需要人肉自觉」的步骤;当作者忘记提升、而新
配置含有影响路径或所有权语义的新字段时,旧 CLI 若仅警告后继续 prune,风险不可接受。
严格解码把这种遗忘变成**失效安全**:旧 CLI 明确拒绝依赖该配置的阶段,而不是错误执行。

自本规范首版起不存在缺少 `requires` 的有效旧格式。此后的格式演进规则不变:**新版 CLI
必须能读此前有效格式;新字段必须有缺省行为;配置一旦使用新字段,同一 commit 必须提升
`requires`**(07 号文档 §3)。
