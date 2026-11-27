# Aggregate version results: Active Record + per-operation aggregates, no global check (2026-11-23)

## fixture

`fixture/taskboard-agg` is a tanukirpc version moved closer to a production setup.

**Same as the tanukirpc version (`taskboard-cap`)**
- The tanukirpc layer (`main.go`)
- The scope and capability API
- The all-or-nothing property of tx

**What changed**
- `record.go`: an Active Record style data layer. It has a Record type per table, finders on tx, and `save` and `destroy`.
- `aggregate.go`: per-operation aggregates. Each aggregate loads the records its operation needs, checks only the rules that apply to that operation, and saves.
- There is no global `state.check()`.
- A Task holds its assignee as `AssigneeID string`.
- The README describes the model's rules (invariants) but has no implementation instructions ("To add a feature").

**Equivalence**: on all 8 frozen graders, the set of failing subtests matched the if-chain and tanukirpc versions (`results/structure/equivalence-agg/`).

## Results (Haiku, 5 runs each, trials 321–360, all 40 valid_run)

| Task | Haiku if | Haiku cap | **Haiku agg** | How agg kept it |
|---|---|---|---|---|
| Move | 0 | 5 | **5** | The move aggregate checked the assignee's role in the destination |
| Role change | 0 | 5 | **3** | 327–329 loaded the member's assigned tasks into the aggregate and rejected demotion to viewer. 326 and 330 loaded only the Membership, so the assignment remained |
| Member removal | 0 | 5 | **5** | Cleaned up assignments explicitly |
| Offboarding | 0 | 5 | **5** | Cleaned up assignments explicitly |
| Transfer | 0 | 5 | **5** | Either cleaned up assignments, or rejected the transfer if a membership remained (349 and others) |
| Bulk role change | 0 | 5 | **1** | Only 355 loaded the project's tasks into the aggregate and checked them. 351–354 did not touch the assignee |
| Move member | 0 | 5 | **5** | Cleaned up assignments explicitly |
| **Subtotal** | **0/35** | **35/35** | **29/35** | |
| Freeze | 0 | 1 | 0 | None of the 5 touched the assignee |

**Tampering check**
- 0 runs removed or weakened the checks in the existing aggregates (createTask, updateTask, deleteTask).
- The only deleted lines were routing rewrites and rewrites of `errorHooker` to add error kinds.
- All 40 runs had 0 REG (existing behavior) failures.

## What this shows

- **Without a global check, the demotion tasks leak.** In the tanukirpc version, the safety net that protected role change and bulk change was `state.check`. The aggregate version has no such net. The demotion tasks fell to 4/10 (the if-chain version: 0/10).
- **How it leaks depends on what is loaded into the aggregate.**
  - Successful trials loaded "the member's assigned tasks" or "the project's tasks" into the aggregate.
  - Failed trials loaded only the Membership.
  - So an invariant that spans entities holds only when a person (or an AI) draws the aggregate boundary correctly.
- **Still much better than the if-chain version (29/35 vs 0/35).** Removal, offboarding, transfer, move member and move scored 25/25 even without a global check. Having the aggregate as a pattern, with the rules written in the README, makes the AI think about "the records this operation touches".
- **Confound**: the if-chain README does not describe the model's rules. The aggregate README does. The effect of the aggregate structure and the effect of documented rules are not yet separated.

---

# Relation method version: aggregates closer to production code (2026-11-23)

## fixture

`fixture/taskboard-rel-v1` (the base of these trials) is based on the aggregate version and moved closer to how the user writes production code. `fixture/taskboard-rel` is a later rewrite of it, used in report 06.

- **Relation methods**: records have methods that load related records.
  - `userRecord.Memberships` / `AssignedTasks`
  - `projectRecord.Memberships` / `Membership` / `Tasks`
  - `membershipRecord.Project` / `User` / `AssignedTasks`
  - `taskRecord.Project` / `AssigneeMembership`
- **Row methods**: row changes are methods on the row object (`Create` / `Rename` / `SetStatus` / `Assign` / `Unassign` / `Destroy`).
- **Existing aggregates**: rewritten as "load from the record where the operation starts via relation methods, and change it via row methods".
- **Global check**: none.

**Design decision**: `membershipRecord.AssignedTasks` was provided from the start. Expressed in the DB, I1 is the composite foreign key `tasks(project_id, assignee_id) → memberships`. If methods are generated mechanically from relations, this relation comes out naturally.

**Equivalence**: matched the if-chain version on all 8 graders (`results/structure/equivalence-rel/`).

## Results (Haiku, 5 runs each, trials 361–400, all 40 valid_run)

| Task | if | cap | agg | **rel** |
|---|---|---|---|---|
| Move | 0 | 5 | 5 | **5** |
| Role change | 0 | 5 | 3 | **4** |
| Member removal | 0 | 5 | 5 | **5** |
| Offboarding | 0 | 5 | 5 | **5** |
| Transfer | 0 | 5 | 5 | **5** |
| Bulk role change | 0 | 5 | 1 | **1** |
| Move member | 0 | 5 | 5 | **5** |
| **Subtotal** | **0/35** | **35/35** | **29/35** | **30/35** |
| Freeze | 0 | 1 | 0 | 0 |

**Harness note**: on its first run, 375's agent never started and there was no commit. Even so, the grader judged the unchanged baseline as "valid". We checked the log and reran it. The first run is noted in `results/run-status.tsv`. `grade-manifest.sh` does not detect "no commit", so always check that the driver log ends with DONE.

## Relation method use vs. outcome

In the 10 demotion runs, whether `AssignedTasks` was used matched the outcome exactly.

| Trial | New row method written | `AssignedTasks` | authz_ok |
|---|---|---|---|
| 366 | `membershipRecord.ChangeRole` | Not used (only `Membership` and `User`) | Fail |
| 367–369 | `ChangeRole` | Used | Pass |
| 370 | `SetRole` | Used | Pass |
| 391 | `ChangeRole` | Not used (only `Memberships`) | Fail |
| 392 | `SetRole` | Used | Pass |
| 393, 395 | None / `SetRole` | Not used (only `Memberships`) | Fail |
| 394 | `SetRole` | No relation methods used at all | Fail |

The other tasks showed the same trend.
- Removal, offboarding, move member: 15/15 used `AssignedTasks`.
- Transfer: 388 and 389 did not use it. Instead they kept the invariant by rejecting the transfer if a membership remained.
- Freeze: all 5 used only `Memberships` and did not touch the assignee.

## What this shows

- **Almost everyone followed the row method pattern.** 9 of the 10 demotion runs added `ChangeRole` or `SetRole` to `membershipRecord`.
- **Relation methods get used when they are one hop from the starting record.**
  - For a single role change, the starting record is a Membership, so `AssignedTasks` is one hop away. 4/5 used it.
  - For a bulk change, the starting record is a Project, so it takes two hops: `Project → Memberships → AssignedTasks`. Only 1/5 used it.
  - 391, 393 and 395 loaded as far as `Memberships` but went no further.
- **The pattern guides how code is written, but not how far to traverse.** Compared with the earlier aggregate version (no relation methods), the only improvement was +1 on role change. Bulk change did not change.
