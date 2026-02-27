package storage

import (
	"fmt"

	"gorm.io/gorm"
)

// RepoStore provides CRUD operations for repos.
type RepoStore struct {
	db *gorm.DB
}

// NewRepoStore creates a new RepoStore.
func NewRepoStore(db *gorm.DB) *RepoStore {
	return &RepoStore{db: db}
}

// Create inserts a new repo.
func (s *RepoStore) Create(repo *Repo) error {
	if err := s.db.Create(repo).Error; err != nil {
		return fmt.Errorf("failed to create repo %s: %w", repo.Name, err)
	}
	return nil
}

// Get retrieves a repo by name.
func (s *RepoStore) Get(name string) (*Repo, error) {
	var repo Repo
	if err := s.db.Where("name = ?", name).First(&repo).Error; err != nil {
		return nil, fmt.Errorf("failed to get repo %s: %w", name, err)
	}
	return &repo, nil
}

// List retrieves all repos.
func (s *RepoStore) List() ([]Repo, error) {
	var repos []Repo
	if err := s.db.Order("name ASC").Find(&repos).Error; err != nil {
		return nil, fmt.Errorf("failed to list repos: %w", err)
	}
	return repos, nil
}

// Update saves changes to an existing repo.
func (s *RepoStore) Update(repo *Repo) error {
	if err := s.db.Save(repo).Error; err != nil {
		return fmt.Errorf("failed to update repo %s: %w", repo.Name, err)
	}
	return nil
}

// Delete removes a repo by name. CASCADE deletes collections and chunks.
func (s *RepoStore) Delete(name string) error {
	if err := s.db.Where("name = ?", name).Delete(&Repo{}).Error; err != nil {
		return fmt.Errorf("failed to delete repo %s: %w", name, err)
	}
	return nil
}
