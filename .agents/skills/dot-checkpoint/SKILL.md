---
name: dot-checkpoint
description: Execute, continue, review, or accept refactor checkpoints from docs/cutover-plan.md in the dot repository, including orchestrating a persistent Codex goal across the full cutover one checkpoint at a time. Use when Codex is asked to run a CP, B, or C checkpoint, start or resume a cutover goal, perform independent promotion reviews, or produce a checkpoint acceptance report.
---

# Dot Checkpoint

Execute one checkpoint without expanding its scope or weakening its gates.

For a persistent goal spanning multiple checkpoints, read
[references/goal-orchestration.md](references/goal-orchestration.md) completely before selecting
the next checkpoint. A goal may span the full plan, but each continuation still executes at most
one checkpoint.

## Establish the checkpoint

1. Read `AGENTS.md`, `CONTRIBUTING.md`, `docs/design-baseline.md`, and
   `docs/cutover-plan.md` completely.
2. Identify the requested checkpoint, its exit conditions, its mapped §13 acceptance items, and
   its allowed files.
3. Record the current HEAD as `checkpoint_base`, then inspect the branch, worktree status, relevant
   implementation, tests, `Makefile`, and CI workflow before changing anything.
4. Preserve all pre-existing staged, unstaged, and untracked work. Stop if it overlaps the
   checkpoint and cannot be preserved safely.

Run only the requested checkpoint. Do not start the next checkpoint in the same run.

## Protect the contract

- Treat `docs/design-baseline.md` and `docs/cutover-plan.md` as read-only during checkpoint
  execution.
- If the checkpoint cannot satisfy its exit conditions without changing either document, stop.
  Report the concrete failure case, implementation cost, and consequence of not implementing it
  as required by baseline §14.
- Do not use archived documentation or implementation plans to decide current behavior.
- Do not read or mutate real `modules/`, `*.local`, `.env`, machine config, state, lock, backup, or
  HOME data. Use absolute synthetic HOME/repo/config/state/lock paths for mutation verification.

## Implement and verify

- Work only on the checkpoint branch. Small, independently meaningful steps may be committed using
  `CONTRIBUTING.md`; do not merge, push `main`, tag, or release.
- Make the smallest change that satisfies the checkpoint. Do not prebuild later checkpoints.
- Name acceptance tests after baseline §13, such as `TestAcceptance05_...`, and assert through the
  CLI: command, exit code, and filesystem results.
- After every successful mutation scenario, run the same apply again and assert zero new
  filesystem mutation.
- Never delete or weaken a test merely to make the gate pass. Delete tests only with a removed
  §2 non-goal, and identify that contract in the commit message.
- Run focused tests while developing, then the complete checkpoint gate. For Go, dependency,
  build, or CI changes, run `make check`.
- Report local execution, cross-compilation, and remote CI as separate evidence. Never present an
  unrun check as passed.

## Use parallel work narrowly

- Parallel implementation is allowed only for B2 and B3, and only when their files are disjoint.
- Parallel branches must not modify shared `go.mod`, `go.sum`, `Makefile`, CI, docs, or CLI wiring.
- Add any shared dependency in a separate, serial commit before parallel B work begins.
- Integrate B2 before refreshing B3. Do not rebase, rewrite history, or choose another integration
  strategy without explicit user authorization.

## Run promotion reviews

After the implementation is complete and `make check` is green:

1. Record the reviewed branch HEAD.
2. Start two independent read-only reviewer contexts in parallel:
   - `contract_auditor`, following
     [references/contract-auditor.md](references/contract-auditor.md).
   - `bug_hunter`, following [references/bug-hunter.md](references/bug-hunter.md).
3. Give reviewers only the checkpoint diff `checkpoint_base..reviewed_head`,
   `docs/design-baseline.md`, the claimed §13 items, and the relevant reviewer reference. Do not
   pass implementation-session narration or expected findings.
4. Treat only correctness defects and contract violations as promotion blockers. Record style
   comments as non-blocking.
5. Fix confirmed blockers in new commits and rerun both reviews from fresh contexts. Allow at most
   two fix-and-rereview rounds; unresolved disagreement goes to the user.
6. Run a freshness gate: confirm the reviewed HEAD is still the branch HEAD, the
   `checkpoint_base..HEAD` diff is the reviewed diff, the worktree contains no unexpected changes,
   and the required checks remain green.

The primary context alone decides whether the checkpoint is complete.

## Report completion

Return a checkpoint report containing:

- checkpoint and final branch HEAD;
- changed behavior and files;
- mapping to every claimed baseline §13 item;
- focused tests and `make check` results;
- local, cross-compile, and remote CI evidence;
- reviewer results and freshness-gate evidence;
- deviations, unresolved risks, and unrun checks.

Do not declare completion while a required check, reviewer, freshness gate, or exit condition is
missing.
