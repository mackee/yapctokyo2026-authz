# haiku-attempt: success and failure cases (for reproduction)

This is a record of trials where we asked Haiku (`claude --model haiku`) to add features to a naive Go app (taskboard) built from chains of `if` statements.
Each trial is saved on a branch `haiku-trials/NNN`.

## Branch layout

| Commit | Contents |
|---|---|
| 1st | Base (identical to `fixture/taskboard` or `fixture/taskboard-v2`) |
| 2nd | Haiku's submission (the working tree, excluding `.git`) and the metadata in `.trial/` |

Contents of `.trial/`:
- `task.md`: the task text given to Haiku (verbatim)
- `haiku-settings.json`: the settings given to Haiku (permissions, sandbox, environment variables)
- `meta.json`: verdict, model, fixture, task, grader and grade-summary path
- `grade-summary.json`: grading result from the frozen grader

The driver log and the Haiku session transcript were also on these branches originally. They are not published; the run status from the driver logs is in `results/run-status.tsv`.

The branches were not imported from the trial's `.git` (which is untrusted). They were rebuilt from the working-tree files.

## How to reproduce

**1. Reproduce only the grading (deterministic)**
Extract the branch and run the frozen grader on it. The graders are in `results/<grader>/` (check the hash with `FROZEN.sha256`).
```sh
X=$(mktemp -d /private/tmp/repro.XXXXXX)
git archive haiku-trials/015 | tar -x -C "$X" && rm -rf "$X/.trial"
results/015/run.sh "$X" /private/tmp/repro-015.json   # go runs inside sandbox-exec
jq '{integrity_ok, failures: [.failures[].name]}' /private/tmp/repro-015.json
```
Tried on 015, 009 and 013. Each gave the same set of FAILs as recorded.

**2. Redo from implementation (Haiku's output is stochastic)**
Use the harness in this repository.
```sh
FIXTURE=<fixture dir> BASE_REPO=~/src/haiku-probe/base-v2 .claude/skills/haiku-attempt/scripts/setup-base.sh
BASE_REPO=~/src/haiku-probe/base-v2 .claude/skills/haiku-attempt/scripts/run-trial.sh <new-id> tasks/<task>.md
results/<grader>/run.sh ~/src/haiku-probe/trials/<new-id> <out.json>
```
Requirements: run inside herdr, `/opt/homebrew/bin/go`, macOS `sandbox-exec`. For caveats, see Pitfalls in SKILL.md.

## Failure cases

### F1. Missing destination assignee rule (move task, 6/6)
- **Task**: `POST /projects/{pid}/tasks/{tid}/move {"toProjectId":...}` (`tasks/004-move-task.md`; 005 uses the same text).
- **Mistake**: It checks 404/403/409 for the destination, but not "the assignee must be a contributor/manager of the destination, otherwise 400". It ends with `task.ProjectID = target.ID`.
- **Input that reproduces it**: ivy moves t1 (assigned to ben) from web to ops → **200, and ben stays as the assignee in ops** (correct: 400 and nothing changes).

| Branch | Seed | Grader | Result | Notes |
|---|---|---|---|---|
| `haiku-trials/004` | v1 | 004 (133 tests) | 133/133 PASS | Not reachable with the v1 seed (latent). Haiku itself wrote in the README that it "does not re-check" |
| Same (v2 seed grafted on) | v2 | 004v2 (185 tests) | **4 FAIL** | `results/004v2/004v2-trial004-grafted.json` |
| `haiku-trials/005` | v2 | 004v2 | **22 FAIL** | Missing assignee rule (4) plus moves restricted to managers (safe-side over-restriction, 18). Its own test expects ivy's move of t1 to return 200 |
| `haiku-trials/009` | v2 | 004v2 | **4 FAIL** | Missing assignee rule only |
| `haiku-trials/010` | v2 | 004v2 | **4 FAIL** | Same |
| `haiku-trials/011` | v2 | 004v2 | **22 FAIL** | Same pattern as 005 (manager-only) |
| `haiku-trials/012` | v2 | 004v2 | **22 FAIL** | Same |

Failing subtests: `ATOM_/CONTRACT_/VALID_IvyAssigneeNotWriterInDest` and `VALID_IvyAssigneeViewerInDest`.

### F2. Invariant violation through role demotion (015)
- **Branch**: `haiku-trials/015`. Grader 015 (100 tests), **6 ATOM FAIL**.
- **Task**: `PUT /projects/{pid}/members/{uid} {"role":...}` (`tasks/015-change-member-role.md`).
- **Mistake**: The caller checks (manager only, 409 if archived, target is a member) are correct. But it only adds `SetRole` to the store (a direct assignment, `s.members[p][u] = role`) and calls it. It does not look at assigned tasks.
- **Input that reproduces it**: ivy demotes ben to viewer in web → **200, and ben becomes "a viewer who is still the assignee of t1"**. Demoting cat in ops does the same (t4).
- Failing subtests: `ATOM_DemoteAssigneeToViewer_{benWeb,benWebByManagerAnn,catOps,catWeb}`, `ATOM_DemotedManagerAssignee_ivy`, `ATOM_DemoteViaBothPathsNoViewerAssignees`.

### Reference: harness failures (not model results)
- `haiku-trials/002`: The allowlist was too narrow, so every Bash call was denied. No tests ran and nothing was committed (SKILL.md P4b). Not graded.
- 003 and 004 passed their tests but failed only at commit (inside the sandbox `~/.gitconfig` could not be read, and the `.git/**` deny rule blocked `index.lock`). The working tree was graded.

## Success cases

| Branch | Task | Grader / result | Why it was correct (from the code) |
|---|---|---|---|
| `haiku-trials/001` | Bulk status update | 001 / 98/98 | Copied `createTask`'s checks in the same order, and checked every item before writing |
| `haiku-trials/003` | Policy change: treat org admin as manager | 002 / 151/151 | Created `effectiveRole()` on its own and applied it to all 5 handlers |
| `haiku-trials/006` | "My tasks" across projects | 006 / 80/80 | Takes the user only from `X-User`. Passed each task through the same "can read" check as `listTasks` |
| `haiku-trials/007` | Bulk assignee change | 007 / 161/161 | 403 for non-managers (did not copy 001's "allowed if it is your own task"). Also validated the assignee per project |
| `haiku-trials/008` | Copy to another project | 008 / 124/124 | Implemented all of `createTask`'s checks on the destination, including the assignee rule. Contrast with F1 |
| `haiku-trials/013` | Add `projectId` to PATCH to re-home | 013 / 155/155 | Re-targeted the existing PATCH assignee check at the destination project (`s.store.Role(target.ID, resultAssignee)`). Same operation as F1, but done correctly when it goes through the existing check |
| `haiku-trials/014` | Merge projects (bulk move) | 014 / 105/112 | Validated every assignee against the merge target and processed atomically. All 7 FAILs are CONTRACT (409 where 400 was expected), so it stays on the safe side |

## What the contrast between F1 and the success cases shows
- For the same "validate the assignee at the destination", it was correct **when the operation goes through the existing create/update checks** (008 copy, 013 PATCH re-home, 014 merge). It was missing in 6 of 6 **when a new handler only rewrites `ProjectID`** (F1 move).
- F2 has the same structure. When a new write path (`SetRole`) is created, invariants that are not on the existing check path get dropped.
- Both correspond to items 1 and 2 in [`A1-tanukirpc-binding-examples.md`](A1-tanukirpc-binding-examples.md), as examples that model functions required by type can constrain.
