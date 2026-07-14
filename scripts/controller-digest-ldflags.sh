#!/usr/bin/env bash
# Emit -X ldflags for agent runtime image digests baked into the controller binary.
#
# Required environment variables:
#   APP_IMG         Python agent runtime image ref (repo:tag)
#   APP_FULL_IMG    Python agent full runtime image ref (repo:tag)
#   GOLANG_ADK_IMG  Go agent runtime image ref (repo:tag)
#   GOLANG_ADK_FULL_IMG  Go agent full runtime image ref (repo:tag)
#   ACP_SANDBOX_OPENCLAW_IMG  acp-sandbox openclaw workload image ref (repo:tag)
#   ACP_SANDBOX_HERMES_IMG    acp-sandbox hermes workload image ref (repo:tag)
#
# Optional:
#   CONTAINER_RUNTIME  docker (default)

set -o errexit
set -o pipefail

CONTAINER_RUNTIME="${CONTAINER_RUNTIME:-docker}"
TRANSLATOR_PKG="github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
SUBSTRATE_PKG="github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
MANIFEST_ACCEPT="application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"
# Set DIGEST_CURL_INSECURE=true to skip TLS verification when resolving digests
# against a self-signed local registry.
CURL_INSECURE_ARG=""
if [[ "${DIGEST_CURL_INSECURE:-false}" == "true" ]]; then
	CURL_INSECURE_ARG="-k"
fi

: "${APP_IMG:?APP_IMG is required}"
: "${APP_FULL_IMG:?APP_FULL_IMG is required}"
: "${GOLANG_ADK_IMG:?GOLANG_ADK_IMG is required}"
: "${GOLANG_ADK_FULL_IMG:?GOLANG_ADK_FULL_IMG is required}"
: "${ACP_SANDBOX_OPENCLAW_IMG:?ACP_SANDBOX_OPENCLAW_IMG is required}"
: "${ACP_SANDBOX_HERMES_IMG:?ACP_SANDBOX_HERMES_IMG is required}"

registry_manifest_digest() {
	local image_ref=$1
	local registry remainder repository tag scheme headers

	if [[ "${image_ref}" == *@sha256:* ]]; then
		printf '%s\n' "${image_ref##*@}"
		return 0
	fi

	registry="${image_ref%%/*}"
	remainder="${image_ref#*/}"
	if [[ "${registry}" == "${image_ref}" || "${remainder}" != *:* ]]; then
		return 1
	fi

	repository="${remainder%:*}"
	tag="${remainder##*:}"
	scheme="https"
	if [[ "${registry}" == localhost:* || "${registry}" == 127.* || "${registry}" == "[::1]:"* ]]; then
		scheme="http"
	fi

	if ! command -v curl >/dev/null 2>&1; then
		return 1
	fi

	if ! headers="$(
		curl -fsSI ${CURL_INSECURE_ARG} \
			-H "Accept: ${MANIFEST_ACCEPT}" \
			"${scheme}://${registry}/v2/${repository}/manifests/${tag}"
	)"; then
		return 1
	fi

	printf '%s\n' "${headers}" | awk 'tolower($1) == "docker-content-digest:" { gsub("\r", "", $2); print $2; exit }'
}

image_digest() {
	local image_ref=$1
	local digest output status

	if digest="$(registry_manifest_digest "${image_ref}")" && [[ -n "${digest}" ]]; then
		printf '%s\n' "${digest}"
		return 0
	fi

	if output="$("${CONTAINER_RUNTIME}" buildx imagetools inspect "${image_ref}" 2>&1)"; then
		digest="$(printf '%s\n' "${output}" | awk '/^Digest:[[:space:]]+sha256:/ { print $2; exit }')"
		if [[ -n "${digest}" ]]; then
			printf '%s\n' "${digest}"
			return 0
		fi
		echo "error: could not find OCI digest in imagetools output for ${image_ref}" >&2
		printf '%s\n' "${output}" >&2
		return 1
	fi

	status=$?
	echo "error: failed to inspect OCI digest for ${image_ref}" >&2
	printf '%s\n' "${output}" >&2
	return "${status}"
}

append_digest_ldflag() {
	local pkg=$1
	local go_var=$2
	local image_ref=$3
	local digest
	digest="$(image_digest "${image_ref}")"
	if [[ -z "${digest}" ]]; then
		echo "error: could not resolve OCI digest for ${image_ref} (is it pushed to the registry?)" >&2
		exit 1
	fi
	printf ' -X %s.%s=%s' "${pkg}" "${go_var}" "${digest}"
}

append_digest_ldflag "${TRANSLATOR_PKG}" "PythonADKImageDigest" "${APP_IMG}"
append_digest_ldflag "${TRANSLATOR_PKG}" "PythonADKFullImageDigest" "${APP_FULL_IMG}"
append_digest_ldflag "${TRANSLATOR_PKG}" "GoADKImageDigest" "${GOLANG_ADK_IMG}"
append_digest_ldflag "${TRANSLATOR_PKG}" "GoADKFullImageDigest" "${GOLANG_ADK_FULL_IMG}"
append_digest_ldflag "${SUBSTRATE_PKG}" "AcpSandboxOpenClawImageDigest" "${ACP_SANDBOX_OPENCLAW_IMG}"
append_digest_ldflag "${SUBSTRATE_PKG}" "AcpSandboxHermesImageDigest" "${ACP_SANDBOX_HERMES_IMG}"
