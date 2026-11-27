# yapctokyo2026: how coding agents get authorization wrong

[日本語版はこちら / Japanese](README.ja.md)

Experiment material for a YAPC::Tokyo 2026 talk.

The question: when a coding agent adds a feature to an existing web app, does it get
**resource-level authorization** right? If it gets it wrong, does the way the app is
structured make a difference?

- **App**: `taskboard`, a small Go HTTP API with orgs, projects, memberships
  (manager / contributor / viewer) and tasks with an assignee. It comes in several
  versions that behave the same over HTTP but are built differently: hand-copied `if`
  checks, capabilities with a global invariant check, aggregates, relation methods,
  and SQLite variants.
- **Implementers**: Claude Haiku and Claude Sonnet, run autonomously (`claude --model …
  --permission-mode dontAsk`). Each trial runs in its own clone, inside an OS sandbox
  with no network.
- **Graders**: hidden Go test suites, written and frozen (SHA-256) before any
  implementation was seen. Each suite was checked against both baselines, a reference
  implementation and mutants. A trial is `authz_ok` when no AUTHZ (deny what must be
  denied) or ATOM (nothing changes on denial; invariants hold) subtest fails. It is
  `func_ok` when the feature works. **Over-denial** means `authz_ok` but not `func_ok`:
  the trial is safe but does not work.
- About **1,840 trials** in total. Most cells have n=5.

## Experiments

Each report has an English and a Japanese version with the same content.

| # | Report (en / ja) | Trials | Question |
|---|---|---|---|
| 01 | [Early trials](reports/en/01-early-trials.md) / [ja](reports/ja/01-early-trials.md) | 001–015 | Single Haiku trials on the `if`-chain app: where does it go wrong? |
| 02 | [Trial branches](reports/en/02-trial-branches.md) / [ja](reports/ja/02-trial-branches.md) | 001–015 | How to reproduce each early trial from its `haiku-trials/NNN` branch |
| 03 | [Structure vs. `if`](reports/en/03-structure-vs-if.md) / [ja](reports/ja/03-structure-vs-if.md) | 016–160 (+ 005, 009–012, 015) | `if` chains vs. tanukirpc + capabilities + global invariant check (Haiku) |
| 04 | [Sonnet and guide ablation](reports/en/04-sonnet-and-guide-ablation.md) / [ja](reports/ja/04-sonnet-and-guide-ablation.md) | 201–320 | Does a bigger model close the gap? Does the README's "how to add a feature" guide matter? |
| 05 | [Aggregates and relation methods](reports/en/05-aggregate-and-relation-methods.md) / [ja](reports/ja/05-aggregate-and-relation-methods.md) | 321–400 | Production-like structure without a global check |
| 06 | [Production style and SQLite](reports/en/06-production-style-and-sqlite.md) / [ja](reports/ja/06-production-style-and-sqlite.md) | 401–525 | Authorization objects + `*Table` + SQLite (Haiku) |
| 07 | [Sonnet on SQLite](reports/en/07-sonnet-sqlite.md) / [ja](reports/ja/07-sonnet-sqlite.md) | 526–605 | The same with Sonnet |
| 08 | [README invariant ablation](reports/en/08-readme-invariant-ablation.md) / [ja](reports/ja/08-readme-invariant-ablation.md) | 606–645 | Is it the structure, or the invariants written in the README? |
| 09 | [Allowed-path tests and spec sentences](reports/en/09-happy-path-tests.md) / [ja](reports/ja/09-happy-path-tests.md) | 646–805 | Does an allowed-only table test plus an allowed-side task sentence reduce over-denial? |
| 10 | [Sentence vs. test](reports/en/10-split-sentence-vs-test.md) / [ja](reports/ja/10-split-sentence-vs-test.md) | 806–1127 | Which of the two did it? |
| 11 | [Quint model](reports/en/11-quint-model.md) / [ja](reports/ja/11-quint-model.md) | 1128–1447 | What if the spec and invariants are modelled in Quint, and the agent must update and check the model? |
| 12 | [Invariant tests and TDD](reports/en/12-invariant-tests-and-tdd.md) / [ja](reports/ja/12-invariant-tests-and-tdd.md) | 1448–1927 | What if a test helper checks the invariants? What if the agent is told to work test-first? |
| A1 | [Type-level binding examples](reports/en/A1-tanukirpc-binding-examples.md) / [ja](reports/ja/A1-tanukirpc-binding-examples.md) | — | Ideas for binding these rules with tanukirpc Transformers and types |

## Findings

Counts are `authz_ok` trials unless noted. "Seven tasks" means the cross-entity tasks: move, role change, member removal, offboarding, transfer, bulk role change, move member. Freeze is reported separately.

1. **With hand-copied `if` checks, agents get the caller's checks right but break invariants that span entities.**
   - The agents copy "is the caller a manager? is the project archived?" correctly. On the seven tasks, the `if` versions had no AUTHZ (caller-check) failures at all.
   - What fails is a new write path that breaks a rule held by another entity. Examples: moving a task leaves an assignee who cannot write in the destination; demoting or removing a member leaves them assigned to tasks.
   - On the seven tasks, Haiku passed 0/35 in memory and 1/35 on SQLite.
   - Sonnet passed 22/35 and 23/35. Some of Sonnet's failures kept the invariant and failed on a grader side effect (offboarding deleted the user). Counted by the invariant alone, Sonnet kept it in 27/35 and 24/35 (reports 04, 07).
   - Demotion (role change + bulk role change) stays weak even for Sonnet: 2/10 in memory and on SQLite. Offboarding on SQLite was also 1/5.
   - Single Haiku runs (n=1 each) of read-only features, and of features whose permission decision is explicit, were correct even in the `if` version (reports 01, 03).
2. **Structure helps, through two mechanisms.**
   - With capabilities plus a global invariant check at commit time (`cap`), both models passed 35/35. This is `authz_ok`: functionally, Haiku passed 27/35 and Sonnet 30/35, and Haiku's five move trials all passed by refusing the move (over-denial).
   - Without the global check, Haiku scored 29–30/35 on every structure tried: aggregates, relation methods, and authorization objects + `*Table` (in memory and SQLite). Sonnet was run only on the last of these, on SQLite, and scored 35/35 (func 30/35).
   - In the versions with relation methods, pass or fail lined up exactly with whether the trial loaded the member's assigned tasks (`AssignedTasks`). Haiku used it when it was one hop from the starting record. Two hops away (bulk role change), Haiku mostly did not (1–2/5); Sonnet did (5/5).
3. **For Haiku, most of the gain comes from writing the invariants down.**
   - Removing the invariants from the structured version's README dropped Haiku to 0/10 on demotion.
   - Adding them to the `if` version's README raised it to 7/10.
   - Sonnet benefits from both the structure and the written invariants, and reaches 10/10 only with both.
4. **When the spec collides with an invariant, agents tend to weaken the invariant.**
   - Freezing a project turns every member into a viewer, which collides with "an assignee must be a writer".
   - In the `cap` version, agents rewrote the global invariant check to make an exception: Haiku 4/5, Sonnet 4/5, Haiku without the "how to add a feature" guide 5/5. All of these failed.
   - With a Quint model, 8 of the 40 freeze trials rewrote the Quint invariant, and all 8 failed. Only 2 of the 8 had seen a counterexample; the rest weakened it up front. The other freeze trials mostly kept it: when the update of the model was required (condition B), 16/20 freeze trials were `authz_ok`.
   - With the invariants checked by a test helper (`rules_test.go`), 7 of the 40 freeze trials (all Haiku) added a "frozen projects are exempt" exception to that rule, and all 7 failed. Only 1 of them loosened it after seeing the check fail (report 12).
5. **Over-denial is a separate failure, and tests alone do not fix it.**
   - Agents often make "move a task" manager-only: 15 of the 20 move trials in the SQLite comparison (experiments 06–07).
   - One allowed-side sentence in the task text ("a contributor in both projects can move their own task") brought over-denial from 16 to 2. But in the `if` version it traded one failure for another: all 10 `if` move trials then allowed the move and left an assignee who cannot write in the destination. The sentence is also close to spelling out what the grader checks (report 10).
   - An allowed-only table-driven test in the repo did not help (16 → 14). Agents added rows that mirrored their own implementation, not the spec.
   - Telling the agent to work test-first did not help either (16 → 18). Many trials wrote "only managers can move" as the expected result before implementing, which locked the over-denial in (report 12).
6. **A formal model helps with invariants, but not with what the model itself gets wrong.**
   - Requiring the agent to add the operation to a Quint model and pass `quint run --invariant` (condition B) gave `authz_ok` 35/35 in all four cells. This includes `if` × Haiku, which was at 1/35.
   - Just placing the model in the repo (condition A, update not required) already helped the `if` versions: Haiku 1 → 18/35, Sonnet 23 → 33/35.
   - Few trials ever saw a counterexample (at most 20/160 in B). The gain came from writing the action against invariants stated as formulas.
   - Over-denial was not reduced (16 → 19). The manager-only rule was written into the model itself, and an invariant check cannot see it.
   - A rule left out of the model's invariants (the archived check, trial 1310) passed the check and failed the grader.
7. **Invariant tests help, but what works is that the rules can be read. TDD alone does not help.**
   - We made the shared test helper `do()` check the three invariants in the database after every write. That alone took if-chain × Haiku from 1 to 31/35 and Sonnet from 23 to 35/35; the rel version reached 35/35 with both models. This is about as good as Quint B.
   - But a rule check actually failed during a `go test` run in only 1 of 320 trials. Every trial read `rules_test.go`. The agents read the rules first and implemented to keep them.
   - A test catches only the cases the agent tries. On if-chain × Haiku move, 4/5 still leaked with the helper: no test set up the case of moving a task assigned to ivy.
   - When told to work test-first, all 320 trials committed their tests first. The results were still about the same as the original comparison (if-chain × Haiku 1 → 2/35, Sonnet 23 → 22/35). Tests written first copy what the agent already understands: if-chain × Haiku tested what happens to the assignee in 1 of 30 demotion-type, move and freeze trials (report 12).

## Caveats

- Most cells have n=5. Treat differences of 1–2 trials as noise.
- The same side (the author and Claude) built the fixtures, the tasks and the graders. The graders were frozen and validated, but this is not an independent evaluation.
- The graders can detect only what their tests cover. See the report notes, e.g. on grader 029.
- Everything ran on one macOS machine with a specific harness (herdr, Claude Code). Running new trials elsewhere will need adjustment; see [REPRODUCE.md](REPRODUCE.md).

## Repository layout

| Path | What |
|---|---|
| `reports/{en,ja}/` | The reports above |
| `fixture/` | Every app version used as a trial baseline. `fixture/quint/` holds the Quint model. `taskboard-cap-noguide` and `taskboard-rel-v1` are snapshots of bases that no longer match a fixture |
| `tasks/` | Task texts given verbatim to the agents (Japanese). `tasks/happy/`, `tasks/notest/`, `tasks/quint/` and `tasks/tdd/` are the variants used in experiments 09–12 |
| `results/*-manifest.tsv` | trial → variant, task, grader, key subtest |
| `results/*-verdicts/verdicts.jsonl` | Per-trial verdicts (`authz_ok`, `func_ok`, failing subtests) |
| `results/structure/trials/` | Per-trial submission diff (`<id>.diff`) and grader summary (`<grader>-trial<id>.json`) |
| `results/structure/graders/`, `graders-200/`, `results/NNN/` | Frozen graders (`zz_grader_test.go`, `FROZEN.sha256`, expected subtests) |
| `results/trial-bases.tsv` | trial → fixture → fixture Git tree SHA |
| `results/run-status.tsv` | How each run ended (DONE / NO COMMIT / never started, uncommitted changes, reruns), taken from the driver logs |
| `results/queues/` | Queue files used to run trials 201+ (paths templated) |
| `results/quint-usage.tsv`, `*-testrows.tsv`, `invtest-rulecheck-hits.tsv`, `tdd-first-commit-tests.tsv` | Extra per-trial tallies for experiments 09–12 (Quint usage and rule-check failures are from the agent transcripts; test rows and TDD first commits are from the diffs) |
| `.claude/skills/haiku-attempt/` | The harness: run, grade and audit scripts, plus the pitfalls write-up (`SKILL.md`) |
| `scripts/` | `regrade-trial.sh` rebuilds and regrades one trial. `summarize-verdicts.py` prints the result tables |
| branches `haiku-trials/001`…`015` | Early trials with task, settings and grade summary |

## Reproducing

See [REPRODUCE.md](REPRODUCE.md) ([ja](REPRODUCE.ja.md)). There are three levels:

1. Recompute the tables from the verdicts.
2. Rebuild and regrade any trial from its fixture and diff.
3. Run new trials with the harness.

## What is not published

Raw Claude Code logs are not in this repository: the agent session transcripts and the
driver logs (terminal captures of each run). They hold the operator's environment
(account details, system prompt, local paths) and are not needed to check the results.
What the reports took from them is published as derived tables:

- `results/run-status.tsv`: how each run ended, from the driver logs.
- `results/quint-usage.tsv`: per-trial Quint usage (commands run, counterexamples seen), from the transcripts.
- `results/invtest-rulecheck-hits.tsv`: per-trial count of `go test` runs, and of runs where the invariant check failed, from the transcripts.

The other statements in the reports that were counted from transcripts say so.
