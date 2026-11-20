#!/usr/bin/env bash
# Validates a pull request title against the Conventional Commits format.
#
#   <type>(<scope>)?: <subject>
#
# Usage: scripts/check-pr-title.sh "feat(esclient): add version detection"
# Source of truth for allowed types and scopes; keep docs/contributing.md in sync.
set -euo pipefail

TYPES='feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert'
SCOPES='server|api|esclient|tsdb|collector|agenthub|logengine|migration|inspector|store|config|domain|matrix|web|agent|ci|docs|deps|repo|release'
MAX_LENGTH=72

title="${1:-}"

fail() {
  echo "Invalid PR title: \"$title\""
  echo "  $1"
  echo
  echo "Expected: <type>(<scope>)?: <subject>"
  echo "  types : ${TYPES//|/, }"
  echo "  scopes: ${SCOPES//|/, } (optional)"
  echo "  subject: ASCII only, starts with a lowercase letter or digit, no trailing period, max ${MAX_LENGTH} chars in total"
  exit 1
}

[ -n "$title" ] || fail "Title is empty."

export LC_ALL=C
[[ "$title" =~ ^[[:print:]]+$ ]] || fail "Title must contain printable ASCII characters only."
[ "${#title}" -le "$MAX_LENGTH" ] || fail "Title is longer than ${MAX_LENGTH} characters."
[[ "$title" =~ ^($TYPES)(\(($SCOPES)\))?!?:\ [a-z0-9] ]] || fail "Title does not match the required format, or uses an unknown type or scope."
[[ "$title" != *. ]] || fail "Title must not end with a period."

echo "PR title OK: $title"
