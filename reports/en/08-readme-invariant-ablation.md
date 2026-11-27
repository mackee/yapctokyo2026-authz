# Follow-up experiment: separating the effect of writing invariants in the README from the effect of structure (2026-11-24)

## Motivation

An independent review of the slides pointed out a confound in the SQLite comparison (if-sql vs. rel-sql).

- The rel-sql README and comments state the model rules (invariants).
  - "tasks belong to existing projects; an assignee is a contributor or manager of the task's project; project members belong to the project's organization"
- The if-sql README does not.

As it stood, we could not separate "the effect of structure" from "the effect of writing the invariants". So we ran the ablation in both directions and laid the results out as a 2×2.

## Conditions

| | No invariant text | Invariant text |
|---|---|---|
| If-chain version | `base-v2-sql` (existing 441– / 526–) | **`base-v2-sql-rules` (606–625)** |
| Structured (rel) version | **`base-rel-sql-norules` (626–645)** | `base-rel-sql` (existing 481– / 566–) |

- `fixture/taskboard-v2-sql-rules`:
  - At the end of the README's Access rules, we added the three model rules with the same meaning as in rel-sql, plus "reject a write that breaks them with 400 and change nothing".
  - The if-chain version has no relation methods, so we did not include the name `AssignedTasks`.
  - Everything except the README is identical to v2-sql. The diff is `results/v2-sql-rules-readme.diff`.
- `fixture/taskboard-rel-sql-norules`:
  - We removed the model-rule sentence and "validates the model rules on them" from the README.
  - We also removed the list of rules ("Model rules (see README): ...") from the header comment of `internal/model/aggregate.go`.
  - The list of relation methods (`membership.AssignedTasks(ctx, db)` and so on) and the code itself are unchanged. The diff is `results/rel-sql-norules.diff`.
  - We kept the error string "assignee must be a contributor or manager of the project", because the if version has the same one.
  - The user reviewed and decided what to remove.
- Tasks: the two demotion tasks, 015 (role change) and 025 (bulk role change). 5 trials per condition per model.
- Graders: `015-200` / `025-200`. `SHARED_GOMODCACHE` / `WARM_GOCACHE` are the same as in the SQLite runs.
- All 40 new driver logs ended with DONE, and none had uncommitted changes. All 40 were valid_run.
- Result files:
  - Manifests: `results/{v2-sql-rules,rel-sql-norules}-manifest.tsv`
  - Verdicts: `results/*-verdicts/verdicts.jsonl`
  - Per-trial diffs and summaries: `results/structure/trials/{606..645}.*`

## Results (authz_ok, role change /5 + bulk role change /5 = /10)

| Model | if × no rules | if × rules | structured × no rules | structured × rules |
|---|---|---|---|---|
| Haiku | 0 + 0 = **0** | 4 + 3 = **7** | 0 + 0 = **0** | 4 + 2 = **6** |
| Sonnet | 2 + 0 = **2** | 4 + 1 = **5** | 4 + 3 = **7** | 5 + 5 = **10** |

Each cell has only 5 trials. Read differences of 1–2 trials as noise.

## What we can read from this

- **For Haiku, what works is the invariant in the README, not the structure itself.**
  - With the rules removed, even the structured version scored 0/10. With the rules written into the if version, it scored 7/10, about the same as the structured version with rules (6/10).
  - The earlier gap between rel-sql and if-sql (Haiku 6/10 vs. 0/10) was almost entirely the effect of writing the invariants.
- **For Sonnet, both work, and only the combination gives a perfect score.**
  - Structure alone (7/10) and rules alone (5/10) are both better than neither (2/10). With both, it reached 10/10.
  - Sonnet sometimes sees the relation method `AssignedTasks` and realizes on its own that the assignments need cleanup. Haiku did not.
- **Pass/fail matched "did it touch the assignee" exactly.**
  - In the 40 new trials, every passing trial either removed the assignment or rejected the demotion.
    - In the structured version, every pass called `AssignedTasks`.
    - In the if version, passes removed the assignment in SQL or rejected. Haiku 611 rejected with 400 any change that would leave a viewer as assignee.
  - Failing trials have not a single line in production code that handles the assignee (both if × rules and structured × no rules).
- **Some failing trials did notice.** Cases that mentioned it in the final report but did not fix it:
  - if × rules:
    - Sonnet 617: "Demoting does not change the assignee. I haven't decided whether to remove it automatically, so tell me if you need that."
    - Sonnet 623: stated explicitly that "a state that contradicts the existing rule 'an assignee must be a contributor or manager' remains", and left the handling to the user.
  - structured × no rules:
    - Haiku 627: "A demoted member keeps their assigned tasks. They just can't edit them."
    - Haiku 630: reported that a demoted contributor keeps their assignments, as "A consequence to be aware of".
    - Sonnet 641: considered rejecting demotion of an assignee. But it dropped that idea because the example in the task text (demoting ben, who holds t1) would fail. In the final report it listed "a viewer stays as assignee" as an Open issue.
  - The other failures (Haiku 606, 612, 613, 626, 628, 629, 631–635, Sonnet 622, 624, 625, 636, 642) do not mention the assignee at all.
    - Haiku 631–633 used the word "assignee" only in the context "a 400 for a non-member user is handled the same as the assignee check".
  - So some failures were of the "noticed it, but did not fix it because it was not in the spec" type. Even with the invariant written in the README, Sonnet did this in 2 trials (617, 623).

## Caveats

- This covers only the two demotion tasks. We did not do the same decomposition for the other 6 tasks (move, delete, etc.).
- The same side built the fixtures, the tasks and the graders. This confound remains.
