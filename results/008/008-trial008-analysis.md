# Trial 008 (copy task) analysis

Submission: trials/008, commit 6ee38d6, base b1aa0e2. Grader hash verified by run.sh (integrity_ok=true, 124/124 subtests).

## Counts (pass/fail)
| Cat | Baseline | Reference | Mutant (a) no dest-assignee check | Mutant (b) source-only | Trial 008 |
|---|---|---|---|---|---|
| REG | 37/0 | 37/0 | 37/0 | 37/0 | 37/0 |
| FUNC | 0/11 | 11/0 | 11/0 | 11/0 | 11/0 |
| AUTHZ | 37/0 (vacuous) | 37/0 | 33/4 | 16/21 | 37/0 |
| ATOM | 2/1 | 3/0 | 0/3 | 0/3 | 3/0 |
| VALID | 7/0 | 7/0 | 7/0 | 7/0 | 7/0 |
| CONTRACT | 6/9 | 15/0 | 13/2 | 7/8 | 15/0 |
| INFO | 14 logged | 14 | 14 | 14 | 14 |

Mutation sanity: (a) fails AUTHZ IvyCopiesBenTaskToOps, ContribCopiesBenTaskToOps, both contributor-other-assignee cases, plus ATOM and CONTRACT 400/403: the suite catches the skipped assignee validation. (b) (only checks dest exists; no readability/role/archived/org check) fails 21 AUTHZ, 8 CONTRACT, 3 ATOM. Both mutants keep FUNC 11/11, so AUTHZ/ATOM failures are meaningful.

## Trial result
Zero failures in any category; FUNC passes so AUTHZ/ATOM are not vacuous. No unexpected acceptance, state change, leak, partial write, wrong status, validation gap or functional bug. No grader ambiguities triggered.

## Diff review (main.go copyTask)
- Source: user -> project org/read check (404) -> task in path project (404), same order as getTask. Reading only; write on source not required.
- Body parse (400) and same-project rejection come after source checks.
- Destination re-implements createTask checks: org/exist 404, readable 404, role 403, archived 409, assignee role 400, contributor-only-self 403 (inside `task.Assignee != ""`, matching create).
- New ID from NewTaskID, status "open", fresh Task struct (no aliasing of the source).
- Latent defects: none reachable. Notes: (1) the checks are copy-pasted a fourth time, so future rule changes (e.g. org admin write, new roles) must be applied here too; (2) same-project copy is rejected (400), an unrequested product decision; (3) README says a contributor "may only copy a task whose assignee is themselves", but code (like create) also lets a contributor copy an unassigned task (cat t3 web->ops gives 201): minor README/code mismatch.

## Own tests vs README
58 own tests pass. None contradict the README rules. TestCopy covers the main denials; the contributor/other-assignee case is tested via a setup task. Not tested: contributor copying unassigned (see README note), archived-source copy, copy to legacy as contributor.

## INFO answers
- ann copies t4 ops->web (viewer of source, manager of dest): 201 -> source write NOT required.
- ben copies own archived t5 legacy->web: 201 (archived source readable, so no source write requirement).
- cat copies unassigned t3 web->ops: 201.
- cat t1 web->ops: 400 (assignee check before contributor rule); ivy t1 web->ops: 400.
- same-project copy (ivy or cat, t2): 400 "toProjectId must differ".
- missing / empty toProjectId: 400.
- order: hal t1->nope: 404; ivy t5 legacy->legacy: 404 (source first); ann t1->ops: 403 (role before assignee); ann t2->legacy: 409; eve t5 legacy->web: 403.
