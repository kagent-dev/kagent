# acp-sandbox launcher scripts

Small POSIX `sh` launchers baked into the agent images defined by
[docker/acp-sandbox/Dockerfile](../../../../docker/acp-sandbox/Dockerfile). They
are kept here, next to the `acp-shim` binary they wrap
([main.go](main.go)), and `COPY`'d into `/usr/local/bin/` by the relevant agent
target. The Dockerfile build context is `go/`, so the copy paths are
`core/cmd/acp-shim/sandbox/<script>`.

## Why these scripts exist

Every acp-sandbox image runs the same entrypoint — `acp-shim` — which bridges a
stdio ACP agent to `ws://0.0.0.0:9000/acp`. Some agents need a **sidecar gateway
process** running alongside that per-connection stdio child:

- **OpenClaw**: `openclaw acp` is only a stdio↔WS bridge; it needs a local
  `openclaw gateway` to talk to.
- **Hermes**: messaging channels (Slack/Telegram/...) are served by a long-lived
  `hermes gateway`, separate from the per-connection `hermes acp` child.

Under Substrate the actor is checkpointed and later restored, and **any process
started before the snapshot cannot be relied on afterwards**. So the gateway is
(re-)ensured by a freshly spawned process at connection time, not just at boot.
That "ensure it's up, idempotently" need is what these scripts encapsulate.

## The scripts

| Script | Image | Role |
|---|---|---|
| [hermes-gateway-ensure.sh](hermes-gateway-ensure.sh) | hermes | Idempotently starts the Hermes messaging gateway. No-op unless a channel token (`SLACK_BOT_TOKEN` / `TELEGRAM_BOT_TOKEN`) is set. Liveness via a PID file (`kill -0`). Fire-and-forget — never blocks the acp child. |
| [openclaw-gateway-ensure.sh](openclaw-gateway-ensure.sh) | openclaw | Idempotently starts the OpenClaw gateway and waits (up to 60s) for it to accept connections. Liveness via an HTTP probe on `OPENCLAW_GATEWAY_PORT`. |
| [openclaw-acp-child.sh](openclaw-acp-child.sh) | openclaw | The per-connection child the shim spawns: ensures the gateway, then `exec`s the `openclaw acp` bridge. |
| [openclaw-acp-entrypoint.sh](openclaw-acp-entrypoint.sh) | openclaw | Container entrypoint: writes the gateway config, pre-warms the gateway (so the golden snapshot has a hot cache), then `exec`s the shim with the child wrapper. |
