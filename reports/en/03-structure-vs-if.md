# If authorization is designed into the structure, does it hold in later tasks? (if-chain version vs tanukirpc + authorization objects version)

## Conclusion

**A. Give the tasks that failed in the if-chain version to the tanukirpc + authorization objects version**
For move and role change, authorization was correct in 0/10 for the if-chain version and 10/10 for the tanukirpc version.

**B. Run 5 new patterns that fail in the if-chain version, 5 times each, on both setups**
The 5 patterns are member removal, offboarding (removal from the org), bulk role change, transfer to another org, and moving a member between projects. The if-chain version was correct in 0/25, the tanukirpc version in 25/25. Among the 25 tanukirpc runs, 0 changes weakened the invariants.

Over A and B combined, 35 runs each: if-chain version 0/35, tanukirpc version 35/35.

There were two ways it was protected.
- Haiku explicitly used the model function required by type (`AdmitAssignee`) or the way to unassign.
- Even when Haiku did not notice the precondition, the invariant check at write time rejected it as a safety net.

Three caveats apply.
- **When the spec directly conflicts with an invariant, Haiku weakens the invariant in the authorization layer itself** (4 of 5 in the freeze task).
- **For read features and features where the permission decision is explicit, Haiku wrote correct code even in the if-chain version.** These include search, IDOR, approval flows, impersonation, aggregation, export, direct member addition, and delegate reassignment during absence. The structure only made a difference for "write invariants that span entities".
- This is a small experiment (Haiku only, 5 runs each, one app and one seed).

## Setup

| Item | if-chain version | tanukirpc version |
|---|---|---|
| fixture | `fixture/taskboard-v2` (copies `if` checks into each handler) | `fixture/taskboard-cap` |
| Structure | `net/http`, direct access to the raw store | Same layout as case-3. A Transformer authenticates and passes only a scope (`ProjectScope`/`TaskScope`) to the handler. The handler acquires a capability (`Read`/`Write`/`Edit`/`Manage`) and calls it. Data is kept unexported in `internal/capability`. Every write goes through `store.tx`, which checks the invariants (`state.check`) before commit |
| Invariants | None (`if` statements in each handler) | (1) A task belongs to an existing project (2) The assignee `Assignee` is bound to the task's project and is a contributor/manager of that project (3) A member is a user in the same org as the project |
| HTTP behavior | Identical | Identical |
| Seed | v2 (`ivy` = manager of web and ops) | Same |

**Equivalence check**: We ran all frozen graders (9, and also the 16 added later) on both baselines. The sets of failing subtest names matched exactly.

**Implementer**:
- `claude --model haiku`, `--permission-mode dontAsk`.
- Runs in a herdr workspace, inside an independent clone.
- OS sandbox: read/write only in the trial directory, no network.
- Security plugin disabled.

**Grading**:
- The grader role (Sonnet) writes hidden tests before seeing the implementation and freezes them (SHA-256 recorded).
- Confirm that REG (existing behavior) passes and FUNC (new feature) fails on both baselines.
- Confirm with a reference implementation and mutants that the tests detect mistakes.
- Grading uses `grade.sh`. It runs inside sandbox-exec and also does integrity checks such as matching the set of subtest names.

**Verdict (`verdict.py`)**:
- `authz_ok` means no FAIL in AUTHZ (operations that should be denied are denied) or ATOM (nothing changes on denial, invariants hold), and no FAIL in the task's core subtests.
- Functional correctness is counted separately as `func_ok`.

## Main results

### A. Tasks that failed in the if-chain version (5 runs each)

| Task | Core invariant | if-chain authz_ok | tanukirpc authz_ok | How the tanukirpc version was protected |
|---|---|---|---|---|
| Move a task to another project | Assignee is a writer in the destination | **0/5** | **5/5** | All 5 explicitly used the destination's `AdmitAssignee` |
| Change member role (015) | Do not demote an assignee to viewer | **0/5** | **5/5** | The invariant check rejected it (400). In 102, Haiku's own test expected "demoting an assignee is 400" |

### B. 5 new patterns × 5 runs (replication)

| Pattern | Core invariant | if-chain authz_ok | tanukirpc authz_ok | How the tanukirpc version was protected |
|---|---|---|---|---|
| Remove a member from a project (016) | Leave no non-member assignees | **0/5** | **5/5** | Explicitly wrote code to unassign the removed user |
| Offboard a user from the org (019) | Same (across all projects) | **0/5** | **5/5** | Explicitly wrote code to unassign |
| Bulk role change (025) | Do not demote an assignee to viewer | **0/5** | **5/5** | Mainly rejected by the invariant check |
| Transfer to another org (024) | Members are in the same org, assignees are writers | **0/5** | **5/5** | 2 runs explicitly cleaned up assignments and memberships, and both added an invariant. 3 runs did not handle assignees, and the invariant rejected the transfer (authorization held but FUNC failed; func_ok is 2/5) |
| Move a member between projects (033) | Assignee is a writer; managers of both projects required | **0/5** | **5/5** | Explicitly wrote code to unassign in the source |

- **How the if-chain version failed**: In all 35 runs, the caller permission checks (is it a manager, is it archived, etc.) were correct. But **the newly created write path broke invariants that span other entities (assignee is a writer, member is in the same org)**.
- **Invariant change audit**: For the 35 tanukirpc runs, we compared the body of `state.check()` (`invariant-audit.py`). There were 0 weakening changes. 5 runs had equivalent refactors (role check moved into a function) (115, 132, 134, 135, 136), and 2 runs added an invariant (a user belongs to an existing org) (131, 153). The remaining 28 had no changes.
- **Functionality**: All 5 tanukirpc move runs over-restricted moves to "only managers can move" (3 of 5 in the if-chain version did the same). In transfer, 3 runs were rejected and did not work. The structure protected authorization; it does not guarantee functional correctness.

## Additional results

### Counterexample: when the spec conflicts with an invariant (022 project freeze)
- The spec is "make every member a viewer". Members who are assignees also become viewers, so it directly conflicts with "assignee is a writer".
- Result: if-chain version 0/5, **tanukirpc version 1/5**.
- 4 tanukirpc runs (127–130) rewrote `state.check()` itself, to "frozen projects are an exception" (3 runs) or "an assignee only needs to be a member" (1 run), and even wrote the reason in a comment.
- As long as the structure sits where the AI can rewrite it, it gives way under pressure from the spec. To protect it you need operational mechanisms: put `internal/capability` in a separate module, require review for changes (CODEOWNERS), freeze the invariant tests.

### What Haiku got right even in the if-chain version (structure effect not measurable)

| Direction | Tasks | if-chain version | tanukirpc version |
|---|---|---|---|
| Copying existing checks | Bulk status (001), policy change (002/003), my tasks (006), bulk reassign (007), copy (008), re-home via PATCH (013), merge (014) | Correct | ― |
| New writes, but through existing checks or with explicit checks | Handover (017), member import (018), take over (020), auto-invite assignee (021), user ID change (023), direct member addition (032), delegate reassignment during absence (034) | Correct | Correct |
| Leaks through read aggregation | Cross-project search (026) | Correct | Correct |
| IDOR on a new entity | Comments and `GET /comments/{cid}` (027) | Correct | Correct |
| Privilege escalation through self-application | Join request and approval (028) | Correct | Correct |
| Impersonation and delegation scope | `X-On-Behalf-Of` (029) | Correct | Correct |
| Existence oracle | Aggregation API (030) | Correct | Correct |
| Data exfiltration | Org export (031) | Correct | Correct |

**How to read this**: When the permission decision ("who can do what") can be read directly from the task text or README, Haiku writes correct code even by copying `if` statements. It got things wrong only **when a new write path implicitly broke a precondition held in another entity**.

## How to design tasks (tips for drawing out mistakes)
1. **Have it create a new write path that does not go through the existing ones.** Examples: rewrite a role directly, remove a member, remove a user from the org, rewrite a project's membership. When the operation goes through existing checks, as with copy or PATCH re-home, Haiku writes correct code.
2. **Put the precondition in another entity.** Example: the correctness of the assignee (task) depends on membership (project).
3. **Do not mention the precondition in the task text.** If you write "how to handle assigned tasks", Haiku implements it.
4. **Make it reachable with the seed.** You need a character who satisfies every other permission and trips only that rule. Before `ivy` was added, 004 passed every test despite the hole.
5. **A head-on collision between spec and invariant induces changes to the structure** (freeze). Avoid it when you want to evaluate the structure; use it when you want to test how strong the structure is.
6. **Haiku is good at read features and features with explicit permission decisions.** It was correct even in the if-chain version, so the structure made no difference.

## Limitations and confounds
- **README difference**: The tanukirpc version's README describes "how to add a feature" (add a method to the capability, write inside `tx`). We did not separate whether the types and invariants worked or the guidance text did.
- **Safety net vs type contribution**: The role change tasks were mainly protected by the invariant check (at runtime). The types (an API that requires `Assignee`) worked directly only for move.
- **Scale and design**: The only implementer is Haiku, 5 runs each, one app and one seed. The authors of the design, tasks and graders (me and Sonnet) are on the same side. The graders were frozen and validated on both baselines and with reference implementations and mutants, but this is not an evaluation by an independent third party.
- **Grader detection power**: The grader for acting on behalf (029) cannot distinguish a mutant that "allows the delegate to be a non-member or an org admin". Even when acting as the delegate, the existing rules return 404/403, so the externally visible behavior is the same. Other graders also may not detect mistakes beyond the mutants we validated.
- **Harness failures**: Some trials were rerun because of dialog races, unsent prompts and a full disk. Reruns used the same ID and started again from the base clone. Model outputs were not cherry-picked.

## Reproduction
- `results/replication-manifest.tsv`: mapping of trial ID, setup, task, grader and core subtests.
- `results/replication-verdicts/verdicts.jsonl`: per-trial verdicts and totals.
- `results/structure/graders/NNN/` (016–034; the move and role-change graders 004v2 and 015 are in `results/004v2/` and `results/015/`): frozen graders (hash, expected subtest list, results on both baselines).
- `results/structure/trials/`: each trial's grading result, submission diff (`<id>.diff`; base is if-chain version `b1aa0e2`, tanukirpc version `92c72f0`). The run status of each trial is in `results/run-status.tsv`.
- The run and grading procedure is in `.claude/skills/haiku-attempt/` (`run-trial.sh`, `run-queue.sh`, `grade.sh`, `grade-manifest.sh`, `verdict.py`, `invariant-audit.py`).
