package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoStore_CRUD(t *testing.T) {
	mgr := newTestManager(t)
	store := NewRepoStore(mgr.DB())

	// Create
	repo := &Repo{
		Name:      "test-repo",
		URL:       "https://github.com/example/test.git",
		Branch:    "main",
		Status:    RepoStatusCloning,
		LocalPath: "/data/repos/test-repo",
	}
	require.NoError(t, store.Create(repo))

	// Get
	got, err := store.Get("test-repo")
	require.NoError(t, err)
	assert.Equal(t, "test-repo", got.Name)
	assert.Equal(t, "https://github.com/example/test.git", got.URL)
	assert.Equal(t, "main", got.Branch)
	assert.Equal(t, RepoStatusCloning, got.Status)
	assert.Equal(t, "/data/repos/test-repo", got.LocalPath)

	// List
	repos, err := store.List()
	require.NoError(t, err)
	assert.Len(t, repos, 1)

	// Update
	got.Status = RepoStatusCloned
	got.FileCount = 42
	require.NoError(t, store.Update(got))

	updated, err := store.Get("test-repo")
	require.NoError(t, err)
	assert.Equal(t, RepoStatusCloned, updated.Status)
	assert.Equal(t, 42, updated.FileCount)

	// Delete
	require.NoError(t, store.Delete("test-repo"))
	repos, err = store.List()
	require.NoError(t, err)
	assert.Len(t, repos, 0)
}

func TestRepoStore_Get_NotFound(t *testing.T) {
	mgr := newTestManager(t)
	store := NewRepoStore(mgr.DB())

	_, err := store.Get("nonexistent")
	assert.Error(t, err)
}

func TestRepoStore_Create_Duplicate(t *testing.T) {
	mgr := newTestManager(t)
	store := NewRepoStore(mgr.DB())

	repo := &Repo{
		Name:      "dup",
		URL:       "https://github.com/example/dup.git",
		Branch:    "main",
		Status:    RepoStatusCloning,
		LocalPath: "/data/repos/dup",
	}
	require.NoError(t, store.Create(repo))

	err := store.Create(repo)
	assert.Error(t, err)
}

func TestRepoStore_ListEmpty(t *testing.T) {
	mgr := newTestManager(t)
	store := NewRepoStore(mgr.DB())

	repos, err := store.List()
	require.NoError(t, err)
	assert.Len(t, repos, 0)
}

func TestRepoStore_ListOrdered(t *testing.T) {
	mgr := newTestManager(t)
	store := NewRepoStore(mgr.DB())

	for _, name := range []string{"charlie", "alpha", "bravo"} {
		require.NoError(t, store.Create(&Repo{
			Name:      name,
			URL:       "https://example.com/" + name + ".git",
			Branch:    "main",
			Status:    RepoStatusCloned,
			LocalPath: "/data/repos/" + name,
		}))
	}

	repos, err := store.List()
	require.NoError(t, err)
	require.Len(t, repos, 3)
	assert.Equal(t, "alpha", repos[0].Name)
	assert.Equal(t, "bravo", repos[1].Name)
	assert.Equal(t, "charlie", repos[2].Name)
}
