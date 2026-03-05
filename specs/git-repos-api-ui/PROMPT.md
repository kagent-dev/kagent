# Git Repos MCP Server + Kagent Integration

## Objective

Build `gitrepo-mcp` — a standalone Go MCP server that clones git repos, indexes them with local CPU embeddings (EmbeddingGemma-300M), and exposes semantic search + ast-grep structural search. Integrate into kagent via proxy API handlers and UI pages.

## Key Requirements

- Go CLI (Cobra) at `go/cmd/gitrepo-mcp/` with `serve`, `add`, `list`, `remove`, `sync`, `index`, `search`, `ast-search` commands
- REST API (gorilla/mux, 8 endpoints under `/api/repos`) + MCP protocol (7 tools) served from `serve` command
- SQLite storage: repos table, collections table, chunks table with float32 BLOB embeddings
- EmbeddingGemma-300M via `yalue/onnxruntime_go` — local CPU, 768 dims, batch size 32
- Tree-sitter chunking (`smacker/go-tree-sitter`) for .go, .py, .js, .ts, .java, .rs; heading-based for .md; document-level for .yaml, .toml, .groovy
- Brute-force cosine similarity search, results include chunk content + N context lines + file path + line range + score
- Content-hash (SHA256) deduplication — skip unchanged chunks on re-index
- ast-grep CLI wrapper: shell out to `ast-grep --pattern <pattern> --json <repo-path>`
- Kagent proxy handlers at `go/internal/httpserver/handlers/gitrepos.go` forwarding `/api/gitrepos/*` to MCP server
- Kagent UI: replace `/git` "Coming soon" stub with list page + add form + search bar (follow AgentCronJob UI patterns)
- Helm chart at `contrib/tools/gitrepo-mcp/` with Dockerfile, PVC, optional CronJob for periodic sync
- Incremental re-index: detect changed files after git pull, only re-embed those

## Acceptance Criteria

- Given a valid git URL, when `POST /api/repos`, then repo is cloned to PVC and status transitions cloning → cloned
- Given a cloned repo, when `POST /api/repos/{name}/index`, then files are chunked via tree-sitter and embedded
- Given a .go file with 3 functions, when indexed, then 3 separate chunks created with correct line ranges
- Given a previously indexed repo with unchanged files, when re-indexed, then unchanged chunks are skipped
- Given an indexed repo, when `POST /api/repos/{name}/search` with query, then ranked results returned with content + context
- Given a cloned repo, when `ast_search` called with pattern `func $NAME($$$) error`, then matching functions returned
- Given kagent UI at `/git`, when loaded, then repos listed with status badges, counts, and action buttons
- Given the search bar, when query submitted, then semantic search results displayed with file paths and code snippets
- Given `helm install`, then gitrepo-mcp deploys with PVC and is reachable from kagent

## Reference

Full spec at `specs/git-repos-api-ui/` — see `design.md` for architecture, data models, API contracts; `plan.md` for 13 implementation steps; `research/` for codebase patterns and technology decisions.
