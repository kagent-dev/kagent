#!/usr/bin/env bash
# Append one table row to a file on the `ledger` branch (FORK.md, "Ledger"):
#   scripts/fork/ledger-append.sh builds.md '| … |'
# The branch is an orphan the workflows own; it is created on first use. A
# concurrent append is retried on a rejected push. LEDGER_TOKEN (default
# GITHUB_TOKEN) pushes; no workflow reacts to the ledger branch.
set -euo pipefail
file=$1
row=$2
token=${LEDGER_TOKEN:-${GITHUB_TOKEN:?LEDGER_TOKEN or GITHUB_TOKEN required}}
remote=${LEDGER_REMOTE_URL:-$(git remote get-url origin)}
here=$(cd "$(dirname "$0")" && pwd)

header() {
  case $1 in
    builds.md) cat <<'H'
# Builds

Every dev build and release published by Tag and Push, newest last (see FORK.md). Image
digests are the multi-arch index digests — what a `Harness` pins.

| published (UTC) | version | commit | upstream pin | controller | ui | golang-adk | claude-harness | chart kagent | chart kagent-crds | run |
|---|---|---|---|---|---|---|---|---|---|---|
H
    ;;
    re-pins.md) cat <<'H'
# Re-pins

Every re-pin of the consumed branch onto upstream main, newest last (see FORK.md). A re-pin
that stopped (conflict, failed checks) is a pull request against the consumed branch, not a row.

| date (UTC) | previous pin | new pin | consumed head | replayed | dropped | run |
|---|---|---|---|---|---|---|
H
    ;;
    *) printf '# %s\n\n' "$1" ;;
  esac
}

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
for attempt in 1 2 3 4 5; do
  rm -rf "$dir" && git init -q "$dir"
  (
    cd "$dir"
    git remote add origin "$remote"
    if git fetch -q origin ledger 2>/dev/null; then
      git checkout -q -B ledger FETCH_HEAD
    else
      git checkout -q --orphan ledger
      printf '# Ledger\n\nMachine-written records of the kagent line (see FORK.md on the consumed branch): builds.md — every published build with its digests; re-pins.md — every re-pin onto upstream main.\n' > README.md
    fi
    [ -f "$file" ] || header "$file" > "$file"
    printf '%s\n' "$row" >> "$file"
    git add -A
    git -c user.name="${LEDGER_GIT_NAME:-github-actions[bot]}" -c user.email="${LEDGER_GIT_EMAIL:-41898282+github-actions[bot]@users.noreply.github.com}" \
      commit -q -m "ledger: ${file}" -m "${row}"
    "$here/git-push-as.sh" "$token" origin HEAD:refs/heads/ledger
  ) && exit 0
  echo "ledger push rejected (attempt ${attempt}), retrying" >&2
  sleep $((attempt * 5))
done
echo "could not append to the ledger" >&2
exit 1
