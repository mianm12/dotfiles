# Goal orchestration

Use one persistent Codex goal to carry the cutover across multiple continuations while keeping each
checkpoint independently reviewable.

## Contents

- Keep durable and per-run instructions separate
- Start only from a stable base
- Advance one checkpoint per continuation
- Use one draft pull request for remote CI
- Stop at decision boundaries
- Goal text template

## Keep durable and per-run instructions separate

- Keep reusable workflow, safety gates, and recovery rules in this reference and `SKILL.md`.
- Keep repository-wide hard boundaries and the skill entrypoint in `AGENTS.md`.
- Put only this run's outcome, branch, authorization, and completion criteria in the goal text.
- Do not copy the full cutover plan or this reference into the goal text.

## Start only from a stable base

Before creating the goal:

1. Check whether the current chat already has an unfinished goal. Resume or edit it only under user
   direction; do not create a competing goal.
2. Confirm the Codex workflow configuration is committed and the worktree is clean.
3. Require the goal text to name an immutable starting commit and one long-lived cutover working
   branch. Create or select that branch exactly from the named commit; do not infer a different
   base from `main`, the current branch, or remote state.
4. Record the named starting commit as the first `checkpoint_base`.
5. Confirm the goal text states whether Codex may:
   - commit on the working branch;
   - push that branch and create or update one draft pull request;
   - perform no merge, push to `main`, tag, release, rebase, or history rewrite.
6. Leave C2 real-machine access unauthorized. It requires a separate, contemporaneous user
   decision after a read-only impact report.

If remote CI is required but working-branch push or draft-PR creation is not authorized, do not
start implementation. Report the missing authorization instead.

## Advance one checkpoint per continuation

Use this order:

```text
CP0 -> CP1 -> B1 -> B2 -> B3 -> B4 -> B5 -> B6 -> C1 -> C2 -> C3
```

Default to serial B2 then B3. Use parallel implementation only when the user explicitly authorizes
the required worktrees and integration strategy.

For every continuation:

1. Read the current goal and inspect the branch, HEAD, worktree, recent checkpoint commits, and
   remote CI state.
2. Identify the first checkpoint without a completed promotion gate.
3. Set `checkpoint_base` to the prior checkpoint's final HEAD, or the goal starting HEAD for CP0.
4. Execute only that checkpoint through the main `SKILL.md` workflow.
5. Use checkpoint-scoped commit subjects such as `refactor(cp0): ...`. Make sure the final
   checkpoint state is represented by committed history before review.
6. Review only `checkpoint_base..reviewed_head`.
7. Complete local checks, both independent reviews, remote CI, and the freshness gate.
8. Emit the checkpoint report and end the continuation. Do not begin the next checkpoint in the
   same continuation.

The next continuation uses the accepted checkpoint HEAD as its new base. Do not maintain a second
progress checklist in `docs/cutover-plan.md`; committed history, the goal state, and checkpoint
reports are the progress evidence.

## Use one draft pull request for remote CI

The repository CI runs for pull requests and pushes to `main`, not ordinary working-branch pushes.
When authorized:

1. Push the working branch without force.
2. Create one draft pull request if none exists; reuse it for later checkpoints.
3. After each reviewed checkpoint, push its exact HEAD and wait for all required macOS/Linux jobs.
4. Treat a CI fix as a new checkpoint commit: rerun affected checks, both fresh reviews, CI, and the
   freshness gate.
5. Never promote a checkpoint with red, cancelled, stale, or missing required CI.

A pre-existing failure in code that the current checkpoint explicitly deletes does not require a
separate compatibility fix. The checkpoint HEAD itself must still obtain green CI before promotion.

## Stop at decision boundaries

- If a checkpoint cannot meet its exit conditions without changing the protected baseline or plan,
  stop and escalate under baseline §14.
- If two fix-and-rereview rounds do not resolve a blocker, stop for user judgment.
- Before C2, stop without reading or mutating real machine config, state, lock, backup, `*.local`,
  modules, or HOME data. Provide the intended paths, archive operation, rollback limitations, and
  verification commands, then request explicit authorization.
- End the C2 pre-authorization continuation with the decision request. Keep the goal unfinished;
  do not mark it complete or blocked, retry C2 automatically, or advance to C3 while the request is
  unanswered. Resume only after the user responds in the same goal.
- Do not mark the goal complete while any checkpoint, C2 approval, required evidence, or C3
  completion gate is missing.

## Goal text template

```text
使用 $dot-checkpoint 从固定基点 <starting-commit> 创建或使用 <working-branch>，完成
docs/cutover-plan.md 的全部检查点；不得自行改用 main、当前分支或远端其他基点。
每次自动续跑最多完成并晋级一个检查点，严格按 CP0、CP1、B1、B2、B3、B4、B5、B6、C1、C2、C3
顺序；每个检查点使用起始 HEAD 到评审 HEAD 的独立 diff，完成本机门禁、两项独立评审、远程
macOS/Linux CI 与 freshness gate 后才可进入下一次续跑。

授权：可在工作分支小步 commit；<允许或不允许> push 工作分支并创建/更新一个 draft PR。
不允许 merge、push main、tag、release、rebase、force push 或历史重写。
C2 真实机器切换尚未授权；到达 C2 前必须停止并提交只读影响报告，等待单独明确授权。

完成条件：C3 完成，cutover DoD 全部有真实证据，工作区无意外改动，并输出最终检查点汇总。
```
