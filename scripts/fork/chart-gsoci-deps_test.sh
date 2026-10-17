#!/usr/bin/env bash
# Tests of scripts/fork/chart-gsoci-deps.sh against a copy of this checkout's
# charts: the published charts' dependencies build with ghcr.io out of reach,
# and a chart pulled from the registry builds its dependencies as it is.
# ghcr.io is cut off by a proxy that answers nothing; every other host is
# reached directly (NO_PROXY). The copy as upstream ships it must fail that
# build — the test proves the cut — and the copy the script edited must pass
# it, reference no ghcr.io, and render no object of a dropped subchart. The
# registry is a plain-HTTP OCI registry the caller runs (the fork-ledger job's
# service), standing in for gsoci: the publish job's pushes go there, and the
# pulled charts are built from there.
#
#   CHART_TEST_REGISTRY=localhost:5000 scripts/fork/chart-gsoci-deps_test.sh
set -euo pipefail

registry=${CHART_TEST_REGISTRY:?set CHART_TEST_REGISTRY to a plain-HTTP OCI registry, host:port}
repo=oci://$registry/kagent/helm

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
render() {
  for chart in kagent kagent-crds tools/grafana-mcp; do
    VERSION=0.0.0-test KMCP_VERSION=0.4.0 envsubst < "$1/$chart/Chart-template.yaml" > "$1/$chart/Chart.yaml"
  done
}
cut() {
  local direct=github.io,github.com,githubusercontent.com,localhost,127.0.0.1,${registry%:*}
  HTTPS_PROXY=http://127.0.0.1:9 HTTP_PROXY=http://127.0.0.1:9 NO_PROXY=$direct no_proxy=$direct \
    HELM_CACHE_HOME=$work/cache HELM_CONFIG_HOME=$work/config HELM_DATA_HOME=$work/data "$@"
}
depbuild() {
  local chart
  for chart in "$@"; do
    if ! cut helm dependency build --plain-http "$chart" >"$work/build.log" 2>&1; then
      sed 's/^/  /' "$work/build.log" >&2
      return 1
    fi
  done
}
build() {
  render "$1"
  depbuild "$1/kagent" "$1/kagent-crds"
}
# The publish job's order: the local subcharts to the registry, then the
# charts, their dependencies updated from there.
publish() {
  render "$1"
  cut helm package -d "$1/dist" "$1/tools/grafana-mcp" >/dev/null
  cut helm push --plain-http "$1/dist/grafana-mcp-0.0.0-test.tgz" "$repo" >/dev/null 2>&1
  local name version from
  while read -r name version from; do
    mkdir -p "$1/mirror-$name"
    cut helm pull "$name" --repo "$from" --version "$version" -d "$1/mirror-$name" >/dev/null
    cut helm push --plain-http "$1/mirror-$name"/*.tgz "$repo" >/dev/null 2>&1
  done <"$1/mirror"
  depbuild "$1/kagent" "$1/kagent-crds"
  for chart in kagent-crds kagent; do
    cut helm package -d "$1/dist" "$1/$chart" >/dev/null
    cut helm push --plain-http "$1/dist/$chart-0.0.0-test.tgz" "$repo" >/dev/null 2>&1
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

# As 1.15.1-rc.1 shipped it (ghcr.io dropped, grafana-mcp still a file://
# path): the packaged chart, unpacked the way `helm pull --untar` does, cannot
# build its dependencies.
copy "$work/rc1"
(cd "$work/rc1" && "$here/chart-gsoci-deps.sh" kagent kagent-crds >/dev/null)
build "$work/rc1" 2>/dev/null
helm package -d "$work/rc1/dist" "$work/rc1/kagent" >/dev/null
mkdir -p "$work/rc1-pulled" && tar -xzf "$work/rc1/dist/kagent-0.0.0-test.tgz" -C "$work/rc1-pulled"
if depbuild "$work/rc1-pulled/kagent" 2>/dev/null; then
  fail "a packaged chart with a file:// dependency built: the test proves nothing"
elif grep -q 'grafana-mcp.*not found' "$work/build.log"; then
  pass "a packaged chart with the file:// grafana-mcp dependency does not build"
else
  fail "the packaged file:// chart failed for another reason: $(tail -1 "$work/build.log")"
fi

# As the publish job ships it.
copy "$work/published"
(cd "$work/published" && "$here/chart-gsoci-deps.sh" --local-repo "$repo" --mirror-list mirror kagent kagent-crds)
if grep -qE 'repository: *"?(file|https?)://' "$work/published/kagent/Chart-template.yaml"; then
  fail "kagent/Chart-template.yaml still has a file:// or http(s):// repository"
else
  pass "kagent/Chart-template.yaml depends on grafana-mcp and oauth2-proxy from the registry"
fi
[ "$(cat "$work/published/mirror")" = "oauth2-proxy ~10.7.0 https://oauth2-proxy.github.io/manifests" ] &&
  pass "the mirror list names oauth2-proxy" || fail "the mirror list is: $(cat "$work/published/mirror")"
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
if publish "$work/published"; then
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
  # A consumer's pull: the chart as the registry serves it builds its
  # dependencies, ghcr.io still cut off.
  for chart in kagent kagent-crds; do
    mkdir -p "$work/pulled"
    if cut helm pull --plain-http "$repo/$chart" --version 0.0.0-test --untar --untardir "$work/pulled" >/dev/null 2>&1 &&
      depbuild "$work/pulled/$chart"; then
      pass "$chart pulled from the registry builds its dependencies with ghcr.io cut off"
    else
      fail "$chart pulled from the registry does not build its dependencies"
    fi
  done
else
  fail "the published charts do not build with ghcr.io cut off"
fi

# The script is idempotent: a second run over its own output changes nothing.
cp -r "$work/published" "$work/again"
(cd "$work/again" && "$here/chart-gsoci-deps.sh" --local-repo "$repo" kagent kagent-crds >/dev/null)
diff -r -q -x Chart.yaml -x Chart.lock -x charts -x dist -x mirror -x "mirror-*" "$work/published" "$work/again" >/dev/null &&
  pass "a second run changes nothing" || fail "a second run changed the charts"

if [ "$failures" -gt 0 ]; then
  echo "$failures failure(s)" >&2
  exit 1
fi
