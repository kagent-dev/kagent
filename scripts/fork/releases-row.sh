#!/usr/bin/env bash
# Write a stable release's row into FORK.md's Releases table (FORK.md,
# "Releases"):
#
#   scripts/fork/releases-row.sh <version>
#
# Everything the row says is read, nothing is typed: the commit, the promoted
# candidate and the previous stable from the tags, the Substrate release from
# the tag's Makefile, and the upstream pin, the digests and the run from the
# release's row of builds.md on the `ledger` branch, which the publish
# pipeline's `record` job appends once the artifacts are pushed. The row goes
# in at its place in version order; a version that already has a row is left
# as it is, so a re-run writes nothing twice. The caller has the tags and the
# `ledger` branch fetched (LEDGER_REF, default origin/ledger).
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
fork=FORK.md
version=${1#v}
ledger_ref=${LEDGER_REF:-origin/ledger}
repo_url=${REPO_URL:-https://github.com/giantswarm/kagent-upstream}
stable='^[0-9]+\.[0-9]+\.[0-9]+$'

[[ "$version" =~ $stable ]] || { echo "ERROR: ${1:-}: not a stable release, the Releases table holds stable releases only" >&2; exit 64; }
tag=v$version

if grep -qF "| \`${version}\` | \`${tag}\` " "$fork"; then
  echo "${fork} already has the row of ${version}"
  exit 0
fi

commit=$(git rev-parse --verify --quiet "${tag}^{commit}") || { echo "ERROR: no tag ${tag}" >&2; exit 1; }
build=$(git show "${ledger_ref}:builds.md" 2>/dev/null | grep -F "| \`${version}\` |" | tail -1) || true
[ -n "$build" ] || { echo "ERROR: builds.md on ${ledger_ref} has no row for ${version}: ${tag} is not published" >&2; exit 1; }

# builds.md: | published | version | commit | pin (date) | controller | ui | golang-adk | claude-harness | chart kagent | chart kagent-crds | run | codex-harness | omp-harness |
# (codex-harness and omp-harness are empty on the rows written before the line published them)
IFS='|' read -r _ published _ _ pin controller ui adk harness chart crds run codex omp _ <<<"$build"
trim() { local v=${1#"${1%%[![:space:]]*}"}; printf '%s' "${v%"${v##*[![:space:]]}"}"; }
published=$(trim "$published")
pin=$(trim "$pin")
run=$(trim "$run")
images="$(trim "$controller"), $(trim "$ui"), $(trim "$adk"), $(trim "$harness")"
codex=$(trim "${codex:-}")
[ -z "$codex" ] || images+=", ${codex}"
omp=$(trim "${omp:-}")
[ -z "$omp" ] || images+=", ${omp}"
charts="$(trim "$chart"), $(trim "$crds")"

candidate=$(git tag --points-at "$commit" -l "${tag}-rc.*" --sort=-v:refname | head -1)
previous=$(git tag -l 'v*' | sed -n 's/^v\([0-9]*\.[0-9]*\.[0-9]*\)$/\1/p' | { cat; echo "$version"; } | sort -uV |
  awk -v v="$version" '$0 == v { print p; exit } { p = $0 }')
substrate=$(git show "${tag}:Makefile" | sed -n 's/^SUBSTRATE_VERSION ?= *//p')

short=${commit:0:8}
what="\`${tag}\` (published ${published/ /T}Z, on \`${short}\`"
[ -n "$candidate" ] && what+=", promoted from \`${candidate}\`"
what+=": "
[ -n "$previous" ] && what+="\`v${previous}\` plus "
what+="the changes of its [release notes](${repo_url}/releases/tag/${tag}). Substrate \`${substrate}\`. Published by CircleCI, ${run})"
row="| \`${version}\` | ${what} | \`${short}\` | ${pin} | ${images} | ${charts} |"

# The table's rows are in version order: the new one goes after the last row
# of a lower version, which is the end of the table for the line's next stable.
awk -v row="$row" -v version="$version" '
  function newer(a, b,   x, y, i, n) {   # a > b, both X.Y.Z
    split(a, x, "."); split(b, y, ".")
    for (i = 1; i <= 3; i++) { if (x[i] + 0 != y[i] + 0) return x[i] + 0 > y[i] + 0 }
    return 0
  }
  { lines[NR] = $0 }
  /^### Releases/ { inside = 1 }
  inside && /^## / { inside = 0 }
  inside && /^\| `[0-9]+\.[0-9]+\.[0-9]+` \|/ {
    v = $2; gsub(/[` ]/, "", v)
    if (newer(version, v)) at = NR
  }
  inside && /^\| `/ { last = NR }
  END {
    if (!at) at = last
    if (!at) { print "ERROR: FORK.md has no Releases table" > "/dev/stderr"; exit 1 }
    for (i = 1; i <= NR; i++) { print lines[i]; if (i == at) print row }
  }
' "$fork" > "${fork}.new"
mv "${fork}.new" "$fork"
echo "${fork}: the Releases table carries ${version}"
