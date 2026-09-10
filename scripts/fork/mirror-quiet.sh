#!/usr/bin/env bash
# Cancel the workflow runs that upstream's own workflow files start when the
# mirror is pushed (FORK.md, "The mirror"): the mirrored refs carry upstream's
# ci.yaml / image-scan.yaml / tag.yaml, which ask for runners the fork does not
# have and publish to registries it must not touch.
#
#   scripts/fork/mirror-quiet.sh <since ISO-8601> <pushed-refs-file>
#
# The refs file is what mirror.sh wrote: one branch or tag name per line.
set -euo pipefail
since=$1
file=$2
[ -s "$file" ] || { echo "nothing was mirrored, nothing to cancel"; exit 0; }
mapfile -t refs < "$file"
repo=${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner -q .nameWithOwner)}
declare -A done=()
deadline=$((SECONDS + 90))
while :; do
  while read -r id branch name; do
    [ -n "$id" ] && [ -z "${done[$id]:-}" ] || continue
    for ref in "${refs[@]}"; do
      [ "$branch" = "$ref" ] || continue
      if gh run cancel "$id" --repo "$repo" > /dev/null 2>&1; then
        done[$id]=1
        echo "cancelled run ${id}: ${name} on ${branch}"
      fi
    done
  done < <(gh run list --repo "$repo" --event push --created ">=${since}" --limit 100 \
    --json databaseId,headBranch,workflowName,status --jq '.[] | select(.status != "completed") | "\(.databaseId) \(.headBranch) \(.workflowName)"')
  [ "$SECONDS" -gt "$deadline" ] && break
  sleep 15
done
echo "${#done[@]} run(s) cancelled after mirroring ${#refs[@]} ref(s)"
