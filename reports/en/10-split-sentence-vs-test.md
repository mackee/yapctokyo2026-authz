# Follow-up experiment: separating the effects of the allowed-path spec sentence and the allowed-path test (2026-11-25)

## Motivation

In the experiment in 09-happy-path-tests.md, over-denial on move dropped from 15 trials to 0. But it changed three things at once from the original comparison:

- Added the allowed-path test `TestAllowed` to the base
- Added one sentence describing the allowed side to the task text
- Removed 「テストも追加し、」 ("also add tests, and")

To find out which of these worked, we added two conditions.

## Conditions

| Condition | Base | Task text | Trials |
|---|---|---|---|
| Original comparison | `base-{v2,rel}-sql` | `tasks/` | 441–605 |
| **Sentence only** | `base-{v2,rel}-sql` (no TestAllowed) | `tasks/happy/` (with the sentence, without 「テストも追加し、」) | 806–965 |
| **Test only** | `base-{v2,rel}-sql-happy` (with TestAllowed) | `tasks/notest/` (the original text with only 「テストも追加し、」 removed) | 966–1125 |
| Both (happy) | `base-{v2,rel}-sql-happy` | `tasks/happy/` | 646–805 |

- "Sentence only" and "test only" each differ from "both" in exactly one place.
- Neither condition has 「テストも追加し、」. The effect of removing it is included to the same degree as in "both".
- For all 8 tasks, the only difference between `tasks/notest/` and the original task text is 「テストも追加し、」.

| Version | Sentence only Haiku | Sentence only Sonnet | Test only Haiku | Test only Sonnet |
|---|---|---|---|---|
| v2-sql (if-chain) | 806–845 | 846–885 | 966–1005 | 1006–1045 |
| rel-sql (structured) | 886–925 | 926–965 | 1046–1085 | 1086–1125 |

- 840 (sentence only, v2, Haiku, bulk role change) stopped on an API disconnect and did not commit. We re-ran it as 1126.
- In 922 (sentence only, rel, Haiku, move member), the agent kept refusing to commit to main. We re-ran it as 1127.
- These two are not in the totals. Their run status is in `results/run-status.tsv`.
- All other 320 driver logs ended with DONE, with no model mix-ups. In grading, all 320 were valid_run.
- 1001 left the built binary untracked. The work was committed, so we counted it as valid.

Result files:
- Manifests: `results/{v2,rel}-sql-{sent,testonly}-{haiku,sonnet}-manifest.tsv`
- Verdicts: `results/*-{sent,testonly}-*-verdicts/verdicts.jsonl`
- Per-trial diffs and summaries: `results/structure/trials/{806..1127}.*`
- Rows added to TestAllowed in the test-only condition: `results/testonly-testrows.tsv`
  - Columns: trial, rows added, non-2xx rows, test files changed.

## Results

### Total over the 7 tasks excluding freeze (/35). authz_ok / func_ok / over-denial (authz pass, func fail)

| Version × model | Original comparison | Sentence only | Test only | Both (happy) |
|---|---|---|---|---|
| if × Haiku | 1 / 28 / 0 | 0 / 32 / 0 | 0 / 28 / 0 | 0 / 32 / 0 |
| if × Sonnet | 23 / 28 / 5 | 19 / **35** / **0** | 24 / 29 / 4 | 19 / **35** / **0** |
| rel × Haiku | 29 / 29 / 6 | 28 / 33 / 2 | 30 / 30 / 5 | 27 / 33 / 2 |
| rel × Sonnet | 35 / 30 / 5 | 35 / **35** / **0** | 35 / 30 / 5 | 35 / **35** / **0** |
| Total over-denial | 16 | **2** | 14 | **2** |

### Move (/5). authz_ok / func_ok / over-denial

| Version × model | Original comparison | Sentence only | Test only | Both (happy) |
|---|---|---|---|---|
| if × Haiku | 1 / 3 / 0 | 0 / 5 / 0 | 0 / 2 / 0 | 0 / 5 / 0 |
| if × Sonnet | 5 / 0 / 5 | 0 / 5 / 0 | 4 / 0 / 4 | 2 / 5 / 0 |
| rel × Haiku | 5 / 0 / 5 | 5 / 5 / 0 | 5 / 2 / 3 | 5 / 5 / 0 |
| rel × Sonnet | 5 / 0 / 5 | 5 / 5 / 0 | 5 / 0 / 5 | 5 / 5 / 0 |
| Over-denial | 15 | **0** | 12 | **0** |
| func failures (move) | 17 | **0** | 16 | **0** |

- "func failures" counts over-denial plus the func failures of trials that also failed authz.
- The 3 trials in if × Haiku test only that failed func (967–969) rejected moves by a contributor. They also fail ATOM, so they are not in the over-denial column.

## What we can read from this

- **What worked was the one sentence in the task text.**
  - In all 4 cells, the sentence-only condition reached the same func_ok as "both" (32, 35, 33, 35). Over-denial also dropped to the same number (total 2).
  - The test-only condition was almost the same as the original comparison. Over-denial went from 16 to 14, on move from 15 to 12, and move func failures from 17 to 16.
  - Removing 「テストも追加し、」 was not the main cause either. The test-only condition also removes it, and hardly anything changed.
- **With the test alone, the AI only adds to the table "what its own implementation allows".**
  - In the test-only condition, the AI still added rows to TestAllowed. Trials that added rows: v2 × Haiku 40/40, v2 × Sonnet 36/40, rel × Haiku 39/40, rel × Sonnet 23/40. There were 0 non-2xx rows.
  - But of the 20 move trials, only 4 (966, 970, 1046, 1048) added a "contributor moves their own task" row. In the "both" condition it was 18 of 20.
  - All 4 of these were func_ok, and all other 16 were func failures. The added rows match the implementation. The table became a copy of the implementation and was not used to check the spec.
  - Of the 5 rel × Sonnet move trials, only 1087 added a move row, and that row was a move by a manager.
- **In this experiment too, there was no case where a failing test caught over-denial.**
  - In the 160 transcripts, `--- FAIL: TestAllowed` appeared in an intermediate `go test` in only 1 trial, 1037 (v2 × Sonnet, bulk role change) (tallied from the agent transcripts, which are not published).
  - The failing row was one the agent itself had added. It hit a rule the agent had created: "cannot demote to viewer while holding assignments (409)". The existing 17 rows failed in no trial.
  - 1037 ended up authz_ok.
- **With the spec sentence the feature works, so the holes in the if version's invariants surface.**
  - In the sentence-only condition, all 10 if-version move trials (Haiku 806–810, Sonnet 846–850) failed on `ATOM_IvyAssigneeNotWriterInDest`.
  - In the "both" condition it was 5 for Haiku and 3 for Sonnet. The same reason explains the drop in if × Sonnet authz (23→19).
  - rel-version move was authz 5/5 in every condition.
- **The AI wrote tests even in the sentence-only condition.**
  - Even without 「テストも追加し、」, 159 of 160 trials changed test files. The only one that did not was 907 (rel × Haiku, freeze).

## Other changes (read as noise)

- if × Sonnet, bulk role change authz was 0, 1, 3, 1 in the order original comparison, sentence only, test only, both.
- if × Sonnet, offboarding authz was 1, 2, 0, 2 in the same order.
- rel × Haiku, bulk role change authz was 2, 2, 2, 0 in the same order.
- Freeze was almost 0 in every condition. Only rel × Sonnet was 1, 0, 0, 1 in the same order.

## Caveats

- Each cell has only 5 trials.
- The same side built the fixtures, the tasks and the graders. This confound remains.
- The content of the spec sentence matches who performs operations in the graders' FUNC tests. The sentence is close to writing out the grading answer directly.
  - So it shows that "writing the sentence reduces over-denial".
  - But it does not tell us "how much needs to be written to be enough".
