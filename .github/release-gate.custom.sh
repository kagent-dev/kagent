#!/usr/bin/env bash
# The line's stable gate (FORK.md, "Release scheme"): a stable release runs on a
# stable Substrate and its predecessors have their rows in the Releases table.
#
#   .github/release-gate.custom.sh <version>
#
# Two callers, the same checks:
#
# - The Auto Release workflow's stable promotion runs it on the candidate's
#   checkout with the version about to be tagged, before the tag and the
#   GitHub release are created: a refusal cuts neither.
# - The publish pipeline's `check-substrate-pin` job runs it with the tag it
#   publishes, ahead of every image build: the second guard. A candidate
#   (`vX.Y.Z-rc.N`) or a dev build (empty) passes whatever the pin.
#
# The tags are read from the checkout; a caller fetches them first.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
release=${1:-}

make -s substrate-pin-check RELEASE_VERSION="$release"
scripts/fork/ledger-check.sh releases "$release"
