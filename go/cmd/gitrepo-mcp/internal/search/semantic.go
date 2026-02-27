package search

import (
	"fmt"
	"math"
	"sort"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/embedder"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
)

// SearchResult represents a single semantic search match.
type SearchResult struct {
	Repo      string   `json:"repo"`
	FilePath  string   `json:"filePath"`
	LineStart int      `json:"lineStart"`
	LineEnd   int      `json:"lineEnd"`
	Score     float64  `json:"score"`
	ChunkType string   `json:"chunkType"`
	ChunkName string   `json:"chunkName,omitempty"`
	Content   string   `json:"content"`
	Context   *Context `json:"context,omitempty"`
}

// Searcher performs semantic search over indexed repositories.
type Searcher struct {
	repoStore      *storage.RepoStore
	embeddingStore *storage.EmbeddingStore
	embedder       embedder.EmbeddingModel
}

// NewSearcher creates a Searcher.
func NewSearcher(
	repoStore *storage.RepoStore,
	embeddingStore *storage.EmbeddingStore,
	emb embedder.EmbeddingModel,
) *Searcher {
	return &Searcher{
		repoStore:      repoStore,
		embeddingStore: embeddingStore,
		embedder:       emb,
	}
}

// Search performs semantic search over a single repo's indexed chunks.
// It embeds the query, computes cosine similarity against all stored embeddings,
// and returns the top-N results sorted by score descending.
func (s *Searcher) Search(query string, repoName string, limit int, contextLines int) ([]SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	if limit <= 0 {
		limit = 10
	}

	// Verify repo exists and is indexed
	repo, err := s.repoStore.Get(repoName)
	if err != nil {
		return nil, fmt.Errorf("repo %s not found: %w", repoName, err)
	}
	if repo.Status != storage.RepoStatusIndexed {
		return nil, fmt.Errorf("repo %s is not indexed (status: %s)", repoName, repo.Status)
	}

	// Embed the query
	vectors, err := s.embedder.EmbedBatch([]string{query})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	queryVec := vectors[0]

	// Load collection and all chunks
	coll, err := s.embeddingStore.GetOrCreateCollection(
		repoName,
		s.embedder.ModelName(),
		s.embedder.Dimensions(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection: %w", err)
	}

	chunks, err := s.embeddingStore.GetChunksByCollection(coll.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load chunks: %w", err)
	}

	if len(chunks) == 0 {
		return nil, nil
	}

	// Compute cosine similarity for each chunk
	type scored struct {
		chunk storage.Chunk
		score float64
	}
	results := make([]scored, len(chunks))
	for i, c := range chunks {
		chunkVec := storage.DecodeEmbedding(c.Embedding)
		results[i] = scored{
			chunk: c,
			score: CosineSimilarity(queryVec, chunkVec),
		}
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// Take top N
	if limit > len(results) {
		limit = len(results)
	}
	top := results[:limit]

	// Build SearchResult list with optional context
	out := make([]SearchResult, len(top))
	for i, s := range top {
		name := ""
		if s.chunk.ChunkName != nil {
			name = *s.chunk.ChunkName
		}
		sr := SearchResult{
			Repo:      repoName,
			FilePath:  s.chunk.FilePath,
			LineStart: s.chunk.LineStart,
			LineEnd:   s.chunk.LineEnd,
			Score:     math.Round(s.score*10000) / 10000, // 4 decimal places
			ChunkType: s.chunk.ChunkType,
			ChunkName: name,
			Content:   s.chunk.Content,
		}

		if contextLines > 0 {
			ctx, err := ExtractContext(repo.LocalPath, s.chunk.FilePath, s.chunk.LineStart, s.chunk.LineEnd, contextLines)
			if err == nil {
				sr.Context = ctx
			}
		}

		out[i] = sr
	}

	return out, nil
}

// CosineSimilarity computes dot(a,b) / (||a|| * ||b||).
// Returns 0 if either vector has zero norm.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
