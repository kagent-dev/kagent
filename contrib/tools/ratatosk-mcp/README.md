# Ratatosk MCP server

[Ratatosk](https://ratatosk.io) reads every CNCF graduated/incubating project's
release notes daily and extracts typed facts — security fixes, breaking
changes, removals, deprecations, default changes — each with a verbatim quote
and a source URL. This MCP server exposes those facts to in-cluster agents.

Privacy: the `check_stack` tool compares your running versions locally, inside
this process. Only project slugs are sent to the ratatosk server; versions
never leave your cluster. The upstream API is public, read-only, and needs no
credentials (rate limit 60 req/min per IP).

## Tools

| Tool | Purpose |
| --- | --- |
| `list_projects` | The tracked-project roster — resolve slugs here first |
| `check_stack` | Compare running versions against known facts (local comparison) |
| `get_release` | One reviewed release with all facts and the source URL |
| `facts_by_entity` | Reverse index: every fact touching one CVE/flag/CRD |
| `list_facts` | Incremental fact feed with a `since` cursor |

## Install

```bash
kubectl apply -f ratatosk-deploy.yaml            # Deployment + Service (no RBAC needed)
kubectl apply -f ratatosk-remote-mcpserver.yaml  # register with kagent
kubectl apply -f ratatosk-agent.yaml             # optional: release-triage example agent
```

Or with the Helm chart from the repository:

```bash
git clone https://github.com/garlicKim21/ratatosk-mcp
helm install ratatosk-mcp ./ratatosk-mcp/charts/ratatosk-mcp -n kagent
```

Ask the example agent things like:

> "We run kubernetes v1.36.0, cilium v1.17.18 and envoy v1.38.3 — anything
> that needs action before we upgrade?"

The agent answers with severity-ranked facts, each backed by a quote from the
release note and a link to the source.
