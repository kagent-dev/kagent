package indexer

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/embedder"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
)

func setupTestDB(t *testing.T) (*storage.RepoStore, *storage.EmbeddingStore) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		DBType: config.DBTypeSQLite,
		DBPath: filepath.Join(tmpDir, "test.db"),
	}
	mgr, err := storage.NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return storage.NewRepoStore(mgr.DB()), storage.NewEmbeddingStore(mgr.DB())
}

func createFakeRepo(t *testing.T, repoStore *storage.RepoStore, name string) string {
	t.Helper()
	repoDir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	repo := &storage.Repo{
		Name:      name,
		URL:       "https://example.com/" + name,
		Branch:    "main",
		Status:    storage.RepoStatusCloned,
		LocalPath: repoDir,
	}
	if err := repoStore.Create(repo); err != nil {
		t.Fatalf("Create repo: %v", err)
	}
	return repoDir
}

func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestIndexer_IndexEmptyRepo(t *testing.T) {
	repoStore, embStore := setupTestDB(t)
	_ = createFakeRepo(t, repoStore, "empty")

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	if err := idx.Index("empty"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	repo, _ := repoStore.Get("empty")
	if repo.Status != storage.RepoStatusIndexed {
		t.Errorf("status = %s, want indexed", repo.Status)
	}
	if repo.FileCount != 0 {
		t.Errorf("fileCount = %d, want 0", repo.FileCount)
	}
	if repo.ChunkCount != 0 {
		t.Errorf("chunkCount = %d, want 0", repo.ChunkCount)
	}
}

func TestIndexer_IndexGoFile(t *testing.T) {
	repoStore, embStore := setupTestDB(t)
	repoDir := createFakeRepo(t, repoStore, "gotest")

	writeFile(t, repoDir, "main.go", `package main

func Hello() string {
	return "hello"
}

func World() string {
	return "world"
}
`)

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	if err := idx.Index("gotest"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	repo, _ := repoStore.Get("gotest")
	if repo.Status != storage.RepoStatusIndexed {
		t.Errorf("status = %s, want indexed", repo.Status)
	}
	if repo.FileCount != 1 {
		t.Errorf("fileCount = %d, want 1", repo.FileCount)
	}
	if repo.ChunkCount < 1 {
		t.Errorf("chunkCount = %d, want >= 1", repo.ChunkCount)
	}
	if repo.LastIndexed == nil {
		t.Error("lastIndexed should be set")
	}
}

func TestIndexer_IndexMultipleFiles(t *testing.T) {
	repoStore, embStore := setupTestDB(t)
	repoDir := createFakeRepo(t, repoStore, "multi")

	writeFile(t, repoDir, "main.go", `package main
func Main() {}
`)
	writeFile(t, repoDir, "README.md", `# My Project
## Overview
Some text here.
`)
	writeFile(t, repoDir, "config.yaml", `key: value
`)

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	if err := idx.Index("multi"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	repo, _ := repoStore.Get("multi")
	if repo.FileCount != 3 {
		t.Errorf("fileCount = %d, want 3", repo.FileCount)
	}
	if repo.ChunkCount < 3 {
		t.Errorf("chunkCount = %d, want >= 3", repo.ChunkCount)
	}
}

func TestIndexer_SkipsHiddenDirs(t *testing.T) {
	repoStore, embStore := setupTestDB(t)
	repoDir := createFakeRepo(t, repoStore, "hidden")

	writeFile(t, repoDir, "visible.go", `package main
func Visible() {}
`)
	writeFile(t, repoDir, ".git/config", `[core]
`)
	writeFile(t, repoDir, ".hidden/secret.go", `package hidden
func Secret() {}
`)

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	if err := idx.Index("hidden"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	repo, _ := repoStore.Get("hidden")
	if repo.FileCount != 1 {
		t.Errorf("fileCount = %d, want 1 (only visible.go)", repo.FileCount)
	}
}

func TestIndexer_SkipsNodeModules(t *testing.T) {
	repoStore, embStore := setupTestDB(t)
	repoDir := createFakeRepo(t, repoStore, "skipnm")

	writeFile(t, repoDir, "app.js", `function hello() { return "hi"; }
`)
	writeFile(t, repoDir, "node_modules/lodash/index.js", `module.exports = {};
`)

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	if err := idx.Index("skipnm"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	repo, _ := repoStore.Get("skipnm")
	if repo.FileCount != 1 {
		t.Errorf("fileCount = %d, want 1", repo.FileCount)
	}
}

func TestIndexer_SkipsUnsupportedExtensions(t *testing.T) {
	repoStore, embStore := setupTestDB(t)
	repoDir := createFakeRepo(t, repoStore, "unsupported")

	writeFile(t, repoDir, "main.go", `package main
func Main() {}
`)
	writeFile(t, repoDir, "image.png", "fake png data")
	writeFile(t, repoDir, "data.csv", "a,b,c\n1,2,3\n")

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	if err := idx.Index("unsupported"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	repo, _ := repoStore.Get("unsupported")
	if repo.FileCount != 1 {
		t.Errorf("fileCount = %d, want 1 (only main.go)", repo.FileCount)
	}
}

func TestIndexer_RejectsCloning(t *testing.T) {
	repoStore, embStore := setupTestDB(t)
	repoDir := createFakeRepo(t, repoStore, "busy")

	// Set to cloning status
	repo, _ := repoStore.Get("busy")
	repo.Status = storage.RepoStatusCloning
	_ = repoStore.Update(repo)
	_ = repoDir

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	err := idx.Index("busy")
	if err == nil {
		t.Fatal("expected error for busy repo")
	}
}

func TestIndexer_RejectsAlreadyIndexing(t *testing.T) {
	repoStore, embStore := setupTestDB(t)
	repoDir := createFakeRepo(t, repoStore, "indexing")

	repo, _ := repoStore.Get("indexing")
	repo.Status = storage.RepoStatusIndexing
	_ = repoStore.Update(repo)
	_ = repoDir

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	err := idx.Index("indexing")
	if err == nil {
		t.Fatal("expected error for already-indexing repo")
	}
}

func TestIndexer_NotFound(t *testing.T) {
	repoStore, embStore := setupTestDB(t)

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	err := idx.Index("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent repo")
	}
}

func TestIndexer_BatchProcessing(t *testing.T) {
	repoStore, embStore := setupTestDB(t)
	repoDir := createFakeRepo(t, repoStore, "batch")

	// Create enough files to trigger multiple batches
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("file%d.go", i)
		content := fmt.Sprintf("package main\nfunc F%d() {}\n", i)
		writeFile(t, repoDir, name, content)
	}

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)
	idx.SetBatchSize(2) // small batch to test batching

	if err := idx.Index("batch"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	repo, _ := repoStore.Get("batch")
	if repo.FileCount != 5 {
		t.Errorf("fileCount = %d, want 5", repo.FileCount)
	}
	if repo.ChunkCount < 5 {
		t.Errorf("chunkCount = %d, want >= 5", repo.ChunkCount)
	}
}

func TestIndexer_EmbeddingsStored(t *testing.T) {
	repoStore, embStore := setupTestDB(t)
	repoDir := createFakeRepo(t, repoStore, "embcheck")

	writeFile(t, repoDir, "main.go", `package main
func Hello() {}
`)

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	if err := idx.Index("embcheck"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Get collection
	coll, err := embStore.GetOrCreateCollection("embcheck", "hash-embedder", 128)
	if err != nil {
		t.Fatalf("GetOrCreateCollection: %v", err)
	}

	chunks, err := embStore.GetChunksByCollection(coll.ID)
	if err != nil {
		t.Fatalf("GetChunksByCollection: %v", err)
	}

	if len(chunks) < 1 {
		t.Fatal("expected at least 1 chunk")
	}

	for _, c := range chunks {
		if len(c.Embedding) == 0 {
			t.Errorf("chunk %q has empty embedding", c.FilePath)
		}
		vec := storage.DecodeEmbedding(c.Embedding)
		if len(vec) != 128 {
			t.Errorf("decoded embedding length = %d, want 128", len(vec))
		}
		if c.ContentHash == "" {
			t.Errorf("chunk %q has empty content hash", c.FilePath)
		}
	}
}

func TestIndexer_ReindexClearsOldChunks(t *testing.T) {
	repoStore, embStore := setupTestDB(t)
	repoDir := createFakeRepo(t, repoStore, "reindex")

	writeFile(t, repoDir, "main.go", `package main
func Hello() {}
`)

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	// First index
	if err := idx.Index("reindex"); err != nil {
		t.Fatalf("first Index: %v", err)
	}

	repo, _ := repoStore.Get("reindex")
	firstCount := repo.ChunkCount

	// Reset status to allow re-index
	repo.Status = storage.RepoStatusCloned
	_ = repoStore.Update(repo)

	// Add another file
	writeFile(t, repoDir, "util.go", `package main
func Util() {}
`)

	// Second index
	if err := idx.Index("reindex"); err != nil {
		t.Fatalf("second Index: %v", err)
	}

	repo, _ = repoStore.Get("reindex")
	if repo.ChunkCount <= firstCount {
		t.Errorf("re-index should produce more chunks: got %d, first was %d", repo.ChunkCount, firstCount)
	}

	// Verify old chunks were replaced (not duplicated)
	coll, _ := embStore.GetOrCreateCollection("reindex", "hash-embedder", 128)
	chunks, _ := embStore.GetChunksByCollection(coll.ID)
	if len(chunks) != repo.ChunkCount {
		t.Errorf("actual chunks in DB = %d, repo says %d", len(chunks), repo.ChunkCount)
	}
}

func TestIndexer_ErrorSetsStatus(t *testing.T) {
	repoStore, embStore := setupTestDB(t)

	// Create repo with non-existent local path
	repo := &storage.Repo{
		Name:      "badpath",
		URL:       "https://example.com/bad",
		Branch:    "main",
		Status:    storage.RepoStatusCloned,
		LocalPath: "/nonexistent/path/that/does/not/exist",
	}
	_ = repoStore.Create(repo)

	emb := embedder.NewHashEmbedder(128)
	idx := NewIndexer(repoStore, embStore, emb)

	err := idx.Index("badpath")
	if err == nil {
		t.Fatal("expected error for bad path")
	}

	repo, _ = repoStore.Get("badpath")
	if repo.Status != storage.RepoStatusError {
		t.Errorf("status = %s, want error", repo.Status)
	}
	if repo.Error == nil {
		t.Error("error message should be set")
	}
}

func TestContentHash(t *testing.T) {
	h1 := contentHash("hello")
	h2 := contentHash("hello")
	h3 := contentHash("world")

	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == h3 {
		t.Error("different inputs should produce different hashes")
	}

	// Verify it's a valid SHA256 hex string
	expected := fmt.Sprintf("%x", sha256.Sum256([]byte("hello")))
	if h1 != expected {
		t.Errorf("contentHash = %q, want %q", h1, expected)
	}
}

func TestIsSupportedFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"app.py", true},
		{"index.ts", true},
		{"README.md", true},
		{"config.yaml", true},
		{"image.png", false},
		{"data.csv", false},
		{"Makefile", false},
	}
	for _, tt := range tests {
		if got := IsSupportedFile(tt.path); got != tt.want {
			t.Errorf("IsSupportedFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
