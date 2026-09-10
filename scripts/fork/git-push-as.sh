#!/usr/bin/env bash
# Push as a given token: `scripts/fork/git-push-as.sh <token> <git push args…>`.
# The workflows use it to choose who pushes: the workflow's own GITHUB_TOKEN
# (a push that triggers no workflow and may not touch workflow files — right
# for the ledger) or the App's token (a push that does trigger workflows and
# may update .github/workflows — the consumed branch, the sync candidates and
# the mirror).
set -euo pipefail
token=$1
shift
auth=$(printf 'x-access-token:%s' "$token" | base64 -w0)
# Mask the encoded credential in the Actions log; outside Actions the line would
# print it, so it is only emitted where the runner interprets it.
[ -n "${GITHUB_ACTIONS:-}" ] && echo "::add-mask::${auth}"
exec git -c "http.https://github.com/.extraheader=AUTHORIZATION: basic ${auth}" push "$@"
