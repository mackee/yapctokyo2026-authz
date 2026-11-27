#!/bin/zsh
# Grade every trial listed in a manifest (trial variant task grader key) that has no
# summary yet, then print per-trial verdicts and per task/variant totals.
# usage: grade-manifest.sh <manifest.tsv> <out-dir-for-verdicts>
set -u
M=$1; OUT=$2; S=${0:A:h}; C=${CONTROL:-$HOME/src/haiku-probe-control}; P=${PROBE_ROOT:-$HOME/src/haiku-probe}
mkdir -p $OUT; : > $OUT/verdicts.jsonl
tail -n +2 $M | sort -u | while IFS=$'\t' read -r id variant task grader key; do
  sum=$C/results/$grader-trial$id.json
  if [[ ! -s $sum ]]; then
    [[ -d $P/trials/$id ]] || { print -r -- "{\"trial\":\"$id\",\"missing\":true}" >> $OUT/verdicts.jsonl; continue; }
    $C/grader/$grader/run.sh $P/trials/$id $sum >/dev/null 2>&1
  fi
  k=(); [[ $key != - ]] && k=($key)
  python3 -I $S/verdict.py $sum $k | python3 -I -c "import json,sys;d=json.load(sys.stdin);d.update(trial='$id',variant='$variant',task='$task',grader='$grader');print(json.dumps(d,ensure_ascii=False))" >> $OUT/verdicts.jsonl
done
python3 -I - $OUT/verdicts.jsonl <<'PY'
import json,sys,collections
rows=[json.loads(l) for l in open(sys.argv[1])]
agg=collections.defaultdict(lambda: {"n":0,"authz_ok":0,"func_ok":0,"invalid":0,"trials":[]})
for r in rows:
    if r.get("missing"): continue
    a=agg[(r["task"],r["variant"])]; a["n"]+=1; a["trials"].append(r["trial"]+("" if r["authz_ok"] else "✗"))
    a["authz_ok"]+=r["authz_ok"]; a["func_ok"]+=r["func_ok"]; a["invalid"]+= (not r["valid_run"])
print("task\tvariant\tn\tauthz_ok\tfunc_ok\tinvalid\ttrials")
for (t,v),a in sorted(agg.items()):
    print(f"{t}\t{v}\t{a['n']}\t{a['authz_ok']}\t{a['func_ok']}\t{a['invalid']}\t{' '.join(sorted(a['trials']))}")
PY
