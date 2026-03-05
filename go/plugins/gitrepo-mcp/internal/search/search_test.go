package search

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/plugins/gitrepo-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/plugins/gitrepo-mcp/internal/embedder"
	"github.com/kagent-dev/kagent/go/plugins/gitrepo-mcp/internal/storage"
)

// --- Cosine similarity tests ---

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1, 2, 3, 4}
	score := CosineSimilarity(a, a)
	if math.Abs(score-1.0) > 1e-6 {
		t.Errorf("identical vectors: want 1.0, got %f", score)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	score := CosineSimilarity(a, b)
	if math.Abs(score) > 1e-6 {
		t.Errorf("orthogonal vectors: want 0.0, got %f", score)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	score := CosineSimilarity(a, b)
	if math.Abs(score+1.0) > 1e-6 {
		t.Errorf("opposite vectors: want -1.0, got %f", score)
	}
}

func TestCosineSimilarity_KnownAngle(t *testing.T) {
	// 45 degrees: cos(45°) ≈ 0.7071
	a := []float32{1, 0}
	b := []float32{1, 1}
	score := CosineSimilarity(a, b)
	expected := 1.0 / math.Sqrt(2.0)
	if math.Abs(score-expected) > 1e-6 {
		t.Errorf("45-degree angle: want %f, got %f", expected, score)
	}
}

func TestCosineSimilarity_EmptyVectors(t *testing.T) {
	score := CosineSimilarity(nil, nil)
	if score != 0 {
		t.Errorf("empty vectors: want 0.0, got %f", score)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2}
	score := CosineSimilarity(a, b)
	if score != 0 {
		t.Errorf("different lengths: want 0.0, got %f", score)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	score := CosineSimilarity(a, b)
	if score != 0 {
		t.Errorf("zero vector: want 0.0, got %f", score)
	}
}

// --- Context extraction tests ---

func TestExtractContext_Basic(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\n"
	if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ExtractContext(dir, "test.go", 3, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if ctx == nil {
		t.Fatal("expected context, got nil")
	}

	if len(ctx.Before) != 2 {
		t.Errorf("before lines: want 2, got %d", len(ctx.Before))
	}
	if ctx.Before[0] != "line1" || ctx.Before[1] != "line2" {
		t.Errorf("before: want [line1, line2], got %v", ctx.Before)
	}

	if len(ctx.After) != 2 {
		t.Errorf("after lines: want 2, got %d", len(ctx.After))
	}
	if ctx.After[0] != "line6" || ctx.After[1] != "line7" {
		t.Errorf("after: want [line6, line7], got %v", ctx.After)
	}
}

func TestExtractContext_AtFileStart(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ExtractContext(dir, "test.go", 1, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if ctx == nil {
		t.Fatal("expected context, got nil")
	}

	if len(ctx.Before) != 0 {
		t.Errorf("before at start: want 0, got %d", len(ctx.Before))
	}
	if len(ctx.After) != 3 {
		t.Errorf("after: want 3, got %d", len(ctx.After))
	}
}

func TestExtractContext_AtFileEnd(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ExtractContext(dir, "test.go", 4, 5, 3)
	if err != nil {
		t.Fatal(err)
	}
	if ctx == nil {
		t.Fatal("expected context, got nil")
	}

	if len(ctx.Before) != 3 {
		t.Errorf("before: want 3, got %d", len(ctx.Before))
	}
	if len(ctx.After) != 0 {
		t.Errorf("after at end: want 0, got %d", len(ctx.After))
	}
}

func TestExtractContext_ZeroContextLines(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ExtractContext(dir, "test.go", 1, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ctx != nil {
		t.Errorf("zero context: want nil, got %+v", ctx)
	}
}

func TestExtractContext_FileNotFound(t *testing.T) {
	_, err := ExtractContext(t.TempDir(), "missing.go", 1, 1, 1)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestExtractContext_SingleLineFile(t *testing.T) {
	dir := t.TempDir()
	content := "only line"
	if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ExtractContext(dir, "test.go", 1, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	// No before or after lines for a single-line file
	if ctx != nil {
		t.Errorf("single line file: want nil context, got %+v", ctx)
	}
}

// --- Searcher integration tests (using in-memory DB + HashEmbedder) ---

func setupTestSearcher(t *testing.T) (*Searcher, *storage.RepoStore, *storage.EmbeddingStore, embedder.EmbeddingModel) {
	t.Helper()

	dir := t.TempDir()
	cfg := &config.Config{
		DBType:  config.DBTypeSQLite,
		DBPath:  filepath.Join(dir, "test.db"),
		DataDir: dir,
	}
	mgr, err := storage.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Initialize(); err != nil {
		t.Fatal(err)
	}

	repoStore := storage.NewRepoStore(mgr.DB())
	embStore := storage.NewEmbeddingStore(mgr.DB())
	emb := embedder.NewHashEmbedder(64)

	s := NewSearcher(repoStore, embStore, emb)
	return s, repoStore, embStore, emb
}

func createIndexedRepo(t *testing.T, repoStore *storage.RepoStore, embStore *storage.EmbeddingStore, emb embedder.EmbeddingModel, name, localPath string, chunkTexts []string) {
	t.Helper()

	now := time.Now()
	repo := &storage.Repo{
		Name:        name,
		URL:         "https://example.com/" + name,
		Branch:      "main",
		Status:      storage.RepoStatusIndexed,
		LocalPath:   localPath,
		LastIndexed: &now,
		FileCount:   1,
		ChunkCount:  len(chunkTexts),
	}
	if err := repoStore.Create(repo); err != nil {
		t.Fatal(err)
	}

	coll, err := embStore.GetOrCreateCollection(name, emb.ModelName(), emb.Dimensions())
	if err != nil {
		t.Fatal(err)
	}

	vectors, err := emb.EmbedBatch(chunkTexts)
	if err != nil {
		t.Fatal(err)
	}

	chunks := make([]storage.Chunk, len(chunkTexts))
	for i, text := range chunkTexts {
		n := "chunk" + string(rune('A'+i))
		chunks[i] = storage.Chunk{
			CollectionID: coll.ID,
			FilePath:     "test.go",
			LineStart:    i*10 + 1,
			LineEnd:      i*10 + 10,
			ChunkType:    "function",
			ChunkName:    &n,
			Content:      text,
			ContentHash:  "hash" + string(rune('A'+i)),
			Embedding:    storage.EncodeEmbedding(vectors[i]),
		}
	}

	if err := embStore.InsertChunks(chunks); err != nil {
		t.Fatal(err)
	}
}

func TestSearcher_BasicSearch(t *testing.T) {
	s, repoStore, embStore, emb := setupTestSearcher(t)

	texts := []string{
		"func authenticate(user, pass string) error",
		"func listUsers() []User",
		"func parseConfig(path string) Config",
	}
	createIndexedRepo(t, repoStore, embStore, emb, "test-repo", t.TempDir(), texts)

	// Search with exact chunk text — HashEmbedder is deterministic so identical text → score 1.0
	results, err := s.Search(texts[0], "test-repo", 10, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}

	// First result should be the exact match (score 1.0)
	if results[0].Content != texts[0] {
		t.Errorf("top result: want %q, got %q", texts[0], results[0].Content)
	}
	if results[0].Score != 1.0 {
		t.Errorf("exact match score: want 1.0, got %f", results[0].Score)
	}

	// Scores should be descending
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: score[%d]=%f > score[%d]=%f", i, results[i].Score, i-1, results[i-1].Score)
		}
	}

	// All results should have repo name
	for _, r := range results {
		if r.Repo != "test-repo" {
			t.Errorf("want repo=test-repo, got %s", r.Repo)
		}
	}
}

func TestSearcher_LimitResults(t *testing.T) {
	s, repoStore, embStore, emb := setupTestSearcher(t)

	texts := []string{"func a()", "func b()", "func c()", "func d()", "func e()"}
	createIndexedRepo(t, repoStore, embStore, emb, "test-repo", t.TempDir(), texts)

	results, err := s.Search("func", "test-repo", 2, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Fatalf("want 2 results (limit), got %d", len(results))
	}
}

func TestSearcher_EmptyQuery(t *testing.T) {
	s, _, _, _ := setupTestSearcher(t)

	_, err := s.Search("", "test-repo", 10, 0)
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestSearcher_RepoNotFound(t *testing.T) {
	s, _, _, _ := setupTestSearcher(t)

	_, err := s.Search("query", "nonexistent", 10, 0)
	if err == nil {
		t.Error("expected error for missing repo")
	}
}

func TestSearcher_RepoNotIndexed(t *testing.T) {
	s, repoStore, _, _ := setupTestSearcher(t)

	repo := &storage.Repo{
		Name:      "unindexed",
		URL:       "https://example.com/unindexed",
		Branch:    "main",
		Status:    storage.RepoStatusCloned,
		LocalPath: "/tmp/unindexed",
	}
	if err := repoStore.Create(repo); err != nil {
		t.Fatal(err)
	}

	_, err := s.Search("query", "unindexed", 10, 0)
	if err == nil {
		t.Error("expected error for unindexed repo")
	}
}

func TestSearcher_EmptyRepo(t *testing.T) {
	s, repoStore, _, _ := setupTestSearcher(t)

	now := time.Now()
	repo := &storage.Repo{
		Name:        "empty-repo",
		URL:         "https://example.com/empty",
		Branch:      "main",
		Status:      storage.RepoStatusIndexed,
		LocalPath:   "/tmp/empty",
		LastIndexed: &now,
	}
	if err := repoStore.Create(repo); err != nil {
		t.Fatal(err)
	}

	results, err := s.Search("query", "empty-repo", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("want 0 results for empty repo, got %d", len(results))
	}
}

func TestSearcher_WithContext(t *testing.T) {
	s, repoStore, embStore, emb := setupTestSearcher(t)

	// Create a real file for context extraction
	dir := t.TempDir()
	content := "package main\n\nimport \"fmt\"\n\nfunc hello() {\n\tfmt.Println(\"hello\")\n}\n\nfunc world() {\n\tfmt.Println(\"world\")\n}\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create indexed repo with file pointing to real dir
	now := time.Now()
	repo := &storage.Repo{
		Name:        "ctx-repo",
		URL:         "https://example.com/ctx",
		Branch:      "main",
		Status:      storage.RepoStatusIndexed,
		LocalPath:   dir,
		LastIndexed: &now,
		FileCount:   1,
		ChunkCount:  1,
	}
	if err := repoStore.Create(repo); err != nil {
		t.Fatal(err)
	}

	coll, err := embStore.GetOrCreateCollection("ctx-repo", emb.ModelName(), emb.Dimensions())
	if err != nil {
		t.Fatal(err)
	}

	chunkContent := "func hello() {\n\tfmt.Println(\"hello\")\n}"
	vectors, err := emb.EmbedBatch([]string{chunkContent})
	if err != nil {
		t.Fatal(err)
	}

	name := "hello"
	chunks := []storage.Chunk{{
		CollectionID: coll.ID,
		FilePath:     "main.go",
		LineStart:    5,
		LineEnd:      7,
		ChunkType:    "function",
		ChunkName:    &name,
		Content:      chunkContent,
		ContentHash:  "testhash",
		Embedding:    storage.EncodeEmbedding(vectors[0]),
	}}
	if err := embStore.InsertChunks(chunks); err != nil {
		t.Fatal(err)
	}

	results, err := s.Search("hello function", "ctx-repo", 1, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Context == nil {
		t.Fatal("expected context, got nil")
	}

	// Lines 5-7, context=2 → before=lines 3-4, after=lines 8-9
	if len(r.Context.Before) != 2 {
		t.Errorf("before lines: want 2, got %d: %v", len(r.Context.Before), r.Context.Before)
	}
	if len(r.Context.After) != 2 {
		t.Errorf("after lines: want 2, got %d: %v", len(r.Context.After), r.Context.After)
	}
}

func TestSearcher_ScoreRounding(t *testing.T) {
	s, repoStore, embStore, emb := setupTestSearcher(t)

	texts := []string{"func test()"}
	createIndexedRepo(t, repoStore, embStore, emb, "test-repo", t.TempDir(), texts)

	results, err := s.Search("func test()", "test-repo", 1, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}

	// Same text → same embedding → cosine similarity = 1.0
	if results[0].Score != 1.0 {
		t.Errorf("identical text score: want 1.0, got %f", results[0].Score)
	}
}

func TestSearcher_ChunkNamePopulated(t *testing.T) {
	s, repoStore, embStore, emb := setupTestSearcher(t)

	texts := []string{"func myFunction()"}
	createIndexedRepo(t, repoStore, embStore, emb, "test-repo", t.TempDir(), texts)

	results, err := s.Search("myFunction", "test-repo", 1, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}

	if results[0].ChunkName == "" {
		t.Error("chunk name should be populated")
	}
	if results[0].ChunkType != "function" {
		t.Errorf("chunk type: want function, got %s", results[0].ChunkType)
	}
}
