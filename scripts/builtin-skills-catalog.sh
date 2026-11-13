#!/usr/bin/env bash
set -euo pipefail

# Prints the built-in skills catalog: the kagent-tools image that the chart
# deploys, pinned by digest, and the name, description and path of every skill
# in it. Rerun with make builtin-skills-catalog after you bump kagent-tools in
# helm/kagent/Chart-template.yaml.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repository="ghcr.io/kagent-dev/kagent/tools"
skills_root="opt/kagent-tools-skills/skills"
# The kagent-tools skills the UI offers. Cilium is left out; the chart offers
# Prometheus only when kagent-tools.tools.prometheus.url is set.
skills=(argo-rollouts-progressive-delivery helm-release-management istio-traffic-debugging
  k8s-troubleshooting kubescape-security-posture prometheus-observability)

version="$(awk '/- name: kagent-tools$/ { found = 1; next } found && /version:/ { print $2; exit }' \
  "$root/helm/kagent/Chart-template.yaml")"
[[ -n "$version" ]] || { echo "kagent-tools version not found in helm/kagent/Chart-template.yaml" >&2; exit 1; }

digest="$(docker buildx imagetools inspect "$repository:$version" --format '{{json .Manifest}}' | jq -r .digest)"
[[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "invalid digest for $repository:$version: $digest" >&2; exit 1; }
image="$repository@$digest"

work="$(mktemp -d)"
container=""
cleanup() {
  [[ -z "$container" ]] || docker rm "$container" > /dev/null
  rm -rf "$work"
}
trap cleanup EXIT
container="$(docker create "$image" true)"
docker cp "$container:/$skills_root" - | tar -x -C "$work"

for dir in "${skills[@]}"; do
  file="$work/skills/$dir/SKILL.md"
  [[ -f "$file" ]] || { echo "$image has no $skills_root/$dir/SKILL.md" >&2; exit 1; }
  awk -v dir="$dir" -v file="$dir/SKILL.md" '
    function fail(message) { printf "%s: %s\n", file, message > "/dev/stderr"; failed = 1; exit 1 }
    function plain(field, value) {
      if (value ~ /^["\047]/ || value ~ /^[|>]/) {
        fail(field " must be one line of unquoted text, not a quoted or block value: " value)
      }
      return value
    }
    NR == 1 && $0 != "---" { exit 1 }
    NR > 1 && $0 == "---" { closed = 1; exit }
    NR > 1 && /^name:/ { name = plain("name", substr($0, 7)) }
    NR > 1 && /^description:/ { description = plain("description", substr($0, 14)) }
    END {
      if (failed) exit 1
      if (!closed || name == "" || description == "") fail("front matter needs a name and a description")
      if (name != dir) fail("name " name " must match its directory " dir)
      print name; print description
    }' "$file" | jq -Rn --arg path "$skills_root/$dir" '[inputs] | {name: .[0], description: .[1], path: $path}'
done | jq -es --arg image "$image" '
  if length == 0 then error("no skills found") else {image: $image, skills: sort_by(.name)} end'
