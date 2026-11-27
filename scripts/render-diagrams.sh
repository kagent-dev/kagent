#!/usr/bin/env bash
set -euo pipefail

# Called by make diagrams-render. Renders each Excalidraw scene, NAME.excalidraw,
# to NAME.svg beside it. Uses the renderer at KROKI_EXCALIDRAW_URL when one
# answers, as in CI; otherwise runs the pinned image for this call and removes
# it on exit.
runtime="${CONTAINER_RUNTIME:?container runtime is required}"
image="${KROKI_EXCALIDRAW_IMAGE:?renderer image is required}"
url="${KROKI_EXCALIDRAW_URL:?renderer URL is required}"

# The renderer answers an empty request with 400, so curl without --fail
# succeeds once something is listening.
if ! curl --silent --output /dev/null "$url"; then
  echo "Starting $image"
  container="$("$runtime" run --detach --rm --publish 8004:8004 "$image")"
  trap '"$runtime" stop "$container" >/dev/null' EXIT
  curl --silent --output /dev/null --retry 30 --retry-delay 2 --retry-all-errors "$url"
fi

for scene in "$@"; do
  svg="${scene%.excalidraw}.svg"
  echo "$scene -> $svg"
  curl --fail-with-body --silent --show-error \
    --header 'Content-Type: text/plain' --data-binary "@$scene" \
    --output "$svg" "$url"
done
