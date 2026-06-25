# ACP sandbox images

Prototype image family for kagent's ACP integration (see
[design/EP-XXXX-acp-integration.md](../../design/EP-XXXX-acp-integration.md)).
Every image in this family runs the same entrypoint — `acp-shim`
([go/core/cmd/acp-shim](../../go/core/cmd/acp-shim)) — which exposes a stdio
ACP agent over `ws://0.0.0.0:9000/acp`, reachable through Substrate's atenet
ingress (WebSocket upgrades are enabled there).

## Stages and targets

The [Dockerfile](Dockerfile) defines:

| Stage / target | Buildable | What it is |
|---|---|---|
| `builder` | internal | Compiles the `acp-shim` static binary from this repo. |
| `base` | `--target base` | kagent-owned runtime (`debian:trixie-slim`) with a single unprivileged `agent` user and the shim as `ENTRYPOINT`. Contains **no agent** — it is the uniform transport layer. |
| `node` / `node-base` | internal | `base` + Node.js 22 (trixie apt ships v20, below OpenClaw's >=22.19 requirement). |
| `hermes` | `--target hermes` | `base` + Hermes installed via pip (`hermes-agent[acp]`). Child command: `hermes acp`. |
| `openclaw` | `--target openclaw` | `node-base` + the OpenClaw CLI (`npm install -g openclaw`). Runs a sandbox-local `openclaw gateway` alongside the shim via a small launcher. |

The base↔agent contract is intentionally tiny:

- `ENTRYPOINT` is `acp-shim`; it serves `ws://0.0.0.0:9000/acp`.
- The agent layer provides the child command as `CMD` args after `--`, or via
  the `ACP_SHIM_CHILD` env var.

### Why a kagent-owned base instead of extending NemoClaw's sandbox-base

`ghcr.io/kagent-dev/nemoclaw/sandbox-base` carries NemoClaw-specific
contracts: its gateway/sandbox user split, gosu privilege separation, and
`.openclaw` directory tree are a stable contract with NemoClaw, not with
Substrate. Substrate sandboxes get isolation from the microVM boundary, so that
machinery is dead weight here — and adopting the image would couple every agent
to NemoClaw's release cadence. So the `openclaw` target installs the OpenClaw
CLI directly on the kagent base (the same `npm install -g` NemoClaw's
`Dockerfile.base` uses) rather than extending NemoClaw's image.

## Building

From the repo root (build context is `go/`):

```sh
docker build -f docker/acp-sandbox/Dockerfile --target base     -t kagent/acp-sandbox-base     go/
docker build -f docker/acp-sandbox/Dockerfile --target hermes   -t kagent/acp-sandbox-hermes   go/
docker build -f docker/acp-sandbox/Dockerfile --target openclaw -t kagent/acp-sandbox-openclaw go/
```

## Smoke test (no cluster needed)

`base` has no agent, so smoke-test an agent target (`hermes` is simplest — no
gateway):

```sh
docker run --rm -p 9000:9000 kagent/acp-sandbox-hermes
# then from another shell, speak newline-delimited JSON-RPC over WS:
websocat ws://localhost:9000/acp
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}
```

The shim does not authenticate the WebSocket handshake; in Substrate the actor's
ingress is its only reachable surface and the controller proxies to it.

## Standalone cluster tests

Two manifests in this folder exercise the image **without** the AgentHarness
controller. Each file's header comment has the full step-by-step; in short:

- [test-deployment.yaml](test-deployment.yaml) — runs the `openclaw` image as a
  plain Kubernetes Deployment + Service. Easiest path; works with a kind-loaded
  local image.

  ```sh
  docker build -f docker/acp-sandbox/Dockerfile --target openclaw -t acp-sandbox-openclaw:dev go/
  kind load docker-image acp-sandbox-openclaw:dev --name kagent
  kubectl apply -f docker/acp-sandbox/test-deployment.yaml
  kubectl -n kagent port-forward svc/acp-shim-test 9000:9000
  websocat "ws://localhost:9000/acp?access_token=dev-token"
  ```

- [test-substrate.yaml](test-substrate.yaml) — runs the same image as a real
  Substrate actor (gVisor microVM) via a manual `ActorTemplate` + `ate-api`
  `CreateActor`. Requires pushing a digest-pinned image to a registry (Substrate
  workers pull it themselves). See the file header for the `grpcurl` / atenet
  routing details.

## Open items (tracked in the EP)

- OpenClaw target: the `openclaw gateway` invocation in the launcher is a
  best-guess prototype — needs the real port/auth wiring verified.
- Agent credential injection (`~/.hermes/.env`, OpenClaw provider keys, ...)
  belongs to the harness bootstrap, not these images.
- Whether the shim is baked (this approach) or injected via init container
  + shared volume.
