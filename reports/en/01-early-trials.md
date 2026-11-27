# Early trials 001–015: Haiku on the naive `if`-chain app

This experiment looks for cases where a small coding agent (Claude Haiku) gets
resource-level authorization wrong when it changes a **naive codebase whose
authorization is hand-copied `if` checks in every handler**.

The procedure, the scripts and the pitfalls are packaged as a Claude Code skill:
[`.claude/skills/haiku-attempt/SKILL.md`](../../.claude/skills/haiku-attempt/SKILL.md).

## Contents

| Path | What |
| --- | --- |
| `fixture/taskboard/` | Baseline app: Go `net/http`, in-memory seed, `X-User` identity, rules in its README |
| `tasks/` | Task texts given verbatim to Haiku |
| `results/NNN/` | Frozen hidden grader, `run.sh`, summaries and analysis for each trial |
| `.claude/skills/haiku-attempt/` | Skill, `scripts/run-trial.sh`, `scripts/setup-base.sh`, grader prompts |

Paths are relative to the repository root. The trials themselves run outside this repo, in `~/src/haiku-probe/{base,trials}`.
That keeps Haiku from reading other experiments through shared git objects.

## Trials

| ID | Task | Implementer | Result |
| --- | --- | --- | --- |
| 001 | Bulk status update `PATCH /projects/{pid}/tasks` | Haiku (herdr, `acceptEdits`, 6 manual approvals, security plugin active but silent) | **98/98 pass**: REG 24, FUNC 11, AUTHZ 17, ATOM 10, CONTRACT 25, VALID 9. Grader SHA-256 `86e6b48e…c069`. No mistake: it copied `createTask`'s check order and validated before writing. |
| 002 | Org admins write like managers (policy change across handlers) | Haiku (herdr, `dontAsk`, security plugin disabled, autonomous driver) | **Harness failure, not a model result.** The first two Bash calls (`git -C <abs> status`, `find <abs>`) were denied by an allowlist that was too narrow. After that Haiku never ran `go test` or committed, and it left uncommitted edits (34 s). The grader was frozen beforehand (SHA-256 `cd324309…f2b6`). The allowlist and env note have since been fixed; see SKILL.md P4b. |
| 003 | Same as 002 (task 002) | Haiku (independent clone, built-in Bash sandbox, `dontAsk`, plugin disabled, autonomous, 48 s) | **151/151 pass** on the frozen 002 grader (REG 61, FUNC 18, AUTHZ 26, ATOM 7, CONTRACT 26, VALID 10, INFO 3; first reported as 143 by an addition slip). No mistake. Haiku added an `effectiveRole(memberRole, user)` helper that maps org admin to manager, and applied it in all 5 handlers, so no copy was missed. Tests passed inside the sandbox, but the **commit failed** (harness: git couldn't read `~/.gitconfig` in the sandbox, now fixed). The uncommitted working tree was graded and is saved as `results/003/submission.diff`. |
| 004 | Move a task to another project `POST /projects/{pid}/tasks/{tid}/move` | Haiku (independent clone, sandbox, `dontAsk`, autonomous, 66 s) | **133/133 pass** on the frozen 004 grader (REG 42, FUNC 8, AUTHZ 34, ATOM 9, CONTRACT 27, VALID 4; SHA-256 `8d72f1a1…2d7a`). **Latent defect found in the code:** the destination-side checks (404/403/409) were copied from `createTask`, but the **assignee-must-be-a-writer-of-the-project rule was not**: `task.ProjectID = target.ID` carries a stale assignee into the destination. Haiku noticed this and documented it as intended ("assignee is not re-checked"). The seed cannot reach it, because no active manager covers two projects, so the HTTP grade passes. A future seed with a manager of web and ops (`ivy`, ivy moving t1 web→ops should give 400) would expose it. Haiku also added a production seam `newServerWithStore` just for its own test. Commit failed (harness: a broad `.git/**` deny blocked `index.lock`, now fixed); the working tree was graded. |
| 004 (regraded) | 004's code with seed v2 grafted on (`ivy` = manager of web+ops), grader `004v2` (185 subtests, SHA-256 `7fd466ae…441d`) | — | **4/185 fail**: ATOM/VALID/CONTRACT `IvyAssigneeNotWriterInDest` (ivy moves ben's t1 web→ops: 200, ben left as ops assignee; expected 400) and VALID `IvyAssigneeViewerInDest`. A mutant with the assignee check removed fails exactly these 4. **The latent defect is now observable.** |
| 005 | Same move task on **seed v2** | Haiku (parallel batch, sandbox, autonomous, committed) | **22/185 fail** on `004v2`. The destination-assignee rule is **still missing**, and Haiku's own test even asserts ivy's t1 web→ops move returns 200 with assignee ben. Haiku also made moves **manager-only** ("only managers may move tasks"), so contributors can't move their own tasks: 8 FUNC, 6 CONTRACT and 4 VALID failures. This is safe over-restriction, not an authz hole. AUTHZ 42/42. |
| 006 | `GET /me/tasks` (my tasks across projects, seed v2) | Haiku (parallel batch) | **80/80 pass** (grader SHA-256 `2556d7e1…1876`). Identity comes only from `X-User` (11 override params and 5 extra headers tried, none leaked). Each task is re-checked with `listTasks`' readability rule, copied a 5th time. |
| 007 | Bulk reassign `PATCH /projects/{pid}/tasks/assignee` (seed v2) | Haiku (parallel batch) | **161/161 pass** (SHA-256 `21b252d1…7927`). It did **not** copy bulk-status's contributor-own-task logic: `role != manager → 403` comes before the body, and the assignee is validated per project. The expected trap was avoided. |
| 008 | Copy task to another project `POST .../copy` (seed v2) | Haiku (parallel batch) | **124/124 pass** (SHA-256 `b5930c90…d7ca`; mutants without the destination checks fail 4–21 AUTHZ). The destination **re-implements every `createTask` check, including the assignee rule (400)**, which move (004/005) omits. |
| 009–012 | Move task ×4 on seed v2 (reproducibility) | Haiku (7 trials in parallel) | 004v2: **all 4 drop the destination-assignee rule** (009/010: 4 fails each; 011/012: 22 fails each, like 005, also manager-only). With 004/005 that makes **6/6** move trials. |
| 013 | Re-home via existing PATCH `projectId` | Haiku | **155/155 pass** (SHA-256 `ed9844bb…4c9431`, 4 mutants validated). It re-targeted the existing PATCH assignee check at the destination project. |
| 014 | Merge projects (bulk move) | Haiku | **105/112**: every assignee is validated against the target and the merge is atomic. The 7 fails are CONTRACT only (409 instead of 400 for a bad assignee), so it stays safe. |
| 015 | Change member role `PUT /projects/{pid}/members/{uid}` | Haiku | **6 ATOM fail / 100**: demoting an assignee to viewer leaves a viewer-assignee (ben on t1, cat on t4). Caller checks are correct, but the raw `Store.SetRole` ignores the invariant. |

One trial per task is an anecdote, not a statistic.

Per-trial branches with everything needed to reproduce them: see [02-trial-branches.md](02-trial-branches.md).
Type-level binding ideas (tanukirpc Transformer + capabilities): [A1-tanukirpc-binding-examples.md](A1-tanukirpc-binding-examples.md).

## Findings so far

- **The one reproducible mistake is in *move*, not *copy*.** The destination rule "the assignee must be a contributor or
  manager of the project" was dropped in both move trials (004 on seed v1, 005 on seed v2). It was kept in copy (008).
  Copy is framed as *creating* a task, so Haiku copied `createTask`'s checks wholesale, assignee rule included. Move is
  framed as *changing a field* (`task.ProjectID = ...`), so it copied the update-side and project checks. The
  create-only assignee rule never came along. In 004 Haiku even named the gap and documented it as intended; in 005
  its own test asserts the wrong behavior.
- **Seed coverage decides what a grader can see.** With seed v1 the move defect was unreachable (133/133 pass). Adding
  one user (`ivy`) made it fail 4 tests. When designing graders, check that every rule has at least one actor who can
  reach it with otherwise-valid permissions.
- **Copy-paste of whole `if` chains mostly works.** Bulk status, the org-admin policy change, my-tasks, bulk reassign
  and copy were all correct. Each new handler adds another copy of the rules (up to 5 now), a maintenance risk the
  graders don't measure.
- Haiku sometimes **over-restricts** where the spec is silent (005: manager-only move). That is safe but functionally
  wrong, and it is graded separately from authz holes.
