# omp Harness

The omp Harness runs [oh-my-pi](https://github.com/can1357/oh-my-pi) (omp)
`18.11.0` as a bring-your-own Harness. Kagent has no omp runtime, so a `Harness`
names the `omp-harness` image by digest with `workload.command` set, and the
controller compiles the Agent into the ADK-shaped `KAGENT_CONFIG_JSON` it gives
every BYO Harness. The image serves Kagent's private A2A contract, `/readyz` and
the TaskStore at `KAGENT_API_URL` on port 80 through the shared runtime
packages, and runs each turn as one `omp --mode rpc` process on the Actor's omp
session under `/data/omp`.

## Code structure

| Path | Look here for |
| --- | --- |
| [`cmd/main.go`](cmd/main.go) | Startup, environment inputs, omp version check, continuation store and private A2A |
| [`internal/config`](internal/config) | Reading the ADK configuration and refusing what the adapter does not implement |
| [`internal/adapter`](internal/adapter) | omp's files under `/data/omp`, the isolation flags and the MCP tool bridge |
| [`internal/driver`](internal/driver) | The omp process per turn, the RPC framing, event translation, resume and usage |

## Implemented support

- An Anthropic ModelConfig, the same one the claude Harness takes: the model is
  omp's only provider, `kagent/<model>`, on the ModelConfig's base URL (default
  `https://api.anthropic.com`) and headers. The key travels as the `X-Api-Key`
  header holding the `ANTHROPIC_API_KEY` placeholder the egress gateway
  replaces; omp sends no `Authorization`.
- Streamable HTTP MCP servers with their headers, Secret-backed headers as the
  egress gateway's placeholders. A server is named after the first label of its
  host (`mcp__muster_<tool>`). A binding that selects tools is served through a
  loopback MCP bridge that lists and calls only those tools, because omp has no
  MCP allowlist; the bridge holds the server's headers.
- Standalone skills, written to omp's user skills directory.
- Streaming text, tool calls and results; one turn's usage (the difference of
  omp's session totals around the prompt, every prompt token counted as input
  and cache reads among them) on its final task status.
- Resume: a turn after a suspend starts omp with `--resume <session>` on the
  session directory under `/data`.

Refused with the reason: other model providers, API-key passthrough, custom
TLS, SSE and stdio MCP servers, tool approval, sub-agents and remote agents,
memory, structured output and plugins.

## Isolation

A repository's own omp, `.claude`, `.codex` or other agent configuration adds
no tool, server, rule or provider:

- `--no-extensions` (ambient extensions, hooks and plugins) and `--no-rules`;
- a `--config` overlay, omp's highest layer below the flags, that turns off
  every discovery source but omp's own (`disabledProviders`), project MCP
  configuration and every skill source but the adapter's;
- the adapter rewrites the overlay, `models.yml`, `mcp.json` and the
  instruction before every turn, so a turn cannot change the next turn's tools
  or providers by editing them.

omp runs as the image's non-root user, the harness's own. The harness process
is not dumpable, so omp and its commands cannot read its environment or
memory; omp's environment carries neither the compiled configuration nor the
header variables of the bridged servers. A server without a tool selection is
reached directly, and its headers are in `mcp.json`.

Hooks have no off switch in omp beyond `--no-extensions`; the gap stays
documented until omp offers one. MCP tool selection keeps a server's other
tools away from the model, not from a shell command that guesses the gateway's
placeholder; the Substrate Actor and the egress gateway remain the boundary.

## The pin

The image installs omp's `omp-linux-musl-{x64,arm64}` release binary, checked
against the `OMP_*_SHA256` arguments of the [`Dockerfile`](Dockerfile).
`PinnedOmpVersion` in [`internal/config`](internal/config/config.go) is the only
version the harness accepts. Renovate moves both with every omp release;
`scripts/fork/harness-pins.sh` writes the checksums from the release's
`SHA256SUMS.txt`.

## Tests

`go test ./harness/omp/...` runs the unit tests. The integration test runs the
pinned omp against a fake Anthropic endpoint, an MCP server and a workspace
carrying hostile omp, Claude and Codex configuration:

```sh
KAGENT_OMP_BINARY=/path/to/omp-linux-x64 go test ./harness/omp/...
```
