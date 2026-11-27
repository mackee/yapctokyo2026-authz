# Trial 005 (seed v2, commit a4759f2) vs grader 004v2

Grader hash verified (7fd466ae...441d), integrity_ok true. Raw: results/004v2-trial005.json. Build ok, vet ok, own tests exit 0 (no failures).

| Category | Grafted trial 004 | Trial 005 |
|---|---|---|
| REG | 60/0 | 60/0 |
| FUNC | 17/0 | 9/8 |
| AUTHZ | 42/0 | 42/0 |
| ATOM | 10/1 | 10/1 |
| CONTRACT | 35/1 | 29/7 |
| VALID | 4/2 | 0/6 |
| INFO | 13/0 | 13/0 |

## Central question: destination-assignee rule
Not applied, even with ivy in the seed. moveTask ends:
    targetRole := s.store.Role(target.ID, user.ID) ... 404 / 403 / 409 checks
    task.ProjectID = target.ID
    writeJSON(w, 200, task)
There is no `s.store.Role(target.ID, task.Assignee)` check (createTask/updateTask require contributor/manager, else 400). Failures: ATOM_IvyAssigneeNotWriterInDest, VALID_IvyAssigneeNotWriterInDest, CONTRACT_IvyAssigneeNotWriterInDest (ivy moves t1/ben web->ops -> 200, assignee ben stays in ops), VALID_IvyAssigneeViewerInDest (ann, viewer in ops, stays assignee). Classification: unexpected acceptance / validation gap / state changed (task carries an invalid assignee).
Did Haiku notice? No. The README addition does not mention the assignee. Worse, its own TestMoveResponseAndListings asserts the defect: ivy moves t1 web->ops and expects `Task{ID:"t1", ProjectID:"ops", ..., Assignee:"ben"}` with 200, although ben is not an ops member. It used ivy for the manager-of-both case but never questioned the assignee.

## New deviation: contributors cannot move at all
README line added: "Moving a task requires the `manager` role in its current project." Code:
    if role != RoleManager { writeError(w, 403, "only managers may move tasks") ...}
replacing the source rule of single PATCH (contributor may modify own tasks). Haiku chose a stricter policy than the existing edit rules. Effects (all failures except the assignee ones stem from this):
- FUNC (8 fail): CatWebToOps, CatOpsToWeb, StatusCarried, TitleStatusCarried, OpsTaskStatusCarried, MovedTaskUsableInDest, RoundTrip, BothDirectionsIndependent: cat (contributor, own task) -> 403 instead of 200. Classification: functional bug (over-restriction, safe).
- VALID_MalformedJSON / NotJSON / ToProjectIdWrongType / ToProjectIdObject: graded as cat, got 403 instead of 400. These are consequences of the contributor denial, not a validation gap: ivy's INFO shows 400 for missing toProjectId and for same-project. Classification: wrong-but-safe status / consequence.
- CONTRACT ContribDestNotReadable (403 vs 404), IntoArchivedByContributorOwn (403 vs 409), IntoArchivedByNonMember (403 vs 404), IntoOtherOrgByContributor, IntoOtherOrgFromOps (403 vs 404), SourceArchivedContributor (ben, legacy t5 -> web: 403 vs 409): wrong-but-safe status, same cause. AUTHZ and the other ATOM cases pass (non-2xx, no state change, no leak).
Is it a grader ambiguity? The task text and README define edit rights per role; the frozen intent says contributors may move own tasks. Treat as a functional deviation, but note the manager-only reading is a defensible interpretation that the task text does not exclude; it is graded as a bug because the implementer was meant to reuse the existing rule.

## Check order and INFO differences (vs 004)
- Same-project check precedes target lookup/role checks (ivy web->web: 400; 004 also 400 but after dest checks). For cat: 403 from source rule.
- targetRole == "" returns 404 without an OrgAdmin exemption (fine, eve cannot be a source writer).
- Missing toProjectId: ivy 400 (004: 400). Unknown target: ivy 404. For cat-based INFO cases (empty/missing/unknown/same, order cases) the answer is 403 from the manager-only source rule, so those INFO lines are not comparable to 004.
- Source archived vs viewer dest: 409 (source first), same as 004.

## Latent defects
1. No destination-assignee check (above).
2. Manager-only source rule (documented in README, so deliberate).
3. Own test encodes defect 1 as expected behaviour; own tests otherwise match README, including the new manager-only rule.
