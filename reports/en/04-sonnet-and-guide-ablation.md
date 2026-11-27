# Follow-up: Sonnet and a README guide ablation (2026-11-23)

We took the 8 tasks where Haiku scored 0/5 on the if-chain version and added three conditions.

1. Sonnet × if-chain version, 5 runs each (201–240)
2. Sonnet × tanukirpc version, 5 runs each (241–280)
3. Haiku × tanukirpc version with the 4 "To add a feature: …" lines removed from the README (`results/cap-noguide-readme.diff`), 5 runs each (281–320)

All run conditions were the same as for Haiku.
- Scripts: `run-trial.sh` / `run-queue.sh`. The model was switched with `MODEL`. Effort was the default.
- The task texts and the frozen graders were also the same.

Result files:
- Manifests: `results/{sonnet-if,sonnet-cap,haiku-cap-noguide}-manifest.tsv`
- Verdicts: `results/*-verdicts/verdicts.jsonl`

All 120 runs were valid_run.

## Results (authz_ok / 5)

| Task | Haiku if | **Sonnet if** | Haiku cap | **Sonnet cap** | **Haiku cap, no guide** |
|---|---|---|---|---|---|
| Move 004v2 | 0 | 5 | 5 | 5 | 5 |
| Role change 015 | 0 | **0** | 5 | 5 | 5 |
| Member removal 016 | 0 | 5 | 5 | 5 | 5 |
| Offboarding 019 | 0 | 0 ※ | 5 | 5 | 5 |
| Transfer 024 | 0 | 5 | 5 | 5 | 5 |
| Bulk role change 025 | 0 | **2** | 5 | 5 | 5 |
| Move member 033 | 0 | 5 | 5 | 5 | 5 |
| **Subtotal (excluding freeze)** | **0/35** | **22/35** (27/35 by the invariant) | **35/35** | **35/35** | **35/35** |
| Freeze 022 (spec conflicts with the invariant) | 0 | 0 | 1 | 1 | 0 |

※ In offboarding, all 5 Sonnet runs unassigned the tasks and kept the invariant. They also deleted the user itself, so the grader failed them for a side effect (404→401). The Haiku if failure was "the assignment remains".

## What this shows

- **A bigger model improves the if-chain version, but the failures do not go away.**
  - For the "remove a member" tasks (removal, offboarding, transfer, move member), Sonnet wrote the assignment cleanup on its own.
  - For the "lower a role" tasks, it did not notice that a demotion makes the assignee no longer a writer. Role change was 0/5 and bulk change was 2/5.
- **The tanukirpc version scored 35/35 regardless of model.** The effect of the structure still added on top with Sonnet.
- **Haiku scored 35/35 even without the README implementation guide.** The "README guide", a candidate confound, is not the main factor.
  - Even without the guide, most of the added code went into `internal/capability` (2–6× main.go by line count).
  - Data cannot be touched from outside the capability package, so the structure itself decides where code goes.
  - The way the invariant was kept matched the original experiment.
    - Removal, offboarding and move member wrote explicit code to unassign.
    - Role change and bulk change were rejected mainly by `state.check`.
- **Invariant tampering audit (`invariant-audit.py`, the 70 non-freeze runs)**
  - 0 changes weakened it.
  - 8 runs added constraints.
    - 6 Sonnet runs (247, 248, 250, 271, 273, 275) added "a project keeps at least one manager".
    - 2 no-guide Haiku runs (306, 307) added "users and projects belong to an existing org".
  - 4 runs (289, 311, 312, 315) made equivalent refactors.
- **Freeze is still the weak spot of the structure.**
  - 4/5 Sonnet cap runs and 5/5 no-guide Haiku cap runs weakened `state.check()`. The rewrite took the form of "exempt while frozen", such as `!p.frozen &&`.
  - Only Sonnet's 265 did not weaken it, and it was authz_ok.
  - Removing the guide raised freeze tampering from 4/5 to 5/5. The sample is small, so this is not a real difference.
- **Functionality**
  - For move, Sonnet if, Sonnet cap and no-guide Haiku cap all restricted it too much to managers and got func_ok 0/5.
  - No-guide Haiku's transfer got func_ok 3/5.
