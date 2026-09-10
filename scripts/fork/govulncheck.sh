#!/usr/bin/env bash
# Scan the Go module graph with govulncheck and fail on any finding that is
# reached from kagent's code, unless .govulncheck-ignore carries a time-boxed
# ignore for it (FORK.md, "CI and security"). govulncheck has no ignore file of
# its own; the filter here gives the fork the same discipline as .nancy-ignore:
#
#   GO-YYYY-NNNN until=YYYY-MM-DD # <module@version>: why unresolvable, what to track
#
# An expired entry is a failure ("re-review"), never a silent pass.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
ignore_file="${root}/.govulncheck-ignore"
cd "${root}/go"

go run golang.org/x/vuln/cmd/govulncheck@latest -format json ./... > govulncheck.json

# Findings with a call trace are reached from the module's own code; the rest
# (imported or required only) is informational, as in govulncheck's own verdict.
mapfile -t found < <(jq -r 'select(.finding != null) | .finding | select(.trace[0].function != null) | .osv' govulncheck.json | sort -u)

describe() {
  jq -r --arg id "$1" 'select(.osv != null) | .osv | select(.id == $id) | "\(.id): \(.summary) — \(.affected[0].package.name) (fixed: \([.affected[0].ranges[0].events[]? | .fixed // empty] | first // "none"))"' govulncheck.json
}

today=$(date -u +%F)
failed=0
for id in "${found[@]}"; do
  entry=$(grep -E "^${id}[[:space:]]" "$ignore_file" 2>/dev/null || true)
  if [ -z "$entry" ]; then
    echo "::error::${id} reached from kagent code and not ignored — $(describe "$id")"
    failed=1
    continue
  fi
  until=$(sed -n 's/.*until=\([0-9-]*\).*/\1/p' <<< "$entry")
  if [ -z "$until" ] || [[ "$until" < "$today" ]]; then
    echo "::error::${id} ignore expired (${until:-no until=}) — re-review: $(describe "$id")"
    failed=1
    continue
  fi
  echo "::notice::${id} ignored until ${until}: ${entry#*# }"
done
echo "govulncheck: ${#found[@]} finding(s) reached from kagent code, $(jq -r 'select(.finding != null) | .finding | select(.trace[0].function == null) | .osv' govulncheck.json | sort -u | wc -l) informational"
exit "$failed"
