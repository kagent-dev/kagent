#!/bin/sh
# OpenClaw image entrypoint. Writes the gateway config (loopback bind, auth mode
# "none" — gateway and ACP client share this container; the only externally
# reachable surface is the shim, reached through the controller's same-origin
# proxy over the actor's private atenet ingress), pre-warms the gateway so the
# golden snapshot includes a hot npm/node page cache, then hands off to the shim
# with the gateway-ensuring child wrapper.
set -eu
: "${OPENCLAW_GATEWAY_PORT:=18789}"
mkdir -p "${HOME}/.openclaw"
if [ ! -f "${HOME}/.openclaw/openclaw.json" ]; then
    printf '{"gateway":{"port":%s,"bind":"loopback","auth":{"mode":"none"}}}\n' \
        "${OPENCLAW_GATEWAY_PORT}" > "${HOME}/.openclaw/openclaw.json"
fi
/usr/local/bin/openclaw-gateway-ensure.sh || true
exec /usr/local/bin/acp-shim "$@" -- /usr/local/bin/openclaw-acp-child.sh
