#!/usr/bin/env bash
# The git half of the re-pin (FORK.md, "Re-pin"): rebase the consumed branch's
# carried commits onto the upstream head into a candidate branch and report.
# Pushing, testing and landing the candidate belong to the sync workflow — or
# to the operator running the same steps by hand.
#
#   CONSUMED=poc/agent-platform UPSTREAM_REMOTE=upstream UPSTREAM_REF=main \
#   CANDIDATE=sync/upstream-<date>-<sha7> scripts/fork/repin.sh
#
# Exit 0: the candidate branch exists locally (status=rebased) or nothing is to
# do (status=up-to-date). Exit 2: a carried commit conflicts (status=conflict);
# the rebase is aborted and the working tree is back on the consumed head.
# The report is written to $REPORT (markdown) and $REPORT_ENV (key=value).
set -euo pipefail

CONSUMED=${CONSUMED:-poc/agent-platform}
UPSTREAM_REMOTE=${UPSTREAM_REMOTE:-upstream}
UPSTREAM_REF=${UPSTREAM_REF:-main}
REPORT=${REPORT:-repin-report.md}
REPORT_ENV=${REPORT_ENV:-repin-report.env}

git fetch -q origin "$CONSUMED"
git fetch -q "$UPSTREAM_REMOTE" "$UPSTREAM_REF"
head=$(git rev-parse "origin/$CONSUMED")
target=$(git rev-parse "$UPSTREAM_REMOTE/$UPSTREAM_REF")
pin=$(git merge-base "$head" "$target")
CANDIDATE=${CANDIDATE:-sync/upstream-$(date -u +%Y%m%d)-${target:0:7}}

short() { git rev-parse --short=7 "$1"; }
subject() { git show -s --format=%s "$1"; }
cdate() { git show -s --format=%cI "$1"; }
item() { printf -- '- `%s` %s\n' "$(short "$1")" "$(subject "$1")"; }
patch_id() { git diff-tree -p "$1" | git patch-id --stable | awk '{print $1}'; }
env_out() { printf '%s=%s\n' "$1" "$2" >> "$REPORT_ENV"; }

mapfile -t carried < <(git rev-list --reverse --no-merges "$pin..$head")

: > "$REPORT_ENV"
env_out consumed "$CONSUMED"
env_out old_head "$head"
env_out pin "$pin"
env_out target "$target"
env_out candidate "$CANDIDATE"

{
  printf '### Re-pin `%s` onto `%s/%s`\n\n' "$CONSUMED" "$UPSTREAM_REMOTE" "$UPSTREAM_REF"
  printf '| | commit | date |\n|---|---|---|\n'
  printf '| previous pin | `%s` | %s |\n' "$(short "$pin")" "$(cdate "$pin")"
  printf '| new pin | `%s` | %s |\n' "$(short "$target")" "$(cdate "$target")"
  printf '| consumed head before | `%s` | %s |\n\n' "$(short "$head")" "$(cdate "$head")"
  printf '%d upstream commits since the previous pin.\n\n' "$(git rev-list --count "$pin..$target")"
  printf '#### Carried commits (%d, in order)\n\n' "${#carried[@]}"
  for c in "${carried[@]}"; do item "$c"; done
  printf '\n'
} > "$REPORT"

if [ "$pin" = "$target" ]; then
  printf 'Nothing to do: the consumed branch is already based on the upstream head.\n' >> "$REPORT"
  env_out status up-to-date
  exit 0
fi

# Upstream commits since the pin by patch id: names the upstream commit a
# dropped carried commit was merged as.
declare -A upstream_by_patch=()
while read -r sha; do
  upstream_by_patch[$(patch_id "$sha")]=$sha
done < <(git rev-list --no-merges "$pin..$target")

git checkout -q -B "$CANDIDATE" "$head"
if ! git rebase --quiet --no-reapply-cherry-picks --empty=drop --onto "$target" "$pin" 2> rebase.err; then
  conflict=$(git rev-parse REBASE_HEAD)
  applied=$(git rev-parse HEAD)
  mapfile -t files < <(git diff --name-only --diff-filter=U)
  git rebase --abort
  git checkout -q --detach "$head"
  {
    printf '#### Conflict\n\n'
    printf 'Carried commit `%s` %s does not apply onto `%s`.\n\n' "$(short "$conflict")" "$(subject "$conflict")" "$(short "$target")"
    printf 'Conflicting files:\n\n'
    for f in "${files[@]}"; do printf -- '- `%s`\n' "$f"; done
    printf '\nCommits replayed before it: %d (`%s`).\n\n' "$(git rev-list --count "$target..$applied")" "$(short "$applied")"
    printf 'Resolve by hand (FORK.md, "Manual re-pin"): rebase the consumed branch onto `%s`, fix the conflict in that commit, run the checks on a `sync/**` branch, force-push.\n' "$(short "$target")"
  } >> "$REPORT"
  env_out status conflict
  env_out conflict "$conflict"
  env_out conflict_subject "$(subject "$conflict")"
  exit 2
fi

new_head=$(git rev-parse HEAD)
mapfile -t replayed < <(git rev-list --reverse --no-merges "$target..$new_head")
dropped=()
{
  printf '#### Replayed (%d)\n\n' "${#replayed[@]}"
  for c in "${replayed[@]}"; do item "$c"; done
  printf '\n'
} >> "$REPORT"

# A carried commit with no replayed commit of the same subject was dropped:
# merged upstream (same patch id in the upstream range) or superseded.
declare -A replayed_subjects=()
for c in "${replayed[@]}"; do replayed_subjects[$(subject "$c")]=$c; done
for c in "${carried[@]}"; do
  s=$(subject "$c")
  [ -n "${replayed_subjects[$s]:-}" ] && continue
  dropped+=("$c")
done
if [ ${#dropped[@]} -gt 0 ]; then
  printf '#### Dropped (%d)\n\n' "${#dropped[@]}" >> "$REPORT"
  for c in "${dropped[@]}"; do
    up=${upstream_by_patch[$(patch_id "$c")]:-}
    if [ -n "$up" ]; then
      printf -- '- `%s` %s — merged upstream as `%s`\n' "$(short "$c")" "$(subject "$c")" "$(short "$up")"
    else
      printf -- '- `%s` %s — empty after the rebase (superseded upstream)\n' "$(short "$c")" "$(subject "$c")"
    fi
  done >> "$REPORT"
  printf '\nRemove the dropped rows from FORK.md and giantswarm/giantswarm#37742 in a follow-up pull request.\n\n' >> "$REPORT"
fi

env_out status rebased
env_out new_head "$new_head"
env_out replayed_count "${#replayed[@]}"
env_out dropped_count "${#dropped[@]}"
env_out dropped "$(IFS=,; echo "${dropped[*]:-}")"
