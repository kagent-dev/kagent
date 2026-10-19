#!/usr/bin/env bash
# The carried-commit ledger (FORK.md, "Which kagent are we running"): one file
# per carried change under fork/carried/, so a pull request that adds a carried
# commit adds a file and never touches a line another pull request adds.
#
#   scripts/fork/carried.sh          # print the ledger table, rows in the order
#                                    # the commits sit on top of the pin
#   scripts/fork/carried.sh --check  # verify the files, print nothing
#
# A file opens with the YAML front-matter `scripts/fork/ledger-header.sh`
# checks; its `commits:` list names the carried commit(s) by subject (the pull
# request number GitHub appends to a subject is ignored). A file without the
# list, a subject that is not a commit on top of the pin (upstream merged it:
# delete the file in the re-pin's follow-up; or it is misspelt) and a commit
# two files record are each a problem: they are listed on stderr and the exit
# code is 1. The pin is the merge base with the mirror (MIRROR, default
# origin/main), as in the re-pin. The printed table has one row per file: its
# subjects, its title, and its upstream outcome and exit from the header.
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
header='| Carried commit (subject) | Change | Upstream |'
separator='|---|---|---|'

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
  # The front-matter: `---` on line 1 to the next `---`. `commits:` is a block
  # list of single-quoted subjects ('' is a quote inside); `outcome:` and
  # `exit:` sit under `upstream:`; the title is the first `# ` line after it.
  mapfile -t subjects < <(awk '
    NR == 1 && $0 != "---" { exit }
    NR > 1 && $0 == "---" { exit }
    /^commits:/ { in_commits = 1; next }
    in_commits && /^  - / {
      v = substr($0, 5)
      if (v ~ /^\x27/) { v = substr(v, 2, length(v) - 2); gsub(/\x27\x27/, "\x27", v) }
      else if (v ~ /^"/) { v = substr(v, 2, length(v) - 2) }
      print v; next
    }
    in_commits { in_commits = 0 }
  ' "$file")
  mapfile -t meta < <(awk '
    NR == 1 && $0 != "---" { exit }
    NR > 1 && !closed && $0 == "---" { closed = 1; next }
    !closed && /^upstream:/ { up = 1; next }
    !closed && up && /^  outcome:/ { v = $0; sub(/^  outcome:[ \t]*/, "", v); outcome = v; next }
    !closed && up && /^  exit:/ { v = $0; sub(/^  exit:[ \t]*/, "", v); exitv = v; next }
    !closed && /^[a-z_]+:/ { up = 0 }
    closed && /^# / && !title { title = substr($0, 3) }
    END { print outcome; print exitv; print title }
  ' "$file")
  if [ "${#subjects[@]}" -eq 0 ]; then
    problem "$name" "names no carried commit in its commits: list"
    continue
  fi
  order=
  for key in "${subjects[@]}"; do
    [[ $key =~ ^(.*)\ \(#[0-9]+\)$ ]] && key=${BASH_REMATCH[1]}
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
  cell=$(printf '`%s`, ' "${subjects[@]}")
  cell=${cell%, }
  exitv=${meta[1]#\'}; exitv=${exitv%\'}; exitv=${exitv//\'\'/\'}
  exitv=${exitv#\"}; exitv=${exitv%\"}
  row="| $cell | ${meta[2]} | ${meta[0]}: $exitv |"
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
