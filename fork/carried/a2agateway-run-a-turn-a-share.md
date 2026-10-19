---
id: a2agateway-run-a-turn-a-share
fork: kagent
theme: identity-authorizer
category: bugfix
status: carried
upstream:
  outcome: declined
  refs: [kagent-dev/kagent#3066, kagent-dev/kagent#3189]
  exit: 'drop the read-write share path; fork PR #629 watches'
depends_on: []
commit: 9c4fc975b
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(a2agateway): run a turn a share of the session authorizes in its owner's session` | the runtime keys its session on `x-user-id` and the context id, and the runtime dialer forwarded the visitor there, so a turn a session share authorized started an empty session in the conversation's context: a Slack collaborator klaus-gateway lets into a thread (giantswarm/klaus-gateway#356) got an agent with no history (seen on a production installation). The dialer now names the share's owner, as the HTTP A2A handler does for a session share; the visitor stays the person of the turn (`x-kagent-user`) and keeps their own credentials (MCP and model calls). What the runtime keys on `x-user-id` follows the owner too, as before klaus-gateway#356: the long-term memory of the agent and the sessions of its sub-agents | kagent-dev/kagent#3066 (`71b1e7857`), carried as the pull request has it |
