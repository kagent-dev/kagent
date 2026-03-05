# ast-grep Research

Reference: https://ast-grep.github.io/

## What It Is
AST-based structural code search, lint, and rewrite tool. Written in Rust, uses tree-sitter parsers. Unlike regex grep, it understands code structure.

## Key Capabilities
- **Structural search:** `ast-grep -p 'func $NAME($$$ARGS) error'` — finds Go functions returning error
- **Rewrite:** `ast-grep -p 'PATTERN' -r 'REPLACEMENT'` — syntax-aware refactoring
- **Lint:** YAML-configured rules, `ast-grep scan`
- **20+ languages:** Go, Python, JS/TS, Java, C/C++, Rust, Ruby, etc.

## Why It's Relevant for Code Indexing
- Can extract structural elements (functions, classes, methods, imports) from any supported language
- AST-aware chunking > naive line-based chunking for embeddings
- Could replace/complement AST parsing for the future graph feature
- Provides precise code search that semantic search may miss

## Integration Options for Go MCP Server
1. **Shell out to CLI** — simplest, `ast-grep` as a binary dependency
2. **Tree-sitter Go bindings** — `smacker/go-tree-sitter` provides native Go tree-sitter access
3. **NAPI/Python bindings** — not useful for a Go service

## Potential Use Cases in This Project
- **Smart chunking:** Use AST to split files into function/method/class chunks before embedding (vs. naive line splitting)
- **Structural search MCP tool:** Expose `ast-grep` pattern search as an MCP tool alongside semantic search
- **Hybrid search:** Combine semantic similarity + structural AST matching

## Relationship to Scope
- **Phase 1 (semantic search):** Could use tree-sitter for smarter file chunking
- **Phase 2 (graph):** AST parsing → FalkorDB nodes/edges (future feature)
