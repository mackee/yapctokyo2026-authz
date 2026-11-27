# Reproducing the experiments

[日本語版 / Japanese](REPRODUCE.ja.md)

There are three levels. Each one needs more setup than the one before.

| Level | What you get | Needs |
|---|---|---|
| 1. Recompute the tables | The numbers in the reports, from the archived verdicts | Python 3 |
| 2. Regrade archived trials | Each trial rebuilt from its fixture and diff, then graded with the frozen grader | macOS (`sandbox-exec`), Go, Python 3 |
| 3. Run new trials | New agent runs with the same harness | Level 2, plus Claude Code, herdr, and (for experiment 11) Quint |

The original runs used macOS on Apple silicon, Go 1.27.4 (`/opt/homebrew/bin/go`; the
fixtures declare `go 1.26`), Claude Code 2.1.332, herdr 0.9.5 and Quint 0.33.1.

The harness passes model aliases (`--model haiku` / `--model sonnet`), and an alias may
point to a newer model later. In the original runs (2026-11-21 to 2026-11-26) they
resolved to `claude-haiku-5-5` and `claude-sonnet-5-5`. To run the same models, set
`MODEL` to the full ID, e.g. `MODEL=claude-haiku-5-5`.

## 1. Recompute the tables

`scripts/summarize-verdicts.py` prints authz_ok / func_ok / over-denial per task for
any set of verdict files. Pick the verdict sets from `results/*-manifest.tsv` and the
reports. The TOTAL row sums the seven core tasks (it leaves out freeze, and the one-off
tasks that `replication-verdicts` also holds).

```sh
# Experiment 11, if-chain x Haiku: original comparison vs. Quint A vs. Quint B
scripts/summarize-verdicts.py orig=results/haiku-v2-sql-verdicts \
  A=results/v2-sql-quinta-haiku-verdicts B=results/v2-sql-quintb-haiku-verdicts

# Experiment 03: replication-verdicts holds both variants; pick one with :VARIANT
scripts/summarize-verdicts.py if=results/replication-verdicts:if cap=results/replication-verdicts:cap
```

Each `verdicts.jsonl` line also lists the failing AUTHZ/ATOM/CONTRACT/VALID subtests. The
raw grader output for each trial is `results/structure/trials/<grader>-trial<id>.json`.
`verdict.py` (in `.claude/skills/haiku-attempt/scripts/`) turns it into a verdict.

## 2. Regrade archived trials

The submissions are stored as diffs against the base, which is a fixture committed as is.
`results/trial-bases.tsv` maps each trial to its fixture directory and the fixture's Git tree SHA in this repository.
`scripts/regrade-trial.sh` does the whole job for one trial:

1. Copy the fixture.
2. Apply `results/structure/trials/<id>.diff`.
3. Run the frozen grader inside `sandbox-exec`.
4. Print the verdict.

**One-time setup: a Go module cache outside `$HOME`.** The grader runs Go with
`GOPROXY=off` inside a sandbox that cannot read `$HOME`. The tanukirpc fixtures
(`taskboard-cap*`, `-agg`, `-rel`, `-rel-v1`) are vendored. The SQLite fixtures
(`taskboard-*-sql*`) need their modules in a shared cache:

```sh
export SHARED_GOMODCACHE=/Users/Shared/yapc-gomodcache
for f in fixture/taskboard-v2-sql fixture/taskboard-rel-sql; do
  (cd $f && GOMODCACHE=$SHARED_GOMODCACHE go mod download)
done
```

Optionally, set `WARM_GOCACHE` to a Go build cache directory. It is cloned into each
grading sandbox and speeds builds up. If Go is not at `/opt/homebrew/bin`, set
`GO_BIN_DIR`. Go must be installed outside `$HOME` (the sandbox cannot read `$HOME`, so a
Go from mise/asdf/goenv under `$HOME` will not run).

The graders' `run.sh` finds the repository with `git rev-parse`. If you downloaded a ZIP
(no `.git`), set `YAPC_REPO` to the repository root. `regrade-trial.sh` sets it for you.

**Regrade:**

```sh
scripts/regrade-trial.sh 1352            # grader and key subtest are read from the manifests
scripts/regrade-trial.sh 106 019         # or name the grader explicitly
```

Compare the output with the archived verdict
(`python3 .claude/skills/haiku-attempt/scripts/verdict.py results/structure/trials/<grader>-trial<id>.json`).
While preparing this repository, trials 106, 307, 366, 806 and 1352 were rebuilt this
way. They gave the same verdicts and failing subtests as the original runs. Between them
they cover the cap, cap-noguide, rel-v1, v2-sql and v2-sql-quint bases.

**Early trials 001–015.** Only 005, 009–012 and 015 have a diff in
`results/structure/trials/` (so `regrade-trial.sh` works for them). Every early trial is
also a branch `haiku-trials/NNN`. The first commit is the baseline. The second commit holds the
submission plus `.trial/` (task, settings, metadata, grade summary). See
[reports/en/02-trial-branches.md](reports/en/02-trial-branches.md). Their graders are in
`results/NNN/`.

**Graders.** Every grader directory holds `zz_grader_test.go` (hashed in
`FROZEN.sha256`), the expected subtest list and `run.sh`. `run.sh` finds `grade.sh`
from the repository root. If you copy a grader out of the repository, or have no `.git`,
set `YAPC_REPO`.

| Grader name in manifests | Directory |
|---|---|
| `NNN` for 001–015, `004v2` | `results/NNN/` |
| `NNN` for 016–034 | `results/structure/graders/NNN/` |
| `NNN-200` (SQLite and production-style fixtures) | `results/structure/graders-200/NNN/` |

`results/structure/equivalence-*/` records the check that each fixture fails the same
subtests as the `if`-chain baseline under every grader. In other words, the fixtures
behave the same over HTTP.

## 3. Run new trials

The harness is the Claude Code skill in `.claude/skills/haiku-attempt/`. Read `SKILL.md`
first. Its "Pitfalls" section lists every problem that stalled or contaminated a run.
Examples: Go shims that hang, permission dialogs, the security plugin, and git config
escapes from the sandbox. The layout section of `SKILL.md` describes the original
monorepo path `yapc2026/haiku-attempt/`, which is this repository's root.

**Requirements:** macOS with `sandbox-exec`, zsh, Go, Python 3, jq, git, the `claude` CLI
(logged in) and herdr (the operator session runs inside herdr and drives one agent pane
per trial). `trial-git.sh` runs git from `/Library/Developer/CommandLineTools` by default;
set `TRIAL_GIT_BIN` to use another real git binary (not the `/usr/bin/git` shim).

**Directories** (defaults; override with env):
- `PROBE_ROOT=~/src/haiku-probe`: base repos and trial clones. Keep it outside this
  repository, so that the agent cannot see graders or other trials.
- `CONTROL=~/src/haiku-probe-control`: graders, per-trial settings, logs, grade summaries.

**Steps:**

1. Create the base repos from the fixtures (one commit, "taskboard baseline"):
   ```sh
   S=.claude/skills/haiku-attempt/scripts
   FIXTURE=$PWD/fixture/taskboard-v2-sql BASE_REPO=$HOME/src/haiku-probe/base-v2-sql \
     GOMODCACHE=$SHARED_GOMODCACHE $S/setup-base.sh
   ```
   The queue files use these base names:

   | Base | Fixture |
   |---|---|
   | `base` | `taskboard` |
   | `base-v2` | `taskboard-v2` |
   | `base-cap` | `taskboard-cap` |
   | `base-cap-noguide` | `taskboard-cap-noguide` |
   | `base-agg` | `taskboard-agg` |
   | `base-rel` | `taskboard-rel-v1` |
   | `base-rel2` | `taskboard-rel` |
   | `base-<name>` (all SQLite variants) | `taskboard-<name>` |

2. Install the graders where `grade-manifest.sh` looks for them:
   ```sh
   mkdir -p $HOME/src/haiku-probe-control/grader
   for d in results/structure/graders-200/*; do cp -R $d $HOME/src/haiku-probe-control/grader/$(basename $d)-200; done
   for d in results/structure/graders/* results/0* ; do [ -f $d/zz_grader_test.go ] && cp -R $d $HOME/src/haiku-probe-control/grader/$(basename $d); done
   export YAPC_REPO=$PWD
   ```

3. Render a queue file and run it. Queue lines are `<trial-id> <base-repo> <task-file>`.
   ```sh
   sed -e "s|@PROBE_ROOT@|$HOME/src/haiku-probe|" -e "s|@REPO@|$PWD|" \
     results/queues/haiku-quintb.queue > /tmp/my.queue
   # Change the trial IDs first if those directories already exist under $PROBE_ROOT/trials.
   export SHARED_GOMODCACHE WARM_GOCACHE=...       # SQLite fixtures
   export QUINT_BIN=/Users/Shared/quint/bin/quint  # experiment 11 only
   MODEL=haiku $S/run-queue.sh /tmp/my.queue 7
   ```
   Queues exist for trials 201 and later. For 005–160, build the queue from
   `results/replication-manifest.tsv` and `results/trial-bases.tsv`.

4. Check every driver log (`$CONTROL/logs/<id>.log`). It must end in `DONE` and report `dirty=0`.
   The archived runs' status is in `results/run-status.tsv`; the raw logs are not published. A trial whose
   agent never started can still grade as "valid" against the untouched baseline (see
   report 05, trial 375). Then grade:
   ```sh
   $S/grade-manifest.sh my-manifest.tsv /tmp/verdicts
   ```
   The manifest lists your own trial IDs. Copy the columns from the archived one
   (e.g. `results/v2-sql-quintb-haiku-manifest.tsv`) and replace the IDs if you renumbered.

**Quint (experiment 11).** The trial sandbox cannot read `$HOME` and has no network, so
Quint must live outside `$HOME`, with its Rust evaluator pre-installed:

```sh
Q=/Users/Shared/quint; mkdir -p $Q/bin $Q/home
cp "$(which node)" $Q/bin/node
npm install --prefix $Q @informalsystems/quint@0.33.1
cat > $Q/bin/quint <<'EOF'
#!/bin/sh
export QUINT_HOME=/Users/Shared/quint/home
exec /Users/Shared/quint/bin/node /Users/Shared/quint/node_modules/@informalsystems/quint/dist/src/cli.js "$@"
EOF
chmod +x $Q/bin/quint
# The first `quint run` downloads the Rust evaluator into QUINT_HOME; do it once outside the sandbox:
$Q/bin/quint run fixture/quint/taskboard.qnt --invariant=invariants --max-steps=5 --max-samples=10
```

When `QUINT_BIN` is set, `run-trial.sh` allows that one command and adds
`.claude/skills/haiku-attempt/scripts/env-note-quint.md` to the agent's system prompt.

**What will differ.** Model versions change over time, and agent runs are not
deterministic. New trials reproduce the setup, not the exact outputs. The archived diffs
and verdicts are the record of what happened.
