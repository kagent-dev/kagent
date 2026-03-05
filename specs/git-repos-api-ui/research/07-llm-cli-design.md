# LLM CLI Design Patterns (Simon Willison)

Reference: https://llm.datasette.io/ | https://github.com/simonw/llm

## UX Model (What We're Adopting)
```bash
# Install embedding model plugin
llm install llm-sentence-transformers

# Batch embed files into a named collection
llm embed-multi myrepo -m sentence-transformers/all-MiniLM-L6-v2 --files . '**/*.go'

# Similarity search against collection
llm similar myrepo -c "where do we set up auth?"
```

## Key Design Patterns

### 1. Named Collections
- String key ("myrepo") namespaces embeddings in DB
- Each collection locked to one embedding model
- Created automatically on first use

### 2. SQLite + BLOB Storage
- Vectors stored as little-endian float32 BLOBs
- 384-dim vector = 1,536 bytes
- Two tables: `collections` (name, model) + `embeddings` (collection_id, id, embedding, content, content_hash, metadata)
- Separate DB from other app data

### 3. Content-Hash Deduplication
- MD5 hash of content before embedding
- On re-run, only changed files get re-embedded
- Makes `embed-multi` idempotent for incremental updates

### 4. Glob-Based File Discovery
- `--files <dir> <glob>` pattern
- Relative file path becomes the embedding ID
- Search results point directly to files

### 5. Brute-Force Cosine Similarity
- Sufficient for <20K embeddings (typical code repo)
- Custom SQLite function for distance calculation
- Top-N results sorted by score

### 6. Model Interface
```python
class EmbeddingModel:
    model_id: str
    batch_size: int = 100
    def embed_batch(self, items: list[str]) -> list[list[float]]: ...
```

### 7. Output: NDJSON
```json
{"id": "internal/auth/handler.go", "score": 0.832, "content": "...", "metadata": {...}}
```

## Go Translation

| Python (`llm`) | Go MCP Server |
|------|------|
| `Collection` class | `Collection` struct |
| `embed_batch()` | `EmbedBatch([]string) ([][]float32, error)` |
| `struct.pack("<f"...)` | `encoding/binary.LittleEndian` |
| MD5 content hash | SHA256 content hash |
| Pluggy hooks | Go interface + compile-time registration |
| Click CLI | Cobra CLI |
| brute-force cosine | brute-force cosine (upgrade to sqlite-vec later) |

## CLI Command Structure (Proposed for Go)
```bash
# Repo management (REST API backed)
gitrepo add <name> --url <repo-url> --branch main
gitrepo list
gitrepo remove <name>
gitrepo sync <name>        # trigger pull + re-index

# Embedding (local)
gitrepo index <name>       # AST parse + embed all files
gitrepo search <name> -c "query string"

# MCP server mode
gitrepo serve --port 8090  # REST API + MCP protocol
```
