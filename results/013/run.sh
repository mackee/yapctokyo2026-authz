#!/bin/bash
# Grade one submission with this frozen grader: run.sh <submission-dir> <summary.json>
# grade.sh is found from the repository root (or $YAPC_REPO when this grader is copied elsewhere).
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="${YAPC_REPO:-$(git -C "$HERE" rev-parse --show-toplevel 2>/dev/null)}"
exec "$REPO/.claude/skills/haiku-attempt/scripts/grade.sh" "$HERE" "$1" "$2"
