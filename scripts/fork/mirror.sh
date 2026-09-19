#!/usr/bin/env bash
# Mirror upstream into the fork, fast-forward only (FORK.md, "The mirror"):
# `main`, every `release/v*.x` branch and every tag upstream has. Nothing is
# ever moved back or deleted; a mirror ref that is not an ancestor of its
# upstream ref was edited and stops the run.
#
#   scripts/fork/mirror.sh <token> [--check]
#
# The token has to be one that may update workflow files — GITHUB_TOKEN may
# not, not even in a mirrored upstream commit — so the sync passes the App's
# token; its pushes start upstream's workflows at the mirrored refs, which
# mirror-quiet.sh cancels. The pushed branch and tag names are written to
# $MIRROR_PUSHED (default mirror-pushed.txt). --check only prints them.
set -euo pipefail
token=$1
mode=${2:-}
here=$(cd "$(dirname "$0")" && pwd)

git fetch -q upstream 'refs/heads/main:refs/remotes/upstream/main' \
  'refs/heads/release/*:refs/remotes/upstream/release/*' '+refs/tags/*:refs/tags/*'
git fetch -q origin 'refs/heads/main:refs/remotes/origin/main' \
  'refs/heads/release/*:refs/remotes/origin/release/*' 2>/dev/null || true

pushed_file=${MIRROR_PUSHED:-mirror-pushed.txt}
: > "$pushed_file"
refspecs=()
names=()
for branch in main $(git for-each-ref --format='%(refname:strip=3)' refs/remotes/upstream/release/); do
  up=$(git rev-parse "refs/remotes/upstream/${branch}")
  if cur=$(git rev-parse -q --verify "refs/remotes/origin/${branch}"); then
    [ "$cur" = "$up" ] && continue
    if ! git merge-base --is-ancestor "$cur" "$up"; then
      echo "::error::origin/${branch} is not an ancestor of upstream/${branch} — the mirror was edited; restore it by hand (FORK.md, 'The mirror')."
      exit 1
    fi
  fi
  refspecs+=("${up}:refs/heads/${branch}")
  names+=("$branch")
  echo "branch ${branch}: ${cur:-(new)} -> ${up}"
done

# Tags: every upstream tag the fork lacks is copied; one the fork already holds
# at the same object is current. One the fork holds at ANOTHER object is a
# collision — the line's own release tags share upstream's `vX.Y.Z` shape and
# live in a major above upstream's (FORK.md, "Release scheme") — and stops the
# run, naming the tag, instead of one side silently winning.
tags_of() { git ls-remote --tags "$1" | sed -n 's#^\([0-9a-f]*\)[[:space:]]*refs/tags/\([^^]*\)$#\2 \1#p' | sort; }
declare -A fork_tag
while read -r tag sha; do
  [ -n "$tag" ] && fork_tag[$tag]=$sha
done < <(tags_of origin)
while read -r tag sha; do
  [ -n "$tag" ] || continue
  if [ -n "${fork_tag[$tag]:-}" ]; then
    if [ "${fork_tag[$tag]}" != "$sha" ]; then
      echo "::error::tag ${tag} exists in the fork at ${fork_tag[$tag]} and upstream at ${sha} — a release tag of the line collides with upstream's; move the line to its next major first (FORK.md, 'Release scheme')."
      exit 1
    fi
    continue
  fi
  refspecs+=("refs/tags/${tag}:refs/tags/${tag}")
  names+=("$tag")
  echo "tag ${tag}: new"
done < <(tags_of upstream)

if [ ${#refspecs[@]} -eq 0 ]; then
  echo "mirror is current"
  exit 0
fi
[ "$mode" = --check ] && exit 0
"${here}/git-push-as.sh" "$token" origin "${refspecs[@]}"
printf '%s\n' "${names[@]}" > "$pushed_file"
