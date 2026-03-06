# Summary: Git Repos API + UI

## Artifacts

| File | Description |
|------|-------------|
| `specs/git-repos-api-ui/rough-idea.md` | Original idea |
| `specs/git-repos-api-ui/requirements.md` | 20 Q&A pairs covering scope, architecture, technology choices |
| `specs/git-repos-api-ui/design.md` | Full design: architecture, components, APIs, data models, acceptance criteria |
| `specs/git-repos-api-ui/plan.md` | 10-step implementation plan, each step demoable |
| `specs/git-repos-api-ui/research/01-api-patterns.md` | Kagent HTTP API patterns |
| `specs/git-repos-api-ui/research/02-database-patterns.md` | Kagent database/GORM patterns |
| `specs/git-repos-api-ui/research/03-ui-patterns.md` | Kagent Next.js UI patterns (AgentCronJob reference) |
| `specs/git-repos-api-ui/research/04-existing-git-refs.md` | Existing git references in codebase |
| `specs/git-repos-api-ui/research/05-crd-flow.md` | CRD → Controller → DB → API → UI flow |
| `specs/git-repos-api-ui/research/06-local-embeddings.md` | Local CPU embedding options (deferred) |
| `specs/git-repos-api-ui/research/07-llm-cli-design.md` | Simon Willison's llm CLI design patterns (deferred) |
| `specs/git-repos-api-ui/research/08-ast-grep.md` | ast-grep structural search (superseded by Reflex) |
| `specs/git-repos-api-ui/research/09-reflex-vs-astgrep.md` | Reflex vs ast-grep — Reflex embedded inside gitrepo-mcp |

## Overview

A standalone Go MCP server (`gitrepo-mcp`) that clones git repos and delegates all search to an embedded Reflex subprocess. Reflex provides trigram full-text search, Tree-sitter symbol search, regex search, AST pattern search, and dependency analysis — all proxied through a single `/mcp` endpoint. No embedding pipeline in v1. Kagent provides a REST proxy layer and UI for repo management.

**Key technology choices:**
- Reflex (reflex-search) embedded as subprocess — trigram index + Tree-sitter + dependency analysis, built-in MCP via stdio proxy
- SQLite for repo metadata only (search index managed by Reflex)
- Cobra CLI, gorilla/mux REST, MCP protocol

**What's NOT in v1:**
- Embedding/semantic search (ONNX, EmbeddingGemma)
- `rfx ask` (LLM-based natural language search)
- FalkorDB code graph
- UI search bar (search via agents + MCP tools)

**Code locations:**
- `go/cmd/gitrepo-mcp/` — MCP server binary
- `contrib/tools/gitrepo-mcp/` — Helm chart + Dockerfile
- `go/internal/httpserver/handlers/gitrepos.go` — kagent proxy
- `ui/src/app/git/` — UI pages
- `ui/src/app/actions/gitrepos.ts` — server actions

## Suggested Next Steps

1. **Implement** — work through the 10 steps in plan.md
2. **Embedding search** — future: add EmbeddingGemma-300M + cosine similarity as a `semantic_search` tool
3. **`rfx ask`** — future: enable natural language → code search via LLM
4. **FalkorDB code graph** — future: AST → graph nodes/edges, Cypher queries
