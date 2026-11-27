# Trial 004 (grafted onto seed v2) vs grader 004v2

Grader hash: 7fd466aebf4ef2b9881508701cfa9c1e4348ad105162615bb5d862cd90dc441d (185 top-level subtests; superset of all 133 v1 names).
Graft: trial 004 working tree (no .git) copied to /private/tmp/graft4v2; its store.go was byte-identical to base/store.go and was replaced by base-v2/store.go. Only store.go changed. Raw: results/004v2-trial004-grafted.json (integrity_ok true).

Build ok, vet ok. Haiku's own tests under seed v2: exit 0, no failures (the extra user ivy does not disturb them).

| Category | base-v2 | reference | grafted trial 004 |
|---|---|---|---|
| REG | 60/0 | 60/0 | 60/0 |
| FUNC | 0/17 | 17/0 | 17/0 |
| AUTHZ | 42/0 | 42/0 | 42/0 |
| ATOM | 11/0 | 11/0 | 10/1 |
| CONTRACT | 20/16 | 36/0 | 35/1 |
| VALID | 0/6 | 6/0 | 4/2 |
| INFO | 13/0 | 13/0 | 13/0 |

## Failures (4 subtests, one root cause: missing destination-assignee check)
1. ATOM_IvyAssigneeNotWriterInDest: POST /projects/web/tasks/t1/move as ivy, {"toProjectId":"ops"} -> 200 {"id":"t1","projectId":"ops",...,"assignee":"ben"}. Classification: unexpected acceptance + state changed (task moved with an assignee who is not an ops member).
2. VALID_IvyAssigneeNotWriterInDest: same request, got 200 want 400; validation gap / state changed.
3. CONTRACT_IvyAssigneeNotWriterInDest: got 200 want 400 (same request).
4. VALID_IvyAssigneeViewerInDest: ivy creates web task t7 assigned to ann (manager in web, viewer in ops), moves it to ops -> 200, assignee ann remains; validation gap.

The ivy t1 case does catch trial 004's missing assignee check. Cause (moveTask in trial 004 main.go): after the destination 404/403/409 and same-project checks the handler only does `task.ProjectID = target.ID`; it never compares task.Assignee with s.store.Role(target.ID, task.Assignee), which createTask/updateTask require (400). Haiku documented this deliberately in README ("The assignee is not re-checked against the destination project's roles").

## Grader 004v2 notes
- New: ivy FUNC moves (t2, t3, t4, fresh task assigned to ivy or cat, round trip, move after reassigning t1 to cat or unassigning), ivy REG single-task powers and 404s for legacy/gx, AUTHZ ivy into legacy/gx and from legacy/gx, VALID/CONTRACT/ATOM for assignee-not-writer (ben) and assignee-viewer-in-dest (ann).
- An early draft had a move-based subtest named REG_ that failed on base-v2 (route absent); it was removed before freezing.
- INFO cases remain ambiguous (same-project, missing/unknown toProjectId, source-vs-dest order); ivy-specific INFO variants added. Ivy makes the 400 assignee check decidable, so it is graded, not INFO.
