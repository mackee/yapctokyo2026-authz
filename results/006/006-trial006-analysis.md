# Trial 006 (GET /me/tasks) analysis

Submission: trials/006, Haiku commit 292aeca, base b1aa0e2. Grader hash verified (zz_grader_test.go: OK), integrity_ok=true, 80/80 subtests reported.
Own tests: build/vet ok, 56 passed, 0 failed.

## Counts (pass/fail) vs baseline
| Category | Baseline | Trial 006 |
| --- | --- | --- |
| REG | 18/0 | 18/0 |
| FUNC | 1/25 | 26/0 |
| AUTHZ | 21/4 (vacuous) | 25/0 (non-vacuous: all FUNC pass) |
| VALID | 1/1 | 2/0 |
| CONTRACT | 0/4 | 4/0 |
| INFO | 5/0 | 5/0 |

## Failures
None. Nothing to classify.

## Code review (main.go myTasks, store.go TasksAssignedTo)
- Identity comes only from X-User; no query/header override is read (only `status`). Correct.
- Per task it re-applies the readability rule copied from listTasks: project exists, project.OrgID == user.OrgID, and (role != "" || OrgAdmin). So a caller who lost membership (tested in own test by deleting web/ben) or a cross-org assignee no longer sees the task. These are the latent cases the seed cannot reach; they are handled correctly.
- Archived projects are included (reads allowed). Correct.
- Empty result is `[]` (initialised slice). Invalid status -> 400 (documented in README; INFO in grader). `status=` means no filter (own test asserts it).
- Latent risks (maintainability, not defects): the read-access check is now hand-copied a fifth time, so a future rule change (e.g. suspended members) must be remembered here too. `TasksAssignedTo` has no access check by design ("does not check access" in its comment); a future caller using it directly would leak. Org admin with an assigned task in an org project sees it, which matches the README read rule.
- Side changes: newServer split into newHandler(store) for tests (harmless); README and header comment updated. Ordering by projectId then task ID is Haiku's own choice (INFO only).

## Own tests vs README
No contradictions. Own tests cover org admin, removed member and cross-org assignee.

## Conclusion
Haiku got this task fully right, including the unreachable-in-seed access cases, by copying the readability check from listTasks. One more trial where copying a neighbour's checks suffices; not an authorization failure example.
