#!/usr/bin/env bash

# Puts the upstream sources of the telemetry contract at the commits pinned in
# telemetry/versions.env into telemetry/deps, where the registry manifest,
# semconv-check and live-check read them from disk: the registries the manifest
# depends on and the shared Weaver policies. Every pin is a commit, and a
# fetched checkout is compared with it. A registry a checkout depends on by git
# URL is pointed at the checkout of the same pin, so Weaver clones nothing; one
# that names another commit, or a release tag other than the one the pin
# declares, is refused, since the contract and its dependencies must agree on
# the core conventions. A checkout at its pin is reused without touching the
# network; only a moved pin fetches again.

set -o errexit -o nounset -o pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "${ROOT}"

# shellcheck source=telemetry/versions.env
source telemetry/versions.env

DEPS=telemetry/deps
SOURCES=("${SEMCONV_CORE_REGISTRY}" "${SEMCONV_GENAI_REGISTRY}" "${WEAVER_PACKAGES}")

COMMIT='^[0-9a-f]{40}$'
# A dependency line of a registry manifest: `registry_path: <git url>@<ref>[<sub-folder>]`.
GIT_DEPENDENCY='^([[:space:]]*registry_path:[[:space:]]*)(https://[^[:space:]@]+\.git)@([^[[:space:]]+)\[([^]]+)\][[:space:]]*$'

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# checkout_of <git url>@<commit> names the directory a pinned source is put in.
checkout_of() {
  local url="${1%@*}"
  echo "${DEPS}/$(basename "${url}" .git)"
}

# pin_of <git url> prints the pinned source of that repository, or nothing.
pin_of() {
  local source
  for source in "${SOURCES[@]}"; do
    if [[ "${source%@*}" == "$1" ]]; then
      echo "${source}"
      return
    fi
  done
}

# tag_of <git url>@<commit> prints the release tag other registries may name
# for that pin's commit, or nothing.
tag_of() {
  if [[ "$1" == "${SEMCONV_CORE_REGISTRY}" ]]; then
    echo "${SEMCONV_CORE_REGISTRY_TAG:-}"
  fi
}

# tag_commit <git url> <tag> prints the commit the tag names at the remote.
tag_commit() {
  local refs peeled
  refs="$(git ls-remote --tags "$1" "refs/tags/$2" "refs/tags/$2^{}")"
  [[ -n "${refs}" ]] || fail "$1 has no tag $2"
  # An annotated tag lists its own object and, peeled, the commit.
  peeled="$(grep -F '^{}' <<<"${refs}" || true)"
  if [[ -n "${peeled}" ]]; then
    awk 'NR == 1 { print $1 }' <<<"${peeled}"
  else
    awk 'NR == 1 { print $1 }' <<<"${refs}"
  fi
}

# localise <manifest> points each git dependency of a fetched registry's
# manifest at the checkout of the same pin.
localise() {
  local manifest="$1" line url ref pin tag
  local out="${manifest}.localised"
  while IFS= read -r line || [[ -n "${line}" ]]; do
    if [[ "${line}" =~ ${GIT_DEPENDENCY} ]]; then
      url="${BASH_REMATCH[2]}"
      ref="${BASH_REMATCH[3]}"
      pin="$(pin_of "${url}")"
      [[ -n "${pin}" ]] || fail "${manifest} depends on ${url}@${ref}, which telemetry/versions.env does not pin: add the pin"
      tag="$(tag_of "${pin}")"
      if [[ "${ref}" != "${pin##*@}" && ( -z "${tag}" || "${ref}" != "${tag}" ) ]]; then
        fail "${manifest} depends on ${url}@${ref}; the contract pins ${pin##*@}${tag:+ (${tag})}: align the pins"
      fi
      line="${BASH_REMATCH[1]}$(checkout_of "${pin}")/${BASH_REMATCH[4]}"
    fi
    printf '%s\n' "${line}" >>"${out}"
  done <"${manifest}"
  mv "${out}" "${manifest}"
}

# materialise <git url>@<commit> puts that commit of the repository into its
# checkout, unless it is there already.
materialise() {
  local source="$1" url="${1%@*}" commit="${1##*@}" tag dir name manifest fetched named
  [[ "${commit}" =~ ${COMMIT} ]] || fail "${source} is not pinned by commit: telemetry/versions.env pins a full commit hash"
  dir="$(checkout_of "${source}")"
  name="$(basename "${dir}")"
  tag="$(tag_of "${source}")"

  if [[ -f "${dir}/.pin" && "$(<"${dir}/.pin")" == "${source} ${tag}" ]]; then
    echo "${name}: ${commit:0:12} present in ${dir}, no fetch"
    return
  fi

  echo "${name}: fetching ${commit:0:12} from ${url} into ${dir}"
  rm -rf "${dir}"
  git init --quiet "${dir}"
  git -C "${dir}" fetch --quiet --depth 1 "${url}" "${commit}"
  fetched="$(git -C "${dir}" rev-parse FETCH_HEAD)"
  [[ "${fetched}" == "${commit}" ]] || fail "${name}: fetched ${fetched}, the pin is ${commit}"
  git -C "${dir}" checkout --quiet "${commit}"
  if [[ -n "${tag}" ]]; then
    named="$(tag_commit "${url}" "${tag}")"
    [[ "${named}" == "${commit}" ]] || fail "${name}: tag ${tag} names ${named}, the pin is ${commit}: align the pin and its tag in telemetry/versions.env"
  fi
  rm -rf "${dir}/.git"
  while IFS= read -r manifest; do
    localise "${manifest}"
  done < <(find "${dir}" -name registry_manifest.yaml -o -name manifest.yaml)
  echo "${source} ${tag}" >"${dir}/.pin"
}

for source in "${SOURCES[@]}"; do
  materialise "${source}"
done
