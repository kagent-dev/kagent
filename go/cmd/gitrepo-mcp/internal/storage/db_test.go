package storage

import (
	"testing"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	cfg := &config.Config{
		DBType: config.DBTypeSQLite,
		DBPath: ":memory:",
	}
	mgr, err := NewManager(cfg)
	require.NoError(t, err)
	require.NoError(t, mgr.Initialize())
	return mgr
}

func TestNewManager_SQLite(t *testing.T) {
	mgr := newTestManager(t)
	assert.NotNil(t, mgr.DB())
}

func TestNewManager_InvalidDBType(t *testing.T) {
	cfg := &config.Config{
		DBType: "invalid",
	}
	_, err := NewManager(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid database type")
}

func TestInitialize_CreatesTables(t *testing.T) {
	mgr := newTestManager(t)
	db := mgr.DB()

	// Verify tables exist by querying them
	var repoCount int64
	require.NoError(t, db.Model(&Repo{}).Count(&repoCount).Error)
	assert.Equal(t, int64(0), repoCount)

	var collCount int64
	require.NoError(t, db.Model(&Collection{}).Count(&collCount).Error)
	assert.Equal(t, int64(0), collCount)

	var chunkCount int64
	require.NoError(t, db.Model(&Chunk{}).Count(&chunkCount).Error)
	assert.Equal(t, int64(0), chunkCount)
}
