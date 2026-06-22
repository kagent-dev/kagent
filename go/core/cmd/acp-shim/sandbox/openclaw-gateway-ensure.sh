#!/bin/sh
# Start the sandbox-local OpenClaw gateway if it isn't accepting connections,
# and wait for it. Used both at boot and from the ACP child wrapper — under
# Substrate the actor is checkpointed/restored and any process started before
# the snapshot (the gateway, or a supervising subshell) cannot be relied on
# afterwards, so the gateway is re-ensured at connection time by a freshly
# spawned process.
set -u
: "${OPENCLAW_GATEWAY_PORT:=18789}"
probe() { curl -s -o /dev/null --max-time 2 "http://127.0.0.1:${OPENCLAW_GATEWAY_PORT}/"; }
probe && exit 0
nohup openclaw gateway run --port "${OPENCLAW_GATEWAY_PORT}" --allow-unconfigured \
    >> "${HOME}/.openclaw/gateway.log" 2>&1 &
i=0
while [ "$i" -lt 60 ]; do
    probe && exit 0
    i=$((i+1))
    sleep 1
done
echo "openclaw-gateway-ensure.sh: gateway did not come up on :${OPENCLAW_GATEWAY_PORT}" >&2
exit 1
