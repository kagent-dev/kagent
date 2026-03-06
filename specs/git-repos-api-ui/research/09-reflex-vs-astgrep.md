# Reflex vs ast-grep: Analysis and Recommendation

## Reflex (reflex-search/reflex)

**What it is:** Local-first code search engine written in Rust. Combines trigram indexing + Tree-sitter parsing + static dependency analysis.

**Installation:** `npm install -g reflex-search` or `cargo install reflex-search`

**Search capabilities (4 types):**

| Type | How | Example |
|------|-----|---------|
| Full-text | Trigram inverted index | `rfx query "extract_symbols"` |
| Symbol-only | Tree-sitter runtime parsing | `rfx query "parse" --symbols --kind function` |
| Regex | Trigram-optimized | `rfx query "fn.*test" --regex` |
| AST pattern | Tree-sitter S-expressions | `rfx query "fn" --ast "(function_item) @fn" --lang rust` |

**Dependency analysis (unique to Reflex):**
- Import tracking, reverse lookups, transitive deps
- Circular dependency detection
- Hotspot analysis (most-imported files)
- Orphaned file detection
- Disconnected component detection

**MCP server (built-in, 12+ tools):**
- `search_code` — full-text or symbol search
- `search_regex` — regex matching
- `search_ast` — AST pattern matching
- `list_locations` — fast file+line discovery
- `count_occurrences` — quick stats
- `index_project` — trigger reindex
- `get_dependencies` / `get_dependents` / `get_transitive_deps`
- `find_hotspots` / `find_circular` / `find_unused` / `find_islands`
- `analyze_summary`

**Indexing:**
- Incremental via blake3 content hashing
- Memory-mapped I/O (trigrams.bin, content.bin)
- SQLite for metadata (meta.db)
- Background symbol indexing
- 80% CPU parallelism on initial index

**Languages (14+):** Go, Python, JS/TS, Java, Rust, C/C++, C#, Ruby, Kotlin, Zig, PHP, Vue, Svelte

**AI integration:** `rfx ask` — natural language → code search (OpenAI, Anthropic, Groq)

**Storage:** `.reflex/` directory per project (SQLite + memory-mapped binaries)

---

## ast-grep

**What it is:** AST-based code search, lint, and rewrite tool written in Rust.

**Search capabilities (1 type):**
- Structural AST pattern matching with metavariables

**Pattern syntax (more intuitive than tree-sitter S-expressions):**
```bash
ast-grep -p 'func $NAME($$$) error'           # Go functions returning error
ast-grep -p 'var code = $PAT' -r 'let code = $PAT'  # Search + rewrite
```

**MCP server:** None (no built-in MCP support)

**Languages (20+):** Slightly broader language support via tree-sitter

**Unique features:**
- Code rewriting/refactoring via patterns
- YAML lint rule definitions
- LSP support
- Testing framework for rules

---

## Feature Comparison

| Capability | Reflex | ast-grep | Winner |
|-----------|--------|----------|--------|
| Full-text search | Trigram index | No | Reflex |
| Symbol search | Tree-sitter | No (patterns only) | Reflex |
| Regex search | Trigram-optimized | No | Reflex |
| AST pattern search | S-expressions | Metavariable patterns | Both (different syntax) |
| Code rewriting | No | Yes (`-r` flag) | ast-grep |
| Dependency analysis | Full graph | No | Reflex |
| MCP server | Built-in (12+ tools) | None | Reflex |
| Incremental indexing | blake3 hash | N/A (no index) | Reflex |
| AI query assistant | `rfx ask` | No | Reflex |
| Pattern syntax UX | Complex (S-exprs) | Simple (`$NAME`, `$$$`) | ast-grep |
| Lint rules | No | Yes (YAML) | ast-grep |
| Languages | 14+ | 20+ | ast-grep (slightly) |
| Installation | npm/cargo | npm/cargo/brew | Tie |

---

## Recommendation: Reflex replaces ast-grep for this project

**ast-grep is redundant.** Here's why:

1. **Reflex already has an MCP server** — no wrapper code needed. The original plan (Step 6) required writing a Go wrapper that shells out to `ast-grep` CLI and parses JSON output. With Reflex, the MCP server is built-in.

2. **Reflex covers all ast-grep search use cases** — `search_ast` tool provides AST pattern matching. The syntax differs (S-expressions vs metavariables), but for agent use both work.

3. **Reflex adds capabilities ast-grep doesn't have** — full-text search, symbol search, regex search, dependency analysis, and AI query assistant. These are highly valuable for code exploration agents.

4. **Simpler architecture** — instead of gitrepo-mcp wrapping ast-grep as a subprocess, Reflex runs as a standalone MCP server that agents connect to directly.

5. **Same deployment model** — both are single binaries installed via npm/cargo. Container image adds `reflex-search` instead of `ast-grep`.

**One thing lost:** ast-grep's intuitive metavariable syntax (`func $NAME($$$) error`) is more natural than Reflex's tree-sitter S-expressions. However, Reflex's `rfx ask` agentic mode can translate natural language to the right query syntax, which is arguably better for AI agents.

---

## Architecture: Reflex Embedded Inside gitrepo-mcp

### Before (with ast-grep)
```
gitrepo-mcp serve
  ├── REST API (repo mgmt + semantic search)
  ├── MCP tools (repo mgmt + semantic search + ast_search wrapper)
  └── shells out to ast-grep binary
```

### After (Reflex embedded)
```
gitrepo-mcp serve
  ├── REST API (repo mgmt + semantic search)
  ├── Unified MCP /mcp endpoint
  │   ├── Native tools: add_repo, list_repos, remove_repo, sync_repo, semantic_search
  │   └── Proxied tools: search_code, search_ast, get_dependencies, ... (13 tools)
  └── Reflex subprocess (rfx mcp via stdio JSON-RPC)
```

**Single MCP connection for agents.** gitrepo-mcp spawns `rfx mcp` as a child process, communicates via stdio JSON-RPC, and proxies Reflex tools through its own `/mcp` endpoint. No prefix needed — native and Reflex tool names don't collide.

**Why embedded, not separate:**
- Agents connect once, get all 18 tools
- Single Helm deployment — no sidecar, no multi-server coordination
- gitrepo-mcp controls Reflex lifecycle (start/stop/restart/health)
- Shared PVC access is automatic
- Graceful degradation: if `rfx` binary missing, native tools still work

**Proxy mechanism:**
```
Agent → MCP request (tool: search_code, params: {...})
  → gitrepo-mcp routing table → Reflex tool
  → forwards to rfx subprocess via stdio JSON-RPC
  → reads response from subprocess stdout
  → wraps in MCP response → returns to agent
```

---

## Changes to Design

### Removed
- Step 6 (ast-grep structural search wrapper) — entire step replaced
- `ast_search` and `ast_search_languages` MCP tools from gitrepo-mcp
- `structural.go` from gitrepo-mcp internal packages
- ast-grep binary dependency in Dockerfile
- Separate Reflex MCP server / Helm chart / sidecar

### Added
- `internal/reflex/` package: `proxy.go` (stdio MCP proxy), `indexer.go` (trigger `rfx index`), `lifecycle.go` (subprocess mgmt)
- Reflex tools merged into unified MCP tool list (no prefix, routing table dispatches)
- Reflex binary bundled in Dockerfile
- `--reflex-enabled` and `--reflex-path` CLI flags
- Routing table dispatches tool calls: native tool names → Go handlers, Reflex tool names → subprocess

### Unchanged
- Semantic search (embedding-based) stays native in gitrepo-mcp — Reflex doesn't do embeddings
- Repo management (clone, sync, remove) stays native
- Kagent proxy + UI stays the same
- REST API stays the same (Reflex tools exposed only via MCP, not REST)
