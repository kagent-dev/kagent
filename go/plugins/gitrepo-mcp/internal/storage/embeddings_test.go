package storage

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeEmbedding(t *testing.T) {
	original := []float32{1.0, -2.5, 3.14159, 0.0, math.MaxFloat32, math.SmallestNonzeroFloat32}
	encoded := EncodeEmbedding(original)
	decoded := DecodeEmbedding(encoded)

	require.Len(t, decoded, len(original))
	for i := range original {
		assert.Equal(t, original[i], decoded[i], "mismatch at index %d", i)
	}
}

func TestEncodeEmbedding_Empty(t *testing.T) {
	encoded := EncodeEmbedding(nil)
	assert.Len(t, encoded, 0)
	decoded := DecodeEmbedding(encoded)
	assert.Len(t, decoded, 0)
}

func TestEmbeddingStore_GetOrCreateCollection(t *testing.T) {
	mgr := newTestManager(t)
	repoStore := NewRepoStore(mgr.DB())
	embStore := NewEmbeddingStore(mgr.DB())

	// Must create repo first (foreign key)
	require.NoError(t, repoStore.Create(&Repo{
		Name:      "test-repo",
		URL:       "https://example.com/test.git",
		Branch:    "main",
		Status:    RepoStatusCloned,
		LocalPath: "/data/repos/test-repo",
	}))

	// Create collection
	coll, err := embStore.GetOrCreateCollection("test-repo", "gemma-300m", 768)
	require.NoError(t, err)
	assert.Equal(t, "test-repo", coll.RepoName)
	assert.Equal(t, "gemma-300m", coll.Model)
	assert.Equal(t, 768, coll.Dimensions)
	assert.NotZero(t, coll.ID)

	// Get same collection (idempotent)
	coll2, err := embStore.GetOrCreateCollection("test-repo", "gemma-300m", 768)
	require.NoError(t, err)
	assert.Equal(t, coll.ID, coll2.ID)
}

func TestEmbeddingStore_InsertAndQueryChunks(t *testing.T) {
	mgr := newTestManager(t)
	repoStore := NewRepoStore(mgr.DB())
	embStore := NewEmbeddingStore(mgr.DB())

	require.NoError(t, repoStore.Create(&Repo{
		Name:      "test-repo",
		URL:       "https://example.com/test.git",
		Branch:    "main",
		Status:    RepoStatusCloned,
		LocalPath: "/data/repos/test-repo",
	}))

	coll, err := embStore.GetOrCreateCollection("test-repo", "gemma-300m", 768)
	require.NoError(t, err)

	embedding := EncodeEmbedding([]float32{1.0, 2.0, 3.0})
	chunks := []Chunk{
		{
			CollectionID: coll.ID,
			FilePath:     "main.go",
			LineStart:    1,
			LineEnd:      10,
			ChunkType:    "function",
			Content:      "func main() {}",
			ContentHash:  "abc123",
			Embedding:    embedding,
		},
		{
			CollectionID: coll.ID,
			FilePath:     "main.go",
			LineStart:    12,
			LineEnd:      20,
			ChunkType:    "function",
			Content:      "func helper() {}",
			ContentHash:  "def456",
			Embedding:    embedding,
		},
	}

	require.NoError(t, embStore.InsertChunks(chunks))

	// Query chunks
	got, err := embStore.GetChunksByCollection(coll.ID)
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "main.go", got[0].FilePath)
}

func TestEmbeddingStore_ChunkExistsByHash(t *testing.T) {
	mgr := newTestManager(t)
	repoStore := NewRepoStore(mgr.DB())
	embStore := NewEmbeddingStore(mgr.DB())

	require.NoError(t, repoStore.Create(&Repo{
		Name:      "test-repo",
		URL:       "https://example.com/test.git",
		Branch:    "main",
		Status:    RepoStatusCloned,
		LocalPath: "/data/repos/test-repo",
	}))

	coll, err := embStore.GetOrCreateCollection("test-repo", "gemma-300m", 768)
	require.NoError(t, err)

	embedding := EncodeEmbedding([]float32{1.0})
	require.NoError(t, embStore.InsertChunks([]Chunk{
		{
			CollectionID: coll.ID,
			FilePath:     "main.go",
			LineStart:    1,
			LineEnd:      10,
			ChunkType:    "function",
			Content:      "func main() {}",
			ContentHash:  "hash1",
			Embedding:    embedding,
		},
	}))

	exists, err := embStore.ChunkExistsByHash(coll.ID, "hash1")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = embStore.ChunkExistsByHash(coll.ID, "hash2")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestEmbeddingStore_DeleteChunksByFile(t *testing.T) {
	mgr := newTestManager(t)
	repoStore := NewRepoStore(mgr.DB())
	embStore := NewEmbeddingStore(mgr.DB())

	require.NoError(t, repoStore.Create(&Repo{
		Name:      "test-repo",
		URL:       "https://example.com/test.git",
		Branch:    "main",
		Status:    RepoStatusCloned,
		LocalPath: "/data/repos/test-repo",
	}))

	coll, err := embStore.GetOrCreateCollection("test-repo", "gemma-300m", 768)
	require.NoError(t, err)

	embedding := EncodeEmbedding([]float32{1.0})
	require.NoError(t, embStore.InsertChunks([]Chunk{
		{CollectionID: coll.ID, FilePath: "a.go", LineStart: 1, LineEnd: 5, ChunkType: "function", Content: "a", ContentHash: "h1", Embedding: embedding},
		{CollectionID: coll.ID, FilePath: "b.go", LineStart: 1, LineEnd: 5, ChunkType: "function", Content: "b", ContentHash: "h2", Embedding: embedding},
	}))

	require.NoError(t, embStore.DeleteChunksByFile(coll.ID, "a.go"))

	chunks, err := embStore.GetChunksByCollection(coll.ID)
	require.NoError(t, err)
	assert.Len(t, chunks, 1)
	assert.Equal(t, "b.go", chunks[0].FilePath)
}
