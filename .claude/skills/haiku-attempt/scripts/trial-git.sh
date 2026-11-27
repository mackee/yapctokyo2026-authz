#!/bin/bash
# Run git on an untrusted trial repo: read-only, inside sandbox-exec (trial-git.sb:
# only the trial readable under $HOME, writes only to a scratch dir, no network, and
# exec limited to the git binary itself), with exec-capable config neutralized, so a
# planted filter/hook/fsmonitor/pager/diff driver cannot run.
# usage: trial-git.sh <trial-dir> <git args...>
set -u
T="$(cd "$1" && pwd -P)"; shift
HERE="$(cd "$(dirname "$0")" && pwd)"
# real git binary (the /usr/bin/git xcrun shim wants to write a cache under /var/folders)
GIT="${TRIAL_GIT_BIN:-/Library/Developer/CommandLineTools/usr/bin/git}"
W="$(mktemp -d /private/tmp/trialgit.XXXXXX)"
trap 'rm -rf "$W"' EXIT
# env is cleared outside the sandbox (inside, only $GIT may exec)
/usr/bin/env -i PATH=/usr/bin:/bin HOME="$W" TMPDIR="$W" GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null \
  GIT_OPTIONAL_LOCKS=0 GIT_PAGER=cat GIT_TERMINAL_PROMPT=0 GIT_ATTR_NOSYSTEM=1 \
  /usr/bin/sandbox-exec -f "$HERE/trial-git.sb" -D HOME="$HOME" -D R="$T" -D W="$W" -D GIT="$GIT" \
  "$GIT" -C "$T" -c core.fsmonitor=false -c core.hooksPath=/dev/null \
  -c core.pager=cat -c core.sshCommand=false -c core.attributesFile=/dev/null \
  -c safe.directory="$T" --no-optional-locks "$@"
