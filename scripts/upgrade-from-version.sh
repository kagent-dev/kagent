#!/usr/bin/env bash
# upgrade-from-version.sh — prints the version the upgrade test should upgrade
# FROM, derived from git release tags (vMAJOR.MINOR.PATCH):
#
#   - the latest release's patch minus one (e.g. latest 0.9.10 -> 0.9.9), when
#     the patch is >= 1 and that prior patch release actually exists;
#   - otherwise the latest release of the previous minor line (e.g. latest
#     0.9.0 -> the highest 0.8.x), since patch-1 does not exist.
#
# Output has no leading 'v'. Pre-release tags (e.g. v0.9.10-rc1) are ignored.
# Kept POSIX-bash-3.2 compatible so it also works on stock macOS.
set -euo pipefail

# Released versions, normalized and sorted ascending. Only clean X.Y.Z tags.
versions="$(git tag --list 'v[0-9]*' | sed 's/^v//' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -V)"
if [ -z "${versions}" ]; then
  echo "ERROR: no release tags (vMAJOR.MINOR.PATCH) found" >&2
  exit 1
fi

latest="$(printf '%s\n' "${versions}" | tail -1)"
major="${latest%%.*}"
rest="${latest#*.}"
minor="${rest%%.*}"
patch="${rest#*.}"

exists() { printf '%s\n' "${versions}" | grep -qx "$1"; }

# Same minor, patch - 1, when that release exists.
if [ "${patch}" -ge 1 ]; then
  candidate="${major}.${minor}.$((patch - 1))"
  if exists "${candidate}"; then
    echo "${candidate}"
    exit 0
  fi
fi

# Fallback: highest release strictly below ${major}.${minor}.0 — the latest
# patch of the previous minor line.
prev=""
while IFS= read -r v; do
  vmajor="${v%%.*}"
  vrest="${v#*.}"
  vminor="${vrest%%.*}"
  if [ "${vmajor}" -lt "${major}" ] || { [ "${vmajor}" -eq "${major}" ] && [ "${vminor}" -lt "${minor}" ]; }; then
    prev="${v}"
    break
  fi
done < <(printf '%s\n' "${versions}" | sort -rV)

if [ -n "${prev}" ]; then
  echo "${prev}"
  exit 0
fi

echo "ERROR: could not determine an upgrade-from version before ${latest}" >&2
exit 1
