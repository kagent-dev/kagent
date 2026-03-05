package storage

import (
	"encoding/binary"
	"fmt"
	"math"

	"gorm.io/gorm"
)

// EmbeddingStore provides CRUD operations for collections and chunks.
type EmbeddingStore struct {
	db *gorm.DB
}

// NewEmbeddingStore creates a new EmbeddingStore.
func NewEmbeddingStore(db *gorm.DB) *EmbeddingStore {
	return &EmbeddingStore{db: db}
}

// GetOrCreateCollection returns the collection for a repo, creating it if needed.
func (s *EmbeddingStore) GetOrCreateCollection(repoName, model string, dimensions int) (*Collection, error) {
	var coll Collection
	err := s.db.Where("repo_name = ?", repoName).First(&coll).Error
	if err == nil {
		return &coll, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("failed to query collection for repo %s: %w", repoName, err)
	}

	coll = Collection{
		RepoName:   repoName,
		Model:      model,
		Dimensions: dimensions,
	}
	if err := s.db.Create(&coll).Error; err != nil {
		return nil, fmt.Errorf("failed to create collection for repo %s: %w", repoName, err)
	}
	return &coll, nil
}

// GetChunksByCollection returns all chunks for a collection.
func (s *EmbeddingStore) GetChunksByCollection(collectionID uint) ([]Chunk, error) {
	var chunks []Chunk
	if err := s.db.Where("collection_id = ?", collectionID).Find(&chunks).Error; err != nil {
		return nil, fmt.Errorf("failed to get chunks for collection %d: %w", collectionID, err)
	}
	return chunks, nil
}

// ChunkExistsByHash checks if a chunk with the given content hash exists in the collection.
func (s *EmbeddingStore) ChunkExistsByHash(collectionID uint, contentHash string) (bool, error) {
	var count int64
	err := s.db.Model(&Chunk{}).
		Where("collection_id = ? AND content_hash = ?", collectionID, contentHash).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check chunk hash: %w", err)
	}
	return count > 0, nil
}

// InsertChunks inserts multiple chunks in a transaction.
func (s *EmbeddingStore) InsertChunks(chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&chunks).Error; err != nil {
			return fmt.Errorf("failed to insert chunks: %w", err)
		}
		return nil
	})
}

// DeleteChunksByFile removes all chunks for a specific file in a collection.
func (s *EmbeddingStore) DeleteChunksByFile(collectionID uint, filePath string) error {
	err := s.db.Where("collection_id = ? AND file_path = ?", collectionID, filePath).
		Delete(&Chunk{}).Error
	if err != nil {
		return fmt.Errorf("failed to delete chunks for file %s: %w", filePath, err)
	}
	return nil
}

// DeleteChunksByCollection removes all chunks for a collection.
func (s *EmbeddingStore) DeleteChunksByCollection(collectionID uint) error {
	err := s.db.Where("collection_id = ?", collectionID).Delete(&Chunk{}).Error
	if err != nil {
		return fmt.Errorf("failed to delete chunks for collection %d: %w", collectionID, err)
	}
	return nil
}

// EncodeEmbedding converts a float32 slice to a little-endian byte slice.
func EncodeEmbedding(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// DecodeEmbedding converts a little-endian byte slice to a float32 slice.
func DecodeEmbedding(data []byte) []float32 {
	n := len(data) / 4
	vec := make([]float32, n)
	for i := 0; i < n; i++ {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vec
}
