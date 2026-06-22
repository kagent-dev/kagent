#!/bin/sh
# ACP child: spawned by the shim per bridge connection. Guarantees a live
# gateway before exec'ing the stdio<->WS bridge.
set -eu
: "${OPENCLAW_GATEWAY_PORT:=18789}"
/usr/local/bin/openclaw-gateway-ensure.sh
exec openclaw acp --url "ws://127.0.0.1:${OPENCLAW_GATEWAY_PORT}"
