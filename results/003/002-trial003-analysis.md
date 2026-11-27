# Trial 003 vs frozen grader 002 (working tree, uncommitted, base a261dee)

Grader hash verified (cd324309...f2b6). Raw: results/002-trial003.json. Build ok, vet ok, own tests exit 0.

| Category | Baseline | Reference | Trial 003 |
|---|---|---|---|
| REG | 61/0 | 61/0 | 61/0 |
| FUNC | 0/18 | 18/0 | 18/0 |
| AUTHZ | 26/0 | 26/0 | 26/0 |
| ATOM | 7/0 | 7/0 | 7/0 |
| CONTRACT | 22/4 | 26/0 | 26/0 |
| VALID | 0/10 | 10/0 | 10/0 |
| INFO | 3/0 | 3/0 | 3/0 |

FUNC passes, so the AUTHZ/ATOM passes are meaningful. No failing subtests: no unexpected acceptance, state change, leak, partial write, wrong status, or validation gap.

## Review of the diff (main.go)
- New helper `effectiveRole(memberRole, user)`: returns RoleManager for any OrgAdmin, else the membership role. Applied at the `role :=` line in all five handlers (list, get, create, update, delete).
- Reads: list/get only test `role == "" && !user.OrgAdmin`; an admin was already allowed through, so reading is unchanged (REG list/get cases pass, gx still 404 for eve).
- Other roles: ann, ben, cat, dan, hal, gus are not OrgAdmin, so they get their membership role unchanged. No shared condition was loosened (REG viewer/non-member/contributor/gus denials all pass).
- Ordering: every inline check order is untouched. Project/org 404 still precedes role, so eve on gx is 404; archived 409 still follows the role check, so eve on legacy gets 409 (and 409 before body validation, INFO confirms).
- Latent semantic difference: an admin who also holds an explicit lower project role (viewer/contributor) would be upgraded to manager. No seed user is both, so ungraded; arguably matches "same as manager".
- Now-dead conditions: `role == "" && !user.OrgAdmin` is redundant after the helper (harmless).
- Assignee rules for eve unchanged: she cannot be an assignee (400), same as the reference.

## Own tests vs README
Haiku changed the "org admin 403" expectations to 201/200 and added gx 404, archived 409, and non-member project cases, matching the task. It also updated the README access rules and comments (README now states org admins act as managers, with 404 other-org and 409 archived). The old README line "writes require contributor or manager role in the project" is now slightly inconsistent with the new line, but no test contradicts the README rules; the intentional org-admin change is documented. Deleted-tests: none removed.
