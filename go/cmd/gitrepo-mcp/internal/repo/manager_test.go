package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
	"github.com/stretchr/testify/require"
)

func newTestManager(t *testing.T) (*Manager, *storage.RepoStore) {
	t.Helper()
	cfg := &config.Config{
		DBType: config.DBTypeSQLite,
		DBPath: ":memory:",
	}
	dbMgr, err := storage.NewManager(cfg)
	require.NoError(t, err)
	require.NoError(t, dbMgr.Initialize())

	repoStore := storage.NewRepoStore(dbMgr.DB())
	reposDir := t.TempDir()
	mgr := NewManager(repoStore, reposDir)
	return mgr, repoStore
}

func TestManager_Add_ValidationErrors(t *testing.T) {
	mgr, _ := newTestManager(t)

	_, err := mgr.Add("", "https://example.com/repo.git", "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "repo name is required")

	_, err = mgr.Add("test", "", "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "repo URL is required")
}

func TestManager_Add_DuplicateName(t *testing.T) {
	mgr, repoStore := newTestManager(t)

	// Pre-create a repo in DB to simulate duplicate.
	err := repoStore.Create(&storage.Repo{
		Name:      "existing",
		URL:       "https://example.com/existing.git",
		Branch:    "main",
		Status:    storage.RepoStatusCloned,
		LocalPath: "/tmp/existing",
	})
	require.NoError(t, err)

	_, err = mgr.Add("existing", "https://example.com/other.git", "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to register repo")
}

func TestManager_Add_CloneFailure(t *testing.T) {
	mgr, repoStore := newTestManager(t)

	// Use a URL that will definitely fail to clone.
	_, err := mgr.Add("badrepo", "https://invalid.example.com/nonexistent.git", "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to clone repo")

	// Verify the repo was saved with error status.
	repo, err := repoStore.Get("badrepo")
	require.NoError(t, err)
	require.Equal(t, storage.RepoStatusError, repo.Status)
	require.NotNil(t, repo.Error)
}

func TestManager_Remove_NotFound(t *testing.T) {
	mgr, _ := newTestManager(t)

	err := mgr.Remove("nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestManager_Remove_CleansUpDirectory(t *testing.T) {
	mgr, repoStore := newTestManager(t)

	// Create a repo record and a fake directory.
	repoDir := filepath.Join(mgr.reposDir, "myrepo")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("hello"), 0o644))

	err := repoStore.Create(&storage.Repo{
		Name:      "myrepo",
		URL:       "https://example.com/myrepo.git",
		Branch:    "main",
		Status:    storage.RepoStatusCloned,
		LocalPath: repoDir,
	})
	require.NoError(t, err)

	// Remove should delete both DB record and directory.
	require.NoError(t, mgr.Remove("myrepo"))

	_, err = repoStore.Get("myrepo")
	require.Error(t, err)

	_, err = os.Stat(repoDir)
	require.True(t, os.IsNotExist(err))
}

func TestManager_Get(t *testing.T) {
	mgr, repoStore := newTestManager(t)

	err := repoStore.Create(&storage.Repo{
		Name:      "testrepo",
		URL:       "https://example.com/test.git",
		Branch:    "main",
		Status:    storage.RepoStatusCloned,
		LocalPath: "/tmp/testrepo",
	})
	require.NoError(t, err)

	repo, err := mgr.Get("testrepo")
	require.NoError(t, err)
	require.Equal(t, "testrepo", repo.Name)
	require.Equal(t, "https://example.com/test.git", repo.URL)
}

func TestManager_Get_NotFound(t *testing.T) {
	mgr, _ := newTestManager(t)

	_, err := mgr.Get("nonexistent")
	require.Error(t, err)
}

func TestManager_List(t *testing.T) {
	mgr, repoStore := newTestManager(t)

	// Empty list.
	repos, err := mgr.List()
	require.NoError(t, err)
	require.Empty(t, repos)

	// Add some repos.
	for _, name := range []string{"bravo", "alpha", "charlie"} {
		err := repoStore.Create(&storage.Repo{
			Name:      name,
			URL:       "https://example.com/" + name + ".git",
			Branch:    "main",
			Status:    storage.RepoStatusCloned,
			LocalPath: "/tmp/" + name,
		})
		require.NoError(t, err)
	}

	repos, err = mgr.List()
	require.NoError(t, err)
	require.Len(t, repos, 3)
	require.Equal(t, "alpha", repos[0].Name)
	require.Equal(t, "bravo", repos[1].Name)
	require.Equal(t, "charlie", repos[2].Name)
}

func TestManager_Sync_NotFound(t *testing.T) {
	mgr, _ := newTestManager(t)

	_, err := mgr.Sync("nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestManager_Sync_BusyStatus(t *testing.T) {
	mgr, repoStore := newTestManager(t)

	err := repoStore.Create(&storage.Repo{
		Name:      "busyrepo",
		URL:       "https://example.com/busy.git",
		Branch:    "main",
		Status:    storage.RepoStatusCloning,
		LocalPath: "/tmp/busyrepo",
	})
	require.NoError(t, err)

	_, err = mgr.Sync("busyrepo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "busy")
}

func TestManager_DefaultBranch(t *testing.T) {
	mgr, _ := newTestManager(t)

	// Add with empty branch should fail at clone (bad URL), but the DB record
	// should have branch defaulted to "main".
	_, _ = mgr.Add("defaultbranch", "https://invalid.example.com/x.git", "")

	repo, err := mgr.Get("defaultbranch")
	require.NoError(t, err)
	require.Equal(t, "main", repo.Branch)
}
