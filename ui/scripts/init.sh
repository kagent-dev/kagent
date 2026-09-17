#!/usr/bin/env bash
set -euo pipefail

# Create nginx temp directories
# These are required when running with readOnlyRootFilesystem: true
# The /tmp emptyDir volume is mounted empty at runtime, so we need to
# recreate the directory structure that was created during the Docker build
mkdir -p /tmp/nginx/client_temp \
         /tmp/nginx/proxy_temp \
         /tmp/nginx/fastcgi_temp \
         /tmp/nginx/uwsgi_temp \
         /tmp/nginx/scgi_temp \
         /tmp/kagent

# Runtime browser configuration.
#
# Vite inlines import.meta.env at BUILD time, so anything the chart configures
# per-deployment cannot be baked into the bundle — it would freeze the Helm
# values as of image build and silently ignore whatever the operator set. These
# values are rendered into a small script instead, which the document loads
# before the app and nginx serves with no-store.
#
# A script rather than a JSON document the app fetches, because the API base URL
# is needed by a module-level constant: there is no point early enough for an
# awaited fetch to have landed. Keep the keys in sync with `ui/src/env.ts`.
CONFIG_PATH=/tmp/kagent/env-config.js

API_BASE_URL="${KAGENT_API_BASE_URL:-/api}"
SSO_REDIRECT_PATH="${SSO_REDIRECT_PATH:-/oauth2/start}"
STREAM_TIMEOUT_MS="${KAGENT_STREAM_TIMEOUT_MS:-1800000}"
ENABLE_MOCK_UI="${ENABLE_MOCK_UI:-false}"

# A non-numeric timeout would abort every chat stream immediately, which looks
# like the backend hanging up rather than like a bad value. Fall back instead.
if ! [[ "$STREAM_TIMEOUT_MS" =~ ^[0-9]+$ ]]; then
  echo "init.sh: KAGENT_STREAM_TIMEOUT_MS='${STREAM_TIMEOUT_MS}' is not a number; using 1800000" >&2
  STREAM_TIMEOUT_MS=1800000
fi

# Escape backslashes first, then double quotes, so the value is safe inside a
# JSON string regardless of what the chart passed through. `<` is escaped as well
# because this lands inside a <script> element in the browser: a value containing
# markup could otherwise close the element early and inject what followed.
json_escape() {
  local value=${1//\\/\\\\}
  value=${value//\"/\\\"}
  printf '%s' "${value//</\\u003c}"
}

# Anything an installed extension reads, passed through verbatim and by name rather
# than by list: whatever the chart sets as `EXTENSION_*` arrives, so this image needs
# no change when an extension grows a setting, and this repository does not enumerate
# another product's configuration. The application ignores keys it has no use for.
#
# Built as JSON fragments here and spliced in below, because the heredoc cannot loop.
extension_json=""
while IFS='=' read -r name value; do
  extension_json+="  \"$(json_escape "$name")\": \"$(json_escape "$value")\",
"
done < <(env | grep '^EXTENSION_' | sort)

cat > "$CONFIG_PATH" <<EOF
window.environmentVariables = {
  "API_BASE_URL": "$(json_escape "$API_BASE_URL")",
  "SSO_REDIRECT_PATH": "$(json_escape "$SSO_REDIRECT_PATH")",
  "STREAM_TIMEOUT_MS": "$STREAM_TIMEOUT_MS",
${extension_json}  "ENABLE_MOCK_UI": "$(json_escape "$ENABLE_MOCK_UI")"
};
EOF

# nginx is the only process in this container, so it runs as PID 1 directly
# instead of under a process manager.
exec nginx -g "daemon off;"
