# Implementation Plan: Git Repos API + UI

## Known Issues (discovered during browser testing)

Before acceptance tests can pass, these issues must be addressed:

1. **`fetchApi` routes through Next.js server actions → `getBackendUrl()`** — in dev mode this resolves to `localhost:8083/api` (controller), but the controller needs `GITREPO_MCP_URL` env var set or it returns 503. In production (nginx), requests go directly to gitrepo-mcp via rewrite rules, bypassing the controller entirely.
2. **Nginx rewrite rule for base path** — rule order in `ui/conf/nginx.conf` lines 93-99 is correct (nginx evaluates all rewrites in order, first match with `break` wins), but the `location /api/gitrepos` block uses prefix matching so `/api/gitrepos` and `/api/gitrepos/foo` both enter the block. Verify by testing `curl http://localhost:8080/api/gitrepos` through nginx.
3. **gitrepo-mcp service must be running** — if not deployed, all gitrepo endpoints fail silently (503 from controller, 502 from nginx).
4. **ErrorState component renders error message outside the layout** — `<p>` with message is rendered *before* the centered error card, so it appears unstyled at the top of the page (`ErrorState.tsx` line 14 is outside the `min-h-screen` container).

## Checklist

- [ ] Step 1: Scaffold gitrepo-mcp CLI + SQLite storage
- [ ] Step 2: Repo management (add, list, remove, clone)
- [ ] Step 3: Tree-sitter code chunking
- [ ] Step 4: Embedding pipeline (EmbeddingGemma-300M + ONNX Runtime)
- [ ] Step 5: Semantic search (cosine similarity)
- [ ] Step 6: Reflex embedded subprocess (MCP proxy + indexing trigger)
- [ ] Step 7: REST API server
- [ ] Step 8: Unified MCP server (native + Reflex proxy)
- [ ] Step 9: Kagent proxy handlers
- [ ] Step 10: Kagent UI — list page + add form
- [ ] Step 11: Kagent UI — search
- [ ] Step 12: Helm chart + Dockerfile
- [ ] Step 13: Sync + re-index + CronJob support
- [ ] Step 14: Browser acceptance tests (Cypress)

---

## Step 1: Scaffold gitrepo-mcp CLI + SQLite storage

**Objective:** Bootstrap the Go CLI binary with Cobra, GORM/SQLite, and project structure.

**Implementation guidance:**
- Create `go/cmd/gitrepo-mcp/main.go` with Cobra root command
- Create `go/cmd/gitrepo-mcp/internal/storage/` with GORM models for `repos`, `collections`, `chunks` tables
- Use `glebarez/sqlite` driver (same as kagent) for now — vector search will be raw SQL on BLOBs, not sqlite-vec
- Implement `storage.New(dataDir)` → opens/creates SQLite DB, runs AutoMigrate
- Add `--data-dir` persistent flag on root command (default: `./data`)
- Add placeholder subcommands: `serve`, `add`, `list`, `remove`, `sync`, `index`, `search`

**Test requirements:**
- Unit test: DB opens, AutoMigrate creates tables, basic CRUD on repos table
- Verify models serialize/deserialize correctly

**Integration notes:**
- All subsequent steps build on this foundation
- Data dir contains: `gitrepo.db` (SQLite) + `repos/` (cloned repos)

**Demo:** `gitrepo-mcp --help` shows all subcommands. `gitrepo-mcp list` returns empty JSON array.

---

## Step 2: Repo management (add, list, remove, clone)

**Objective:** Implement git clone/remove operations and CLI commands for repo lifecycle.

**Implementation guidance:**
- Create `go/cmd/gitrepo-mcp/internal/repo/manager.go`:
  - `Add(name, url, branch)` — validate inputs, insert DB row (status: "cloning"), `git clone --branch <branch> --single-branch --depth 1 <url> <data-dir>/repos/<name>`, update status to "cloned"
  - `List()` — query all repos from DB
  - `Remove(name)` — delete repo dir + DB rows (CASCADE deletes chunks)
  - `Get(name)` — single repo details
- Use `os/exec` to shell out to `git` CLI (simpler than go-git for clone/pull)
- Wire up Cobra commands: `add`, `list`, `remove`
- `add` command: `gitrepo-mcp add kagent --url https://github.com/kagent-dev/kagent.git --branch main`
- `list` command: outputs JSON array of repos with status
- `remove` command: `gitrepo-mcp remove kagent`

**Test requirements:**
- Unit test: repo CRUD in DB (mock git operations)
- Integration test: clone a small public repo, verify directory created, list shows it, remove cleans up

**Integration notes:**
- Repo manager is used by CLI commands, REST API, and MCP tools

**Demo:** `gitrepo-mcp add test --url https://github.com/simonw/llm.git --branch main` clones repo. `gitrepo-mcp list` shows it with status "cloned".

---

## Step 3: Tree-sitter code chunking

**Objective:** Implement function/block-level code chunking using tree-sitter Go bindings.

**Implementation guidance:**
- Add `smacker/go-tree-sitter` dependency + language grammars (Go, Python, JavaScript, TypeScript, Java, Rust)
- Create `go/cmd/gitrepo-mcp/internal/indexer/chunker.go`:
  - `ChunkFile(filePath, content, language) → []Chunk`
  - `Chunk` struct: `FilePath`, `LineStart`, `LineEnd`, `ChunkType` (function/method/class/module), `ChunkName`, `Content`
- Language-specific tree-sitter queries:
  - Go: `function_declaration`, `method_declaration`, `type_declaration`
  - Python: `function_definition`, `class_definition`
  - JS/TS: `function_declaration`, `arrow_function`, `class_declaration`, `method_definition`
  - Java/Groovy: `method_declaration`, `class_declaration`, `interface_declaration`
  - Rust: `function_item`, `impl_item`, `struct_item`
- Create `go/cmd/gitrepo-mcp/internal/indexer/chunker_md.go`:
  - Split by `## ` / `### ` headings, each section becomes a chunk
- Create `go/cmd/gitrepo-mcp/internal/indexer/chunker_config.go`:
  - Whole file as single chunk for .yaml, .toml
- File type → language mapping: extension-based lookup table
- Groovy: no tree-sitter grammar available — fall back to regex-based function detection or whole-file chunking

**Test requirements:**
- Unit test per language: provide sample source file, verify correct chunks extracted with line ranges
- Test .md heading splitting
- Test .yaml/.toml whole-file chunking
- Test unknown file type fallback (skip or whole-file)

**Integration notes:**
- Chunker is called by the indexer (Step 4) for each file in the repo

**Demo:** `gitrepo-mcp index kagent --dry-run` shows chunked output for a cloned repo without embedding.

---

## Step 4: Embedding pipeline (EmbeddingGemma-300M + ONNX Runtime)

**Objective:** Embed code chunks using EmbeddingGemma-300M running locally via ONNX Runtime.

**Implementation guidance:**
- Add `yalue/onnxruntime_go` dependency
- Create `go/cmd/gitrepo-mcp/internal/embedder/embedder.go`:
  - `EmbeddingModel` interface: `EmbedBatch(texts []string) ([][]float32, error)`, `Dimensions() int`
  - `GemmaEmbedder` struct: loads ONNX model on first call (lazy), configurable model path
- Create `go/cmd/gitrepo-mcp/internal/embedder/gemma.go`:
  - Load `EmbeddingGemma-300M.onnx` from `--model-dir` flag
  - Tokenize input (need tokenizer — use `buckhx/gobert/tokenize` or bundle vocab file + custom WordPiece)
  - Run ONNX inference in batches (batch size: 32, configurable)
  - Return 768-dim float32 vectors
  - Mean pooling over token embeddings → single vector per chunk
- Create `go/cmd/gitrepo-mcp/internal/embedder/batch.go`:
  - Content-hash dedup: SHA256 of chunk content, skip if hash matches existing DB row
  - Process files in batches, store results in transaction
- Embedding BLOB encoding: `encoding/binary.LittleEndian` to pack `[]float32` → `[]byte`
- Create `go/cmd/gitrepo-mcp/internal/indexer/indexer.go`:
  - `Index(repoName)` — walk repo files → filter by extension → chunk each file → batch embed → store in DB
  - Update repo status: "indexing" → "indexed", set file_count and chunk_count
- Wire up `gitrepo-mcp index <name>` command

**Test requirements:**
- Unit test: BLOB encode/decode round-trip
- Unit test: dedup logic (same content → skip, changed content → re-embed)
- Unit test: batch processing (mock embedder)
- Integration test: index a small repo, verify chunks stored with embeddings in DB

**Integration notes:**
- ONNX Runtime shared library + model file must be available at runtime
- Add `--model-dir` and `--onnx-lib` flags for configuration
- Fallback: if ONNX not available, provide a `MockEmbedder` for development/testing

**Demo:** `gitrepo-mcp index kagent` indexes all files, `gitrepo-mcp list` shows chunk count.

---

## Step 5: Semantic search (cosine similarity)

**Objective:** Implement brute-force cosine similarity search over stored embeddings.

**Implementation guidance:**
- Create `go/cmd/gitrepo-mcp/internal/search/semantic.go`:
  - `Search(query string, repoName *string, limit int, contextLines int) → []SearchResult`
  - Embed query string using same model
  - Load all chunk embeddings for target repo(s) from DB
  - Compute cosine similarity: `dot(a,b) / (||a|| * ||b||)`
  - Sort by score descending, return top N
  - Optimize: decode BLOBs in bulk, use SIMD-friendly loop if possible
- Create `go/cmd/gitrepo-mcp/internal/search/context.go`:
  - Given file path + line range + contextLines, read surrounding lines from cloned repo on disk
  - Return `{before: []string, after: []string}`
- `SearchResult` struct: `Repo`, `FilePath`, `LineStart`, `LineEnd`, `Score`, `ChunkType`, `ChunkName`, `Content`, `Context`
- Wire up `gitrepo-mcp search <name> -c "query" --limit 10 --context 3`

**Test requirements:**
- Unit test: cosine similarity correctness (known vectors)
- Unit test: context extraction (mock file on disk)
- Integration test: index repo → search → verify relevant results ranked higher

**Integration notes:**
- For repos with >10K chunks, consider loading embeddings in memory at startup (cache)
- NDJSON output format for CLI, JSON for API

**Demo:** `gitrepo-mcp search kagent -c "where do we set up auth?" --limit 5` returns ranked results with file paths and code snippets.

---

## Step 6: Reflex embedded subprocess (MCP proxy + indexing trigger)

**Objective:** Embed Reflex inside gitrepo-mcp as a subprocess. Proxy Reflex's MCP tools through the same `/mcp` endpoint so agents see one unified tool surface.

**Why embedded (not separate server):**
- Single MCP connection for agents — all tools (repo mgmt + semantic + structural + deps) in one place
- Single Helm deployment — no sidecar coordination
- gitrepo-mcp controls Reflex lifecycle (start/stop/restart)
- Shared PVC access is automatic (same process)
- See `research/09-reflex-vs-astgrep.md` for Reflex vs ast-grep comparison

**Implementation guidance:**

Create `go/cmd/gitrepo-mcp/internal/reflex/`:

- **`lifecycle.go`** — Reflex subprocess management:
  - `ReflexManager` struct: manages `rfx mcp` subprocess
  - `Start()` — spawn `rfx mcp` with stdin/stdout pipes, set `cwd` to repos base dir
  - `Stop()` — send SIGTERM, wait with timeout, SIGKILL fallback
  - `IsAvailable() bool` — check if `rfx` binary exists in PATH at startup
  - `IsRunning() bool` — health check (subprocess alive + responsive)
  - `Restart()` — stop + start with exponential backoff on repeated failures
  - If `rfx` not in PATH: log warning, set `available=false`, Reflex tools omitted from tool list

- **`proxy.go`** — MCP tool proxying:
  - `ListTools() []ToolDef` — call Reflex's `tools/list` via stdio JSON-RPC, cache result
  - No prefix — Reflex tool names (`search_code`, `find_circular`, etc.) don't collide with native names (`add_repo`, `semantic_search`, etc.)
  - `CallTool(name, params) (result, error)` — forward to subprocess via stdio JSON-RPC, read response, return
  - Maintain a routing table: native tool names → Go handlers, Reflex tool names → subprocess
  - Handle timeouts: 30s per call, return MCP error on timeout
  - Handle subprocess crash mid-call: return error, trigger restart

- **`indexer.go`** — trigger Reflex indexing:
  - `IndexRepo(repoPath string) error` — run `rfx index` as a one-shot command (not via MCP, just exec)
  - Called after successful clone (Step 2) and after sync/pull (Step 13)
  - Non-blocking: run in goroutine, update repo metadata with Reflex index status
  - Creates `.reflex/` directory inside each cloned repo

- Add `--reflex-enabled` flag (default: true) to `serve` command
- Add `--reflex-path` flag (default: `rfx`) for custom binary location

**Test requirements:**
- Unit test: `ReflexManager` starts/stops subprocess correctly (mock exec)
- Unit test: `proxy.ListTools()` returns Reflex tool names
- Unit test: `proxy.CallTool()` forwards request and returns response
- Unit test: handle `rfx` not found → `IsAvailable()` returns false, tools list empty
- Unit test: handle subprocess crash → error returned, restart triggered
- Integration test: start gitrepo-mcp with Reflex → call `search_code` → get results

**Integration notes:**
- Step 8 (MCP server) will merge native tools + proxied Reflex tools into one tool list
- Reflex subprocess starts when `gitrepo-mcp serve` starts (eager at startup)
- Each repo gets its own `.reflex/` index inside `<data-dir>/repos/<name>/.reflex/`

**Demo:** `gitrepo-mcp serve` → agent connects to `/mcp` → `tools/list` returns both `add_repo`, `semantic_search` AND `search_code`, `find_circular`, etc.

---

## Step 7: REST API server

**Objective:** Serve all functionality via REST API endpoints.

**Implementation guidance:**
- Create `go/cmd/gitrepo-mcp/internal/server/rest.go`:
  - Use `gorilla/mux` router (same as kagent)
  - Inject repo manager, indexer, search into handler struct
  - Implement all 8 endpoints per design doc
  - JSON request/response with standard error format
  - Logging middleware
  - Health check: `GET /health`
- Async operations: clone and index can be long-running
  - `POST /api/repos` — start clone in goroutine, return immediately with status "cloning"
  - `POST /api/repos/{name}/index` — start index in goroutine, return immediately with status "indexing"
  - Client polls `GET /api/repos/{name}` to check progress
- Add to `serve` command: `gitrepo-mcp serve --port 8090 --data-dir /data`

**Test requirements:**
- Unit test: each handler (mock dependencies)
- Test request validation (missing fields, invalid repo name)
- Test async status transitions

**Integration notes:**
- REST API is the interface kagent proxies to
- Same handlers reused by MCP tool implementations

**Demo:** `gitrepo-mcp serve --port 8090` → `curl localhost:8090/api/repos` returns repo list.

---

## Step 8: Unified MCP server (native + Reflex proxy)

**Objective:** Expose all tools — native and proxied Reflex — via a single MCP endpoint at `/mcp`.

**Implementation guidance:**
- Create `go/cmd/gitrepo-mcp/internal/server/mcp.go`:
  - Use `mark3labs/mcp-go` SDK (or equivalent Go MCP library)
  - Register 5 native tools: `add_repo`, `list_repos`, `remove_repo`, `sync_repo`, `semantic_search`
  - Each native tool handler delegates to the same internal functions as REST handlers
  - MCP transport: stdio (for local) + SSE/HTTP (for network access from agents)
- **Merge Reflex tools into the same tool list:**
  - On startup, call `reflexProxy.ListTools()` to get Reflex tool definitions
  - Append Reflex tools to native tool list (no prefix — names don't collide)
  - Build routing table: `map[string]handler` — native tool names → Go handlers, Reflex tool names → `reflexProxy.CallTool()`
  - On `tools/call`: look up tool name in routing table, dispatch accordingly
- If Reflex is unavailable (`--reflex-enabled=false` or binary not found), only native tools are registered
- Add MCP server to `serve` command alongside REST API
- Tool schemas: native tools have explicit JSON Schema; Reflex tools use schemas from `reflexProxy.ListTools()`

**Test requirements:**
- Unit test: `tools/list` returns merged native + Reflex tools
- Unit test: native tool call routes to native handler
- Unit test: Reflex tool call routes to Reflex proxy
- Unit test: with Reflex unavailable, only native tools listed
- Integration test: call both `semantic_search` and `search_code` via MCP client, verify responses

**Integration notes:**
- Agents see ONE MCP server with 5 native + 13 Reflex = 18 tools
- Agents connect to gitrepo-mcp once and get everything
- MCP, REST, and Reflex subprocess all run in same process, share dependencies

**Demo:** Agent connects to `/mcp` → `tools/list` returns 18 tools → agent calls `semantic_search` (embedding-based) and `search_code` (trigram-based) in the same session.

---

## Step 9: Kagent proxy handlers

**Objective:** Add proxy handlers to kagent HTTP server that forward to gitrepo-mcp REST API.

**Implementation guidance:**
- Create `go/internal/httpserver/handlers/gitrepos.go`:
  - `GitReposHandler` struct with `Base` + `GitRepoMCPURL string`
  - Generic proxy function: read request body → forward to gitrepo-mcp URL → stream response back
  - Auth check on each handler (same pattern as other handlers)
  - Handle connection errors to downstream (return 502)
- Register routes in `go/internal/httpserver/server.go`:
  - 8 routes under `/api/gitrepos/*`
- Add `gitRepoMCPURL` to `ServerConfig` (env var: `GITREPO_MCP_URL`)
- Wire into `NewHandlers()`

**Test requirements:**
- Unit test: proxy forwards correctly (mock downstream HTTP)
- Unit test: handles downstream failures (timeout, 5xx)
- Test auth middleware applied

**Integration notes:**
- Kagent Helm chart needs new env var `GITREPO_MCP_URL`
- If gitrepo-mcp not configured, handlers return 503 "service not available"

**Demo:** `curl localhost:8083/api/gitrepos` returns repo list (proxied from gitrepo-mcp).

---

## Step 10: Kagent UI — list page + add form

**Objective:** Replace the "Coming soon" git page with a functional repo list and add form.

**Implementation guidance:**
- Create `ui/src/app/actions/gitrepos.ts`:
  - Server actions: `getGitRepos`, `addGitRepo`, `removeGitRepo`, `syncGitRepo`, `indexGitRepo`
  - Use `fetchApi` pattern from `utils.ts`
- Add types to `ui/src/types/index.ts`: `GitRepo`, `AddGitRepoRequest`
- Replace `ui/src/app/git/page.tsx`:
  - "use client" list page following AgentCronJob pattern
  - Table: name, URL, branch, status badge, last synced, file count, chunk count
  - Expandable rows for details
  - Action buttons: Sync, Re-index, Delete (with confirmation dialog)
  - "Add Repo" button → navigates to `/git/new`
- Create `ui/src/app/git/new/page.tsx`:
  - Form: name, URL, branch (default: main)
  - Validation: name required, URL required and valid
  - Submit → `addGitRepo()` → navigate back to list
- Update sidebar navigation to include Git Repos link (if not already present)

**Test requirements:**
- Verify list page renders with mock data
- Verify add form validates and submits
- Verify delete confirmation dialog works

**Integration notes:**
- Matches AgentCronJob UI patterns exactly
- Status badges: green (indexed), yellow (cloning/indexing), red (error)

**Demo:** Navigate to `/git`, see repo list. Click "Add Repo", fill form, see new repo appear in list.

---

## Step 11: Kagent UI — search

**Objective:** Add semantic search functionality to the git repos UI.

**Implementation guidance:**
- Add `searchGitRepos` server action to `gitrepos.ts`
- Add `SearchResult`, `SearchRequest` types to `types/index.ts`
- Add search bar component to `ui/src/app/git/page.tsx`:
  - Text input + search button at top of page
  - Debounced input or explicit submit
  - Results displayed below search bar, above repo list
- Create `ui/src/components/SearchResults.tsx`:
  - Card per result: file path, line range, score badge, chunk type
  - Code block with syntax highlighting (use existing code display patterns or `prismjs`/`highlight.js`)
  - Context lines shown in lighter style above/below the match
  - Click file path → could link to source (future) or copy path
- Loading state while searching
- "No results" state

**Test requirements:**
- Verify search bar submits query
- Verify results render with all fields
- Verify loading and empty states

**Integration notes:**
- Search queries all repos by default; future: add repo filter dropdown

**Demo:** Type "authentication middleware" in search bar, see ranked results with code snippets from across repos.

---

## Step 12: Helm chart + Dockerfile

**Objective:** Package gitrepo-mcp for Kubernetes deployment.

**Implementation guidance:**
- Create `contrib/tools/gitrepo-mcp/`:
  - `Dockerfile`: multi-stage build — Go binary + ONNX Runtime lib + model file + reflex-search binary
  - `Chart.yaml`: chart metadata
  - `values.yaml`: port, PVC size, model config, git credentials
  - `templates/deployment.yaml`: pod with PVC mount, env vars for data-dir, model-dir
  - `templates/service.yaml`: ClusterIP service
  - `templates/pvc.yaml`: PersistentVolumeClaim for repos + DB
  - `templates/secret.yaml`: git credentials (optional)
  - `templates/cronjob.yaml`: optional periodic sync CronJob
- Dockerfile layers:
  1. Build Go binary
  2. Download ONNX Runtime shared library
  3. Download EmbeddingGemma-300M ONNX model
  4. Install reflex-search binary (`npm install -g reflex-search` or download from cargo)
  5. Runtime image: distroless/base + binary + libs + model + reflex
- Values:
  ```yaml
  replicaCount: 1
  port: 8090
  persistence:
    size: 10Gi
    storageClass: ""
  model:
    name: "EmbeddingGemma-300M"
    dimensions: 768
  git:
    credentials:
      secretName: ""
  cronJob:
    enabled: false
    schedule: "0 */6 * * *"
  ```

**Test requirements:**
- `helm lint` passes
- `helm template` renders valid manifests
- Docker build succeeds

**Integration notes:**
- Update kagent Helm chart `values.yaml` to include `gitRepoMCPURL` setting
- Document in README how to deploy alongside kagent

**Demo:** `helm install gitrepo-mcp contrib/tools/gitrepo-mcp/` deploys the service. Kagent UI can manage repos.

---

## Step 13: Sync + re-index + CronJob support

**Objective:** Implement git pull sync and incremental re-indexing.

**Implementation guidance:**
- Add to repo manager:
  - `Sync(name)` — `git -C <repo-path> pull`, update `last_synced` timestamp
  - Detect changed files: `git diff --name-only HEAD@{1} HEAD` after pull
- Add to indexer:
  - `ReIndex(repoName, changedFiles []string)` — only re-chunk and re-embed changed files
  - Delete old chunks for changed files, insert new ones
  - Full re-index if `changedFiles` is nil (force mode)
- Wire up `sync` CLI command and REST endpoint:
  - `POST /api/repos/{name}/sync` → pull + incremental re-index
- CronJob support:
  - `POST /api/repos/{name}/sync` is the target
  - Helm chart CronJob template calls this endpoint for each registered repo
  - Or: `gitrepo-mcp sync --all` CLI command that syncs all repos
- Concurrency guard: mutex per repo to prevent concurrent sync/index operations

**Test requirements:**
- Unit test: git pull executes, changed files detected
- Unit test: incremental re-index only processes changed files
- Integration test: modify file → sync → verify only affected chunks updated

**Integration notes:**
- CronJob in Helm chart is optional (disabled by default)
- Users can also trigger sync manually via UI or API

**Demo:** Modify a file in remote repo → trigger sync → search returns updated content.

---

## Step 14: Browser acceptance tests (Cypress)

**Objective:** Verify the entire git repos feature works end-to-end in a real browser. Catches issues invisible to unit tests: routing, server action serialization, nginx proxy rewrites, error rendering, and interactive UI flows.

**Implementation guidance:**

The project already uses Cypress (`ui/cypress/`) with `start-server-and-test` to boot the Next.js dev server. Add a new spec file alongside the existing smoke test.

- Create `ui/cypress/e2e/git-repos.cy.ts` with the following test suites:

**Suite 1: Page loading and empty state**
```
- Visit /git with onboarding completed (localStorage kagent-onboarding=true)
- Assert h1 "GIT Repos" visible
- Assert "Add Repo" button visible
- Assert search bar visible
- Assert empty state message "No git repos found" when no backend or empty list
```

**Suite 2: API error handling (service unavailable)**
```
- Intercept GET /api/gitrepos → return 503 { error: "gitrepo-mcp service not configured" }
- Visit /git
- Assert ErrorState component renders with the error message
- Assert "Return to Home" button is present and navigates to /
```

**Suite 3: List repos with mock data**
```
- Intercept GET /api/gitrepos → return fixture array of 2 repos:
  [{ name: "test-repo", url: "https://...", branch: "main", status: "indexed", fileCount: 10, chunkCount: 100, ... },
   { name: "errored-repo", status: "error", error: "clone failed", ... }]
- Visit /git
- Assert both repos appear in the list
- Assert status badges: green "Indexed" for test-repo, red "Error" for errored-repo
- Assert file/chunk counts visible for indexed repo
- Click row → assert expanded details (URL, branch, timestamps)
- Click again → assert collapsed
```

**Suite 4: Add repo form flow**
```
- Visit /git/new
- Assert form fields: name, URL, branch (default "main")
- Submit empty form → assert validation prevents submission
- Fill name="my-repo", URL="https://github.com/foo/bar.git"
- Intercept POST /api/gitrepos → return { name: "my-repo", status: "cloning", ... }
- Submit → assert redirect to /git
- Assert new repo appears in list
```

**Suite 5: Repo actions (sync, re-index, delete)**
```
- Mock list with one "indexed" repo
- Click sync button → intercept POST /api/gitrepos/test-repo/sync → return updated repo
- Assert success toast "synced successfully"
- Click re-index button → intercept POST /api/gitrepos/test-repo/index → return updated repo
- Assert success toast "Indexing started"
- Click delete button → assert confirmation dialog appears with repo name
- Click "Cancel" → dialog closes, repo still in list
- Click delete button again → click "Delete" → intercept DELETE /api/gitrepos/test-repo → 204
- Assert success toast "deleted successfully"
- Assert repo removed from list
```

**Suite 6: Semantic search**
```
- Mock list with one indexed repo
- Type "authentication middleware" in search bar → press Enter
- Intercept POST /api/gitrepos/search → return fixture with 2 results:
  [{ repo: "test-repo", filePath: "auth.go", lineStart: 10, lineEnd: 25, score: 0.85, chunkType: "function", chunkName: "Authenticate", content: "func Authenticate...", context: { before: [...], after: [...] } }, ...]
- Assert search results component renders
- Assert file path, score badge, code content visible
- Assert context lines shown
- Click X to clear search → assert results hidden, input cleared
```

**Suite 7: Loading and busy states**
```
- Intercept GET /api/gitrepos with 2s delay → assert LoadingState spinner visible
- Mock list with one repo, click sync → assert spinner on sync button, buttons disabled
```

**Suite 8: Live integration (optional, requires running backend)**
```
- Tag: @live (skip in CI, run manually)
- Visit /git → assert page loads without errors
- Add a small public repo (e.g., https://github.com/simonw/datasette-hello-world.git)
- Wait for status to change from "cloning" → "cloned"
- Trigger index → wait for "indexed"
- Search for a known string → assert results returned
- Delete repo → assert removed
```

**Fixtures:**
- Create `ui/cypress/fixtures/git-repos.json` — array of mock repos
- Create `ui/cypress/fixtures/git-search-results.json` — mock search results

**Cypress intercepts pattern:**
```typescript
// Example: mock the Next.js server action (RSC call)
// Since server actions use POST with RPC-style calls, intercept at the API level
// by stubbing the backend URL that fetchApi calls.
cy.intercept('GET', '**/api/gitrepos*', { fixture: 'git-repos.json' }).as('getRepos');
cy.visit('/git');
cy.wait('@getRepos');
```

**Important:** Since the UI uses Next.js server actions (`"use server"`), the fetch happens server-side, not in the browser. Cypress `cy.intercept()` only intercepts browser-level requests. Two approaches:
1. **Preferred:** Set `NEXT_PUBLIC_BACKEND_URL` to a test URL and use `cy.intercept()` to mock the Next.js → backend call at the network level (works if Next.js makes the call from the browser via a fetch in `useEffect`)
2. **Alternative:** Spin up a lightweight mock HTTP server on a test port that returns fixture data, set `NEXT_PUBLIC_BACKEND_URL` to point at it

Since this UI uses `"use client"` pages that call server actions, the actual HTTP requests go from the Next.js server process, not the browser. The browser sends RSC/server-action RPCs. Therefore:
- **Option A (recommended):** Create a tiny Express/Fastify mock server in `ui/cypress/support/mock-backend.ts` that serves fixture responses on the expected endpoints. Start it before tests via `cy.task()` or `start-server-and-test`.
- **Option B:** Refactor `fetchApi` to support a test mode where `getBackendUrl()` returns a URL the browser can reach, allowing `cy.intercept()` to work.

**npm scripts to add:**
```json
{
  "test:e2e:git": "start-server-and-test dev http://localhost:8001 'cypress run --spec cypress/e2e/git-repos.cy.ts'",
  "test:e2e:git:open": "start-server-and-test dev http://localhost:8001 'cypress open --e2e --spec cypress/e2e/git-repos.cy.ts'"
}
```

**Test requirements:**
- All 7 mock-based suites pass in CI without any backend running
- Suite 8 (@live) passes manually when gitrepo-mcp is deployed
- No flaky waits — use `cy.wait('@alias')` for network, `cy.contains().should('be.visible')` for UI
- Tests are independent — each suite sets up its own intercepts/fixtures

**Integration notes:**
- Matches existing `ui/cypress/e2e/smoke.cy.ts` patterns (localStorage setup, `cy.visit`, `cy.contains`)
- Add `data-test` attributes to key UI elements for stable selectors:
  - `data-test="git-repo-row-{name}"` on each repo row
  - `data-test="git-repo-sync-{name}"` on sync button
  - `data-test="git-repo-delete-{name}"` on delete button
  - `data-test="git-search-input"` on search input
  - `data-test="git-search-submit"` on search button
  - `data-test="git-add-repo-btn"` on "Add Repo" button
- Update `ui/src/app/git/page.tsx` and `ui/src/app/git/new/page.tsx` to include `data-test` attributes

**Demo:** `npm run test:e2e:git:open` opens Cypress, shows all git repos test suites running in a real browser, mocked data flows through the full UI correctly.
