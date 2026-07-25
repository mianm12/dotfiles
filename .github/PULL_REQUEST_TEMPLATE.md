## 目标

<!-- 说明本次变化解决什么，以及明确不解决什么。 -->

## 风险等级

<!-- Internal / Behavioral / Safety-critical / Operational，可多选。 -->

- [ ] Internal
- [ ] Behavioral
- [ ] Safety-critical
- [ ] Operational

失败模式：

<!-- 对 Behavioral 及以上变更列出主要失败方式；Internal 可写“不适用”。 -->

## 用户行为与规范

用户可观察行为：

<!-- 说明行为是否变化。 -->

规范影响：

<!-- 链接对应规范文件与标题；已有稳定验收编号时同时引用。无影响时说明原因。 -->

## 测试证据

<!-- 列出实际运行的命令、平台和结果。不要用预期结果代替真实证据。 -->

## 未验证项

<!-- 明确未运行或无法在当前环境验证的内容；没有则写“无”。 -->

## 回滚或前向修复

<!-- 说明安全撤销方式，或为什么只能前向修复。Operational 变更需包含真实环境恢复步骤。 -->

## 审查

- [ ] 已自审完整 diff 和相关 untracked 文件
- [ ] 已运行 `git diff --check`
- [ ] Behavioral 变更已完成独立契约审查，或不适用
- [ ] Safety-critical 变更已完成独立缺陷审查，或不适用
