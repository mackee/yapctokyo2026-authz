#!/usr/bin/env python3
"""Classify one grade summary (grade.sh JSON).

authz_ok : no failure in AUTHZ_*, ATOM_* and the task's key subtests (substring match)
func_ok  : no failure in FUNC_*
Usage: verdict.py <summary.json> [key-substring ...]
"""
import json, sys
s = json.load(open(sys.argv[1]))
keys = sys.argv[2:]
fails = [f["name"] for f in s.get("failures", [])]
authz_fail = [n for n in fails if n.startswith(("AUTHZ_", "ATOM_")) or any(k in n for k in keys)]
func_fail = [n for n in fails if n.startswith("FUNC_")]
ok = s.get("integrity_ok") and s.get("build_ok") and s.get("grader_ran")
print(json.dumps({
    "file": sys.argv[1].rsplit("/", 1)[-1],
    "valid_run": bool(ok),
    "authz_ok": bool(ok) and not authz_fail,
    "func_ok": bool(ok) and not func_fail,
    "authz_fail": authz_fail, "func_fail_count": len(func_fail), "total_fail": len(fails),
}, ensure_ascii=False))
