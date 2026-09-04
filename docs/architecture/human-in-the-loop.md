# Human in the Loop

Kagent human-in-the-loop (HITL) lets an agent pause an A2A task, request a human
decision, and resume the exact paused operation. It is a framework-independent
A2A extension; framework confirmation events are adapter details.

The extension URI is:

```text
https://kagent.dev/extensions/hitl/v1
```

An Agent Card advertises that URI as an optional capability. A client activates it
with the standard `A2A-Extensions` header.

## Protocol

HITL is an exchange on an ordinary A2A task:

1. The agent emits an `input-required` status whose status message metadata
   contains a HITL request.
2. The gateway persists the event and quiesces the Actor at that exact boundary.
3. The client sends a user message to the same task and context with a structured
   decision.
4. The runtime adapter validates the decision and resumes its stored continuation.

No HITL RPC, session, or second task model exists. The response continues the
same A2A task; it is not a prompt asking the model to reconstruct the pending
operation.

## Request forms

Version 1 supports two request kinds:

- **tool approval** identifies one or more pending tool calls. Each decision
  explicitly approves or rejects a call and may include a rejection reason.
- **ask user** identifies one or more questions and returns an answer for each.

Requests and responses live in the A2A status message metadata under the
extension URI key. The message also includes the extension URI in its `extensions`
array. IDs correlate each decision with its exact pending call or question.
Clients must preserve unknown metadata and return a complete, unambiguous
response.

The payload types are:

```json
{
  "type": "tool_approval_request",
  "hint": "Allow these calls?",
  "tools": [
    {"id": "tool-1", "call_id": "call-1", "name": "deploy", "args": {}}
  ]
}
```

```json
{
  "type": "tool_approval_response",
  "approvals": [
    {"id": "tool-1", "approved": false, "rejection_reason": "not production"}
  ]
}
```

Ask-user requests use `type: "ask_user_request"`, an `id`, and an array of
framework-neutral question objects. Responses use `type: "ask_user_response"`,
the same `id`, and an `answers` array whose entries contain string arrays. A
request may also carry `nested` child-agent correlation: `subagent_name`,
`task_id`, `context_id`, and the child's pending tools.

The server rejects responses that use the wrong task/context, omit required
decisions, duplicate IDs, or answer an operation that is no longer pending.

## Ownership

| Component | Responsibility |
| --- | --- |
| Agent Card | Advertise the exact versioned extension URI |
| A2A client | Activate the extension, render requests, and continue the same task |
| Public gateway | Route and durably persist the interaction |
| Harness adapter | Translate public request/response data to native pause/resume state |
| Remote A2A tool | Preserve child task/context IDs for nested continuation |

The kagent Go harness maps the contract to ADK confirmations. Remote A2A tools in
the Go and Python ADKs can retain a child continuation when a subagent requests
input.

The current v2 MCP tool-binding API does not expose per-tool approval policy, so
users cannot configure generic MCP approval gating through `AgentTemplate` today.
The extension remains valid for runtimes and tools that actually produce a pause,
including ask-user and nested A2A continuation.

For the base extension mechanism, see the
[A2A extension specification](https://a2a-protocol.org/latest/specification/#46-extensions).
