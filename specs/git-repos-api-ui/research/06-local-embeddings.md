# Local Embeddings Research

## User Preference: Google Gemma Embedding (CPU)

### EmbeddingGemma-300M
- **Source:** Google DeepMind, derived from Gemma 3
- **Parameters:** 300M (lightweight)
- **Dimensions:** 768 (truncatable to 512/256/128 via Matryoshka)
- **RAM:** Under 200MB with quantization
- **Latency:** <22ms on EdgeTPU
- **Languages:** 100+
- **Caveat:** No float16 support — use float32 or bfloat16
- **Ref:** https://www.bentoml.com/blog/a-guide-to-open-source-embedding-models

### Go Integration Options

**Option 1: ONNX Runtime (recommended)**
- `yalue/onnxruntime_go` — CGO-based, most mature
- Export Gemma model to ONNX format from HuggingFace
- Requires: ONNX Runtime shared lib (~100MB) + model file
- Handles tokenization separately (need Go tokenizer)

**Option 2: fastembed-go**
- `Anush008/fastembed-go` — higher-level wrapper over ONNX Runtime
- May support Gemma if added to model registry, otherwise custom model loading

**Option 3: Ollama sidecar**
- Ollama supports embedding models including Gemma variants
- Go client available, but adds a sidecar dependency
- Simplest integration, heaviest deployment

### Vector Storage Options

**sqlite-vec (best fit for standalone MCP server)**
- Separate SQLite DB for vector storage
- `ncruces/go-sqlite3` + sqlite-vec WASM bindings (no CGO)
- KNN search via SQL: `WHERE embedding MATCH ? ORDER BY distance LIMIT k`

**coder/hnsw (simpler alternative)**
- Pure Go, in-memory HNSW
- Save/load to disk
- Good for moderate scale (<1M vectors)

### Graph Storage: FalkorDB
- External dependency (Redis-compatible protocol)
- Cypher query language
- Go client: `FalkorDB/falkordb-go`
- Separate Helm chart deployment

## Recommended Stack for MCP Server
```
Embedding:   EmbeddingGemma-300M via ONNX Runtime (CPU)
Vector DB:   sqlite-vec (embedded, same process)
Graph DB:    FalkorDB (external, Helm chart)
Storage:     PVC for cloned repos + SQLite DB
```
