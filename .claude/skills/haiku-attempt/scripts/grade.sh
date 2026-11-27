#!/bin/bash
# Grade one submission with a frozen grader. Untrusted code (build/vet/tests) runs
# only inside sandbox-exec (sandboxed-go.sh); the host never writes into, or reads
# results from, any directory the sandbox can write.
#
# usage: grade.sh <grader-dir> <submission-dir> <summary.json>
#   grader-dir holds zz_grader_test.go, FROZEN.sha256 and optionally EXPECTED_SUBTESTS
#   (number of top-level TestGrader/<name> subtests that must be reported).
set -u
GD="$(cd "$1" && pwd)"; SUB="$(cd "$2" && pwd)"
OUT="$3"; case "$OUT" in /*) ;; *) OUT="$PWD/$OUT" ;; esac  # absolute: the script cd's around
HERE="$(cd "$(dirname "$0")" && pwd)"
SG="$HERE/sandboxed-go.sh"

EXPECT="$(cut -d' ' -f1 "$GD/FROZEN.sha256")"
ACTUAL="$(shasum -a 256 "$GD/zz_grader_test.go" | cut -d' ' -f1)"
[ "$EXPECT" = "$ACTUAL" ] || { echo "grader hash mismatch" >&2; exit 2; }

W="$(mktemp -d /private/tmp/grade.XXXXXX)"
# Each step gets its OWN sandbox root (own copy, own GOCACHE/GOTMPDIR). A process left
# behind by step 1 stays confined to sb1 and cannot touch step 2's copy or cache.
SB1="$W/sb1"   # sandbox-writable for step 1 (submission's own build/vet/tests)
SB2="$W/sb2"   # sandbox-writable for step 2 (frozen grader); created only after step 1
HOUT="$W/out"  # host-only: logs and test output (never writable by any sandbox)
mkdir -p "$SB1" "$HOUT" "$(dirname "$OUT")"
# Copies + Go caches are ~300 MB per run; keep only the host-side logs in $HOUT.
trap 'chmod -R u+w "$SB1" "$SB2" 2>/dev/null; rm -rf "$SB1" "$SB2"' EXIT

# Run one sandboxed step in its own process group and kill the whole group when it
# returns, so nothing the untrusted code spawned outlives the step.
step(){
  local out="$1" err="$2"; shift 2
  set -m
  "$@" > "$out" 2> "$err" &
  local pid=$!
  wait "$pid"; local rc=$?
  kill -KILL -- "-$pid" 2>/dev/null
  pkill -KILL -f "$W/" 2>/dev/null   # anything still referring to this run's dirs
  set +m
  return $rc
}

copy_sub(){ # fresh copy of the submission (without .git) into $1
  mkdir -p "$1" && (cd "$SUB" && tar --exclude=.git -cf - .) | (cd "$1" && tar xf -)
}

# 1) the submission's own build/vet/tests, in their own copy
copy_sub "$SB1/own" || exit 3
cd "$SB1/own" || exit 3
rm -f zz_grader_test.go
step "$HOUT/build.txt" "$HOUT/build.err" "$SG" "$SB1" build ./...; BUILD=$?
step "$HOUT/vet.txt" "$HOUT/vet.err" "$SG" "$SB1" vet ./...; VET=$?
step "$HOUT/own.json" "$HOUT/own.err" "$SG" "$SB1" test -count=1 -json ./...; OWN=$?

# 2) the frozen grader, in a FRESH copy that the step-1 code never touched, with the
#    submission's own *_test.go removed so no TestMain/init in tests can hijack it.
cd "$W" || exit 3
copy_sub "$SB2/grade" || exit 3
find "$SB2/grade" -name '*_test.go' \( -type f -o -type l \) -delete
cp "$GD/zz_grader_test.go" "$SB2/grade/zz_grader_test.go"
[ "$(shasum -a 256 "$SB2/grade/zz_grader_test.go" | cut -d' ' -f1)" = "$EXPECT" ] || { echo "copied grader hash mismatch" >&2; exit 2; }
cd "$SB2/grade" || exit 3
step "$HOUT/grader.json" "$HOUT/grader.err" "$SG" "$SB2" test -count=1 -json -run '^TestGrader$' .; GR=$?
cd "$W" || exit 3

EXPECTED="$(cat "$GD/EXPECTED_SUBTESTS" 2>/dev/null || echo 0)"
NAMES="$GD/EXPECTED_SUBTESTS.list"; [ -f "$NAMES" ] || NAMES=""
python3 -I - "$HOUT" "$OUT" "$BUILD" "$VET" "$OWN" "$GR" "$SUB" "$EXPECTED" "$NAMES" <<'PY'
import json,sys,collections
H,OUT,build,vet,own,gr,sub,expected,names_file=sys.argv[1:10]
def load(p):
    ev=[]
    for l in open(p,errors='replace'):
        try: ev.append(json.loads(l))
        except Exception: pass
    return ev
ev=load(H+'/grader.json')
out=collections.defaultdict(list); res={}; terminal=collections.Counter()
for e in ev:
    t=e.get('Test')
    if not t: continue
    if e['Action']=='output': out[t].append(e['Output'])
    elif e['Action'] in('pass','fail','skip'): res[t]=e['Action']; terminal[t]+=1
cats=collections.defaultdict(lambda:{'pass':0,'fail':0})
fails=[]; info={}; n_sub=0
for t,a in sorted(res.items()):
    if not t.startswith('TestGrader/'): continue
    n=t.split('/',1)[1]
    if '/' in n: continue
    n_sub+=1
    cat=n.split('_')[0]
    cats[cat]['fail' if a=='fail' else 'pass']+=1
    msg=''.join(l for l in out[t] if not l.startswith(('=== ','--- '))).strip()
    if a=='fail': fails.append({'name':n,'message':msg})
    if cat=='INFO': info[n]=msg
expected=int(expected)
reported=sorted(t.split('/',1)[1] for t in res if t.startswith('TestGrader/') and '/' not in t.split('/',1)[1])
problems=[]
if expected and len(reported)!=expected: problems.append(f'count {len(reported)} != {expected}')
if names_file:
    want=sorted(l.strip() for l in open(names_file) if l.strip())
    if reported!=want: problems.append(f'name set differs: missing={sorted(set(want)-set(reported))[:5]} extra={sorted(set(reported)-set(want))[:5]}')
if terminal.get('TestGrader',0)!=1: problems.append('top-level TestGrader terminal event missing or duplicated')
any_fail = any(a=='fail' for t,a in res.items() if t.startswith('TestGrader'))
if (int(gr)==0) == any_fail: problems.append(f'grader exit {gr} inconsistent with parsed results (any_fail={any_fail})')
dups=[t for t,c in terminal.items() if t.startswith('TestGrader') and c!=1]
if dups: problems.append(f'duplicate terminal events: {dups[:5]}')
integrity_ok = not problems
own_ev=load(H+'/own.json')
json.dump({'submission':sub,'build_ok':build=='0','build_output':(open(H+'/build.txt',errors='replace').read()+open(H+'/build.err',errors='replace').read())[:2000],
 'vet_ok':vet=='0','own_tests_exit':int(own),
 'own_tests_passed':sum(1 for e in own_ev if e.get('Test') and e['Action']=='pass'),
 'own_tests_failed':[e['Test'] for e in own_ev if e.get('Test') and e['Action']=='fail'],
 'grader_exit':int(gr),'grader_ran':len(res)>0,'subtests_reported':n_sub,'subtests_expected':expected,
 'integrity_ok':integrity_ok,'integrity_problems':problems,'subtests':reported,'categories':cats,'failures':fails,'info':info,
 'grader_stderr':open(H+'/grader.err',errors='replace').read()[:2000],'workdir':str(H)},open(OUT,'w'),indent=2)
PY
