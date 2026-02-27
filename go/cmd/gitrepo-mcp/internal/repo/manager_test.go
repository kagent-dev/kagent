package repo

import (
	"os"
	"os/exec"
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

// --- git repo helper for sync tests ---

// createClonedGitRepo creates a bare repo + clone in reposDir/name, suitable for git pull.
func createClonedGitRepo(t *testing.T, reposDir, name string) {
	t.Helper()

	bareDir := filepath.Join(t.TempDir(), name+"-bare.git")
	runGit(t, "", "init", "--bare", "--initial-branch=main", bareDir)

	cloneDir := filepath.Join(reposDir, name)
	runGit(t, "", "clone", bareDir, cloneDir)

	require.NoError(t, os.WriteFile(filepath.Join(cloneDir, "file.txt"), []byte("hello"), 0o644))
	runGit(t, cloneDir, "add", "file.txt")
	runGitEnv(t, cloneDir, "commit", "-m", "initial")
	runGit(t, cloneDir, "push", "origin", "main")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, string(out))
}

func runGitEnv(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, string(out))
}

// --- SyncAndReindex ---

func TestManager_SyncAndReindex_IndexedRepo(t *testing.T) {
	mgr, repoStore := newTestManager(t)

	createClonedGitRepo(t, mgr.reposDir, "myrepo")

	err := repoStore.Create(&storage.Repo{
		Name:      "myrepo",
		URL:       "https://example.com/myrepo.git",
		Branch:    "main",
		Status:    storage.RepoStatusIndexed,
		LocalPath: filepath.Join(mgr.reposDir, "myrepo"),
	})
	require.NoError(t, err)

	var reindexCalled bool
	reindexFn := func(name string) error {
		reindexCalled = true
		require.Equal(t, "myrepo", name)
		return nil
	}

	repo, reindexed, err := mgr.SyncAndReindex("myrepo", reindexFn)
	require.NoError(t, err)
	require.True(t, reindexed)
	require.True(t, reindexCalled)
	require.NotNil(t, repo.LastSynced)
}

func TestManager_SyncAndReindex_ClonedRepo_NoReindex(t *testing.T) {
	mgr, repoStore := newTestManager(t)

	createClonedGitRepo(t, mgr.reposDir, "clonedrepo")

	err := repoStore.Create(&storage.Repo{
		Name:      "clonedrepo",
		URL:       "https://example.com/cloned.git",
		Branch:    "main",
		Status:    storage.RepoStatusCloned,
		LocalPath: filepath.Join(mgr.reposDir, "clonedrepo"),
	})
	require.NoError(t, err)

	reindexCalled := false
	reindexFn := func(_ string) error {
		reindexCalled = true
		return nil
	}

	repo, reindexed, err := mgr.SyncAndReindex("clonedrepo", reindexFn)
	require.NoError(t, err)
	require.False(t, reindexed)
	require.False(t, reindexCalled)
	require.NotNil(t, repo.LastSynced)
}

func TestManager_SyncAndReindex_NilReindexFn(t *testing.T) {
	mgr, repoStore := newTestManager(t)

	createClonedGitRepo(t, mgr.reposDir, "nilreindex")

	err := repoStore.Create(&storage.Repo{
		Name:      "nilreindex",
		URL:       "https://example.com/nil.git",
		Branch:    "main",
		Status:    storage.RepoStatusIndexed,
		LocalPath: filepath.Join(mgr.reposDir, "nilreindex"),
	})
	require.NoError(t, err)

	repo, reindexed, err := mgr.SyncAndReindex("nilreindex", nil)
	require.NoError(t, err)
	require.False(t, reindexed)
	require.NotNil(t, repo.LastSynced)
}

func TestManager_SyncAndReindex_NotFound(t *testing.T) {
	mgr, _ := newTestManager(t)

	_, _, err := mgr.SyncAndReindex("nonexistent", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

// --- SyncAll ---

func TestManager_SyncAll_Empty(t *testing.T) {
	mgr, _ := newTestManager(t)

	results, err := mgr.SyncAll(nil)
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestManager_SyncAll_SkipsBusy(t *testing.T) {
	mgr, repoStore := newTestManager(t)

	err := repoStore.Create(&storage.Repo{
		Name: "busy", URL: "https://example.com/busy.git", Branch: "main",
		Status: storage.RepoStatusCloning, LocalPath: "/tmp/busy",
	})
	require.NoError(t, err)

	results, err := mgr.SyncAll(nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "busy", results[0].Name)
	require.False(t, results[0].Synced)
	require.Contains(t, results[0].Error, "busy")
}

func TestManager_SyncAll_MixedResults(t *testing.T) {
	mgr, repoStore := newTestManager(t)

	// Create a syncable repo with real git
	createClonedGitRepo(t, mgr.reposDir, "good")
	err := repoStore.Create(&storage.Repo{
		Name: "good", URL: "https://example.com/good.git", Branch: "main",
		Status: storage.RepoStatusCloned, LocalPath: filepath.Join(mgr.reposDir, "good"),
	})
	require.NoError(t, err)

	// Create a busy repo
	err = repoStore.Create(&storage.Repo{
		Name: "busy", URL: "https://example.com/busy.git", Branch: "main",
		Status: storage.RepoStatusIndexing, LocalPath: "/tmp/busy",
	})
	require.NoError(t, err)

	results, err := mgr.SyncAll(nil)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// Results are ordered by name (alpha sort from DB)
	require.Equal(t, "busy", results[0].Name)
	require.False(t, results[0].Synced)
	require.Contains(t, results[0].Error, "busy")

	require.Equal(t, "good", results[1].Name)
	require.True(t, results[1].Synced)
	require.False(t, results[1].Reindexed)
}
