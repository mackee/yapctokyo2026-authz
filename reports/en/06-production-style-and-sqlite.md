# Production-style fixtures and SQLite versions (2026-11-24)

## fixture

- **`taskboard-rel`** (commit a121261, trials 401–440): an in-memory version closer to production code.
  - tanukirpc decodes and validates typed request bodies. 400 is returned before authorization.
  - Task creation returns 200.
  - A Transformer builds authorization objects (`ProjectAuthz` / `TaskAuthz`, `CanWrite` / `CanEdit` / `CanManage` …).
  - DB access is hidden behind `*Table` (`Find` / `List*`, `ErrNoRows`) and row objects.
    - Relation methods take `(ctx, db)`.
    - Row change methods are unexported.
  - API operations take the form `TaskTable.Create(ctx, db, authz, input)`.
  - There is no global check.
- **`taskboard-v2-sql`** (trials 441–480): the if-chain version on SQLite.
  - Handlers send raw SQL to `*sql.DB` and check permissions with copied if statements.
  - Creation returns 200.
- **`taskboard-rel-sql`** (trials 481–520): the `taskboard-rel` structure, with the data layer switched to `database/sql` and raw SQL.
  - `DB` is `*sql.DB` or `*sql.Tx`. Transactions use `inTx`.
  - There is no DB constraint for "the assignee is a writer".
- **Grading**: used `results/structure/graders-200/`. Only two changes from the original graders.
  - The expected status for task creation changed from 201 → 200.
  - The body of the role-probing request was made valid.
- **Equivalence**: for all 3 fixtures, on all 8 graders, the results matched the original graders run on the if-chain version (`results/structure/equivalence-{rel2,v2-sql,rel-sql}/`).
- **Reruns**: 5 runs whose agent did not start were rerun under different numbers (`results/rerun-replacements.txt`). All 120 runs were valid_run.

## Results (Haiku, 5 runs each, authz_ok)

| Task | if (mem) | **if (SQL)** | cap (mem) | agg (mem) | rel old (mem) | **rel (mem)** | **rel (SQL)** |
|---|---|---|---|---|---|---|---|
| Move | 0 | 1 | 5 | 5 | 5 | 5 | 5 |
| Role change | 0 | 0 | 5 | 3 | 4 | 3 | 4 |
| Member removal | 0 | 0 | 5 | 5 | 5 | 5 | 5 |
| Offboarding | 0 | 0 | 5 | 5 | 5 | 5 | 4 |
| Transfer | 0 | 0 | 5 | 5 | 5 | 5 | 5 |
| Bulk role change | 0 | 0 | 5 | 1 | 1 | 2 | 2 |
| Move member | 0 | 0 | 5 | 5 | 5 | 5 | 4 |
| **Subtotal** | **0/35** | **1/35** | **35/35** | **29/35** | **30/35** | **30/35** | **29/35** |
| Freeze | 0 | 0 | 1 | 0 | 0 | 0 | 0 |

- "rel old" is the relation method version from trials 361–400 (`c40d418`).
- "rel (mem)" is the same structure rewritten in production style.

## What this shows

- **SQLite does not change the results.**
  - The if-chain version is 1/35 on SQL too. Only 444 passed, on move.
  - The relation method version is 30/35 on mem and 29/35 on SQL.
  - Every failure was "the assignment remains". There were no infrastructure failures such as 500.
- **Production style does not change the properties of the structure.** Old rel 30/35, new rel 30/35.
- **The pattern was followed completely.** All 22 runs (the demotion runs plus the 2 SQL failures, 499 and 519) wrote the new operation as a `*Table` method (`MembershipTable.ChangeRole` / `SetRole` / `UpdateRoles` / `RemoveFromOrg` / `Move` and so on).
- **The outcome splits exactly on whether `AssignedTasks` (loading assignments) was called.**
  - It matched in all 22 runs. With the 10 old rel runs, it matched in 32.
  - Bulk change was 1–2/5 in every version. A pattern that needs two hops from the starting record (Project) leaks even with structure.
- **Freeze is 0/5 in every version without a global check (only the cap version, which has one, got 1/5).** With no global check, the run does not even need to weaken the invariant. It passes without touching the assignee.
