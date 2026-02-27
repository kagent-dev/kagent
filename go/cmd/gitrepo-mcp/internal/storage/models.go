package storage

import (
	"time"
)

// RepoStatus represents the state of a git repository.
type RepoStatus string

const (
	RepoStatusCloning  RepoStatus = "cloning"
	RepoStatusCloned   RepoStatus = "cloned"
	RepoStatusIndexing RepoStatus = "indexing"
	RepoStatusIndexed  RepoStatus = "indexed"
	RepoStatusError    RepoStatus = "error"
)

// Repo is the GORM model for a git repository.
type Repo struct {
	Name        string     `gorm:"primaryKey;type:text" json:"name"`
	URL         string     `gorm:"not null;type:text" json:"url"`
	Branch      string     `gorm:"not null;default:'main';type:text" json:"branch"`
	Status      RepoStatus `gorm:"not null;default:'cloning';type:text" json:"status"`
	LocalPath   string     `gorm:"not null;type:text" json:"localPath"`
	LastSynced  *time.Time `json:"lastSynced,omitempty"`
	LastIndexed *time.Time `json:"lastIndexed,omitempty"`
	FileCount   int        `gorm:"default:0" json:"fileCount"`
	ChunkCount  int        `gorm:"default:0" json:"chunkCount"`
	Error       *string    `gorm:"type:text" json:"error,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// Collection represents an embedding collection for a repo.
type Collection struct {
	ID         uint   `gorm:"primaryKey;autoIncrement"`
	RepoName   string `gorm:"not null;uniqueIndex;type:text"`
	Repo       Repo   `gorm:"foreignKey:RepoName;references:Name;constraint:OnDelete:CASCADE"`
	Model      string `gorm:"not null;type:text"`
	Dimensions int    `gorm:"not null"`
}

// Chunk represents a code chunk with its embedding.
type Chunk struct {
	ID           uint       `gorm:"primaryKey;autoIncrement"`
	CollectionID uint       `gorm:"not null;index:idx_chunks_collection"`
	Collection   Collection `gorm:"foreignKey:CollectionID;constraint:OnDelete:CASCADE"`
	FilePath     string     `gorm:"not null;type:text;index:idx_chunks_file"`
	LineStart    int        `gorm:"not null"`
	LineEnd      int        `gorm:"not null"`
	ChunkType    string     `gorm:"not null;type:text"` // "function", "method", "class", "heading", "document"
	ChunkName    *string    `gorm:"type:text"`
	Content      string     `gorm:"not null;type:text"`
	ContentHash  string     `gorm:"not null;type:text;index:idx_chunks_hash"`
	Embedding    []byte     `gorm:"not null;type:blob"`
	Metadata     *string    `gorm:"type:text"` // JSON
	CreatedAt    time.Time
}
