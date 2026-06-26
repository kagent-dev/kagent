#!/usr/bin/env bash
# prev-stable-version.sh — prints the latest released patch of the stable line
# immediately BELOW the line currently being built: the highest
# vMAJOR.MINOR.PATCH tag on the newest release/vMAJOR.MINOR.x branch whose
# MAJOR.MINOR is strictly less than the current line. This is the rollback-window
# floor a contraction must stay compatible with.
#
# The current line comes from the base/target branch:
#   - release/vX.Y.x  -> current line is X.Y, so this resolves to the newest
#                        release line below X.Y (release/v0.9.x -> 0.8.x).
#   - main (or any non-release branch) -> the unreleased next minor, which sorts
#                        above every release line, so this resolves to the newest
#                        release line overall (-> 0.9.x).
# Source order for the current ref: CURRENT_REF override, GITHUB_BASE_REF (PR
# target), GITHUB_REF_NAME (push), then the checked-out branch.
#
# Prints nothing and exits 0 when no stable line exists below the current one
# (e.g. building the oldest release line), so the caller can skip that target.
# Uses `git ls-remote`, so it needs network to the remote but not the branch
# checked out locally. Output has no leading 'v'. Override the remote with REMOTE.
set -euo pipefail

remote="${REMOTE:-origin}"

# Current line MAJOR.MINOR, or empty for main / any non-release line.
current_ref="${CURRENT_REF:-${GITHUB_BASE_REF:-${GITHUB_REF_NAME:-$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)}}}"
current_minor=""
if [[ "${current_ref}" =~ ^release/v([0-9]+\.[0-9]+)\.x$ ]]; then
  current_minor="${BASH_REMATCH[1]}"
fi

# All release lines on the remote, ascending by version (MAJOR.MINOR only).
lines="$(git ls-remote --heads "$remote" 'refs/heads/release/v*' 2>/dev/null \
  | sed -nE 's#.*refs/heads/release/v([0-9]+\.[0-9]+)\.x$#\1#p' \
  | sort -V)"
if [ -z "${lines}" ]; then
  echo "ERROR: no release/vMAJOR.MINOR.x branch found on ${remote}" >&2
  exit 1
fi

# ver_lt A B -> success when A < B by version sort.
ver_lt() { [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -1)" = "$1" ]; }

# Highest line strictly below the current line. With no current_minor (main /
# next), every line qualifies, so this lands on the newest line overall.
prev_minor=""
for l in ${lines}; do
  if [ -z "${current_minor}" ] || ver_lt "$l" "${current_minor}"; then
    prev_minor="$l"
  fi
done
if [ -z "${prev_minor}" ]; then
  # No stable line below the current one; let the caller skip that target.
  exit 0
fi

esc="${prev_minor//./\\.}"
latest="$(git ls-remote --tags "$remote" 2>/dev/null \
  | grep -oE "refs/tags/v${esc}\.[0-9]+$" \
  | sed 's#refs/tags/v##' | sort -V | tail -1)"
if [ -z "${latest}" ]; then
  echo "ERROR: no v${prev_minor}.PATCH release tags found on ${remote} (fetch tags?)" >&2
  exit 1
fi

echo "${latest}"
