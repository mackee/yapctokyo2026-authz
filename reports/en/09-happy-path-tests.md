# Follow-up experiment: do an allowed-path table-driven test and an allowed-path spec sentence reduce over-denial? (2026-11-24)

## Motivation

In the original SQLite comparison (441–605), 15 trials over-denied on task move. They returned 403 even for "a contributor moves their own task", so AUTHZ passes but FUNC fails. The spec is upheld, but the feature does not work.

The user's hypothesis: "If the base has allowed-path tests, the AI will follow them and notice over-denial." The request came from the slides session (yapc2026tokyo-65).

## Conditions

We changed three things at once from the original comparison.

1. **We added an allowed-path test to the base.**
   - `fixture/taskboard-{v2,rel}-sql-happy` is the original fixture plus `allowed_test.go`, nothing else (`results/happy-allowed_test.diff`; identical in both versions).
   - `TestAllowed` is a table-driven test of the allowed side only, with 17 rows. The rows are role × operation (read, create, update, delete), and they check only the status code.
   - The header comment is "When you add an operation, add a row for every role that should be allowed to perform it.".
   - We kept the existing tests that mix allowed and denied cases (TestCreate and so on).
   - Where the README invariants are placed is the same as in the original comparison (present in the rel version, absent in the if version).
2. **We stated the allowed path in the task text.**
   - We added one sentence describing only the allowed side to all 8 tasks (`tasks/happy/`; diff in `results/happy-tasks.diff`).
   - The content of the sentence matches who actually performs operations in the frozen graders' FUNC tests.
   - Example (move): 「移動元と移動先の両方で contributor か manager のユーザーは、自分が担当のタスクを移動できます。両方で manager のユーザーは、誰が担当のタスクでも移動できます。」 ("A user who is a contributor or manager in both the source and the destination can move a task they are assigned to. A user who is a manager in both can move a task no matter who is assigned.")
3. **We removed 「テストも追加し、」 ("also add tests, and").** We kept 「go test ./... が通る状態で git commit」 ("git commit with go test ./... passing").

The user reviewed and approved the base test, the task text and the trial count.

We also checked grader equivalence. We ran the 8 graders-200 on both the original base and the -happy base, and the sets of failing items matched in every case. On the -happy base, all 56 of its own tests passed, and integrity was OK.

Trials: 8 tasks × 2 versions × Haiku/Sonnet × 5 runs = 160.

| Version | Haiku | Sonnet |
|---|---|---|
| v2-sql-happy | 646–685 | 686–725 |
| rel-sql-happy | 726–765 | 766–805 |

All 160 driver logs ended with DONE. There were no model mix-ups and no uncommitted changes, and all 160 were valid_run.

Result files:
- Manifests: `results/{v2,rel}-sql-happy-{haiku,sonnet}-manifest.tsv`
- Verdicts: `results/*-happy-*-verdicts/verdicts.jsonl`
- Per-trial diffs and summaries: `results/structure/trials/{646..805}.*`
- Test row counts: `results/happy-testrows.tsv`

## Results

### Total over the 7 tasks excluding freeze (/35). Numbers are "original comparison → this run"

| Version × model | authz_ok | func_ok | Over-denial (authz pass, func fail) |
|---|---|---|---|
| if × Haiku | 1 → 0 | 28 → 32 | 0 → 0 |
| if × Sonnet | 23 → 19 | 28 → **35** | 5 → **0** |
| rel × Haiku | 29 → 27 | 29 → 33 | 6 → 2 |
| rel × Sonnet | 35 → 35 | 30 → **35** | 5 → **0** |

### Move (/5)

| Version × model | authz_ok | func_ok | Over-denial |
|---|---|---|---|
| if × Haiku | 1 → 0 | 3 → 5 | 0 → 0 |
| if × Sonnet | 5 → 2 | 0 → 5 | 5 → 0 |
| rel × Haiku | 5 → 5 | 0 → 5 | 5 → 0 |
| rel × Sonnet | 5 → 5 | 0 → 5 | 5 → 0 |

### Other changes

- rel × Haiku:
  - Transfer: func 4→3, over-denial 1→2. It fails on `FUNC_TransferAnnManagerAllThree`. The cause is how members of archived projects are handled, and the original comparison had the same failure.
  - Role change: authz 4→3. Bulk role change: 2→0. Move member: 4→5.
- if × Sonnet: role change authz 2→0, delete 5→4, bulk role change 0→1, offboarding 1→2.
- Freeze: no change (if 0/0; rel Haiku 0, Sonnet 1).

## What we can read from this

- **Over-denial on move went from 15 trials to 0.** In all 4 cells, func_ok for move became 5/5. Overall, over-denial dropped from 16 trials to 2.
- **But the test did not catch the over-denial.**
  - None of the 160 transcripts records `--- FAIL: TestAllowed` in an intermediate `go test` (tallied from the agent transcripts, which are not published).
  - The agents wrote implementations that did not over-deny from the start. What worked was either the allowed-path spec sentence that says "who is allowed to do what", or the expectation the agent got from reading the table. It was not feedback from a failing test. This design cannot separate the two.
- **The AI added rows to the table. Every added row was an allowed-path row.**

  | Version × model | Trials that added rows | Rows added |
  |---|---|---|
  | if × Haiku | 39/40 | 75 rows |
  | if × Sonnet | 38/40 | 89 rows |
  | rel × Haiku | 40/40 | 86 rows |
  | rel × Sonnet | 30/40 | 75 rows |

  - 0 trials added a denied-side (non-2xx) row to `TestAllowed`.
  - In the move trials, 18 of 20 added a "contributor moves their own task" row.
  - Without being told to write tests, all 160 also wrote separate test functions. Those did check the denied side (400/403/404/409).
- **For invariants (ATOM), the if version's gaps actually became visible.**
  - if × Sonnet on move went authz 5→2. The 3 failures this time (686, 687, 690) and the 5 from if × Haiku (646–650) all fail on `ATOM_IvyAssigneeNotWriterInDest`. That is the case where a manager can move a task whose assignee is not a writer in the destination.
  - In the original comparison, if × Sonnet rejected even moves by a manager, so this gap was hidden (vacuous pass). Once the feature worked, the hole in the invariant surfaced.
  - In the rel version, move ATOM stayed 5/5 for both Haiku and Sonnet, and no gap surfaced. We think this is because the invariant is in the README and the aggregate.
- **There is no clear evidence that adding the test made invariants worse.**
  - The demotion tasks (role change and bulk role change) moved up or down by a few trials, e.g. if × Sonnet 2/10→1/10 (role change 2→0, bulk 0→1) and rel × Haiku 6/10→3/10.
  - With 5 trials per cell, this cannot be told apart from noise. The failure pattern on demotion (not handling the assignee) was the same as in the original comparison.

## Caveats

- We changed three things at once from the original comparison: the base test, the allowed-path spec sentence, and removing "write tests". The difference can only be read as a combined effect.
  - To separate them next, add two conditions: "allowed-path sentence only" and "test only".
- Each cell has only 5 trials.
