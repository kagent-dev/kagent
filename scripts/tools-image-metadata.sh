#!/usr/bin/env bash
set -euo pipefail

# Called by make tools-image-metadata. kagent-tools releases from its own
# repository, so no build in this repository records the image digest. This
# script looks it up in the registry.
chart_template="${1:?kagent Chart-template.yaml is required}"
repository="${2:?kagent-tools image repository is required}"
output="${3:?output file is required}"

command -v yq >/dev/null || { echo "tools-image-metadata.sh needs yq (mikefarah yq v4)" >&2; exit 1; }
mkdir -p "$(dirname "$output")"
rm -f "$output"
version="$(yq -e '.dependencies[] | select(.name == "kagent-tools") | .version' "$chart_template")"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "The kagent-tools dependency in $chart_template must pin an exact version, got: $version" >&2
  exit 1
fi
# kagent-tools 0.3.0 and earlier ship no opt/kagent-tools-skills folder. Record
# no digest for them, so the example Agents need an explicit skillsImage.
last_without_skills="0.3.0"
if [[ "$(printf '%s\n%s\n' "$version" "$last_without_skills" | sort -V | tail -n1)" == "$last_without_skills" ]]; then
  echo "kagent-tools $version has no skills folder, so no digest is recorded" >&2
  exit 0
fi
# The kagent-tools chart tags its image with the chart version.
image="$repository:$version"

# Read all output so Docker does not get a broken pipe under pipefail.
digest="$(docker buildx imagetools inspect "$image" | awk '$1 == "Digest:" { print $2 }')"
if [[ ! "$digest" =~ ^sha256:[a-f0-9]{64}$ ]]; then
  echo "could not resolve the digest of $image" >&2
  exit 1
fi
jq -n --arg image "$repository@$digest" --arg version "$version" '{image: $image, version: $version}' > "$output"
