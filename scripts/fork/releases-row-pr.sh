#!/usr/bin/env bash
# Open the pull request that writes a published stable release's row into
# FORK.md's Releases table on the consumed branch (FORK.md, "Releases"):
#
#   scripts/fork/releases-row-pr.sh <version>
#
# The publish pipeline's `record` job runs it on a stable tag, after the
# release's builds.md row is on the `ledger` branch: the row is written from
# that (scripts/fork/releases-row.sh) on a fresh clone of the consumed branch,
# committed on `fork-ledger/releases-<version>` and proposed as a `docs(fork)`
# pull request, which cuts no candidate. The next stable's gate counts the row
# on the consumed branch or on this pull request's branch, so a row still
# waiting for its merge refuses no promotion. A re-run finds the row or the
# open pull request and adds nothing. GH_TOKEN pushes and opens the pull request; GH_REPO names the
# repository.
set -euo pipefail

version=${1#v}
repo=${GH_REPO:?GH_REPO required}
token=${GH_TOKEN:?GH_TOKEN required}
base=${BASE_BRANCH:-giantswarm}
branch=fork-ledger/releases-${version}
title="docs(fork): the Releases table carries ${version}"
here=$(cd "$(dirname "$0")" && pwd)

# Only a pull request from this repository's own branch counts: a fork's
# branch of the same name proposes nothing the pipeline wrote.
open=$(gh pr list --repo "$repo" --head "$branch" --state open --json url,isCrossRepository \
  --jq '[.[] | select(.isCrossRepository == false)][0].url // empty')
if [ -n "$open" ]; then
  echo "the row of ${version} is proposed in ${open}"
  exit 0
fi

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
git clone -q --filter=blob:none --branch "$base" "https://github.com/${repo}.git" "$dir"
cd "$dir"
git fetch -q --tags origin
git fetch -q origin ledger:refs/remotes/origin/ledger
LEDGER_REF=origin/ledger REPO_URL="https://github.com/${repo}" "$here/releases-row.sh" "$version"
if git diff --quiet -- FORK.md; then
  echo "${base} already carries the row of ${version}"
  exit 0
fi

name=${LEDGER_GIT_NAME:-github-actions[bot]}
email=${LEDGER_GIT_EMAIL:-41898282+github-actions[bot]@users.noreply.github.com}
git checkout -q -b "$branch"
git -c user.name="$name" -c user.email="$email" commit -q -s -m "$title" -- FORK.md
"$here/git-push-as.sh" "$token" origin "HEAD:refs/heads/${branch}"
gh pr create --repo "$repo" --base "$base" --head "$branch" --title "$title" --body "$(cat <<EOF
The Releases row of \`v${version}\`, written by the publish pipeline from the release's builds.md row on the \`ledger\` branch, its tags and its Makefile (\`scripts/fork/releases-row.sh\`).

The next stable's gate (\`.github/release-gate.custom.sh\`) counts this row on \`${base}\` or on this pull request's branch, so the next promotion does not wait for this merge. Merge it to put the row on \`${base}\`; close it only with its branch deleted.
EOF
)"
