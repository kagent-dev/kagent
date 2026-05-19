# A2A Subagents

Kagent allows users to add subagents (other agents running on Kagent or remotely) as tools to a main agent, connected via the A2A protocol. This feature is enabled by `KAgentRemoteA2ATool` (`python/packages/kagent-adk/src/kagent/adk/_remote_a2a_tool.py`), kagent's custom replacement for the upstream `AgentTool(RemoteA2aAgent(...))` pairing. 

It directly manages the A2A conversation with a remote subagent and adds three things the upstream lacks: HITL propagation, live activity viewing, and user ID forwarding.

See [human-in-the-loop.md](human-in-the-loop.md) for HITL details.

---

## How it works

Each parent A2A request creates a fresh `Runner` and fresh tool instances. `KAgentRemoteA2ATool.__init__` generates a UUID (`_last_context_id`) that is used as the A2A `context_id` for every message sent to the subagent. On the subagent side, this `context_id` becomes the session ID.

`run_async` has two phases:

- **Phase 1** (normal call): sends the request to the subagent and handles the response — returning the result, pausing for HITL if the subagent returns `input_required`, or returning an error string.
- **Phase 2** (HITL resume): reads the stored `task_id`/`context_id` from `tool_context.tool_confirmation.payload` and forwards the user's decision (approve / reject / batch / ask-user answers) to the subagent's pending task.

On success, `run_async` returns:
```python
{"result": str, "subagent_session_id": str}           # normal
{"result": str, "subagent_session_id": str,
 "kagent_usage_metadata": dict}                        # with usage
{"status": "pending", "waiting_for": "subagent_approval", ...}  # HITL pause
```

`KAgentRemoteA2AToolset` is a thin `BaseToolset` wrapper whose only job is ensuring the owned `httpx.AsyncClient` is closed when the runner shuts down — ADK's cleanup path only discovers `BaseToolset` instances, not bare `BaseTool` instances.

---

## User ID and session tagging

`_SubagentInterceptor` is registered on the A2A client at construction time and injects two headers on every outgoing request:

| Header | Value | Purpose |
|---|---|---|
| `x-user-id` | parent session's user ID | Scopes the subagent DB session to the same user |
| `x-kagent-source` | `"agent"` | Hides the session from the agent's session history sidebar |

> Interceptors must be passed to `ClientFactory.create(interceptors=[...])` — `A2AClient.add_request_middleware()` appends to a list that the transport never reads.

On the subagent side, `KAgentRequestContextBuilder` reads these headers and passes them through to `_prepare_session`, which calls `KAgentSessionService.create_session()` with `source="subagent"`. The Go layer stores this in a `Source` column and excludes such sessions from `ListSessionsForAgent`.

The `external-bearer` auth mode keeps this user-continuity boundary generic for human users: A2A/subagent calls may continue to use `X-User-Id` for user continuity, while forwarding the inbound bearer token is configurable and opt-in with `AUTH_EXTERNAL_BEARER_PROPAGATE_TOKEN` / `controller.auth.externalBearer.propagateToken`. Validated service actors do not receive a human-looking `X-User-Id` by default; they require explicit service-actor semantics and local A2A policy bounds.

---

## External-bearer service actors and A2A policy

For `external-bearer`, kagent treats RFC 7662 `active: true` as provider-side token validation, then requires the configured local policy's top-level resource-binding controls (`requiredScopes`, `allowedAudiences`, or `allowedIssuers`) to pass before accepting the token. The A2A mux derives the target namespace, name, and workload type (`agent` or `sandbox`) from the request path before dispatching to the target handler.

Human-user tokens that pass top-level policy also pass this A2A-specific service policy check and continue through normal A2A handling. Service/client-credentials tokens must match a configured `serviceActors[*].match.allOf` policy entry and then match an `allowedA2A` target. Service actors are denied non-A2A API routes by default.

Minimal policy with no service actor allowlist:

```json
{"allowedAudiences":["kagent"],"requiredScopes":["kagent:a2a"]}
```

Example policy targets:

```json
{
  "requiredScopes": ["kagent:a2a"],
  "allowedAudiences": ["kagent"],
  "serviceActors": {
    "ci-runner": {
      "match": {
        "allOf": [
          { "claim": "client_id", "value": "ci-client" },
          { "claim": "grant_type", "value": "client_credentials" },
          { "claim": "scope", "contains": "kagent:a2a" }
        ]
      },
      "allowedA2A": [
        { "namespace": "kagent", "name": "release-bot", "workloadType": "agent" },
        { "namespace": "kagent", "name": "debug-sandbox", "workloadType": "sandbox" },
        { "namespace": "observability", "name": "*", "workloadType": "*" }
      ]
    }
  }
}
```

Expected behavior:

| Caller/token | Target | Result |
|---|---|---|
| Human user token with matching top-level policy | Any resolved A2A target | Allowed by this service-actor policy layer. |
| `ci-runner` service token | `/api/a2a/kagent/release-bot` | Allowed. |
| `ci-runner` service token | `/api/a2a/kagent/other-agent` | Denied with `403`. |
| `ci-runner` service token | `/api/a2a-sandboxes/kagent/debug-sandbox` | Allowed because `workloadType` is `sandbox`. |
| `ci-runner` service token | `/api/a2a/observability/<any-name>` or sandbox equivalent | Allowed by the whole-field wildcards. |
| `ci-runner` service token | `/api/me` or another non-A2A route | Denied with `403`. |

MCP-to-A2A invocation uses the same A2A request handling path, so bearer propagation and human user continuity should be validated there as the regression surface for subagent calls.

---

## Live activity viewing

The UI can show what a subagent is doing in a live panel before it finishes. This works because the session ID is known before the tool runs:

Before the run loop, `A2aAgentExecutor` builds a `{tool_name → session_id}` map from all tools implementing the `SubagentSessionProvider` protocol (`subagent_session_id` property). The event converter stamps this as `kagent_subagent_session_id` metadata on each `function_call` DataPart as soon as the LLM emits the call. The UI reads it immediately and begins polling `/api/sessions/{id}` every 2 seconds, rendering the subagent's events as a nested chat thread. Nesting is capped at depth 3.

The map is keyed by tool name because within one parent request, all calls to the same subagent tool intentionally share one `context_id` — giving the subagent conversation continuity across sequential invocations. A fresh `context_id` is generated on the next parent request when the runner rebuilds.

When sending session requests to Go backend, take note that:

| Session query | Includes subagent sessions? |
|---|---|
| `GET /api/sessions/agent/{ns}/{name}` | No — filtered by `source != 'agent'` |
| `GET /api/sessions/{id}` | Yes |
| `GET /api/sessions/{id}/tasks` | Yes |
