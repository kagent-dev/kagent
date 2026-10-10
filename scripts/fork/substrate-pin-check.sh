#!/usr/bin/env bash
# A stable release of the line runs on a stable Substrate (FORK.md, "Release
# scheme"). `make substrate-pin-check` calls this with the two pins and the
# version being published:
#
#   substrate-pin-check.sh <chart pin> <module pin> [<release version>]
#
# The chart pin is the Makefile's SUBSTRATE_VERSION (the charts' substrate
# dependency and the worker image), the module pin the go/go.mod replace
# target's version (the ate-api contract the controller is compiled against),
# the release version the tag the pipeline publishes (`v1.6.1`,
# `v1.6.1-rc.1`) or empty for a dev build. A stable release (`vX.Y.Z`) whose
# chart or module pin carries a prerelease suffix is refused, naming the
# version, with exit 1: the Substrate release is promoted first, the pin moved
# to it and a new candidate cut. A candidate or a dev build passes whatever
# the pin: a candidate exists to prove a prerelease Substrate.
set -euo pipefail

if [ $# -lt 2 ] || [ $# -gt 3 ]; then
  echo "usage: $0 <chart pin> <module pin> [<release version>]" >&2
  exit 64
fi

chart=$1
module=$2
release=${3:-}
stable='^v?[0-9]+\.[0-9]+\.[0-9]+$'

if [[ ! "$release" =~ $stable ]]; then
  echo "${release:-dev build}: not a stable release, the Substrate pin is not checked (chart ${chart}, module ${module})"
  exit 0
fi

prerelease=()
[[ "$chart" =~ $stable ]] || prerelease+=("the charts' SUBSTRATE_VERSION ${chart}")
[[ "$module" =~ $stable ]] || prerelease+=("the go.mod replace target github.com/giantswarm/substrate ${module}")

if [ ${#prerelease[@]} -gt 0 ]; then
  printf -v named '%s, ' "${prerelease[@]}"
  echo "ERROR: ${release} is a stable release built on a Substrate prerelease: ${named%, }. A stable release runs on a stable Substrate: promote the Substrate release first, move the pin to it (Makefile SUBSTRATE_VERSION, go/go.mod replace) and cut a new candidate." >&2
  exit 1
fi

echo "${release} runs on Substrate ${chart} (module ${module})"
