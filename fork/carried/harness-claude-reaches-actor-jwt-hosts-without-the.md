---
id: harness-claude-reaches-actor-jwt-hosts-without-the
fork: kagent
theme: credential-injection
category: feature
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3263, kagent-dev/kagent#3257, kagent-dev/kagent#3018]
  exit: 'with #3263: muster as a tokenPassthrough RemoteMCPServer delivered by the egress gateway, the placeholder compiled into mcp.json'
depends_on: [harness-send-the-placeholder-to-actor-jwt-hosts, harness-codex-and-omp-send-the-placeholder-to-actor]
commit: giantswarm/kagent-upstream#717
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(harness): Claude reaches actor-JWT hosts without the forwarder` | with the placeholder sent to actor-JWT hosts, the forwarder adds nothing to a call to muster that the egress gateway does not replace, yet `mcp.json`, readable by Claude's user and so by every process of the sandbox, still carried the forwarder's loopback token, which any of them could use during the turn. The harness now rewrites `mcp.json` before each turn's Claude process starts, and before anything runs as Claude's user, and writes a server on one of the turn's actor-JWT hosts without a tool selection with its compiled URL and headers and the placeholder bearer, with or without the caller's credential, so a Session whose every MCP server is on one holds no loopback token in the sandbox. A server with a tool selection stays on the forwarder, which learns the turn's hosts without the caller's credential too. The claude, codex and omp harnesses build a direct server's headers with one rule, `workspace.ActorJWTHeaders`. `TestMCPConfigLeavesTheLoopbackTokenOutOfAnActorJWTOnlyTurn`, `TestMCPConfigSendsThePlaceholderWithoutTheForwarder`, `TestMCPConfigKeepsASelectionOnTheForwarder`, `TestProcessDriverWritesTheMCPConfigBeforeTheSessionDirectory`, `TestProcessDriverBindsTheCallerCredentialPerTurn`, `TestActorJWTHostsOnlyBindsNoCallerCredential`, `TestActorJWTHeaders` | exits with kagent-dev/kagent#3263, which builds on #3257's `RemoteMCPServer.spec.tokenPassthrough`: the egress gateway delivers the credential and the Claude translator compiles the placeholder into `mcp.json`, so neither the forwarder nor the per-turn rewrite is needed for muster: the platform's `ate-secret` provider would pick the Actor's JWT or the caller's bearer per request, provided the gateway tells it which Actor a request comes from. A server with a tool selection still needs the forwarder, since upstream's Claude translator only warns about a selection it cannot enforce. Supersedes the exit through kagent-dev/kagent#3018 |
