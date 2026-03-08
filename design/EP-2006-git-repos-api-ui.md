# EP-2006: Git Repos API and UI with Code Search

* Status: **Implemented**
* Spec: [specs/git-repos-api-ui](../specs/git-repos-api-ui/)

## Background

Standalone Go MCP server (`gitrepo-mcp`) that clones git repositories and provides code search capabilities. Proxied through the kagent controller API with a dedicated UI page for repository management.

## Motivation

AI agents need access to source code for context-aware assistance. A managed git repo service provides cloning, indexing, and search capabilities accessible via both MCP tools and REST API.

### Goals

- Clone and manage git repositories via REST API
- Full-text search, symbol search, regex search across repos
- MCP tools for agent-driven code search
- UI page for adding, syncing, and searching repositories
- Controller proxy integration at `/api/gitrepos`

### Non-Goals

- Embedding/semantic search (v2)
- LLM-based natural language code search (v2)
- FalkorDB code graph integration (v2)

## Implementation Details

- **Binary:** `go/plugins/gitrepo-mcp/` — standalone Go server
- **API proxy:** `go/core/internal/httpserver/handlers/gitrepos.go` — proxies to gitrepo-mcp service
- **Controller config:** `GITREPO_MCP_URL` env var in controller configmap
- **UI:** `ui/src/app/git/page.tsx` — repo list, add, sync, search
- **Dockerfile:** Custom Dockerfile with `chainguard/wolfi-base` for `git` binary
- **Helm:** Sub-chart in `helm/tools/gitrepo-mcp/`

### Test Plan

- API endpoint tests for CRUD and search
- E2E tests for clone and sync operations
