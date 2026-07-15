# EP-XXXX: Agent Client Protocol (ACP) integration for AgentHarness backends

* Issue: [#XXXX](https://github.com/kagent-dev/kagent/issues/XXXX) <!-- TODO: create issue and rename file to EP-<issue#>-acp-integration.md -->

## Background

The [Agent Client Protocol (ACP)](https://agentclientprotocol.com/) standardizes communication between clients (editors, UIs) and coding agents, much like LSP standardized language server integration. It is a JSON-RPC 2.0 protocol, by default spoken over stdio with the agent running as a subprocess of the client.

On transports, the [v1 Transports spec](https://agentclientprotocol.com/protocol/v1/transports) defines:

1. **stdio** — the only fully-specified transport ("agents and clients SHOULD support stdio whenever possible");
2. **Streamable HTTP** — listed in the spec but explicitly marked *"in discussion, draft proposal in progress"*; the draft is actively iterating upstream (e.g., [agent-client-protocol#1124](https://github.com/agentclientprotocol/agent-client-protocol/pull/1124), "Revisions on streamable-http/ws GET streams", covering both HTTP and WebSocket streams);
3. **Custom Transports** — the protocol is explicitly transport-agnostic: any bidirectional channel is permitted as long as the JSON-RPC message format and lifecycle are preserved. This clause is what legitimizes OpenClaw's stdio→WebSocket gateway bridge today.

Key protocol elements (v1):

* **Lifecycle**: `initialize` (version + capability negotiation) → optional `authenticate` → `session/new` / `session/load` / `session/resume` / `session/close` → `session/prompt` turns, ending with a stop reason.
* **Streaming**: the agent emits `session/update` notifications during a turn — message chunks (agent/user/thought), `tool_call` / `tool_call_update`, plans, and available slash commands.
* **Bidirectional**: the agent can call back into the client — `session/request_permission` (a built-in human-in-the-loop primitive), `fs/read_text_file`, `fs/write_text_file`, and `terminal/*` methods.
* **MCP passthrough**: `session/new` carries a working directory and a list of MCP servers the agent should connect to (stdio required; HTTP/SSE behind `mcpCapabilities`).
* **Extensibility**: `_meta` fields on any message, `_`-prefixed custom methods, and custom capabilities advertised at initialization.

Both harness backends kagent supports today already implement ACP, in different ways:

* **OpenClaw** ([docs](https://docs.openclaw.ai/cli/acp)): `openclaw acp` is a stdio→WebSocket *bridge* that forwards ACP traffic to an OpenClaw Gateway (`--url wss://host:18789 --token …`). The bridge process can run anywhere that can reach the gateway — it does **not** need to run inside the sandbox. ACP sessions map to Gateway session keys (`--session agent:main:main`, or per-session via `_meta.sessionKey`). Implemented: `initialize`, `session/new`, `session/prompt`, cancel, list, resume, close; partial: `session/load` (event-ledger replay), `session/set_mode`, exec-approval relay via `session/request_permission`, tool-call streaming. Not supported: per-session `mcpServers` (rejected), client `fs/*` and `terminal/*` callbacks, plan/thought streaming.
* **Hermes** ([docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/acp)): `hermes acp` runs the full agent *in-process* as a stdio ACP server (requires the `hermes-agent[acp]` extra). It uses a curated `hermes-acp` toolset (file, terminal, web, memory, skills, execute_code, delegate_task, vision), reads the same `~/.hermes/{.env,config.yaml}` configuration kagent's Hermes bootstrap already writes, and supports `allow_once` / `allow_session` / `allow_always` / `deny` approval semantics through `session/request_permission`. ACP sessions are tracked in-memory per server process; `list/load/resume/fork` are scoped to that process lifetime.

kagent's current integration with these harnesses is entirely proprietary:

* The `AgentHarness` CRD (`go/api/v1alpha2/agentharness_types.go`) selects a `backend` (`openclaw`, `hermes`). [Substrate](https://github.com/agent-substrate/substrate) is the only supported sandbox runtime.
* Interaction happens through the backend's own gateway HTTP API, proxied by the kagent controller at `/api/agentharnesses/{namespace}/{name}/gateway/`. Substrate has no SSH and no exec/attach channel into actors. The ate API is lifecycle-only: `CreateActor`/`SuspendActor`/`ResumeActor`/`DeleteActor`; the only path into an actor is network ingress via the atenet router. We can't rely on substrate adding ssh support any time soon, possibly never.

* On Substrate, the per-backend interaction story is:
  * **OpenClaw**: the proxied OpenClaw web control UI — works, but is backend-specific.
  * **Hermes**: no interaction channel at all — its TUI requires SSH.
  * **Codex and other coding agents**: TUI-only; no web UI to proxy, no SSH to launch them — unintegrable on Substrate today.
* Gateway authentication uses a Bearer token (`spec.substrate.gatewayToken` / `spec.substrate.gatewayTokenSecretRef` on the Substrate runtime).
* Harnesses are *not* agents from kagent's perspective: they cannot be invoked through the A2A surface (`/api/a2a/{namespace}/{name}`), cannot be used as subagents/tools, and each backend requires bespoke kagent code for chat-style interaction.

Meanwhile, kagent's native agent protocol is A2A: declarative and BYO agents serve A2A on port 8080, the controller proxies and streams it, and human-in-the-loop (HITL) flows ride on A2A task pause/resume (`docs/architecture/a2a-subagents.md`).

This EP proposes adopting ACP as the standard protocol surface between kagent and harness backends, and evaluates the integration options.

## Motivation

The immediate problem is the **interaction channel on Substrate**. The TUI path depends on SSH, which Substrate does not support (and may never support). That leaves OpenClaw usable only through its own proxied web UI, Hermes with no interaction channel at all, and TUI-only agents like Codex impossible to integrate. Each backend also requires bespoke integration code: gateway API client, chat proxying, session handling, and approval plumbing.

ACP dissolves both problems at once: OpenClaw, Hermes, Codex, and much of the broader ecosystem (Gemini CLI, Claude Agent, Goose, etc.) already implement ACP. If kagent implements the *client* side of ACP once, it can talk to all of them — no SSH, no TUI, no per-backend UI. By speaking ACP, kagent can:

* provide a chat interaction channel for every harness backend on Substrate, including ones that only ship a TUI today (Codex, etc.),
* interact with any ACP-capable harness through one code path,
* promote harnesses from opaque sandboxes to first-class agents that the UI, other agents, and A2A clients can talk to,
* reuse ACP's built-in `session/request_permission` flow to surface harness tool approvals in kagent's existing HITL UX,
* reduce the per-backend surface area in `go/core/pkg/sandboxbackend/`.

### Goals

* Define a single, backend-agnostic protocol surface (ACP) for conversational interaction with `AgentHarness` resources.
* Expose ACP-connected harnesses as A2A agents so they are addressable at `/api/a2a/{namespace}/{name}`, usable from the kagent UI, and invocable as subagents/tools.
* Map ACP `session/request_permission` onto kagent's HITL approval flow (A2A input-required / task pause-resume).
* Support session lifecycle parity where the backend allows it: create, prompt, cancel, list, resume, close.
* Keep the existing gateway proxy path working unchanged; ACP is additive and opt-in.
* Target the Substrate runtime exclusively; no SSH/exec-based transports.
* Establish a pattern that extends to additional ACP-capable backends (Codex, Gemini CLI, …) without new interaction-channel work.

Success criteria: a user can chat with an OpenClaw or Hermes harness from the kagent UI through the standard agent chat surface, see streamed tool activity, and answer approval prompts — without any backend-specific UI code.

### Non-Goals

* Replacing A2A as kagent's native agent protocol. ACP is the harness-facing protocol; A2A remains the kagent-facing one.
* Exposing kagent agents *as ACP servers* to IDEs (Zed, VS Code). This is valuable but is a separate enhancement (see Alternatives / Future Work).
* Per-session MCP server injection for OpenClaw — the upstream bridge explicitly rejects `mcpServers` in bridge mode; MCP must be configured on the gateway/agent side.
* Implementing ACP client `fs/*` or `terminal/*` callbacks. Neither backend calls them today (OpenClaw's bridge never does; Hermes uses its own sandbox-local tools), and the harness filesystem is remote from kagent's perspective.
* Replicating the TUI experience. ACP gives a structured chat/approval surface, not a terminal; there is no plan to emulate terminals over Substrate.
* Migrating kagent's own agents (declarative/BYO) to ACP — see "Scope: which agents need ACP (and which don't)" below.
* Rewriting the AgentHarness UI (control-UI proxy) — that remains as-is.

### Scope: which agents need ACP (and which don't)

The shim and the bridge are keyed to the agent's **native protocol surface**, not to where it runs:

| Agent type | Native surface | In Substrate needs |
|---|---|---|
| Declarative / BYO kagent agent | A2A over HTTP :8080 | Nothing — atenet routes HTTP directly; the existing `/api/a2a/{ns}/{name}` proxy works unchanged |
| Harness backends (OpenClaw, Hermes, Codex, …) | ACP over stdio | `acp-shim` (stdio→WS) + A2A↔ACP bridge |

Declarative agents running as Substrate actors do **not** need the shim: they already serve A2A over HTTP, and HTTP ingress is exactly what atenet provides. The shim exists solely for agents whose only interface is stdio.

Conversely, moving kagent's own agents to ACP would be a strict downgrade and is explicitly out of scope:

* **Wrong direction** — in ACP the platform is the *client* and the coding agent is the *server*; declarative agents would need to grow an ACP server implementation in the ADK, replacing working A2A code for no new capability.
* **A2A is a superset for kagent's needs** — agent cards/discovery, the task model, push notifications, and subagent composition have no ACP equivalent; ACP is deliberately scoped to "a client drives one coding agent over a session."
* **No interop gain** — ACP's value here is meeting third-party agents where they already are. kagent's own agents are already on the platform-native protocol.

A2A remains the lingua franca inside kagent; ACP is an adapter at the edge for foreign agents.

## Implementation Details

Three options were considered. **Option A is the recommended long-term direction**; B and C are documented for completeness. The **first shipped implementation follows Option B** (UI-direct ACP) — see "Implementation Status (as built)" below for how the current code actually works.

### Option A (recommended): A2A↔ACP bridge — harness as a first-class agent

Add a Go ACP client package and a per-harness bridge component that makes an ACP-capable harness look like a regular kagent agent over A2A.

```
UI / A2A client
      │  A2A (JSON-RPC over HTTP/SSE)
      ▼
kagent controller ── /api/a2a/{ns}/{name} ──► A2A↔ACP bridge (ACP client)
                                                   │  ACP over WS (atenet ingress)
                                                   ▼
                                          ┌─ Substrate sandbox ─────────┐
                                          │  acp-shim (WS ↔ stdio)      │
                                          │     │ stdin/stdout          │
                                          │     ▼                       │
                                          │  openclaw acp / hermes acp  │
                                          │  / codex acp / …            │
                                          └─────────────────────────────┘
```

**New components**

* `go/core/pkg/acp` (or similar): minimal ACP v1 client — JSON-RPC framing, `initialize` capability negotiation, session lifecycle, `session/update` notification dispatch, and server→client request handling (`session/request_permission`).
* A bridge runner per harness, owned by the AgentHarness controller, that:
  1. connects to the harness's in-sandbox `acp-shim` endpoint over WS (see transport below),
  2. registers an A2A endpoint for the harness at `/api/a2a/{namespace}/{name}`,
  3. translates between the two protocols.
* `acp-shim`: a small agent-agnostic stdio↔WebSocket adapter that runs inside the sandbox (design below).

**Protocol mapping**

| A2A (kagent native) | ACP (harness) |
|---|---|
| `message/send` (new conversation) | `session/new` + `session/prompt` |
| `message/send` (existing conversation) | `session/prompt` (same `sessionId`) |
| Streamed task events (text chunks) | `session/update` `agent_message_chunk` / `agent_thought_chunk` |
| Streamed tool events (function_call / tool_result parts) | `session/update` `tool_call` / `tool_call_update` |
| Task `input-required` (HITL pause) | `session/request_permission` (bridge responds with the user's selected option) |
| Task cancel | `session/cancel` |
| Task completion + final status | `session/prompt` response stop reason |
| kagent session/conversation id | ACP `sessionId` (persisted in kagent DB alongside the conversation) |

**Transport: a uniform in-sandbox shim (`acp-shim`)**

All backends are reached the same way: a single agent-agnostic shim runs inside the Substrate sandbox, exposing the agent's stdio ACP server over a WebSocket endpoint reachable through the atenet ingress (which supports WS upgrade). This is permitted by ACP's Custom Transports clause — the JSON-RPC message format and lifecycle are preserved; only the pipe moves onto the network.

The shim is a transport adapter and nothing more. On accepting a connection it:

1. **Auth-gates the WS handshake** — validates a bearer token (mounted file or env, same pattern as `gatewayToken`). This is the one responsibility stdio doesn't have: a network listener needs explicit auth where "I spawned the process" sufficed before.
2. **Spawns (or attaches to) the agent subprocess** with the configured argv/env/cwd.
3. **Pumps frames**: one WS text frame ⇄ one newline-delimited JSON-RPC message on the child's stdin/stdout. Child stderr goes to container logs. The shim never parses message contents — it is ACP-version-agnostic.
4. **Couples lifecycles**: WS close → SIGTERM (grace) → SIGKILL; child exit → WS close with a status code the bridge can distinguish (crash vs. clean exit). A configurable grace window keeps the child alive across bridge reconnects so `session/load`/`session/resume` can recover in-memory sessions (important for Hermes).

Deliberately *out* of the shim: JSON-RPC parsing, session management, protocol translation — all of that lives in the bridge. Expected size: a few hundred lines of Go, statically linked.

The shim binary is identical for every backend; only its configuration differs:

```yaml
command: ["hermes", "acp"]      # the only truly per-backend part
workdir: /sandbox
env: { ... }                    # backend credentials/config (written by bootstrap)
listen: :9000
tokenFile: /var/run/acp/token
childPolicy: long-lived          # or per-connection
```

**Image packaging**: because Substrate has no exec channel, the agent binary and the shim must both be present in the workload image at build time — but image prep is already mandatory for every backend (`spec.image` / `spec.substrate.workloadImage`). Adding a backend costs a `COPY acp-shim` line plus an entrypoint setting. If Substrate workload templates support an init-container + shared volume, the shim could instead be injected at workload creation, letting stock upstream agent images work unmodified (needs verification).

**Per-backend child commands**

| Backend | Child command | Notes |
|---|---|---|
| OpenClaw | `openclaw acp --url ws://localhost:<gateway-port> --token-file …` | `openclaw acp` is itself a stdio→WS bridge; pointed at the sandbox-local gateway it behaves like any other stdio ACP agent under the shim. Session pinning via `--session`/`_meta.sessionKey`. |
| Hermes | `hermes acp` | Requires the `hermes-agent[acp]` extra in the image; reads the `~/.hermes/{.env,config.yaml}` kagent's bootstrap already writes. In-memory sessions → `childPolicy: long-lived`. |
| Codex (future) | `codex acp` | Credentials (`~/.codex/auth.json` / `OPENAI_API_KEY`) written by bootstrap. |
| Gemini CLI, Goose, … (future) | `gemini --experimental-acp`, etc. | Same pattern; no new transport work. |

Using the shim for OpenClaw too (rather than running `openclaw acp` on the kagent side against the gateway WSS) keeps one code path for all backends: every harness is "shim + stdio ACP command in the sandbox", and the bridge needs exactly one transport implementation. Backend differences that remain — advertised capabilities, approval semantics, session durability — surface through ACP `initialize` negotiation, not through transport variation.

If/when the upstream Streamable HTTP transport stabilizes and agents adopt it, agents would serve ACP over HTTP natively in-sandbox and the shim disappears — with no changes to the bridge's A2A-facing API.

**CRD sketch**

ACP exposure is opt-in via a new optional field on `AgentHarnessSpec`:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: AgentHarness
spec:
  backend: openclaw
  runtime: substrate
  substrate:
    gatewayTokenSecretRef: { name: my-harness-token }
  acp:                      # new, optional
    enabled: true
    sessionKey: agent:main:main   # optional; OpenClaw only — pins the Gateway session key
```

When `spec.acp.enabled` is true and the harness is Ready, the controller starts the bridge and publishes the A2A endpoint (e.g., in `status.connection` and the agents list). No change to existing fields. Future ACP-only backends (e.g., `codex`) slot into the same shape: backend-specific bootstrap plus an ACP transport — no new interaction-channel machinery.

**Capability degradation**

The bridge MUST honor `initialize` capabilities: e.g., only offer session resume/list where advertised, and degrade gracefully (OpenClaw has no plan/thought streaming; Hermes sessions don't survive a server-process restart). Unsupported ACP features simply don't surface in A2A events.

**UI impact**

Because the bridge exposes harnesses as standard A2A agents, the existing kagent chat surface (agent chat page, streaming, HITL approval prompts) works as-is — that is the main payoff of Option A. Expected UI changes are limited to:

* listing ACP-enabled harnesses alongside regular agents (sourced from the same agents list the bridge registers into),
* an entry point on the AgentHarness detail page ("Chat") linking to the standard agent chat for that harness,
* optionally rendering ACP-specific niceties the A2A mapping already carries (thought chunks, tool-call progress) — these reuse the existing event-rendering components.

No new chat code path, no backend-specific UI. The bespoke OpenClaw control-UI proxy remains available unchanged.

### Option B: UI chat via ACP only

Teach the kagent UI/HTTP server to drive harness sessions over ACP directly (replacing the bespoke gateway chat proxy) without exposing an A2A surface. Smaller scope than Option A, but harnesses remain non-composable — they can't be used as subagents/tools or reached by external A2A clients — and a second chat code path persists in the UI. Rejected as the primary direction; Option A subsumes it.

### Option C: kagent agents as ACP servers (future work)

The inverse direction: a `kagent acp` CLI that speaks ACP over stdio to an IDE (Zed, VS Code ACP client) and forwards to a cluster-hosted kagent agent over A2A — analogous to `openclaw acp`'s relationship to its gateway. This would let any ACP-capable editor drive kagent agents. Deliberately out of scope here; should be its own EP.

### Test Plan

* **Unit**: ACP client package tested against a fake in-process ACP server (table-driven, covering capability negotiation, session lifecycle, update dispatch, permission round-trips, and malformed-frame handling). Bridge translation logic tested with golden A2A event sequences for given ACP update streams. `acp-shim` tested with a fake stdio child (frame pumping, auth rejection, lifecycle coupling, reconnect grace window).
* **Protocol smoke tests**: following OpenClaw's documented proof procedure — drive `openclaw acp` over stdio with raw JSON-RPC frames covering `initialize`, `session/new`, `session/list` (absolute `cwd`), `session/resume`, `session/close`, duplicate close, missing resume; assert advertised `sessionCapabilities` and gateway `sessions.list` log entries.
* **E2E**: extend the existing harness e2e setup (Kind + AgentHarness, Substrate runtime) with an ACP-enabled OpenClaw harness; send an A2A `message/send` through `/api/a2a/{ns}/{name}`, assert streamed chunks and a terminal task state. Hermes e2e gated on the in-sandbox transport shim being available in CI.

## Alternatives

* **Keep proprietary gateway APIs per backend** — status quo. Works, but every new backend multiplies bespoke code, and harnesses stay outside the agent graph.
* **Wrap harnesses via MCP instead** (`openclaw mcp serve`, Hermes MCP) — exposes harness capabilities as *tools*, not conversational agents; loses streaming UX, session semantics, and the approval flow. Complementary, not a substitute.
* **Wait for ACP Streamable HTTP transport standardization** — the [v1 Transports spec](https://agentclientprotocol.com/protocol/v1/transports) lists Streamable HTTP as a draft proposal in progress (with WS stream revisions already merged into the draft text via [#1124](https://github.com/agentclientprotocol/agent-client-protocol/pull/1124)), so this is closer than "unspecified future work" — but it is not yet normative, and neither OpenClaw nor Hermes serves it. The OpenClaw WS bridge already gives a remote-friendly path today under the Custom Transports clause, and the bridge interface can swap to Streamable HTTP later without API changes.

## Open Questions

* **Substrate + stdio-only ACP agents (Hermes, Codex, …)**: all backends use the in-sandbox `acp-shim` (see Implementation Details). Remaining sub-questions: shim delivery (baked into workload images vs. injected via init-container + shared volume, if Substrate workload templates allow it), and whether to wait for upstream Streamable HTTP adoption instead for some backends. The shim, once built, covers OpenClaw, Hermes, Codex, and any other stdio ACP agent uniformly.
* **Shim child policy**: one child per WS connection (clean lifecycle) vs. one long-lived child with exactly-one-client semantics (preserves in-memory ACP sessions — required for Hermes). Proposed default: long-lived, configurable per backend. This choice is coupled to the actor suspension model: if the default is long-running actors (suspended/resumed via Substrate checkpoint-restore rather than torn down between connections), a long-lived child is the natural fit, since the child — and its in-memory ACP sessions — is frozen and restored with the actor rather than respawned per connection. If instead actors are short-lived/per-connection, the per-WS-connection child policy aligns better and the long-lived option buys little. Revisit once the suspension model is settled.
* **Hermes session durability**: ACP sessions are in-memory per `hermes acp` process; a harness or bridge restart loses ACP session mappings even though Hermes' own task persistence survives. Should the bridge transparently start a new ACP session bound to the same kagent conversation, and how is that surfaced to the user?
* ~~**Gateway WS reachability**~~ — resolved: the atenet router's Envoy configuration explicitly enables WebSocket upgrades (`UpgradeConfigs: {UpgradeType: websocket}` in `cmd/atenet/internal/app/router/xds.go`), so both `openclaw acp --url wss://…` and an in-sandbox WS shim are reachable through the Substrate ingress path.
* **Auth model**: ACP defines an `authenticate` method, but OpenClaw's bridge authenticates to the gateway out-of-band via token. Should kagent standardize on out-of-band auth (Secret-resolved tokens, as today) and ignore ACP `authenticate`, or support both?
* **Bridge placement**: controller-managed subprocess vs. per-harness sidecar/Deployment. Sidecar isolates failure domains and scales naturally, but adds a pod per harness; subprocess is cheaper but couples bridge lifetime to the controller.
* **EP number**: placeholder `XXXX` pending issue creation; rename the file and update links before merging.
