#!/usr/bin/env bash
# upgrade-from-version.sh — prints the version the upgrade test upgrades FROM:
# the latest published tag reachable from HEAD (the adjacent previous release in
# the current line), including prereleases. Output has no leading 'v'.
set -euo pipefail

tag="$(git describe --tags --abbrev=0 --match='v[0-9]*' 2>/dev/null || true)"
if [ -z "${tag}" ]; then
  echo "ERROR: no release tag reachable from HEAD (fetch tags?)" >&2
  exit 1
fi
echo "${tag#v}"
