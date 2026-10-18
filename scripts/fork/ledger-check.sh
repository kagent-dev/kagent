#!/usr/bin/env bash
# The release ledger never trails the code (FORK.md, "Releases" and "Which
# kagent are we running"). Two checks, both read FORK.md:
#
#   scripts/fork/ledger-check.sh pin                # every pull request and push
#   scripts/fork/ledger-check.sh releases <version> # the publish pipeline
#
# pin: FORK.md's "Substrate version the pin runs with" row opens with the
# Makefile's SUBSTRATE_VERSION and its "Substrate the pin runs with" row names
# the go.mod replace target's version, so a change of the pin without its two
# rows is red.
#
# releases <version>: every stable release tag below <version> (from
# LEDGER_FROM on) has a row in the Releases table, so a promote whose
# predecessor's row is missing is refused before an image is built. The new
# release's own row is written after its publish: the pipeline's `record` job
# proposes it as a pull request (scripts/fork/releases-row-pr.sh). A row
# counts when it is in the checkout's FORK.md, in the consumed branch's
# (CONSUMED_BRANCH, default giantswarm, fetched from origin), or in the FORK.md
# of that pull request's branch (fork-ledger/releases-<version> on origin): a
# candidate cut before the row's pull request merged is promoted all the same,
# and a row still waiting in its pull request never refuses the next stable.
# A refusal names where the row's pull request is found (REPO_URL).
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
fork=FORK.md
ledger_from=${LEDGER_FROM:-1.6.0}
stable='^v?[0-9]+\.[0-9]+\.[0-9]+$'
problems=()

check_pin() {
  local chart module pins
  pins=$(make -s substrate-pin 2>/dev/null)
  chart=$(sed -n 's/^SUBSTRATE_VERSION=//p' <<<"$pins")
  module=$(sed -n 's/^SUBSTRATE_MODULE_VERSION=//p' <<<"$pins")
  [ -n "$chart" ] && [ -n "$module" ] || { echo "ERROR: cannot read the Substrate pin from the Makefile" >&2; exit 1; }
  grep -qF "| Substrate version the pin runs with | \`${chart}\` " "$fork" ||
    problems+=("the \"Substrate version the pin runs with\" row of ${fork} does not open with the charts' SUBSTRATE_VERSION \`${chart}\`")
  grep -F "| Substrate the pin runs with |" "$fork" | grep -qF "github.com/giantswarm/substrate ${module}" ||
    problems+=("the \"Substrate the pin runs with\" row of ${fork} does not name the go.mod replace target github.com/giantswarm/substrate ${module}")
}

check_releases() {
  local release=${1#v} tag version row proposed prs ledgers consumed=${CONSUMED_BRANCH:-giantswarm}
  local repo_url=${REPO_URL:-https://github.com/giantswarm/kagent-upstream}
  [[ "$release" =~ $stable ]] || { echo "${1:-dev build}: not a stable release, the Releases table is not checked"; return; }
  ledgers=$(cat "$fork")
  if git fetch -q origin "$consumed" 2>/dev/null; then
    ledgers+=$'\n'$(git show FETCH_HEAD:"$fork" 2>/dev/null || true)
  else
    echo "WARNING: cannot fetch ${consumed}: only the checkout's ${fork} is read" >&2
  fi
  while read -r tag; do
    version=${tag#v}
    [[ "$version" =~ $stable ]] || continue
    [ "$(printf '%s\n%s\n' "$ledger_from" "$version" | sort -V | head -1)" = "$ledger_from" ] || continue
    [ "$(printf '%s\n%s\n' "$version" "$release" | sort -V | head -1)" = "$version" ] && [ "$version" != "$release" ] || continue
    row="| \`${version}\` | \`v${version}\` "
    grep -qF "$row" <<<"$ledgers" && continue
    proposed=fork-ledger/releases-${version}
    prs="${repo_url}/pulls?q=is%3Apr+head%3A${proposed//\//%2F}"
    if git fetch -q origin "refs/heads/${proposed}" 2>/dev/null && git show FETCH_HEAD:"$fork" 2>/dev/null | grep -qF "$row"; then
      echo "the row of ${version} is not on ${consumed} yet: it waits in the pull request of ${proposed} (${prs}), which counts; merge it"
      continue
    fi
    problems+=("the Releases table of ${fork} has no row for ${version}: merge the pull request \"docs(fork): the Releases table carries ${version}\" the publish of v${version} opened (${prs}), or run scripts/fork/releases-row.sh ${version}")
  done < <(git tag -l 'v*')
}

case ${1:-} in
  pin) check_pin ;;
  releases) check_releases "${2:-}" ;;
  *) echo "usage: $0 pin | releases <version>" >&2; exit 64 ;;
esac

if [ ${#problems[@]} -gt 0 ]; then
  printf 'ERROR: %s\n' "${problems[@]}" >&2
  echo "Write the rows in the same pull request that changes the pin, or before promoting the next release." >&2
  exit 1
fi
echo "ledger ok (${1})"
