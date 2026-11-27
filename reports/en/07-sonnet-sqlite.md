# Follow-up: Sonnet × SQLite fixtures (2026-11-23)

Same conditions as the Haiku SQLite runs (441–520), with only the model switched to Sonnet.

- Fixtures:
  - `base-v2-sql` (if-chain): 526–565
  - `base-rel-sql` (production-style relation methods and aggregates): 566–605
  - Both are base repos from before vendoring (126d511).
- The task texts and graders (`NNN-200`) are the same as for Haiku.
- How it was run:
  - `MODEL=sonnet run-queue.sh sonnet-sql.queue 6`
  - `SHARED_GOMODCACHE` and `WARM_GOCACHE` had the same values as for Haiku.
- Result files:
  - Manifests: `results/sonnet-{v2,rel}-sql-manifest.tsv`
  - Verdicts: `results/sonnet-{v2,rel}-sql-verdicts/`
  - Per-trial diffs and grade summaries: `results/structure/trials/{526..605}.*`, `*-trial{526..605}.json`

In all 80 runs the driver log ended with DONE and there were no uncommitted changes. All 80 were valid_run.

## Results (authz_ok / 5)

| Task | Haiku if-sql | **Sonnet if-sql** | Haiku rel-sql | **Sonnet rel-sql** |
|---|---|---|---|---|
| Move 004v2 | 1 | 5 ※1 | 5 | 5 ※1 |
| Role change 015 | 0 | **2** | 4 | 5 |
| Member removal 016 | 0 | 5 | 5 | 5 |
| Offboarding 019 | 0 | 1 ※2 | 4 | 5 |
| Transfer 024 | 0 | 5 | 5 | 5 |
| Bulk role change 025 | 0 | **0** | 2 | **5** |
| Move member 033 | 0 | 5 | 4 | 5 |
| **Subtotal (excluding freeze)** | **1/35** | **23/35** (24/35 by the invariant) | **29/35** | **35/35** |
| Freeze 022 | 0 | 0 | 0 | 1 ※3 |

- ※1 For move, all 10 Sonnet runs had func_ok=false.
  - The cause: a contributor who tries to move their own task gets 403. This is a safe-side denial, the same pattern as the earlier Sonnet if (201–) and Haiku rel-sql (481–).
  - So read the authz pass as a vacuous pass, on a feature that does not work.
- ※2 The 4 offboarding failures:
  - 541 and 545: the assignment remains. Real failures.
  - 544: the assignment remains, and it also deletes the user itself.
  - 542: it unassigned the tasks, but also deleted the user itself, so it failed on a side effect (404→401). This failure comes from how the grader works, the same as in-memory Sonnet.
- ※3 590, which passed freeze, unassigned the project's tasks on freeze (`AssignedTasks`→`unassign`) and kept the invariant.

## What this shows

- **On SQLite, the if-chain version shows the same trend as in-memory.**
  - Sonnet if went from 22/35 to 23/35, almost unchanged.
  - The failures again concentrated on the "lower a role" tasks. Role change was 2/5 and bulk change was 0/5. It does not notice that after a demotion the assignee is no longer a writer.
- **On rel-sql, Sonnet scored 35/35.**
  - On bulk role change, which Haiku missed (2/5, the two hops Project→Memberships→AssignedTasks), Sonnet called `AssignedTasks` and handled assignments in all 5 runs. The 5 role change runs did the same.
  - The guidance on "which related rows to check" was not enough for Haiku but was enough for Sonnet.
- **The effect of structure remains with a bigger model.**
  - With the same Sonnet, if-sql is 23/35 and rel-sql is 35/35, a gap of 12 runs.
  - Limited to the demotion tasks (role change and bulk change), if-sql is 2/10 and rel-sql is 10/10.
