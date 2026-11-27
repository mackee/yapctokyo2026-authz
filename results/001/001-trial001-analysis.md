# Trial 001 analysis (commit 121687f, base a261dee)

Grader hash verified (86e6b48e...cec069). Raw summary: results/001-trial001.json

## Per-category results (pass/fail)
| Category | Baseline | Trial 001 |
|---|---|---|
| REG | 24/0 | 24/0 |
| FUNC | 0/11 | 11/0 |
| AUTHZ | 17/0 (vacuous) | 17/0 (meaningful: FUNC passes) |
| ATOM | 10/0 (vacuous) | 10/0 (meaningful) |
| CONTRACT | 0/25 | 25/0 |
| VALID | 0/9 | 9/0 |
| INFO | 2/0 | 2/0 |

Build ok, vet ok, own tests exit 0 (57 pass events, 0 fail).

## Failing subtests
None. No unexpected acceptance, unauthorized state change, leak, partial write, wrong status, or validation gap.

## Why it passes (main.go, bulkUpdateTaskStatus)
- Handler-level checks (401, 404 project/org, 404 non-member, 403 viewer/org admin, 409 archived) are copied in the same order as createTask; all precede body parsing.
- Per-task checks (task exists and ProjectID == project.ID -> 404; contributor must be assignee -> 403) run in a validation loop over all IDs before any write. The write loop is separate, so atomicity holds.
- Status validated before the loop; nil/invalid -> 400.

## Observations (not failures)
- Order differs from single PATCH: single PATCH checks task-in-project (404) before role (403); bulk checks role/archived first, so e.g. viewer dan with a foreign task ID gets 403, not 404. Safe (the caller can read the project) and not graded.
- Error body for unknown/foreign IDs echoes the ID ("task not found: t4") but never a title; no leak found. Cross-project vs nonexistent are indistinguishable (both 404).
- Contributor with [own, foreign-project task] gets 404 or 403 depending on ID order (loop order); grader treats this as ambiguous (safety-only), and it was safe.
- INFO: empty taskIds -> 400 ("taskIds must not be empty"); duplicate IDs deduped, response lists the task once, 200.
- Not covered by grader and unverified: concurrency (handler holds the store mutex, so fine).

## Submission's own tests vs README
No contradictions. TestBulkStatus status codes (401/403/404/409/400) all match README rules; TestBulkStatusIsAllOrNothing checks state after rejected batches. Its README edit only adds the endpoint row.

## Grader ambiguity
None triggered.
