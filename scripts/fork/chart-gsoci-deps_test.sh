#!/usr/bin/env bash
# Tests of scripts/fork/chart-gsoci-deps.sh against a copy of this checkout's
# charts: the published charts' dependencies build with ghcr.io out of reach.
# ghcr.io is cut off by a proxy that answers nothing; every other host is
# reached directly (NO_PROXY). The copy as upstream ships it must fail that
# build — the test proves the cut — and the copy the script edited must pass
# it, reference no ghcr.io, and render no object of a dropped subchart.
#
#   scripts/fork/chart-gsoci-deps_test.sh
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }
pass() { echo "ok: $*"; }

# Chart.yaml from Chart-template.yaml the way `make helm-version` renders it,
# then the dependency build, ghcr.io behind the dead proxy. Go's proxy
# matching has no negation, so the hosts reached directly are named: the
# oauth2-proxy chart repository and its downloads.
build() {
  local helm=$1
  for chart in kagent kagent-crds tools/grafana-mcp; do
    VERSION=0.0.0-test KMCP_VERSION=0.4.0 envsubst < "$helm/$chart/Chart-template.yaml" > "$helm/$chart/Chart.yaml"
  done
  local direct=github.io,github.com,githubusercontent.com,localhost,127.0.0.1
  for chart in kagent kagent-crds; do
    if ! HTTPS_PROXY=http://127.0.0.1:9 HTTP_PROXY=http://127.0.0.1:9 NO_PROXY=$direct no_proxy=$direct \
      HELM_CACHE_HOME=$work/cache HELM_CONFIG_HOME=$work/config HELM_DATA_HOME=$work/data \
      helm dependency build "$helm/$chart" >"$work/build.log" 2>&1; then
      sed 's/^/  /' "$work/build.log" >&2
      return 1
    fi
  done
}

copy() {
  mkdir -p "$1"
  cp -r "$root/helm/kagent" "$root/helm/kagent-crds" "$1/"
  mkdir -p "$1/tools" && cp -r "$root/helm/tools/grafana-mcp" "$1/tools/"
  # The publish job appends appVersion before the script runs.
  for chart in kagent kagent-crds; do printf 'appVersion: 0.0.0-test\n' >>"$1/$chart/Chart-template.yaml"; done
  rm -rf "${1:?}"/kagent/charts "${1:?}"/kagent-crds/charts "${1:?}"/kagent/Chart.lock "${1:?}"/kagent-crds/Chart.lock
}

# As upstream ships it: the build reaches ghcr.io and fails.
copy "$work/upstream"
if build "$work/upstream" 2>/dev/null; then
  fail "the upstream charts built with ghcr.io cut off: the cut does not hold, the test proves nothing"
elif grep -q 'ghcr\.io.*proxyconnect' "$work/build.log"; then
  pass "the upstream charts need ghcr.io"
else
  fail "the upstream charts failed to build for another reason than ghcr.io: $(tail -1 "$work/build.log")"
fi

# As the publish job ships it.
copy "$work/published"
(cd "$work/published" && "$here/chart-gsoci-deps.sh" kagent kagent-crds)
for chart in kagent kagent-crds; do
  if grep -q 'ghcr\.io' "$work/published/$chart/Chart-template.yaml"; then
    fail "$chart/Chart-template.yaml still references ghcr.io"
  else
    pass "$chart/Chart-template.yaml references no ghcr.io"
  fi
done
for value in kagent:kmcp kagent:kagent-tools kagent-crds:kmcp; do
  chart=${value%%:*} key=${value#*:}
  got=$(sed -n "/^${key}:\$/,/^[^ #]/ s/^  enabled: //p" "$work/published/$chart/values.yaml")
  [ "$got" = false ] && pass "$chart: $key.enabled is false" || fail "$chart: $key.enabled is $got, want false"
done
if build "$work/published"; then
  pass "the published charts build with ghcr.io cut off"
  for chart in kagent kagent-crds; do
    if grep -qs 'ghcr\.io' "$work/published/$chart/Chart.yaml" "$work/published/$chart/Chart.lock"; then
      fail "$chart: Chart.yaml or Chart.lock references ghcr.io"
    else
      pass "$chart: Chart.yaml and Chart.lock reference no ghcr.io"
    fi
  done
  manifest=$(helm template kagent "$work/published/kagent" --namespace kagent)
  if grep -qE 'name: kagent-tool-server|kmcp' <<<"$manifest"; then
    fail "the kagent chart renders an object of a dropped subchart"
  else
    pass "the kagent chart renders no object of a dropped subchart"
  fi
else
  fail "the published charts do not build with ghcr.io cut off"
fi

# The script is idempotent: a second run over its own output changes nothing.
cp -r "$work/published" "$work/again"
(cd "$work/again" && "$here/chart-gsoci-deps.sh" kagent kagent-crds >/dev/null)
diff -r -q -x Chart.yaml -x Chart.lock -x charts "$work/published" "$work/again" >/dev/null &&
  pass "a second run changes nothing" || fail "a second run changed the charts"

if [ "$failures" -gt 0 ]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
