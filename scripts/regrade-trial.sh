#!/bin/zsh
# Rebuild one archived trial from its fixture and its diff, grade it with the frozen
# grader, and print the verdict.
#
# usage: scripts/regrade-trial.sh <trial-id> [grader [key]]
#   trial-id  e.g. 1352 (see results/trial-bases.tsv)
#   grader    grader name, e.g. 022-200, 004v2-200, 016; default: from results/*-manifest.tsv
#   key       key subtest for verdict.py (the manifests' "key" column; "-" = none)
#
# env: SHARED_GOMODCACHE  module cache outside $HOME (needed by the SQLite fixtures; the
#                         tanukirpc fixtures are vendored)
#      WARM_GOCACHE       optional build cache to copy in (speeds up grading)
#
# Of trials 001-015 only 005, 009-012 and 015 have a diff here; for the others use the
# haiku-trials/NNN branches (reports/*/02-trial-branches.md).
set -eu
ID=$(printf %03d $((10#$1))); R=${0:A:h:h}
export YAPC_REPO=${YAPC_REPO:-$R}   # lets the graders' run.sh find grade.sh without .git
if (( $# >= 2 )); then G=$2; KEY=${3:--}
else
  row=$(awk -F'\t' -v i=$ID '$1==i{print $4"\t"$5; exit}' $R/results/*-manifest.tsv)
  [[ -n $row ]] || { print -u2 "trial $ID is in no manifest; pass the grader"; exit 1; }
  G=${row%%$'\t'*}; KEY=${row#*$'\t'}
fi
FIX=$(awk -F'\t' -v i=$ID '$1==i{print $3}' $R/results/trial-bases.tsv)
DIFF=$R/results/structure/trials/$ID.diff
[[ -n $FIX && -d $R/fixture/$FIX ]] || { print -u2 "no fixture for trial $ID"; exit 1; }
[[ -f $DIFF ]] || { print -u2 "no diff for trial $ID"; exit 1; }
if [[ $G == *-200 ]]; then GD=$R/results/structure/graders-200/${G%-200}
elif [[ -d $R/results/structure/graders/$G ]]; then GD=$R/results/structure/graders/$G
else GD=$R/results/$G; fi
[[ -f $GD/zz_grader_test.go ]] || { print -u2 "no grader $G"; exit 1; }
if [[ -f $R/fixture/$FIX/go.sum && ! -d $R/fixture/$FIX/vendor && -z ${SHARED_GOMODCACHE:-} ]]; then
  print -u2 "fixture $FIX needs modules: set SHARED_GOMODCACHE (see REPRODUCE.md, level 2)"; exit 1
fi

W=$(mktemp -d /private/tmp/regrade-$ID.XXXXXX)
cp -R $R/fixture/$FIX/. $W/sub/ 2>/dev/null || { mkdir -p $W/sub; cp -R $R/fixture/$FIX/. $W/sub/; }
git -C $W/sub init -q
git -C $W/sub apply $DIFF
$GD/run.sh $W/sub $W/summary.json > $W/grade.log 2>&1 || true
[[ -s $W/summary.json ]] || { print -u2 "grading produced no summary; see $W/grade.log:"; tail -20 $W/grade.log >&2; exit 1; }
k=(); [[ $KEY != - ]] && k=($KEY)
python3 -I $R/.claude/skills/haiku-attempt/scripts/verdict.py $W/summary.json $k
print -u2 "work dir: $W (archived summary: results/structure/trials/$G-trial$ID.json)"
