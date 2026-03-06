# Git Repos MCP Server + Kagent Integration

## Objective

Build `gitrepo-mcp` — a standalone Go MCP server that clones git repos and delegates all search to an embedded Reflex subprocess. Integrate into kagent via proxy API handlers and UI pages.

## Key Requirements

- Go CLI (Cobra) at `go/cmd/gitrepo-mcp/` with `serve`, `add`, `list`, `remove`, `sync` commands
- REST API (gorilla/mux, 6 endpoints under `/api/repos`) for repo management
- Unified MCP endpoint (`/mcp`) with 17 tools: 4 native (repo lifecycle) + 13 proxied from Reflex (search + deps)
- SQLite storage: repos table only (search index managed by Reflex `.reflex/` per repo)
- Reflex (`reflex-search`) embedded as subprocess: `rfx mcp` spawned via stdio, tools proxied through unified `/mcp` endpoint. Routing table dispatches by tool name, no prefix needed.
- `rfx index` triggered automatically after clone/sync (Reflex handles incremental indexing via blake3)
- Kagent proxy handlers at `go/internal/httpserver/handlers/gitrepos.go` forwarding `/api/gitrepos/*` to MCP server
- Kagent UI: replace `/git` "Coming soon" stub with list page + add form (follow AgentCronJob UI patterns)
- Helm chart at `contrib/tools/gitrepo-mcp/` with Dockerfile (Go binary + reflex-search), PVC, optional CronJob

## Out of Scope (v1)

- Embedding/semantic search (ONNX, EmbeddingGemma-300M, cosine similarity)
- `rfx ask` (LLM-based natural language search)
- FalkorDB code graph
- UI search bar (search via agents + MCP tools)

## Acceptance Criteria

- Given a valid git URL, when `POST /api/repos`, then repo is cloned to PVC and status transitions cloning → cloned
- Given a cloned repo, then `rfx index` is triggered automatically and `.reflex/` directory is created
- Given an indexed repo, when agent calls `search_code` via unified MCP, then matching results returned through Reflex subprocess
- Given an indexed repo, when agent calls `search_ast` with a Tree-sitter pattern, then matching AST nodes returned
- Given an indexed repo, when agent calls `get_dependencies`, then file import graph returned
- Given Reflex unavailable, when agent calls `tools/list`, then only 4 native tools returned (Reflex tools omitted)
- Given kagent UI at `/git`, when loaded, then repos listed with status badges, counts, and action buttons
- Given `helm install`, then gitrepo-mcp deploys with PVC and is reachable from kagent

## Reference

Full spec at `specs/git-repos-api-ui/` — see `design.md` for architecture, data models, API contracts; `plan.md` for 10 implementation steps; `research/` for codebase patterns and technology decisions.
