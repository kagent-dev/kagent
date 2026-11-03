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

Version 1 supports three request kinds:

- **tool approval** identifies one or more pending tool calls. Each decision
  explicitly approves or rejects a call and may include a rejection reason.
- **ask user** identifies one or more questions and returns an answer for each.
- **URL elicitation** asks the person to open a URL outside the conversation,
  such as an MCP server's sign-in link (MCP URL mode elicitation).

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

A URL elicitation request names the MCP `server` that asked, its `message`, the
`url`, and the server's `elicitation_id` when it sent one:

```json
{
  "type": "url_elicitation_request",
  "id": "tool-1",
  "server": "github",
  "message": "Sign in to github",
  "url": "https://gateway.example/oauth/start?s=1",
  "elicitation_id": "e1"
}
```

```json
{"type": "url_elicitation_response", "id": "tool-1", "action": "accept"}
```

`action` is `accept`, `decline`, or `cancel`. Unlike MCP, where `accept` only
means the person agreed to open the URL, `accept` here means the person completed
the interaction there: the paused tool call runs again. `decline` and `cancel`
reject the call. The URL is `https`, or `http` on a loopback host, with no user
info; the runtime drops any other URL. Form mode elicitations are declined,
because what the person typed would reach the model's context.

A pause carries one URL. When a server asks for several at once, the runtime
pauses on the first and leaves the others unanswered when the call runs
again, so the server asks for them again. A call that already resumed from a
confirmation cannot pause a second time: a further URL elicitation fails the
call.

When a URL elicitation shares its pause with other calls, the pause is a tool
approval, and the waiting call's tool carries the request as `url_elicitation`.
Approving that tool accepts it.

The server rejects responses that use the wrong task/context, omit required
decisions, duplicate IDs, or answer an operation that is no longer pending.

The status message also carries a text part that describes the pause. Every
runtime composes that text the same way, and a client receives it whether or not
it activated the extension:

- an `ask_user` pause renders its questions, joined with `; `;
- a URL elicitation renders its message followed by the URL, so a client that
  does not know `url_elicitation_request` still shows the link; in a tool
  approval, that text is the waiting tool's hint;
- any other pause renders the hints its tools supplied, followed by the tool
  names in parentheses: `Deleting this file requires approval (delete_file,
  restart_pod)`;
- with no hints, it names the tools: `Approval is required for tool(s):
  delete_file`;
- a pause that carries no tool call renders `Human input is required before the
  agent can continue.`

The `hint` field of an activated payload holds that same string.

## Configuring built-in questions

The Go and Python ADK runtimes enable the built-in `ask_user` tool by default.
To disable it across agents managed by the controller, use these Helm values:

```yaml
config:
  runtimeDefaults:
    builtinTools:
      disableAskUser: true
```

In a controller configuration file, the same setting is
`runtimeDefaults.builtinTools.disableAskUser` (without the Helm `config` prefix).
The controller passes it to ADK runtimes as
`runtime.builtin_tools.disable_ask_user`. Omitted or `false` preserves the enabled
behavior, following the existing `disableBash` convention.

The shared runtime setting also applies to Go Shared subagents. A BYO image that
uses the ADK configuration contract can honor the same setting. Native Claude
and Codex harnesses and custom BYO implementations manage their own tools.
There is no per-template or Harness override.

Disabling this built-in tool does not disable MCP tool approvals or questions
from remote A2A agents, so it does not guarantee that every task can complete
without human input.

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
input; a subagent's URL elicitation is not forwarded yet and fails the parent's
call.

For an MCP server on protocol 2026-07-28, which asks through an `input_required`
tool result, the Go harness keeps the server's request state and input responses
in runtime session state while the task is paused, never in the public request,
and sends them back when the call runs again.

A rejected call is reported to the model as `Tool call was rejected by user.`,
followed by `Reason: <reason>` when the decision carried a `rejection_reason`.
The Go and Python ADKs say the same, so a model can answer the question behind a
rejection and ask for the call again.

The current MCP tool-binding API does not expose per-tool approval policy, so
users cannot configure generic MCP approval gating through `AgentTemplate` today.
The extension remains valid for runtimes and tools that actually produce a pause,
including ask-user and nested A2A continuation.

For the base extension mechanism, see the
[A2A extension specification](https://a2a-protocol.org/latest/specification/#46-extensions).
