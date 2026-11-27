#!/bin/zsh
# Create the isolated base repo for haiku-attempt trials from fixture/taskboard.
# usage: setup-base.sh   (env: PROBE_ROOT, GO_BIN_DIR, FIXTURE=<fixture dir>, BASE_REPO=<base repo path>)
set -eu
SKILL_DIR=${0:A:h:h}
FIXTURE=${FIXTURE:-${SKILL_DIR:h:h:h}/fixture/taskboard}
PROBE_ROOT=${PROBE_ROOT:-$HOME/src/haiku-probe}
GO_BIN_DIR=${GO_BIN_DIR:-/opt/homebrew/bin}
BASE=${BASE_REPO:-$PROBE_ROOT/base}

if [[ -e $BASE ]]; then
  print -u2 "$BASE already exists; remove it first if you really want a fresh base"
  exit 1
fi
mkdir -p $BASE $PROBE_ROOT/trials
cp -R $FIXTURE/. $BASE/   # whole fixture (tanukirpc variant has internal/ and vendor/)
cd $BASE
git init -q -b main
export GOTOOLCHAIN=local GOPROXY=off
$GO_BIN_DIR/gofmt -l .
GOFLAGS= $GO_BIN_DIR/go vet ./...
GOFLAGS= $GO_BIN_DIR/go test -count=1 ./...
git add -A
git -c user.name=probe -c user.email=probe@example.invalid commit -qm "taskboard baseline"
git log --oneline -1
