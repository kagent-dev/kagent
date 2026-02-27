package indexer

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/embedder"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
)

const (
	defaultBatchSize = 32
	maxFileSize      = 1 << 20 // 1 MB
)

// Indexer orchestrates file walking, chunking, embedding, and storage.
type Indexer struct {
	repoStore      *storage.RepoStore
	embeddingStore *storage.EmbeddingStore
	embedder       embedder.EmbeddingModel
	batchSize      int
}

// NewIndexer creates an Indexer.
func NewIndexer(
	repoStore *storage.RepoStore,
	embeddingStore *storage.EmbeddingStore,
	emb embedder.EmbeddingModel,
) *Indexer {
	return &Indexer{
		repoStore:      repoStore,
		embeddingStore: embeddingStore,
		embedder:       emb,
		batchSize:      defaultBatchSize,
	}
}

// SetBatchSize overrides the default embedding batch size.
func (idx *Indexer) SetBatchSize(n int) {
	if n > 0 {
		idx.batchSize = n
	}
}

// Index indexes all supported files in a repository:
// walk files → chunk → content-hash dedup → batch embed → store.
func (idx *Indexer) Index(repoName string) error {
	repo, err := idx.repoStore.Get(repoName)
	if err != nil {
		return fmt.Errorf("repo %s not found: %w", repoName, err)
	}

	if repo.Status == storage.RepoStatusCloning || repo.Status == storage.RepoStatusIndexing {
		return fmt.Errorf("repo %s is busy (status: %s)", repoName, repo.Status)
	}

	// Set status to indexing
	repo.Status = storage.RepoStatusIndexing
	repo.Error = nil
	if err := idx.repoStore.Update(repo); err != nil {
		return fmt.Errorf("failed to update repo status: %w", err)
	}

	// Run indexing, capture any error to set error status
	fileCount, chunkCount, indexErr := idx.doIndex(repo)

	now := time.Now()
	if indexErr != nil {
		errMsg := indexErr.Error()
		repo.Status = storage.RepoStatusError
		repo.Error = &errMsg
		_ = idx.repoStore.Update(repo)
		return indexErr
	}

	repo.Status = storage.RepoStatusIndexed
	repo.LastIndexed = &now
	repo.FileCount = fileCount
	repo.ChunkCount = chunkCount
	repo.Error = nil
	if err := idx.repoStore.Update(repo); err != nil {
		return fmt.Errorf("failed to update repo after indexing: %w", err)
	}

	return nil
}

func (idx *Indexer) doIndex(repo *storage.Repo) (fileCount, chunkCount int, err error) {
	coll, err := idx.embeddingStore.GetOrCreateCollection(
		repo.Name,
		idx.embedder.ModelName(),
		idx.embedder.Dimensions(),
	)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get/create collection: %w", err)
	}

	// Delete existing chunks for a clean re-index
	if err := idx.embeddingStore.DeleteChunksByCollection(coll.ID); err != nil {
		return 0, 0, fmt.Errorf("failed to clear old chunks: %w", err)
	}

	// Walk and chunk all supported files
	type pendingChunk struct {
		chunk       Chunk
		contentHash string
	}
	var pending []pendingChunk

	walkErr := filepath.WalkDir(repo.LocalPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Skip hidden directories
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip non-regular files
		if !d.Type().IsRegular() {
			return nil
		}

		// Skip files with no known language
		if DetectLanguage(path) == "" {
			return nil
		}

		// Skip large files
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if info.Size() > maxFileSize {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			log.Printf("warn: skip unreadable file %s: %v", path, readErr)
			return nil
		}

		relPath, _ := filepath.Rel(repo.LocalPath, path)

		chunks, chunkErr := ChunkFile(relPath, content)
		if chunkErr != nil {
			log.Printf("warn: skip unchunkable file %s: %v", relPath, chunkErr)
			return nil
		}

		fileCount++
		for _, c := range chunks {
			hash := contentHash(c.Content)
			pending = append(pending, pendingChunk{chunk: c, contentHash: hash})
		}

		return nil
	})
	if walkErr != nil {
		return 0, 0, fmt.Errorf("failed to walk repo directory: %w", walkErr)
	}

	if len(pending) == 0 {
		return fileCount, 0, nil
	}

	// Batch embed and store
	for batchStart := 0; batchStart < len(pending); batchStart += idx.batchSize {
		batchEnd := batchStart + idx.batchSize
		if batchEnd > len(pending) {
			batchEnd = len(pending)
		}
		batch := pending[batchStart:batchEnd]

		// Collect texts for embedding
		texts := make([]string, len(batch))
		for i, p := range batch {
			texts[i] = p.chunk.Content
		}

		vectors, embedErr := idx.embedder.EmbedBatch(texts)
		if embedErr != nil {
			return 0, 0, fmt.Errorf("embedding batch failed: %w", embedErr)
		}

		// Build storage chunks
		storageChunks := make([]storage.Chunk, len(batch))
		for i, p := range batch {
			var name *string
			if p.chunk.ChunkName != "" {
				n := p.chunk.ChunkName
				name = &n
			}
			storageChunks[i] = storage.Chunk{
				CollectionID: coll.ID,
				FilePath:     p.chunk.FilePath,
				LineStart:    p.chunk.LineStart,
				LineEnd:      p.chunk.LineEnd,
				ChunkType:    p.chunk.ChunkType,
				ChunkName:    name,
				Content:      p.chunk.Content,
				ContentHash:  p.contentHash,
				Embedding:    storage.EncodeEmbedding(vectors[i]),
			}
		}

		if err := idx.embeddingStore.InsertChunks(storageChunks); err != nil {
			return 0, 0, fmt.Errorf("failed to insert chunk batch: %w", err)
		}

		chunkCount += len(storageChunks)
	}

	return fileCount, chunkCount, nil
}

// contentHash returns the SHA256 hex digest of a string.
func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// IsSupportedFile returns true if the file path has a known language extension.
func IsSupportedFile(filePath string) bool {
	return DetectLanguage(filePath) != ""
}
