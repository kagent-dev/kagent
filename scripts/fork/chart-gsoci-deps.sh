#!/usr/bin/env bash
# The published charts depend on nothing outside gsoci (FORK.md, "What is
# published"). Upstream's chart templates declare subcharts served from
# ghcr.io (kmcp and kmcp-crds, kagent-tools), which the platform does not run:
# muster fills kmcp's role and the platform's MCP servers kagent-tools'. A
# subchart of this repository (grafana-mcp) is a file:// path, which a pulled
# chart cannot resolve, and one (oauth2-proxy) comes from an HTTP chart
# repository, which a pulled chart's lock resolves only after a `helm repo add`.
# The publish job calls this before `make helm-publish`, on its checkout, then
# pushes those subcharts to the given repository before the dependency update:
#
#   chart-gsoci-deps.sh [--local-repo <oci repo> [--mirror-list <file>]] [<chart dir>...]
#                                     (default: helm/kagent helm/kagent-crds)
#
# Every dependency of a chart's Chart-template.yaml whose repository is
# oci://ghcr.io/... is removed, and the value its condition names is set to
# false in the chart's values.yaml, so the chart renders nothing that needs
# the removed subchart (the kagent-tools RemoteMCPServer for one). With
# --local-repo, every file:// and http(s):// repository becomes that OCI
# repository: a file:// subchart is published there at the same version, an
# HTTP one mirrored there; --mirror-list appends `<name> <version> <repository>`
# of each HTTP one to <file>, the charts the publish job mirrors. Both files keep their formatting
# (`helm show values` is documentation), and no yq is needed: the template's
# dependencies are a block list of `  - name:` items. A ghcr.io repository, or
# with --local-repo a file:// or http(s):// one, that survives, or a condition
# left on, fails with exit 1.
set -euo pipefail

local_repo=
mirror_list=/dev/null
if [ "${1:-}" = --local-repo ]; then
  local_repo=${2:?--local-repo needs an oci:// repository}
  shift 2
  if [ "${1:-}" = --mirror-list ]; then
    mirror_list=${2:?--mirror-list needs a file}
    shift 2
  fi
fi
[ $# -gt 0 ] || set -- helm/kagent helm/kagent-crds

for chart in "$@"; do
  template=$chart/Chart-template.yaml
  values=$chart/values.yaml
  # The template without the ghcr.io items, and each dropped item's condition.
  : >"$template.conditions"
  awk -v conditions="$template.conditions" -v local_repo="$local_repo" -v mirror_list="$mirror_list" '
    function field(item, key,   v) {
      if (!match(item, "\n *" key ": *[^\n]*")) return ""
      v = substr(item, RSTART, RLENGTH); sub("^\n *" key ": *", "", v); gsub(/"/, "", v)
      return v
    }
    function flush(   from) {
      if (item != "" && local_repo != "") {
        from = field(item, "repository")
        if (from ~ /^(file|https?):\/\//) {
          if (from !~ /^file:/) print field("\n" substr(item, 5), "name"), field(item, "version"), from >> mirror_list
          sub(/\n *repository: *[^\n]*/, "\n    repository: " local_repo, item)
        }
      }
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
  if [ -n "$local_repo" ] && grep -nE 'repository: *"?(file|https?)://' "$template" >&2; then
    echo "ERROR: ${template} still has a file:// or http(s):// repository" >&2
    exit 1
  fi
done
