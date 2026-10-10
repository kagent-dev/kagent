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
jq -en \
  --arg release "$version" \
  --arg repository "$repository" \
  --slurpfile kagent "$metadata_dir/golang-adk.json" \
  --slurpfile claude "$metadata_dir/claude-harness.json" \
  --slurpfile codex "$metadata_dir/codex-harness.json" '
  def image($metadata; $name):
    $metadata[0]["containerimage.digest"] as $digest |
    if ($metadata | length) == 1 and ($digest | type) == "string" and
       ($digest | test("^sha256:[a-f0-9]{64}$"))
    then $repository + "/" + $name + "@" + $digest
    else error("missing or invalid final image digest for " + $name)
    end;
  {
    release: $release,
    kagent: image($kagent; "golang-adk"),
    claude: image($claude; "claude-harness"),
    codex: image($codex; "codex-harness")
  }' > "$staging/builtin-harness-images.json"

cp -R "$source_dir" "$staging/helm"
mkdir -p "$staging/helm/kagent/files"
mv "$staging/builtin-harness-images.json" "$staging/helm/kagent/files/builtin-harness-images.json"

# Reuse local chart preparation, keeping generated files and intermediate archives isolated.
make --no-print-directory -j1 -C "$root" helm-version VERSION="$version" \
  KMCP_VERSION="${KMCP_VERSION:?KMCP version is required}" \
  HELM_SOURCE_DIR="$staging/helm" HELM_DIST_FOLDER="$staging/dist"

# Expose release artifacts only after every chart has packaged successfully.
mkdir -p "$output_dir"
cp "$staging/dist/kagent-$version.tgz" "$staging/dist/kagent-crds-$version.tgz" "$output_dir/"
