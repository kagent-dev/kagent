#!/usr/bin/env bash
# The published charts depend on nothing outside gsoci (FORK.md, "What is
# published"). Upstream's chart templates declare subcharts served from
# ghcr.io (kmcp and kmcp-crds, kagent-tools), which the platform does not run:
# muster fills kmcp's role and the platform's MCP servers kagent-tools'. The
# publish job calls this before `make helm-publish`, on its checkout:
#
#   chart-gsoci-deps.sh [<chart dir>...]    (default: helm/kagent helm/kagent-crds)
#
# Every dependency of a chart's Chart-template.yaml whose repository is
# oci://ghcr.io/... is removed, and the value its condition names is set to
# false in the chart's values.yaml, so the chart renders nothing that needs
# the removed subchart (the kagent-tools RemoteMCPServer for one). Both files
# keep their formatting (`helm show values` is documentation), and no yq is
# needed: the template's dependencies are a block list of `  - name:` items.
# A ghcr.io repository that survives, or a condition left on, fails with exit 1.
set -euo pipefail

[ $# -gt 0 ] || set -- helm/kagent helm/kagent-crds

for chart in "$@"; do
  template=$chart/Chart-template.yaml
  values=$chart/values.yaml
  # The template without the ghcr.io items, and each dropped item's condition.
  : >"$template.conditions"
  awk -v conditions="$template.conditions" '
    function flush() {
      if (item != "") {
        if (item ~ /\n *repository: *"?oci:\/\/ghcr\.io\//) {
          if (match(item, /\n *condition: *[^\n]*/)) {
            c = substr(item, RSTART, RLENGTH); sub(/^\n *condition: */, "", c); gsub(/"/, "", c)
            print c > conditions
          }
        } else printf "%s", item
      }
      item = ""
    }
    /^dependencies:/ { deps = 1; print; next }
    deps && /^  - / { flush(); item = $0 "\n"; next }
    deps && /^(  |    )/ && item != "" { item = item $0 "\n"; next }
    deps && /^[^ ]/ { flush(); deps = 0 }
    { print }
    END { flush() }
  ' "$template" >"$template.new"
  mv "$template.new" "$template"
  while read -r condition; do
    key=${condition%.enabled}
    if [ "$key" = "$condition" ] || [[ "$key" == *.* ]]; then
      echo "ERROR: ${template}: condition ${condition} is not <key>.enabled" >&2
      exit 1
    fi
    sed -i "/^${key}:\$/,/^[^ #]/ s/^  enabled: true\$/  enabled: false/" "$values"
    if [ "$(sed -n "/^${key}:\$/,/^[^ #]/ s/^  enabled: //p" "$values")" != false ]; then
      echo "ERROR: ${values}: ${condition} is not false after its dependency left ${template}" >&2
      exit 1
    fi
    echo "${chart}: dropped the ghcr.io dependency behind ${condition}, set it false"
  done <"$template.conditions"
  rm -f "$template.conditions"
  if grep -n 'ghcr\.io' "$template" >&2; then
    echo "ERROR: ${template} still references ghcr.io" >&2
    exit 1
  fi
done
