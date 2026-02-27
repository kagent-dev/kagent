package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
)

// Manager handles git repository lifecycle operations.
type Manager struct {
	repoStore *storage.RepoStore
	reposDir  string
}

// NewManager creates a repo Manager. reposDir is the directory where repos are cloned.
func NewManager(repoStore *storage.RepoStore, reposDir string) *Manager {
	return &Manager{
		repoStore: repoStore,
		reposDir:  reposDir,
	}
}

// Add clones a git repository and registers it in the database.
func (m *Manager) Add(name, url, branch string) (*storage.Repo, error) {
	if name == "" {
		return nil, fmt.Errorf("repo name is required")
	}
	if url == "" {
		return nil, fmt.Errorf("repo URL is required")
	}
	if branch == "" {
		branch = "main"
	}

	localPath := filepath.Join(m.reposDir, name)

	repo := &storage.Repo{
		Name:      name,
		URL:       url,
		Branch:    branch,
		Status:    storage.RepoStatusCloning,
		LocalPath: localPath,
	}

	if err := m.repoStore.Create(repo); err != nil {
		return nil, fmt.Errorf("failed to register repo %s: %w", name, err)
	}

	if err := m.cloneRepo(url, branch, localPath); err != nil {
		errMsg := err.Error()
		repo.Status = storage.RepoStatusError
		repo.Error = &errMsg
		_ = m.repoStore.Update(repo)
		return nil, fmt.Errorf("failed to clone repo %s: %w", name, err)
	}

	now := time.Now()
	repo.Status = storage.RepoStatusCloned
	repo.LastSynced = &now
	if err := m.repoStore.Update(repo); err != nil {
		return nil, fmt.Errorf("failed to update repo status: %w", err)
	}

	return repo, nil
}

// Get returns a single repo by name.
func (m *Manager) Get(name string) (*storage.Repo, error) {
	return m.repoStore.Get(name)
}

// List returns all registered repos.
func (m *Manager) List() ([]storage.Repo, error) {
	return m.repoStore.List()
}

// Remove deletes a repo from the database and removes its cloned directory.
func (m *Manager) Remove(name string) error {
	repo, err := m.repoStore.Get(name)
	if err != nil {
		return fmt.Errorf("repo %s not found: %w", name, err)
	}

	if repo.LocalPath != "" {
		if err := os.RemoveAll(repo.LocalPath); err != nil {
			return fmt.Errorf("failed to remove repo directory %s: %w", repo.LocalPath, err)
		}
	}

	if err := m.repoStore.Delete(name); err != nil {
		return fmt.Errorf("failed to delete repo %s from database: %w", name, err)
	}

	return nil
}

// Sync pulls latest changes for a repo.
func (m *Manager) Sync(name string) (*storage.Repo, error) {
	repo, err := m.repoStore.Get(name)
	if err != nil {
		return nil, fmt.Errorf("repo %s not found: %w", name, err)
	}

	if repo.Status == storage.RepoStatusCloning || repo.Status == storage.RepoStatusIndexing {
		return nil, fmt.Errorf("repo %s is busy (status: %s)", name, repo.Status)
	}

	if err := m.pullRepo(repo.LocalPath); err != nil {
		errMsg := err.Error()
		repo.Status = storage.RepoStatusError
		repo.Error = &errMsg
		_ = m.repoStore.Update(repo)
		return nil, fmt.Errorf("failed to sync repo %s: %w", name, err)
	}

	now := time.Now()
	repo.LastSynced = &now
	repo.Error = nil
	if repo.Status == storage.RepoStatusError {
		repo.Status = storage.RepoStatusCloned
	}
	if err := m.repoStore.Update(repo); err != nil {
		return nil, fmt.Errorf("failed to update repo after sync: %w", err)
	}

	return repo, nil
}

// cloneRepo runs git clone with shallow depth.
func (m *Manager) cloneRepo(url, branch, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	cmd := exec.Command("git", "clone",
		"--branch", branch,
		"--single-branch",
		"--depth", "1",
		url, dest,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}

// pullRepo runs git pull in the repo directory.
func (m *Manager) pullRepo(dir string) error {
	cmd := exec.Command("git", "-C", dir, "pull", "--ff-only")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}
	return nil
}
