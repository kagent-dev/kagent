# Requirements

## Questions & Answers

### Q1: What is the primary purpose of "Git Repos" in kagent?
Is this about letting users register/manage git repositories so that agents can clone and work with them (read code, make changes, open PRs)? Or is it more of a metadata registry (just tracking which repos exist and their URLs)? Or something else entirely?

**A1:** Register managed git repos, index them for vector semantic search and graph search.

### Q2: What does "managed" mean for a git repo?
Does kagent clone the repo locally/into a volume, keep it synced (periodic pulls), and maintain the indexed state? Or does the user provide a snapshot/archive? Specifically:
- Does kagent clone and periodically sync (pull) the repo?
- Where is the repo stored (PVC, local filesystem, object storage)?
- What triggers re-indexing (manual, on new commits, on a schedule)?

**A2:** Standalone MCP server with its own database and persistent volume. Periodic pull via external CronJob (not built into the server).

### Q3: What is the scope of this task — the MCP server itself, or the kagent integration layer?
Given this is a standalone MCP server, is this task about:
- (a) Building the MCP server (clone, index, search tools, own DB, PVC) — a new standalone service
- (b) Building the kagent API + UI to register/manage these git repo MCP servers — the integration layer
- (c) Both

**A3:** (b) — kagent API + UI integration layer only. The MCP server is a separate concern.

### Q4: What data does the UI need to manage for each git repo?
What fields does a user fill in when registering a git repo? For example:
- Repo URL (required)
- Branch (default: main?)
- Auth credentials (SSH key, PAT token — via K8s Secret reference?)
- Display name / description
- Which MCP server instance to associate with
- Anything else?

**A4:** Auth credentials are installed via Helm chart (not per-repo in UI). User provides repo URL, branch, etc. in the UI.

### Q5: Is this a CRD-backed resource or DB-only?
Since the git repo MCP server is standalone, the kagent integration could be:
- (a) **CRD-backed** — a new `GitRepo` CRD that the controller reconciles (K8s-native, GitOps-friendly, but heavier)
- (b) **DB-only** — API handlers store repo registrations directly in the kagent DB (simpler, faster to build)
- (c) The repos are managed by the MCP server itself, and kagent just proxies/displays what the MCP server knows

**A5:** (c) — The MCP server owns the repos. Kagent proxies/displays what the MCP server knows.

### Q6: How does kagent communicate with the git repo MCP server?
Does the MCP server expose:
- (a) A REST API that kagent proxies (CRUD for repos, search endpoints)
- (b) MCP protocol only (tools like `add_repo`, `search_code`, `list_repos`)
- (c) Both — REST API for management, MCP tools for agent use

Which one, and does the MCP server API already exist or is it being designed in parallel?

**A6:** (c) Both — REST API for management (kagent proxies for UI), MCP tools for agent use.

### Q7: Does the MCP server's REST API already exist, or does it need to be designed as part of this effort?
If it exists, can you share the API contract (endpoints, request/response shapes)? If not, should we define the expected API surface as part of this design so both sides can be built independently?

**A7:** New MCP server needs to be built — implemented in Go as a CLI. API contract should be defined as part of this design.

### Q8: You mentioned vector semantic search and graph search. What indexing/search stack do you have in mind?
For example:
- **Vector DB:** embedded (e.g., sqlite-vec, hnswlib) or external (e.g., Qdrant, Weaviate, pgvector)?
- **Graph DB:** embedded (e.g., in-memory graph) or external (e.g., Neo4j)?
- **Embeddings model:** which LLM/embedding model for vectorizing code?
- Or should we keep this flexible/pluggable for now and just define the interface?

**A8:** FalkorDB Code Graph approach — https://www.falkordb.com/blog/code-graph/
- Uses FalkorDB (graph DB with Cypher queries)
- AST parsing → graph construction (nodes: Module, Class, Function, File; edges: CONTAINS, CALLS, INHERITS_FROM, DEPENDS_ON)
- GraphRAG: natural language → Cypher queries via LLM
- Local CPU embeddings using Google EmbeddingGemma-300M (300M params, 768 dims, <200MB RAM)
- Ref: https://www.bentoml.com/blog/a-guide-to-open-source-embedding-models

### Q9: So the architecture involves FalkorDB as a dependency. Should the MCP server bundle FalkorDB (e.g., embedded), or expect it as an external service (separate Helm chart/deployment)?

**A9:** (deferred — user clarified embeddings preference: local CPU, Google Gemma)

### Q10: Design reference — Simon Willison's `llm` CLI
User wants to follow the UX pattern of the `llm` CLI tool for the Go MCP server CLI:
```bash
llm embed-multi myrepo -m sentence-transformers/all-MiniLM-L6-v2 --files . '**/*.go'
llm similar myrepo -c "where do we set up auth?"
```

**A10:** Key patterns adopted from `llm` CLI:
- Named collections (string key per repo)
- SQLite + BLOB vector storage (little-endian float32)
- Content-hash deduplication (skip unchanged files on re-index)
- Glob-based file discovery (file path = embedding ID)
- Brute-force cosine similarity (sufficient for <20K embeddings)
- NDJSON output format
- Cobra CLI in Go translation

See `research/07-llm-cli-design.md` for full analysis.

### Q11: Let me now confirm the full picture. Two deliverables:

**Deliverable A — Go MCP Server CLI (`gitrepo`):**
- Cobra CLI with subcommands: `add`, `list`, `remove`, `sync`, `index`, `search`, `serve`
- `serve` mode: REST API + MCP protocol (for kagent + agents)
- SQLite for metadata + vector embeddings (BLOB, brute-force cosine)
- FalkorDB for code graph (AST → nodes/edges, Cypher queries)
- EmbeddingGemma-300M via ONNX Runtime (local CPU)
- PVC for cloned repos
- External CronJob for periodic sync

**Deliverable B — Kagent integration:**
- Proxy handlers in kagent HTTP server → MCP server REST API
- UI pages: list repos, add/remove repo, view index status, search
- Replace the "Coming soon" stub at `/git`

Are both deliverables in scope for this design, or just one?

**A11:** Both in scope, but **semantic search first**. FalkorDB/graph search is a separate future feature.

### Q12: So the scope for this design is:

**Go MCP Server CLI:**
- Repo management: `add`, `list`, `remove`, `sync`
- Indexing: clone repo → glob files → embed with EmbeddingGemma-300M → store in SQLite
- Search: brute-force cosine similarity, return ranked file chunks
- Serve: REST API + MCP tools (`add_repo`, `list_repos`, `search_code`, etc.)
- No FalkorDB, no graph, no AST parsing (deferred)

**Kagent integration:**
- Proxy REST API for repo management
- UI pages for list/add/remove/search
- Replace `/git` stub

**Out of scope (future):**
- FalkorDB code graph
- AST parsing → graph nodes/edges
- GraphRAG / Cypher queries

Is this the right scope?

**A12:** User also wants to consider ast-grep for structural code search.

### Q13: How to use ast-grep in Phase 1?

**A13:** Minimal CLI wrapped with MCP for ast-grep over the repo. Shell out to `ast-grep` binary, expose as MCP tools.

### Q14: So now we have two MCP tool groups in the same server:

**Semantic search tools:**
- `add_repo` — register repo URL, clone to PVC
- `list_repos` — list registered repos
- `remove_repo` — remove repo and its embeddings
- `sync_repo` — pull latest + re-index changed files
- `search_code` — semantic similarity search across repo

**Structural search tools (ast-grep wrapper):**
- `ast_search` — run ast-grep pattern against a repo (e.g., `func $NAME($$$) error`)
- `ast_search_languages` — list supported languages

Both served from the same Go CLI via `serve` command. Does this capture what you want, or should the ast-grep wrapper be a separate MCP server?

**A14:** Same server. Single Go CLI serves both semantic search and ast-grep MCP tools.

### Q15: Where should the MCP server binary and Helm chart live?
- (a) In this repo under `go/cmd/gitrepo-mcp/` + `helm/tools/gitrepo-mcp/` (like kanban-mcp)
- (b) In a separate repo (per CLAUDE.md guidance: "All new MCP servers should be in separate repos/charts")
- (c) Your preference?

**A15:** In this repo: `go/cmd/gitrepo-mcp/` for binary, `contrib/tools/gitrepo-mcp/` for Helm chart.

### Q16: What chunking strategy for embedding files?
When indexing a repo, how should files be split before embedding?
- (a) **Whole file** — one embedding per file (simple, but poor for large files)
- (b) **Fixed-size chunks** — split by N lines or N tokens with overlap (like llm CLI)
- (c) **Function/block-level** — use tree-sitter to extract functions/methods/classes as chunks (best quality, more complex)
- (d) **Start simple, evolve** — whole file or fixed chunks for v1, add tree-sitter chunking later

**A16:** (c) Function/block-level chunking via tree-sitter. Each function/method/class becomes a separate embedding.

### Q17: What file types should be indexed?
Should the server index all text files, or only recognized source code? For example:
- (a) Only source code files matching known extensions (.go, .py, .js, .ts, .java, .rs, etc.)
- (b) Source code + docs (.md, .txt, .yaml, .json)
- (c) Configurable via glob patterns per repo (like `llm embed-multi --files . '**/*.go'`)

**A17:** Fixed set: `.go`, `.py`, `.js`, `.ts`, `.java`, `.groovy`, `.rs`, `.yaml`, `.toml`, `.md`

### Q18: For non-code files (.yaml, .toml, .md) that don't have functions/classes, what chunking?
Tree-sitter works for source code, but YAML/TOML/Markdown don't have function-level structure. Options:
- (a) Whole file for non-code, tree-sitter for code
- (b) Heading-based chunks for .md, document-level for .yaml/.toml
- (c) Fixed-size line chunks with overlap for non-code files

**A18:** (b) Heading-based for .md, document-level for .yaml/.toml.

### Q19: What should the search results return?
When a semantic search matches, what does the response include?
- (a) Just the chunk content + file path + line range + score
- (b) Chunk content + surrounding context (N lines before/after) + file path + score
- (c) Chunk content + file path + score + repo metadata (branch, last commit)

**A19:** (b) Chunk content + surrounding context (N lines) + file path + score.

### Q20: For the kagent UI, what does the Git Repos page need to show?
Thinking about the list page:
- Repo name, URL, branch, indexing status (indexed/indexing/error), last synced timestamp, file count, chunk count
- Actions: add, remove, trigger sync, trigger re-index
- A search bar to run semantic queries across all repos (or per-repo)?

Is there anything else you'd want on the UI, or is this sufficient for v1?

**A20:** Sufficient for v1.

**A9:** User wants local embeddings (no external API dependency for embeddings).

