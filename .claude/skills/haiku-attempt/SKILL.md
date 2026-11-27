---
name: haiku-attempt
description: Run a probe trial where Claude Haiku, isolated to one directory (independent clone, herdr workspace, OS sandbox), adds a feature to a naive if-chain Go app (taskboard), and a separate Sonnet grader scores it with a hidden test suite frozen before the implementation. Use it to find examples of an agent getting authorization wrong in hand-copied `if` checks. Covers setup, autonomous driving, grading, and the pitfalls that stalled earlier runs (Go shims, permission dialogs, the security plugin, git leaks).
---

# haiku-attempt: probing Haiku's authorization mistakes in naive code

Goal: find concrete cases where a small model gets resource-level authorization
wrong when it extends or changes a codebase whose authorization is written as
`if` checks copied into every handler. This is separate from the other
experiments prepared for the same talk.

There are three roles:

| Role | Who | What it can see |
| --- | --- | --- |
| Operator | you (the main Claude Code session, running inside herdr) | everything |
| Implementer | Claude **Haiku** in a herdr pane, with `dontAsk` permissions | only its own trial worktree |
| Grader | Claude **Sonnet** sub-agent (Agent tool, `model: sonnet`) | baseline, task text, and later the submission |

## Layout

The skill lives in the repo; the trials run **outside** the repo (see pitfall P1).

```text
yapc2026/haiku-attempt/
  .claude/skills/haiku-attempt/   this skill + scripts/ + references/
  fixture/taskboard/              naive baseline app (source of truth)
  tasks/NNN-*.md                  task texts given verbatim to Haiku
  results/NNN/                    frozen grader, run.sh, summaries, analysis (copied back after a trial)

$PROBE_ROOT   (default ~/src/haiku-probe)
  base/                           isolated git repo made from fixture/taskboard
  trials/NNN/                     independent clone of base (main only), Haiku's only visible dir
$CONTROL      (default ~/src/haiku-probe-control)
  grader/NNN/                     zz_grader_test.go, FROZEN.sha256, run.sh
  results/  settings/  logs/      per-trial outputs and the generated Haiku settings
```

## Procedure

1. **Preconditions.** Run inside a herdr pane: `test "$HERDR_ENV" = 1`.
   `herdr status` should show matching client and server versions (0.9.5 worked).
   You do not need to restart herdr unless a CLI subcommand is missing.
2. **Base repo (once).** `scripts/setup-base.sh` copies `fixture/taskboard`
   into `$PROBE_ROOT/base`, runs `git init`, verifies the tests and commits.
3. **Write the task** as `tasks/NNN-*.md` in plain product language. Do not
   restate the access rules (README/code already contain them), and do not
   hint at grading. Ask for tests and a `git commit` at the end.
4. **Freeze the grader before the trial.** Spawn (or resume with SendMessage)
   a Sonnet agent using `references/grader-prompt.md` (freeze section). It writes
   `$CONTROL/grader/NNN/zz_grader_test.go` and validates it against the baseline
   and a throwaway reference implementation. It then records `FROZEN.sha256` and
   writes a 3-line `run.sh` that execs `scripts/grade.sh <grader-dir> <submission> <out.json>`, plus
   `EXPECTED_SUBTESTS` (the number of top-level subtests in the reference run). It must not look at
   `trials/`.
5. **Run Haiku autonomously** in the background (this can run in parallel with step 4):
   `scripts/run-trial.sh NNN tasks/NNN-*.md` (via Bash `run_in_background: true`).
   The script does the following:
   1. clones base into the trial dir, opens a herdr workspace there, and writes the per-trial settings (permissions + sandbox)
   2. starts `claude --model haiku --permission-mode dontAsk ...`
   3. accepts the folder-trust dialog
   4. sends the task and waits until Haiku settles
   5. records the HEAD SHA

   Exit codes: 0 done, 1 setup/start failure, 3 blocked on a dialog (the screen is
   in the log), 4 no commit.
6. **Grade.** Send the grader the "grade" section of `references/grader-prompt.md`.
   It runs `run.sh <trial-dir> <summary.json>` and classifies every failure.
   Verify the hash and the summary JSON yourself before reporting.
7. **Archive.** Copy `$CONTROL/grader/NNN/*` and `$CONTROL/results/NNN-*` into
   `results/NNN/`. Record the trial commit SHA, the Haiku session id
   (`~/.claude/projects/-Users-...-trials-NNN/*.jsonl`) and the driver log.

## Reading results

- Categories: `REG_` (no regression), `FUNC_` (feature works), `AUTHZ_`
  (denials: non-2xx, no state change, no leak), `ATOM_` (mixed batch is all-or-nothing),
  `VALID_` (input validation), `CONTRACT_` (exact status code), `INFO_` (ambiguous,
  never graded).
- **AUTHZ_/ATOM_ passes are vacuous unless the matching FUNC_ passes.** On the
  baseline the route is absent (405), so every denial "passes".
- Keep a wrong-but-safe status (403 vs 404) in CONTRACT_, separate from security failures.
- One trial is an anecdote, not a statistic. Report it as a concrete example.

## Picking tasks that can expose mistakes

Trial 001 (bulk status update) passed 98/98. Haiku copied `createTask`'s `if`
chain in order and validated everything before writing. **Tasks that can be
solved by copying an existing handler do not reveal much.** Prefer these:

- **Policy changes that must be applied to every copy** (e.g. 002: org admins
  write like managers). A copy is missed, or a shared condition is loosened so
  other roles gain privileges.
- **Features where copying a neighbor is wrong**: cross-project move (destination
  role and archived state), a cross-project "my tasks" view (org admin, archived,
  removed membership), reassignment rules, exceptions such as "archived is
  read-only" or "contributors only on their own tasks".

## Pitfalls (each one stalled or contaminated a run)

**P1. Do not run trials in a worktree that shares objects with anything
sensitive.** A worktree of the main repository would expose other experiments and graders
through `git log --all` / `git show`. Even a worktree of `$PROBE_ROOT/base`
exposes other trials' branches. Each trial is therefore an independent
single-branch clone of base with its origin removed (see P8).

**P2. The Go toolchain: `go` on PATH hangs.** The `go` on PATH is an aqua shim
that delegates to mise. mise reports "go is not active in this directory" and
`aqua exec -- go` hangs forever. This hits your own Bash calls, the grader's,
and Haiku's.
- Always use **`/opt/homebrew/bin/go`** and `/opt/homebrew/bin/gofmt` by absolute
  path, with `GOTOOLCHAIN=local GOPROXY=off`.
- Exporting PATH in the herdr pane is **not enough**. Claude Code's Bash tool
  re-sources the user's shell snapshot, which puts the shims first again. Tell
  Haiku via `--append-system-prompt-file scripts/env-note.md`, and allow only
  the absolute-path commands.
- macOS has no `timeout` command. Use a background process plus `sleep`/`kill`,
  or Bash `timeout` (ms) on the tool call.
- `go mod init` writes the local version (`go 1.27.3`). Pin `go 1.26` in
  `go.mod` so `GOTOOLCHAIN=local` never tries to download a toolchain.
- If a Go command hangs, kill it (TaskStop / `kill`) instead of starting a second copy.

**P3. The security-guidance plugin is on even for Haiku.** It is enabled through
managed settings, so
`--setting-sources project` does not remove it. It runs Opus reviews on Stop and
on commit, and it would feed findings back to Haiku. That measures
"Haiku + Opus reviewer", not Haiku. Disable it per session with
`SECURITY_GUIDANCE_DISABLE=1` and `ENABLE_CODE_SECURITY_REVIEW=0`, both exported
in the pane before `claude` starts and set in the settings `env`. Verify
afterwards: `~/.claude/security/security_warnings_state_<session>.json` and the
transcript should show no reviews and no injected context.

**P4. Use `--permission-mode dontAsk`, not `acceptEdits`.** The user default
mode is `auto`, so always pass a mode explicitly. With `acceptEdits`, Haiku's
compound commands stop the loop on a dialog even when every part is allowlisted.
That happens with `;`, `&&`, pipes, `echo "rc=$?"`, `${PIPESTATUS[0]}` and
heredocs (six stops in trial 001). `dontAsk` silently denies anything not
allowlisted, and the env note tells Haiku to run one simple command per call.
Scope Read/Edit/Write allow rules to `//<trial-dir>/**`, so everything else is
denied, including other trials.

**P4b. Under `dontAsk`, a denial or two makes Haiku give up on Bash entirely.**
In trial 002 Haiku's first commands were `git -C /abs/trial status --short --branch`
and `find /abs/trial -type f ...`. Neither matched `Bash(git status:*)`, so both
were denied. Haiku then never tried `go test` or `git commit`. It finished with
uncommitted, untested edits and told the user to run the commands themselves.
- Allowlist the forms Haiku actually uses: `git -C <trial> status|diff|add|commit|log`
  and `cd <trial>`. The `run-trial.sh` script does this. Without the OS sandbox,
  do **not** allowlist `find` (`find . -exec ...` runs arbitrary commands). With the sandbox (P8) `find .`/`grep`
  are allowed because Glob/Grep tools may be missing.
- The env note must list the permitted commands explicitly. It must also say that
  a single denial does not mean Bash is unavailable, and that running the tests
  and committing are expected.
- After a trial, list the tool calls and denials from the transcript:
  `jq -r 'select(.type=="assistant")|.message.content[]?|select(.type=="tool_use")|"\(.name) \(.input.command // .input.file_path // "")"' <session>.jsonl`.

**P5. herdr control details.**
- The first launch in a new path shows the folder-trust dialog, and `agent start`
  returns `agent_not_ready`. Send `down enter` (the script does this).
- In permission dialogs, **Enter did not select**. Number keys (`1`, `4`, ...) did.
  Extra keys stay in the input box. Clear them with `backspace`; **`esc`
  interrupts the running turn**.
- A `agent prompt --wait` sent right after an interrupt can return
  `agent_prompt_stalled`, and the text may not have been submitted. Check the
  input line with `agent read --source visible` before resending.
- `agent read --lines N` fails while the agent is blocked; use `--source visible`.
- Text like `❯ push` in Haiku's input line can be a prompt suggestion, not typed input.
- Claude Code can show an **"LSP plugin recommendation" (gopls-lsp) dialog**
  when it touches `.go` files. It is not a permission prompt, but it occupies the
  pane. Answer `2` (Not now); `run-trial.sh` dismisses it automatically.
- After `agent wait` returns, re-check `agent get`, because the status can change right after.

**P6. Grader hygiene.**
- Freeze the grader before seeing any submission, and keep the hash.
- Run it on a copy of the submission without `.git`, never in place.
- Prefix grader helpers (e.g. `zg...`) so they don't collide with the submission's
  own test helpers.
- Fresh `newServer()` per subtest, and verify state by re-reading every seeded task over HTTP.
- Reuse the same grader agent across trials (SendMessage) so the design stays consistent.

**P8. `go test` is code execution, so permissions alone don't isolate. Use OS sandboxes on both sides.**
Allowing `go test`/`go build` lets Haiku run any Go code it writes, for example a
test that reads `$CONTROL/grader/` or `~/.claude`. The grader's `run.sh` likewise
executes submission code. Trials 001 and 002 ran without this protection. Their
transcripts show no access outside the trial dir, but nothing technically
prevented it. Since then both sides are sandboxed (macOS `sandbox-exec`;
bubblewrap is Linux-only and absent on this Mac):
- **Haiku side**: Claude Code's built-in Bash sandbox, configured in the per-trial
  settings that `run-trial.sh` writes:
  - `sandbox.enabled`, `failIfUnavailable`, and `allowUnsandboxedCommands: false`
  - filesystem `denyRead: [$HOME]` with `allowRead`/`allowWrite: [trial dir, per-trial Go cache]`
  - `network.allowedDomains: []`
  - `GOCACHE`/`GOTMPDIR` point into the per-trial cache, and `GOENV=off`

  Every Bash child (`go test`, git) inherits it.
- **Grader side**: `scripts/sandboxed-go.sh <W> <go args>` runs go under
  `scripts/grade-sandbox.sb`. That profile denies network and everything under
  `$HOME`, and allows writes only in the per-run work dir. It uses `env -i`, a
  private `HOME`, and `GOCACHE`/`GOTMPDIR` inside the work dir. The `run.sh`
  runners call it for build, vet, own tests and the grader.
- **Trials are independent clones**, not herdr worktrees: `git clone --single-branch -b main`,
  then `remote remove origin`, then `herdr workspace create --cwd <trial>`. `.git`
  then lives inside the trial dir, so the sandbox can stay limited to it. A
  worktree's `.git` lives in the base repo, which would have to be readable and
  would expose other trials' branches.
- **Verified with an escape probe** (`zz_escape_test.go`: read `$CONTROL`,
  `~/.claude`, the main repository; write outside; HTTPS). On both sides all five were
  `operation not permitted` or denied, while `go test` itself passed. Trial 001
  regraded inside the sandbox gave the same 98/98.
- **git inside the sandbox** aborts with `fatal: unable to access '~/.gitconfig': Operation not permitted`.
  This made trial 003's commit fail after tests had passed. Fix: set `GIT_CONFIG_GLOBAL=<per-trial empty file>`
  and `GIT_CONFIG_NOSYSTEM=1` in the settings `env`, set repo user.name/email, and `core.excludesFile=/dev/null`
  (this silences `~/.config/git/ignore` warnings). `run-trial.sh` does all of this.
- **Glob/Grep tools are not available** in these Haiku sessions ("No such tool available: Glob").
  Allow `find .` and `grep` in Bash instead. Under the OS sandbox `find -exec` can't reach outside the
  trial dir, which resolves the earlier review finding about `find`.
- **The trial's `.git` is untrusted.** Haiku could plant `core.fsmonitor`, `include.path` or hooks in
  `.git/config` / `.git/hooks`, and the operator's unsandboxed `git status`/`diff` would then execute them.
  To prevent that, the settings deny Edit/Write on `<trial>/.git/**` and the sandbox has
  `denyWrite: [<trial>/.git/config, <trial>/.git/hooks]`. `git commit` still works (verified, and
  `git config core.fsmonitor ...` fails with "could not lock config file"). Even so, code run by Haiku (e.g. `go test`) can replace `.git` entirely: rename it, write a gitfile,
  or point it at a crafted gitdir whose config has filter/diff/pager drivers. So **never run plain git on a
  trial dir**. Use `scripts/trial-git.sh <trial> <args>`. It runs the real git binary (not the xcrun shim) read-only
  inside `sandbox-exec` (`trial-git.sb`: only the trial readable under `$HOME`, writes only to a scratch dir,
  no network), with `GIT_OPTIONAL_LOCKS=0`, no global/system config, and fsmonitor/hooks/pager/external diff/
  sshCommand neutralized, and **exec deny-by-default** (only the git binary and its `git-core` helpers may run;
  `env -i` is applied outside the sandbox). Verified: a copy with a planted `core.fsmonitor` and a `filter.x.clean` that touch a
  file outside left no trace after `status`/`diff`. Still pass `--no-ext-diff --no-textconv` to diff/log/show. (Setting
  `GIT_EXTERNAL_DIFF=`/`diff.external=` to empty breaks `diff` ("cannot run"); exec deny-by-default covers it instead.)
  (git reports `cannot exec ... Operation not permitted`). `run-trial.sh` (`tgit`) and the grader use it.
  Trials 001-003 were audited: no injected
  config or hooks.
- **Deny rules are also enforced by the sandbox.** A permission deny `Edit(<trial>/.git/**)` made Bash `git commit`
  fail with `Unable to create .git/index.lock: Operation not permitted` (trial 004's commit failed after tests
  passed). Deny only `Edit(<trial>/.git/config)` and `Edit(<trial>/.git/hooks/**)`, mirroring the sandbox
  `denyWrite`. `Write(...)` rules are ignored for file permission checks (Claude Code warns); use `Edit(...)`.
  After changing settings, run a one-shot `claude -p --model haiku` check that commits in a scratch repo before
  spending a trial.
- **`scripts/grade.sh` (shared by every grader's `run.sh`) keeps host and sandbox apart.**
  - Each step has its **own sandbox root**: step 1 (the submission's own build/vet/tests) writes only `sb1`, and
    step 2 (the frozen grader) writes only `sb2`, which is created after step 1. Each has its own GOCACHE/GOTMPDIR,
    so step-1 code can't poison the grader's build cache or copy.
  - Each step runs in its own **process group**, and the group is killed when the step returns, so nothing
    spawned by untrusted code outlives its step.
  - Logs and test JSON go to the host-only `out/`, so a planted symlink can't redirect a host write.
  - The grader copy has the submission's `*_test.go` removed (no `TestMain` hijack). The copied grader's hash
    is re-checked right before `-run '^TestGrader$'`.
  - Integrity: the reported subtest names must equal `EXPECTED_SUBTESTS.list`, recorded from a run on the
    trusted baseline, with exactly one terminal event each (`integrity_ok`, `integrity_problems`).
  - Verified attacks, all blocked:
    - symlinks into `out/`: `operation not permitted`
    - a fake-pass `TestMain`: ignored
    - a lingering step-1 process rewriting `sb2/zz_grader_test.go`: confined to sb1, then killed
    - `init()` printing forged test2json lines: `go test -json` wraps them as plain output
  - Also: the grader sandbox can't read other `/private/tmp/grade.*` runs and may signal only `same-sandbox`;
    after each step `pkill -f "$W/"` sweeps anything still referring to the run. Integrity also requires exactly one
    top-level `TestGrader` terminal event and a grader exit code consistent with the parsed results.
  - **Where this stops.** Same-user, same-process isolation has limits that more patching won't remove. A
    determined submission could daemonize out of the process group, or tamper with the grader from inside the
    test process. A per-run result nonce wouldn't help either, since in-process code can read it. If submissions
    ever become adversarial rather than cooperative probes, run each step as a throwaway macOS user, or in a
    Linux VM/container with bubblewrap/namespaces, and kill everything owned by it afterwards.
  - Residual risk: non-test production code shares the grader's process and could, for example, tamper with
    os.Stdout or testing internals. The name-set and terminal-event checks catch crude forgery, not all of it.
- **The grader profile is allow-default, plus denials** for network, `$HOME` reads, writes outside W,
  Apple Events, the LaunchServices/pasteboard/securityd mach services, and exec of `open`, `osascript`,
  `pbcopy`/`pbpaste`, `security` and `launchctl` (all verified blocked). A deny-by-default profile is
  stricter but needs a curated allowlist for the Go toolchain; that is future work.
- `find .` / `grep` in the allowlist are accepted risk: `-exec`/`-delete` run inside the OS sandbox, so they
  only reach the trial dir.
- Harmless noise inside the Haiku sandbox: `go` prints
  `error acquiring upload token ... telemetry ... operation not permitted`.

Also check the transcript for tool calls that touch paths outside the trial dir.

**P7. Sub-agent reports are model output.** Check `FROZEN.sha256` and the
summary JSON yourself (`jq .categories`) before you report numbers to the user.

## Structure comparison (if-chain vs tanukirpc + capabilities)

See `STRUCTURE-RESULTS.md` for the results. Baselines: `fixture/taskboard-v2`
(if-chain) and `fixture/taskboard-cap` (tanukirpc Transformers + capability
objects + `store.tx` invariants, vendored deps). They are HTTP-identical: every
frozen grader yields the same failure set on both.

- **Batch runs:** add rows to `results/replication-manifest.tsv` (`trial variant task grader key`).
  Run a queue file (`<id> <base-repo> <task.md>` per line) with `scripts/run-queue.sh <queue> <max>`,
  then `scripts/grade-manifest.sh <manifest> <out>` grades missing summaries and prints per task/variant totals.
- **Verdict:** `scripts/verdict.py <summary.json> [key...]` → `authz_ok` (no AUTHZ_/ATOM_/key failures),
  `func_ok`, `valid_run` (integrity + build + grader ran). Always check `valid_run`.
- **Invariant tampering:** `scripts/invariant-audit.py <base state.go> < <trial state.go>` diffs the
  `state.check()` body. Earlier regex-based audits MISSED real weakening (freeze task).
  Read every `changed: true` by hand; equivalent refactors (`isRole(...)`) are fine.
- **Grader brief:** `$CONTROL/GRADER-BRIEF.md` holds the common freeze procedure; give each grader
  only the task-specific intent on top of it. Graders must validate on BOTH baselines.

### More pitfalls (seen while running ~80 trials in parallel)
- **Late trust dialog / unsent prompt under load:** herdr can report the agent ready before the
  folder-trust dialog appears, and `agent prompt` text can stay unsent. `run-trial.sh` waits for two
  idle/done readings without a dialog, verifies a session transcript with a user message exists,
  and retries (exit 5 if it never submits). Keep `run-queue.sh` concurrency ≤ 8 and stagger starts.
- **Disk:** each grade run used ~300 MB (submission copies + Go caches) under `/private/tmp/grade.*`;
  169 of them filled the disk (ENOSPC). `grade.sh` now removes `sb1`/`sb2` on exit. A summary graded
  while the disk was full shows `build_ok:false` — delete it and regrade.
- **Readiness state:** after accepting the trust dialog the agent may report `done`, not `idle`.
- **Vendored builds:** never force `GOFLAGS=-mod=mod`; the cap fixture builds offline from `vendor/`.
