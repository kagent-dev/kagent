---
id: harness-claude-reaches-actor-jwt-hosts-without-the
fork: kagent
theme: credential-injection
category: feature
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3018]
  exit: 'with #3018, after harness-send-the-placeholder-to-actor-jwt-hosts'
depends_on: [harness-send-the-placeholder-to-actor-jwt-hosts, harness-codex-and-omp-send-the-placeholder-to-actor]
commit: giantswarm/kagent-upstream#717
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(harness): Claude reaches actor-JWT hosts without the forwarder` | with the placeholder sent to actor-JWT hosts, the forwarder adds nothing to a call to muster that the egress gateway does not replace, yet `mcp.json`, readable by Claude's user and so by every process of the sandbox, still carried the forwarder's loopback token, which any of them could use during the turn. The harness now rewrites `mcp.json` before each turn's Claude process starts and writes a server on one of the turn's actor-JWT hosts without a tool selection with its compiled URL and headers and the placeholder bearer, so a Session whose every MCP server is on one holds no loopback token in the sandbox. The claude, codex and omp harnesses build a direct server's headers with one rule, `workspace.ActorJWTHeaders`. `TestMCPConfigLeavesTheLoopbackTokenOutOfAnActorJWTOnlyTurn`, `TestCredentialForwarderNamesTheServersClaudeReachesDirectly`, `TestProcessDriverBindsTheCallerCredentialPerTurn`, `TestActorJWTHeaders` | to file with kagent-dev/kagent#3018, whose forwarder it changes, after `feat(harness): no caller credential to actor-JWT hosts` |
