#!/usr/bin/env bash
# Emit -X ldflags for agent runtime image digests baked into the controller binary.
#
# Required environment variables:
#   APP_IMG         Python agent runtime image ref (repo:tag)
#   GOLANG_ADK_IMG  Go agent runtime image ref (repo:tag)
#   GOLANG_ADK_FULL_IMG  Go agent full runtime image ref (repo:tag)
#
# Optional:
#   CONTAINER_RUNTIME  docker (default)

set -o errexit
set -o pipefail

CONTAINER_RUNTIME="${CONTAINER_RUNTIME:-docker}"
TRANSLATOR_PKG="github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"

: "${APP_IMG:?APP_IMG is required}"
: "${GOLANG_ADK_IMG:?GOLANG_ADK_IMG is required}"
: "${GOLANG_ADK_FULL_IMG:?GOLANG_ADK_FULL_IMG is required}"

image_digest() {
	"${CONTAINER_RUNTIME}" buildx imagetools inspect "$1" --format '{{.Manifest.Digest}}' 2>/dev/null || true
}

append_digest_ldflag() {
	local go_var=$1
	local image_ref=$2
	local digest
	digest="$(image_digest "${image_ref}")"
	if [[ -z "${digest}" ]]; then
		echo "error: could not resolve OCI digest for ${image_ref} (is it pushed to the registry?)" >&2
		exit 1
	fi
	printf ' -X %s.%s=%s' "${TRANSLATOR_PKG}" "${go_var}" "${digest}"
}

append_digest_ldflag "PythonADKImageDigest" "${APP_IMG}"
append_digest_ldflag "GoADKImageDigest" "${GOLANG_ADK_IMG}"
append_digest_ldflag "GoADKFullImageDigest" "${GOLANG_ADK_FULL_IMG}"
