# How A2A works in kagent

A2A ("Agent2Agent") is a **JSON-RPC 2.0 over HTTP** protocol. This document reconstructs
how it is implemented in kagent from the source, with real request-flow examples and
diagrams dissecting the interaction between agents.

---

## TL;DR — the mental model

A2A shows up in **two distinct layers** that are often conflated:

1. **The agent pod itself serves A2A.** Every Agent that declares `a2aConfig` becomes a
   Deployment + Service. Inside the pod, the runtime (Python `a2a-sdk` or Go ADK) exposes
   A2A on port **8080**. This is the *real* server.
2. **The controller is an A2A proxy / front door.** The kagent controller exposes
   `/api/a2a/{namespace}/{name}` on port **8083**. It's a *passthrough* — it authenticates,
   negotiates protocol version, validates share tokens, then forwards to the agent pod.
   This is what the UI and external callers hit.

Agent-to-agent calls *inside* the cluster mostly **skip the proxy** and go pod→pod directly
over Service DNS (`http://<agent>.<ns>:8080`). The proxy is the edge.

```
                          ┌─────────────────────────── kagent controller (:8083) ───┐
   UI / external          │  gorilla/mux router                                       │
   JSON-RPC client  ─────▶│  /api/a2a/{ns}/{name}  ──▶ handlerMux ──▶ Passthrough     │
                          │                            (auth, version, share-token)   │
                          └───────────────────────────────────┬──────────────────────┘
                                                               │  forwards (A2A client)
                                                               ▼
   Agent A pod (:8080)                              Agent B pod (:8080)
   ┌────────────────────────┐   direct pod→pod      ┌────────────────────────┐
   │ a2a-sdk / Go ADK server │   A2A over Svc DNS    │ a2a-sdk / Go ADK server │
   │   AgentExecutor         │ ────────────────────▶ │   AgentExecutor         │
   │   (LLM + RemoteA2ATool) │  http://B.ns:8080     │   (LLM + tools)         │
   └────────────────────────┘                        └────────────────────────┘
```

---

## 1. The protocol surface

Whatever serves A2A (controller proxy or agent pod) exposes the same shape:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/.well-known/agent-card.json` (Py) / `/.well-known/agent` (Go) | `GET` | **Discovery** — returns the AgentCard (name, skills, URL, capabilities) |
| `/` (or the path prefix) | `POST` | **JSON-RPC 2.0** — `message/send`, `message/stream`, `tasks/get`, `tasks/list`, … |

**Task lifecycle** (the state machine every call drives):

```
        message/send
            │
            ▼
       ┌─────────┐     ┌─────────┐     ┌──────────────┐
       │submitted│ ──▶ │ working │ ──▶ │  completed   │  ← terminal (success)
       └─────────┘     └────┬────┘     └──────────────┘
                            │  ├──────▶ │   failed     │  ← terminal (error)
                            │  └──────▶ │input_required│  ← HITL pause, resumable
                            │           └──────────────┘
                            ▼
                  streamed as SSE: TaskStatusUpdateEvent + TaskArtifactUpdateEvent
```

**Wire-version negotiation** matters here: kagent supports A2A `0.3` (legacy) and `1.0` (v1)
side by side. The controller picks the handler off a header —
`go/core/internal/utils/a2a_version.go:21` (`NegotiateA2AWireVersion`) reads `SvcParamVersion`;
empty/`v0.3` → legacy `a2av0.NewJSONRPCHandler`, `1.0` → `a2asrv.NewJSONRPCHandler`. The agent
card even advertises **both** interfaces (`GetA2AAgentCard`,
`go/core/internal/controller/translator/agent/utils.go:11`).

A2A library dependencies:
- Go: `github.com/a2aproject/a2a-go v0.3.15` (legacy), `github.com/a2aproject/a2a-go/v2 v2.3.1`
  (current), `trpc.group/trpc-go/trpc-a2a-go v0.2.5`.
- Python: `a2a-sdk[http-server]>=0.3.23`.

---

## 2. From CRD to a running A2A endpoint

The relationship "this agent speaks A2A" is declarative:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: search-agent
  namespace: default
spec:
  declarative:
    a2aConfig:                      # ← this line makes it an A2A server
      skills:
        - id: web-search
          name: "Web Search"
          description: "Searches the web"
          tags: ["search"]
```

What the controller does with it:

```
Agent CRD (a2aConfig set)
   │
   │  reconciler + manifest_builder
   ▼
┌──────────────────────────────────────────────────────────┐
│ Deployment  (runs Python/Go runtime, A2A server on :8080)  │
│ Service     (ClusterIP :8080,                              │
│              AppProtocol = "kgateway.dev/a2a")             │  ← manifest_builder.go:580
└──────────────────────────────────────────────────────────┘
   │
   │  A2ARegistrar watches Agent objects (informer)
   ▼
Registers a proxy handler in the controller mux keyed "default/search-agent"
   • builds an A2A client pointed at http://search-agent.default:8080
   • rewrites the card's URLs → http://<controller>:8083/api/a2a/default/search-agent/
```

- The Service + `kgateway.dev/a2a` app-protocol:
  `go/core/internal/controller/translator/agent/manifest_builder.go:580`.
- The registrar wiring (informer add/update/delete → `upsertAgentHandler`):
  `go/core/internal/a2a/a2a_registrar.go:104` and `:213`.
- The proxy route registration: `go/core/internal/httpserver/server.go:343`
  (`PathPrefix("/api/a2a/{namespace}/{name}")`).
- Registrar/handler wiring at startup: `go/core/pkg/app/app.go:675`
  (`NewA2AHttpMux`, `NewA2ARegistrar`); `--a2a-base-url` default `http://127.0.0.1:8083`.

---

## 3. Real flow #1 — UI / external client invokes an agent

This is the "front door" path through the controller proxy.

```
┌────────┐   1. POST /api/a2a/default/search-agent   ┌─────────────────────────────┐
│ Client │ ─────────────────────────────────────────▶│ controller :8083            │
│ (UI)   │      JSON-RPC: {"method":"message/send"}   │  gorilla/mux                │
└────────┘                                            └──────────────┬──────────────┘
     ▲                                                               │ 2. mux.Vars → ns=default,
     │                                                               │    name=search-agent
     │                                                               ▼
     │                                              ┌─────────────────────────────────┐
     │                                              │ handlerMux.ServeHTTP             │
     │                                              │  routeKey("default/search-agent")│  a2a_handler_mux.go:119
     │                                              │  → lookup handler                │
     │                                              └───────────────┬─────────────────┘
     │                                                              │ 3. negotiate version
     │                                                              │    + auth middleware
     │                                                              ▼
     │                                              ┌─────────────────────────────────┐
     │                                              │ PassthroughRequestHandler        │  passthrough_handler.go
     │                                              │  • validateShareContext          │
     │                                              │  • injectInitiatedBy (user id)   │
     │                                              │  • client.SendMessage()          │
     │                                              └───────────────┬─────────────────┘
     │                                                              │ 4. upstreamAuthInterceptor
     │                                                              │    adds X-User-Id, auth,
     │                                                              │    W3C traceparent
     │                                                              ▼
     │                                              ┌─────────────────────────────────┐
     │                                              │ Agent pod :8080                  │
     │                                              │  a2a-sdk DefaultRequestHandler   │
     │                                              │  → A2aAgentExecutor.execute()    │  _agent_executor.py:160
     │                                              └───────────────┬─────────────────┘
     │                                                              │ 5. convert A2A→ADK,
     │                                                              │    Runner.run_async()
     │  6. SSE stream of TaskStatusUpdateEvent /                    │    LLM loop runs
     └──── TaskArtifactUpdateEvent flows all the way back ──────────┘
```

**The request the client actually sends** (A2A `message/send`, JSON-RPC 2.0):

```json
POST /api/a2a/default/search-agent
Content-Type: application/json
X-User-Id: jm@solo.io

{
  "jsonrpc": "2.0",
  "id": "1",
  "method": "message/send",
  "params": {
    "message": {
      "messageId": "c0ffee...",
      "role": "user",
      "parts": [{ "kind": "text", "text": "Find recent kagent releases" }],
      "contextId": "session-abc"        // ← becomes the ADK session id
    }
  }
}
```

**Key dissection points:**

- **`contextId` == session.** On the Python side `convert_a2a_request_to_adk_run_args` maps
  `context_id → session_id` (`request_converter.py:20`). The A2A "context" *is* the
  conversation/session. The executor creates the session in the kagent backend over HTTP if
  it doesn't exist (`_session_service.py`).
- **Identity travels in headers, not the body.** `KAgentRequestContextBuilder.build`
  (`kagent-core/.../a2a/_requests.py:30`) pulls `x-user-id` and tags `x-kagent-source` so the
  backend knows the call was agent-originated vs. human.
- **The executor is the bridge.** `A2aAgentExecutor.execute` (`_agent_executor.py:160`) is the
  heart on the Python side: convert request → resolve a fresh `Runner` → `run_async` → for each
  ADK event, `convert_event_to_a2a_events` → `event_queue.enqueue_event` (which the SDK
  serializes as SSE). The Go ADK has the exact analog in `KAgentExecutor.Execute`
  (`go/adk/pkg/a2a/executor.go:103`): emit `submitted`, loop `r.Run(...)`, convert GenAI parts
  ↔ A2A parts, emit `completed`/`failed`/`input_required`.

---

## 4. Real flow #2 — Agent calls Agent

You make Agent A able to call Agent B by declaring B as a **tool of type `Agent`**:

```yaml
kind: Agent
metadata: { name: orchestrator, namespace: default }
spec:
  declarative:
    tools:
      - type: Agent
        agent:
          name: search-agent           # B, can be cross-namespace
          namespace: default
```

The compiler turns that into a remote-agent entry in A's runtime config:

```
compiler.go (getAgentTools)                       toolAgentURL(B)            compiler.go:91/329
   tool.Agent != nil
   └─▶ resolve B, check AllowedNamespaces          normal:  http://search-agent.default:8080
   └─▶ cfg.RemoteAgents += RemoteAgentConfig{       sandbox: http://kagent-controller.kagent
         Name: "search_agent",                               :8083/api/a2a-sandboxes/default/search-agent
         Url:  <toolAgentURL(B)>,
         Description, Headers }
```

At runtime A's process wraps each `RemoteAgentConfig` as a callable tool —
`KAgentRemoteA2ATool` in Python (`_remote_a2a_tool.py:158`) / `NewKAgentRemoteA2ATool` in Go
(`go/adk/pkg/tools/remote_a2a_tool.go:163`). To A's LLM it looks like an ordinary function.
When the model calls it, an A2A `message/send` fires at B. Note the URL is **B's Service
directly** — normal agent→agent traffic does *not* loop back through the controller proxy.

```
   Agent A pod (:8080)                                        Agent B pod (:8080)
   ┌─────────────────────────────┐                           ┌──────────────────────────┐
   │ Runner.run_async (A's LLM)   │                           │ A2aAgentExecutor.execute │
   │   │                          │                           │   │                      │
   │   │ LLM emits tool_call      │                           │   │                      │
   │   ▼  "search_agent(query)"   │                           │   │                      │
   │ KAgentRemoteA2ATool          │   message/send (JSON-RPC) │   │                      │
   │  • A2ACardResolver.get_card  │ ─────────────────────────▶│ DefaultRequestHandler    │
   │    GET /.well-known/agent... │   POST http://search-     │   │                      │
   │  • A2AClientFactory.create   │        agent.default:8080  │   │                      │
   │  • client.send_message(msg)  │                           │   ▼ runs B's LLM loop    │
   │       with headers:          │                           │ enqueue events           │
   │       x-user-id              │◀───── result text ────────│  TaskStatus/Artifact     │
   │       x-kagent-parent-ctx-id │      (or SSE stream)      │                          │
   │       x-kagent-root-ctx-id   │                           │                          │
   │   ▼                          │                           │                          │
   │ tool returns text → LLM      │                           │                          │
   │ continues A's turn           │                           │                          │
   └─────────────────────────────┘                           └──────────────────────────┘
```

**The crucial detail: session lineage.** Because each A2A `contextId` is its own session,
kagent threads parent/child identity through headers so the whole tree is correlatable:

```
PARENT_CONTEXT_ID_HEADER = "x-kagent-parent-context-id"   # the immediate caller's session
ROOT_CONTEXT_ID_HEADER   = "x-kagent-root-context-id"     # the top of the whole chain
```

`_build_lineage_headers` (`_remote_a2a_tool.py:256`): parent = A's current `session.id`; root =
inbound root header if present, else parent. So in a 3-deep chain A→B→C, C receives `parent=B`,
`root=A`. (Go mirrors this at `remote_a2a_tool.go:49`.) This is how kagent reconstructs the
multi-agent call tree and propagates the human user id all the way down.

---

## 5. The trickiest interaction — Human-in-the-Loop across agents

When B needs approval (a dangerous tool), it doesn't just block — it returns the task in
`input_required`, and A must *surface that up to the human* and then *resume* B. This is a
**two-phase** A2A exchange over the same `contextId`/`taskId`.

```
 Human        Agent A (orchestrator)                 Agent B (has HITL tool)
   │                  │                                       │
   │ "do X"           │                                       │
   ├─────────────────▶│ LLM → calls search_agent tool         │
   │                  │  ── message/send ───────────────────▶ │ runs, hits tool needing approval
   │                  │                                        │ emits Task(state=input_required)
   │                  │ ◀── Task{input_required, taskId} ───── │   ⟂ B is now PAUSED, task stored
   │                  │                                        │
   │                  │ _handle_input_required:                │
   │                  │  tool_context.request_confirmation(    │   _remote_a2a_tool.py:365
   │                  │    payload={task_id, context_id,       │
   │                  │            subagent_name, hitl_parts}) │
   │ ◀── A asks "approve search_agent's action?" ───────────  │
   │                  │  (A itself is now input_required)      │
   │ "approve" ──────▶│                                        │
   │                  │ _handle_resume: build decision msg     │   _remote_a2a_tool.py:404
   │                  │  ── message/send (same taskId, ───────▶│ _process_hitl_decision:
   │                  │     DataPart{decision:"approve"}) ─────│   match pending confirmation →
   │                  │                                        │   FunctionResponse(confirmed=true)
   │                  │ ◀── Task{completed, result} ────────── │   B's LLM resumes & finishes
   │ ◀── final answer │                                        │
```

The decision types are first-class: `approve` / `reject` / `batch` (per-tool mixed decisions) —
`kagent-core/.../a2a/_consts.py`, processed in `_agent_executor.py:417`
(`_process_hitl_decision`). The Go executor builds the resume message via
`BuildResumeHITLMessage` (`go/adk/pkg/a2a/executor.go`). This is why A2A here is *task-based*,
not just request/response — the `taskId` is the resumable handle.

---

## 6. Streaming (SSE)

For `message/stream`, run config flips to `StreamingMode.SSE` (`request_converter.py:34`). As
the ADK runner yields events, each is converted and `enqueue_event`'d immediately; the SDK
serializes them as SSE frames.

```
runner.run_async ──┬─▶ adk_event (partial=True)  ──▶ A2A event ──▶ SSE frame  (streamed live)
                   ├─▶ adk_event (partial=True)  ──▶ A2A event ──▶ SSE frame
                   └─▶ adk_event (partial=False) ──▶ aggregated into final result + SSE frame
```

Partial chunks carry `adk_partial=True` metadata (`event_converter.py:73`) so they're streamed
but **not** double-counted in the final aggregated result (`_agent_executor.py:597`). `httpx-sse`
is pinned specifically to handle U+2028/U+2029 line terminators in streamed JSON.

---

## 7. Cross-namespace access control

A2A references across namespaces are gated Gateway-API style. The *target* agent declares who
may call it:

```yaml
kind: Agent
metadata: { name: search-agent, namespace: shared }
spec:
  allowedNamespaces:
    from: All        # All | Same (default) | Selector
```

The reconciler enforces it when compiling A's tools — `AllowsNamespace(...)` in
`reconciler.go:789`; a disallowed reference fails reconciliation rather than failing at call
time. Definition: `go/api/v1alpha2/common_types.go:32`.

---

## 8. Sessions, tasks, and persistence

- **Session service** (`kagent-adk/.../_session_service.py`) creates/reads/appends ADK events to
  the kagent backend over HTTP: `POST /api/sessions`, `GET /api/sessions/{id}`,
  `POST /api/sessions/{id}/events`.
- **Task store** (`kagent-core/.../a2a/_task_store.py`) persists A2A tasks to the backend at
  `/api/tasks` (`save`/`get`/`delete`, plus `wait_for_save` for event-based sync). The stored
  task is what makes `input_required` resumable.
- **Request context** (`kagent-core/.../a2a/_requests.py`) is where header-derived identity
  (`x-user-id`) and source tagging (`x-kagent-source`) get attached to the session.

---

## Key file map

| Concern | File |
|---|---|
| Proxy routes | `go/core/internal/httpserver/server.go:343` |
| Proxy dispatch by ns/name | `go/core/internal/a2a/a2a_handler_mux.go:119` |
| Passthrough + share/identity | `go/core/internal/a2a/passthrough_handler.go` |
| Registrar (CRD→handler) | `go/core/internal/a2a/a2a_registrar.go:213` |
| Outbound auth/trace interceptor | `go/core/internal/a2a/client_interceptors.go:35` |
| Wire-version negotiation | `go/core/internal/utils/a2a_version.go:21` |
| Agent card generation | `go/core/internal/controller/translator/agent/utils.go:11` |
| Agent-as-tool URL + RemoteAgents | `go/core/internal/controller/translator/agent/compiler.go:91` |
| Service + a2a app-protocol | `go/core/internal/controller/translator/agent/manifest_builder.go:580` |
| Startup wiring (mux + registrar) | `go/core/pkg/app/app.go:675` |
| Agent CRD A2A fields | `go/api/v1alpha2/agent_types.go:211` (`A2AConfig`), `:584` (skills) |
| Cross-namespace policy | `go/api/v1alpha2/common_types.go:32` |
| **Py** A2A app wiring | `python/packages/kagent-adk/src/kagent/adk/_a2a.py:155` |
| **Py** executor (bridge) | `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py:160` |
| **Py** remote-agent tool + lineage/HITL | `python/packages/kagent-adk/src/kagent/adk/_remote_a2a_tool.py:158` |
| **Py** request/event converters | `python/packages/kagent-adk/src/kagent/adk/converters/{request,event}_converter.py` |
| **Py** session service | `python/packages/kagent-adk/src/kagent/adk/_session_service.py` |
| **Py** task store / request context | `python/packages/kagent-core/src/kagent/core/a2a/{_task_store,_requests}.py` |
| **Go** agent-side server | `go/adk/pkg/a2a/server/server.go:35` |
| **Go** executor | `go/adk/pkg/a2a/executor.go:103` |
| **Go** remote-agent tool | `go/adk/pkg/tools/remote_a2a_tool.go:163` |
| **Go** message converter | `go/adk/pkg/a2a/converter.go` |
