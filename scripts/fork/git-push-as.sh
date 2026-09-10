#!/usr/bin/env bash
# Push as a given token: `scripts/fork/git-push-as.sh <token> <git push args…>`.
# The workflows use it to choose who pushes: the workflow's own GITHUB_TOKEN
# (a push that triggers no workflow — right for the mirror `main` and the
# ledger) or the bot token (a push that does — right for the consumed branch
# and the sync candidates, so CI Build, Scan images and Tag and Push run).
set -euo pipefail
token=$1
shift
auth=$(printf 'x-access-token:%s' "$token" | base64 -w0)
echo "::add-mask::${auth}"
exec git -c "http.https://github.com/.extraheader=AUTHORIZATION: basic ${auth}" push "$@"
