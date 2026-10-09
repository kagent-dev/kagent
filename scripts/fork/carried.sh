#!/usr/bin/env bash
# The carried-commit ledger (FORK.md, "Which kagent are we running"): one file
# per carried change under fork/carried/, each the one-row table FORK.md used
# to hold, so a pull request that adds a carried commit adds a file and never
# touches a line another pull request adds.
#
#   scripts/fork/carried.sh          # print the table, rows in the order the
#                                    # commits sit on top of the pin
#   scripts/fork/carried.sh --check  # verify the files, print nothing
#
# A file is the table header, its separator and one row of three cells; the
# first cell names the carried commit(s) in backticks, by subject (the pull
# request number GitHub appends to a subject is ignored). A file of another
# shape, a subject that is not a commit on top of the pin (upstream merged it:
# delete the file in the re-pin's follow-up; or it is misspelt) and a commit
# two files record are each a problem: they are listed on stderr and the exit
# code is 1. The pin is the merge base with the mirror (MIRROR, default
# origin/main), as in the re-pin.
set -euo pipefail

mode=${1:-print}
case $mode in
  print | --check) ;;
  *)
    echo "usage: $0 [--check]" >&2
    exit 64
    ;;
esac

root=$(cd "$(dirname "$0")/../.." && pwd)
dir=fork/carried
header='| Carried commit (subject) | Why the platform needs it | Upstream |'
separator='|---|---|---|'
# A conventional commit subject inside backticks: `type(scope): text`.
# shellcheck disable=SC2016
subject_re='`([a-z]+(\([^)`]*\))?!?: [^`]+)`'

pin=$(git -C "$root" merge-base HEAD "${MIRROR:-origin/main}")
declare -A position=()
n=0
while IFS= read -r subject; do
  n=$((n + 1))
  position[$subject]=$n
done < <(git -C "$root" log --reverse --no-merges --format=%s "$pin..HEAD" | sed -E 's/ \(#[0-9]+\)$//')

problems=0
problem() {
  printf '%s: %s\n' "$1" "$2" >&2
  problems=$((problems + 1))
}

declare -A recorded=()
rows=()
shopt -s nullglob
for file in "$root/$dir"/*.md; do
  name=$dir/$(basename "$file")
  mapfile -t lines < "$file"
  if [ "${#lines[@]}" -ne 3 ] || [ "${lines[0]}" != "$header" ] || [ "${lines[1]}" != "$separator" ]; then
    problem "$name" "not the table header, its separator and one row"
    continue
  fi
  row=${lines[2]}
  # An escaped pipe (`\|`, a literal one inside a cell) is no cell border.
  unescaped=${row//\\|/}
  pipes=${unescaped//[^|]/}
  if [[ $row != "| "* || $row != *" |" || ${#pipes} -ne 4 ]]; then
    problem "$name" "the row does not have three cells"
    continue
  fi
  cell=${unescaped#"| "}
  cell=${cell%%" |"*}
  order=
  keys=0
  while [[ $cell =~ $subject_re ]]; do
    key=${BASH_REMATCH[1]}
    cell=${cell#*"${BASH_REMATCH[0]}"}
    [[ $key =~ ^(.*)\ \(#[0-9]+\)$ ]] && key=${BASH_REMATCH[1]}
    keys=$((keys + 1))
    if [ -z "${position[$key]:-}" ]; then
      problem "$name" "names \`$key\`, which is not a commit on top of the pin: upstream merged it (delete the file), or the subject is misspelt"
      continue
    fi
    if [ -n "${recorded[$key]:-}" ]; then
      problem "$name" "names \`$key\`, which ${recorded[$key]} already records"
      continue
    fi
    recorded[$key]=$name
    if [ -z "$order" ] || [ "${position[$key]}" -lt "$order" ]; then
      order=${position[$key]}
    fi
  done
  if [ "$keys" -eq 0 ]; then
    problem "$name" "names no carried commit in its first cell"
    continue
  fi
  [ -n "$order" ] && rows+=("$order	$row")
done

if [ "$problems" -gt 0 ]; then
  printf '%d problem(s) in %s\n' "$problems" "$dir" >&2
  exit 1
fi
[ "$mode" = --check ] && exit 0

printf '%s\n%s\n' "$header" "$separator"
[ "${#rows[@]}" -gt 0 ] && printf '%s\n' "${rows[@]}" | sort -n -k1,1 | cut -f2-
exit 0
