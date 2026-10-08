#!/usr/bin/env bash

# Puts the upstream sources of the telemetry contract at the refs pinned in
# telemetry/versions.env into telemetry/deps, where the registry manifest and
# semconv-check read them from disk: the registries the manifest depends on and
# the shared Weaver policies. A registry a checkout depends on by git URL is
# pointed at the checkout of that same pin, so Weaver clones nothing; one pinned
# to another ref is refused, since the contract and its dependencies must agree
# on the core conventions. A checkout at the pinned ref is reused without
# touching the network; only a moved pin fetches again.

set -o errexit -o nounset -o pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "${ROOT}"

# shellcheck source=telemetry/versions.env
source telemetry/versions.env

DEPS=telemetry/deps
SOURCES=("${SEMCONV_CORE_REGISTRY}" "${SEMCONV_GENAI_REGISTRY}" "${WEAVER_PACKAGES}")

# A dependency line of a registry manifest: `registry_path: <git url>@<ref>[<sub-folder>]`.
GIT_DEPENDENCY='^([[:space:]]*registry_path:[[:space:]]*)(https://[^[:space:]]+\.git@[^[[:space:]]+)\[([^]]+)\][[:space:]]*$'

# checkout_of <git url>@<ref> names the directory a pinned source is put in.
checkout_of() {
  local url="${1%@*}"
  echo "${DEPS}/$(basename "${url}" .git)"
}

# pinned <git url>@<ref> succeeds when that source is one of the pins.
pinned() {
  local source
  for source in "${SOURCES[@]}"; do
    [[ "${source}" == "$1" ]] && return 0
  done
  return 1
}

# localise <manifest> points each git dependency of a fetched registry's
# manifest at the checkout of the same pin.
localise() {
  local manifest="$1" line
  local out="${manifest}.localised"
  while IFS= read -r line || [[ -n "${line}" ]]; do
    if [[ "${line}" =~ ${GIT_DEPENDENCY} ]]; then
      if ! pinned "${BASH_REMATCH[2]}"; then
        echo "FAIL: ${manifest} depends on ${BASH_REMATCH[2]}, which telemetry/versions.env does not pin: align the pins." >&2
        exit 1
      fi
      line="${BASH_REMATCH[1]}$(checkout_of "${BASH_REMATCH[2]}")/${BASH_REMATCH[3]}"
    fi
    printf '%s\n' "${line}" >>"${out}"
  done <"${manifest}"
  mv "${out}" "${manifest}"
}

# materialise <git url>@<ref> puts that ref of the repository into its checkout,
# unless it is there already.
materialise() {
  local source="$1" url="${1%@*}" ref="${1##*@}" dir name manifest
  dir="$(checkout_of "${source}")"
  name="$(basename "${dir}")"

  if [[ -f "${dir}/.pin" && "$(<"${dir}/.pin")" == "${source}" ]]; then
    echo "${name}: ${ref} present in ${dir}, no fetch"
    return
  fi

  echo "${name}: fetching ${ref} from ${url} into ${dir}"
  rm -rf "${dir}"
  git init --quiet "${dir}"
  git -C "${dir}" fetch --quiet --depth 1 "${url}" "${ref}"
  git -C "${dir}" checkout --quiet FETCH_HEAD
  rm -rf "${dir}/.git"
  while IFS= read -r manifest; do
    localise "${manifest}"
  done < <(find "${dir}" -name registry_manifest.yaml -o -name manifest.yaml)
  echo "${source}" >"${dir}/.pin"
}

for source in "${SOURCES[@]}"; do
  materialise "${source}"
done
