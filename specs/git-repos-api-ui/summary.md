# Summary: Git Repos API + UI

## Artifacts

| File | Description |
|------|-------------|
| `specs/git-repos-api-ui/rough-idea.md` | Original idea |
| `specs/git-repos-api-ui/requirements.md` | 20 Q&A pairs covering scope, architecture, technology choices |
| `specs/git-repos-api-ui/design.md` | Full design: architecture, components, APIs, data models, acceptance criteria |
| `specs/git-repos-api-ui/plan.md` | 14-step implementation plan, each step demoable |
| `specs/git-repos-api-ui/research/01-api-patterns.md` | Kagent HTTP API patterns |
| `specs/git-repos-api-ui/research/02-database-patterns.md` | Kagent database/GORM patterns |
| `specs/git-repos-api-ui/research/03-ui-patterns.md` | Kagent Next.js UI patterns (AgentCronJob reference) |
| `specs/git-repos-api-ui/research/04-existing-git-refs.md` | Existing git references in codebase |
| `specs/git-repos-api-ui/research/05-crd-flow.md` | CRD → Controller → DB → API → UI flow |
| `specs/git-repos-api-ui/research/06-local-embeddings.md` | Local CPU embedding options |
| `specs/git-repos-api-ui/research/07-llm-cli-design.md` | Simon Willison's llm CLI design patterns |
| `specs/git-repos-api-ui/research/08-ast-grep.md` | ast-grep structural search (superseded by Reflex) |
| `specs/git-repos-api-ui/research/09-reflex-vs-astgrep.md` | Reflex vs ast-grep — Reflex embedded inside gitrepo-mcp |

## Overview

A standalone Go MCP server (`gitrepo-mcp`) that clones git repos, indexes them using tree-sitter chunking + EmbeddingGemma-300M local CPU embeddings, and exposes semantic search + ast-grep structural search via REST API and MCP tools. Kagent provides a proxy layer and UI for repo management and search.

**Key technology choices:**
- EmbeddingGemma-300M via ONNX Runtime (local CPU, 768 dims, <200MB RAM)
- Tree-sitter for function/block-level code chunking
- SQLite with float32 BLOB vectors + brute-force cosine similarity
- Reflex (reflex-search) embedded as subprocess — MCP tools proxied via stdio through unified `/mcp` endpoint (replaces ast-grep)
- Cobra CLI, gorilla/mux REST, MCP protocol

**Code locations:**
- `go/cmd/gitrepo-mcp/` — MCP server binary
- `contrib/tools/gitrepo-mcp/` — Helm chart + Dockerfile
- `go/internal/httpserver/handlers/gitrepos.go` — kagent proxy
- `ui/src/app/git/` — UI pages
- `ui/src/app/actions/gitrepos.ts` — server actions

## Suggested Next Steps

1. **Implement** — use `ralph run` with this spec, or work through the 13 steps manually
2. **FalkorDB code graph** — future phase, builds on the tree-sitter chunking from Step 3
3. **Webhook sync** — replace CronJob with GitHub/GitLab webhook triggers
