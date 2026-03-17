# Subagent Live Activity Viewing

Allows users to see what a subagent is doing **during and after** a parent agent's execution, without leaving the parent chat. An "Activity" panel appears inline on each subagent tool call card.

---

## Overview

When a parent agent delegates to a subagent via `KAgentRemoteA2ATool`, the subagent's session is created in the database under the same user. The UI polls that session for live events and renders them as a nested chat thread inside the parent conversation.

---

## Request Flow

### 1. Session ID stamping (Python, parent agent)

`KAgentRemoteA2ATool` pre-generates a `context_id` (UUID) in `__init__` before the tool runs. The event converter stamps this ID as `adk_subagent_session_id` metadata onto the `function_call` DataPart, so the UI knows the subagent session ID as soon as the LLM emits the call — before the tool actually executes.

### 2. User ID passthrough (Python, A2A interceptor)

`_SubagentInterceptor` is registered at A2A client construction time. It injects two headers on every outgoing A2A request:

| Header | Value | Purpose |
|--------|-------|---------|
| `x-user-id` | parent session's `user_id` | Scopes the subagent session to the same DB user |
| `x-kagent-source` | `subagent` | Tags the session as subagent-originated |

> **Note on A2A SDK:** `A2AClient.add_request_middleware()` appends to `Client._middleware`, which is never read by the transport. Interceptors must be passed to `ClientFactory.create(interceptors=[...])` at construction time to be registered on `JsonRpcTransport.interceptors`.

### 3. Subagent session creation (Python, subagent runtime)

The subagent's `KAgentRequestContextBuilder` reads both headers from `context.state["headers"]` (populated automatically by the A2A SDK's `DefaultCallContextBuilder`):

- `x-user-id` → sets `context.user = KAgentUser(user_id=...)` so the session is stored under the correct user
- `x-kagent-source` → stored in `context.state["kagent_source"]`

`_prepare_session` reads `kagent_source` directly from the request context. The value is threaded through `state["source"]` → `KAgentSessionService.create_session()` → Go `POST /api/sessions` with `"source": "subagent"` in the body.

### 4. Session storage (Go)

The `Session` model has a `Source *string` column (`"subagent"` or `nil`). `ListSessionsForAgent` excludes subagent sessions (`WHERE source IS NULL OR source != 'subagent'`), so they don't appear in the agent's session history sidebar. `GetSession` is unfiltered — the UI can still fetch them by ID.

### 5. UI polling

`AgentCallDisplay` shows an "Activity" button when `subagentSessionId` is present. Clicking it opens `SubagentActivityPanel`, which polls `getSubagentSessionWithEvents(sessionId)` every 2 seconds while the subagent is running, stopping once `isComplete`.

Depth is tracked via `ActivityDepthContext` (max depth: 3) to prevent unbounded nesting.

---

## Data Flow Summary

```
Parent LLM emits function_call for subagent tool
    → event_converter stamps adk_subagent_session_id on DataPart
    → UI extracts sessionId from DataPart metadata
    → "Activity" button appears on AgentCallDisplay

KAgentRemoteA2ATool executes
    → _SubagentInterceptor adds x-user-id + x-kagent-source: subagent
    → A2A request → subagent pod

Subagent pod receives request
    → KAgentRequestContextBuilder extracts headers
    → _prepare_session creates session in DB:
        user_id = admin@kagent.dev (from x-user-id)
        source  = "subagent"      (from x-kagent-source)

UI SubagentActivityPanel polls /api/sessions/{id} + /api/sessions/{id}/tasks
    → Renders subagent's messages as nested chat thread
    → Stops polling once parent function_response received
```

---

## Session Visibility

| Query | Includes subagent sessions? |
|-------|----------------------------|
| `GET /api/sessions/agent/{ns}/{name}` (session history sidebar) | No — filtered by `source != 'subagent'` |
| `GET /api/sessions/{id}` (direct fetch by ID) | Yes — unfiltered |
| `GET /api/sessions/{id}/tasks` | Yes — unfiltered |
