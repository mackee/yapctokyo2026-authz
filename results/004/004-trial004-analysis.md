# Trial 004 vs frozen grader 004 (working tree, uncommitted, base a261dee)

Grader hash verified (8d72f1a1...2d7a). Raw: results/004-trial004.json. Build ok, vet ok, own tests exit 0.
Diff read via trial-git.sh with `diff --no-ext-diff` (plain `diff` failed on an external diff driver).

| Category | Baseline | Reference | Trial 004 |
|---|---|---|---|
| REG | 42/0 | 42/0 | 42/0 |
| FUNC | 0/8 | 8/0 | 8/0 |
| AUTHZ | 34/0 | 34/0 | 34/0 |
| ATOM | 9/0 | 9/0 | 9/0 |
| CONTRACT | 13/14 | 27/0 | 27/0 |
| VALID | 0/10->0/4 | 4/0 | 4/0 |
| INFO | 9/0 | 9/0 | 9/0 |

(VALID baseline is 0/4.) FUNC passes, so AUTHZ/ATOM passes are meaningful. No failing subtests.

## INFO observations (differences from the reference, all ungraded)
- Same-project move: 400 "task is already in that project" (reference 200).
- Missing or empty toProjectId: 400 (reference 404). Unknown project: 404.
- Check order: viewer source vs non-member dest -> 403 (source first); archived source vs viewer/non-member dest -> 409 (source first); eve on legacy -> 403.

## Latent defect (code review)
main.go moveTask, final lines: after the destination 404/403/409/same-project checks it does only
`task.ProjectID = target.ID`. It never validates `task.Assignee` against `s.store.Role(target.ID, task.Assignee)`.
createTask (and updateTask reassign) require the assignee to be contributor/manager of the project, else 400.
So a task assigned to a non-member of the destination is moved with the stale assignee.
Haiku knew: the README line it added says "The assignee is not re-checked against the destination project's roles", which is a documented deviation from the create rule, not an accident.

Why frozen suite can't reach it: the check only matters when the caller passes both write checks yet the task's assignee is not a writer in the destination.
- Contributor source: assignee must equal the caller, who is a contributor in the destination (else 403/404 earlier) -> always valid.
- Manager source needs manager/contributor in the destination. Seed has only ann (manager web+legacy, viewer ops); legacy is archived (409). No user is a non-archived manager/contributor-capable pair where a foreign assignee exists (ann web->ops is 403 first).
So 400 is unreachable; the grader has ATOM/AUTHZ cases (ann t1 web->ops, ben t1 web->ops) only as safety checks that fail on the other check first.

Own test: TestMoveByManagerOfBothProjects mutates the store (st.members["ops"]["ann"]=manager) via a new production seam `newServerWithStore`, but moves t2 (assignee cat, valid in ops), so it exercises no assignee case; a seam added only for test use.

Other destination checks:
- Dest 404 (not found / other org / non-member): present, copied; reachable (ben, cat->gx).
- Dest 403 (read-only): present, reachable only via ann (viewer in ops).
- Dest 409: present, reachable (ann/ben -> legacy).
- Org-admin branch `targetRole == "" && !user.OrgAdmin` is copied but effectively dead: eve fails the source write check first.
- Same-project 400 is last, after the dest role/archive checks; harmless.
- Order versus reference: identical except same-project (late 400 vs allowed 200) and missing toProjectId (400 vs 404 in reference).

## Proposed seed addition (future grader revision only)
Add user `ivy` (acme, not admin) as manager of both web and ops: members web: ivy manager; ops: ivy manager. Then:
- ivy moves t1 (assignee ben, not in ops) web->ops: expected 400, state unchanged (observable defect: trial 004 returns 200 and leaves ben on an ops task).
- ivy moves t2 (cat, ops contributor) web->ops: 200; t3 (unassigned): 200.
- ivy moves t4 ops->web: 200 (cat is a web contributor).
Optionally also a user ("zoe") contributor in ops only to test contributor-own with a stale assignee after reassign. Keep ann/eve/dan/hal/gus unchanged so existing REG cases remain valid.
