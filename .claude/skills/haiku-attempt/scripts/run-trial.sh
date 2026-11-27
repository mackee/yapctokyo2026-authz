#!/bin/zsh
# Drive one Haiku trial autonomously through herdr.
# usage: run-trial.sh <id> <task-file>
# env:   PROBE_ROOT (~/src/haiku-probe), CONTROL (~/src/haiku-probe-control),
#        GO_BIN_DIR (/opt/homebrew/bin), MODEL (haiku), PROMPT_TIMEOUT_MS (1500000),
#        BASE_REPO (default $PROBE_ROOT/base; e.g. base-v2 for seed v2), KEEP_WORKSPACE,
#        SHARED_GOMODCACHE (read-only module cache outside $HOME, for fixtures with module deps
#        but no vendor/), WARM_GOCACHE (build cache copied into the trial with APFS clonefile),
#        QUINT_BIN (absolute path of a quint wrapper outside $HOME; allows `quint` and adds
#        env-note-quint.md to the appended system prompt)
# exit:  0 done, 1 setup/start failure, 3 blocked on a dialog, 4 no commit, 5 prompt never submitted
set -u
ID=$1; TASK=${2:A}
SKILL_DIR=${0:A:h:h}
PROBE_ROOT=${PROBE_ROOT:-$HOME/src/haiku-probe}
CONTROL=${CONTROL:-$HOME/src/haiku-probe-control}
GO_BIN_DIR=${GO_BIN_DIR:-/opt/homebrew/bin}
MODEL=${MODEL:-haiku}
BASE=${BASE_REPO:-$PROBE_ROOT/base}
T=$PROBE_ROOT/trials/$ID
NAME=haiku$ID
mkdir -p $CONTROL/settings $CONTROL/logs
LOG=$CONTROL/logs/$ID.log

log(){ print -r -- "[$(date +%H:%M:%S)] $*" | tee -a $LOG; }
screen(){ herdr agent read $NAME --source visible 2>&1 | grep -v '^\s*$' | tail -${1:-25} >> $LOG; }
# $T/.git is untrusted after the trial: never let its config/hooks run code as the operator.
# (it may even replace .git), so operator-side git runs read-only inside sandbox-exec.
tgit(){ $SKILL_DIR/scripts/trial-git.sh $T "$@"; }
st(){ herdr agent get $NAME 2>/dev/null | jq -r '.result.agent.agent_status // "none"'; }

[[ ${HERDR_ENV:-} == 1 ]] || { print -u2 "not inside herdr"; exit 1; }
[[ -d $BASE/.git ]] || { print -u2 "missing $BASE (run setup-base.sh)"; exit 1; }
[[ -e $T ]] && { print -u2 "$T already exists"; exit 1; }

# Independent clone (not a worktree): .git lives inside $T and holds only main, so
# neither git nor code run by Haiku can reach other trials' objects via the base repo.
log "clone base into $T"
git clone -q --no-hardlinks --single-branch -b main $BASE $T || { log "clone failed"; exit 1; }
git -C $T remote remove origin
git -C $T config user.name "haiku-trial"; git -C $T config user.email "haiku-trial@example.invalid"
# ~/.gitconfig and ~/.config/git are unreadable inside the sandbox; git aborts on the former.
git -C $T config core.excludesFile /dev/null
K=$PROBE_ROOT/cache/$ID; mkdir -p $K/gocache $K/tmp; : > $K/gitconfig
[[ -n ${WARM_GOCACHE:-} ]] && cp -c -R $WARM_GOCACHE/. $K/gocache/
# The notes name /opt/homebrew/bin and /Users/Shared/quint/bin/quint; rewrite them to the
# configured paths so the agent is told the same commands the permission rules allow.
EXTRA_ALLOW=""; NOTE=$CONTROL/settings/$ID.note.md; NOTES=($SKILL_DIR/scripts/env-note.md)
if [[ -n ${QUINT_BIN:-} ]]; then
  EXTRA_ALLOW=", \"Bash($QUINT_BIN:*)\""
  NOTES+=($SKILL_DIR/scripts/env-note-quint.md)
fi
cat $NOTES | sed -e "s|/opt/homebrew/bin|$GO_BIN_DIR|g" -e "s|/Users/Shared/quint/bin/quint|${QUINT_BIN:-/Users/Shared/quint/bin/quint}|g" > $NOTE
EXTRA_ENV=""; [[ -n ${SHARED_GOMODCACHE:-} ]] && EXTRA_ENV=", \"GOMODCACHE\": \"$SHARED_GOMODCACHE\""
WS_JSON=$(herdr workspace create --cwd $T --label haiku-$ID --no-focus)
PANE=$(print -r -- $WS_JSON | jq -r '.result.root_pane.pane_id')
WS=$(print -r -- $WS_JSON | jq -r '.result.workspace.workspace_id')
# Close this trial's herdr workspace (and its Haiku agent) on exit unless KEEP_WORKSPACE=1.
[[ ${KEEP_WORKSPACE:-0} == 1 ]] || trap '[[ -n ${WS:-} && $WS != null ]] && herdr workspace close $WS >/dev/null 2>&1' EXIT
[[ -z $PANE || $PANE == null ]] && { log "workspace create failed"; exit 1; }
log "pane $PANE"

# Per-trial settings.
# - permissions: Read/Edit/Write only inside $T; everything else is silently denied (dontAsk).
# - sandbox: every Bash command (incl. go test running Haiku's code) runs under the
#   built-in sandbox (sandbox-exec on macOS): nothing under $HOME readable except $T
#   and the per-trial Go cache, writes only there, no network.
cat > $CONTROL/settings/$ID.json <<JSON
{
  "permissions": {
    "allow": [
      "Read(/$T/**)", "Edit(/$T/**)",
      "Bash($GO_BIN_DIR/go test:*)", "Bash($GO_BIN_DIR/go build:*)", "Bash($GO_BIN_DIR/go vet:*)",
      "Bash($GO_BIN_DIR/go version)", "Bash($GO_BIN_DIR/gofmt:*)",
      "Bash(git status:*)", "Bash(git diff:*)", "Bash(git add:*)", "Bash(git commit:*)", "Bash(git log:*)",
      "Bash(git -C $T status:*)", "Bash(git -C $T diff:*)", "Bash(git -C $T add:*)", "Bash(git -C $T commit:*)", "Bash(git -C $T log:*)",
      "Bash(cd $T)", "Bash(ls:*)", "Bash(pwd)", "Bash(find .:*)", "Bash(grep:*)"$EXTRA_ALLOW
    ],
    "deny": ["WebFetch", "WebSearch", "Agent", "Edit(/$T/.git/config)", "Edit(/$T/.git/hooks/**)"]
  },
  "sandbox": {
    "enabled": true,
    "failIfUnavailable": true,
    "allowUnsandboxedCommands": false,
    "autoAllowBashIfSandboxed": false,
    "filesystem": {
      "denyRead": ["$HOME"],
      "allowRead": ["$T", "$K"],
      "allowWrite": ["$T", "$K"],
      "denyWrite": ["$T/.git/config", "$T/.git/hooks"]
    },
    "network": { "allowedDomains": [] }
  },
  "env": { "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOCACHE": "$K/gocache", "GOTMPDIR": "$K/tmp", "GOENV": "off",
           "GIT_CONFIG_GLOBAL": "$K/gitconfig", "GIT_CONFIG_NOSYSTEM": "1",
           "SECURITY_GUIDANCE_DISABLE": "1", "ENABLE_CODE_SECURITY_REVIEW": "0"$EXTRA_ENV }
}
JSON

# The security-guidance plugin is enabled by managed settings; disable it for this
# session via env (must be in the process env before claude starts).
herdr pane run $PANE "export PATH=$GO_BIN_DIR:\$PATH GOTOOLCHAIN=local SECURITY_GUIDANCE_DISABLE=1 ENABLE_CODE_SECURITY_REVIEW=0; clear" >/dev/null
sleep 2
log "start agent ($MODEL)"
herdr agent start $NAME --kind claude --pane $PANE --timeout 60000 -- \
  --model $MODEL --setting-sources project --settings $CONTROL/settings/$ID.json \
  --permission-mode dontAsk --strict-mcp-config \
  --append-system-prompt-file $NOTE >> $LOG 2>&1
# The folder-trust dialog can appear after herdr already reported the agent as
# ready (seen when many trials start at once). Only proceed after two consecutive
# idle readings, 3 s apart, with no trust dialog on screen.
stable=0
for i in {1..40}; do
  if herdr agent read $NAME --source visible 2>/dev/null | grep -q "trust this folder\|Yes, I trust"; then
    log "accept trust dialog"; herdr agent send-keys $NAME down >/dev/null; sleep 1; herdr agent send-keys $NAME enter >/dev/null
    stable=0; sleep 3; continue
  fi
  case $(st) in idle|done) (( stable++ )); (( stable >= 2 )) && break ;; *) stable=0 ;; esac
  sleep 3
done
(( stable >= 2 )) || { log "agent not ready: $(st)"; screen; exit 1; }

# Claude Code may pop an "LSP plugin recommendation" dialog when it sees .go files.
# It is not a permission prompt; answer "Not now" so it never blocks the loop.
dismiss_lsp(){
  if herdr agent read $NAME --source visible 2>/dev/null | grep -q "Install this LSP plugin"; then
    log "dismiss LSP recommendation"; herdr agent send-keys $NAME 2 >/dev/null; sleep 2
  fi
}

# Send the task and make sure it was really submitted: under load the trust dialog
# can appear late, or the text can sit unsent in the input box. A submitted prompt
# creates a session transcript containing a user message.
SESS_DIR=$HOME/.claude/projects/${T//[^A-Za-z0-9]/-}
submitted(){ local f; f=$(ls -t $SESS_DIR/*.jsonl 2>/dev/null | head -1); [[ -n $f ]] && grep -q '"type":"user"' $f; }
accept_trust(){
  if herdr agent read $NAME --source visible 2>/dev/null | grep -q "trust this folder\|Yes, I trust"; then
    log "accept trust dialog (late)"; herdr agent send-keys $NAME down >/dev/null; sleep 1; herdr agent send-keys $NAME enter >/dev/null; sleep 5
  fi
}
for attempt in 1 2 3; do
  accept_trust
  log "send task $TASK (attempt $attempt)"
  herdr agent prompt $NAME "$(cat $TASK)" --wait --timeout ${PROMPT_TIMEOUT_MS:-1500000} >> $LOG 2>&1
  sleep 3
  submitted && break
  log "prompt not submitted; retrying"
  accept_trust
  herdr agent send-keys $NAME ctrl+u >/dev/null 2>&1; sleep 2
done
submitted || { log "PROMPT NEVER SUBMITTED"; screen 30; exit 5; }
for i in {1..10}; do
  dismiss_lsp
  s=$(st); log "status $s"
  [[ $s == idle || $s == done ]] && break
  if [[ $s == blocked ]]; then log "BLOCKED"; screen 40; exit 3; fi
  herdr agent wait $NAME --timeout 600000 >/dev/null 2>&1
done
screen 30
dismiss_lsp

HEAD=$(tgit rev-parse HEAD); BASESHA=$(git -C $BASE rev-parse main)
log "base $BASESHA head $HEAD dirty=$(tgit status --porcelain | wc -l | tr -d ' ')"
log "session $(ls -t $HOME/.claude/projects/${T//[^A-Za-z0-9]/-}/*.jsonl 2>/dev/null | head -1)"
[[ $HEAD == $BASESHA ]] && { log "NO COMMIT"; exit 4; }
log "DONE"
