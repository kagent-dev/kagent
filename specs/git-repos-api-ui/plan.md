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
- [ ] Step 3: Reflex embedded subprocess (MCP proxy + indexing trigger)
- [ ] Step 4: REST API server
- [ ] Step 5: Unified MCP server (native + Reflex proxy)
- [ ] Step 6: Kagent proxy handlers
- [ ] Step 7: Kagent UI — list page + add form
- [ ] Step 8: Helm chart + Dockerfile
- [ ] Step 9: Sync + re-index + CronJob support
- [ ] Step 10: Browser acceptance tests (Cypress)

---

## Step 1: Scaffold gitrepo-mcp CLI + SQLite storage

**Objective:** Bootstrap the Go CLI binary with Cobra, GORM/SQLite, and project structure.

**Implementation guidance:**
- Create `go/cmd/gitrepo-mcp/main.go` with Cobra root command
- Create `go/cmd/gitrepo-mcp/internal/storage/` with GORM model for `repos` table
- Use `glebarez/sqlite` driver (same as kagent)
- Implement `storage.New(dataDir)` → opens/creates SQLite DB, runs AutoMigrate
- Add `--data-dir` persistent flag on root command (default: `./data`)
- Add placeholder subcommands: `serve`, `add`, `list`, `remove`, `sync`

**Test requirements:**
- Unit test: DB opens, AutoMigrate creates table, basic CRUD on repos table
- Verify model serializes/deserializes correctly

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
  - `Remove(name)` — delete repo dir (including `.reflex/`) + DB row
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

## Step 3: Reflex embedded subprocess (MCP proxy + indexing trigger)

**Objective:** Embed Reflex inside gitrepo-mcp as a subprocess. Proxy Reflex's MCP tools through the same `/mcp` endpoint so agents see one unified tool surface.

**Why embedded (not separate server):**
- Single MCP connection for agents — all tools (repo mgmt + search + deps) in one place
- Single Helm deployment — no sidecar coordination
- gitrepo-mcp controls Reflex lifecycle (start/stop/restart)
- Shared PVC access is automatic (same process)

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
  - No prefix — Reflex tool names (`search_code`, `find_circular`, etc.) don't collide with native names (`add_repo`, `list_repos`, etc.)
  - `CallTool(name, params) (result, error)` — forward to subprocess via stdio JSON-RPC, read response, return
  - Maintain a routing table: native tool names → Go handlers, Reflex tool names → subprocess
  - Handle timeouts: 30s per call, return MCP error on timeout
  - Handle subprocess crash mid-call: return error, trigger restart

- **`indexer.go`** — trigger Reflex indexing:
  - `IndexRepo(repoPath string) error` — run `rfx index` as a one-shot command (not via MCP, just exec)
  - Called after successful clone (Step 2) and after sync/pull (Step 9)
  - Non-blocking: run in goroutine, update repo metadata with index status
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
- Step 5 (MCP server) will merge native tools + proxied Reflex tools into one tool list
- Reflex subprocess starts when `gitrepo-mcp serve` starts (eager at startup)
- Each repo gets its own `.reflex/` index inside `<data-dir>/repos/<name>/.reflex/`

**Demo:** `gitrepo-mcp serve` → agent connects to `/mcp` → `tools/list` returns both `add_repo` AND `search_code`, `find_circular`, etc.

---

## Step 4: REST API server

**Objective:** Serve repo management via REST API endpoints.

**Implementation guidance:**
- Create `go/cmd/gitrepo-mcp/internal/server/rest.go`:
  - Use `gorilla/mux` router (same as kagent)
  - Inject repo manager, Reflex indexer into handler struct
  - Implement 6 endpoints per design doc
  - JSON request/response with standard error format
  - Logging middleware
  - Health check: `GET /health`
- Async operations: clone and index can be long-running
  - `POST /api/repos` — start clone in goroutine, return immediately with status "cloning"
  - `POST /api/repos/{name}/index` — trigger `rfx index` in goroutine, return immediately with status "indexing"
  - Client polls `GET /api/repos/{name}` to check progress
- Add to `serve` command: `gitrepo-mcp serve --port 8090 --data-dir /data`

**Test requirements:**
- Unit test: each handler (mock dependencies)
- Test request validation (missing fields, invalid repo name)
- Test async status transitions

**Integration notes:**
- REST API is the interface kagent proxies to
- Search is NOT exposed via REST — search goes through MCP tools only

**Demo:** `gitrepo-mcp serve --port 8090` → `curl localhost:8090/api/repos` returns repo list.

---

## Step 5: Unified MCP server (native + Reflex proxy)

**Objective:** Expose all tools — native and proxied Reflex — via a single MCP endpoint at `/mcp`.

**Implementation guidance:**
- Create `go/cmd/gitrepo-mcp/internal/server/mcp.go`:
  - Use `mark3labs/mcp-go` SDK (or equivalent Go MCP library)
  - Register 4 native tools: `add_repo`, `list_repos`, `remove_repo`, `sync_repo`
  - Each native tool handler delegates to repo manager
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
- Integration test: call both `add_repo` and `search_code` via MCP client, verify responses

**Integration notes:**
- Agents see ONE MCP server with 4 native + 13 Reflex = 17 tools
- Agents connect to gitrepo-mcp once and get everything
- MCP, REST, and Reflex subprocess all run in same process

**Demo:** Agent connects to `/mcp` → `tools/list` returns 17 tools → agent calls `add_repo` (native) and `search_code` (Reflex) in the same session.

---

## Step 6: Kagent proxy handlers

**Objective:** Add proxy handlers to kagent HTTP server that forward to gitrepo-mcp REST API.

**Implementation guidance:**
- Create `go/internal/httpserver/handlers/gitrepos.go`:
  - `GitReposHandler` struct with `Base` + `GitRepoMCPURL string`
  - Generic proxy function: read request body → forward to gitrepo-mcp URL → stream response back
  - Auth check on each handler (same pattern as other handlers)
  - Handle connection errors to downstream (return 502)
- Register routes in `go/internal/httpserver/server.go`:
  - 6 routes under `/api/gitrepos/*`
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

## Step 7: Kagent UI — list page + add form

**Objective:** Replace the "Coming soon" git page with a functional repo list and add form.

**Implementation guidance:**
- Create `ui/src/app/actions/gitrepos.ts`:
  - Server actions: `getGitRepos`, `addGitRepo`, `removeGitRepo`, `syncGitRepo`, `indexGitRepo`
  - Use `fetchApi` pattern from `utils.ts`
- Add types to `ui/src/types/index.ts`: `GitRepo`, `AddGitRepoRequest`
- Replace `ui/src/app/git/page.tsx`:
  - "use client" list page following AgentCronJob pattern
  - Table: name, URL, branch, status badge, last synced, file count
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

## Step 8: Helm chart + Dockerfile

**Objective:** Package gitrepo-mcp for Kubernetes deployment.

**Implementation guidance:**
- Create `contrib/tools/gitrepo-mcp/`:
  - `Dockerfile`: multi-stage build — Go binary + reflex-search binary
  - `Chart.yaml`: chart metadata
  - `values.yaml`: port, PVC size, git credentials
  - `templates/deployment.yaml`: pod with PVC mount, env vars for data-dir
  - `templates/service.yaml`: ClusterIP service
  - `templates/pvc.yaml`: PersistentVolumeClaim for repos + DB
  - `templates/secret.yaml`: git credentials (optional)
  - `templates/cronjob.yaml`: optional periodic sync CronJob
- Dockerfile layers:
  1. Build Go binary
  2. Install reflex-search binary (`npm install -g reflex-search` or download from cargo)
  3. Runtime image: distroless/base + binary + reflex
- Values:
  ```yaml
  replicaCount: 1
  port: 8090
  persistence:
    size: 10Gi
    storageClass: ""
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

## Step 9: Sync + re-index + CronJob support

**Objective:** Implement git pull sync and Reflex re-indexing.

**Implementation guidance:**
- Add to repo manager:
  - `Sync(name)` — `git -C <repo-path> pull`, update `last_synced` timestamp
  - After pull, trigger `rfx index` (Reflex's incremental indexing handles changed files via blake3 hashing)
- Wire up `sync` CLI command and REST endpoint:
  - `POST /api/repos/{name}/sync` → pull + trigger Reflex re-index
- CronJob support:
  - `POST /api/repos/{name}/sync` is the target
  - Helm chart CronJob template calls this endpoint for each registered repo
  - Or: `gitrepo-mcp sync --all` CLI command that syncs all repos
- Concurrency guard: mutex per repo to prevent concurrent sync/index operations

**Test requirements:**
- Unit test: git pull executes, status updated
- Unit test: `rfx index` triggered after pull
- Integration test: modify file → sync → verify Reflex index updated

**Integration notes:**
- CronJob in Helm chart is optional (disabled by default)
- Users can also trigger sync manually via UI or API

**Demo:** Modify a file in remote repo → trigger sync → Reflex search returns updated content.

---

## Step 10: Browser acceptance tests (Cypress)

**Objective:** Verify the git repos UI works end-to-end in a real browser.

**Implementation guidance:**

Create `ui/cypress/e2e/git-repos.cy.ts` with the following test suites:

**Suite 1: Page loading and empty state**
- Visit /git with onboarding completed
- Assert h1 "GIT Repos" visible
- Assert "Add Repo" button visible
- Assert empty state message when no backend or empty list

**Suite 2: API error handling**
- Intercept GET /api/gitrepos → return 503
- Assert ErrorState component renders

**Suite 3: List repos with mock data**
- Intercept GET /api/gitrepos → return fixture array
- Assert repos appear with status badges and file counts
- Assert expandable rows

**Suite 4: Add repo form flow**
- Visit /git/new, fill form, submit
- Assert redirect to /git

**Suite 5: Repo actions (sync, re-index, delete)**
- Mock list, click sync/re-index/delete
- Assert feedback (toast, spinner, confirmation dialog)

**Suite 6: Loading states**
- Intercept with delay → assert spinner visible

**Suite 7: Live integration (@live tag)**
- Full flow: add → index → delete

**Test requirements:**
- All mock-based suites pass in CI without backend
- Suite 7 passes manually when gitrepo-mcp is deployed
- Tests are independent — each suite sets up its own intercepts

**Integration notes:**
- Matches existing `ui/cypress/e2e/smoke.cy.ts` patterns
- Add `data-test` attributes to key UI elements

**Demo:** `npm run test:e2e:git:open` opens Cypress with all test suites passing.
