---
id: runtime-name-the-session-on-every-model-call
fork: kagent
theme: identity-authorizer
category: feature
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file with identity headers
depends_on: [runtime-name-the-agent-and-the-user]
commit: 01864607b
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(runtime): name the session on every model call` | the model calls name the agent and the person of the turn (the row "name the agent and the user" above) but not the conversation, so the platform's `llm` listener cannot price one session (a Dev Portal conversation, a Slack thread) and a tenant's report cannot list its most expensive sessions. The a2a gateway's runtime dialer forwards the id of the `Session` it routed the turn to (the header name is unchanged) as `x-kagent-agent-instance-id`, the name of the header that routes a call to an instance, never a value the caller sent; the Go ADK's HTTP transport sets it on every model call of that turn and removes it from any call outside a turn, so no ModelConfig can name a session. An accounting identity, never an authorization input; only the model and embedding clients use that transport, so a sub-agent's A2A call is never routed by it. The Claude harness sends no per-turn headers yet | to file — prepared in the fork, after the row above it, whose upstream filing it extends |
