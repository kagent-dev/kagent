#!/usr/bin/env bash
# prev-stable-version.sh — prints the latest released patch of the previous
# stable line: the highest vMAJOR.MINOR.PATCH tag on the newest
# release/vMAJOR.MINOR.x branch (e.g. release/v0.9.x -> 0.9.10). This is the
# rollback-window floor a contraction must stay compatible with.
#
# Uses `git ls-remote`, so it needs network to the remote but not the branch
# checked out locally. Output has no leading 'v'. Override the remote with REMOTE.
set -euo pipefail

remote="${REMOTE:-origin}"

# Newest release branch's MAJOR.MINOR (release/v0.9.x -> 0.9). Pre-release and
# non-release branches are ignored by the pattern.
minor="$(git ls-remote --heads "$remote" 'refs/heads/release/v*' 2>/dev/null \
  | sed -nE 's#.*refs/heads/release/v([0-9]+\.[0-9]+)\.x$#\1#p' \
  | sort -V | tail -1)"
if [ -z "${minor}" ]; then
  echo "ERROR: no release/vMAJOR.MINOR.x branch found on ${remote}" >&2
  exit 1
fi

# Highest clean vMINOR.PATCH tag on that line. The `$` anchor skips the
# annotated-tag deref entries (refs/tags/vX.Y.Z^{}).
esc="${minor//./\\.}"
latest="$(git ls-remote --tags "$remote" 2>/dev/null \
  | grep -oE "refs/tags/v${esc}\.[0-9]+$" \
  | sed 's#refs/tags/v##' | sort -V | tail -1)"
if [ -z "${latest}" ]; then
  echo "ERROR: no v${minor}.PATCH release tags found on ${remote}" >&2
  exit 1
fi

echo "${latest}"
