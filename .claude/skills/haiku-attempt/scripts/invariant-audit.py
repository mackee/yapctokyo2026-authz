#!/usr/bin/env python3
"""Report whether a cap trial changed code inside `func (s *state) check()`.
Reads the trial's full state.go from stdin (post-trial) and the baseline state.go path as argv[1].
Prints JSON {changed: bool, removed: [...], added: [...]} comparing the check() bodies."""
import sys, re, json
def body(src):
    m = re.search(r'func \(s \*state\) check\(\) error \{', src)
    if not m: return None
    i = m.end(); depth = 1
    while depth and i < len(src):
        depth += {'{':1,'}':-1}.get(src[i],0); i += 1
    lines = [l.strip() for l in src[m.start():i].splitlines()]
    return [l for l in lines if l and not l.startswith('//')]
base = body(open(sys.argv[1]).read()); cur = body(sys.stdin.read())
if cur is None: print(json.dumps({"changed": True, "missing": True})); sys.exit()
print(json.dumps({"changed": base != cur, "removed": [l for l in base if l not in cur], "added": [l for l in cur if l not in base]}, ensure_ascii=False))
