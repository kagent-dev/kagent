# EP-[1256]: [Supporting Long-term Memory for Agents]

- Issue: [#1256](https://github.com/kagent-dev/kagent/issues/1256)

## Background

A well-designed memory system consists of two parts:

1. A **memory store** that allows for efficient generation, storage, and retrieval of memories. This ranges from a simple keyword search to complex knowledge-graph and semantic hybrid searches.

2. A **memory usage** workflow in the agent / agent runtime so that the agent can effectively use the memory. This is done primarily via interfaces exposed by ADK for now, but there are rooms for improvement.

## Motivation

Agents require long-term memory to remember and learn from past interactions.

### Goals

1. Include a vector store built-in option for Kagent

2. Support a built-in memory store using semantic search for agents

3. Extend various agent and backend interfaces to support memory

### Non-Goals

1. RAG and Knoweldge Base

2. Graph based memory

## Implementation

### 1. Database

#### PostgreSQL + pgvector

- **Table**: `memory` table stores the actual memory entries.
- **Columns**:
  - `content` (Text): The actual memory text.
  - `embedding` (Vector): 768-dimensional vector using `pgvector`.
  - `metadata` (JSON): Stores source session ID, timestamp, app name.
  - `access_count` (Int): Tracks how many times this memory has been retrieved.
  - `expires_at` (Timestamp): Defines when the memory is eligible for pruning (TTL).
- **Indexing**: Uses HNSW (Hierarchical Navigable Small World) index (`idx_memory_embedding_hnsw`) with `vector_cosine_ops` for efficient approximate nearest neighbor search.

Note that this does not use `pgvectorscale` which is more performant than the original `pgvector`, but is bundled with `timescaledb` which has some setup overhead and possible compatibility issues. If needed we can switch over to the [timescaledb container image](https://www.tigerdata.com/docs/self-hosted/latest/install/installation-docker) instead which has the vector scale extension built-in.

#### SQLite / Turso (Local Development)

> This change replaced the original driver `go-sqlite` based on `modernc/sqlite` with `turso` go driver for all sqlite database usage.

- **Driver**: All SQLite connections use Turso (`turso.tech/database/tursogo`), which embeds libSQL with native vector support (no CGO; container needs C/C++ runtime libs). GORM talks to it via `glebarez/sqlite` as a dialector over the Turso `*sql.DB`.
- **Schema**:
  - `embedding` (F32_BLOB): 768-dimensional float32 blob `F32_BLOB(768)`.
- **Query Syntax**:
  - Uses `vector_distance_cos(embedding, vector32(?))` for similarity search.
  - Requires specific handling of vector params (passed as JSON string literals) due to driver limitations.
  - Does not use the same query syntax as Postgres.
- **Indexing**:
  - Uses brute-force scan for small datasets (efficient for under ~10k vectors).
  - Supports `libsql_vector_idx` for ANN at larger scales (currently using direct scan; some issues when enabling the index).

An alternative would be to fork `glebarez/sqlite` and replace everything with `turso` so we can remove the dependency of `go-sqlite` and `modernc/sqlite`. Both works.

**SQLite vector alternatives (not used):**

| Option | Why not used |
|--------|----------------|
| **CGO SQLite + sqlite-vec** (e.g. mattn/go-sqlite3 + vector extension) | Lots of reliable extensions exist, but requires CGO. |
| **SQLite vtable API from modernc sqlite** | Purego (current choice), but no existing extensions for vector, need to homebrew (non-trivial). |
| **Turso/libSQL native (F32_BLOB)** | **What we use.** No CGO, native vector support in the engine, single driver path for all SQLite, optimized vector search. |

[Turso's AI and Embedding documentation can be found here](https://docs.turso.tech/features/ai-and-embeddings)

### 2. Kagent Controller (Go)

#### HTTPServer

- POST `/api/memories/sessions`: Adds memories (with default 15-day TTL).
- POST `/api/memories/sessions/batch`: Adds memories in batch (with default 15-day TTL).
- POST `/api/memories/search`: Performs cosine similarity search.
- GET `/api/memories`: returns all memories for an agent+user, ranked by access frequency.
- DELETE `/api/memories`: Clears memories for an agent/user.

The controller does not provide endpoints for embedding and reranking or summarising. These are done in the Python runtime instead, see below.

#### Translator

**Embedding config in AgentConfig**: The `embedding` field is an `EmbeddingConfig` (provider, model, optional base_url). Serialized JSON uses the key `provider` (not `type`) so it matches Python’s `EmbeddingConfig` schema. The translator turns the resolved ModelConfig into `EmbeddingConfig` via `ModelToEmbeddingConfig()`. Unmarshaling accepts either `type` or `provider` in JSON. You may use a different provider for LLM and embedding models.

#### CRD

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
...
spec:
  type: Declarative
  declarative:
    ...
    memory:
      enabled: true
      modelConfig: embedding-model # create this separately
```

### 3. Python Agent Runtime

- **Tools**:
  - `SaveMemoryTool`: Saves explicit text facts to memory.
  - `LoadMemoryTool`: Semantic search by query; returns compact JSON (null/empty fields omitted) to keep token usage low when passed to the LLM.
  - `PrefetchMemoryTool`: Runs on the first user message only and auto-appends relevant past context to the initial user message.
- **Service**:
  - `KagentMemoryService`: Embedding generation (LiteLLM, 768 dims) and session summarization. Expects an `EmbeddingConfig` instance (e.g. `agent_config.embedding`).
  - **Auto-Save Callback**: Fires `add_session_to_memory` every 5 user turns.
- **Instruction**: When memory is enabled, the agent’s instruction is extended with two lines: use `save_memory` for findings and learnings, and `load_memory` when more context is needed.

### 4. UI

The user can view memories for an agent and clear it manually. UI section to configure agent with memory.

## Memory Mechanism

This describes how the memory system works for an agent. This is inspired by the [memory docs from ADK](https://google.github.io/adk-docs/sessions/memory/#how-memory-works-in-practice).

### Saving Memory

Memory is saved in two ways:

1. **Auto-Save (Periodic)**: Every **5 user turns**, the `auto_save_session_to_memory_callback` is triggered.
   - The current session content is sent to the LLM to "extract and summarize key information".
   - The summary is embedded and stored in the database.
   - This happens async as a background process since the agent interaction does not rely on memory being immediately available.
2. **Explicit Save (Tool)**: The agent can decide to call `SaveMemoryTool(content="...")`.
   - This saves the specific content immediately without summarization.

**Default TTL**: All new memories are created with an expiration date (`expires_at`) set to **15 days** from creation.

### Retrieving Memory

- **Search**: When the agent calls `LoadMemoryTool(query="...")`:
  1. The query string is embedded into a vector.
  2. The database performs a vector similarity search (Cosine Similarity).
  3. Results are filtered by a `min_score` (currently ~0.3) to ensure relevance.
- **Popularity Tracking**: When a memory is successfully returned in a search, its `access_count` is incremented in the background. This signals that the memory is "useful".
- **Prefetch**: Memory is prefetched only on the first user message in a session; top results are injected into that turn’s LLM request. Prefetch does not run on every LLM call (that would be too expensive). User prompt is split into setences before searching so that entries matching only part of the query will still hit in order to provide better context (additionally, most embedding models traditionally work best with sentences).

### Pruning and Deletion

A pruning process (`PruneExpiredMemories`) manages the lifecycle of memories:

1. **Identify Expired**: Looks for items where `expires_at < Now`.
2. **Check Popularity**:
   - **Keep Popular**: If `access_count >= 10`, the memory is deemed valuable. Its `expires_at` is extended by **15 days**, and `access_count` is reset to 0.
   - **Delete Unpopular**: If `access_count < 10`, the memory is permanently deleted from the database.

This is run like a cron job every 24 hours.

The user can also choose to delete all memories for a specific agent from the UI.

## Limitations

1. **Embedding Support**: First-class support for diverse embedding models is limited. We currently hardcode dimensions to 768 and use basic truncation/normalization if the model output differs.

2. **No Reranking**: There is no re-ranking step (e.g., using a Cross-Encoder) after vector retrieval. Results are ranked purely by cosine similarity, which may miss subtle nuances.

3. **No Hybrid Search**: We rely solely on dense vector retrieval. There is no sparse (keyword/BM25) search, which can lead to poor performance on exact match queries (e.g., searching for a specific error code or ID).

4. **Scaling & Performance**: The performance of `pgvector` with HNSW indices at large scale (millions of vectors) with this specific configuration has not been extensively benchmarked against dedicated vector databases (e.g., Milvus, Qdrant). We might want to consider using `pgvectorscale` if we hit performance issues or use a dedicated vector database.

5. **Agent Overhead**: Memory tools consume token budget. The auto-save summarization step uses additional LLM calls, which increases cost and latency.

6. **Duplication & Consolidation**: There is no background "consolidation" process. If the auto-save summarizes the same facts repeatedly, or if the agent saves the same info explicitly, duplicate semantic entries will exist in the DB, potentially crowding out diverse results in retrieval.

## Future Improvements

1. **Hybrid search and reranking**: This is a common new pattern in LLM applications that would be helpful for memory retrieval. It would allow for both dense vector similarity and sparse keyword matching, potentially leading to better retrieval performance. These results will then be reranked using a cross-encoder model that takes in all the results as well as the original query to provide higher quality retrieval. [Here is an example from Qdrant](https://qdrant.tech/documentation/fastembed/fastembed-rerankers/)

2. **Consolidation Step**: Most production memory store have an extraction and consolidation process when saving memory. We have a basic extraction step but lacks a consolidation step. This usually involves retrieving some relevant entries, potentially updating them, and then saving them back to the database (or create new entries if no related entires exist). Note that this likely involves making memory saving a background process because this takes quite some time, we need to extract from session, fetch similar from Go backend, then in Python memory interface call LLM again to consolidate, and write back the operations (ADD or UDPATE) to the Go backend (then to vector store). [Here is an example from Vertex AI](https://docs.cloud.google.com/agent-builder/agent-engine/memory-bank/generate-memories)
