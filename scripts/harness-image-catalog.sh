#!/usr/bin/env bash
set -euo pipefail

version="${1:?version is required}"
metadata_dir="${2:?runtime image metadata directory is required}"
kagent_image="${3:?kagent image is required}"
claude_image="${4:?claude image is required}"
codex_image="${5:?codex image is required}"

for image in golang-adk claude-harness codex-harness; do
  if [[ ! -f "$metadata_dir/$image.json" ]]; then
    echo "Missing runtime image metadata: $metadata_dir/$image.json. Run make build or provide RUNTIME_IMAGE_METADATA_DIR with existing build outputs." >&2
    exit 1
  fi
done

jq -en \
  --arg release "$version" \
  --arg kagent_image "$kagent_image" \
  --arg claude_image "$claude_image" \
  --arg codex_image "$codex_image" \
  --slurpfile kagent "$metadata_dir/golang-adk.json" \
  --slurpfile claude "$metadata_dir/claude-harness.json" \
  --slurpfile codex "$metadata_dir/codex-harness.json" '
  def image($metadata; $name):
    $metadata[0]["containerimage.digest"] as $digest |
    if ($metadata | length) == 1 and ($digest | type) == "string" and
       ($digest | test("^sha256:[a-f0-9]{64}$"))
    then ($name | split("@")[0] | sub(":[^/:]+$"; "")) + "@" + $digest
    else error("missing or invalid final image digest for " + $name)
    end;
  {
    release: $release,
    kagent: image($kagent; $kagent_image),
    claude: image($claude; $claude_image),
    codex: image($codex; $codex_image)
  }'

