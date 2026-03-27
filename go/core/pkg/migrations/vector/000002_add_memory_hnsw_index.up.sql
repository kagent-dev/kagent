-- Add HNSW index for fast approximate nearest-neighbor vector similarity search.
-- GORM did not create this index automatically; pgvector's HNSW index significantly
-- outperforms IVFFlat for production workloads (better recall, no reindex on insert).
CREATE INDEX IF NOT EXISTS idx_memory_embedding_hnsw ON memory USING hnsw (embedding vector_cosine_ops);
