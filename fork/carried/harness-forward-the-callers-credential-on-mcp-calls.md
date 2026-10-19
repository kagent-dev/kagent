---
id: harness-forward-the-callers-credential-on-mcp-calls
fork: kagent
theme: mcp-forwarder
category: feature
status: carried
commits:
  - 'feat(harness): forward the caller''s credential on MCP calls from the Claude harness'
  - 'fix(harness): refuse forwarder paths with dot segments or encoded slashes'
  - 'fix(harness): check the raw forwarder path and forward only transport headers'
upstream:
  outcome: open
  refs: [kagent-dev/kagent#3018, kagent-dev/kagent#3257]
  exit: 'reconcile against #3257, then #3018 or close'
depends_on: []
verified: 2026-10-19
---
# feat(harness): forward the caller's credential on MCP calls from the Claude harness

## Change

`feat(harness): forward the caller's credential on MCP calls from the Claude harness` (with `fix(harness): refuse forwarder paths with dot segments or encoded slashes` and `fix(harness): check the raw forwarder path and forward only transport headers`)

## Reason

a coding agent on the `claude` Harness must call muster as the invoking person: the claude executor never read the A2A `authorization`, so the harness could reach MCP servers with static credentials only; gated on `KAGENT_PROPAGATE_TOKEN` like the ADK, the credential rides a per-turn loopback forwarder and is dropped before a turn parks; the forwarded path stays absolute for an upstream URL without a path, a path with a dot segment, an encoded slash, backslash or `%`, a `;` or a control byte is refused before it is matched (D23), and only the transport and trace headers of Claude's request reach the server

## History

kagent-dev/kagent#3018 (`a9617a48d`), carried as the pull request has it; the two forwarder fixes are to file on top of it
