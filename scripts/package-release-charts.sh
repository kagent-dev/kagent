#!/usr/bin/env bash
set -euo pipefail

# Called by make helm-release. Buildx outputs are required inputs, never registry lookups.
version="${1:?release version is required}"
metadata_dir="${2:?Buildx metadata directory is required}"
source_dir="${3:?chart source directory is required}"
output_dir="${4:?output directory is required}"
repository="${5:?image repository is required}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

# Validate every required digest before preparing or packaging any charts.
bash "$root/scripts/harness-image-catalog.sh" "$version" "$metadata_dir" \
  "$repository/golang-adk" "$repository/claude-harness" "$repository/codex-harness" \
  > "$staging/builtin-harness-images.json"

# make tools-image-metadata records the kagent-tools image digest for the
# example Agents. Without it, the chart can enable those Agents only with an
# explicit exampleAgents.skillsImage.
tools_metadata="$metadata_dir/kagent-tools.json"
if [[ -e "$tools_metadata" ]]; then
  # The version must be the kagent-tools version the chart depends on, so a
  # stale metadata file cannot pin another release's skills.
  tools_version="$(yq '(.dependencies // [])[] | select(.name == "kagent-tools") | .version' "$source_dir/kagent/Chart-template.yaml")"
  jq -e --arg tools_version "$tools_version" '
    (.image | type) == "string" and (.image | test("^[^[:space:]@]+@sha256:[a-f0-9]{64}$")) and
    ($tools_version == "" or .version == $tools_version)' "$tools_metadata" >/dev/null || {
    echo "Invalid kagent-tools image metadata, or it is not for kagent-tools $tools_version: $tools_metadata. Run make tools-image-metadata." >&2
    exit 1
  }
fi

cp -R "$source_dir" "$staging/helm"
mkdir -p "$staging/helm/kagent/files"
mv "$staging/builtin-harness-images.json" "$staging/helm/kagent/files/builtin-harness-images.json"
if [[ -e "$tools_metadata" ]]; then
  cp "$tools_metadata" "$staging/helm/kagent/files/kagent-tools-image.json"
fi

# Reuse local chart preparation, keeping generated files and intermediate archives isolated.
make --no-print-directory -j1 -C "$root" helm-version VERSION="$version" \
  HELM_SOURCE_DIR="$staging/helm" HELM_DIST_FOLDER="$staging/dist"

# Expose release artifacts only after every chart has packaged successfully.
mkdir -p "$output_dir"
cp "$staging/dist/kagent-$version.tgz" "$staging/dist/kagent-crds-$version.tgz" "$output_dir/"
