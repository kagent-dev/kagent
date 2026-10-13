#!/usr/bin/env bash
# The re-pin's other git half (FORK.md, "Manual re-pin"): take a candidate an
# operator rebased and pushed by hand instead of rebasing in the workflow, check
# that it is a re-pin onto the upstream ref, and report like repin.sh, so the
# sync workflow waits for its checks and lands it the same way.
#
#   CONSUMED=giantswarm UPSTREAM_REMOTE=upstream UPSTREAM_REF=<sha or ref> \
#   CANDIDATE=sync/upstream-<date>-<sha7>-manual scripts/fork/take-candidate.sh
#
# Exit 0: the candidate is checked out detached (status=rebased) or it is
# already the consumed head (status=up-to-date). Exit 1: the candidate is not a
# re-pin onto the upstream ref, or it lost a carried hunk that ACCEPT_HUNKS does
# not name (status=refused, the reason in the report).
# The report is written to $REPORT (markdown) and $REPORT_ENV (key=value).
set -euo pipefail

CONSUMED=${CONSUMED:-giantswarm}
UPSTREAM_REMOTE=${UPSTREAM_REMOTE:-upstream}
UPSTREAM_REF=${UPSTREAM_REF:-main}
REPORT=${REPORT:-repin-report.md}
REPORT_ENV=${REPORT_ENV:-repin-report.env}
: "${CANDIDATE:?CANDIDATE names the candidate branch}"

short() { git rev-parse --short=7 "$1"; }
subject() { git show -s --format=%s "$1"; }
item() { printf -- '- `%s` %s\n' "$(short "$1")" "$(subject "$1")"; }
env_out() { printf '%s=%s\n' "$1" "$2" >> "$REPORT_ENV"; }

: > "$REPORT_ENV"
: > "$REPORT"
refuse() {
  printf '### Candidate `%s` refused\n\n%s\n' "$CANDIDATE" "$1" >> "$REPORT"
  env_out status refused
  exit 1
}

case $CANDIDATE in
  sync/*) ;;
  *) refuse 'A candidate is a `sync/**` branch: only those run the checks the landing waits for.' ;;
esac

git fetch -q origin "$CONSUMED" "refs/heads/$CANDIDATE:refs/remotes/origin/$CANDIDATE"
git fetch -q "$UPSTREAM_REMOTE" "$UPSTREAM_REF"
head=$(git rev-parse "origin/$CONSUMED")
target=$(git rev-parse FETCH_HEAD)
new_head=$(git rev-parse "origin/$CANDIDATE")
pin=$(git merge-base "$head" "$target")

env_out consumed "$CONSUMED"
env_out old_head "$head"
env_out pin "$pin"
env_out target "$target"
env_out candidate "$CANDIDATE"

if [ "$new_head" = "$head" ]; then
  printf 'Nothing to do: `%s` is already the consumed head.\n' "$CANDIDATE" >> "$REPORT"
  env_out status up-to-date
  exit 0
fi
# The candidate sits on exactly the upstream ref: no upstream commit past it
# and no carried commit below it.
if [ "$(git merge-base "$new_head" "$target")" != "$target" ]; then
  refuse "\`${CANDIDATE}\` (\`$(short "$new_head")\`) does not contain upstream \`$(short "$target")\`."
fi
if git merge-base --is-ancestor "$head" "$new_head"; then
  refuse "\`${CANDIDATE}\` contains the consumed head \`$(short "$head")\`: a re-pin rebases the carried commits, it does not merge them."
fi

# Every hunk the carried commits brought is in the candidate as it is in the
# consumed head, unless ACCEPT_HUNKS names it as dropped or reworked on purpose.
hunks=$(scripts/fork/hunks.py "$pin" "$head" "$new_head" 2>&1) || refuse "A carried hunk is lost in the candidate (\`scripts/fork/hunks.py\`):

${hunks}"

git checkout -q --detach "$new_head"
ledger=$(MIRROR="$target" scripts/fork/carried.sh --check 2>&1) || refuse "The ledger does not match the candidate (\`scripts/fork/carried.sh --check\`):

\`\`\`
${ledger}
\`\`\`"

mapfile -t carried < <(git rev-list --reverse --no-merges "$pin..$head")
mapfile -t replayed < <(git rev-list --reverse --no-merges "$target..$new_head")
declare -A replayed_subjects=()
for c in "${replayed[@]}"; do replayed_subjects[$(subject "$c")]=$c; done
dropped=()
for c in "${carried[@]}"; do
  [ -n "${replayed_subjects[$(subject "$c")]:-}" ] || dropped+=("$c")
done

{
  printf '### Re-pin `%s` onto `%s` from the candidate `%s`\n\n' "$CONSUMED" "$(short "$target")" "$CANDIDATE"
  printf '| | commit |\n|---|---|\n'
  printf '| previous pin | `%s` |\n| new pin | `%s` |\n' "$(short "$pin")" "$(short "$target")"
  printf '| consumed head before | `%s` |\n| candidate head | `%s` |\n\n' "$(short "$head")" "$(short "$new_head")"
  printf '#### Replayed (%d)\n\n' "${#replayed[@]}"
  for c in "${replayed[@]}"; do item "$c"; done
  printf '\n'
  if [ ${#dropped[@]} -gt 0 ]; then
    printf '#### Dropped or reworked (%d)\n\n' "${#dropped[@]}"
    for c in "${dropped[@]}"; do item "$c"; done
    printf '\n'
  fi
  printf '%s\n' "$hunks"
} >> "$REPORT"

env_out status rebased
env_out new_head "$new_head"
env_out replayed_count "${#replayed[@]}"
env_out dropped_count "${#dropped[@]}"
env_out dropped "$(IFS=,; echo "${dropped[*]:-}")"
