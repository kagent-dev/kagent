#!/usr/bin/env bash

# Puts the contract's upstream sources at the commits pinned in
# telemetry/versions.env into telemetry/deps, where the registry manifest,
# semconv-check and live-check read them; a source at its pin is reused. The
# GenAI registry is built into a Weaver package, which resolves its own
# dependency on the core registry once, when it is packaged.

set -o errexit -o nounset -o pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "${ROOT}"

# shellcheck source=telemetry/versions.env
source telemetry/versions.env

DEPS=telemetry/deps

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# dir_of <git url>@<commit> names the directory a pinned source is put in.
dir_of() {
  echo "${DEPS}/$(basename "${1%@*}" .git)"
}

# commit_of <git url>@<commit> prints the commit, refusing any other ref.
commit_of() {
  local commit="${1##*@}"
  [[ "${commit}" =~ ^[0-9a-f]{40}$ ]] || fail "$1 is not pinned by commit"
  echo "${commit}"
}

# present <dir> <git url>@<commit> succeeds when the directory holds that pin.
present() {
  [[ -f "$1/.pin" && "$(<"$1/.pin")" == "$2" ]]
}

# checkout <git url>@<commit> fetches that commit into its directory.
checkout() {
  local url="${1%@*}" commit dir
  commit="$(commit_of "$1")"
  dir="$(dir_of "$1")"
  if present "${dir}" "$1"; then
    echo "$(basename "${dir}"): ${commit:0:12} present"
    return
  fi
  echo "$(basename "${dir}"): fetching ${commit:0:12}"
  rm -rf "${dir}"
  git init --quiet "${dir}"
  git -C "${dir}" fetch --quiet --depth 1 "${url}" "${commit}"
  [[ "$(git -C "${dir}" rev-parse FETCH_HEAD)" == "${commit}" ]] || fail "$(basename "${dir}"): fetched $(git -C "${dir}" rev-parse FETCH_HEAD), the pin is ${commit}"
  git -C "${dir}" checkout --quiet "${commit}"
  rm -rf "${dir}/.git"
  echo "$1" >"${dir}/.pin"
}

# package <git url>@<commit> builds that commit's registry into a Weaver package; a Weaver bump rebuilds it.
package() {
  local commit dir
  commit="$(commit_of "$1")"
  dir="$(dir_of "$1")"
  if present "${dir}" "$1 ${WEAVER_VERSION}"; then
    echo "$(basename "${dir}"): ${commit:0:12} present"
    return
  fi
  echo "$(basename "${dir}"): packaging ${commit:0:12}"
  rm -rf "${dir}"
  telemetry/weaver.sh registry package -r "$1[model]" --v2 --skip-policies --resolved-registry-uri resolved.yaml -o "${dir}" --quiet
  echo "$1 ${WEAVER_VERSION}" >"${dir}/.pin"
}

# The core registry stays a checkout: as a v2 package, its excluded GenAI spans collide with the imports.
checkout "${SEMCONV_CORE_REGISTRY}"
checkout "${WEAVER_PACKAGES}"
package "${SEMCONV_GENAI_REGISTRY}"

# The GenAI package was built on the core release its manifest names, which
# must be the one the contract pins.
core="$(sed -n 's/^schema_url: *//p' "$(dir_of "${SEMCONV_CORE_REGISTRY}")/model/manifest.yaml")"
genai_core="$(sed -n 's/^- *schema_url: *//p' "$(dir_of "${SEMCONV_GENAI_REGISTRY}")/manifest.yaml")"
[[ "${genai_core}" == "${core}" ]] || fail "the GenAI registry is built on ${genai_core}, the contract pins ${core}: align the pins"
