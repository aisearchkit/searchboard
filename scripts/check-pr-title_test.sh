#!/usr/bin/env bash
# Table-driven tests for scripts/check-pr-title.sh.
set -uo pipefail

cd "$(dirname "$0")"

failures=0

expect() {
  local want="$1" title="$2" got=0
  ./check-pr-title.sh "$title" >/dev/null 2>&1 || got=$?
  if { [ "$want" = ok ] && [ "$got" -ne 0 ]; } || { [ "$want" = bad ] && [ "$got" -eq 0 ]; }; then
    echo "FAIL: expected $want for: $title"
    failures=$((failures + 1))
  fi
}

expect ok  "feat(esclient): add version detection"
expect ok  "fix: handle empty shard list"
expect ok  "docs(docs): describe the matrix api"
expect ok  "feat(api)!: rename cluster routes"
expect ok  "chore(deps): bump vite"
expect bad ""
expect bad "Add version detection"
expect bad "feature(esclient): add version detection"
expect bad "feat(unknown): add thing"
expect bad "feat(esclient): Add version detection"
expect bad "feat(esclient): add version detection."
expect bad "feat(esclient):missing space"
expect bad "feat(esclient): add $(printf '\xe4\xb8\xad\xe6\x96\x87')"
expect bad "feat(esclient): this title is far too long to be useful because it keeps going and going"

if [ "$failures" -ne 0 ]; then
  echo "$failures test(s) failed"
  exit 1
fi
echo "All PR title tests passed"
