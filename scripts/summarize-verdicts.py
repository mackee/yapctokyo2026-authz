#!/usr/bin/env python3
"""Print per-task authz_ok / func_ok / over-denial counts for one or more verdict sets.

usage: scripts/summarize-verdicts.py LABEL=results/<name>-verdicts[:VARIANT] [LABEL=...]

Append :VARIANT to keep only rows of that variant (e.g. replication-verdicts:cap).

Each cell is authz_ok/func_ok/over-denial, where over-denial = authz_ok and not func_ok.
The TOTAL row sums the seven core tasks (move ... movemember), as the reports do: it leaves
out freeze and the one-off tasks that some verdict sets also hold.

Example (the original comparison vs. the Quint conditions, if-chain x Haiku):
  scripts/summarize-verdicts.py orig=results/haiku-v2-sql-verdicts \\
      A=results/v2-sql-quinta-haiku-verdicts B=results/v2-sql-quintb-haiku-verdicts
"""
import collections
import json
import os
import sys

CORE = ["move", "role", "remove", "offboard", "transfer", "bulkrole", "movemember"]
TASKS = CORE + ["freeze"]


def load(path, variant=None):
    d = collections.defaultdict(lambda: [0, 0, 0, 0, 0])  # n authz func over invalid
    with open(os.path.join(path, "verdicts.jsonl")) as f:
        for line in f:
            v = json.loads(line)
            if v.get("missing") or (variant and v.get("variant") != variant):
                continue
            t = d[v["task"]]
            t[0] += 1
            t[1] += bool(v["authz_ok"])
            t[2] += bool(v["func_ok"])
            t[3] += bool(v["authz_ok"] and not v["func_ok"])
            t[4] += not v["valid_run"]
    return d


def main(argv):
    if not argv:
        print(__doc__)
        return 2
    sets = []
    for a in argv:
        label, _, path = a.partition("=")
        if not path:
            sys.exit(f"expected LABEL=path[:VARIANT], got {a!r}")
        variant = None
        head, sep, tail = path.rpartition(":")
        if sep and "/" not in tail:
            path, variant = head, tail
        if label in dict(sets):
            sys.exit(f"duplicate label {label!r}")
        d = load(path, variant)
        if not d:
            sys.exit(f"{a}: no verdict rows" + (f" with variant {variant!r}" if variant else ""))
        sets.append((label, d))
    tasks = [t for t in TASKS if any(t in d for _, d in sets)]
    tasks += sorted({t for _, d in sets for t in d} - set(tasks))
    print("task".ljust(11) + " | ".join(l.rjust(14) for l, _ in sets) + "   (authz/func/over-denial)")
    tot = {l: [0, 0, 0, 0] for l, _ in sets}
    for t in tasks:
        cells = []
        for l, d in sets:
            x = d.get(t, [0, 0, 0, 0, 0])
            c = f"{x[1]}/{x[2]}/{x[3]} n{x[0]}"
            if x[4]:
                c += f" inv{x[4]}"
            cells.append(c)
            if t in CORE:
                for k in range(4):
                    tot[l][k] += x[k]
        print(t.ljust(11) + " | ".join(c.rjust(14) for c in cells))
    print("TOTAL-core".ljust(11) + " | ".join(f"{v[1]}/{v[2]}/{v[3]} n{v[0]}".rjust(14) for v in tot.values()))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
