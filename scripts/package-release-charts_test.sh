#!/usr/bin/env bash
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
check="fixture setup"
trap 'echo "FAIL: $check (line $LINENO)" >&2; if [[ -f "$scratch/command.log" ]]; then cat "$scratch/command.log" >&2; fi' ERR

source_dir="$scratch/chart sources"
metadata_dir="$scratch/metadata"
output_dir="$scratch/release charts"
version=1.0.0-alpha9
digest="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
config_digest="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
mkdir -p "$source_dir/kagent/templates" "$source_dir/kagent/files" "$metadata_dir" "$output_dir"

# Use the real controller templates in dependency-free charts. The test invokes
# the same packaging target as CI without fetching charts or image manifests.
for chart in kagent kagent-crds tools/grafana-mcp; do
  mkdir -p "$source_dir/$chart"
  printf 'apiVersion: v2\nname: %s\nversion: ${VERSION}\n' "${chart##*/}" > "$source_dir/$chart/Chart-template.yaml"
done
cp "$root/helm/kagent/values.yaml" "$source_dir/kagent/values.yaml"
cp "$root/helm/kagent/templates/"*.tpl "$root/helm/kagent/templates/controller-configmap.yaml" "$source_dir/kagent/templates/"
printf 'stale catalog\n' > "$source_dir/kagent/files/builtin-harness-images.json"
cp -R "$source_dir" "$scratch/source-before"
for image in golang-adk claude-harness codex-harness; do
  jq -n --arg digest "$digest" --arg config "$config_digest" \
    '{"containerimage.digest": $digest, "containerimage.config.digest": $config}' > "$metadata_dir/$image.json"
done

release() {
  make --no-print-directory -C "$root" VERSION="$version" KMCP_VERSION=0.4.0 AWK=awk \
    HELM_SOURCE_DIR="$source_dir" HELM_DIST_FOLDER="$output_dir" \
    RUNTIME_IMAGE_METADATA_DIR="$metadata_dir" "$@" > "$scratch/command.log" 2>&1
}

render_images() {
  local archive="$1"
  shift
  helm template release "$archive" --show-only templates/controller-configmap.yaml "$@" > "$scratch/rendered.yaml"
  # This ConfigMap value is a JSON string quoted by Helm's quote function.
  sed -n 's/^  KAGENT_BUILTIN_HARNESS_IMAGES: //p' "$scratch/rendered.yaml" \
    | jq -er 'fromjson' > "$scratch/rendered.json"
}

expect_failure() {
  local expected="$1"
  shift
  if release "$@"; then
    echo 'Expected release packaging to fail' >&2
    return 1
  fi
  [[ "$(< "$scratch/command.log")" == *"$expected"* ]]
  diff -r "$scratch/previous-release" "$output_dir"
}

check="release defaults replace a stale source catalog"
release helm-release
archive="$output_dir/kagent-$version.tgz"
test -f "$output_dir/kagent-crds-$version.tgz"
render_images "$archive"
jq -e --arg digest "$digest" --arg version "$version" '
  .release == $version and
  .kagent == "ghcr.io/kagent-dev/kagent/golang-adk@" + $digest and
  .claude == "ghcr.io/kagent-dev/kagent/claude-harness@" + $digest and
  .codex == "ghcr.io/kagent-dev/kagent/codex-harness@" + $digest
' "$scratch/rendered.json" >/dev/null
cp "$scratch/rendered.json" "$scratch/defaults.json"
diff -r "$scratch/source-before" "$source_dir"

check="override one image"
render_images "$archive" --set-string "controller.harnessImages.codex=custom.example/team/codex@$config_digest"
jq -e --slurpfile defaults "$scratch/defaults.json" --arg config "$config_digest" \
  '. == ($defaults[0] + {codex: ("custom.example/team/codex@" + $config)})' "$scratch/rendered.json" >/dev/null

check="mirror defaults and overrides"
render_images "$archive" --set global.imageRegistry=mirror.example/ \
  --set-string "controller.harnessImages.codex=custom.example/team/codex@$config_digest"
jq -e --arg digest "$digest" --arg config "$config_digest" --arg version "$version" '
  .release == $version and
  .kagent == "mirror.example/kagent-dev/kagent/golang-adk@" + $digest and
  .claude == "mirror.example/kagent-dev/kagent/claude-harness@" + $digest and
  .codex == "mirror.example/team/codex@" + $config
' "$scratch/rendered.json" >/dev/null

check="clear one image"
render_images "$archive" --set-string controller.harnessImages.codex=
jq -e --slurpfile defaults "$scratch/defaults.json" \
  '. == ($defaults[0] + {codex: ""})' "$scratch/rendered.json" >/dev/null

check="local registry and version with v prefix"
release helm-release VERSION=v0.0.1-test RUNTIME_IMAGE_REPOSITORY=localhost:5001/kagent
test -f "$output_dir/kagent-crds-v0.0.1-test.tgz"
render_images "$output_dir/kagent-v0.0.1-test.tgz"
jq -e --arg digest "$digest" '
  .release == "v0.0.1-test" and
  .kagent == "localhost:5001/kagent/golang-adk@" + $digest and
  .claude == "localhost:5001/kagent/claude-harness@" + $digest and
  .codex == "localhost:5001/kagent/codex-harness@" + $digest
' "$scratch/rendered.json" >/dev/null
cp -R "$output_dir" "$scratch/previous-release"

for target in helm-release helm-publish; do
  check="$target requires metadata"
  expect_failure 'RUNTIME_IMAGE_METADATA_DIR is required' "$target" RUNTIME_IMAGE_METADATA_DIR=
done

cp "$metadata_dir/codex-harness.json" "$scratch/codex-valid.json"
for invalid in '{}' '{"containerimage.digest":"sha256:bad"}' '{"containerimage.digest":null}' '{broken'; do
  check="invalid metadata: $invalid"
  printf '%s\n' "$invalid" > "$metadata_dir/codex-harness.json"
  expect_failure codex-harness helm-release
done
check="missing metadata file"
mv "$metadata_dir/codex-harness.json" "$scratch/codex-invalid.json"
expect_failure codex-harness helm-release
cp "$scratch/codex-valid.json" "$metadata_dir/codex-harness.json"

check="packaging failure preserves previous archives"
mv "$source_dir/kagent/Chart-template.yaml" "$scratch/Chart-template.yaml"
expect_failure kagent/Chart-template.yaml helm-release
mv "$scratch/Chart-template.yaml" "$source_dir/kagent/Chart-template.yaml"
diff -r "$scratch/source-before" "$source_dir"

echo 'Release chart packaging tests passed.'
