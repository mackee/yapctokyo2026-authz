# Trial 007 (bulk reassign) analysis

Submission: trials/007, Haiku commit c0bceb1, base b1aa0e2. Grader hash verified (21b252d1...7927), integrity_ok true, build/vet OK, own tests 64 pass.

| Category | Baseline | Reference | Trial 007 |
| --- | --- | --- | --- |
| REG | 24/0 | 24/0 | 24/0 |
| FUNC | 0/18 | 18/0 | 18/0 |
| AUTHZ | 41/0 (vacuous) | 41/0 | 41/0 |
| ATOM | 14/0 (vacuous) | 14/0 | 14/0 |
| CONTRACT | 20/31 | 51/0 | 51/0 |
| VALID | 0/6 | 6/0 | 6/0 |
| INFO | 7 | 7 | 7 |

FUNC passes, so AUTHZ/ATOM passes are real. No failures to classify.

## Why it passed
bulkAssignTasks (main.go) copied updateTask's prologue but replaced the role gate with `if role != RoleManager -> 403`, placed before the archived check and before body parsing. It did not copy the bulk-status "contributor own task" logic, so the central contributor-reassign case (ben/cat) is denied. Validation (assignee role, duplicate ids, task-in-project) all happens before the single write loop, giving atomicity.

## INFO observations
- Empty taskIds, missing/null assignee, null taskIds: 400.
- Duplicate ids: 400 (stricter than a reference that dedupes; documented in README).
- Contributor with invalid assignee: 403 (role checked first).

## Latent defects the seed cannot reach
- Route `PATCH .../tasks/assignee` is a literal that shadows a task whose ID is "assignee"; IDs are `tN`, so unreachable.
- Assignee is validated before task ids, so a bad assignee plus a bad id returns 400 not 404 (grader treats as ambiguous, no contract).
- Assignee membership check duplicates updateTask's inline logic again (a third copy: createTask, updateTask, bulk), a drift risk if the rule changes.
- Response returns live *Task pointers but encodes under the store lock; safe.

## Own tests vs README
None contradict the README. Haiku also updated README endpoint table and rules (adds duplicate/empty 400, request-order response).
