#!/usr/bin/env bash
# Fails if any tracked text file contains CJK characters or full-width punctuation.
# Project language policy: all code, comments, docs and commit text are English.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# Unicode ranges: CJK punctuation, CJK extension A, CJK unified ideographs,
# Hangul syllables, Hiragana/Katakana, and full-width forms.
pattern='[\x{3000}-\x{30FF}\x{3400}-\x{4DBF}\x{4E00}-\x{9FFF}\x{AC00}-\x{D7AF}\x{FF00}-\x{FFEF}]'

status=0
while IFS= read -r -d '' file; do
  case "$file" in
    *package-lock.json|*Cargo.lock|*go.sum) continue ;;
  esac
  [ -f "$file" ] || continue
  if LC_ALL=C.UTF-8 grep -InP "$pattern" -- "$file" >/tmp/check-english.out 2>/dev/null; then
    echo "Non-English text found in $file:"
    head -n 5 /tmp/check-english.out
    status=1
  fi
done < <(git ls-files -z)

if [ "$status" -ne 0 ]; then
  echo
  echo "All project text must be written in English. See docs/contributing.md."
fi
exit "$status"
