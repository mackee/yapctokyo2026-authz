# Trial 013 (PATCH projectId re-homes task) analysis

Submission trials/013, commit 00e430b, base b1aa0e2. Grader hash verified by run.sh; integrity_ok=true, 155/155 subtests. Build, vet OK; own tests 55 pass.

## Counts (pass/fail)
| Cat | Baseline | Reference | Mut a (no dest assignee check) | Mut b (source-only) | Mut c (assignee vs source only) | Mut d (partial update) | Trial 013 |
|---|---|---|---|---|---|---|---|
| REG | 40/0 | 40/0 | 40/0 | 40/0 | 40/0 | 40/0 | 40/0 |
| FUNC | 1/16 | 17/0 | 17/0 | 17/0 | 17/0 | 17/0 | 17/0 |
| AUTHZ | 24/15 | 39/0 | 38/1 | 27/12 | 38/1 | 39/0 | 39/0 |
| ATOM | 7/9 | 16/0 | 13/3 | 9/7 | 13/3 | 8/8 | 16/0 |
| VALID | 4/3 | 7/0 | 7/0 | 7/0 | 7/0 | 7/0 | 7/0 |
| CONTRACT | 11/8 | 19/0 | 18/1 | 13/6 | 18/1 | 19/0 | 19/0 |
| INFO | 17 | 17 | 17 | 17 | 17 | 17 | 17 |

Trial 013 matches the reference exactly; no failure, so it resembles no mutant. FUNC passes, so AUTHZ/ATOM are non-vacuous.

## Central question
The resulting assignee IS validated against the destination (main.go, in the new block):

    resultAssignee := task.Assignee
    if req.Assignee != nil { resultAssignee = *req.Assignee }
    if resultAssignee != "" {
        assigneeRole := s.store.Role(target.ID, resultAssignee)
        if assigneeRole != RoleManager && assigneeRole != RoleContributor { 400 }
    }

This covers both the implicit case (ivy t1 ben web->ops: 400) and a combined assignee change. The hypothesised omission did not occur.

Atomic: yes. All validation (title, projectId empty, status, assignee, and every destination check) runs before the mutations; `task.ProjectID = target.ID` is assigned together with title/status/assignee only at the end. Own test TestMoveFailureLeavesTaskUnchanged also covers it.

## Failures
None to classify.

## Diff review / latent defects
- Source rules untouched (existing 404/403/own-task/reassign/409 checks run first), then destination: 404 missing/other-org/unreadable, 403 viewer or org admin, 409 archived, 400 assignee. Same order as createTask.
- Over-strict, safe: the pre-existing `req.Assignee != nil` validation still checks the new assignee against the SOURCE project before the destination check. A manager cannot move a task and assign someone valid only in the destination (INFO: ivy ops t4 -> web assignee ben gives 400). Reverse direction is caught by the destination check.
- Empty projectId rejected with 400 (own decision); same-project projectId allowed as no-op (README says so); null projectId ignored.
- Rules duplicated again in updateTask (a fifth hand-copied check set); future changes need syncing.
- Archived target check message differs but status correct.

## Own tests vs README
55 tests pass; none contradict the README (README states the assignee rule, same-project allowed, 404/403/409 for target). Own tests cover the assignee rule (t1 ben -> 400, assignee dan -> 400), the viewer/non-member/other-org/archived/missing targets, and atomicity with invalid status. Not tested: contributor reassign while moving, move from archived source, org-admin as dest.

## INFO answers
- same-project projectId: 200 no-op (ivy, cat; with title applies title); dan 403.
- empty projectId: 400 ("projectId must not be empty"); null: ignored, other fields applied (200).
- cat own move with assignee:"cat": 403 (only managers reassign).
- ivy ops t4 -> web assignee ben: 400 (source-project check, see above).
- order: hal t1->nope 404 (source first); dan t3->ops 403; ann t5(legacy)->ops 409 (source archived); ann t2 bad status 400 (validation before dest checks); ann t2->legacy 409; cat t1->nope 403 (own-task before dest existence); eve legacy t5->gx 403.
