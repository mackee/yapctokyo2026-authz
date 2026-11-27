#!/bin/bash
# Run the Go toolchain on untrusted code inside sandbox-exec.
# usage: sandboxed-go.sh <work-dir> <go args...>   (cwd must be inside <work-dir>)
# No network, nothing under $HOME readable, only <work-dir> writable.
set -u
W="$1"; shift
HERE="$(cd "$(dirname "$0")" && pwd)"
GO_BIN_DIR="${GO_BIN_DIR:-/opt/homebrew/bin}"
mkdir -p "$W/.gocache" "$W/.gotmp"
# Optional: start from a warm build cache (APFS clonefile copy, so the sandbox's writes
# never reach the original) and read modules from a shared read-only module cache.
if [ -n "${WARM_GOCACHE:-}" ] && [ -z "$(ls -A "$W/.gocache")" ]; then
  cp -c -R "$WARM_GOCACHE/." "$W/.gocache/"
fi
MODCACHE="${SHARED_GOMODCACHE:-$W/go/pkg/mod}"
exec sandbox-exec -f "$HERE/grade-sandbox.sb" -D HOME="$HOME" -D W="$W" \
  env -i PATH="$GO_BIN_DIR:/usr/bin:/bin" HOME="$W" TMPDIR="$W/.gotmp" \
  GOCACHE="$W/.gocache" GOTMPDIR="$W/.gotmp" GOENV=off GOTOOLCHAIN=local \
  GOPROXY=off GOFLAGS= GOTELEMETRY=off GOMODCACHE="$MODCACHE" \
  "$GO_BIN_DIR/go" "$@"
