# Follow-up experiment: what happens if the spec and invariants are modeled in Quint? (2026-11-26)

## Motivation

We anticipated the question "Why not just use formal verification?". So we wrote the spec and invariants in Quint, put the model in the repository, and compared two ways of using it.

- A: just put the model there
- B: require the agent to update and check the model

We look at what happens in cases that break invariants (demotion, freeze, etc.), and whether over-denial (move) is eased.

## Conditions

### Model: `fixture/quint/taskboard.qnt` (identical for the if and rel versions)

- State: users, projects, members, tasks. The initial state is the same as the README seed.
- Each of the 4 existing operations (read, create, update, delete) is written as a pair:
  - an outcome function: decides which HTTP status to return
  - an action: changes the state only on 2xx
- The content follows the README access rules and the check order of the if version.
- The invariants are the same 3 as README-rules.
  - `inv_taskInExistingProject`: a task belongs to an existing project
  - `inv_assigneeIsWriter`: an assignee is a contributor or manager of that project
  - `inv_memberInProjectOrg`: a member belongs to that project's org
- We attached 16 `quint test` cases that check the seed examples.
- Sanity checks:
  - `quint typecheck` / `test` / `run --invariant=invariants` pass inside a sandbox with no network and no `$HOME`.
  - A version that models demotion naively produced a violation in 3 of 3 runs.

### Fixtures and task text

- `taskboard-{v2,rel}-sql-quint`:
  - The original fixture plus `spec/taskboard.qnt`, with a "Formal model" section added at the end of the README.
  - The section contains only a description of the model and the check commands. It does not state the invariants in English.
  - The Go code is identical.
  - The rel version's README already has the invariants in English.
- A (just put it there): the task text is unchanged (`tasks/`). The comparison is against the original comparison (441–605).
- B (require update and check): `tasks/quint/`. We added this sentence to the original text:
  > 実装の前に、spec/taskboard.qnt にこの操作の outcome 関数とアクションを追加して step から選ばれるようにし、quint run spec/taskboard.qnt --invariant=invariants で違反が出ないことを確かめてください。実装はモデルに合わせてください。
  >
  > (Before implementing, add an outcome function and an action for this operation to spec/taskboard.qnt so that step can choose it, and confirm that quint run spec/taskboard.qnt --invariant=invariants reports no violation. Make the implementation match the model.)

### Environment and harness

- We installed Quint 0.33.1 at `/Users/Shared/quint`.
  - The node binary and the Rust evaluator are in the same place, with `QUINT_HOME=/Users/Shared/quint/home`.
  - Inside a trial it runs as `/Users/Shared/quint/bin/quint`.
- Running quint is allowed only when `QUINT_BIN` is set for `run-trial.sh`.
  - We also added one line to the environment note saying "quint can be run at this path" (`env-note-quint.md`).
  - This line is in both A and B.

### Trials

8 tasks × 2 versions × A/B × Haiku/Sonnet × 5 runs = 320. The user approved the plan.

| Condition | v2 Haiku | v2 Sonnet | rel Haiku | rel Sonnet |
|---|---|---|---|---|
| A just put it there | 1128–1167 | 1168–1207 | 1208–1247 | 1248–1287 |
| B update and check | 1288–1327 | 1328–1367 | 1368–1407 | 1408–1447 |

- All 320 driver logs ended with DONE, with no model mix-ups. In grading, all 320 were valid_run.
- 7 trials left quint output files (`out.itf.json` etc.) untracked. The work was committed, so we counted them as valid.

Result files:
- Manifests: `results/{v2,rel}-sql-quint{a,b}-{haiku,sonnet}-manifest.tsv`
- Verdicts: `results/*-quint?-*-verdicts/verdicts.jsonl`
- Per-trial diffs and summaries: `results/structure/trials/{1128..1447}.*`
- quint usage counts: `results/quint-usage.tsv`
  - Columns: number of quint commands, number of runs, runs with no violation, runs with a violation, times denied by permissions, whether the model was changed.

## Results

### authz_ok (7 tasks excluding freeze /35; in parentheses, /40 including freeze)

| Version × model | Original comparison | A just put it there | **B update and check** | Reference: allowed-path sentence only |
|---|---|---|---|---|
| if × Haiku | 1 (1) | 18 (18) | **35 (38)** | 0 (0) |
| if × Sonnet | 23 (23) | 33 (37) | **35 (38)** | 19 (19) |
| rel × Haiku | 29 (29) | 31 (32) | **35 (40)** | 28 (28) |
| rel × Sonnet | 35 (36) | 35 (39) | **35 (40)** | 35 (35) |

### Over-denial (authz pass, func fail, excluding freeze) and move

| | Original comparison | A just put it there | B update and check | Reference: allowed-path sentence only |
|---|---|---|---|---|
| Over-denial (total of 4 cells) | 16 | 22 | 19 | **2** |
| Move func failures (/20) | 17 | 18 | 14 | **0** |

### Move (/5). authz / func / over-denial

| Version × model | Original comparison | A | B | Allowed-path sentence only |
|---|---|---|---|---|
| if × Haiku | 1 / 3 / 0 | 4 / 1 / 4 | 5 / 2 / 3 | 0 / 5 / 0 |
| if × Sonnet | 5 / 0 / 5 | 5 / 0 / 5 | 5 / 1 / 4 | 0 / 5 / 0 |
| rel × Haiku | 5 / 0 / 5 | 5 / 1 / 4 | 5 / 3 / 2 | 5 / 5 / 0 |
| rel × Sonnet | 5 / 0 / 5 | 5 / 0 / 5 | 5 / 0 / 5 | 5 / 5 / 0 |

### How quint was used

| | Changed the model | Saw a violation |
|---|---|---|
| A v2 × Haiku | 6/40 | 1 |
| A v2 × Sonnet | 28/40 | 0 |
| A rel × Haiku | 2/40 | 0 |
| A rel × Sonnet | 9/40 | 0 |
| B (all 4 cells) | 40/40 | Haiku 7 + 11, Sonnet 1 + 1 |

- Of the 115 A trials that did not change the model, 68 opened and read the model file.
- "Saw a violation" includes cases where the agent deliberately broke the model to check that the check could detect it (1293 etc.).

## What we can read from this

### 1. Invariant gaps almost disappeared when updating and checking the model was required

- B scored 35/35 excluding freeze in all 4 cells. Even if × Haiku, which was 1/35 in the original comparison, reached 35/35. Including freeze, it was 38–40/40.
- This is larger than the effect of writing the invariants in English (README-RULES; on demotion, Haiku 7/10, Sonnet 5/10).
- But what worked was not counterexample feedback.
  - Of the 160 B trials, only 20 saw a violation. Most trials put a "remove the assignment" or "reject" guard in from the start, at the stage of writing the action in the model.
  - We read it as this: the invariant sat in front of the agent as "a formula that must hold", and having the agent write a new action against it is what worked.
- Even A (just put it there) improved the if version a lot (Haiku 1→18, Sonnet 23→33).
  - Many A trials that did not change the model still passed (if × Haiku: 15/32). Just reading the model works as a statement of the invariants.

### 2. Over-denial did not go down. The over-denial was written in the model

- Over-denial was 16 in the original comparison, 22 in A and 19 in B. It does not drop the way it did in the condition with the allowed-path sentence (2).
- In the 28 trials that wrote a move model (A and B combined), the model's policy and the func result matched in all 28.
  - If the model's `moveOutcome` said "403 unless manager" (in the source; some also required it in the destination), func failed (21 trials).
  - If it said "a contributor may move their own task", func passed (7 trials).
- So the implementation followed the model. The over-denial was decided at the stage of writing the model.
- An invariant check only looks at "states that must not happen". So an over-denying model passes the check untouched.

### 3. The check does not guarantee that the model matches the spec

- **On freeze, some trials loosened the invariant instead. Most did so before seeing any counterexample.**
  - 8 trials rewrote the invariant definition on freeze (A 5, B 3). They did it in two ways:
    - Changed `inv_assigneeIsWriter` to `inv_assigneeIsMember` (an assignee may be a viewer as long as they are a member).
    - Made frozen projects an exception.
  - All 8 failed authz. Assignees remained in frozen projects.
  - Only 2 of the 8 (1151 and 1352) had seen a violation (counterexample) from `quint run` (`results/quint-usage.tsv`). The other 6 rewrote the invariant before seeing one.
  - The final report of 1352 (B, Sonnet) said: "The first model run broke inv_assigneeIsWriter. ... It wasn't requested and it loses data, so I didn't remove assignments on freeze. Instead I loosened the invariant to accept frozen projects."
  - Of the 23 trials that modeled freeze without changing the invariants, 22 were authz_ok.
- **A rule that is not in the invariants is not caught when it is missing from the model.**
  - 1310 (B, Haiku, freeze) dropped the archived check (409) from the model's `freezeOutcome`.
  - The implementation did the same, and failed on `AUTHZ_ArchivedManagerAnn_legacy`. Archiving is not part of the invariants, so `quint run` does not find it.
- In short, we saw almost no drift between model and implementation. The only authz failures in B were 4 on freeze (1311, 1351, 1352, which loosened the invariant, and 1310, which dropped the archived check). Every error was in the model itself (which invariants to uphold, what to reject).

### 4. Even Haiku could write Quint

- All 80 Haiku trials in B added actions to the model, ran `quint run`, and got as far as committing.
- No trial stayed stuck on a type error or syntax error.

## Other notes

- Transfer func_ok dropped slightly in the rel version in both A and B.
  - rel × Haiku: original comparison 4/5 → A 2/5, B 2/5
  - rel × Sonnet: 5/5 → A 4/5, B 4/5
  - if × Haiku went the other way, up from 0/5 → A 1/5, B 4/5.
  - With 5 trials per cell, we have not investigated the cause yet.
- "Denied by permissions" happened 13 times, in 8 trials, in B. These were trials that typed a quint command in a form other than the allowed ones, such as with a pipe. The agents retyped it in an allowed form.

## Caveats

- Each cell has only 5 trials.
- The same side built the model, the fixtures, the tasks and the graders.
  - The model's invariants express, as formulas, the same content as the ATOM checks the graders look at. Like README-rules, this hands over information close to the answer.
- `quint run` is random simulation, not exhaustive checking (we did not use `quint verify`, which uses Apalache).
- Both A and B include the environment note saying "quint is available". We cannot say A is "placed there without saying anything".
